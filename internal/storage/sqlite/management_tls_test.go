package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/domain/managementtls"
)

func TestManagementTLSPersistsAndConfirmsCandidate(t *testing.T) {
	ctx := context.Background()
	set, err := OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	if _, found, err := set.ReadManagementTLS(ctx); err != nil || found {
		t.Fatalf("initial management TLS found/error = %t/%v", found, err)
	}
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	configuration := managementtls.Configuration{
		Mode:                    managementtls.ModeLocalCA,
		ListenHost:              "192.168.50.10",
		ListenPort:              8443,
		SubjectAlternativeNames: []string{"simplus.local", "192.168.50.10"},
		CACertificatePEM:        []byte("ca"),
		LeafCertificatePEM:      []byte("leaf"),
		EncryptedCAPrivateKey:   []byte("encrypted-ca"),
		EncryptedLeafPrivateKey: []byte("encrypted-leaf"),
		RootFingerprintSHA256:   "AA:AA:AA:AA:AA:AA:AA:AA:AA:AA:AA:AA:AA:AA:AA:AA:AA:AA:AA:AA:AA:AA:AA:AA:AA:AA:AA:AA:AA:AA:AA:AA",
		LeafNotAfter:            now.Add(90 * 24 * time.Hour),
		Confirmed:               false,
		ConfiguredAt:            now,
	}
	if err := set.ConfigureManagementTLS(ctx, configuration); err != nil {
		t.Fatal(err)
	}
	stored, found, err := set.ReadManagementTLS(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !found || stored.Mode != configuration.Mode || stored.ListenHost != configuration.ListenHost || stored.Confirmed || len(stored.EncryptedLeafPrivateKey) == 0 {
		t.Fatalf("stored management TLS = %#v", stored)
	}
	if confirmed, err := set.ConfirmManagementTLS(ctx, "wrong", now); err != nil || confirmed {
		t.Fatalf("wrong fingerprint confirmation = %t/%v", confirmed, err)
	}
	confirmed, err := set.ConfirmManagementTLS(ctx, configuration.RootFingerprintSHA256, now.Add(time.Minute))
	if err != nil || !confirmed {
		t.Fatalf("confirmation = %t/%v", confirmed, err)
	}
	stored, _, err = set.ReadManagementTLS(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Confirmed {
		t.Fatal("confirmed flag was not persisted")
	}
}

func TestManagementTLSAcceptsConfirmedLoopbackWithoutKeys(t *testing.T) {
	ctx := context.Background()
	set, err := OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	configuration := managementtls.Configuration{
		Mode:         managementtls.ModeLoopbackOnly,
		ListenHost:   "127.0.0.1",
		ListenPort:   8080,
		Confirmed:    true,
		ConfiguredAt: time.Now(),
	}
	if err := set.ConfigureManagementTLS(ctx, configuration); err != nil {
		t.Fatal(err)
	}
	stored, found, err := set.ReadManagementTLS(ctx)
	if err != nil || !found || !stored.Confirmed || stored.Mode != managementtls.ModeLoopbackOnly {
		t.Fatalf("loopback configuration = %#v, %t, %v", stored, found, err)
	}
}
