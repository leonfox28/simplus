package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/control"
)

func TestDoctorSupportStatusIsKnown(t *testing.T) {
	status := supportStatus()
	switch status {
	case "supported-runtime", "development-only", "unsupported":
	default:
		t.Fatalf("unexpected support status %q", status)
	}
}

func TestBootstrapURLRequiresRootBeforeContactingDaemon(t *testing.T) {
	called := false
	var stdout, stderr bytes.Buffer
	exitCode := runWithDependencies(
		[]string{"bootstrap-url", "--socket", "/tmp/simplus.sock"},
		&stdout,
		&stderr,
		dependencies{
			effectiveUID: func() int { return 501 },
			generateBootstrap: func(context.Context, string) (control.BootstrapResponse, error) {
				called = true
				return control.BootstrapResponse{}, nil
			},
		},
	)
	if exitCode != 1 || called {
		t.Fatalf("exit = %d, daemon called = %v", exitCode, called)
	}
	if !strings.Contains(stderr.String(), "must be run as root") || stdout.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestBootstrapURLUsesFragmentAndSupportsJSON(t *testing.T) {
	const code = "ERERERERERERERERERERERERERERERERERERERERERE"
	expires := time.Date(2026, 8, 2, 12, 10, 0, 0, time.UTC)
	var stdout, stderr bytes.Buffer
	exitCode := runWithDependencies(
		[]string{
			"bootstrap-url",
			"--socket", "/run/simplus/control.sock",
			"--base-url", "https://simplus.example",
			"--json",
		},
		&stdout,
		&stderr,
		dependencies{
			effectiveUID: func() int { return 0 },
			generateBootstrap: func(_ context.Context, socket string) (control.BootstrapResponse, error) {
				if socket != "/run/simplus/control.sock" {
					t.Fatalf("socket = %q", socket)
				}
				return control.BootstrapResponse{Code: code, ExpiresAt: expires}, nil
			},
		},
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr.String())
	}
	var result struct {
		URL       string    `json:"url"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.URL != "https://simplus.example/setup#bootstrap="+code {
		t.Fatalf("URL = %q", result.URL)
	}
	if !result.ExpiresAt.Equal(expires) {
		t.Fatalf("expiry = %s", result.ExpiresAt)
	}
}

func TestProvisionAdministratorPrintsGeneratedCredentialOnlyWhenCreated(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := runWithDependencies([]string{"provision-admin", "--socket", "/run/simplus/control.sock", "--json"}, &stdout, &stderr, dependencies{
		effectiveUID: func() int { return 0 },
		provisionAdmin: func(_ context.Context, socket string, request control.ProvisionAdministratorRequest) (control.ProvisionAdministratorResponse, error) {
			if socket != "/run/simplus/control.sock" || request.Username != "simplus_admin" || request.Locale != "zh-CN" || len(request.Password) != 32 {
				t.Fatalf("request = %#v, socket = %q", request, socket)
			}
			return control.ProvisionAdministratorResponse{Created: true}, nil
		},
	})
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr.String())
	}
	var result struct {
		Created            bool `json:"created"`
		Username, Password string
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Created || result.Username != "simplus_admin" || len(result.Password) != 32 {
		t.Fatalf("result = %#v", result)
	}
}

func TestProvisionAdministratorDoesNotRedisplayCredential(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := runWithDependencies([]string{"provision-admin", "--socket", "/run/simplus/control.sock", "--json"}, &stdout, &stderr, dependencies{
		effectiveUID: func() int { return 0 },
		provisionAdmin: func(context.Context, string, control.ProvisionAdministratorRequest) (control.ProvisionAdministratorResponse, error) {
			return control.ProvisionAdministratorResponse{Created: false}, nil
		},
	})
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if created, _ := result["created"].(bool); created || result["username"] != nil || result["password"] != nil {
		t.Fatalf("repeated provisioning disclosed credential fields: %#v", result)
	}
}

func TestHardwareProbeRequiresRootBeforeContactingAgent(t *testing.T) {
	called := false
	var stdout, stderr bytes.Buffer
	exitCode := runWithDependencies(
		[]string{"hardware", "probe", "--socket", "/run/simplus/agent.sock", "--json"},
		&stdout, &stderr,
		dependencies{
			effectiveUID: func() int { return 1000 },
			probeHardware: func(context.Context, string) (hardwareProbeOutput, error) {
				called = true
				return hardwareProbeOutput{}, nil
			},
		},
	)
	if exitCode != 1 || called || !strings.Contains(stderr.String(), "must be run as root") {
		t.Fatalf("exit=%d called=%v stdout=%q stderr=%q", exitCode, called, stdout.String(), stderr.String())
	}
}

func TestHardwareProbeJSONUsesTypedAgentReport(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := runWithDependencies(
		[]string{"hardware", "probe", "--socket", "/run/simplus/agent.sock", "--json"},
		&stdout, &stderr,
		dependencies{
			effectiveUID: func() int { return 0 },
			probeHardware: func(_ context.Context, socket string) (hardwareProbeOutput, error) {
				if socket != "/run/simplus/agent.sock" {
					t.Fatalf("socket = %q", socket)
				}
				return hardwareProbeOutput{
					Hello:    agentapi.Hello{Protocol: agentapi.ProtocolName, ProtocolVersion: agentapi.ProtocolVersion, AgentInstanceID: "01234567-89ab-cdef-0123-456789abcdef"},
					Snapshot: agentapi.Snapshot{ProtocolVersion: agentapi.ProtocolVersion, AgentInstanceID: "01234567-89ab-cdef-0123-456789abcdef", Generation: 4, Revision: strings.Repeat("a", 64), Devices: []agentapi.DeviceReport{{ID: "usb-1-1", DisplayName: "QDC507"}}},
					Probe:    agentapi.ProbeResponse{ProtocolVersion: agentapi.ProtocolVersion, AgentInstanceID: "01234567-89ab-cdef-0123-456789abcdef", SnapshotGeneration: 4, SnapshotRevision: strings.Repeat("a", 64)},
				}, nil
			},
		},
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr.String())
	}
	var report hardwareProbeOutput
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Snapshot.Generation != 4 || len(report.Snapshot.Devices) != 1 {
		t.Fatalf("report = %#v", report)
	}
}

func TestBootstrapURLRejectsNonOriginBeforeGenerating(t *testing.T) {
	called := false
	var stdout, stderr bytes.Buffer
	exitCode := runWithDependencies(
		[]string{
			"bootstrap-url",
			"--socket", "/run/simplus/control.sock",
			"--base-url", "https://simplus.example/prefix?secret=value",
		},
		&stdout,
		&stderr,
		dependencies{
			effectiveUID: func() int { return 0 },
			generateBootstrap: func(context.Context, string) (control.BootstrapResponse, error) {
				called = true
				return control.BootstrapResponse{}, nil
			},
		},
	)
	if exitCode != 2 || called {
		t.Fatalf("exit=%d daemon called=%v", exitCode, called)
	}
}

func TestHealthSubcommandsUseTypedCheckers(t *testing.T) {
	tests := []struct {
		kind, flagName, target string
	}{
		{kind: "app", flagName: "--url", target: "http://127.0.0.1:18080"},
		{kind: "agent", flagName: "--socket", target: "/run/test/agent.sock"},
		{kind: "netd", flagName: "--socket", target: "/run/test/netd.sock"},
	}
	for _, test := range tests {
		t.Run(test.kind, func(t *testing.T) {
			called := ""
			checker := func(_ context.Context, target string) error {
				called = target
				return nil
			}
			deps := dependencies{}
			switch test.kind {
			case "app":
				deps.checkAppHealth = checker
			case "agent":
				deps.checkAgentHealth = checker
			case "netd":
				deps.checkNetdHealth = checker
			}
			var stdout, stderr bytes.Buffer
			status := runWithDependencies([]string{"health", test.kind, test.flagName, test.target}, &stdout, &stderr, deps)
			if status != 0 || called != test.target || stderr.Len() != 0 || !strings.Contains(stdout.String(), "healthy") {
				t.Fatalf("status=%d called=%q stdout=%q stderr=%q", status, called, stdout.String(), stderr.String())
			}
		})
	}
}

func TestHealthFailureIsFailClosed(t *testing.T) {
	var stdout, stderr bytes.Buffer
	status := runWithDependencies([]string{"health", "agent"}, &stdout, &stderr, dependencies{
		checkAgentHealth: func(context.Context, string) error { return errors.New("socket unavailable") },
	})
	if status != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "socket unavailable") {
		t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if status := runWithDependencies([]string{"health", "unknown"}, &stdout, &stderr, dependencies{}); status != 2 {
		t.Fatalf("unknown health status = %d", status)
	}
}

func TestAppHealthRequiresLoopbackAndTypedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/health" {
			t.Fatalf("health path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","apiVersion":"v1"}`))
	}))
	defer server.Close()
	if err := checkAppHealth(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{
		"http://192.0.2.1:8080", "https://127.0.0.1:8080", "http://localhost:8080", "http://127.0.0.1:8080/path",
	} {
		if err := checkAppHealth(context.Background(), invalid); err == nil {
			t.Fatalf("accepted invalid health origin %q", invalid)
		}
	}
}
