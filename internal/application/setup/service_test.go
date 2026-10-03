package setup_test

import (
	"path/filepath"
	"testing"

	"github.com/leonfox28/simplus/internal/application/auth"
	"github.com/leonfox28/simplus/internal/application/setup"
	"github.com/leonfox28/simplus/internal/security/password"
	"github.com/leonfox28/simplus/internal/storage/sqlite"
)

func TestProvisionMakesInstanceReadyAndLoginWorksAfterRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "control-v2")
	store, err := sqlite.OpenSet(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	hasher := password.NewDefaultHasher()
	service, err := setup.New(setup.Dependencies{StateStore: store, AdministratorStore: store, PasswordHasher: hasher})
	if err != nil {
		t.Fatal(err)
	}
	input := setup.AdministratorInput{Username: "admin", Password: "synthetic password for tests", PasswordConfirmation: "synthetic password for tests", InstanceDefaultLocale: "zh-CN"}
	// Force the ready transition to fail, proving that the credential rolls back too.
	_, err = store.DB.Exec(`CREATE TRIGGER reject_ready BEFORE UPDATE ON installation_state BEGIN SELECT RAISE(ABORT, 'fixture'); END`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.ProvisionAdministrator(t.Context(), input); err == nil {
		t.Fatal("accepted failed transaction")
	}
	_, _, found, err := store.ReadInitialAdministrator(t.Context())
	if err != nil || found {
		t.Fatalf("credential escaped transaction: %v %v", found, err)
	}
	store.DB.Exec(`DROP TRIGGER reject_ready`)
	created, err := service.ProvisionAdministrator(t.Context(), input)
	if err != nil || !created {
		t.Fatalf("provision=%v %v", created, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqlite.OpenSet(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	state, err := store.InstallationState(t.Context())
	if err != nil || state != "ready" {
		t.Fatalf("state=%s %v", state, err)
	}
	if _, err = auth.NewService(store, store, hasher).Login(t.Context(), input.Username, input.Password); err != nil {
		t.Fatal(err)
	}
	service, _ = setup.New(setup.Dependencies{StateStore: store, AdministratorStore: store, PasswordHasher: hasher})
	input.Password = "replacement"
	input.PasswordConfirmation = input.Password
	if created, err = service.ProvisionAdministrator(t.Context(), input); err != nil || created {
		t.Fatalf("reprovision=%v %v", created, err)
	}
}
