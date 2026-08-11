package notification_test

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	app "github.com/leonfox28/simplus/internal/application/notification"
	"github.com/leonfox28/simplus/internal/security/secretbox"
	"github.com/leonfox28/simplus/internal/storage/sqlite"
)

type integrationRegistrar struct{}

func (integrationRegistrar) Begin(context.Context) (app.FeishuRegistration, error) {
	return app.FeishuRegistration{DeviceCode: "device_synthetic", VerificationURL: "https://accounts.feishu.cn/synthetic", ExpiresAt: time.Now().Add(time.Minute)}, nil
}
func (integrationRegistrar) Poll(context.Context, app.FeishuRegistration) (app.FeishuRegistrationResult, error) {
	return app.FeishuRegistrationResult{AppID: "cli_synthetic_app", AppSecret: "synthetic_app_secret", OpenID: "ou_synthetic_user", TenantBrand: "feishu"}, nil
}

type integrationMessenger struct{}

func (integrationMessenger) SendText(context.Context, app.FeishuRegistrationResult, string) error {
	return nil
}

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

func TestFeishuAppCredentialsAreIndependentlyEncryptedAndOmittedFromViews(t *testing.T) {
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
	service.ConfigureFeishuBinding(ctx, integrationRegistrar{}, integrationMessenger{}, nil)
	if _, err := service.StartFeishuBinding(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for service.FeishuBindingStatus().State != app.BindingStateSucceeded && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	state := service.FeishuBindingStatus()
	if state.State != app.BindingStateSucceeded || state.ChannelID == "" || state.VerificationURL != "" {
		t.Fatalf("state = %#v", state)
	}
	stored, found, err := stores.ReadNotificationChannel(ctx, state.ChannelID)
	if err != nil || !found {
		t.Fatalf("stored = %#v/%t/%v", stored, found, err)
	}
	for name, ciphertext := range map[string][]byte{"app id": stored.FeishuAppIDCiphertext, "app secret": stored.FeishuAppSecretCiphertext, "open id": stored.FeishuRecipientOpenIDCiphertext} {
		if len(ciphertext) == 0 || bytes.Contains(ciphertext, []byte("synthetic")) {
			t.Fatalf("%s ciphertext is missing or plaintext", name)
		}
	}
	views, err := service.List(ctx)
	if err != nil || len(views) != 1 || views[0].DeliveryMode != "feishu_app" || views[0].TargetType != "authorized_user" || views[0].WebhookHint != "open.feishu.cn" {
		t.Fatalf("views = %#v/%v", views, err)
	}
}
