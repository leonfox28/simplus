package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	domain "github.com/leonfox28/simplus/internal/domain/notification"
	"github.com/pressly/goose/v3"
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

func TestFeishuAppNotificationMigrationDownKeepsWebhookAndReupgradeStartsEmpty(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "db")
	set, err := OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	webhook := domain.Channel{ID: "channel_CCCCCCCCCCCCCCCCCCCCCC", Provider: "feishu", DeliveryMode: domain.DeliveryModeWebhook, DisplayName: "Legacy", WebhookCiphertext: make([]byte, 40), WebhookHint: "open.feishu.cn", Enabled: true, EventKinds: []string{"system.degraded"}, LastDeliveryStatus: "never", CreatedAt: now, UpdatedAt: now}
	app := domain.Channel{ID: "channel_DDDDDDDDDDDDDDDDDDDDDD", Provider: "feishu", DeliveryMode: domain.DeliveryModeFeishuApp, DisplayName: "App", FeishuAppIDCiphertext: make([]byte, 40), FeishuAppSecretCiphertext: make([]byte, 40), FeishuRecipientOpenIDCiphertext: make([]byte, 40), Enabled: true, EventKinds: []string{"system.degraded"}, LastDeliveryStatus: "success", LastDeliveryAt: now, CreatedAt: now, UpdatedAt: now}
	if err := set.UpsertNotificationChannel(ctx, webhook); err != nil {
		t.Fatal(err)
	}
	if err := set.UpsertNotificationChannel(ctx, app); err != nil {
		t.Fatal(err)
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", filepath.Join(root, "core.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	migrationMu.Lock()
	goose.SetLogger(goose.NopLogger())
	err = goose.SetDialect("sqlite3")
	if err == nil {
		err = goose.DownToContext(ctx, database, "migrations/core", 22)
	}
	migrationMu.Unlock()
	if err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	var version, webhookCount, appTables int
	if err := database.QueryRowContext(ctx, `SELECT schema_version FROM dataset_metadata WHERE singleton=1`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM notification_channels`).Scan(&webhookCount); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='feishu_app_notification_channels'`).Scan(&appTables); err != nil {
		t.Fatal(err)
	}
	if version != 22 || webhookCount != 1 || appTables != 0 {
		t.Fatalf("down state version=%d webhook=%d appTables=%d", version, webhookCount, appTables)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	set, err = OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	items, err := set.ListNotificationChannels(ctx)
	if err != nil || len(items) != 1 || items[0].ID != webhook.ID {
		t.Fatalf("reupgrade items=%#v err=%v", items, err)
	}
}
