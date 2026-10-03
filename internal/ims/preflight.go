package ims

import (
	"context"
	"errors"
	"regexp"

	"github.com/leonfox28/simplus/internal/agentapi"
)

var hardwareLineIDPattern = regexp.MustCompile(`^agent-line-[0-9a-f]{32}$`)

// Inspection is transient authentication material. Callers must keep it in
// memory and must never log, persist, or return its identity fields.
type Inspection struct {
	Target      agentapi.SIMAKATarget
	IMSI        string
	IMSIdentity agentapi.SIMIMSIdentityResponse
}

// InspectHostVoWiFiLine resolves one Line through the Agent's evidence-backed
// host-vowifi-auth capability. The common worker does not know the modem model
// and does not inspect RF state; model-specific SIM communication remains
// behind the Agent adapter selected for the matched device.
func InspectHostVoWiFiLine(ctx context.Context, lineID string) (Inspection, error) {
	if !hardwareLineIDPattern.MatchString(lineID) {
		return Inspection{}, errors.New("invalid Host VoWiFi Line")
	}
	readOnly, err := agentapi.NewClient(ReadOnlyAgentSocket)
	if err != nil {
		return Inspection{}, err
	}
	snapshot, err := readOnly.Snapshot(ctx, true)
	if err != nil {
		return Inspection{}, err
	}
	deviceIDs := make([]string, 0, len(snapshot.Devices))
	deviceByID := make(map[string]agentapi.DeviceReport, len(snapshot.Devices))
	for _, device := range snapshot.Devices {
		if !hasObservedCapability(device, "sim-auth") || !hasObservedCapability(device, "host-vowifi-auth") {
			continue
		}
		deviceIDs = append(deviceIDs, device.ID)
		deviceByID[device.ID] = device
	}
	if len(deviceIDs) == 0 {
		return Inspection{}, errors.New("no Host VoWiFi authentication adapter is available")
	}
	probe, err := readOnly.Probe(ctx, agentapi.ProbeRequest{DeviceIDs: deviceIDs})
	if err != nil || probe.AgentInstanceID != snapshot.AgentInstanceID ||
		probe.SnapshotGeneration != snapshot.Generation || probe.SnapshotRevision != snapshot.Revision {
		return Inspection{}, errors.New("probe fence changed")
	}
	type match struct {
		device   agentapi.DeviceReport
		observed agentapi.DeviceProbe
	}
	matches := []match{}
	for _, observed := range probe.Devices {
		device, exists := deviceByID[observed.DeviceID]
		if !exists || observed.State != agentapi.ProbeStateComplete ||
			observed.SIM.State != agentapi.SIMStatePresent || observed.SIM.PrimaryLockState != agentapi.PrimaryLockReady ||
			len(observed.SIM.IdentityFingerprint) != 64 || observed.ActiveCallCount == nil || *observed.ActiveCallCount != 0 {
			continue
		}
		if "agent-line-"+observed.SIM.IdentityFingerprint[:32] == lineID {
			matches = append(matches, match{device: device, observed: observed})
		}
	}
	if len(matches) != 1 {
		return Inspection{}, errors.New("Host VoWiFi Line does not resolve to exactly one ready SIM authentication adapter")
	}
	selected := matches[0]
	target := agentapi.SIMAKATarget{
		AgentInstanceID: snapshot.AgentInstanceID, SnapshotGeneration: snapshot.Generation,
		SnapshotRevision: snapshot.Revision, DeviceID: selected.device.ID, DeviceGeneration: selected.device.Generation,
		IdentityFingerprint: selected.observed.SIM.IdentityFingerprint,
	}
	return readSIMAuthInspection(ctx, snapshot.AgentInstanceID, target)
}

func readSIMAuthInspection(ctx context.Context, agentInstanceID string, target agentapi.SIMAKATarget) (Inspection, error) {
	rootOnly, err := agentapi.NewClient(SIMAKASocket)
	if err != nil {
		return Inspection{}, err
	}
	hello, err := rootOnly.Hello(ctx)
	if err != nil || hello.AgentInstanceID != agentInstanceID ||
		!containsString(hello.Features, agentapi.FeatureSIMAKAHIL) ||
		!containsString(hello.Features, agentapi.FeatureSIMIMSHIL) {
		return Inspection{}, errors.New("SIM authentication socket is not current")
	}
	identity, err := rootOnly.SIMAKAIdentity(ctx, agentapi.SIMAKAIdentityRequest{SIMAKATarget: target})
	if err != nil {
		return Inspection{}, err
	}
	profile, err := rootOnly.SIMIMSProfile(ctx, agentapi.SIMIMSProfileRequest{SIMAKATarget: target})
	if err != nil {
		return Inspection{}, err
	}
	imsIdentity, err := rootOnly.SIMIMSIdentity(ctx, agentapi.SIMIMSIdentityRequest{SIMAKATarget: target})
	if err != nil || imsIdentity.IdentitySource != profile.IdentitySource {
		return Inspection{}, errors.New("SIM IMS identity is unavailable")
	}
	return Inspection{Target: target, IMSI: identity.IMSI, IMSIdentity: imsIdentity}, nil
}

func hasObservedCapability(device agentapi.DeviceReport, expected string) bool {
	for _, capability := range device.Capabilities {
		if capability.Capability == expected {
			return capability.Status == agentapi.EvidenceObserved
		}
	}
	return false
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
