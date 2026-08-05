package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsAreLoopbackAndSimulator(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Listen != "127.0.0.1:8080" {
		t.Fatalf("listen = %q", cfg.Server.Listen)
	}
	if cfg.Runtime.Backend != BackendSimulator {
		t.Fatalf("backend = %q", cfg.Runtime.Backend)
	}
	if cfg.Runtime.AgentSocket != "/run/simplus/simplus-agent.sock" {
		t.Fatalf("agent socket = %q", cfg.Runtime.AgentSocket)
	}
	if !filepath.IsAbs(cfg.Storage.DataRoot) {
		t.Fatalf("data root is not absolute: %q", cfg.Storage.DataRoot)
	}
}

func TestHardwareBackendRequiresAbsoluteAgentSocket(t *testing.T) {
	t.Setenv("SIMPLUS_BACKEND", BackendHardware)
	t.Setenv("SIMPLUS_AGENT_SOCKET", "relative/agent.sock")
	if _, err := Load(""); err == nil {
		t.Fatal("Load() accepted a relative hardware agent socket")
	}
	t.Setenv("SIMPLUS_AGENT_SOCKET", "/run/simplus/../simplus/agent.sock")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Runtime.AgentSocket != "/run/simplus/agent.sock" {
		t.Fatalf("agent socket = %q", cfg.Runtime.AgentSocket)
	}
}

func TestAcceptsPrivateLANListener(t *testing.T) {
	t.Setenv("SIMPLUS_LISTEN_ADDR", "192.168.50.10:8080")
	if _, err := Load(""); err != nil {
		t.Fatalf("Load() rejected a private LAN listener: %v", err)
	}
}

func TestRejectsUnspecifiedOrPublicListener(t *testing.T) {
	for _, address := range []string{"0.0.0.0:8080", "8.8.8.8:8080"} {
		t.Setenv("SIMPLUS_LISTEN_ADDR", address)
		if _, err := Load(""); err == nil {
			t.Fatalf("Load() accepted %q", address)
		}
	}
}

func TestRejectsHostnameListenerEvenWhenNamedLocalhost(t *testing.T) {
	t.Setenv("SIMPLUS_LISTEN_ADDR", "localhost:8080")
	if _, err := Load(""); err == nil {
		t.Fatal("Load() accepted a hostname listener")
	}
}

func TestRejectsInvalidListenPorts(t *testing.T) {
	for _, address := range []string{"127.0.0.1:0", "127.0.0.1:65536", "127.0.0.1:http"} {
		t.Run(address, func(t *testing.T) {
			t.Setenv("SIMPLUS_LISTEN_ADDR", address)
			if _, err := Load(""); err == nil {
				t.Fatalf("Load() accepted %q", address)
			}
		})
	}
}

func TestResolvesConfigRelativeDataRootAgainstConfigDirectory(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "simplus.yaml")
	contents := "storage:\n  data_root: state\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Storage.DataRoot != filepath.Join(directory, "state") {
		t.Fatalf("data root = %q", cfg.Storage.DataRoot)
	}
}

func TestRejectsRelativeEnvironmentDataRoot(t *testing.T) {
	t.Setenv("SIMPLUS_DATA_ROOT", filepath.Join("relative", "state"))
	if _, err := Load(""); err == nil {
		t.Fatal("Load() accepted a relative SIMPLUS_DATA_ROOT")
	}
}

func TestRejectsFilesystemRootAsDataRoot(t *testing.T) {
	t.Setenv("SIMPLUS_DATA_ROOT", string(filepath.Separator))
	if _, err := Load(""); err == nil {
		t.Fatal("Load() accepted the filesystem root as storage.data_root")
	}
}

func TestRejectsUnknownYAMLFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "simplus.yaml")
	if err := os.WriteFile(path, []byte("server:\n  listen: 127.0.0.1:8080\nunknown: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load() accepted an unknown field")
	}
}

func TestRejectsMultipleYAMLDocuments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "simplus.yaml")
	contents := "server:\n  listen: 127.0.0.1:8080\n---\nserver:\n  listen: 0.0.0.0:9999\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load() accepted multiple YAML documents")
	}
}
