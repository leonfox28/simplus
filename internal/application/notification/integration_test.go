package notification_test

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	app "github.com/leonfox28/simplus/internal/application/notification"
	"github.com/leonfox28/simplus/internal/security/secretbox"
	"github.com/leonfox28/simplus/internal/storage/sqlite"
)

func TestChannelCredentialsAreEncryptedInSQLiteAndOmittedFromViews(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	stores, err := sqlite.OpenSet(ctx, filepath.Join(root, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()
	keyring, err := secretbox.Open(filepath.Join(root, "notification.key"))
	if err != nil {
		t.Fatal(err)
	}
	service := app.New(stores, keyring)
	created, err := service.Create(ctx, "feishu", "Alerts", "https://open.feishu.cn/open-apis/bot/v2/hook/super-secret-hook", "super-secret-signing", true, []string{"system.degraded"})
	if err != nil {
		t.Fatal(err)
	}
	if created.WebhookHint != "open.feishu.cn" || !created.SigningSecretConfigured {
		t.Fatalf("view = %#v", created)
	}
	stored, found, err := stores.ReadNotificationChannel(ctx, created.ID)
	if err != nil || !found {
		t.Fatalf("stored = %#v/%t/%v", stored, found, err)
	}
	if bytes.Contains(stored.WebhookCiphertext, []byte("super-secret-hook")) || bytes.Contains(stored.SigningSecretCiphertext, []byte("super-secret-signing")) {
		t.Fatal("notification credential stored in plaintext")
	}
	views, err := service.List(ctx)
	if err != nil || len(views) != 1 || views[0].WebhookHint != "open.feishu.cn" {
		t.Fatalf("views = %#v/%v", views, err)
	}
}
