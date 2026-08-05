package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/domain/sms"
)

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

func TestQueuedOutboundSMSIsFailedDuringRestartReconciliation(t *testing.T) {
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
	count, err := set.FailQueuedOutboundSMS(ctx, "SEND_OUTCOME_UNKNOWN_AFTER_RESTART", now.Add(time.Minute))
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
	if len(messages) != 1 || messages[0].ID != created.ID || messages[0].Status != sms.StatusFailed || messages[0].ErrorCode != "SEND_OUTCOME_UNKNOWN_AFTER_RESTART" {
		t.Fatalf("reconciled messages = %#v", messages)
	}
	if _, err := set.MarkOutboundSMSSent(ctx, created.ID, "late", now.Add(2*time.Minute)); !errors.Is(err, ErrSMSMessageNotFound) {
		t.Fatalf("late completion error = %v", err)
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
