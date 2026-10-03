//go:build integration

// simulatorserver runs the real control process with disposable Simulator state.
// The rejecting proxy prevents fixture notification credentials reaching a provider.
package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/leonfox28/simplus/internal/application/setup"
	"github.com/leonfox28/simplus/internal/security/password"
	"github.com/leonfox28/simplus/internal/storage/sqlite"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) != 3 {
		return fmt.Errorf("usage: simulatorserver <simplusd> <web-dist>")
	}
	root, err := os.MkdirTemp("", "simplus-browser-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	db, err := sqlite.OpenSet(ctx, filepath.Join(root, "state"))
	if err != nil {
		return err
	}
	install, err := setup.New(setup.Dependencies{StateStore: db, AdministratorStore: db, PasswordHasher: password.NewDefaultHasher()})
	if err != nil {
		_ = db.Close()
		return err
	}
	_, err = install.ProvisionAdministrator(ctx, setup.AdministratorInput{Username: "browser_admin", Password: "integration-test-password", PasswordConfirmation: "integration-test-password", InstanceDefaultLocale: "zh-CN"})
	closeErr := db.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	proxy := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })}
	defer proxy.Close()
	go func() { _ = proxy.Serve(listener) }()
	command := exec.Command(os.Args[1])
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	command.Env = append(os.Environ(), "SIMPLUS_DATA_ROOT="+root, "SIMPLUS_BACKEND=simulator", "SIMPLUS_LISTEN_ADDR=127.0.0.1:4183", "SIMPLUS_WEB_ROOT="+os.Args[2], "SIMPLUS_CONTROL_SOCKET="+filepath.Join(root, "control.sock"), "SIMPLUS_MIHOMO_SUPERVISOR_SOCKET=", "HTTPS_PROXY=http://"+listener.Addr().String(), "HTTP_PROXY=http://"+listener.Addr().String(), "NO_PROXY=127.0.0.1,localhost")
	if err := command.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
	}
	_ = command.Process.Signal(syscall.SIGTERM)
	select {
	case err := <-done:
		return err
	case <-time.After(35 * time.Second):
		_ = command.Process.Kill()
		<-done
		return fmt.Errorf("simulator shutdown timed out")
	}
}
