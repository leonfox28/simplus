package hardwareprobe

import (
	"context"
	"fmt"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/modemadapter"
)

type simAKAQuerier interface {
	ReadSIMAKAIdentity(context.Context, string, modemadapter.SIMAuthAdapter, string) (string, error)
	AuthenticateSIMAKA(context.Context, string, modemadapter.SIMAuthAdapter, string, agentapi.SIMAKAChallenge) (agentapi.SIMAKAExecution, error)
	ProbeSIMIMSProfile(context.Context, string, modemadapter.SIMAuthAdapter, string) (bool, error)
	ReadSIMIMSIdentity(context.Context, string, modemadapter.SIMAuthAdapter, string) (agentapi.SIMIMSIdentityMaterial, error)
}

func (scanner *Scanner) ReadSIMAKAIdentity(ctx context.Context, snapshot agentapi.Snapshot, deviceID, identityFingerprint string) (string, error) {
	if scanner == nil {
		return "", agentapi.ErrSIMAKAUnavailable
	}
	release, err := scanner.operationGate().Acquire(ctx, deviceID)
	if err != nil {
		return "", agentapi.ErrSIMAKAUnavailable
	}
	defer release()
	snapshot, _, err = scanner.currentSnapshotDevice(snapshot, deviceID)
	if err != nil {
		return "", agentapi.ErrSIMAKADeviceStale
	}
	adapter, endpoint, querier, err := scanner.simAKATarget(snapshot, deviceID)
	if err != nil {
		return "", err
	}
	return querier.ReadSIMAKAIdentity(ctx, endpoint, adapter, identityFingerprint)
}

func (scanner *Scanner) AuthenticateSIMAKA(ctx context.Context, snapshot agentapi.Snapshot, deviceID, identityFingerprint string, challenge agentapi.SIMAKAChallenge) (agentapi.SIMAKAExecution, error) {
	if scanner == nil {
		return agentapi.SIMAKAExecution{}, agentapi.ErrSIMAKAUnavailable
	}
	release, err := scanner.operationGate().Acquire(ctx, deviceID)
	if err != nil {
		return agentapi.SIMAKAExecution{}, agentapi.ErrSIMAKAUnavailable
	}
	defer release()
	snapshot, _, err = scanner.currentSnapshotDevice(snapshot, deviceID)
	if err != nil {
		return agentapi.SIMAKAExecution{}, agentapi.ErrSIMAKADeviceStale
	}
	adapter, endpoint, querier, err := scanner.simAKATarget(snapshot, deviceID)
	if err != nil {
		return agentapi.SIMAKAExecution{}, err
	}
	return querier.AuthenticateSIMAKA(ctx, endpoint, adapter, identityFingerprint, challenge)
}

func (scanner *Scanner) ProbeSIMIMSProfile(ctx context.Context, snapshot agentapi.Snapshot, deviceID, identityFingerprint string) (bool, error) {
	if scanner == nil {
		return false, agentapi.ErrSIMAKAUnavailable
	}
	release, err := scanner.operationGate().Acquire(ctx, deviceID)
	if err != nil {
		return false, agentapi.ErrSIMAKAUnavailable
	}
	defer release()
	snapshot, _, err = scanner.currentSnapshotDevice(snapshot, deviceID)
	if err != nil {
		return false, agentapi.ErrSIMAKADeviceStale
	}
	adapter, endpoint, querier, err := scanner.simAKATarget(snapshot, deviceID)
	if err != nil {
		return false, err
	}
	return querier.ProbeSIMIMSProfile(ctx, endpoint, adapter, identityFingerprint)
}

func (scanner *Scanner) ReadSIMIMSIdentity(ctx context.Context, snapshot agentapi.Snapshot, deviceID, identityFingerprint string) (agentapi.SIMIMSIdentityMaterial, error) {
	if scanner == nil {
		return agentapi.SIMIMSIdentityMaterial{}, agentapi.ErrSIMAKAUnavailable
	}
	release, err := scanner.operationGate().Acquire(ctx, deviceID)
	if err != nil {
		return agentapi.SIMIMSIdentityMaterial{}, agentapi.ErrSIMAKAUnavailable
	}
	defer release()
	snapshot, _, err = scanner.currentSnapshotDevice(snapshot, deviceID)
	if err != nil {
		return agentapi.SIMIMSIdentityMaterial{}, agentapi.ErrSIMAKADeviceStale
	}
	adapter, endpoint, querier, err := scanner.simAKATarget(snapshot, deviceID)
	if err != nil {
		return agentapi.SIMIMSIdentityMaterial{}, err
	}
	return querier.ReadSIMIMSIdentity(ctx, endpoint, adapter, identityFingerprint)
}

func (scanner *Scanner) simAKATarget(snapshot agentapi.Snapshot, deviceID string) (modemadapter.SIMAuthAdapter, string, simAKAQuerier, error) {
	var device *agentapi.DeviceReport
	for index := range snapshot.Devices {
		if snapshot.Devices[index].ID == deviceID {
			device = &snapshot.Devices[index]
			break
		}
	}
	if device == nil {
		return nil, "", nil, fmt.Errorf("%w: device is absent", agentapi.ErrSIMAKADeviceStale)
	}
	adapter, ok := scanner.adapterRegistry().ForProfile(device.Profile)
	if !ok {
		return nil, "", nil, agentapi.ErrSIMAKAUnsupported
	}
	auth, ok := adapter.(modemadapter.SIMAuthAdapter)
	if !ok || !observedCapability(*device, "sim-auth") {
		return nil, "", nil, agentapi.ErrSIMAKAUnsupported
	}
	endpoint, ok := auth.SIMAuthEndpoint(*device)
	if !ok || endpoint.Node == "" {
		return nil, "", nil, agentapi.ErrSIMAKAUnavailable
	}
	querier, ok := scanner.Querier.(simAKAQuerier)
	if !ok || querier == nil {
		return nil, "", nil, agentapi.ErrSIMAKAUnsupported
	}
	return auth, endpoint.Node, querier, nil
}

func observedCapability(device agentapi.DeviceReport, expected string) bool {
	for _, capability := range device.Capabilities {
		if capability.Capability == expected {
			return capability.Status == agentapi.EvidenceObserved
		}
	}
	return false
}
