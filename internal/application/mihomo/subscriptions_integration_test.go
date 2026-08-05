package mihomo_test

import (
	"context"
	"path/filepath"
	"testing"

	app "github.com/leonfox28/simplus/internal/application/mihomo"
	"github.com/leonfox28/simplus/internal/security/secretbox"
	"github.com/leonfox28/simplus/internal/storage/sqlite"
)

func TestSubscriptionCRUDStoresAndReturnsPlaintextURLInPrivateDatabase(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	stores, err := sqlite.OpenSet(ctx, filepath.Join(root, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()
	keyring, err := secretbox.Open(filepath.Join(root, "subscription.key"))
	if err != nil {
		t.Fatal(err)
	}
	service := app.NewSubscriptionService(stores, keyring)
	created, err := service.Create(ctx, "", "https://subscription.example/api?token=highly-secret-token", true)
	if err != nil {
		t.Fatal(err)
	}
	if created.URL != "https://subscription.example/api?token=highly-secret-token" || created.URLHint != "subscription.example" || created.NodeCount != 0 || len(created.DisplayName) != 6 {
		t.Fatalf("created = %#v", created)
	}
	stored, found, err := stores.ReadMihomoSubscription(ctx, created.ID)
	if err != nil || !found {
		t.Fatalf("stored = %#v, %t, %v", stored, found, err)
	}
	if stored.URLPlaintext != created.URL {
		t.Fatalf("stored plaintext URL = %q", stored.URLPlaintext)
	}
	listed, err := service.List(ctx)
	if err != nil || len(listed) != 1 || listed[0].URL != created.URL || listed[0].URLHint != "subscription.example" {
		t.Fatalf("listed = %#v, %v", listed, err)
	}
	updated, err := service.Update(ctx, created.ID, "Renamed", "", false)
	if err != nil || updated.ID != created.ID || updated.DisplayName != "Renamed" || updated.Enabled {
		t.Fatalf("updated = %#v, %v", updated, err)
	}
	duplicateName, err := service.Create(ctx, "Renamed", "https://other-subscription.example/api", true)
	if err != nil || duplicateName.ID == created.ID || duplicateName.DisplayName != updated.DisplayName {
		t.Fatalf("duplicate display name = %#v, %v", duplicateName, err)
	}
	if err := service.Delete(ctx, duplicateName.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if listed, err := service.List(ctx); err != nil || len(listed) != 0 {
		t.Fatalf("after delete = %#v, %v", listed, err)
	}
}
