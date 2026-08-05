package vowifihil

import (
	"context"
	"errors"

	"github.com/leonfox28/simplus/internal/agentapi"
)

// Inspection is transient root-only HIL material. Callers must keep it in
// memory and must never log, persist, or return its identity fields.
type Inspection struct {
	Target      agentapi.SIMAKATarget
	IMSI        string
	IMSIdentity agentapi.SIMIMSIdentityResponse
}

// InspectML307AVOXI fences an operation to the only ready, RF-off ML307A and
// reads SIM/IMS identities through the separate root-only Agent socket.
func InspectML307AVOXI(ctx context.Context) (Inspection, error) {
	readOnly, err := agentapi.NewClient(ReadOnlyAgentSocket)
	if err != nil {
		return Inspection{}, err
	}
	snapshot, err := readOnly.Snapshot(ctx, true)
	if err != nil {
		return Inspection{}, err
	}
	var matches []agentapi.DeviceReport
	for _, device := range snapshot.Devices {
		if device.Profile == agentapi.ProfileML307A {
			matches = append(matches, device)
		}
	}
	if len(matches) != 1 {
		return Inspection{}, errors.New("expected exactly one ML307A")
	}
	device := matches[0]
	probe, err := readOnly.Probe(ctx, agentapi.ProbeRequest{DeviceIDs: []string{device.ID}})
	if err != nil || probe.AgentInstanceID != snapshot.AgentInstanceID ||
		probe.SnapshotGeneration != snapshot.Generation || probe.SnapshotRevision != snapshot.Revision ||
		len(probe.Devices) != 1 {
		return Inspection{}, errors.New("probe fence changed")
	}
	observed := probe.Devices[0]
	if observed.DeviceID != device.ID || observed.State != agentapi.ProbeStateComplete ||
		observed.RF.State != agentapi.RFStateOff || observed.SIM.State != agentapi.SIMStatePresent ||
		observed.SIM.PrimaryLockState != agentapi.PrimaryLockReady ||
		observed.SIM.IdentityFingerprint == "" || observed.ActiveCallCount == nil || *observed.ActiveCallCount != 0 {
		return Inspection{}, errors.New("device is not ready and RF-off")
	}
	target := agentapi.SIMAKATarget{
		AgentInstanceID: snapshot.AgentInstanceID, SnapshotGeneration: snapshot.Generation,
		SnapshotRevision: snapshot.Revision, DeviceID: device.ID, DeviceGeneration: device.Generation,
		IdentityFingerprint: observed.SIM.IdentityFingerprint,
	}

	rootOnly, err := agentapi.NewClient(SIMAKASocket)
	if err != nil {
		return Inspection{}, err
	}
	hello, err := rootOnly.Hello(ctx)
	if err != nil || hello.AgentInstanceID != snapshot.AgentInstanceID ||
		!containsString(hello.Features, agentapi.FeatureSIMAKAHIL) ||
		!containsString(hello.Features, agentapi.FeatureSIMIMSHIL) {
		return Inspection{}, errors.New("SIM AKA socket is not current")
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

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
