package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/leonfox28/simplus/internal/modemadapter"
)

func TestHardwareWriteFlagsAreNotAccepted(t *testing.T) {
	for _, name := range []string{"--enable-radio-ensure-off", "--enable-qdc507-sms"} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		if status := run([]string{name}, &stdout, &stderr); status != 2 {
			t.Fatalf("run(%q) status = %d, want 2", name, status)
		}
		if !strings.Contains(stderr.String(), "flag provided but not defined") {
			t.Fatalf("run(%q) stderr = %q", name, stderr.String())
		}
	}
}

func TestProductionRuntimeRequiresIdentityAndStateRootWithoutEnableFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	status := run([]string{"--identity-key", "/synthetic/key"}, &stdout, &stderr)
	if status != 2 || !strings.Contains(stderr.String(), "state-root") {
		t.Fatalf("missing state root status=%d stderr=%q", status, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	status = run([]string{"--state-root", "/synthetic/state"}, &stdout, &stderr)
	if status != 2 || !strings.Contains(stderr.String(), "identity-key") {
		t.Fatalf("missing identity key status=%d stderr=%q", status, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	status = run([]string{"--identity-key", "/synthetic/key", "--state-root", "relative"}, &stdout, &stderr)
	if status != 2 || !strings.Contains(stderr.String(), "absolute non-root") {
		t.Fatalf("relative state root status=%d stderr=%q", status, stderr.String())
	}
}

func TestRegisterOptionDriverUsesOnlyRegistryIDs(t *testing.T) {
	registry := modemadapter.DefaultRegistry()
	var paths []string
	var ids []modemadapter.USBSerialID
	writer := func(path string, id modemadapter.USBSerialID) error {
		paths = append(paths, path)
		ids = append(ids, id)
		return nil
	}
	var stdout, stderr bytes.Buffer
	if status := runRegisterOptionDriver(&stdout, &stderr, 0, registry, writer); status != 0 {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
	if len(paths) != 1 || paths[0] != containerOptionNewIDPath || len(ids) != 1 ||
		ids[0] != (modemadapter.USBSerialID{VendorID: "2ecc", ProductID: "3012"}) {
		t.Fatalf("paths = %#v, ids = %#v", paths, ids)
	}
	if !strings.Contains(stdout.String(), "registered 1 verified") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRegisterOptionDriverRequiresRootAndFailsClosed(t *testing.T) {
	called := false
	writer := func(string, modemadapter.USBSerialID) error {
		called = true
		return syscall.EPERM
	}
	var stdout, stderr bytes.Buffer
	if status := runRegisterOptionDriver(&stdout, &stderr, 10002, modemadapter.DefaultRegistry(), writer); status != 1 || called {
		t.Fatalf("non-root status = %d, writer called = %t", status, called)
	}
	stdout.Reset()
	stderr.Reset()
	if status := runRegisterOptionDriver(&stdout, &stderr, 0, modemadapter.DefaultRegistry(), writer); status != 1 || !called {
		t.Fatalf("write failure status = %d, writer called = %t", status, called)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "operation not permitted") {
		t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}
}

func TestRegisterOptionDriverAcceptsAlreadyRegisteredID(t *testing.T) {
	writer := func(string, modemadapter.USBSerialID) error { return syscall.EEXIST }
	var stdout, stderr bytes.Buffer
	if status := runRegisterOptionDriver(&stdout, &stderr, 0, modemadapter.DefaultRegistry(), writer); status != 0 {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
}

func TestOptionDriverWriterRejectsSymlinkAndWritesBoundedID(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "new_id")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	id := modemadapter.USBSerialID{VendorID: "2ecc", ProductID: "3012"}
	if err := writeOptionIDFile(path, id); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "2ecc 3012\n" {
		t.Fatalf("new_id body = %q, error = %v", body, err)
	}
	symlink := filepath.Join(directory, "linked-new_id")
	if err := os.Symlink(path, symlink); err != nil {
		t.Fatal(err)
	}
	if err := writeOptionIDFile(symlink, id); err == nil {
		t.Fatal("option driver writer accepted a symlink")
	}
}
