package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	sqlitestore "github.com/leonfox28/simplus/internal/storage/sqlite"
)

func TestNewSetupServiceAcceptsCompleteProductionDependencies(t *testing.T) {
	databaseRoot := filepath.Join(t.TempDir(), "db")
	stores, err := sqlitestore.OpenSet(context.Background(), databaseRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stores.Close() })

	instanceSecretKeyPath := filepath.Join(databaseRoot, ".simplus-secrets-key-v1")
	service, err := newSetupService(stores, instanceSecretKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	if service == nil {
		t.Fatal("Setup composition returned a nil service")
	}
	if _, err := os.Lstat(instanceSecretKeyPath); !os.IsNotExist(err) {
		t.Fatalf("Setup composition opened its lazy secret protector: %v", err)
	}
}
