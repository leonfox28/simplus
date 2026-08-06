package hardwareprobe

import (
	"context"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/modemadapter"
)

type equipmentIdentityReader interface {
	ReadEquipmentIdentity(context.Context, string, modemadapter.EquipmentIdentityAdapter) (agentapi.EquipmentIdentityObservation, error)
}

// ReadEquipmentIdentity implements the dedicated Agent identity backend. The
// caller selects only a fenced device ID; endpoint and command selection stay
// inside the scanner and model adapter.
func (scanner *Scanner) ReadEquipmentIdentity(ctx context.Context, snapshot agentapi.Snapshot, deviceID string) (agentapi.EquipmentIdentityObservation, error) {
	if scanner == nil {
		return agentapi.EquipmentIdentityObservation{}, agentapi.ErrEquipmentIdentityUnavailable
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
		return agentapi.EquipmentIdentityObservation{}, agentapi.ErrEquipmentIdentityDeviceStale
	}
	base, ok := scanner.adapterRegistry().ForProfile(device.Profile)
	if !ok {
		return agentapi.EquipmentIdentityObservation{}, agentapi.ErrEquipmentIdentityUnsupported
	}
	adapter, ok := base.(modemadapter.EquipmentIdentityAdapter)
	if !ok {
		return agentapi.EquipmentIdentityObservation{}, agentapi.ErrEquipmentIdentityUnsupported
	}
	endpoint, ok := adapter.Endpoint(*device, modemadapter.EndpointPrimaryAT)
	if !ok || endpoint.Node == "" {
		return agentapi.EquipmentIdentityObservation{}, agentapi.ErrEquipmentIdentityUnavailable
	}
	reader, ok := scanner.Querier.(equipmentIdentityReader)
	if !ok || reader == nil {
		return agentapi.EquipmentIdentityObservation{}, agentapi.ErrEquipmentIdentityUnsupported
	}
	return reader.ReadEquipmentIdentity(ctx, endpoint.Node, adapter)
}
