package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestConfigureInitialAdministratorPersistsAndRotatesCredentialGeneration(t *testing.T) {
	ctx := context.Background()
	set, err := OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	username, locale, configured, err := set.ReadInitialAdministrator(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if configured || username != "" || locale != "en-US" {
		t.Fatalf("initial administrator = username %q, locale %q, configured %t", username, locale, configured)
	}

	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	const firstHash = "$argon2id$v1$m=8192,t=1,p=1$WlpaWlpaWlpaWlpaWlpaWg$WlpaWlpaWlpaWlpaWlpaWg"
	if err := set.ConfigureInitialAdministrator(ctx, "leon", firstHash, "zh-CN", now); err != nil {
		t.Fatal(err)
	}
	username, locale, configured, err = set.ReadInitialAdministrator(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !configured || username != "leon" || locale != "zh-CN" {
		t.Fatalf("configured administrator = username %q, locale %q, configured %t", username, locale, configured)
	}
	credential, err := set.ReadAdministratorCredential(ctx, "leon")
	if err != nil {
		t.Fatal(err)
	}
	if !credential.Found || credential.PasswordHash != firstHash || credential.SessionGeneration != 1 {
		t.Fatalf("first credential = %#v", credential)
	}

	const secondHash = "$argon2id$v1$m=8192,t=1,p=1$WVlZWVlZWVlZWVlZWVlZWQ$WVlZWVlZWVlZWVlZWVlZWQ"
	if err := set.ConfigureInitialAdministrator(ctx, "admin", secondHash, "en-US", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if old, err := set.ReadAdministratorCredential(ctx, "leon"); err != nil || old.Found {
		t.Fatalf("old username lookup = %#v, %v", old, err)
	}
	credential, err = set.ReadAdministratorCredential(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !credential.Found || credential.PasswordHash != secondHash || credential.SessionGeneration != 2 {
		t.Fatalf("rotated credential = %#v", credential)
	}
}

func TestConfigureInitialAdministratorRejectsReadyInstance(t *testing.T) {
	ctx := context.Background()
	set, err := OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	if _, err := set.Core.ExecContext(ctx, `UPDATE installation_state SET state = 'ready' WHERE singleton = 1`); err != nil {
		t.Fatal(err)
	}
	if err := set.ConfigureInitialAdministrator(ctx, "admin", "$argon2id$v1$m=8192,t=1,p=1$WlpaWlpaWlpaWlpaWlpaWg$WlpaWlpaWlpaWlpaWlpaWg", "en-US", time.Now()); err == nil {
		t.Fatal("ConfigureInitialAdministrator accepted a ready instance")
	}
}

func TestChangeAdministratorPasswordUsesGenerationAndRevokesSessions(t *testing.T) {
	ctx := context.Background()
	set, err := OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	now := time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)
	const oldHash = "$argon2id$v1$m=8192,t=1,p=1$WlpaWlpaWlpaWlpaWlpaWg$WlpaWlpaWlpaWlpaWlpaWg"
	const newHash = "$argon2id$v1$m=8192,t=1,p=1$WVlZWVlZWVlZWVlZWVlZWQ$WVlZWVlZWVlZWVlZWVlZWQ"
	if err := set.ConfigureInitialAdministrator(ctx, "simplus_admin", oldHash, "zh-CN", now); err != nil {
		t.Fatal(err)
	}
	var token, csrf [32]byte
	token[0], csrf[0] = 1, 2
	if err := set.CreateAdministratorSession(ctx, token, csrf, "simplus_admin", 1, now.Unix(), now.Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if changed, err := set.ChangeAdministratorPassword(ctx, "simplus_admin", newHash, 2, now); err != nil || changed {
		t.Fatalf("stale change = %t/%v", changed, err)
	}
	if changed, err := set.ChangeAdministratorPassword(ctx, "simplus_admin", newHash, 1, now); err != nil || !changed {
		t.Fatalf("change = %t/%v", changed, err)
	}
	credential, err := set.ReadAdministratorCredential(ctx, "simplus_admin")
	if err != nil {
		t.Fatal(err)
	}
	if credential.PasswordHash != newHash || credential.SessionGeneration != 2 {
		t.Fatalf("credential = %#v", credential)
	}
	if _, _, _, _, found, err := set.ReadAdministratorSession(ctx, token, now.Unix()); err != nil || found {
		t.Fatalf("old session found = %t/%v", found, err)
	}
}
