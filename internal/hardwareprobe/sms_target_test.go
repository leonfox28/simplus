package hardwareprobe

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/modemadapter"
)

type smsTargetQuerier struct {
	probe          agentapi.DeviceProbe
	calls          int
	rfSetCalls     int
	blockingCalls  int
	callStateKnown bool
}

func (querier *smsTargetQuerier) SetRFState(context.Context, string, modemadapter.RFControlAdapter, bool) (agentapi.RFObservation, error) {
	querier.rfSetCalls++
	return agentapi.RFObservation{}, nil
}

func (querier *smsTargetQuerier) Probe(context.Context, string, modemadapter.Adapter) agentapi.DeviceProbe {
	querier.calls++
	return querier.probe
}

func (querier *smsTargetQuerier) ReadSMSBlockingCallCount(context.Context, string, modemadapter.SMSCallSafetyAdapter) (int, bool) {
	return querier.blockingCalls, querier.callStateKnown
}

type readinessSMSAdapter struct{ payloadCalls int }

func (*readinessSMSAdapter) Profile() string                         { return "readiness-sms" }
func (*readinessSMSAdapter) DisplayName() string                     { return "readiness SMS fixture" }
func (*readinessSMSAdapter) Matches(modemadapter.USBDescriptor) bool { return false }
func (*readinessSMSAdapter) Capabilities(agentapi.DeviceReport) []agentapi.CapabilityEvidence {
	return nil
}
func (*readinessSMSAdapter) Endpoint(device agentapi.DeviceReport, role modemadapter.EndpointRole) (agentapi.Endpoint, bool) {
	if role != modemadapter.EndpointPrimaryAT {
		return agentapi.Endpoint{}, false
	}
	for _, usbInterface := range device.Interfaces {
		for _, endpoint := range usbInterface.Endpoints {
			if endpoint.Kind == agentapi.EndpointTTY && endpoint.Node != "" {
				return endpoint, true
			}
		}
	}
	return agentapi.Endpoint{}, false
}
func (adapter *readinessSMSAdapter) ListSMS(context.Context, modemadapter.SMSRuntimeTarget) ([]agentapi.SMSMessageReference, error) {
	adapter.payloadCalls++
	return nil, nil
}
func (adapter *readinessSMSAdapter) ReadSMS(context.Context, modemadapter.SMSRuntimeTarget, string) (agentapi.SMSStoredMessage, error) {
	adapter.payloadCalls++
	return agentapi.SMSStoredMessage{}, nil
}
func (adapter *readinessSMSAdapter) SendSMS(context.Context, modemadapter.SMSRuntimeTarget, agentapi.SMSSendRequest) (agentapi.SMSSubmission, error) {
	adapter.payloadCalls++
	return agentapi.SMSSubmission{MessageID: "fixture", SubmittedAt: time.Unix(1, 0).UTC()}, nil
}
func (adapter *readinessSMSAdapter) AcknowledgeSMS(context.Context, modemadapter.SMSRuntimeTarget, agentapi.SMSAcknowledgeRequest) (bool, error) {
	adapter.payloadCalls++
	return true, nil
}

type smsTargetSnapshotSource struct{ snapshot agentapi.Snapshot }

func (source *smsTargetSnapshotSource) Snapshot() agentapi.Snapshot { return source.snapshot }

func TestSMSBackendReadinessFailuresPerformNoPayloadOrRFWrite(t *testing.T) {
	equipment, subscription := strings.Repeat("a", 64), strings.Repeat("b", 64)
	readyRegistrations := []agentapi.RegistrationObservation{
		{Domain: agentapi.RegistrationDomainCS, State: agentapi.RegistrationRegisteredHome},
		{Domain: agentapi.RegistrationDomainPacket, State: agentapi.RegistrationNotRegistered},
		{Domain: agentapi.RegistrationDomainEPS, State: agentapi.RegistrationNotRegistered},
	}
	baseProbe := agentapi.DeviceProbe{
		State: agentapi.ProbeStateComplete, Identity: agentapi.ModemIdentity{EquipmentIdentityFingerprint: equipment},
		SIM: agentapi.SIMObservation{State: agentapi.SIMStatePresent, PrimaryLockState: agentapi.PrimaryLockReady, IdentityFingerprint: subscription},
		RF:  agentapi.RFObservation{State: agentapi.RFStateOn}, Registrations: readyRegistrations,
	}
	for _, test := range []struct {
		name     string
		mutate   func(*agentapi.DeviceReport, *agentapi.DeviceProbe)
		expected error
	}{
		{name: "missing endpoint", mutate: func(device *agentapi.DeviceReport, _ *agentapi.DeviceProbe) { device.Interfaces = nil }, expected: agentapi.ErrSMSStatusUnavailable},
		{name: "equipment missing", mutate: func(_ *agentapi.DeviceReport, probe *agentapi.DeviceProbe) { probe.Identity = agentapi.ModemIdentity{} }, expected: agentapi.ErrSMSEquipmentIdentity},
		{name: "SIM missing", mutate: func(_ *agentapi.DeviceReport, probe *agentapi.DeviceProbe) { probe.SIM.IdentityFingerprint = "" }, expected: agentapi.ErrSMSSIMNotReady},
		{name: "SIM absent", mutate: func(_ *agentapi.DeviceReport, probe *agentapi.DeviceProbe) { probe.SIM.State = agentapi.SIMStateAbsent }, expected: agentapi.ErrSMSSIMNotReady},
		{name: "SIM locked", mutate: func(_ *agentapi.DeviceReport, probe *agentapi.DeviceProbe) {
			probe.SIM.PrimaryLockState = agentapi.PrimaryLockPIN1Required
		}, expected: agentapi.ErrSMSSIMNotReady},
		{name: "searching", mutate: func(_ *agentapi.DeviceReport, probe *agentapi.DeviceProbe) {
			probe.Registrations[0].State = agentapi.RegistrationSearching
		}, expected: agentapi.ErrSMSNotRegistered},
		{name: "denied", mutate: func(_ *agentapi.DeviceReport, probe *agentapi.DeviceProbe) {
			probe.Registrations[0].State = agentapi.RegistrationDenied
		}, expected: agentapi.ErrSMSRegistrationDenied},
		{name: "not registered", mutate: func(_ *agentapi.DeviceReport, probe *agentapi.DeviceProbe) {
			probe.Registrations[0].State = agentapi.RegistrationNotRegistered
		}, expected: agentapi.ErrSMSNotRegistered},
		{name: "probe failed", mutate: func(_ *agentapi.DeviceReport, probe *agentapi.DeviceProbe) { probe.State = agentapi.ProbeStateFailed }, expected: agentapi.ErrSMSStatusUnavailable},
		{name: "probe unavailable", mutate: func(_ *agentapi.DeviceReport, probe *agentapi.DeviceProbe) {
			probe.State = agentapi.ProbeStateUnavailable
		}, expected: agentapi.ErrSMSStatusUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			adapter := &readinessSMSAdapter{}
			registry, err := modemadapter.NewRegistry(adapter)
			if err != nil {
				t.Fatal(err)
			}
			device := agentapi.DeviceReport{ID: "usb-synthetic", Generation: 7, Profile: adapter.Profile(),
				Interfaces: []agentapi.USBInterface{{Endpoints: []agentapi.Endpoint{{Kind: agentapi.EndpointTTY, Node: "/synthetic-private-endpoint"}}}}}
			probe := baseProbe
			probe.Registrations = append([]agentapi.RegistrationObservation(nil), baseProbe.Registrations...)
			test.mutate(&device, &probe)
			source := &smsTargetSnapshotSource{snapshot: agentapi.Snapshot{Devices: []agentapi.DeviceReport{device}}}
			querier := &smsTargetQuerier{probe: probe}
			scanner := &Scanner{Querier: querier, Adapters: registry, Gate: NewOperationGate(), CurrentSnapshot: source.Snapshot}
			backend, ok := registry.SMSBackend(source, modemadapter.SMSRuntimeDependencies{Gate: scanner.Gate, Resolver: scanner})
			if !ok {
				t.Fatal("fixture backend unavailable")
			}
			_, err = backend.SendSMS(t.Context(), agentapi.SMSSendRequest{
				OperationID: "operation-0123456789", DeviceID: device.ID, DeviceGeneration: device.Generation,
				Destination: "10086", Body: "synthetic", ExpectedEquipmentFingerprint: equipment,
				ExpectedSubscriptionFingerprint: subscription,
			})
			if !errors.Is(err, test.expected) || adapter.payloadCalls != 0 || querier.rfSetCalls != 0 {
				t.Fatalf("error=%v payload calls=%d RF writes=%d", err, adapter.payloadCalls, querier.rfSetCalls)
			}
		})
	}
}

func TestSMSInboundOperationsRequireSIMReadyWithoutRegistration(t *testing.T) {
	equipment, subscription := strings.Repeat("a", 64), strings.Repeat("b", 64)
	for _, operation := range []struct {
		name string
		run  func(agentapi.SMSBackend) error
	}{
		{name: "list", run: func(backend agentapi.SMSBackend) error {
			_, err := backend.ListSMS(t.Context(), agentapi.SMSListRequest{DeviceID: "usb-synthetic", DeviceGeneration: 7, ExpectedEquipmentFingerprint: equipment, ExpectedSubscriptionFingerprint: subscription})
			return err
		}},
		{name: "read", run: func(backend agentapi.SMSBackend) error {
			_, err := backend.ReadSMS(t.Context(), agentapi.SMSReadRequest{DeviceID: "usb-synthetic", DeviceGeneration: 7, MessageID: "message-1", ExpectedEquipmentFingerprint: equipment, ExpectedSubscriptionFingerprint: subscription})
			return err
		}},
		{name: "acknowledge", run: func(backend agentapi.SMSBackend) error {
			_, err := backend.AcknowledgeSMS(t.Context(), agentapi.SMSAcknowledgeRequest{OperationID: "acknowledge-012345", DeviceID: "usb-synthetic", DeviceGeneration: 7, MessageID: "message-1", ExpectedEquipmentFingerprint: equipment, ExpectedSubscriptionFingerprint: subscription})
			return err
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			adapter := &readinessSMSAdapter{}
			registry, _ := modemadapter.NewRegistry(adapter)
			device := agentapi.DeviceReport{ID: "usb-synthetic", Generation: 7, Profile: adapter.Profile(), Interfaces: []agentapi.USBInterface{{Endpoints: []agentapi.Endpoint{{Kind: agentapi.EndpointTTY, Node: "/synthetic-private-endpoint"}}}}}
			source := &smsTargetSnapshotSource{snapshot: agentapi.Snapshot{Devices: []agentapi.DeviceReport{device}}}
			querier := &smsTargetQuerier{probe: agentapi.DeviceProbe{State: agentapi.ProbeStateComplete,
				Identity: agentapi.ModemIdentity{EquipmentIdentityFingerprint: equipment},
				SIM:      agentapi.SIMObservation{State: agentapi.SIMStateLocked, PrimaryLockState: agentapi.PrimaryLockPIN1Required, IdentityFingerprint: subscription},
				RF:       agentapi.RFObservation{State: agentapi.RFStateOff}}}
			scanner := &Scanner{Querier: querier, Adapters: registry, Gate: NewOperationGate(), CurrentSnapshot: source.Snapshot}
			backend, _ := registry.SMSBackend(source, modemadapter.SMSRuntimeDependencies{Gate: scanner.Gate, Resolver: scanner})
			if err := operation.run(backend); !errors.Is(err, agentapi.ErrSMSSIMNotReady) || adapter.payloadCalls != 0 || querier.rfSetCalls != 0 {
				t.Fatalf("error=%v payload calls=%d RF writes=%d", err, adapter.payloadCalls, querier.rfSetCalls)
			}
		})
	}
}

func TestResolveSMSRuntimeTargetFencesGenerationEquipmentSIMAndReadiness(t *testing.T) {
	equipment, subscription := strings.Repeat("a", 64), strings.Repeat("b", 64)
	probe := agentapi.DeviceProbe{State: agentapi.ProbeStateComplete,
		Identity: agentapi.ModemIdentity{EquipmentIdentityFingerprint: equipment},
		SIM:      agentapi.SIMObservation{State: agentapi.SIMStatePresent, PrimaryLockState: agentapi.PrimaryLockReady, IdentityFingerprint: subscription},
		RF:       agentapi.RFObservation{State: agentapi.RFStateOn}, Registrations: []agentapi.RegistrationObservation{
			{Domain: agentapi.RegistrationDomainCS, State: agentapi.RegistrationRegisteredHome},
			{Domain: agentapi.RegistrationDomainPacket, State: agentapi.RegistrationNotRegistered},
			{Domain: agentapi.RegistrationDomainEPS, State: agentapi.RegistrationNotRegistered},
		}}
	querier := &smsTargetQuerier{probe: probe}
	scanner := &Scanner{Querier: querier, Adapters: modemadapter.DefaultRegistry()}
	device := agentapi.DeviceReport{ID: "usb-synthetic", Generation: 7, Profile: agentapi.ProfileQDC507,
		Interfaces: []agentapi.USBInterface{{Number: 2, Endpoints: []agentapi.Endpoint{{Kind: agentapi.EndpointTTY, InterfaceNumber: 2, Node: "/synthetic-private-endpoint"}}}}}
	snapshot := agentapi.Snapshot{Devices: []agentapi.DeviceReport{device}}
	fence := modemadapter.SMSRuntimeFence{DeviceID: device.ID, DeviceGeneration: 7,
		ExpectedEquipmentFingerprint: equipment, ExpectedSubscriptionFingerprint: subscription}
	target, err := scanner.ResolveSMSRuntimeTarget(t.Context(), snapshot, fence, true)
	if err != nil || target.SubscriptionKey != subscription || target.Device.ID != device.ID || querier.calls != 1 {
		t.Fatalf("target=%#v calls=%d error=%v", target, querier.calls, err)
	}
	stale := fence
	stale.DeviceGeneration++
	if _, err := scanner.ResolveSMSRuntimeTarget(t.Context(), snapshot, stale, true); !errors.Is(err, agentapi.ErrSMSDeviceStale) {
		t.Fatalf("stale error=%v", err)
	}
	wrongEquipment := fence
	wrongEquipment.ExpectedEquipmentFingerprint = strings.Repeat("c", 64)
	if _, err := scanner.ResolveSMSRuntimeTarget(t.Context(), snapshot, wrongEquipment, true); !errors.Is(err, agentapi.ErrSMSEquipmentIdentity) {
		t.Fatalf("equipment error=%v", err)
	}
	wrongSIM := fence
	wrongSIM.ExpectedSubscriptionFingerprint = strings.Repeat("c", 64)
	if _, err := scanner.ResolveSMSRuntimeTarget(t.Context(), snapshot, wrongSIM, true); !errors.Is(err, agentapi.ErrSMSSIMIdentity) {
		t.Fatalf("SIM error=%v", err)
	}
	querier.probe.RF.State = agentapi.RFStateOff
	if _, err := scanner.ResolveSMSRuntimeTarget(t.Context(), snapshot, fence, true); !errors.Is(err, agentapi.ErrSMSRFOff) {
		t.Fatalf("RF error=%v", err)
	}
	if target, err := scanner.ResolveSMSRuntimeTarget(t.Context(), snapshot, fence, false); err != nil || target.SubscriptionKey != subscription {
		t.Fatalf("inbound target=%#v error=%v", target, err)
	}
}

func TestResolveSMSRuntimeTargetAllowsDataModeButRejectsBlockingCalls(t *testing.T) {
	equipment, subscription := strings.Repeat("a", 64), strings.Repeat("b", 64)
	active := 2
	probe := agentapi.DeviceProbe{
		State: agentapi.ProbeStateComplete, Identity: agentapi.ModemIdentity{EquipmentIdentityFingerprint: equipment},
		SIM: agentapi.SIMObservation{State: agentapi.SIMStatePresent, PrimaryLockState: agentapi.PrimaryLockReady, IdentityFingerprint: subscription},
		RF:  agentapi.RFObservation{State: agentapi.RFStateOn}, ActiveCallCount: &active,
		Registrations: []agentapi.RegistrationObservation{
			{Domain: agentapi.RegistrationDomainCS, State: agentapi.RegistrationRegisteredHome},
			{Domain: agentapi.RegistrationDomainPacket, State: agentapi.RegistrationNotRegistered},
			{Domain: agentapi.RegistrationDomainEPS, State: agentapi.RegistrationNotRegistered},
		},
	}
	device := agentapi.DeviceReport{ID: "usb-synthetic", Generation: 7, Profile: agentapi.ProfileQDC507,
		Interfaces: []agentapi.USBInterface{{Number: 2, Endpoints: []agentapi.Endpoint{{Kind: agentapi.EndpointTTY, Node: "/synthetic-private-endpoint"}}}}}
	snapshot := agentapi.Snapshot{Devices: []agentapi.DeviceReport{device}}
	fence := modemadapter.SMSRuntimeFence{DeviceID: device.ID, DeviceGeneration: device.Generation,
		ExpectedEquipmentFingerprint: equipment, ExpectedSubscriptionFingerprint: subscription}
	querier := &smsTargetQuerier{probe: probe, callStateKnown: true}
	scanner := &Scanner{Querier: querier, Adapters: modemadapter.DefaultRegistry()}
	if _, err := scanner.ResolveSMSRuntimeTarget(t.Context(), snapshot, fence, true); err != nil {
		t.Fatalf("data-mode sessions blocked SMS: %v", err)
	}
	querier.blockingCalls = 1
	if _, err := scanner.ResolveSMSRuntimeTarget(t.Context(), snapshot, fence, true); !errors.Is(err, agentapi.ErrRFActiveCall) {
		t.Fatalf("blocking call error=%v", err)
	}
	querier.blockingCalls = 0
	querier.callStateKnown = false
	if _, err := scanner.ResolveSMSRuntimeTarget(t.Context(), snapshot, fence, true); !errors.Is(err, agentapi.ErrSMSStatusUnavailable) {
		t.Fatalf("unknown call state error=%v", err)
	}
	if _, err := scanner.ResolveSMSRuntimeTarget(t.Context(), snapshot, fence, false); err != nil {
		t.Fatalf("receive-only operation was coupled to call state: %v", err)
	}
}
