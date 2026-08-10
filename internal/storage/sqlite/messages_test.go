package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/domain/pagination"
	"github.com/leonfox28/simplus/internal/domain/sms"
)

func TestSMSKeysetPaginationIsStableAcrossTiesFiltersConcurrentInsertAndReopen(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "db")
	set, err := OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2026, 8, 7, 12, 0, 0, 123_000_000, time.UTC)
	lineID := "line_AQEBAQEBAQEBAQEBAQEBAQ"
	remote := "+8613800138000"
	items := []struct {
		id        string
		operation string
		line      string
		remote    string
	}{
		{"msg_page_00000000000000", "operation-page-000000000", "line_other_00000000000000", "+8613900139000"},
		{"msg_page_00000000000001", "operation-page-000000001", lineID, remote},
		{"msg_page_00000000000002", "operation-page-000000002", lineID, remote},
		{"msg_page_00000000000003", "operation-page-000000003", lineID, remote},
	}
	for _, item := range items {
		if _, _, err := set.CreateOutboundSMS(ctx, sms.Message{
			ID: item.id, OperationID: item.operation, Direction: sms.DirectionOutbound,
			LineID: item.line, RemoteAddress: item.remote, Body: item.id,
			Status: sms.StatusQueued, CreatedAt: createdAt, UpdatedAt: createdAt,
		}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := set.ListSMSPage(ctx, pagination.Request{Limit: 2}, "", "")
	if err != nil || len(first.Items) != 2 || first.Items[0].ID != items[3].id || first.Items[1].ID != items[2].id || first.Next == nil {
		t.Fatalf("first page=%#v error=%v", first, err)
	}
	conversation, err := set.ListSMSPage(ctx, pagination.Request{Limit: 2}, lineID, remote)
	if err != nil || len(conversation.Items) != 2 || conversation.Next == nil || conversation.Items[0].ID != items[3].id {
		t.Fatalf("conversation page=%#v error=%v", conversation, err)
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}
	set, err = OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	if _, _, err := set.CreateOutboundSMS(ctx, sms.Message{
		ID: "msg_page_00000000000004", OperationID: "operation-page-000000004", Direction: sms.DirectionOutbound,
		LineID: lineID, RemoteAddress: remote, Body: "concurrent", Status: sms.StatusQueued,
		CreatedAt: createdAt.Add(time.Minute), UpdatedAt: createdAt.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	second, err := set.ListSMSPage(ctx, pagination.Request{Limit: 2, After: first.Next}, "", "")
	if err != nil || len(second.Items) != 2 || second.Items[0].ID != items[1].id || second.Items[1].ID != items[0].id || second.Next != nil {
		t.Fatalf("second page=%#v error=%v", second, err)
	}
	conversationTail, err := set.ListSMSPage(ctx, pagination.Request{Limit: 2, After: conversation.Next}, lineID, remote)
	if err != nil || len(conversationTail.Items) != 1 || conversationTail.Items[0].ID != items[1].id || conversationTail.Next != nil {
		t.Fatalf("conversation tail=%#v error=%v", conversationTail, err)
	}
}

func TestSMSRecipientConversationsUnreadWatermarksAndRemoteOnlyHistory(t *testing.T) {
	ctx := context.Background()
	set, err := OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	now := time.Date(2026, 8, 10, 8, 0, 0, 123_000_000, time.UTC)
	remote := "+447700900123"
	createInbound := func(id, operationID, providerID, lineID, address, body string, createdAt time.Time) sms.Message {
		t.Helper()
		message, replayed, err := set.CreateInboundSMS(ctx, sms.Message{
			ID: id, OperationID: operationID, Direction: sms.DirectionInbound,
			LineID: lineID, RemoteAddress: address, Body: body, Status: sms.StatusReceived,
			ProviderMessageID: providerID, CreatedAt: createdAt, UpdatedAt: createdAt,
		})
		if err != nil || replayed {
			t.Fatalf("create inbound=%#v replayed=%v error=%v", message, replayed, err)
		}
		return message
	}
	first := createInbound("msg_unread_000000000001", "operation-unread-00000001", "provider-unread-1",
		"line_AQEBAQEBAQEBAQEBAQEBAQ", remote, "first", now)
	if _, replayed, err := set.CreateInboundSMS(ctx, sms.Message{
		ID: "msg_unread_replay000001", OperationID: "operation-unread-replay01", Direction: sms.DirectionInbound,
		LineID: first.LineID, RemoteAddress: remote, Body: first.Body, Status: sms.StatusReceived,
		ProviderMessageID: first.ProviderMessageID, CreatedAt: now, UpdatedAt: now,
	}); err != nil || !replayed {
		t.Fatalf("inbound replay replayed=%v error=%v", replayed, err)
	}
	if _, _, err := set.CreateOutboundSMS(ctx, sms.Message{
		ID: "msg_outbound_0000000001", OperationID: "operation-outbound-000001", Direction: sms.DirectionOutbound,
		LineID: "line_AgICAgICAgICAgICAgICAg", RemoteAddress: remote, Body: "reply", Status: sms.StatusQueued,
		CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	createInbound("msg_unread_000000000002", "operation-unread-00000002", "provider-unread-2",
		"line_AgICAgICAgICAgICAgICAg", "447700900123", "different exact address", now.Add(2*time.Second))
	if _, err := set.MarkOutboundSMSFailed(ctx, "msg_outbound_0000000001", "", "SMS_SYNTHETIC_FAILURE", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	page, boundary, err := set.ListSMSPageWithUnread(ctx, pagination.Request{Limit: 10}, "", remote)
	if err != nil || len(page.Items) != 2 || boundary == nil || boundary.MessageID != first.ID {
		t.Fatalf("remote page=%#v boundary=%#v error=%v", page, boundary, err)
	}
	if _, err := set.MarkSMSConversationRead(ctx, "447700900123", *boundary); !errors.Is(err, sms.ErrReadBoundaryInvalid) {
		t.Fatalf("mismatched remote boundary error=%v", err)
	}
	if _, err := set.MarkSMSConversationRead(ctx, remote, sms.UnreadBoundary{UnreadID: boundary.UnreadID + 1, MessageID: boundary.MessageID}); !errors.Is(err, sms.ErrReadBoundaryInvalid) {
		t.Fatalf("mismatched unread id error=%v", err)
	}
	if _, err := set.MarkSMSConversationRead(ctx, remote, sms.UnreadBoundary{UnreadID: boundary.UnreadID, MessageID: "msg_outbound_0000000001"}); !errors.Is(err, sms.ErrReadBoundaryInvalid) {
		t.Fatalf("outbound boundary error=%v", err)
	}
	conversations, err := set.ListSMSConversationPage(ctx, pagination.Request{Limit: 10})
	if err != nil || len(conversations.Items) != 2 {
		t.Fatalf("conversations=%#v error=%v", conversations, err)
	}
	conversationHead, err := set.ListSMSConversationPage(ctx, pagination.Request{Limit: 1})
	if err != nil || len(conversationHead.Items) != 1 || conversationHead.Next == nil || conversationHead.Items[0].RemoteAddress != "447700900123" {
		t.Fatalf("conversation head=%#v error=%v", conversationHead, err)
	}
	conversationTail, err := set.ListSMSConversationPage(ctx, pagination.Request{Limit: 1, After: conversationHead.Next})
	if err != nil || len(conversationTail.Items) != 1 || conversationTail.Next != nil || conversationTail.Items[0].RemoteAddress != remote {
		t.Fatalf("conversation tail=%#v error=%v", conversationTail, err)
	}
	var merged sms.ConversationSummary
	for _, item := range conversations.Items {
		if item.RemoteAddress == remote {
			merged = item
		}
	}
	if merged.UnreadCount != 1 || merged.LastMessage.Body != "reply" || merged.LastOutboundLineID != "line_AgICAgICAgICAgICAgICAg" {
		t.Fatalf("merged conversation=%#v", merged)
	}

	second := createInbound("msg_unread_000000000000", "operation-unread-00000003", "provider-unread-3",
		"line_AgICAgICAgICAgICAgICAg", remote, "later same millisecond", now)
	changed, err := set.MarkSMSConversationRead(ctx, remote, *boundary)
	if err != nil || !changed {
		t.Fatalf("mark first boundary changed=%v error=%v", changed, err)
	}
	changed, err = set.MarkSMSConversationRead(ctx, remote, *boundary)
	if err != nil || changed {
		t.Fatalf("repeat old boundary changed=%v error=%v", changed, err)
	}
	conversations, err = set.ListSMSConversationPage(ctx, pagination.Request{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range conversations.Items {
		if item.RemoteAddress == remote && item.UnreadCount != 1 {
			t.Fatalf("newer unread count after old watermark=%d", item.UnreadCount)
		}
	}
	_, latestBoundary, err := set.ListSMSPageWithUnread(ctx, pagination.Request{Limit: 10}, "", remote)
	if err != nil || latestBoundary == nil || latestBoundary.MessageID != second.ID || latestBoundary.UnreadID <= boundary.UnreadID {
		t.Fatalf("latest boundary=%#v old=%#v error=%v", latestBoundary, boundary, err)
	}
	if err := set.DeleteSMS(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := set.MarkSMSConversationRead(ctx, remote, *latestBoundary); !errors.Is(err, sms.ErrMessageNotFound) {
		t.Fatalf("deleted boundary error=%v", err)
	}
	conversations, err = set.ListSMSConversationPage(ctx, pagination.Request{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range conversations.Items {
		if item.RemoteAddress == remote && item.UnreadCount != 0 {
			t.Fatalf("deleted unread marker count=%d", item.UnreadCount)
		}
	}
	if err := set.DeleteSMS(ctx, "msg_outbound_0000000001"); err != nil {
		t.Fatal(err)
	}
	if err := set.DeleteSMS(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	conversations, err = set.ListSMSConversationPage(ctx, pagination.Request{Limit: 10})
	if err != nil || len(conversations.Items) != 1 || conversations.Items[0].RemoteAddress != "447700900123" {
		t.Fatalf("conversations after deleting last recipient message=%#v error=%v", conversations, err)
	}
}

func TestCreateInboundSMSRollsBackWhenUnreadMarkerCannotBeCreated(t *testing.T) {
	ctx := context.Background()
	set, err := OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	if _, err := set.Messages.ExecContext(ctx, `DROP TABLE sms_message_unread`); err != nil {
		t.Fatal(err)
	}
	messageID := "msg_atomic_unread_000001"
	_, _, err = set.CreateInboundSMS(ctx, sms.Message{
		ID: messageID, OperationID: "operation-atomic-unread-01", Direction: sms.DirectionInbound,
		LineID: "line_AQEBAQEBAQEBAQEBAQEBAQ", RemoteAddress: "+447700900123", Body: "atomic inbound",
		Status: sms.StatusReceived, ProviderMessageID: "provider-atomic-unread-1",
		CreatedAt: time.Date(2026, 8, 10, 8, 30, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 8, 10, 8, 30, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("CreateInboundSMS succeeded without the unread ledger")
	}
	var count int
	if err := set.Messages.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_messages WHERE message_id = ?`, messageID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rolled-back inbound count=%d error=%v", count, err)
	}
}

func TestOutboundSMSPersistsReplaysAndCompletes(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "db")
	set, err := OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 3, 10, 11, 12, 123_000_000, time.UTC)
	request := sms.Message{
		ID: "msg_0123456789abcdef012345", OperationID: "operation-0123456789abcdef",
		Direction: sms.DirectionOutbound, LineID: "simulator-line-1", RemoteAddress: "+8613800138000",
		Body: "第一条模拟短信", Status: sms.StatusQueued, CreatedAt: now, UpdatedAt: now,
	}
	created, replayed, err := set.CreateOutboundSMS(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if replayed || created.Status != sms.StatusQueued || !created.CreatedAt.Equal(now) {
		t.Fatalf("created = %#v, replayed = %v", created, replayed)
	}

	replay, replayed, err := set.CreateOutboundSMS(ctx, sms.Message{
		ID: "msg_different0123456789012", OperationID: request.OperationID,
		Direction: request.Direction, LineID: request.LineID, RemoteAddress: request.RemoteAddress,
		Body: request.Body, Status: sms.StatusQueued, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !replayed || replay.ID != created.ID {
		t.Fatalf("replay = %#v, replayed = %v", replay, replayed)
	}

	_, _, err = set.CreateOutboundSMS(ctx, sms.Message{
		ID: "msg_conflict01234567890123", OperationID: request.OperationID,
		Direction: request.Direction, LineID: request.LineID, RemoteAddress: "+8613900139000",
		Body: request.Body, Status: sms.StatusQueued, CreatedAt: now, UpdatedAt: now,
	})
	if !errors.Is(err, ErrSMSOperationConflict) {
		t.Fatalf("conflicting replay error = %v", err)
	}

	completedAt := now.Add(2 * time.Second)
	completed, err := set.MarkOutboundSMSSent(ctx, created.ID, "sim-provider-1", completedAt)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != sms.StatusSent || completed.ProviderMessageID != "sim-provider-1" || completed.SentAt == nil || !completed.SentAt.Equal(completedAt) {
		t.Fatalf("completed = %#v", completed)
	}
	if _, err := set.MarkOutboundSMSFailed(ctx, created.ID, "sim-provider-1", "IMS_SMS_REJECTED", completedAt.Add(time.Second)); !errors.Is(err, sms.ErrStateConflict) {
		t.Fatalf("sent-to-failed transition error = %v", err)
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}

	set, err = OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	messages, err := set.ListSMS(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].ID != created.ID || messages[0].Status != sms.StatusSent {
		t.Fatalf("persisted messages = %#v", messages)
	}
}

func TestQueuedOutboundSMSBecomesUnconfirmedDuringRestartReconciliation(t *testing.T) {
	ctx := context.Background()
	set, err := OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	created, _, err := set.CreateOutboundSMS(ctx, sms.Message{
		ID: "msg_0123456789abcdef012345", OperationID: "operation-0123456789abcdef",
		Direction: sms.DirectionOutbound, LineID: "simulator-line-1", RemoteAddress: "13800138000",
		Body: "queued", Status: sms.StatusQueued, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	count, err := set.MarkQueuedOutboundSMSUnconfirmed(ctx, "SEND_OUTCOME_UNKNOWN_AFTER_RESTART", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("reconciled count = %d", count)
	}
	messages, err := set.ListSMS(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].ID != created.ID || messages[0].Status != sms.StatusUnconfirmed || messages[0].ErrorCode != "SEND_OUTCOME_UNKNOWN_AFTER_RESTART" {
		t.Fatalf("reconciled messages = %#v", messages)
	}
	late, err := set.MarkOutboundSMSSent(ctx, created.ID, "late", now.Add(2*time.Minute))
	if err != nil || late.Status != sms.StatusSent || late.ProviderMessageID != "late" || late.ErrorCode != "" {
		t.Fatalf("late completion = %#v, error = %v", late, err)
	}
}

func TestOutboundSMSCanPersistUnconfirmedOutcome(t *testing.T) {
	ctx := context.Background()
	set, err := OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	now := time.Date(2026, 8, 3, 12, 30, 0, 0, time.UTC)
	created, _, err := set.CreateOutboundSMS(ctx, sms.Message{
		ID: "msg_0123456789abcdef012345", OperationID: "operation-0123456789abcdef",
		Direction: sms.DirectionOutbound, LineID: "simulator-line-1", RemoteAddress: "13800138000",
		Body: "unconfirmed", Status: sms.StatusQueued, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	unconfirmed, err := set.MarkOutboundSMSUnconfirmed(ctx, created.ID, "", "SMS_SEND_OUTCOME_UNKNOWN", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if unconfirmed.Status != sms.StatusUnconfirmed || unconfirmed.ErrorCode != "SMS_SEND_OUTCOME_UNKNOWN" || unconfirmed.ProviderMessageID != "" || unconfirmed.SentAt != nil {
		t.Fatalf("unconfirmed = %#v", unconfirmed)
	}
}

func TestOutboundSMSAcceptedProviderAdvancesToReportTimeout(t *testing.T) {
	ctx := context.Background()
	set, err := OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	now := time.Date(2026, 8, 5, 19, 0, 0, 0, time.UTC)
	created, _, err := set.CreateOutboundSMS(ctx, sms.Message{
		ID: "msg_timeout012345678901234", OperationID: "operation-timeout01234567",
		Direction: sms.DirectionOutbound, LineID: "simulator-line-1", RemoteAddress: "13800138000",
		Body: "await report", Status: sms.StatusQueued, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	const providerMessageID = "ims_provider_0123456789"
	accepted, err := set.MarkOutboundSMSUnconfirmed(ctx, created.ID, providerMessageID,
		"IMS_SMS_ACCEPTED_AWAITING_REPORT", now.Add(time.Second))
	if err != nil || accepted.ProviderMessageID != providerMessageID || accepted.ErrorCode != "IMS_SMS_ACCEPTED_AWAITING_REPORT" {
		t.Fatalf("accepted=%#v error=%v", accepted, err)
	}
	timedOut, err := set.MarkOutboundSMSUnconfirmed(ctx, created.ID, providerMessageID,
		"SMS_SEND_OUTCOME_UNKNOWN", now.Add(2*time.Minute))
	if err != nil || timedOut.Status != sms.StatusUnconfirmed || timedOut.ProviderMessageID != providerMessageID ||
		timedOut.ErrorCode != "SMS_SEND_OUTCOME_UNKNOWN" {
		t.Fatalf("timed out=%#v error=%v", timedOut, err)
	}
}

func TestInboundSMSDeduplicatesByLineAndAgentMessageID(t *testing.T) {
	ctx := context.Background()
	set, err := OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	receivedAt := time.Date(2026, 8, 3, 13, 0, 0, 0, time.UTC)
	message := sms.Message{
		ID: "msg_inbound012345678901234", OperationID: "inbound-operation-01234567",
		Direction: sms.DirectionInbound, LineID: "simulator-line-1", RemoteAddress: "10086",
		Body: "welcome", Status: sms.StatusReceived, ProviderMessageID: "agent-message-1",
		CreatedAt: receivedAt, UpdatedAt: receivedAt.Add(time.Second),
	}
	created, replayed, err := set.CreateInboundSMS(ctx, message)
	if err != nil {
		t.Fatal(err)
	}
	if replayed || created.Status != sms.StatusReceived || created.ProviderMessageID != "agent-message-1" {
		t.Fatalf("created = %#v, replayed = %v", created, replayed)
	}
	replay := message
	replay.ID = "msg_inbound987654321098765"
	replay.OperationID = "inbound-operation-76543210"
	replayedMessage, replayed, err := set.CreateInboundSMS(ctx, replay)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed || replayedMessage.ID != created.ID {
		t.Fatalf("replayed message = %#v, replayed = %v", replayedMessage, replayed)
	}
	conflict := replay
	conflict.Body = "different"
	if _, _, err := set.CreateInboundSMS(ctx, conflict); !errors.Is(err, sms.ErrSourceConflict) {
		t.Fatalf("source conflict error = %v", err)
	}
	messages, err := set.ListSMS(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Direction != sms.DirectionInbound {
		t.Fatalf("messages = %#v", messages)
	}
}

func TestInboundSMSFragmentsPersistReplayAssembleAndPrune(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "db")
	set, err := OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	receivedAt := time.Now().UTC().Truncate(time.Second)
	first := sms.InboundFragment{
		GroupID: "infrag_0123456789abcdef", SourceMessageID: "imsin_fragment_000001",
		LineID: "line-1", Sender: "+447700900123", Encoding: "gsm7", Reference: 42,
		Part: 1, Total: 2, UnitCount: 1, UserData: []byte{0x05, 0x00, 0x03, 42, 2, 1, 0x61}, ReceivedAt: receivedAt,
	}
	fragments, replayed, err := set.StoreInboundSMSFragment(ctx, first)
	if err != nil || replayed || len(fragments) != 1 {
		t.Fatalf("first fragments=%#v replayed=%v error=%v", fragments, replayed, err)
	}
	retransmission := first
	retransmission.SourceMessageID = "imsin_fragment_retry1"
	fragments, replayed, err = set.StoreInboundSMSFragment(ctx, retransmission)
	if err != nil || !replayed || len(fragments) != 1 {
		t.Fatalf("replayed fragments=%#v replayed=%v error=%v", fragments, replayed, err)
	}
	conflict := first
	conflict.UserData = []byte{0x05, 0x00, 0x03, 42, 2, 1, 0x62}
	if _, _, err := set.StoreInboundSMSFragment(ctx, conflict); !errors.Is(err, sms.ErrSourceConflict) {
		t.Fatalf("fragment conflict error=%v", err)
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}

	set, err = OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	second := first
	second.GroupID = "infrag_fedcba9876543210"
	second.SourceMessageID = "imsin_fragment_000002"
	second.Part = 2
	second.UserData = []byte{0x05, 0x00, 0x03, 42, 2, 2, 0x62}
	second.ReceivedAt = receivedAt.Add(time.Minute)
	fragments, replayed, err = set.StoreInboundSMSFragment(ctx, second)
	if err != nil || replayed || len(fragments) != 2 || fragments[0].Part != 1 || fragments[1].Part != 2 {
		t.Fatalf("reopened fragments=%#v replayed=%v error=%v", fragments, replayed, err)
	}
	pruned, err := set.PruneInboundSMSFragments(ctx, receivedAt.Add(2*time.Hour))
	if err != nil || pruned != 2 {
		t.Fatalf("pruned=%d error=%v", pruned, err)
	}
}
