package hardwareprobe

import (
	"context"
	"crypto/subtle"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/modemadapter"
)

// ResolveSMSRuntimeTarget runs while the caller holds Scanner.Gate for the
// device. It therefore uses probeLocked and never recursively acquires.
func (scanner *Scanner) ResolveSMSRuntimeTarget(ctx context.Context, snapshot agentapi.Snapshot, fence modemadapter.SMSRuntimeFence, requireReady bool) (modemadapter.SMSRuntimeTarget, error) {
	current, device, err := scanner.currentSnapshotDevice(snapshot, fence.DeviceID)
	if err != nil {
		return modemadapter.SMSRuntimeTarget{}, agentapi.ErrSMSDeviceNotFound
	}
	if device.Generation != fence.DeviceGeneration {
		return modemadapter.SMSRuntimeTarget{}, agentapi.ErrSMSDeviceStale
	}
	probes, err := scanner.probeLocked(ctx, current, []string{device.ID})
	if err != nil || len(probes) != 1 || probes[0].State != agentapi.ProbeStateComplete {
		return modemadapter.SMSRuntimeTarget{}, agentapi.ErrSMSStatusUnavailable
	}
	probe := probes[0]
	if !fingerprintPattern.MatchString(probe.Identity.EquipmentIdentityFingerprint) {
		return modemadapter.SMSRuntimeTarget{}, agentapi.ErrSMSEquipmentIdentity
	}
	if subtle.ConstantTimeCompare([]byte(probe.Identity.EquipmentIdentityFingerprint), []byte(fence.ExpectedEquipmentFingerprint)) != 1 {
		return modemadapter.SMSRuntimeTarget{}, agentapi.ErrSMSEquipmentIdentity
	}
	if probe.SIM.State != agentapi.SIMStatePresent || probe.SIM.PrimaryLockState != agentapi.PrimaryLockReady ||
		!fingerprintPattern.MatchString(probe.SIM.IdentityFingerprint) {
		return modemadapter.SMSRuntimeTarget{}, agentapi.ErrSMSSIMNotReady
	}
	if subtle.ConstantTimeCompare([]byte(probe.SIM.IdentityFingerprint), []byte(fence.ExpectedSubscriptionFingerprint)) != 1 {
		return modemadapter.SMSRuntimeTarget{}, agentapi.ErrSMSSIMIdentity
	}
	if requireReady {
		classification := agentapi.ClassifyCellular(probe)
		if !classification.ReadyForSMS {
			return modemadapter.SMSRuntimeTarget{}, smsReadinessError(probe)
		}
	}
	if requireReady && probe.ActiveCallCount != nil && *probe.ActiveCallCount != 0 {
		if err := scanner.requireSMSCallSafety(ctx, device, probe); err != nil {
			return modemadapter.SMSRuntimeTarget{}, err
		}
	}
	return modemadapter.SMSRuntimeTarget{Device: device, SubscriptionKey: fence.ExpectedSubscriptionFingerprint}, nil
}

func (scanner *Scanner) requireSMSCallSafety(ctx context.Context, device agentapi.DeviceReport, probe agentapi.DeviceProbe) error {
	if probe.ActiveCallCount == nil {
		return agentapi.ErrSMSStatusUnavailable
	}
	if *probe.ActiveCallCount == 0 {
		return nil
	}
	adapter, ok := scanner.adapterRegistry().ForProfile(device.Profile)
	if !ok {
		return agentapi.ErrRFActiveCall
	}
	callAdapter, ok := adapter.(modemadapter.SMSCallSafetyAdapter)
	if !ok {
		return agentapi.ErrRFActiveCall
	}
	endpoint, ok := adapter.Endpoint(device, modemadapter.EndpointPrimaryAT)
	if !ok || endpoint.Node == "" {
		return agentapi.ErrSMSStatusUnavailable
	}
	blocking, known := scanner.readSMSBlockingCallCount(ctx, endpoint.Node, callAdapter)
	if !known {
		return agentapi.ErrSMSStatusUnavailable
	}
	if blocking != 0 {
		return agentapi.ErrRFActiveCall
	}
	return nil
}

func smsReadinessError(probe agentapi.DeviceProbe) error {
	classification := agentapi.ClassifyCellular(probe)
	switch classification.ReasonCode {
	case agentapi.ErrorCellularSIMNotReady:
		return agentapi.ErrSMSSIMNotReady
	case agentapi.ErrorCellularRFOff:
		return agentapi.ErrSMSRFOff
	case agentapi.ErrorCellularRegistrationDenied:
		return agentapi.ErrSMSRegistrationDenied
	case agentapi.ErrorCellularNotRegistered:
		return agentapi.ErrSMSNotRegistered
	default:
		return agentapi.ErrSMSStatusUnavailable
	}
}
