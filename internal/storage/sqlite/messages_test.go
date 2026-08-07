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
