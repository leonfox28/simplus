package control_test

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/leonfox28/simplus/internal/application/setup"
	"github.com/leonfox28/simplus/internal/control"
	"github.com/leonfox28/simplus/internal/storage/sqlite"
)

func TestBootstrapControlRejectsUnauthorizedUnixPeer(t *testing.T) {
	ctx := context.Background()
	temporaryRoot, err := os.MkdirTemp("/tmp", "simplus-control-denied-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(temporaryRoot) })
	dataRoot := filepath.Join(temporaryRoot, "data")
	if err := os.Mkdir(dataRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	stores, err := sqlite.OpenSet(ctx, filepath.Join(dataRoot, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()

	socketPath := control.SocketPath(dataRoot)
	deniedUID := uint32(os.Geteuid()) + 1
	listener, err := control.ListenRootOnly(socketPath, deniedUID)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: control.NewBootstrapHandler(setup.New(stores, stores), slog.Default())}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()

	if _, err := control.GenerateBootstrap(ctx, socketPath); err == nil {
		t.Fatal("unauthorized Unix peer generated a bootstrap grant")
	}
	var grants int
	if err := stores.Runtime.QueryRow(`SELECT count(*) FROM setup_bootstrap_grant`).Scan(&grants); err != nil {
		t.Fatal(err)
	}
	if grants != 0 {
		t.Fatalf("bootstrap grants after rejected peer = %d", grants)
	}
	if err := server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-serveDone; err != nil && err != http.ErrServerClosed {
		t.Fatal(err)
	}
}

func TestBootstrapControlRoundTripOverAuthorizedUnixPeer(t *testing.T) {
	ctx := context.Background()
	temporaryRoot, err := os.MkdirTemp("/tmp", "simplus-control-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(temporaryRoot) })
	dataRoot := filepath.Join(temporaryRoot, "data")
	if err := os.Mkdir(dataRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	stores, err := sqlite.OpenSet(ctx, filepath.Join(dataRoot, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()

	socketPath := control.SocketPath(dataRoot)
	listener, err := control.ListenRootOnly(socketPath, uint32(os.Geteuid()))
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: control.NewBootstrapHandler(setup.New(stores, stores), slog.Default())}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()

	response, err := control.GenerateBootstrap(ctx, socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Code) != 43 || response.ExpiresAt.IsZero() {
		t.Fatalf("bootstrap response = %#v", response)
	}
	if err := server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-serveDone; err != nil && err != http.ErrServerClosed {
		t.Fatal(err)
	}
	if _, err := os.Lstat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("control socket remained after shutdown: %v", err)
	}
}

func TestAdministratorProvisioningIsOneTimeOverRootControlSocket(t *testing.T) {
	ctx := context.Background()
	root, err := os.MkdirTemp("/tmp", "simplus-provision-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	dataRoot := filepath.Join(root, "data")
	if err := os.Mkdir(dataRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	stores, err := sqlite.OpenSet(ctx, filepath.Join(dataRoot, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()
	socketPath := control.SocketPath(dataRoot)
	listener, err := control.ListenRootOnly(socketPath, uint32(os.Geteuid()))
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: control.NewBootstrapHandler(setup.New(stores, stores), slog.Default())}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	request := control.ProvisionAdministratorRequest{Username: "simplus_admin", Password: "first-generated-password-123", Locale: "zh-CN"}
	created, err := control.ProvisionAdministrator(ctx, socketPath, request)
	if err != nil || !created.Created {
		t.Fatalf("first provision = %#v, %v", created, err)
	}
	request.Password = "replacement-must-not-win-456"
	repeated, err := control.ProvisionAdministrator(ctx, socketPath, request)
	if err != nil || repeated.Created {
		t.Fatalf("second provision = %#v, %v", repeated, err)
	}
	username, locale, configured, err := stores.ReadInitialAdministrator(ctx)
	if err != nil || !configured || username != "simplus_admin" || locale != "zh-CN" {
		t.Fatalf("administrator = %q %q %t %v", username, locale, configured, err)
	}
	credential, err := stores.ReadAdministratorCredential(ctx, username)
	if err != nil || !credential.Found {
		t.Fatal("provisioned credential missing")
	}
	if err := server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-serveDone; err != nil && err != http.ErrServerClosed {
		t.Fatal(err)
	}
}
