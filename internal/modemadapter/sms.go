package modemadapter

import (
	"context"
	"sync"

	"github.com/leonfox28/simplus/internal/agentapi"
)

// SMSAdapter is implemented only by model adapters with a verified SMS
// transport. The router supplies the current device report so implementations
// can resolve fixed endpoint roles without accepting a path from the caller.
type SMSAdapter interface {
	Adapter
	ListSMS(context.Context, agentapi.DeviceReport) ([]agentapi.SMSMessageReference, error)
	ReadSMS(context.Context, agentapi.DeviceReport, string) (agentapi.SMSStoredMessage, error)
	SendSMS(context.Context, agentapi.DeviceReport, agentapi.SMSSendRequest) (agentapi.SMSSubmission, error)
	AcknowledgeSMS(context.Context, agentapi.DeviceReport, agentapi.SMSAcknowledgeRequest) (bool, error)
}

type SnapshotSource interface {
	Snapshot() agentapi.Snapshot
}

type SMSRouter struct {
	source   SnapshotSource
	registry *Registry

	gatesMu sync.Mutex
	gates   map[string]chan struct{}
}

var _ agentapi.SMSBackend = (*SMSRouter)(nil)

// SMSBackend returns a common Agent backend only when at least one registered
// model adapter implements SMSAdapter. An empty discovery-only registry must
// not make the Agent advertise sms-v1.
func (registry *Registry) SMSBackend(source SnapshotSource) (agentapi.SMSBackend, bool) {
	if registry == nil || source == nil || !registry.SupportsSMS() {
		return nil, false
	}
	return &SMSRouter{source: source, registry: registry, gates: make(map[string]chan struct{})}, true
}

func (registry *Registry) SupportsSMS() bool {
	if registry == nil {
		return false
	}
	for _, adapter := range registry.ordered {
		if _, ok := adapter.(SMSAdapter); ok {
			return true
		}
	}
	return false
}

func (router *SMSRouter) ListSMS(ctx context.Context, deviceID string) ([]agentapi.SMSMessageReference, error) {
	release, err := router.acquire(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	defer release()
	device, adapter, err := router.resolve(deviceID)
	if err != nil {
		return nil, err
	}
	return adapter.ListSMS(ctx, device)
}

func (router *SMSRouter) ReadSMS(ctx context.Context, deviceID, messageID string) (agentapi.SMSStoredMessage, error) {
	release, err := router.acquire(ctx, deviceID)
	if err != nil {
		return agentapi.SMSStoredMessage{}, err
	}
	defer release()
	device, adapter, err := router.resolve(deviceID)
	if err != nil {
		return agentapi.SMSStoredMessage{}, err
	}
	return adapter.ReadSMS(ctx, device, messageID)
}

func (router *SMSRouter) SendSMS(ctx context.Context, request agentapi.SMSSendRequest) (agentapi.SMSSubmission, error) {
	release, err := router.acquire(ctx, request.DeviceID)
	if err != nil {
		return agentapi.SMSSubmission{}, err
	}
	defer release()
	device, adapter, err := router.resolve(request.DeviceID)
	if err != nil {
		return agentapi.SMSSubmission{}, err
	}
	return adapter.SendSMS(ctx, device, request)
}

func (router *SMSRouter) AcknowledgeSMS(ctx context.Context, request agentapi.SMSAcknowledgeRequest) (bool, error) {
	release, err := router.acquire(ctx, request.DeviceID)
	if err != nil {
		return false, err
	}
	defer release()
	device, adapter, err := router.resolve(request.DeviceID)
	if err != nil {
		return false, err
	}
	return adapter.AcknowledgeSMS(ctx, device, request)
}

func (router *SMSRouter) resolve(deviceID string) (agentapi.DeviceReport, SMSAdapter, error) {
	if router == nil || router.source == nil || router.registry == nil {
		return agentapi.DeviceReport{}, nil, agentapi.ErrSMSUnsupported
	}
	for _, device := range router.source.Snapshot().Devices {
		if device.ID != deviceID {
			continue
		}
		adapter, ok := router.registry.ForProfile(device.Profile)
		if !ok {
			return agentapi.DeviceReport{}, nil, agentapi.ErrSMSUnsupported
		}
		smsAdapter, ok := adapter.(SMSAdapter)
		if !ok {
			return agentapi.DeviceReport{}, nil, agentapi.ErrSMSUnsupported
		}
		return device, smsAdapter, nil
	}
	return agentapi.DeviceReport{}, nil, agentapi.ErrSMSDeviceNotFound
}

func (router *SMSRouter) acquire(ctx context.Context, deviceID string) (func(), error) {
	if router == nil {
		return nil, agentapi.ErrSMSUnsupported
	}
	// Reject missing or unsupported IDs before allocating a persistent gate.
	// Resolve again after acquiring because hotplug may change the snapshot while
	// an operation is queued.
	if _, _, err := router.resolve(deviceID); err != nil {
		return nil, err
	}
	router.gatesMu.Lock()
	gate := router.gates[deviceID]
	if gate == nil {
		gate = make(chan struct{}, 1)
		gate <- struct{}{}
		router.gates[deviceID] = gate
	}
	router.gatesMu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-gate:
		return func() { gate <- struct{}{} }, nil
	}
}
