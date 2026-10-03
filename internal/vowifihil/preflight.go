package vowifihil

import (
	"context"
	"errors"

	"github.com/leonfox28/simplus/internal/ims"

	"github.com/leonfox28/simplus/internal/agentapi"
)

func InspectML307AVOXI(ctx context.Context) (ims.Inspection, error) {
	readOnly, err := agentapi.NewClient(ims.ReadOnlyAgentSocket)
	if err != nil {
		return ims.Inspection{}, err
	}
	snapshot, err := readOnly.Snapshot(ctx, true)
	if err != nil {
		return ims.Inspection{}, err
	}
	deviceIDs := []string{}
	for _, device := range snapshot.Devices {
		if device.Profile == agentapi.ProfileML307A && observedCapability(device, "sim-auth") && observedCapability(device, "host-vowifi-auth") {
			deviceIDs = append(deviceIDs, device.ID)
		}
	}
	if len(deviceIDs) != 1 {
		return ims.Inspection{}, errors.New("expected exactly one ML307A HIL target")
	}
	probe, err := readOnly.Probe(ctx, agentapi.ProbeRequest{DeviceIDs: deviceIDs})
	if err != nil || probe.AgentInstanceID != snapshot.AgentInstanceID ||
		probe.SnapshotGeneration != snapshot.Generation || probe.SnapshotRevision != snapshot.Revision ||
		len(probe.Devices) != 1 {
		return ims.Inspection{}, errors.New("probe fence changed")
	}
	observed := probe.Devices[0]
	if observed.DeviceID != deviceIDs[0] || observed.State != agentapi.ProbeStateComplete ||
		observed.RF.State != agentapi.RFStateOff || observed.SIM.State != agentapi.SIMStatePresent ||
		observed.SIM.PrimaryLockState != agentapi.PrimaryLockReady || len(observed.SIM.IdentityFingerprint) != 64 ||
		observed.ActiveCallCount == nil || *observed.ActiveCallCount != 0 {
		return ims.Inspection{}, errors.New("ML307A HIL target is not ready and RF-off")
	}
	return ims.InspectHostVoWiFiLine(ctx, "agent-line-"+observed.SIM.IdentityFingerprint[:32])
}

func observedCapability(device agentapi.DeviceReport, expected string) bool {
	for _, capability := range device.Capabilities {
		if capability.Capability == expected {
			return capability.Status == agentapi.EvidenceObserved
		}
	}
	return false
}
