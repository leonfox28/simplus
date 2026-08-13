package modemadapter

import (
	"context"

	"github.com/leonfox28/simplus/internal/agentapi"
)

type SMSRuntimeTarget struct {
	Device          agentapi.DeviceReport
	SubscriptionKey string
}

type SMSRuntimeFence struct {
	DeviceID                        string
	DeviceGeneration                uint64
	ExpectedEquipmentFingerprint    string
	ExpectedSubscriptionFingerprint string
}

// SMSAdapter is implemented only by model adapters with a verified complete
// SMS transport. Temporary endpoint facts are separated from the stable SIM
// namespace used by durable recovery.
type SMSAdapter interface {
	Adapter
	ListSMS(context.Context, SMSRuntimeTarget) ([]agentapi.SMSMessageReference, error)
	ReadSMS(context.Context, SMSRuntimeTarget, string) (agentapi.SMSStoredMessage, error)
	SendSMS(context.Context, SMSRuntimeTarget, agentapi.SMSSendRequest) (agentapi.SMSSubmission, error)
	AcknowledgeSMS(context.Context, SMSRuntimeTarget, agentapi.SMSAcknowledgeRequest) (bool, error)
}

type SnapshotSource interface{ Snapshot() agentapi.Snapshot }

type DeviceOperationGate interface {
	Acquire(context.Context, string) (func(), error)
}

type SMSRuntimeResolver interface {
	ResolveSMSRuntimeTarget(context.Context, agentapi.Snapshot, SMSRuntimeFence, bool) (SMSRuntimeTarget, error)
}

type SMSRuntimeDependencies struct {
	Gate     DeviceOperationGate
	Resolver SMSRuntimeResolver
}

type SMSRouter struct {
	source   SnapshotSource
	registry *Registry
	gate     DeviceOperationGate
	resolver SMSRuntimeResolver
}

var _ agentapi.SMSBackend = (*SMSRouter)(nil)

// SMSBackend is deliberately unavailable unless the caller supplies the same
// gate and fresh target resolver used by the hardware scanner. Discovery-only
// or partially composed registries cannot advertise sms-v1.
func (registry *Registry) SMSBackend(source SnapshotSource, dependencies ...SMSRuntimeDependencies) (agentapi.SMSBackend, bool) {
	if registry == nil || source == nil || !registry.SupportsSMS() || len(dependencies) != 1 ||
		dependencies[0].Gate == nil || dependencies[0].Resolver == nil {
		return nil, false
	}
	return &SMSRouter{source: source, registry: registry, gate: dependencies[0].Gate, resolver: dependencies[0].Resolver}, true
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

func (router *SMSRouter) ListSMS(ctx context.Context, request agentapi.SMSListRequest) ([]agentapi.SMSMessageReference, error) {
	target, adapter, release, err := router.target(ctx, request.DeviceID, request.DeviceGeneration,
		request.ExpectedEquipmentFingerprint, request.ExpectedSubscriptionFingerprint, false)
	if err != nil {
		return nil, err
	}
	defer release()
	return adapter.ListSMS(ctx, target)
}

func (router *SMSRouter) ReadSMS(ctx context.Context, request agentapi.SMSReadRequest) (agentapi.SMSStoredMessage, error) {
	target, adapter, release, err := router.target(ctx, request.DeviceID, request.DeviceGeneration,
		request.ExpectedEquipmentFingerprint, request.ExpectedSubscriptionFingerprint, false)
	if err != nil {
		return agentapi.SMSStoredMessage{}, err
	}
	defer release()
	return adapter.ReadSMS(ctx, target, request.MessageID)
}

func (router *SMSRouter) SendSMS(ctx context.Context, request agentapi.SMSSendRequest) (agentapi.SMSSubmission, error) {
	target, adapter, release, err := router.target(ctx, request.DeviceID, request.DeviceGeneration,
		request.ExpectedEquipmentFingerprint, request.ExpectedSubscriptionFingerprint, true)
	if err != nil {
		return agentapi.SMSSubmission{}, err
	}
	defer release()
	return adapter.SendSMS(ctx, target, request)
}

func (router *SMSRouter) AcknowledgeSMS(ctx context.Context, request agentapi.SMSAcknowledgeRequest) (bool, error) {
	target, adapter, release, err := router.target(ctx, request.DeviceID, request.DeviceGeneration,
		request.ExpectedEquipmentFingerprint, request.ExpectedSubscriptionFingerprint, false)
	if err != nil {
		return false, err
	}
	defer release()
	return adapter.AcknowledgeSMS(ctx, target, request)
}

func (router *SMSRouter) target(ctx context.Context, deviceID string, generation uint64, equipment, subscription string, requireReady bool) (SMSRuntimeTarget, SMSAdapter, func(), error) {
	if router == nil || router.source == nil || router.registry == nil || router.gate == nil || router.resolver == nil {
		return SMSRuntimeTarget{}, nil, nil, agentapi.ErrSMSUnsupported
	}
	snapshot := router.source.Snapshot()
	if _, _, err := router.adapterFor(snapshot, deviceID); err != nil {
		return SMSRuntimeTarget{}, nil, nil, err
	}
	release, err := router.gate.Acquire(ctx, deviceID)
	if err != nil {
		return SMSRuntimeTarget{}, nil, nil, err
	}
	snapshot = router.source.Snapshot()
	fence := SMSRuntimeFence{DeviceID: deviceID, DeviceGeneration: generation,
		ExpectedEquipmentFingerprint: equipment, ExpectedSubscriptionFingerprint: subscription}
	target, err := router.resolver.ResolveSMSRuntimeTarget(ctx, snapshot, fence, requireReady)
	if err != nil {
		release()
		return SMSRuntimeTarget{}, nil, nil, err
	}
	// Refresh may run independently of the per-device endpoint gate. Re-read
	// the current device after the fresh identity/SIM probe so a hotplug or
	// re-enumeration observed during that probe cannot dispatch through the old
	// endpoint report. Unrelated devices do not invalidate this operation.
	current := router.source.Snapshot()
	currentDevice, adapter, err := router.adapterFor(current, target.Device.ID)
	if err != nil {
		release()
		return SMSRuntimeTarget{}, nil, nil, err
	}
	if currentDevice.Generation != generation || currentDevice.Profile != target.Device.Profile {
		release()
		return SMSRuntimeTarget{}, nil, nil, agentapi.ErrSMSDeviceStale
	}
	target.Device = currentDevice
	return target, adapter, release, nil
}

func (router *SMSRouter) adapterFor(snapshot agentapi.Snapshot, deviceID string) (agentapi.DeviceReport, SMSAdapter, error) {
	for _, device := range snapshot.Devices {
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
