package agentapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeSIMAKABackend struct {
	imsi          string
	execution     SIMAKAExecution
	identityCalls int
	authCalls     int
	imsCalls      int
	isimAvailable bool
	imsIdentity   SIMIMSIdentityMaterial
	imsReadCalls  int
	challenge     SIMAKAChallenge
}

func (backend *fakeSIMAKABackend) ReadSIMAKAIdentity(context.Context, Snapshot, string, string) (string, error) {
	backend.identityCalls++
	return backend.imsi, nil
}

func (backend *fakeSIMAKABackend) AuthenticateSIMAKA(_ context.Context, _ Snapshot, _ string, _ string, challenge SIMAKAChallenge) (SIMAKAExecution, error) {
	backend.authCalls++
	backend.challenge = challenge
	return backend.execution, nil
}

func (backend *fakeSIMAKABackend) ProbeSIMIMSProfile(context.Context, Snapshot, string, string) (bool, error) {
	backend.imsCalls++
	return backend.isimAvailable, nil
}

func (backend *fakeSIMAKABackend) ReadSIMIMSIdentity(context.Context, Snapshot, string, string) (SIMIMSIdentityMaterial, error) {
	backend.imsReadCalls++
	return backend.imsIdentity, nil
}

func newSIMAKAServiceFixture(t *testing.T) (*SIMAKAService, *fakeSIMAKABackend, SIMAKATarget, *monitorScanner) {
	t.Helper()
	fingerprint := strings.Repeat("b", 64)
	scanner := &monitorScanner{
		devices: []DeviceReport{{
			ID: "usb-1-3", DisplayName: "ML307A", PhysicalPath: "1-3", Profile: ProfileML307A,
			Capabilities: []CapabilityEvidence{
				{Capability: "at-control", Status: EvidenceObserved},
				{Capability: "sim-apdu", Status: EvidenceObserved},
			},
		}},
		probes: []DeviceProbe{validCompleteProbeFixture("usb-1-3", SIMObservation{
			State: SIMStatePresent, PrimaryLockState: PrimaryLockReady,
			IdentityFingerprint: fingerprint, DisplayIdentityHint: "ICCID •••• 3198",
		})},
	}
	monitor := newMonitor(scanner, "01234567-89ab-cdef-0123-456789abcdef", 1)
	snapshot, err := monitor.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeSIMAKABackend{imsi: "234150123456789"}
	backend.imsIdentity = SIMIMSIdentityMaterial{
		Source: SIMIMSIdentityISIM, PrivateIdentity: "234150123456789@ims.mnc015.mcc234.3gppnetwork.org",
		HomeDomain:           "ims.mnc015.mcc234.3gppnetwork.org",
		PublicIdentities:     []string{"sip:234150123456789@ims.mnc015.mcc234.3gppnetwork.org"},
		ApplicationDiscovery: SIMIMSDiscoveryEFDIR, ApplicationCandidates: 1,
	}
	backend.execution.State = SIMAKAStateSuccess
	backend.execution.RES = []byte{1, 2, 3, 4, 5, 6, 7, 8}
	for index := range backend.execution.CK {
		backend.execution.CK[index] = byte(index + 1)
		backend.execution.IK[index] = byte(index + 17)
	}
	target := SIMAKATarget{
		AgentInstanceID: snapshot.AgentInstanceID, SnapshotGeneration: snapshot.Generation,
		SnapshotRevision: snapshot.Revision, DeviceID: "usb-1-3", DeviceGeneration: snapshot.Devices[0].Generation,
		IdentityFingerprint: fingerprint,
	}
	return NewSIMAKAService(monitor, backend), backend, target, scanner
}

func TestSIMAKAServiceFencesIdentityAndAuthenticationToReadyRFOffSIM(t *testing.T) {
	service, backend, target, _ := newSIMAKAServiceFixture(t)
	identity, err := service.Identity(t.Context(), SIMAKAIdentityRequest{SIMAKATarget: target})
	if err != nil {
		t.Fatal(err)
	}
	if identity.IMSI != "234150123456789" || identity.DeviceID != target.DeviceID || backend.identityCalls != 1 {
		t.Fatalf("identity = %#v calls=%d", identity, backend.identityCalls)
	}
	profile, err := service.IMSProfile(t.Context(), SIMIMSProfileRequest{SIMAKATarget: target})
	if err != nil || profile.ISIMAvailable || profile.IdentitySource != SIMIMSIdentityDerived || backend.imsCalls != 1 {
		t.Fatalf("profile=%#v calls=%d error=%v", profile, backend.imsCalls, err)
	}
	backend.isimAvailable = true
	profile, err = service.IMSProfile(t.Context(), SIMIMSProfileRequest{SIMAKATarget: target})
	if err != nil || !profile.ISIMAvailable || profile.IdentitySource != SIMIMSIdentityISIM || backend.imsCalls != 2 {
		t.Fatalf("profile=%#v calls=%d error=%v", profile, backend.imsCalls, err)
	}
	imsIdentity, err := service.IMSIdentity(t.Context(), SIMIMSIdentityRequest{SIMAKATarget: target})
	if err != nil || imsIdentity.IdentitySource != SIMIMSIdentityISIM || len(imsIdentity.PublicIdentities) != 1 || backend.imsReadCalls != 1 {
		t.Fatalf("IMS identity=%#v calls=%d error=%v", imsIdentity, backend.imsReadCalls, err)
	}

	request := SIMAKAAuthenticationRequest{
		SIMAKATarget: target, ExchangeID: "exchange_0123456789",
		RAND: strings.Repeat("01", 16), AUTN: strings.Repeat("a2", 16),
	}
	response, err := service.Authenticate(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if response.Result.State != SIMAKAStateSuccess || response.Result.RES != "0102030405060708" ||
		len(response.Result.CK) != 32 || len(response.Result.IK) != 32 || backend.authCalls != 1 {
		t.Fatalf("authentication = %#v calls=%d", response, backend.authCalls)
	}
	if !bytes.Equal(backend.challenge.RAND[:], bytes.Repeat([]byte{0x01}, 16)) ||
		!bytes.Equal(backend.challenge.AUTN[:], bytes.Repeat([]byte{0xa2}, 16)) {
		t.Fatal("backend received a different AKA challenge")
	}
}

func TestSIMAKAServiceFailsClosedBeforeBackend(t *testing.T) {
	service, backend, target, scanner := newSIMAKAServiceFixture(t)
	tests := []struct {
		name   string
		mutate func(*SIMAKATarget)
		err    error
	}{
		{name: "stale Agent", mutate: func(target *SIMAKATarget) { target.AgentInstanceID = "fedcba98-7654-3210-fedc-ba9876543210" }, err: ErrSIMAKAAgentStale},
		{name: "stale snapshot", mutate: func(target *SIMAKATarget) { target.SnapshotGeneration++ }, err: ErrSIMAKASnapshotStale},
		{name: "stale device", mutate: func(target *SIMAKATarget) { target.DeviceGeneration++ }, err: ErrSIMAKADeviceStale},
		{name: "changed SIM", mutate: func(target *SIMAKATarget) { target.IdentityFingerprint = strings.Repeat("c", 64) }, err: ErrSIMAKAIdentityChanged},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := target
			test.mutate(&candidate)
			if _, err := service.Identity(t.Context(), SIMAKAIdentityRequest{SIMAKATarget: candidate}); !errors.Is(err, test.err) {
				t.Fatalf("error = %v, want %v", err, test.err)
			}
		})
	}
	scanner.probes[0].RF.State = RFStateOn
	if _, err := service.Identity(t.Context(), SIMAKAIdentityRequest{SIMAKATarget: target}); !errors.Is(err, ErrSIMAKARFNotOff) {
		t.Fatalf("RF-on error = %v", err)
	}
	if backend.identityCalls != 0 || backend.authCalls != 0 {
		t.Fatalf("backend called after rejected target: identity=%d auth=%d", backend.identityCalls, backend.authCalls)
	}
}

func TestSIMAKAHILHandlerRoundTripAndNoSecretLogging(t *testing.T) {
	service, _, target, _ := newSIMAKAServiceFixture(t)
	var logs bytes.Buffer
	handler := NewSIMAKAHILHandler(service, slog.New(slog.NewJSONHandler(&logs, nil)))

	hello := httptest.NewRecorder()
	handler.ServeHTTP(hello, httptest.NewRequest(http.MethodGet, "/v1/hello", nil))
	var helloBody Hello
	if err := json.Unmarshal(hello.Body.Bytes(), &helloBody); err != nil {
		t.Fatal(err)
	}
	if hello.Code != http.StatusOK || !containsString(helloBody.Features, FeatureSIMAKAHIL) {
		t.Fatalf("hello status=%d body=%#v", hello.Code, helloBody)
	}

	payload, err := json.Marshal(SIMAKAAuthenticationRequest{
		SIMAKATarget: target, ExchangeID: "exchange_0123456789",
		RAND: strings.Repeat("01", 16), AUTN: strings.Repeat("a2", 16),
	})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/sim/aka/authenticate", bytes.NewReader(payload)))
	if response.Code != http.StatusOK || !strings.Contains(logs.String(), "SIM AKA request completed") || !strings.Contains(logs.String(), `"state":"success"`) ||
		strings.Contains(logs.String(), strings.Repeat("01", 16)) || strings.Contains(logs.String(), strings.Repeat("a2", 16)) {
		t.Fatalf("status=%d body=%s logs=%s", response.Code, response.Body.String(), logs.String())
	}

	bad := httptest.NewRecorder()
	handler.ServeHTTP(bad, httptest.NewRequest(http.MethodPost, "/v1/sim/aka/authenticate", strings.NewReader(`{"rand":"secret"}`)))
	if bad.Code != http.StatusBadRequest || strings.Contains(logs.String(), "secret") {
		t.Fatalf("bad status=%d logs=%s", bad.Code, logs.String())
	}

	ims := httptest.NewRecorder()
	imsPayload, err := json.Marshal(SIMIMSIdentityRequest{SIMAKATarget: target})
	if err != nil {
		t.Fatal(err)
	}
	handler.ServeHTTP(ims, httptest.NewRequest(http.MethodPost, "/v1/sim/ims/identity", bytes.NewReader(imsPayload)))
	if ims.Code != http.StatusOK || !strings.Contains(logs.String(), "SIM IMS identity read completed") ||
		strings.Contains(logs.String(), "234150123456789") || strings.Contains(logs.String(), "ims.mnc015") {
		t.Fatalf("IMS status=%d logs=%s", ims.Code, logs.String())
	}
}
