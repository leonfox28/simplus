package hardwareprobe

import (
	"context"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/modemadapter"
)

type rfStateSetter interface {
	SetRFState(context.Context, string, modemadapter.RFControlAdapter, bool) (agentapi.RFObservation, error)
}

// SetRFState implements the common Agent RF backend. Model selection and the
// fixed command remain inside the adapter registry; callers provide only the
// desired boolean state and never a command or device path.
func (scanner *Scanner) SetRFState(ctx context.Context, snapshot agentapi.Snapshot, deviceID string, enabled bool) (agentapi.RFObservation, bool, error) {
	if scanner == nil {
		return agentapi.RFObservation{}, false, agentapi.ErrRFUnavailable
	}
	scanner.controlMu.Lock()
	defer scanner.controlMu.Unlock()

	var device *agentapi.DeviceReport
	for index := range snapshot.Devices {
		if snapshot.Devices[index].ID == deviceID {
			device = &snapshot.Devices[index]
			break
		}
	}
	if device == nil {
		return agentapi.RFObservation{}, false, agentapi.ErrRFDeviceStale
	}
	base, ok := scanner.adapterRegistry().ForProfile(device.Profile)
	if !ok {
		return agentapi.RFObservation{}, false, agentapi.ErrRFUnsupported
	}
	adapter, ok := base.(modemadapter.RFControlAdapter)
	if !ok {
		return agentapi.RFObservation{}, false, agentapi.ErrRFUnsupported
	}
	endpoint, ok := adapter.Endpoint(*device, modemadapter.EndpointPrimaryAT)
	if !ok || endpoint.Node == "" {
		return agentapi.RFObservation{}, false, agentapi.ErrRFUnavailable
	}
	setter, ok := scanner.Querier.(rfStateSetter)
	if !ok || setter == nil {
		return agentapi.RFObservation{}, false, agentapi.ErrRFUnsupported
	}
	probes, err := scanner.probeLocked(ctx, snapshot, []string{deviceID})
	if err != nil || len(probes) != 1 || probes[0].State != agentapi.ProbeStateComplete || probes[0].ActiveCallCount == nil {
		return agentapi.RFObservation{}, false, agentapi.ErrRFUnavailable
	}
	if *probes[0].ActiveCallCount != 0 {
		return probes[0].RF, false, agentapi.ErrRFActiveCall
	}
	expected := agentapi.RFStateOff
	if enabled {
		expected = agentapi.RFStateOn
	}
	if probes[0].RF.State == expected {
		return probes[0].RF, false, nil
	}
	observed, err := setter.SetRFState(ctx, endpoint.Node, adapter, enabled)
	if err != nil {
		return observed, true, err
	}
	if observed.State != expected {
		return observed, true, agentapi.ErrRFNotConfirmed
	}
	return observed, true, nil
}
