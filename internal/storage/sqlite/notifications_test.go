package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	domain "github.com/leonfox28/simplus/internal/domain/notification"
)

func TestFeishuAppNotificationChannelReopensMergesAndRejectsCrossTableCollision(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "db")
	set, err := OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 11, 2, 3, 4, 0, time.UTC)
	webhook := domain.Channel{
		ID: "channel_AAAAAAAAAAAAAAAAAAAAAA", Provider: "wecom", DeliveryMode: domain.DeliveryModeWebhook,
		DisplayName: "Webhook", WebhookCiphertext: make([]byte, 40), WebhookHint: "qyapi.weixin.qq.com",
		Enabled: true, EventKinds: []string{"sms.received"}, LastDeliveryStatus: "never", CreatedAt: now, UpdatedAt: now,
	}
	app := domain.Channel{
		ID: "channel_BBBBBBBBBBBBBBBBBBBBBB", Provider: "feishu", DeliveryMode: domain.DeliveryModeFeishuApp,
		DisplayName: "飞书私聊", FeishuAppIDCiphertext: make([]byte, 40), FeishuAppSecretCiphertext: make([]byte, 41),
		FeishuRecipientOpenIDCiphertext: make([]byte, 42), Enabled: true,
		EventKinds: []string{"call.incoming", "sms.received"}, LastDeliveryAt: now,
		LastDeliveryStatus: "success", CreatedAt: now, UpdatedAt: now,
	}
	if err := set.UpsertNotificationChannel(ctx, webhook); err != nil {
		t.Fatal(err)
	}
	if err := set.UpsertNotificationChannel(ctx, app); err != nil {
		t.Fatal(err)
	}
	invalidID := app
	invalidID.ID = "channel_A" + strings.Repeat("!", 21)
	if err := set.UpsertNotificationChannel(ctx, invalidID); err == nil {
		t.Fatal("app channel ID with characters outside the public contract was accepted")
	}
	invalidEvents := app
	invalidEvents.ID = "channel_EEEEEEEEEEEEEEEEEEEEEE"
	invalidEvents.EventKinds = nil
	if err := set.UpsertNotificationChannel(ctx, invalidEvents); err == nil {
		t.Fatal("app channel with a non-array event set was accepted")
	}
	collision := app
	collision.ID = webhook.ID
	if err := set.UpsertNotificationChannel(ctx, collision); err == nil {
		t.Fatal("cross-table channel ID collision accepted")
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}
	set, err = OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	items, err := set.ListNotificationChannels(ctx)
	if err != nil || len(items) != 2 || items[0].ID != webhook.ID || items[1].ID != app.ID {
		t.Fatalf("items = %#v, err = %v", items, err)
	}
	read, found, err := set.ReadNotificationChannel(ctx, app.ID)
	if err != nil || !found || read.DeliveryMode != domain.DeliveryModeFeishuApp || len(read.WebhookCiphertext) != 0 || len(read.FeishuAppSecretCiphertext) != 41 {
		t.Fatalf("read = %#v, found = %t, err = %v", read, found, err)
	}
	if err := set.RecordNotificationDelivery(ctx, app.ID, "failed", "DELIVERY_REJECTED", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	read, _, _ = set.ReadNotificationChannel(ctx, app.ID)
	if read.LastDeliveryStatus != "failed" || read.LastErrorCode != "DELIVERY_REJECTED" {
		t.Fatalf("delivery = %#v", read)
	}
	if deleted, err := set.DeleteNotificationChannel(ctx, app.ID); err != nil || !deleted {
		t.Fatalf("delete = %t, %v", deleted, err)
	}
	if _, found, err := set.ReadNotificationChannel(ctx, app.ID); err != nil || found {
		t.Fatalf("deleted found = %t, err = %v", found, err)
	}
}
