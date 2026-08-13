package qdc507sms

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/modemadapter"
	"github.com/leonfox28/simplus/internal/smscodec"
)

type fixtureDriver struct {
	messages       map[int]StoredPDU
	deleteFailures map[int]int
	readCalls      []int
	deleteCalls    []int
	sendCalls      int
	sendResult     SendResult
	sendErr        error
}

func (driver *fixtureDriver) List(ctx context.Context, _ agentapi.DeviceReport) ([]StoredPDU, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := make([]StoredPDU, 0, len(driver.messages))
	for _, message := range driver.messages {
		result = append(result, cloneStoredPDU(message))
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Index < result[right].Index })
	return result, nil
}

func (driver *fixtureDriver) Read(ctx context.Context, _ agentapi.DeviceReport, index int) (StoredPDU, error) {
	if err := ctx.Err(); err != nil {
		return StoredPDU{}, err
	}
	driver.readCalls = append(driver.readCalls, index)
	message, found := driver.messages[index]
	if !found {
		return StoredPDU{}, agentapi.ErrSMSMessageNotFound
	}
	message.Status = 1
	driver.messages[index] = message
	return cloneStoredPDU(message), nil
}

func (driver *fixtureDriver) Delete(ctx context.Context, _ agentapi.DeviceReport, index int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	driver.deleteCalls = append(driver.deleteCalls, index)
	if driver.deleteFailures[index] > 0 {
		driver.deleteFailures[index]--
		return errors.New("injected delete response failure")
	}
	if _, found := driver.messages[index]; !found {
		return agentapi.ErrSMSMessageNotFound
	}
	delete(driver.messages, index)
	return nil
}

func (driver *fixtureDriver) Send(ctx context.Context, _ agentapi.DeviceReport, _, _ string) (SendResult, error) {
	if err := ctx.Err(); err != nil {
		return SendResult{}, err
	}
	driver.sendCalls++
	return driver.sendResult, driver.sendErr
}

func TestAssembleInboundKeepsStableIDAcrossReadStatusAndRequiresCompleteMultipart(t *testing.T) {
	receivedAt := time.Date(2026, 8, 3, 10, 11, 12, 0, time.UTC)
	body := strings.Repeat("你", 71)
	segments, err := smscodec.Encode(body)
	if err != nil || len(segments) != 2 {
		t.Fatalf("segments = %#v, error = %v", segments, err)
	}
	first := storedDeliverPDU(t, 10, 0, "+8613800138000", receivedAt, segments[0])
	second := storedDeliverPDU(t, 11, 0, "+8613800138000", receivedAt.Add(time.Second), segments[1])

	complete, err := assembleInbound(strings.Repeat("a", 64), []StoredPDU{second, first})
	if err != nil || len(complete) != 1 || complete[0].Body != body || len(complete[0].Segments) != 2 {
		t.Fatalf("complete assembly = %#v, error = %v", complete, err)
	}
	if complete[0].ReceivedAt != receivedAt || complete[0].Segments[0].Index != 10 || complete[0].Segments[1].Index != 11 {
		t.Fatalf("assembled metadata = %#v", complete[0])
	}

	first.Status = 1
	second.Status = 1
	relisted, err := assembleInbound(strings.Repeat("a", 64), []StoredPDU{first, second})
	if err != nil || len(relisted) != 1 || relisted[0].MessageID != complete[0].MessageID {
		t.Fatalf("read-status assembly = %#v, error = %v", relisted, err)
	}
	if incomplete, err := assembleInbound(strings.Repeat("a", 64), []StoredPDU{second}); err != nil || len(incomplete) != 0 {
		t.Fatalf("incomplete assembly = %#v, error = %v", incomplete, err)
	}

	duplicatePart := first
	duplicatePart.Index = 12
	if ambiguous, err := assembleInbound(strings.Repeat("a", 64), []StoredPDU{first, duplicatePart}); err != nil || len(ambiguous) != 0 {
		t.Fatalf("ambiguous assembly = %#v, error = %v", ambiguous, err)
	}
	lateSecond := storedDeliverPDU(t, 11, 0, "+8613800138000", receivedAt.Add(maxMultipartAssemblySpan+time.Second), segments[1])
	if stale, err := assembleInbound(strings.Repeat("a", 64), []StoredPDU{first, lateSecond}); err != nil || len(stale) != 0 {
		t.Fatalf("stale multipart assembly = %#v, error = %v", stale, err)
	}
}

func TestAdapterRecoversMultipartAcknowledgeAcrossAdapterRestart(t *testing.T) {
	receivedAt := time.Date(2026, 8, 3, 10, 11, 12, 0, time.UTC)
	body := strings.Repeat("你", 71)
	segments, err := smscodec.Encode(body)
	if err != nil {
		t.Fatal(err)
	}
	driver := &fixtureDriver{
		messages: map[int]StoredPDU{
			10: storedDeliverPDU(t, 10, 0, "10086", receivedAt, segments[0]),
			11: storedDeliverPDU(t, 11, 0, "10086", receivedAt.Add(time.Second), segments[1]),
		},
		deleteFailures: map[int]int{11: 1},
	}
	store := NewMemoryStateStore()
	adapter, err := NewAdapter(driver, store)
	if err != nil {
		t.Fatal(err)
	}
	references, err := adapter.ListSMS(t.Context(), qdc507Target())
	if err != nil || len(references) != 1 {
		t.Fatalf("initial list = %#v, error = %v", references, err)
	}
	messageID := references[0].MessageID
	message, err := adapter.ReadSMS(t.Context(), qdc507Target(), messageID)
	if err != nil || message.Body != body {
		t.Fatalf("read = %#v, error = %v", message, err)
	}
	acknowledge := agentapi.SMSAcknowledgeRequest{
		OperationID: "acknowledge-0123456789", DeviceID: qdc507Device().ID, MessageID: messageID,
	}
	if acknowledged, err := adapter.AcknowledgeSMS(t.Context(), qdc507Target(), acknowledge); err == nil || acknowledged {
		t.Fatalf("partial acknowledge = %t, error = %v", acknowledged, err)
	}
	if _, found := driver.messages[10]; found {
		t.Fatal("first multipart segment was not deleted")
	}

	// Reconstructing the adapter while retaining the state store models the
	// lifecycle expected from a future process-durable StateStore.
	restarted, err := NewAdapter(driver, store)
	if err != nil {
		t.Fatal(err)
	}
	references, err = restarted.ListSMS(t.Context(), qdc507Target())
	if err != nil || len(references) != 1 || references[0].MessageID != messageID {
		t.Fatalf("list after partial ACK = %#v, error = %v", references, err)
	}
	message, err = restarted.ReadSMS(t.Context(), qdc507Target(), messageID)
	if err != nil || message.Body != body {
		t.Fatalf("read after partial ACK = %#v, error = %v", message, err)
	}
	acknowledged, err := restarted.AcknowledgeSMS(t.Context(), qdc507Target(), acknowledge)
	if err != nil || !acknowledged {
		t.Fatalf("resumed acknowledge = %t, error = %v", acknowledged, err)
	}
	if countValue(driver.deleteCalls, 10) != 1 || countValue(driver.deleteCalls, 11) != 2 {
		t.Fatalf("delete calls = %#v", driver.deleteCalls)
	}
	if replayed, err := restarted.AcknowledgeSMS(t.Context(), qdc507Target(), acknowledge); err != nil || !replayed {
		t.Fatalf("acknowledge replay = %t, error = %v", replayed, err)
	}
	references, err = restarted.ListSMS(t.Context(), qdc507Target())
	if err != nil || len(references) != 0 {
		t.Fatalf("list after ACK = %#v, error = %v", references, err)
	}
}

func TestAdapterNeverDeletesAReusedStorageIndex(t *testing.T) {
	receivedAt := time.Date(2026, 8, 3, 10, 11, 12, 0, time.UTC)
	original := encodedSingleSegment(t, "原消息")
	replacement := encodedSingleSegment(t, "新消息")
	driver := &fixtureDriver{
		messages:       map[int]StoredPDU{7: storedDeliverPDU(t, 7, 0, "10086", receivedAt, original)},
		deleteFailures: make(map[int]int),
	}
	adapter, err := NewAdapter(driver, NewMemoryStateStore())
	if err != nil {
		t.Fatal(err)
	}
	references, err := adapter.ListSMS(t.Context(), qdc507Target())
	if err != nil || len(references) != 1 {
		t.Fatalf("list = %#v, error = %v", references, err)
	}
	driver.messages[7] = storedDeliverPDU(t, 7, 0, "10086", receivedAt.Add(time.Second), replacement)
	acknowledged, err := adapter.AcknowledgeSMS(t.Context(), qdc507Target(), agentapi.SMSAcknowledgeRequest{
		OperationID: "acknowledge-reused-01", DeviceID: qdc507Device().ID, MessageID: references[0].MessageID,
	})
	if !errors.Is(err, ErrStorageIndexReused) || acknowledged || len(driver.deleteCalls) != 0 {
		t.Fatalf("reused-index acknowledge = %t, error = %v, deletes = %#v", acknowledged, err, driver.deleteCalls)
	}
}

func TestAdapterDurableNamespaceFollowsSIMAcrossUSBPortAndIsolatesSwap(t *testing.T) {
	decoded, err := hex.DecodeString("0891683108200105F0040D91685120012194F600F10180817144302304F4F29C0E")
	if err != nil {
		t.Fatal(err)
	}
	driver := &fixtureDriver{messages: map[int]StoredPDU{3: {Index: 3, Status: 0, TPDULength: 24, PDU: decoded}}, deleteFailures: map[int]int{}}
	adapter, err := NewAdapter(driver, NewMemoryStateStore())
	if err != nil {
		t.Fatal(err)
	}
	first := qdc507Target()
	references, err := adapter.ListSMS(t.Context(), first)
	if err != nil || len(references) != 1 {
		t.Fatalf("first list=%#v error=%v", references, err)
	}
	second := first
	second.Device.ID = "usb-9-9"
	message, err := adapter.ReadSMS(t.Context(), second, references[0].MessageID)
	if err != nil || message.DeviceID != second.Device.ID {
		t.Fatalf("hotplug read=%#v error=%v", message, err)
	}
	swapped := second
	swapped.SubscriptionKey = strings.Repeat("c", 64)
	if _, err := adapter.ReadSMS(t.Context(), swapped, references[0].MessageID); !errors.Is(err, agentapi.ErrSMSMessageNotFound) {
		t.Fatalf("swapped SIM read error=%v", err)
	}
}

func TestAdapterReplaysSuccessfulSendAndDoesNotReplayUnknownSend(t *testing.T) {
	store := NewMemoryStateStore()
	driver := &fixtureDriver{
		messages: make(map[int]StoredPDU), deleteFailures: make(map[int]int),
		sendResult: SendResult{Parts: []PartSubmission{{Part: 1, Total: 1, MessageReference: 42}}},
	}
	adapter, err := NewAdapter(driver, store)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	adapter.now = func() time.Time { return now }
	request := agentapi.SMSSendRequest{
		OperationID: "operation-0123456789", AgentInstanceID: "old-agent", DeviceID: qdc507Device().ID,
		Destination: "10086", Body: "hello",
	}
	first, err := adapter.SendSMS(t.Context(), qdc507Target(), request)
	if err != nil || first.OperationID != request.OperationID || first.MessageID == "" || first.SubmittedAt != now {
		t.Fatalf("first send = %#v, error = %v", first, err)
	}
	restarted, err := NewAdapter(driver, store)
	if err != nil {
		t.Fatal(err)
	}
	request.AgentInstanceID = "new-agent"
	replayed, err := restarted.SendSMS(t.Context(), qdc507Target(), request)
	if err != nil || replayed != first || driver.sendCalls != 1 {
		t.Fatalf("send replay = %#v, error = %v, calls = %d", replayed, err, driver.sendCalls)
	}
	conflict := request
	conflict.Body = "different"
	if _, err := restarted.SendSMS(t.Context(), qdc507Target(), conflict); !errors.Is(err, agentapi.ErrSMSOperationConflict) {
		t.Fatalf("conflicting send error = %v", err)
	}

	unknownDriver := &fixtureDriver{
		messages: make(map[int]StoredPDU), deleteFailures: make(map[int]int),
		sendErr: &SendFailure{CompletedParts: 1, TotalParts: 2, Cause: errors.New("second part rejected")},
	}
	unknownAdapter, err := NewAdapter(unknownDriver, NewMemoryStateStore())
	if err != nil {
		t.Fatal(err)
	}
	unknownRequest := request
	unknownRequest.OperationID = "operation-unknown-0123"
	if _, err := unknownAdapter.SendSMS(t.Context(), qdc507Target(), unknownRequest); !errors.Is(err, agentapi.ErrSMSOutcomeUnknown) {
		t.Fatalf("partial send error = %v", err)
	}
	if _, err := unknownAdapter.SendSMS(t.Context(), qdc507Target(), unknownRequest); !errors.Is(err, agentapi.ErrSMSOutcomeUnknown) {
		t.Fatalf("unknown replay error = %v", err)
	}
	if unknownDriver.sendCalls != 1 {
		t.Fatalf("unknown send was redispatched %d times", unknownDriver.sendCalls)
	}
}

func TestCandidateAdapterRequiresExplicitNonDefaultRegistration(t *testing.T) {
	adapter, err := NewAdapter(&fixtureDriver{
		messages: make(map[int]StoredPDU), deleteFailures: make(map[int]int),
	}, NewMemoryStateStore())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := modemadapter.NewRegistry(adapter)
	if err != nil {
		t.Fatal(err)
	}
	if !registry.SupportsSMS() {
		t.Fatal("explicit candidate registry did not expose its typed SMS capability")
	}
	if modemadapter.DefaultRegistry().SupportsSMS() {
		t.Fatal("candidate adapter leaked into the default Agent registry")
	}
}

func TestAdapterReleasesOperationAfterConfirmedNoSideEffect(t *testing.T) {
	driver := &fixtureDriver{
		messages: make(map[int]StoredPDU), deleteFailures: make(map[int]int), sendErr: errors.New("pre-dispatch failure"),
	}
	adapter, err := NewAdapter(driver, NewMemoryStateStore())
	if err != nil {
		t.Fatal(err)
	}
	request := agentapi.SMSSendRequest{
		OperationID: "operation-retry-01234", DeviceID: qdc507Device().ID, Destination: "10086", Body: "hello",
	}
	for range 2 {
		if _, err := adapter.SendSMS(t.Context(), qdc507Target(), request); err == nil || errors.Is(err, agentapi.ErrSMSOutcomeUnknown) {
			t.Fatalf("confirmed failure error = %v", err)
		}
	}
	if driver.sendCalls != 2 {
		t.Fatalf("confirmed no-side-effect attempts = %d", driver.sendCalls)
	}
}

func TestAdapterDoesNotRedispatchAnAcceptedOperationAfterRestart(t *testing.T) {
	store := NewMemoryStateStore()
	request := agentapi.SMSSendRequest{
		OperationID: "operation-interrupted-1", DeviceID: qdc507Device().ID, Destination: "10086", Body: "hello",
	}
	accepted := operationRecord{
		OperationID: request.OperationID, SubscriptionKey: qdc507Target().SubscriptionKey, Kind: operationSend,
		RequestDigest: digestFields(qdc507Target().SubscriptionKey, request.Destination, request.Body), State: operationAccepted,
	}
	if _, replayed, err := store.PutOperation(t.Context(), accepted); err != nil || replayed {
		t.Fatalf("accepted operation = replayed %t, error = %v", replayed, err)
	}
	driver := &fixtureDriver{
		messages: make(map[int]StoredPDU), deleteFailures: make(map[int]int),
		sendResult: SendResult{Parts: []PartSubmission{{Part: 1, Total: 1, MessageReference: 1}}},
	}
	adapter, err := NewAdapter(driver, store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.SendSMS(t.Context(), qdc507Target(), request); !errors.Is(err, agentapi.ErrSMSOutcomeUnknown) {
		t.Fatalf("interrupted replay error = %v", err)
	}
	if driver.sendCalls != 0 {
		t.Fatalf("interrupted operation was dispatched %d times", driver.sendCalls)
	}
}

func TestSQLiteStateStoreRecoversAcknowledgeAndSendAcrossReopen(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "agent", "qdc507-sms.sqlite3")
	store, err := OpenSQLiteStateStore(t.Context(), statePath)
	if err != nil {
		t.Fatal(err)
	}
	receivedAt := time.Date(2026, 8, 3, 10, 11, 12, 0, time.UTC)
	body := strings.Repeat("你", 71)
	segments, err := smscodec.Encode(body)
	if err != nil {
		t.Fatal(err)
	}
	driver := &fixtureDriver{
		messages: map[int]StoredPDU{
			20: storedDeliverPDU(t, 20, 0, "10086", receivedAt, segments[0]),
			21: storedDeliverPDU(t, 21, 0, "10086", receivedAt.Add(time.Second), segments[1]),
		},
		deleteFailures: map[int]int{21: 1},
		sendResult:     SendResult{Parts: []PartSubmission{{Part: 1, Total: 1, MessageReference: 77}}},
	}
	adapter, err := NewAdapter(driver, store)
	if err != nil {
		t.Fatal(err)
	}
	references, err := adapter.ListSMS(t.Context(), qdc507Target())
	if err != nil || len(references) != 1 {
		t.Fatalf("initial durable list = %#v, error = %v", references, err)
	}
	acknowledge := agentapi.SMSAcknowledgeRequest{
		OperationID: "acknowledge-durable-01", DeviceID: qdc507Device().ID, MessageID: references[0].MessageID,
	}
	if acknowledged, err := adapter.AcknowledgeSMS(t.Context(), qdc507Target(), acknowledge); err == nil || acknowledged {
		t.Fatalf("partial durable acknowledge = %t, error = %v", acknowledged, err)
	}
	sendRequest := agentapi.SMSSendRequest{
		OperationID: "operation-durable-0123", DeviceID: qdc507Device().ID, Destination: "10086", Body: "hello",
	}
	submission, err := adapter.SendSMS(t.Context(), qdc507Target(), sendRequest)
	if err != nil {
		t.Fatal(err)
	}
	interruptedRequest := agentapi.SMSSendRequest{
		OperationID: "operation-durable-pending", DeviceID: qdc507Device().ID, Destination: "10010", Body: "pending",
	}
	accepted := operationRecord{
		OperationID: interruptedRequest.OperationID, SubscriptionKey: qdc507Target().SubscriptionKey, Kind: operationSend,
		RequestDigest: digestFields(qdc507Target().SubscriptionKey, interruptedRequest.Destination, interruptedRequest.Body), State: operationAccepted,
	}
	if _, replayed, err := store.PutOperation(t.Context(), accepted); err != nil || replayed {
		t.Fatalf("durable accepted operation = replayed %t, error = %v", replayed, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenSQLiteStateStore(t.Context(), statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted, err := NewAdapter(driver, reopened)
	if err != nil {
		t.Fatal(err)
	}
	references, err = restarted.ListSMS(t.Context(), qdc507Target())
	if err != nil || len(references) != 1 || references[0].MessageID != acknowledge.MessageID {
		t.Fatalf("durable list after reopen = %#v, error = %v", references, err)
	}
	if acknowledged, err := restarted.AcknowledgeSMS(t.Context(), qdc507Target(), acknowledge); err != nil || !acknowledged {
		t.Fatalf("durable resumed acknowledge = %t, error = %v", acknowledged, err)
	}
	replayed, err := restarted.SendSMS(t.Context(), qdc507Target(), sendRequest)
	if err != nil || replayed != submission || driver.sendCalls != 1 {
		t.Fatalf("durable send replay = %#v, error = %v, calls = %d", replayed, err, driver.sendCalls)
	}
	if _, err := restarted.SendSMS(t.Context(), qdc507Target(), interruptedRequest); !errors.Is(err, agentapi.ErrSMSOutcomeUnknown) {
		t.Fatalf("durable interrupted replay error = %v", err)
	}
	if driver.sendCalls != 1 {
		t.Fatalf("durable interrupted operation was redispatched %d times", driver.sendCalls)
	}
}

func TestSQLiteStateStoreAcceptsLegacySegmentsAndRejectsReviewedDeleteUncertainty(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "agent", "qdc507-sms.sqlite3")
	store, err := OpenSQLiteStateStore(t.Context(), statePath)
	if err != nil {
		t.Fatal(err)
	}
	record := InboundRecord{
		MessageID: "qdc507-in-synthetic-legacy", SubscriptionKey: qdc507Target().SubscriptionKey,
		Sender: "10086", Body: "synthetic legacy record", ReceivedAt: time.Date(2026, 8, 3, 10, 11, 12, 0, time.UTC),
		Segments: []InboundSegment{{Index: 9, PDUDigest: sha256.Sum256([]byte("synthetic-pdu"))}},
	}
	if _, replayed, err := store.PutInbound(t.Context(), record); err != nil || replayed {
		t.Fatalf("put legacy record: replayed=%t error=%v", replayed, err)
	}
	var legacyJSON []byte
	if err := store.db.QueryRow(`SELECT segments_json FROM inbound_messages WHERE message_id = ?`, record.MessageID).Scan(&legacyJSON); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(legacyJSON, []byte("DeleteStarted")) {
		t.Fatalf("ordinary legacy JSON unexpectedly changed: %s", legacyJSON)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenSQLiteStateStore(t.Context(), statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if loaded, found, err := reopened.FindInbound(t.Context(), record.SubscriptionKey, record.MessageID); err != nil || !found || loaded.Segments[0].DeleteStarted {
		t.Fatalf("legacy record=%#v found=%t error=%v", loaded, found, err)
	}
	started := append([]InboundSegment(nil), record.Segments...)
	started[0].DeleteStarted = true
	startedJSON, err := json.Marshal(started)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.db.Exec(`UPDATE inbound_messages SET segments_json = ? WHERE message_id = ?`, startedJSON, record.MessageID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reopened.FindInbound(t.Context(), record.SubscriptionKey, record.MessageID); err == nil {
		t.Fatal("ordinary runtime accepted reviewed delete-started uncertainty")
	}
}

func TestSQLiteStateStoreDoesNotRelaxAnExistingDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSQLiteStateStore(t.Context(), filepath.Join(directory, "state.sqlite3")); err == nil {
		t.Fatal("SQLite state accepted a non-private existing directory")
	}
	info, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("existing directory mode changed to %04o", info.Mode().Perm())
	}
}

func TestSQLiteStateStoreRejectsUnsafePathsAndArtifacts(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*testing.T, string, string)
	}{
		{
			name: "symlink directory",
			setup: func(t *testing.T, directory, _ string) {
				t.Helper()
				realDirectory := filepath.Join(filepath.Dir(directory), "real")
				if err := os.Mkdir(realDirectory, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(realDirectory, directory); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "symlink database",
			setup: func(t *testing.T, directory, path string) {
				t.Helper()
				mustPrivateDirectory(t, directory)
				target := filepath.Join(filepath.Dir(directory), "target.sqlite3")
				if err := os.WriteFile(target, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "hardlinked database",
			setup: func(t *testing.T, directory, path string) {
				t.Helper()
				mustPrivateDirectory(t, directory)
				target := filepath.Join(filepath.Dir(directory), "target.sqlite3")
				if err := os.WriteFile(target, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(target, path); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "public database mode",
			setup: func(t *testing.T, directory, path string) {
				t.Helper()
				mustPrivateDirectory(t, directory)
				if err := os.WriteFile(path, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "symlink WAL",
			setup: func(t *testing.T, directory, path string) {
				t.Helper()
				mustPrivateDirectory(t, directory)
				target := filepath.Join(filepath.Dir(directory), "target-wal")
				if err := os.WriteFile(target, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path+"-wal"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "hardlinked WAL",
			setup: func(t *testing.T, directory, path string) {
				t.Helper()
				mustPrivateDirectory(t, directory)
				target := filepath.Join(filepath.Dir(directory), "target-wal")
				if err := os.WriteFile(target, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(target, path+"-wal"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "public WAL mode",
			setup: func(t *testing.T, directory, path string) {
				t.Helper()
				mustPrivateDirectory(t, directory)
				if err := os.WriteFile(path+"-wal", nil, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path+"-wal", 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "symlink SHM",
			setup: func(t *testing.T, directory, path string) {
				t.Helper()
				mustPrivateDirectory(t, directory)
				target := filepath.Join(filepath.Dir(directory), "target-shm")
				if err := os.WriteFile(target, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path+"-shm"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "public SHM mode",
			setup: func(t *testing.T, directory, path string) {
				t.Helper()
				mustPrivateDirectory(t, directory)
				if err := os.WriteFile(path+"-shm", nil, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path+"-shm", 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "state")
			path := filepath.Join(directory, "state.sqlite3")
			test.setup(t, directory, path)
			if store, err := OpenSQLiteStateStore(t.Context(), path); err == nil {
				_ = store.Close()
				t.Fatal("unsafe QDC507 SMS state artifact was accepted")
			}
		})
	}
}

func TestSQLiteStateStoreRejectsUnsafeAncestorAndSchemaMismatch(t *testing.T) {
	unsafeAncestor := filepath.Join(t.TempDir(), "unsafe")
	if err := os.Mkdir(unsafeAncestor, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unsafeAncestor, 0o777); err != nil {
		t.Fatal(err)
	}
	if store, err := OpenSQLiteStateStore(t.Context(), filepath.Join(unsafeAncestor, "state", "state.sqlite3")); err == nil {
		_ = store.Close()
		t.Fatal("QDC507 SMS state accepted an unsafe ancestor")
	}

	path := filepath.Join(t.TempDir(), "state", "state.sqlite3")
	store, err := OpenSQLiteStateStore(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	var trustedSchema int
	if err := store.db.QueryRow(`PRAGMA trusted_schema`).Scan(&trustedSchema); err != nil || trustedSchema != 0 {
		t.Fatalf("trusted_schema=%d error=%v", trustedSchema, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=rw")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE unexpected_schema_object (id INTEGER PRIMARY KEY)`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if reopened, err := OpenSQLiteStateStore(t.Context(), path); err == nil {
		_ = reopened.Close()
		t.Fatal("QDC507 SMS state accepted a schema manifest mismatch")
	}
}

func mustPrivateDirectory(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

func encodedSingleSegment(t *testing.T, body string) smscodec.Segment {
	t.Helper()
	segments, err := smscodec.Encode(body)
	if err != nil || len(segments) != 1 {
		t.Fatalf("single segment = %#v, error = %v", segments, err)
	}
	return segments[0]
}

func storedDeliverPDU(t *testing.T, index, status int, sender string, receivedAt time.Time, segment smscodec.Segment) StoredPDU {
	t.Helper()
	digits := strings.TrimPrefix(sender, "+")
	address := make([]byte, (len(digits)+1)/2)
	for position := range digits {
		digit := digits[position] - '0'
		if digit > 9 {
			t.Fatalf("invalid fixture sender %q", sender)
		}
		if position%2 == 0 {
			address[position/2] = digit
		} else {
			address[position/2] |= digit << 4
		}
	}
	if len(digits)%2 == 1 {
		address[len(address)-1] |= 0xf0
	}
	firstOctet := byte(0)
	if segment.Total > 1 {
		firstOctet = 0x40
	}
	pdu := []byte{0, firstOctet, byte(len(digits)), 0x81}
	if strings.HasPrefix(sender, "+") {
		pdu[3] = 0x91
	}
	pdu = append(pdu, address...)
	pdu = append(pdu, 0, deliverDCS(segment.Encoding))
	pdu = append(pdu,
		swappedDecimal(receivedAt.Year()%100), swappedDecimal(int(receivedAt.Month())), swappedDecimal(receivedAt.Day()),
		swappedDecimal(receivedAt.Hour()), swappedDecimal(receivedAt.Minute()), swappedDecimal(receivedAt.Second()), 0,
	)
	userDataLength := len(segment.UserData)
	if segment.Encoding == smscodec.EncodingGSM7 {
		userDataLength = segment.UnitCount
		if segment.Total > 1 {
			userDataLength += 7
		}
	}
	pdu = append(pdu, byte(userDataLength))
	pdu = append(pdu, segment.UserData...)
	return StoredPDU{Index: index, Status: status, TPDULength: len(pdu) - 1, PDU: pdu}
}

func deliverDCS(encoding smscodec.Encoding) byte {
	if encoding == smscodec.EncodingUCS2 {
		return 0x08
	}
	return 0
}

func swappedDecimal(value int) byte { return byte((value%10)<<4 | value/10) }

func cloneStoredPDU(message StoredPDU) StoredPDU {
	message.PDU = append([]byte(nil), message.PDU...)
	return message
}

func countValue(values []int, target int) int {
	count := 0
	for _, value := range values {
		if value == target {
			count++
		}
	}
	return count
}
