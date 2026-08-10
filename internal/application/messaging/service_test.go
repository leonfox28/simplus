package messaging

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/application/inventory"
	lineapp "github.com/leonfox28/simplus/internal/application/line"
	modemapp "github.com/leonfox28/simplus/internal/application/modem"
	"github.com/leonfox28/simplus/internal/domain/pagination"
	"github.com/leonfox28/simplus/internal/domain/sms"
	"github.com/leonfox28/simplus/internal/smscodec"
	sqlitestore "github.com/leonfox28/simplus/internal/storage/sqlite"
)

type senderFunc func(context.Context, SendSMSCommand) (SendSMSResult, error)
type messagingTransportAvailability bool

const (
	testManagedLineID1 = "line_AQEBAQEBAQEBAQEBAQEBAQ"
	testManagedLineID2 = "line_AgICAgICAgICAgICAgICAg"
)

type managedTestLineSource struct{ source LineSource }

func (source managedTestLineSource) Topology(ctx context.Context) (inventory.Topology, error) {
	topology, err := source.source.Topology(ctx)
	if err != nil {
		return inventory.Topology{}, err
	}
	for index := range topology.Lines {
		switch topology.Lines[index].ID {
		case "simulator-line-1":
			topology.Lines[index].RuntimeLineID = topology.Lines[index].ID
			topology.Lines[index].ID = testManagedLineID1
		case "simulator-line-2":
			topology.Lines[index].RuntimeLineID = topology.Lines[index].ID
			topology.Lines[index].ID = testManagedLineID2
		}
	}
	return topology, nil
}

func managedTestLines(source LineSource) LineSource { return managedTestLineSource{source: source} }

func (availability messagingTransportAvailability) Available(context.Context, string) bool {
	return bool(availability)
}

func (send senderFunc) SendSMS(ctx context.Context, command SendSMSCommand) (SendSMSResult, error) {
	return send(ctx, command)
}

type sendErrorSMSClient struct {
	agentapi.SMSClientAPI
	err error
}

func (client sendErrorSMSClient) SendSMS(context.Context, agentapi.SMSSendRequest) (agentapi.SMSSendResponse, error) {
	return agentapi.SMSSendResponse{}, client.err
}

type failingInboundRepository struct {
	*sqlitestore.Set
}

func (repository failingInboundRepository) CreateInboundSMS(context.Context, sms.Message) (sms.Message, bool, error) {
	return sms.Message{}, false, errors.New("injected inbound persistence failure")
}

type failOnceInbox struct {
	Inbox
	failed bool
}

func (inbox *failOnceInbox) AcknowledgeSMS(ctx context.Context, target InboxTarget, messageID, operationID string) error {
	if !inbox.failed {
		inbox.failed = true
		return errors.New("injected acknowledge failure")
	}
	return inbox.Inbox.AcknowledgeSMS(ctx, target, messageID, operationID)
}

func newAgentGatewayForTest(t *testing.T, messages ...agentapi.SMSStoredMessage) (*AgentSMSGateway, *agentapi.SimulatorSMSBackend) {
	t.Helper()
	const instanceID = "01234567-89ab-cdef-0123-456789abcdef"
	backend := agentapi.NewSimulatorSMSBackend(messages...)
	client, err := agentapi.NewLocalSMSClient(instanceID, backend)
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := NewAgentSMSGateway(client, instanceID)
	if err != nil {
		t.Fatal(err)
	}
	return gateway, backend
}

func newTestService(t *testing.T, sender Sender) (*Service, *sqlitestore.Set) {
	t.Helper()
	ctx := context.Background()
	stores, err := sqlitestore.OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(ctx, stores, managedTestLines(inventory.NewSimulator()), sender)
	if err != nil {
		stores.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stores.Close() })
	return service, stores
}

func TestRecipientHistoryReadTokenValidationAndIdempotency(t *testing.T) {
	service, stores := newTestService(t, nil)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	message, replayed, err := stores.CreateInboundSMS(ctx, sms.Message{
		ID: "msg_service_unread000001", OperationID: "operation-service-unread01", Direction: sms.DirectionInbound,
		LineID: testManagedLineID1, RemoteAddress: "+447700900123", Body: "synthetic inbound",
		Status: sms.StatusReceived, ProviderMessageID: "provider-service-unread-1", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || replayed {
		t.Fatalf("create inbound=%#v replayed=%v error=%v", message, replayed, err)
	}
	page, err := service.ListPage(ctx, PageRequest{Limit: 20, RemoteAddress: message.RemoteAddress})
	if err != nil || len(page.Messages) != 1 || page.ReadThroughToken == "" {
		t.Fatalf("recipient page=%#v error=%v", page, err)
	}
	if _, err := service.ListPage(ctx, PageRequest{Limit: 20, LineID: testManagedLineID1}); !errors.Is(err, ErrRequestInvalid) {
		t.Fatalf("line-only filter error=%v", err)
	}
	for _, token := range []string{"", "***", "AA", "_" + page.ReadThroughToken, page.ReadThroughToken + "="} {
		if _, err := service.MarkConversationRead(ctx, message.RemoteAddress, token); !errors.Is(err, ErrRequestInvalid) {
			t.Fatalf("invalid token %q error=%v", token, err)
		}
	}
	changed, err := service.MarkConversationRead(ctx, message.RemoteAddress, page.ReadThroughToken)
	if err != nil || !changed {
		t.Fatalf("mark read changed=%v error=%v", changed, err)
	}
	changed, err = service.MarkConversationRead(ctx, message.RemoteAddress, page.ReadThroughToken)
	if err != nil || changed {
		t.Fatalf("repeat mark read changed=%v error=%v", changed, err)
	}
	conversations, err := service.ListConversationPage(ctx, 20, "")
	if err != nil || conversations.TotalCount != 1 || len(conversations.Conversations) != 1 || conversations.Conversations[0].UnreadCount != 0 {
		t.Fatalf("conversations=%#v error=%v", conversations, err)
	}
}

func TestSMSServiceEmitsSequenceCursorAcceptsLegacyAndContinuesAfterBoundaryDeletion(t *testing.T) {
	service, stores := newTestService(t, nil)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	for index := 0; index < 3; index++ {
		if _, _, err := stores.CreateOutboundSMS(ctx, sms.Message{
			ID: fmt.Sprintf("msg_service_page_%08d", index), OperationID: fmt.Sprintf("operation-service-page-%04d", index),
			Direction: sms.DirectionOutbound, LineID: testManagedLineID1, RemoteAddress: fmt.Sprintf("+1202555012%d", index),
			Body: "synthetic", Status: sms.StatusQueued, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := service.ListPage(ctx, PageRequest{Limit: 1})
	if err != nil || len(first.Messages) != 1 || first.NextCursor == "" {
		t.Fatalf("first page=%#v error=%v", first, err)
	}
	if _, err := pagination.Decode(first.NextCursor); !errors.Is(err, pagination.ErrCursorInvalid) {
		t.Fatalf("Calls v1 decoder accepted emitted SMS cursor: %v", err)
	}
	decoded, err := pagination.DecodeSMS(first.NextCursor)
	if err != nil || decoded.RecordSequence <= 0 || decoded.ID != first.Messages[0].ID {
		t.Fatalf("decoded SMS cursor=%#v error=%v", decoded, err)
	}
	legacyCursor, err := pagination.Encode(pagination.Cursor{CreatedAt: first.Messages[0].CreatedAt, ID: first.Messages[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	legacyPage, err := service.ListPage(ctx, PageRequest{Limit: 1, Cursor: legacyCursor})
	if err != nil || len(legacyPage.Messages) != 1 || legacyPage.Messages[0].ID == first.Messages[0].ID {
		t.Fatalf("legacy page=%#v error=%v", legacyPage, err)
	}
	if err := stores.DeleteSMS(ctx, first.Messages[0].ID); err != nil {
		t.Fatal(err)
	}
	afterDeletion, err := service.ListPage(ctx, PageRequest{Limit: 1, Cursor: first.NextCursor})
	if err != nil || len(afterDeletion.Messages) != 1 || afterDeletion.Messages[0].ID != legacyPage.Messages[0].ID {
		t.Fatalf("page after boundary deletion=%#v error=%v", afterDeletion, err)
	}
	if _, err := service.ListPage(ctx, PageRequest{Limit: 1, Cursor: legacyCursor}); !errors.Is(err, pagination.ErrCursorInvalid) {
		t.Fatalf("deleted legacy boundary error=%v", err)
	}

	conversationFirst, err := service.ListConversationPage(ctx, 1, "")
	if err != nil || len(conversationFirst.Conversations) != 1 || conversationFirst.NextCursor == "" {
		t.Fatalf("conversation first page=%#v error=%v", conversationFirst, err)
	}
	conversationBoundary := conversationFirst.Conversations[0].LastMessage
	legacyConversationCursor, err := pagination.Encode(pagination.Cursor{CreatedAt: conversationBoundary.CreatedAt, ID: conversationBoundary.ID})
	if err != nil {
		t.Fatal(err)
	}
	legacyConversationTail, err := service.ListConversationPage(ctx, 1, legacyConversationCursor)
	if err != nil || len(legacyConversationTail.Conversations) != 1 {
		t.Fatalf("legacy conversation tail=%#v error=%v", legacyConversationTail, err)
	}
	if err := stores.DeleteSMS(ctx, conversationBoundary.ID); err != nil {
		t.Fatal(err)
	}
	conversationAfterDeletion, err := service.ListConversationPage(ctx, 1, conversationFirst.NextCursor)
	if err != nil || len(conversationAfterDeletion.Conversations) != 1 ||
		conversationAfterDeletion.Conversations[0].RemoteAddress != legacyConversationTail.Conversations[0].RemoteAddress {
		t.Fatalf("conversation after boundary deletion=%#v error=%v", conversationAfterDeletion, err)
	}
}

func TestSendPersistsBeforeDispatchAndReplaysByOperationID(t *testing.T) {
	var calls int
	var stores *sqlitestore.Set
	service, stores := newTestService(t, senderFunc(func(ctx context.Context, command SendSMSCommand) (SendSMSResult, error) {
		calls++
		decoded, err := smscodec.Decode(command.Segments)
		if err != nil || decoded != command.Body {
			t.Fatalf("encoded segments did not round trip: decoded=%q error=%v", decoded, err)
		}
		messages, err := stores.ListSMS(ctx, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(messages) != 1 || messages[0].Status != sms.StatusQueued || messages[0].ID != command.MessageID {
			t.Fatalf("messages visible before dispatch = %#v", messages)
		}
		return SendSMSResult{ProviderMessageID: "provider-1"}, nil
	}))
	request := SendRequest{
		OperationID: "operation-0123456789abcdef", LineID: testManagedLineID1,
		Destination: "+8613800138000", Body: "hello 模拟器",
	}
	result, err := service.Send(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Replayed || result.Message.Status != sms.StatusSent || result.Message.ProviderMessageID != "provider-1" {
		t.Fatalf("send result = %#v", result)
	}
	replayed, err := service.Send(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Replayed || replayed.Message.ID != result.Message.ID || calls != 1 {
		t.Fatalf("replay = %#v, calls = %d", replayed, calls)
	}
	request.Destination = "+8613900139000"
	if _, err := service.Send(context.Background(), request); !errors.Is(err, sms.ErrOperationConflict) {
		t.Fatalf("conflicting replay error = %v", err)
	}
}

func TestPersistentLineResolvesSimulatorTransportWithoutLeakingRuntimeIdentity(t *testing.T) {
	ctx := context.Background()
	stores, err := sqlitestore.OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()
	rawInventory := inventory.NewSimulator()
	modems, err := modemapp.New(stores, rawInventory)
	if err != nil {
		t.Fatal(err)
	}
	modemCandidates, err := modems.Candidates(ctx)
	if err != nil || len(modemCandidates) < 1 {
		t.Fatalf("modem candidates=%#v error=%v", modemCandidates, err)
	}
	if _, err := modems.Add(ctx, modemCandidates[0].CandidateID); err != nil {
		t.Fatal(err)
	}
	lines, err := lineapp.New(stores, rawInventory)
	if err != nil {
		t.Fatal(err)
	}
	lineCandidates, err := lines.Candidates(ctx)
	if err != nil || len(lineCandidates) != 1 {
		t.Fatalf("line candidates=%#v error=%v", lineCandidates, err)
	}
	created, err := lines.Add(ctx, lineCandidates[0].CandidateID, "Simulator primary")
	if err != nil {
		t.Fatal(err)
	}
	var dispatched SendSMSCommand
	service, err := NewService(ctx, stores, lines, senderFunc(func(_ context.Context, command SendSMSCommand) (SendSMSResult, error) {
		dispatched = command
		return SendSMSResult{ProviderMessageID: "provider-stable-line"}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Send(ctx, SendRequest{
		OperationID: "operation-stable-line-0001", LineID: created.ID,
		Destination: "+8613800138000", Body: "stable Line",
	}); err != nil {
		t.Fatal(err)
	}
	if dispatched.LineID != created.ID || dispatched.PhysicalDeviceID != "simulator-device-1" ||
		dispatched.ModemFunctionID != "simulator-function-1" || dispatched.LineID == "simulator-line-1" {
		t.Fatalf("dispatched=%#v", dispatched)
	}
}

func TestSendSerializesCommandsPerModem(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{}, 2)
	var mu sync.Mutex
	active := 0
	maximum := 0
	service, _ := newTestService(t, senderFunc(func(ctx context.Context, command SendSMSCommand) (SendSMSResult, error) {
		mu.Lock()
		active++
		if active > maximum {
			maximum = active
		}
		mu.Unlock()
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return SendSMSResult{}, ctx.Err()
		}
		mu.Lock()
		active--
		mu.Unlock()
		return SendSMSResult{ProviderMessageID: "provider-" + command.OperationID}, nil
	}))

	results := make(chan error, 2)
	for index, destination := range []string{"13800138000", "13900139000"} {
		index := index
		destination := destination
		go func() {
			_, err := service.Send(context.Background(), SendRequest{
				OperationID: []string{"operation-0123456789abcdef", "operation-fedcba9876543210"}[index],
				LineID:      testManagedLineID1, Destination: destination, Body: "serialized",
			})
			results <- err
		}()
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first send did not enter transport")
	}
	select {
	case <-entered:
		t.Fatal("second send entered the same modem transport concurrently")
	case <-time.After(30 * time.Millisecond):
	}
	release <- struct{}{}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("second send did not enter after the first completed")
	}
	release <- struct{}{}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if maximum != 1 {
		t.Fatalf("maximum concurrent transport calls = %d", maximum)
	}
}

func TestSendAllowsIndependentModemsToProgressConcurrently(t *testing.T) {
	entered := make(chan string, 2)
	release := make(chan struct{}, 2)
	service, _ := newTestService(t, senderFunc(func(ctx context.Context, command SendSMSCommand) (SendSMSResult, error) {
		entered <- command.ModemFunctionID
		select {
		case <-release:
		case <-ctx.Done():
			return SendSMSResult{}, ctx.Err()
		}
		return SendSMSResult{ProviderMessageID: "provider-" + command.OperationID}, nil
	}))
	service.lines = managedTestLines(inventory.NewMultiSimulator())

	results := make(chan error, 2)
	for index, lineID := range []string{testManagedLineID1, testManagedLineID2} {
		index, lineID := index, lineID
		go func() {
			_, err := service.Send(context.Background(), SendRequest{
				OperationID: []string{"operation-parallel-000001", "operation-parallel-000002"}[index],
				LineID:      lineID, Destination: "13800138000", Body: "parallel",
			})
			results <- err
		}()
	}
	seen := map[string]bool{}
	for range 2 {
		select {
		case functionID := <-entered:
			seen[functionID] = true
		case <-time.After(time.Second):
			t.Fatal("independent modem send was blocked by another modem")
		}
	}
	if !seen["simulator-function-1"] || !seen["simulator-function-2"] {
		t.Fatalf("entered modem functions = %#v", seen)
	}
	release <- struct{}{}
	release <- struct{}{}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
}

func TestHostVoWiFiSMSRequiresOnlineTransport(t *testing.T) {
	calls := 0
	service, _ := newTestService(t, senderFunc(func(_ context.Context, command SendSMSCommand) (SendSMSResult, error) {
		calls++
		return SendSMSResult{ProviderMessageID: "provider-" + command.OperationID}, nil
	}))
	request := SendRequest{OperationID: "operation-vowifi-sms-001", LineID: testManagedLineID1, Destination: "13800138000", Body: "VoWiFi"}
	service.UseHostVoWiFiTransport(messagingTransportAvailability(false))
	if _, err := service.Send(context.Background(), request); !errors.Is(err, ErrLineUnavailable) {
		t.Fatalf("offline error=%v", err)
	}
	if calls != 0 {
		t.Fatalf("offline transport calls=%d", calls)
	}
	service.UseHostVoWiFiTransport(messagingTransportAvailability(true))
	request.OperationID = "operation-vowifi-sms-002"
	if result, err := service.Send(context.Background(), request); err != nil || result.Message.Status != sms.StatusSent {
		t.Fatalf("online=%#v err=%v", result, err)
	}
}

func TestTransportFailureBecomesDurableMessageState(t *testing.T) {
	service, _ := newTestService(t, senderFunc(func(context.Context, SendSMSCommand) (SendSMSResult, error) {
		return SendSMSResult{}, &TransportError{Code: "MODEM_REJECTED"}
	}))
	result, err := service.Send(context.Background(), SendRequest{
		OperationID: "operation-0123456789abcdef", LineID: testManagedLineID1,
		Destination: "13800138000", Body: "will fail",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Message.Status != sms.StatusFailed || result.Message.ErrorCode != "MODEM_REJECTED" {
		t.Fatalf("failure result = %#v", result)
	}
	messages, err := service.List(context.Background(), 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Status != sms.StatusFailed {
		t.Fatalf("durable messages = %#v", messages)
	}
}

func TestUnknownTransportOutcomeBecomesDurableUnconfirmedState(t *testing.T) {
	service, _ := newTestService(t, senderFunc(func(context.Context, SendSMSCommand) (SendSMSResult, error) {
		return SendSMSResult{}, &TransportError{Code: ErrorSendOutcomeUnknown}
	}))
	request := SendRequest{
		OperationID: "operation-unknown01234567", LineID: testManagedLineID1,
		Destination: "13800138000", Body: "may have been sent",
	}
	result, err := service.Send(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Message.Status != sms.StatusUnconfirmed || result.Message.ErrorCode != ErrorSendOutcomeUnknown {
		t.Fatalf("unconfirmed result = %#v", result)
	}
	replayed, err := service.Send(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Replayed || replayed.Message.ID != result.Message.ID || replayed.Message.Status != sms.StatusUnconfirmed {
		t.Fatalf("replayed unconfirmed result = %#v", replayed)
	}
}

func TestAgentGatewayPreservesUnknownSendOutcomeCode(t *testing.T) {
	const instanceID = "01234567-89ab-cdef-0123-456789abcdef"
	for _, transportErr := range []error{
		agentapi.ErrSMSOutcomeUnknown,
		&agentapi.ErrorResponse{Code: "SMS_SEND_OUTCOME_UNKNOWN", Detail: "do not resend"},
	} {
		gateway, err := NewAgentSMSGateway(sendErrorSMSClient{err: transportErr}, instanceID)
		if err != nil {
			t.Fatal(err)
		}
		segments, err := smscodec.Encode("hello")
		if err != nil {
			t.Fatal(err)
		}
		_, err = gateway.SendSMS(t.Context(), SendSMSCommand{
			OperationID: "operation-0123456789", PhysicalDeviceID: "usb-1-1",
			Destination: "10086", Body: "hello", Segments: segments,
		})
		var mapped *TransportError
		if !errors.As(err, &mapped) || mapped.Code != ErrorSendOutcomeUnknown {
			t.Fatalf("mapped transport error = %#v", err)
		}
	}
}

func TestServiceStartupDoesNotRedispatchInterruptedQueuedSMS(t *testing.T) {
	ctx := context.Background()
	stores, err := sqlitestore.OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()
	createdAt := time.Now().Add(-time.Minute).UTC()
	_, _, err = stores.CreateOutboundSMS(ctx, sms.Message{
		ID: "msg_0123456789abcdef012345", OperationID: "operation-0123456789abcdef",
		Direction: sms.DirectionOutbound, LineID: testManagedLineID1, RemoteAddress: "13800138000",
		Body: "interrupted", Status: sms.StatusQueued, CreatedAt: createdAt, UpdatedAt: createdAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	transportCalls := 0
	service, err := NewService(ctx, stores, managedTestLines(inventory.NewSimulator()), senderFunc(func(context.Context, SendSMSCommand) (SendSMSResult, error) {
		transportCalls++
		return SendSMSResult{ProviderMessageID: "unexpected"}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	messages, err := service.List(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if transportCalls != 0 || len(messages) != 1 || messages[0].Status != sms.StatusUnconfirmed || messages[0].ErrorCode != ErrorOutcomeUnknownAfterRestart {
		t.Fatalf("transport calls = %d, messages = %#v", transportCalls, messages)
	}
}

func TestSendRejectsInvalidOrUnavailableInputsWithoutPersistence(t *testing.T) {
	service, _ := newTestService(t, senderFunc(func(context.Context, SendSMSCommand) (SendSMSResult, error) {
		return SendSMSResult{ProviderMessageID: "unused"}, nil
	}))
	for name, test := range map[string]struct {
		request  SendRequest
		expected error
	}{
		"invalid destination": {
			request:  SendRequest{OperationID: "operation-0123456789abcdef", LineID: testManagedLineID1, Destination: "not-a-number", Body: "body"},
			expected: ErrRequestInvalid,
		},
		"unknown line": {
			request:  SendRequest{OperationID: "operation-0123456789abcdef", LineID: testManagedLineID2, Destination: "13800138000", Body: "body"},
			expected: ErrLineNotFound,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := service.Send(context.Background(), test.request); !errors.Is(err, test.expected) {
				t.Fatalf("Send() error = %v", err)
			}
		})
	}
	messages, err := service.List(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 0 {
		t.Fatalf("rejected messages were persisted: %#v", messages)
	}
}

func TestSendWithoutRuntimeTransportFailsBeforePersistence(t *testing.T) {
	service, _ := newTestService(t, nil)
	_, err := service.Send(context.Background(), SendRequest{
		OperationID: "operation-0123456789abcdef", LineID: testManagedLineID1,
		Destination: "13800138000", Body: "must not queue",
	})
	if !errors.Is(err, ErrTransportUnavailable) {
		t.Fatalf("Send() error = %v", err)
	}
	messages, err := service.List(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 0 {
		t.Fatalf("message without transport was persisted: %#v", messages)
	}
}

func TestInboundSyncPersistsBeforeAcknowledgeAndDeduplicatesRestart(t *testing.T) {
	ctx := context.Background()
	stores, err := sqlitestore.OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()
	inbound := agentapi.SMSStoredMessage{
		MessageID: "inbound-source-1", DeviceID: "simulator-device-1", Sender: "Simplus",
		Body: "persist before acknowledge", ReceivedAt: time.Date(2026, 8, 3, 14, 0, 0, 0, time.UTC),
	}
	gateway, backend := newAgentGatewayForTest(t, inbound)
	inbox := &failOnceInbox{Inbox: gateway}
	service, err := NewService(ctx, stores, managedTestLines(inventory.NewSimulator()), gateway, inbox)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.SyncInbound(ctx)
	if !errors.Is(err, ErrInboundSync) || first.Persisted != 1 || first.Acknowledged != 0 {
		t.Fatalf("first sync = %#v, error = %v", first, err)
	}
	messages, err := stores.ListSMS(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Status != sms.StatusReceived || messages[0].ProviderMessageID != inbound.MessageID {
		t.Fatalf("persisted inbound messages = %#v", messages)
	}
	second, err := service.SyncInbound(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second.AlreadyKnown != 1 || second.Acknowledged != 1 {
		t.Fatalf("second sync = %#v", second)
	}
	remaining, err := backend.ListSMS(ctx, inbound.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("Agent inbox after acknowledge = %#v", remaining)
	}

	restartedGateway, restartedBackend := newAgentGatewayForTest(t, inbound)
	restarted, err := NewService(ctx, stores, managedTestLines(inventory.NewSimulator()), restartedGateway, restartedGateway)
	if err != nil {
		t.Fatal(err)
	}
	restartResult, err := restarted.SyncInbound(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if restartResult.Persisted != 0 || restartResult.AlreadyKnown != 1 || restartResult.Acknowledged != 1 {
		t.Fatalf("restart sync = %#v", restartResult)
	}
	remaining, err = restartedBackend.ListSMS(ctx, inbound.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("restarted Agent inbox after acknowledge = %#v", remaining)
	}
	messages, err = stores.ListSMS(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("restart duplicated inbound message: %#v", messages)
	}
}

func TestHistoryReadDoesNotCompeteWithBackgroundInboundSync(t *testing.T) {
	ctx := context.Background()
	stores, err := sqlitestore.OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()
	inbound := agentapi.SMSStoredMessage{
		MessageID: "inbound-source-background", DeviceID: "simulator-device-1", Sender: "10086",
		Body: "background owns synchronization", ReceivedAt: time.Date(2026, 8, 7, 15, 0, 0, 0, time.UTC),
	}
	gateway, _ := newAgentGatewayForTest(t, inbound)
	service, err := NewService(ctx, stores, managedTestLines(inventory.NewSimulator()), gateway, gateway)
	if err != nil {
		t.Fatal(err)
	}
	page, err := service.ListPage(ctx, PageRequest{Limit: 20})
	if err != nil || len(page.Messages) != 0 {
		t.Fatalf("history before background sync=%#v error=%v", page, err)
	}
	result, err := service.SyncInbound(ctx)
	if err != nil || result.Persisted != 1 {
		t.Fatalf("background sync result=%#v error=%v", result, err)
	}
}

func TestInboundSyncDoesNotAcknowledgePersistenceFailure(t *testing.T) {
	ctx := context.Background()
	stores, err := sqlitestore.OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()
	inbound := agentapi.SMSStoredMessage{
		MessageID: "inbound-source-1", DeviceID: "simulator-device-1", Sender: "10086",
		Body: "must remain in Agent", ReceivedAt: time.Date(2026, 8, 3, 14, 0, 0, 0, time.UTC),
	}
	gateway, backend := newAgentGatewayForTest(t, inbound)
	service, err := NewService(ctx, failingInboundRepository{Set: stores}, managedTestLines(inventory.NewSimulator()), gateway, gateway)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SyncInbound(ctx); !errors.Is(err, ErrPersistence) {
		t.Fatalf("SyncInbound() error = %v", err)
	}
	remaining, err := backend.ListSMS(ctx, inbound.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].MessageID != inbound.MessageID {
		t.Fatalf("Agent inbox was acknowledged after persistence failure: %#v", remaining)
	}
}
