package sqlite

import (
	"path/filepath"
	"testing"
	"time"

	domain "github.com/leonfox28/simplus/internal/domain/notification"
	"github.com/leonfox28/simplus/internal/domain/sms"
)

func queueChannel(t *testing.T, set *Set, id string) domain.Channel {
	t.Helper()
	c := domain.Channel{ID: id, Provider: "wecom", DeliveryMode: domain.DeliveryModeWebhook, DisplayName: "测试渠道", WebhookCiphertext: make([]byte, 40), WebhookHint: "qyapi.weixin.qq.com", Enabled: true, EventKinds: domain.EventKinds, LastDeliveryStatus: "never", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := set.UpsertNotificationChannel(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestConnectionCheckpointAndOutboxCommitTogetherAndReplayAfterRestart(t *testing.T) {
	ctx := t.Context()
	root := filepath.Join(t.TempDir(), "state")
	s, err := OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	c := queueChannel(t, s, "channel_AAAAAAAAAAAAAAAAAAAAAA")
	previous := domain.Cursor{Source: "vowifi"}
	next := domain.Cursor{Source: "vowifi", Instance: "instance-1", Sequence: 3}
	at := time.Now().UTC()
	states := []domain.Observation{
		{ObjectID: "line-A", DisplayName: "线路 A", Kind: "vowifi", Connected: true, ObservedAt: at},
		{ObjectID: "line-A", DisplayName: "线路 A", Kind: "vowifi", Connected: false, ObservedAt: at},
		{ObjectID: "line-A", DisplayName: "线路 A", Kind: "vowifi", Connected: true, ObservedAt: at},
		{ObjectID: "line-B", DisplayName: "线路 B", Kind: "vowifi", Connected: false, ObservedAt: at},
	}
	if _, err := s.DB.Exec(`CREATE TRIGGER fail_queue BEFORE INSERT ON notification_deliveries BEGIN SELECT RAISE(ABORT,'test'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.CommitConnectionObservations(ctx, previous, next, states); err == nil {
		t.Fatal("transaction unexpectedly committed")
	}
	got, err := s.ReadConnectionCursor(ctx, "vowifi")
	if err != nil || got != previous {
		t.Fatalf("cursor advanced: %+v %v", got, err)
	}
	var count int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM connection_checkpoints`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("checkpoint escaped rollback: %d %v", count, err)
	}
	if _, err := s.DB.Exec(`DROP TRIGGER fail_queue`); err != nil {
		t.Fatal(err)
	}
	if err := s.CommitConnectionObservations(ctx, previous, next, states); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.CommitConnectionObservations(ctx, previous, next, states); err == nil {
		t.Fatal("stale cursor replay accepted")
	}
	if err := s.CommitConnectionObservations(ctx, next, next, states[2:]); err != nil {
		t.Fatal(err)
	}
	counts, err := s.NotificationDeliveryCounts(ctx, c.ID)
	if err != nil || counts.Pending != 3 {
		t.Fatalf("pending=%+v %v", counts, err)
	}
	for _, kind := range []string{"vowifi.connected", "vowifi.disconnected", "vowifi.connected"} {
		items, err := s.ClaimNotificationDeliveries(ctx, at.Add(time.Minute), 20)
		if err != nil || len(items) != 1 || items[0].EventKind != kind {
			t.Fatalf("ordered items=%+v %v", items, err)
		}
		if err := s.FinishNotificationDelivery(ctx, items[0], "delivered", "", at); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDeliveryRetryLeaseIndependentStreamsAndCancellation(t *testing.T) {
	ctx := t.Context()
	s, err := OpenSet(ctx, filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := queueChannel(t, s, "channel_AAAAAAAAAAAAAAAAAAAAAA")
	at := time.Now()
	for _, event := range []domain.Event{
		{Key: "a1", Kind: "cellular.connected", ObjectID: "a", ConnectionKind: "cellular", Message: "online", ObservedAt: at},
		{Key: "a2", Kind: "cellular.disconnected", ObjectID: "a", ConnectionKind: "cellular", Message: "offline", ObservedAt: at},
		{Key: "b1", Kind: "cellular.connected", ObjectID: "b", ConnectionKind: "cellular", Message: "online", ObservedAt: at},
	} {
		if err := s.EnqueueNotification(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	items, err := s.ClaimNotificationDeliveries(ctx, at, 20)
	if err != nil || len(items) != 2 {
		t.Fatalf("items=%+v %v", items, err)
	}
	var slow domain.Delivery
	for _, item := range items {
		if item.ObjectID == "a" {
			slow = item
			err = s.FinishNotificationDelivery(ctx, item, "pending", "TEMPORARY", at.Add(time.Minute))
		} else {
			err = s.FinishNotificationDelivery(ctx, item, "delivered", "", at)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if items, err := s.ClaimNotificationDeliveries(ctx, at, 20); err != nil || len(items) != 0 {
		t.Fatalf("retry bypassed order: %+v %v", items, err)
	}
	items, err = s.ClaimNotificationDeliveries(ctx, at.Add(time.Minute), 20)
	if err != nil || len(items) != 1 || items[0].ID != slow.ID || items[0].Attempts != 2 {
		t.Fatalf("retry=%+v %v", items, err)
	}
	// A crash after dispatch cannot lose work. Its result may be delivered twice externally.
	if err := s.ResetInterruptedNotifications(ctx, at); err != nil {
		t.Fatal(err)
	}
	items, err = s.ClaimNotificationDeliveries(ctx, at, 20)
	if err != nil || len(items) != 1 || items[0].Attempts != 3 {
		t.Fatalf("recovery=%+v %v", items, err)
	}
	c.EventKinds = []string{"sms.received"}
	if err := s.UpsertNotificationChannel(ctx, c); err != nil {
		t.Fatal(err)
	}
	// A late completion from the cancelled subscription cannot resurrect it.
	if err := s.FinishNotificationDelivery(ctx, items[0], "pending", "TEMPORARY", at); err != nil {
		t.Fatal(err)
	}
	counts, err := s.NotificationDeliveryCounts(ctx, c.ID)
	if err != nil || counts.Pending != 0 {
		t.Fatalf("cancelled=%+v %v", counts, err)
	}
}

func TestInboundUnreadAndNotificationAreAtomic(t *testing.T) {
	ctx := t.Context()
	s, err := OpenSet(ctx, filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := queueChannel(t, s, "channel_AAAAAAAAAAAAAAAAAAAAAA")
	at := time.Now().UTC()
	m := sms.Message{ID: "msg_inbound_atomic", Direction: sms.DirectionInbound, LineID: "line_AAAAAAAAAAAAAAAAAAAAAA", RemoteAddress: "+12025550123", Body: "synthetic", OperationID: "inbound_atomic_operation", ProviderMessageID: "source_atomic", Status: sms.StatusReceived, CreatedAt: at, UpdatedAt: at}
	if _, err := s.DB.Exec(`CREATE TRIGGER fail_queue BEFORE INSERT ON notification_deliveries BEGIN SELECT RAISE(ABORT,'test'); END`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateInboundSMS(ctx, m); err == nil {
		t.Fatal("inbound committed without its notification")
	}
	var count int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM sms_messages`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("message escaped rollback %d %v", count, err)
	}
	if _, err := s.DB.Exec(`DROP TRIGGER fail_queue`); err != nil {
		t.Fatal(err)
	}
	if _, replay, err := s.CreateInboundSMS(ctx, m); err != nil || replay {
		t.Fatalf("inbound=%t %v", replay, err)
	}
	if _, replay, err := s.CreateInboundSMS(ctx, m); err != nil || !replay {
		t.Fatalf("replay=%t %v", replay, err)
	}
	counts, err := s.NotificationDeliveryCounts(ctx, c.ID)
	if err != nil || counts.Pending != 1 {
		t.Fatalf("outbox=%+v %v", counts, err)
	}
	if err := s.DeleteSMS(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	counts, err = s.NotificationDeliveryCounts(ctx, c.ID)
	if err != nil || counts.Pending != 0 {
		t.Fatalf("deleted message pending=%+v %v", counts, err)
	}
}

func TestSubscriptionStartDoesNotReplayBufferedConnectionHistory(t *testing.T) {
	s, err := OpenSet(t.Context(), filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := queueChannel(t, s, "channel_AAAAAAAAAAAAAAAAAAAAAA")
	event := func(key string, at time.Time) {
		t.Helper()
		if err := s.EnqueueNotification(t.Context(), domain.Event{Key: key, Kind: "vowifi.connected", ObjectID: "line-A", ConnectionKind: "vowifi", Message: "fixture", ObservedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	event("before", c.CreatedAt.Add(-time.Second))
	event("after", c.CreatedAt.Add(time.Second))
	counts, _ := s.NotificationDeliveryCounts(t.Context(), c.ID)
	if counts.Pending != 1 {
		t.Fatalf("historical notification=%+v", counts)
	}
	c.EventKinds = []string{"sms.received"}
	c.UpdatedAt = c.CreatedAt.Add(2 * time.Second)
	if err := s.UpsertNotificationChannel(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	c.EventKinds = domain.EventKinds
	c.UpdatedAt = c.CreatedAt.Add(3 * time.Second)
	if err := s.UpsertNotificationChannel(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	event("while unsubscribed", c.CreatedAt.Add(2500*time.Millisecond))
	event("resubscribed", c.CreatedAt.Add(4*time.Second))
	counts, _ = s.NotificationDeliveryCounts(t.Context(), c.ID)
	if counts.Pending != 1 {
		t.Fatalf("resubscription backfilled history=%+v", counts)
	}
	// Editing an unchanged subscription must retain its original start time.
	c.DisplayName = "renamed"
	c.UpdatedAt = c.CreatedAt.Add(10 * time.Second)
	if err := s.UpsertNotificationChannel(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	event("buffered while subscribed", c.CreatedAt.Add(5*time.Second))
	counts, _ = s.NotificationDeliveryCounts(t.Context(), c.ID)
	if counts.Pending != 2 {
		t.Fatalf("rename reset subscription=%+v", counts)
	}
}
