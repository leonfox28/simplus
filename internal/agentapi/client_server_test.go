package agentapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestListenerRejectsGroupWritableDirectory(t *testing.T) {
	_, err := Listen(ListenerOptions{
		Path: filepath.Join(t.TempDir(), "agent.sock"), DirectoryMode: 0o770, SocketMode: 0o660,
		OwnerUID: -1, OwnerGID: -1, AllowedUIDs: []uint32{uint32(os.Geteuid())},
	})
	if err == nil {
		t.Fatal("group-writable Agent directory unexpectedly accepted")
	}
}

func TestHelloOmitsMutatingFeaturesWhenCommandServiceIsDisabled(t *testing.T) {
	monitor := newMonitor(&monitorScanner{}, "01234567-89ab-cdef-0123-456789abcdef", 1)
	request := httptest.NewRequest(http.MethodGet, "/v1/hello", nil)
	response := httptest.NewRecorder()
	NewHandler(monitor, nil, nil).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	var hello Hello
	if err := json.Unmarshal(response.Body.Bytes(), &hello); err != nil {
		t.Fatal(err)
	}
	if containsString(hello.Features, CommandRadioEnsureOff) || containsString(hello.Features, "durable-command-outcomes") || containsString(hello.Features, FeatureSMS) {
		t.Fatalf("disabled command features = %#v", hello.Features)
	}
	smsRequest := httptest.NewRequest(http.MethodPost, "/v1/sms/list", strings.NewReader(`{"agentInstanceId":"01234567-89ab-cdef-0123-456789abcdef","deviceId":"usb-1-1"}`))
	smsResponse := httptest.NewRecorder()
	NewHandler(monitor, nil, nil).ServeHTTP(smsResponse, smsRequest)
	if smsResponse.Code != http.StatusNotFound {
		t.Fatalf("disabled SMS route status = %d", smsResponse.Code)
	}
}

func TestReadOnlyHardwareHandlerCannotExposeMutationRoutes(t *testing.T) {
	monitor := newMonitor(&monitorScanner{}, "01234567-89ab-cdef-0123-456789abcdef", 1)
	handler := NewReadOnlyHardwareHandler(monitor, nil)
	helloResponse := httptest.NewRecorder()
	handler.ServeHTTP(helloResponse, httptest.NewRequest(http.MethodGet, "/v1/hello", nil))
	var hello Hello
	if err := json.Unmarshal(helloResponse.Body.Bytes(), &hello); err != nil {
		t.Fatal(err)
	}
	if !containsString(hello.Features, FeatureHardwareReadOnly) {
		t.Fatalf("read-only feature missing: %#v", hello.Features)
	}
	for _, route := range []string{
		"/v1/commands/radio/ensure-off", "/v1/sms/list", "/v1/sms/read", "/v1/sms/send", "/v1/sms/acknowledge",
		"/v1/sim/aka/identity", "/v1/sim/aka/authenticate", "/v1/equipment-identity/read",
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, route, strings.NewReader(`{}`)))
		if response.Code != http.StatusNotFound {
			t.Fatalf("read-only route %s status = %d, want 404", route, response.Code)
		}
	}
}

func TestManagedHardwareHandlerDisclosesEquipmentIdentityOnlyThroughNoStoreRoute(t *testing.T) {
	monitor := newMonitor(&monitorScanner{devices: []DeviceReport{{
		ID: "usb-1-3", DisplayName: "ML307A", PhysicalPath: "1-3", Profile: ProfileML307A,
	}}}, "01234567-89ab-cdef-0123-456789abcdef", 1)
	snapshot, err := monitor.Refresh(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	identity := NewEquipmentIdentityService(monitor, &fakeEquipmentIdentityBackend{observation: EquipmentIdentityObservation{
		IMEI: "490154203237518", Fingerprint: strings.Repeat("a", 64),
	}})
	handler := NewManagedHardwareHandler(monitor, nil, identity, nil)
	body, err := json.Marshal(EquipmentIdentityReadRequest{
		AgentInstanceID: snapshot.AgentInstanceID, SnapshotGeneration: snapshot.Generation,
		SnapshotRevision: snapshot.Revision, DeviceID: snapshot.Devices[0].ID,
		DeviceGeneration: snapshot.Devices[0].Generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/equipment-identity/read", strings.NewReader(string(body))))
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(response.Body.String(), `"imei":"490154203237518"`) {
		t.Fatalf("status=%d cache=%q body=%s", response.Code, response.Header().Get("Cache-Control"), response.Body.String())
	}
}

func TestUnixClientServerProtocolRoundTrip(t *testing.T) {
	scanner := &monitorScanner{
		devices: []DeviceReport{{ID: "usb-1-1", DisplayName: "QDC507", PhysicalPath: "1-1", Profile: ProfileQDC507}},
		probes: []DeviceProbe{validCompleteProbeFixture(
			"usb-1-1",
			SIMObservation{State: SIMStateAbsent, PrimaryLockState: PrimaryLockUnknown},
		)},
	}
	monitor := newMonitor(scanner, "01234567-89ab-cdef-0123-456789abcdef", 1)
	if _, err := monitor.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	outcomes := openTestOutcomeStore(t, filepath.Join(t.TempDir(), "agent-state"), 8, 2)
	executor := &fakeRadioExecutor{execution: RadioEnsureOffExecution{
		Observation: RadioEnsureOffObservation{
			RF: RFObservation{State: RFStateOff, Mode: intPointerForAgentTest(4)}, ActiveCallCount: intPointerForAgentTest(0),
		},
	}}
	commands := NewCommandService(monitor, executor, outcomes)
	smsBackend := NewSimulatorSMSBackend(SMSStoredMessage{
		MessageID: "agent-inbound-1", DeviceID: "usb-1-1", Sender: "10086", Body: "Agent simulator inbound",
		ReceivedAt: time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC),
	})
	directory, err := os.MkdirTemp("/tmp", "simplus-agent-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	socket := filepath.Join(directory, "agent.sock")
	listener, err := Listen(ListenerOptions{
		Path: socket, DirectoryMode: 0o700, SocketMode: 0o600, OwnerUID: -1, OwnerGID: -1,
		AllowedUIDs: []uint32{uint32(os.Geteuid())},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: NewHandler(monitor, commands, nil, smsBackend)}
	go server.Serve(listener)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		_ = listener.Close()
	})
	client, err := NewClient(socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hello, err := client.Hello(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if hello.Protocol != ProtocolName || hello.ProtocolVersion != ProtocolVersion || hello.AgentInstanceID != monitor.InstanceID() {
		t.Fatalf("hello = %#v", hello)
	}
	if !containsString(hello.Features, CommandRadioEnsureOff) || !containsString(hello.Features, "durable-command-outcomes") || !containsString(hello.Features, FeatureSMS) {
		t.Fatalf("hello features = %#v", hello.Features)
	}
	snapshot, err := client.Snapshot(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.AgentInstanceID != hello.AgentInstanceID || snapshot.Generation != 1 || len(snapshot.Devices) != 1 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	probe, err := client.Probe(ctx, ProbeRequest{DeviceIDs: []string{"usb-1-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if probe.AgentInstanceID != hello.AgentInstanceID || len(probe.Devices) != 1 || probe.Devices[0].State != ProbeStateComplete {
		t.Fatalf("probe = %#v", probe)
	}
	commandRequest := commandRequestForSnapshot(snapshot)
	command, err := client.EnsureRadioOff(ctx, commandRequest)
	if err != nil {
		t.Fatal(err)
	}
	if command.Outcome.State != CommandOutcomeSucceeded || command.Outcome.Observation.RF.State != RFStateOff || executor.calls != 1 {
		t.Fatalf("command = %#v calls=%d", command, executor.calls)
	}
	conflict := commandRequest
	conflict.FencingToken++
	_, err = client.EnsureRadioOff(ctx, conflict)
	var apiError *ErrorResponse
	if !errors.As(err, &apiError) || apiError.Code != "OPERATION_REPLAY_CONFLICT" || apiError.Retryable {
		t.Fatalf("replay conflict error = %#v", err)
	}

	listRequest := SMSListRequest{AgentInstanceID: hello.AgentInstanceID, DeviceID: "usb-1-1"}
	listed, err := client.ListSMS(ctx, listRequest)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Messages) != 1 || listed.Messages[0].MessageID != "agent-inbound-1" {
		t.Fatalf("SMS list = %#v", listed)
	}
	read, err := client.ReadSMS(ctx, SMSReadRequest{
		AgentInstanceID: hello.AgentInstanceID, DeviceID: "usb-1-1", MessageID: listed.Messages[0].MessageID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if read.Message.Body != "Agent simulator inbound" {
		t.Fatalf("SMS read = %#v", read)
	}
	sendRequest := SMSSendRequest{
		OperationID: "operation-sms-0123456789", AgentInstanceID: hello.AgentInstanceID,
		DeviceID: "usb-1-1", Destination: "+8613800138000", Body: "一条经过 UCS-2 编码的 fake Agent 短信",
	}
	sent, err := client.SendSMS(ctx, sendRequest)
	if err != nil {
		t.Fatal(err)
	}
	if sent.Submission.OperationID != "operation-sms-0123456789" || sent.Submission.MessageID == "" {
		t.Fatalf("SMS send = %#v", sent)
	}
	replayedSend, err := client.SendSMS(ctx, sendRequest)
	if err != nil || replayedSend.Submission.MessageID != sent.Submission.MessageID {
		t.Fatalf("SMS send replay = %#v, error = %v", replayedSend, err)
	}
	conflictingSend := sendRequest
	conflictingSend.Body = "different body"
	_, err = client.SendSMS(ctx, conflictingSend)
	if !errors.As(err, &apiError) || apiError.Code != "OPERATION_REPLAY_CONFLICT" {
		t.Fatalf("SMS send conflict error = %#v", err)
	}
	ackRequest := SMSAcknowledgeRequest{
		OperationID: "operation-ack-012345678", AgentInstanceID: hello.AgentInstanceID,
		DeviceID: "usb-1-1", MessageID: "agent-inbound-1",
	}
	acknowledged, err := client.AcknowledgeSMS(ctx, ackRequest)
	if err != nil {
		t.Fatal(err)
	}
	if !acknowledged.Acknowledged {
		t.Fatalf("SMS acknowledge = %#v", acknowledged)
	}
	if _, err := client.AcknowledgeSMS(ctx, ackRequest); err != nil {
		t.Fatalf("idempotent SMS acknowledge: %v", err)
	}
	listed, err = client.ListSMS(ctx, listRequest)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Messages) != 0 {
		t.Fatalf("SMS list after acknowledge = %#v", listed)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
