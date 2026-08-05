package mihomosupervisor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

const testSubscriptionID = "subscription_abcdefghijklmnopqrstuv"

func supervisorFixture(t *testing.T, script string) (*Local, StartRequest) {
	t.Helper()
	root := t.TempDir()
	binary := filepath.Join(root, "versions", "v1.19.29", "mihomo")
	config := filepath.Join(root, "subscriptions", testSubscriptionID, "versions", "0123456789abcdef0123456789abcdef", "generated.yaml")
	for _, path := range []string{binary, config} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	local, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	return local, StartRequest{SubscriptionID: testSubscriptionID, BinaryPath: binary, ConfigPath: config}
}

func TestLocalForUserDropsMihomoToNarrowIdentity(t *testing.T) {
	local, err := NewLocalForUser(t.TempDir(), 1234, 2345)
	if err != nil {
		t.Fatal(err)
	}
	attributes := local.processAttributes()
	if attributes.Credential == nil || attributes.Credential.Uid != 1234 || attributes.Credential.Gid != 2345 {
		t.Fatalf("credential=%#v", attributes.Credential)
	}
	want := []uintptr{unix.CAP_NET_BIND_SERVICE, unix.CAP_NET_ADMIN, unix.CAP_NET_RAW}
	if len(attributes.AmbientCaps) != len(want) {
		t.Fatalf("ambient=%v", attributes.AmbientCaps)
	}
	for index := range want {
		if attributes.AmbientCaps[index] != want[index] {
			t.Fatalf("ambient=%v", attributes.AmbientCaps)
		}
	}
	if attributes.Pdeathsig != syscall.SIGTERM {
		t.Fatalf("pdeathsig=%v", attributes.Pdeathsig)
	}
}

func TestLocalForUserRejectsRootChildIdentity(t *testing.T) {
	if _, err := NewLocalForUser(t.TempDir(), 0, 1000); err == nil {
		t.Fatal("expected root uid rejection")
	}
	if _, err := NewLocalForUser(t.TempDir(), 1000, 0); err == nil {
		t.Fatal("expected root gid rejection")
	}
}

func TestLocalStartsAndStopsOnlyValidatedImmutablePaths(t *testing.T) {
	local, request := supervisorFixture(t, "#!/bin/sh\ntrap 'exit 0' TERM INT\nwhile :; do sleep 1; done\n")
	status, err := local.Start(context.Background(), request)
	if err != nil || !status.Running || status.PID <= 1 {
		t.Fatalf("status=%#v err=%v", status, err)
	}
	if _, err := local.Start(context.Background(), request); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("duplicate start err=%v", err)
	}
	if err := local.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status, err := local.Status(context.Background()); err != nil || status.Running {
		t.Fatalf("status=%#v err=%v", status, err)
	}
	request.BinaryPath = "/bin/sh"
	if _, err := local.Start(context.Background(), request); !errors.Is(err, ErrRequestInvalid) {
		t.Fatalf("arbitrary binary err=%v", err)
	}
}

func TestLocalRejectsListenerStartupFailureAndLeavesNoProcess(t *testing.T) {
	local, request := supervisorFixture(t, "#!/bin/sh\necho 'level=error msg=\"Listener country-gb listen err: operation not permitted\"'\ntrap 'exit 0' TERM INT\nwhile :; do sleep 1; done\n")
	if _, err := local.Start(context.Background(), request); !errors.Is(err, ErrStartupFailed) {
		t.Fatalf("err=%v", err)
	}
	if status, err := local.Status(context.Background()); err != nil || status.Running {
		t.Fatalf("status=%#v err=%v", status, err)
	}
}
