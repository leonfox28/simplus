package main

import (
	"bytes"
	"context"
	"encoding/json"
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
