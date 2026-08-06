package modemadapter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
)

type staticSnapshotSource struct {
	snapshot agentapi.Snapshot
}

func (source staticSnapshotSource) Snapshot() agentapi.Snapshot { return source.snapshot }

type smsTestAdapter struct {
	profile string
	entered chan string
	release chan struct{}
}

type matchingAdapter struct{ smsTestAdapter }

func (*matchingAdapter) Matches(USBDescriptor) bool { return true }

func (adapter *smsTestAdapter) Profile() string { return adapter.profile }

func (*smsTestAdapter) DisplayName() string { return "SMS test modem" }

func (*smsTestAdapter) Matches(USBDescriptor) bool { return false }

func (*smsTestAdapter) Endpoint(agentapi.DeviceReport, EndpointRole) (agentapi.Endpoint, bool) {
	return agentapi.Endpoint{}, false
}

func (*smsTestAdapter) Capabilities(agentapi.DeviceReport) []agentapi.CapabilityEvidence {
	return nil
}

func (adapter *smsTestAdapter) ListSMS(ctx context.Context, device agentapi.DeviceReport) ([]agentapi.SMSMessageReference, error) {
	if adapter.entered != nil {
		adapter.entered <- device.ID
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-adapter.release:
		}
	}
	return []agentapi.SMSMessageReference{{MessageID: "message-1", DeviceID: device.ID, Sender: "10086"}}, nil
}

func (*smsTestAdapter) ReadSMS(_ context.Context, device agentapi.DeviceReport, messageID string) (agentapi.SMSStoredMessage, error) {
	return agentapi.SMSStoredMessage{MessageID: messageID, DeviceID: device.ID, Sender: "10086", Body: "test"}, nil
}

func (*smsTestAdapter) SendSMS(_ context.Context, device agentapi.DeviceReport, request agentapi.SMSSendRequest) (agentapi.SMSSubmission, error) {
	if device.ID != request.DeviceID {
		return agentapi.SMSSubmission{}, errors.New("router supplied a mismatched device")
	}
	return agentapi.SMSSubmission{OperationID: request.OperationID, MessageID: "submitted-1", SubmittedAt: time.Unix(1, 0).UTC()}, nil
}

func (*smsTestAdapter) AcknowledgeSMS(_ context.Context, device agentapi.DeviceReport, request agentapi.SMSAcknowledgeRequest) (bool, error) {
	return device.ID == request.DeviceID && request.MessageID == "message-1", nil
}

func TestDefaultRegistryMatchesOnlyEvidenceBackedUSBIdentities(t *testing.T) {
	registry := DefaultRegistry()
	tests := []struct {
		name       string
		descriptor USBDescriptor
		profile    string
	}{
		{
			name:       "original QDC507 identity",
			descriptor: USBDescriptor{VendorID: "2CA3", ProductID: "4006"},
			profile:    agentapi.ProfileQDC507,
		},
		{
			name:       "QDC507 Quectel-compatible identity",
			descriptor: USBDescriptor{VendorID: "2c7c", ProductID: "0125", Manufacturer: "BAIWANG"},
			profile:    agentapi.ProfileQDC507,
		},
		{
			name:       "ML307A identity",
			descriptor: USBDescriptor{VendorID: "2ecc", ProductID: "3012", Product: "ML307A"},
			profile:    agentapi.ProfileML307A,
		},
		{
			name:       "generic Quectel device is not QDC507",
			descriptor: USBDescriptor{VendorID: "2c7c", ProductID: "0125", Manufacturer: "Quectel", Product: "EC25"},
		},
		{
			name:       "unidentified 2ecc device is not ML307A",
			descriptor: USBDescriptor{VendorID: "2ecc", ProductID: "3012", Product: "USB modem"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter, matched := registry.Match(test.descriptor)
			if test.profile == "" {
				if matched {
					t.Fatalf("unexpected adapter %q", adapter.Profile())
				}
				return
			}
			if !matched || adapter.Profile() != test.profile {
				t.Fatalf("match = (%#v, %t), want profile %q", adapter, matched, test.profile)
			}
		})
	}
}

func TestRegistryRejectsAmbiguousDescriptorMatches(t *testing.T) {
	registry, err := NewRegistry(
		&matchingAdapter{smsTestAdapter: smsTestAdapter{profile: "first"}},
		&matchingAdapter{smsTestAdapter: smsTestAdapter{profile: "second"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if adapter, matched := registry.Match(USBDescriptor{VendorID: "ffff", ProductID: "ffff"}); matched || adapter != nil {
		t.Fatalf("ambiguous descriptor resolved to (%#v, %t)", adapter, matched)
	}
}

func TestQDC507ResolvesOnlyKnownEndpointRoles(t *testing.T) {
	device := agentapi.DeviceReport{
		Profile: agentapi.ProfileQDC507,
		Interfaces: []agentapi.USBInterface{
			{Number: 2, Endpoints: []agentapi.Endpoint{
				{Kind: agentapi.EndpointQMI, InterfaceNumber: 2, Node: "/dev/wrong-qmi"},
				{Kind: agentapi.EndpointTTY, InterfaceNumber: 2, Node: "/dev/ttyUSB2"},
			}},
			{Number: 3, Endpoints: []agentapi.Endpoint{
				{Kind: agentapi.EndpointTTY, InterfaceNumber: 3, Node: "/dev/ttyUSB3"},
			}},
			{Number: 4, Endpoints: []agentapi.Endpoint{
				{Kind: agentapi.EndpointQMI, InterfaceNumber: 4, Node: "/dev/cdc-wdm0"},
			}},
		},
	}
	adapter := QDC507{}
	primaryAT, ok := adapter.Endpoint(device, EndpointPrimaryAT)
	if !ok || primaryAT.Node != "/dev/ttyUSB2" {
		t.Fatalf("primary AT endpoint = (%#v, %t)", primaryAT, ok)
	}
	qmi, ok := adapter.Endpoint(device, EndpointQMI)
	if !ok || qmi.Node != "/dev/cdc-wdm0" {
		t.Fatalf("QMI endpoint = (%#v, %t)", qmi, ok)
	}
	if endpoint, ok := adapter.Endpoint(device, EndpointRole("raw-device-path")); ok {
		t.Fatalf("unknown endpoint role resolved to %#v", endpoint)
	}
}

func TestML307AUsesOfficialInterfaceTwoMappingAfterHIL(t *testing.T) {
	device := agentapi.DeviceReport{
		Profile: agentapi.ProfileML307A,
		Interfaces: []agentapi.USBInterface{
			{Number: 0, Endpoints: []agentapi.Endpoint{{Kind: agentapi.EndpointTTY, InterfaceNumber: 0, Node: "/dev/ttyUSB0"}}},
			{Number: 1, Endpoints: []agentapi.Endpoint{{Kind: agentapi.EndpointTTY, InterfaceNumber: 1, Node: "/dev/ttyUSB1"}}},
			{Number: 2, Endpoints: []agentapi.Endpoint{{Kind: agentapi.EndpointTTY, InterfaceNumber: 2, Node: "/dev/ttyUSB2"}}},
		},
	}
	adapter := ML307A{}
	endpoint, ok := adapter.Endpoint(device, EndpointPrimaryAT)
	if !ok || endpoint.Node != "/dev/ttyUSB2" {
		t.Fatalf("primary ML307A endpoint = (%#v, %t)", endpoint, ok)
	}
	assertCapability(t, adapter.Capabilities(device), "at-control", agentapi.EvidenceObserved)
	assertCapability(t, adapter.Capabilities(device), "sim-presence", agentapi.EvidenceObserved)
	assertCapability(t, adapter.Capabilities(device), "sim-access", agentapi.EvidenceObserved)
	assertCapability(t, adapter.Capabilities(device), "sim-apdu", agentapi.EvidenceObserved)
	assertCapability(t, adapter.Capabilities(device), "sim-auth", agentapi.EvidenceObserved)
	assertCapability(t, adapter.Capabilities(device), "sms-control", agentapi.EvidenceUnverified)
	assertCapability(t, adapter.Capabilities(device), "digital-voice-media", agentapi.EvidenceUnsupported)
	probePlan, ok := adapter.ATProbePlan()
	if !ok || probePlan.Handshake != "AT" || probePlan.ActiveCalls != "AT+CLCC" {
		t.Fatalf("ML307A AT probe plan = (%#v, %t)", probePlan, ok)
	}
}

func TestQDC507CapabilitiesSeparateObservedTransportFromUnverifiedBusinessFeatures(t *testing.T) {
	device := agentapi.DeviceReport{
		Profile: agentapi.ProfileQDC507,
		Interfaces: []agentapi.USBInterface{
			{Number: 2, Endpoints: []agentapi.Endpoint{{Kind: agentapi.EndpointTTY, InterfaceNumber: 2, Node: "/dev/ttyUSB2"}}},
			{Number: 4, Endpoints: []agentapi.Endpoint{{Kind: agentapi.EndpointQMI, InterfaceNumber: 4, Node: "/dev/cdc-wdm0"}}},
			{Number: 5, Endpoints: []agentapi.Endpoint{{Kind: agentapi.EndpointALSA, InterfaceNumber: 5, Node: "card0"}}},
		},
	}
	capabilities := (QDC507{}).Capabilities(device)
	assertCapability(t, capabilities, "at-control", agentapi.EvidenceObserved)
	assertCapability(t, capabilities, "qmi-control", agentapi.EvidenceObserved)
	assertCapability(t, capabilities, "sim-presence", agentapi.EvidenceObserved)
	assertCapability(t, capabilities, "sms-control", agentapi.EvidenceDocumented)
	assertCapability(t, capabilities, "digital-voice-media", agentapi.EvidenceUnverified)
	probePlan, ok := (QDC507{}).ATProbePlan()
	if !ok || probePlan.Handshake != "AT" || probePlan.ActiveCalls != "AT+CLCC" {
		t.Fatalf("QDC507 AT probe plan = (%#v, %t)", probePlan, ok)
	}
	for index := 1; index < len(capabilities); index++ {
		if capabilities[index-1].Capability >= capabilities[index].Capability {
			t.Fatalf("capabilities are not strictly sorted: %#v", capabilities)
		}
	}
}

func TestSMSBackendRoutesTypedOperationsByCurrentDeviceProfile(t *testing.T) {
	adapter := &smsTestAdapter{profile: "sms-test"}
	registry, err := NewRegistry(adapter, ML307A{})
	if err != nil {
		t.Fatal(err)
	}
	backend, supported := registry.SMSBackend(staticSnapshotSource{snapshot: agentapi.Snapshot{Devices: []agentapi.DeviceReport{
		{ID: "usb-sms", Profile: "sms-test"},
		{ID: "usb-ml307a", Profile: agentapi.ProfileML307A},
	}}})
	if !supported || backend == nil {
		t.Fatal("registered SMS adapter did not produce a backend")
	}

	messages, err := backend.ListSMS(context.Background(), "usb-sms")
	if err != nil || len(messages) != 1 || messages[0].DeviceID != "usb-sms" {
		t.Fatalf("list = %#v, error = %v", messages, err)
	}
	message, err := backend.ReadSMS(context.Background(), "usb-sms", "message-1")
	if err != nil || message.DeviceID != "usb-sms" || message.MessageID != "message-1" {
		t.Fatalf("read = %#v, error = %v", message, err)
	}
	submission, err := backend.SendSMS(context.Background(), agentapi.SMSSendRequest{
		OperationID: "operation-0123456789", DeviceID: "usb-sms", Destination: "10086", Body: "test",
	})
	if err != nil || submission.OperationID != "operation-0123456789" {
		t.Fatalf("send = %#v, error = %v", submission, err)
	}
	acknowledged, err := backend.AcknowledgeSMS(context.Background(), agentapi.SMSAcknowledgeRequest{
		OperationID: "acknowledge-012345", DeviceID: "usb-sms", MessageID: "message-1",
	})
	if err != nil || !acknowledged {
		t.Fatalf("acknowledged = %t, error = %v", acknowledged, err)
	}
	if _, err := backend.ListSMS(context.Background(), "usb-ml307a"); !errors.Is(err, agentapi.ErrSMSUnsupported) {
		t.Fatalf("ML307A SMS error = %v", err)
	}
	if _, err := backend.ListSMS(context.Background(), "usb-missing"); !errors.Is(err, agentapi.ErrSMSDeviceNotFound) {
		t.Fatalf("missing device SMS error = %v", err)
	}
}

func TestDefaultRegistryDoesNotAdvertiseSMSBeforeDriverVerification(t *testing.T) {
	registry := DefaultRegistry()
	backend, supported := registry.SMSBackend(staticSnapshotSource{})
	if supported || backend != nil || registry.SupportsSMS() {
		t.Fatalf("unverified default adapters exposed SMS: backend=%#v supported=%t", backend, supported)
	}
}

func TestSMSBackendSerializesOperationsForOneDevice(t *testing.T) {
	adapter := &smsTestAdapter{
		profile: "blocking-sms", entered: make(chan string, 1), release: make(chan struct{}),
	}
	registry, err := NewRegistry(adapter)
	if err != nil {
		t.Fatal(err)
	}
	backend, supported := registry.SMSBackend(staticSnapshotSource{snapshot: agentapi.Snapshot{Devices: []agentapi.DeviceReport{
		{ID: "usb-one", Profile: "blocking-sms"},
	}}})
	if !supported {
		t.Fatal("blocking SMS adapter did not produce a backend")
	}
	firstDone := make(chan error, 1)
	go func() {
		_, err := backend.ListSMS(context.Background(), "usb-one")
		firstDone <- err
	}()
	if entered := <-adapter.entered; entered != "usb-one" {
		t.Fatalf("first operation entered for %q", entered)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := backend.ListSMS(cancelled, "usb-one"); !errors.Is(err, context.Canceled) {
		t.Fatalf("queued operation error = %v", err)
	}
	close(adapter.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first operation error = %v", err)
	}
}

func assertCapability(t *testing.T, capabilities []agentapi.CapabilityEvidence, name, status string) {
	t.Helper()
	for _, capability := range capabilities {
		if capability.Capability == name {
			if capability.Status != status {
				t.Fatalf("capability %q status = %q, want %q", name, capability.Status, status)
			}
			return
		}
	}
	t.Fatalf("capability %q missing from %#v", name, capabilities)
}
