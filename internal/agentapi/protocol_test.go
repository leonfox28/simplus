package agentapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func validCompleteProbeFixture(deviceID string, sim SIMObservation) DeviceProbe {
	count := 0
	return DeviceProbe{
		DeviceID:        deviceID,
		State:           ProbeStateComplete,
		RF:              RFObservation{State: RFStateOff},
		SIM:             sim,
		SignalMetrics:   SignalObservation{State: SignalStateUnknown},
		Registrations:   []RegistrationObservation{},
		CurrentNetwork:  NetworkObservation{SelectionMode: NetworkSelectionUnknown},
		ActiveCallCount: &count,
	}
}

func validProbeResponseFixture() ProbeResponse {
	return ProbeResponse{
		ProtocolVersion:    ProtocolVersion,
		AgentInstanceID:    "01234567-89ab-cdef-0123-456789abcdef",
		SnapshotGeneration: 4,
		SnapshotRevision:   strings.Repeat("a", 64),
		ObservedAt:         time.Date(2026, 8, 3, 1, 2, 3, 0, time.UTC),
		Devices: []DeviceProbe{validCompleteProbeFixture(
			"usb-1-1",
			SIMObservation{State: SIMStateAbsent, PrimaryLockState: PrimaryLockUnknown},
		)},
	}
}

func TestValidateProbeResponseRejectsInvalidTypedContract(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ProbeResponse)
	}{
		{name: "missing devices array", mutate: func(response *ProbeResponse) { response.Devices = nil }},
		{name: "invalid probe state", mutate: func(response *ProbeResponse) { response.Devices[0].State = "partial" }},
		{name: "invalid RF state", mutate: func(response *ProbeResponse) { response.Devices[0].RF.State = "airplane" }},
		{name: "raw equipment identity", mutate: func(response *ProbeResponse) {
			response.Devices[0].Identity.EquipmentIdentityFingerprint = "490154203237518"
		}},
		{name: "invalid model text", mutate: func(response *ProbeResponse) { response.Devices[0].Identity.Model = "ML307A\n" }},
		{name: "invalid module serial", mutate: func(response *ProbeResponse) { response.Devices[0].Identity.SerialNumber = strings.Repeat("S", 129) }},
		{name: "missing primary lock state", mutate: func(response *ProbeResponse) { response.Devices[0].SIM.PrimaryLockState = "" }},
		{name: "invalid signal state", mutate: func(response *ProbeResponse) { response.Devices[0].SignalMetrics.State = "weak" }},
		{name: "missing registrations array", mutate: func(response *ProbeResponse) { response.Devices[0].Registrations = nil }},
		{name: "invalid registration state", mutate: func(response *ProbeResponse) {
			response.Devices[0].Registrations = []RegistrationObservation{{Domain: RegistrationDomainCS, State: "attached", Source: "at-creg"}}
		}},
		{name: "invalid selection mode", mutate: func(response *ProbeResponse) { response.Devices[0].CurrentNetwork.SelectionMode = "preferred" }},
		{name: "invalid RAT", mutate: func(response *ProbeResponse) { response.Devices[0].CurrentNetwork.RAT = "6g" }},
		{name: "complete without call count", mutate: func(response *ProbeResponse) { response.Devices[0].ActiveCallCount = nil }},
		{name: "identity hint without fingerprint", mutate: func(response *ProbeResponse) { response.Devices[0].SIM.DisplayIdentityHint = "ICCID •••• 2115" }},
		{name: "operator without fingerprint", mutate: func(response *ProbeResponse) { response.Devices[0].SIM.HomeOperatorCode = "234-15" }},
		{name: "number without fingerprint", mutate: func(response *ProbeResponse) { response.Devices[0].SIM.SubscriberNumber = "+12025550123" }},
		{name: "invalid operator code", mutate: func(response *ProbeResponse) {
			response.Devices[0].SIM = SIMObservation{State: SIMStatePresent, PrimaryLockState: PrimaryLockReady,
				IdentityFingerprint: strings.Repeat("b", 64), DisplayIdentityHint: "ICCID •••• 2115", HomeOperatorCode: "23415"}
		}},
		{name: "invalid operator name", mutate: func(response *ProbeResponse) {
			response.Devices[0].SIM = SIMObservation{State: SIMStatePresent, PrimaryLockState: PrimaryLockReady,
				IdentityFingerprint: strings.Repeat("b", 64), DisplayIdentityHint: "ICCID •••• 2115", HomeOperatorName: "VOXI\n"}
		}},
		{name: "invalid subscriber number", mutate: func(response *ProbeResponse) {
			response.Devices[0].SIM = SIMObservation{State: SIMStatePresent, PrimaryLockState: PrimaryLockReady,
				IdentityFingerprint: strings.Repeat("b", 64), DisplayIdentityHint: "ICCID •••• 2115", SubscriberNumber: "12025550123"}
		}},
		{name: "identity on absent SIM", mutate: func(response *ProbeResponse) {
			response.Devices[0].SIM.IdentityFingerprint = strings.Repeat("b", 64)
			response.Devices[0].SIM.DisplayIdentityHint = "ICCID •••• 2115"
		}},
		{name: "failed without typed error", mutate: func(response *ProbeResponse) { response.Devices[0].State = ProbeStateFailed }},
		{name: "unknown error code", mutate: func(response *ProbeResponse) {
			response.Devices[0].State = ProbeStateFailed
			response.Devices[0].Error = &ProbeError{Layer: ErrorLayerTransport, Code: "UNKNOWN_FAILURE", Retryable: true}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := validProbeResponseFixture()
			test.mutate(&response)
			if err := validateProbeResponse(response); err == nil {
				t.Fatalf("invalid response accepted: %#v", response)
			}
		})
	}
}

func TestValidateProbeResponseAcceptsOnlyMaskedReadySIMIdentity(t *testing.T) {
	response := validProbeResponseFixture()
	response.Devices[0].Identity.SerialNumber = "SYNTHETIC-MODULE-0001"
	response.Devices[0].SIM = SIMObservation{
		State: SIMStatePresent, PrimaryLockState: PrimaryLockReady,
		IdentityFingerprint: strings.Repeat("b", 64), DisplayIdentityHint: "ICCID •••• 2115",
		HomeOperatorName: "VOXI", HomeOperatorCode: "234-15", SubscriberNumber: "+12025550123",
	}
	if err := validateProbeResponse(response); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ProbeResponse
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Devices[0].Identity.SerialNumber != response.Devices[0].Identity.SerialNumber {
		t.Fatalf("module serial did not survive the Agent protocol: %#v", decoded.Devices[0].Identity)
	}
	if decoded.Devices[0].SIM.SubscriberNumber != response.Devices[0].SIM.SubscriberNumber {
		t.Fatalf("subscriber number did not survive the Agent protocol: %#v", decoded.Devices[0].SIM)
	}
}

func TestDescriptorOnlyProbeSerializesEmptyRegistrationsArray(t *testing.T) {
	response := validProbeResponseFixture()
	response.Devices = []DeviceProbe{{
		DeviceID:       "usb-1-3",
		State:          ProbeStateDescriptorOnly,
		RF:             RFObservation{State: RFStateUnknown},
		SIM:            SIMObservation{State: SIMStateUnknown, PrimaryLockState: PrimaryLockUnknown},
		SignalMetrics:  SignalObservation{State: SignalStateUnknown},
		Registrations:  []RegistrationObservation{},
		CurrentNetwork: NetworkObservation{SelectionMode: NetworkSelectionUnknown},
	}}
	if err := validateProbeResponse(response); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(response.Devices[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"registrations":[]`) {
		t.Fatalf("descriptor-only registrations did not serialize as an array: %s", encoded)
	}
}

type staticProbeTransport struct {
	payload []byte
}

func (transport staticProbeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(transport.payload))),
		Request:    request,
	}, nil
}

func TestClientRejectsInvalidTypedProbeResponse(t *testing.T) {
	response := validProbeResponseFixture()
	response.Devices[0].SIM.PrimaryLockState = ""
	payload, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{
		http:       &http.Client{Transport: staticProbeTransport{payload: payload}},
		socketPath: "/run/simplus-agent.sock",
	}
	if _, err := client.Probe(t.Context(), ProbeRequest{}); err == nil || !strings.Contains(err.Error(), "invalid probe response") {
		t.Fatalf("invalid typed response error = %v", err)
	}
}
