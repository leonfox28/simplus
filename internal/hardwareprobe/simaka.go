package hardwareprobe

import (
	"context"
	"fmt"

	"github.com/leonfox28/simplus/internal/agentapi"
)

type simAKAQuerier interface {
	ReadSIMAKAIdentity(context.Context, string, string, string) (string, error)
	AuthenticateSIMAKA(context.Context, string, string, string, agentapi.SIMAKAChallenge) (agentapi.SIMAKAExecution, error)
}

type simIMSQuerier interface {
	ProbeSIMIMSProfile(context.Context, string, string, string) (bool, error)
	ReadSIMIMSIdentity(context.Context, string, string, string) (agentapi.SIMIMSIdentityMaterial, error)
}

func (scanner *Scanner) ReadSIMAKAIdentity(ctx context.Context, snapshot agentapi.Snapshot, deviceID, identityFingerprint string) (string, error) {
	if scanner == nil {
		return "", agentapi.ErrSIMAKAUnavailable
	}
	scanner.controlMu.Lock()
	defer scanner.controlMu.Unlock()
	device, endpoint, querier, err := scanner.simAKATarget(snapshot, deviceID)
	if err != nil {
		return "", err
	}
	return querier.ReadSIMAKAIdentity(ctx, endpoint, device.Profile, identityFingerprint)
}

func (scanner *Scanner) AuthenticateSIMAKA(ctx context.Context, snapshot agentapi.Snapshot, deviceID, identityFingerprint string, challenge agentapi.SIMAKAChallenge) (agentapi.SIMAKAExecution, error) {
	if scanner == nil {
		return agentapi.SIMAKAExecution{}, agentapi.ErrSIMAKAUnavailable
	}
	scanner.controlMu.Lock()
	defer scanner.controlMu.Unlock()
	device, endpoint, querier, err := scanner.simAKATarget(snapshot, deviceID)
	if err != nil {
		return agentapi.SIMAKAExecution{}, err
	}
	return querier.AuthenticateSIMAKA(ctx, endpoint, device.Profile, identityFingerprint, challenge)
}

func (scanner *Scanner) ProbeSIMIMSProfile(ctx context.Context, snapshot agentapi.Snapshot, deviceID, identityFingerprint string) (bool, error) {
	if scanner == nil {
		return false, agentapi.ErrSIMAKAUnavailable
	}
	scanner.controlMu.Lock()
	defer scanner.controlMu.Unlock()
	device, endpoint, _, err := scanner.simAKATarget(snapshot, deviceID)
	if err != nil {
		return false, err
	}
	querier, ok := scanner.Querier.(simIMSQuerier)
	if !ok || querier == nil {
		return false, agentapi.ErrSIMAKAUnsupported
	}
	return querier.ProbeSIMIMSProfile(ctx, endpoint, device.Profile, identityFingerprint)
}

func (scanner *Scanner) ReadSIMIMSIdentity(ctx context.Context, snapshot agentapi.Snapshot, deviceID, identityFingerprint string) (agentapi.SIMIMSIdentityMaterial, error) {
	if scanner == nil {
		return agentapi.SIMIMSIdentityMaterial{}, agentapi.ErrSIMAKAUnavailable
	}
	scanner.controlMu.Lock()
	defer scanner.controlMu.Unlock()
	device, endpoint, _, err := scanner.simAKATarget(snapshot, deviceID)
	if err != nil {
		return agentapi.SIMIMSIdentityMaterial{}, err
	}
	querier, ok := scanner.Querier.(simIMSQuerier)
	if !ok || querier == nil {
		return agentapi.SIMIMSIdentityMaterial{}, agentapi.ErrSIMAKAUnsupported
	}
	return querier.ReadSIMIMSIdentity(ctx, endpoint, device.Profile, identityFingerprint)
}

func (scanner *Scanner) simAKATarget(snapshot agentapi.Snapshot, deviceID string) (*agentapi.DeviceReport, string, simAKAQuerier, error) {
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
	if device.Profile != agentapi.ProfileML307A {
		return nil, "", nil, agentapi.ErrSIMAKAUnsupported
	}
	endpoint := scanner.preferredATEndpoint(*device)
	if endpoint == "" {
		return nil, "", nil, agentapi.ErrSIMAKAUnavailable
	}
	querier, ok := scanner.Querier.(simAKAQuerier)
	if !ok || querier == nil {
		return nil, "", nil, agentapi.ErrSIMAKAUnsupported
	}
	return device, endpoint, querier, nil
}
