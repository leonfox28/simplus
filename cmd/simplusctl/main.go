package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/buildinfo"
	"github.com/leonfox28/simplus/internal/config"
	"github.com/leonfox28/simplus/internal/control"
)

type bootstrapGenerator func(context.Context, string) (control.BootstrapResponse, error)
type administratorProvisioner func(context.Context, string, control.ProvisionAdministratorRequest) (control.ProvisionAdministratorResponse, error)
type hardwareProber func(context.Context, string) (hardwareProbeOutput, error)

type hardwareProbeOutput struct {
	Hello    agentapi.Hello         `json:"hello"`
	Snapshot agentapi.Snapshot      `json:"snapshot"`
	Probe    agentapi.ProbeResponse `json:"probe"`
}

type dependencies struct {
	effectiveUID      func() int
	generateBootstrap bootstrapGenerator
	provisionAdmin    administratorProvisioner
	probeHardware     hardwareProber
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	return runWithDependencies(args, os.Stdout, os.Stderr, dependencies{
		effectiveUID:      os.Geteuid,
		generateBootstrap: control.GenerateBootstrap,
		provisionAdmin:    control.ProvisionAdministrator,
		probeHardware:     probeHardwareAgent,
	})
}

func runWithDependencies(args []string, stdout, stderr io.Writer, deps dependencies) int {
	if len(args) == 1 && (args[0] == "version" || args[0] == "--version") {
		info := buildinfo.Current()
		fmt.Fprintf(stdout, "simplusctl %s (%s)\n", info.Version, info.Commit)
		return 0
	}
	if len(args) == 2 && args[0] == "version" && args[1] == "--json" {
		_ = json.NewEncoder(stdout).Encode(buildinfo.Current())
		return 0
	}
	if len(args) == 1 && args[0] == "doctor" {
		result := struct {
			OS      string `json:"os"`
			Arch    string `json:"arch"`
			Go      string `json:"go"`
			Support string `json:"support"`
		}{OS: runtime.GOOS, Arch: runtime.GOARCH, Go: runtime.Version(), Support: supportStatus()}
		_ = json.NewEncoder(stdout).Encode(result)
		if result.Support == "unsupported" {
			return 1
		}
		return 0
	}
	if len(args) >= 1 && args[0] == "bootstrap-url" {
		return runBootstrapURL(args[1:], stdout, stderr, deps)
	}
	if len(args) >= 1 && args[0] == "provision-admin" {
		return runProvisionAdministrator(args[1:], stdout, stderr, deps)
	}
	if len(args) >= 2 && args[0] == "hardware" && args[1] == "probe" {
		return runHardwareProbe(args[2:], stdout, stderr, deps)
	}

	fmt.Fprintln(stderr, "usage: simplusctl version [--json] | doctor | provision-admin [options] | hardware probe [options]")
	return 2
}

func runProvisionAdministrator(args []string, stdout, stderr io.Writer, deps dependencies) int {
	flags := flag.NewFlagSet("provision-admin", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", os.Getenv("SIMPLUS_CONFIG"), "optional Simplus YAML configuration path")
	socketPath := flags.String("socket", os.Getenv("SIMPLUS_CONTROL_SOCKET"), "simplusd root control socket")
	username := flags.String("username", "simplus_admin", "administrator username")
	locale := flags.String("locale", "zh-CN", "instance default locale")
	jsonOutput := flags.Bool("json", false, "emit JSON output")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "provision-admin accepts no positional arguments")
		return 2
	}
	if deps.effectiveUID == nil || deps.effectiveUID() != 0 {
		fmt.Fprintln(stderr, "provision-admin must be run as root")
		return 1
	}
	resolvedSocket := strings.TrimSpace(*socketPath)
	if resolvedSocket == "" {
		cfg, err := config.Load(*configPath)
		if err != nil {
			fmt.Fprintf(stderr, "resolve control socket: %v\n", err)
			return 1
		}
		resolvedSocket = control.SocketPath(cfg.Storage.DataRoot)
	}
	rawPassword := make([]byte, 24)
	if _, err := io.ReadFull(rand.Reader, rawPassword); err != nil {
		fmt.Fprintf(stderr, "generate administrator password: %v\n", err)
		return 1
	}
	password := base64.RawURLEncoding.EncodeToString(rawPassword)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := deps.provisionAdmin(ctx, filepath.Clean(resolvedSocket), control.ProvisionAdministratorRequest{Username: *username, Password: password, Locale: *locale})
	if err != nil {
		fmt.Fprintf(stderr, "provision administrator: %v\n", err)
		return 1
	}
	output := struct {
		Created  bool   `json:"created"`
		Username string `json:"username,omitempty"`
		Password string `json:"password,omitempty"`
	}{Created: result.Created}
	if result.Created {
		output.Username, output.Password = strings.ToLower(strings.TrimSpace(*username)), password
	}
	if *jsonOutput {
		if err := json.NewEncoder(stdout).Encode(output); err != nil {
			return 1
		}
		return 0
	}
	if !result.Created {
		fmt.Fprintln(stdout, "administrator already configured; credentials unchanged")
		return 0
	}
	fmt.Fprintf(stdout, "Username: %s\nPassword: %s\n", output.Username, output.Password)
	return 0
}

func runBootstrapURL(args []string, stdout, stderr io.Writer, deps dependencies) int {
	flags := flag.NewFlagSet("bootstrap-url", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", os.Getenv("SIMPLUS_CONFIG"), "optional Simplus YAML configuration path")
	socketPath := flags.String("socket", os.Getenv("SIMPLUS_CONTROL_SOCKET"), "simplusd root control socket")
	baseURL := flags.String("base-url", envOrDefault("SIMPLUS_BOOTSTRAP_BASE_URL", "http://127.0.0.1:5173"), "browser origin used in the one-time URL")
	jsonOutput := flags.Bool("json", false, "emit a locale-neutral JSON result")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "bootstrap-url accepts no positional arguments")
		return 2
	}
	if deps.effectiveUID == nil || deps.effectiveUID() != 0 {
		fmt.Fprintln(stderr, "bootstrap-url must be run as root")
		return 1
	}

	resolvedSocket := strings.TrimSpace(*socketPath)
	if resolvedSocket == "" {
		cfg, err := config.Load(*configPath)
		if err != nil {
			fmt.Fprintf(stderr, "resolve control socket: %v\n", err)
			return 1
		}
		resolvedSocket = control.SocketPath(cfg.Storage.DataRoot)
	}
	if !filepath.IsAbs(resolvedSocket) {
		fmt.Fprintln(stderr, "control socket path must be absolute")
		return 2
	}
	browserURL, err := bootstrapBrowserURL(*baseURL, "placeholder")
	if err != nil {
		fmt.Fprintf(stderr, "invalid --base-url: %v\n", err)
		return 2
	}
	_ = browserURL

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	response, err := deps.generateBootstrap(ctx, filepath.Clean(resolvedSocket))
	if err != nil {
		fmt.Fprintf(stderr, "generate bootstrap URL: %v\n", err)
		return 1
	}
	browserURL, err = bootstrapBrowserURL(*baseURL, response.Code)
	if err != nil {
		fmt.Fprintf(stderr, "build bootstrap URL: %v\n", err)
		return 1
	}
	if *jsonOutput {
		result := struct {
			URL       string    `json:"url"`
			ExpiresAt time.Time `json:"expiresAt"`
		}{URL: browserURL, ExpiresAt: response.ExpiresAt}
		if err := json.NewEncoder(stdout).Encode(result); err != nil {
			fmt.Fprintf(stderr, "write bootstrap result: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintln(stdout, browserURL)
	fmt.Fprintf(stderr, "expires at %s\n", response.ExpiresAt.UTC().Format(time.RFC3339))
	return 0
}

func runHardwareProbe(args []string, stdout, stderr io.Writer, deps dependencies) int {
	flags := flag.NewFlagSet("hardware probe", flag.ContinueOnError)
	flags.SetOutput(stderr)
	socketPath := flags.String("socket", envOrDefault("SIMPLUS_AGENT_SOCKET", "/run/simplus/simplus-agent.sock"), "simplus-agent Unix socket")
	jsonOutput := flags.Bool("json", false, "emit the complete locale-neutral JSON report")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "hardware probe accepts no positional arguments")
		return 2
	}
	if deps.effectiveUID == nil || deps.effectiveUID() != 0 {
		fmt.Fprintln(stderr, "hardware probe must be run as root")
		return 1
	}
	if deps.probeHardware == nil {
		fmt.Fprintln(stderr, "hardware probe dependency is unavailable")
		return 1
	}
	if !filepath.IsAbs(*socketPath) {
		fmt.Fprintln(stderr, "agent socket path must be absolute")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	report, err := deps.probeHardware(ctx, filepath.Clean(*socketPath))
	if err != nil {
		fmt.Fprintf(stderr, "read-only hardware probe: %v\n", err)
		return 1
	}
	if *jsonOutput {
		if err := json.NewEncoder(stdout).Encode(report); err != nil {
			fmt.Fprintf(stderr, "write hardware probe report: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(stdout, "simplus-agent protocol v%d, topology generation %d, devices %d\n", report.Hello.ProtocolVersion, report.Snapshot.Generation, len(report.Snapshot.Devices))
	for _, device := range report.Snapshot.Devices {
		state := agentapi.ProbeStateDescriptorOnly
		for _, probe := range report.Probe.Devices {
			if probe.DeviceID == device.ID {
				state = probe.State
				break
			}
		}
		fmt.Fprintf(stdout, "- %s (%s %s:%s): %s\n", device.DisplayName, device.PhysicalPath, device.USB.VendorID, device.USB.ProductID, state)
	}
	return 0
}

func probeHardwareAgent(ctx context.Context, socketPath string) (hardwareProbeOutput, error) {
	client, err := agentapi.NewClient(socketPath)
	if err != nil {
		return hardwareProbeOutput{}, err
	}
	hello, err := client.Hello(ctx)
	if err != nil {
		return hardwareProbeOutput{}, err
	}
	snapshot, err := client.Snapshot(ctx, true)
	if err != nil {
		return hardwareProbeOutput{}, err
	}
	probe, err := client.Probe(ctx, agentapi.ProbeRequest{})
	if err != nil {
		return hardwareProbeOutput{}, err
	}
	if hello.Protocol != agentapi.ProtocolName || hello.ProtocolVersion != agentapi.ProtocolVersion || hello.AgentInstanceID == "" {
		return hardwareProbeOutput{}, errors.New("hardware agent protocol is incompatible")
	}
	if snapshot.AgentInstanceID != hello.AgentInstanceID || probe.AgentInstanceID != hello.AgentInstanceID {
		return hardwareProbeOutput{}, errors.New("hardware agent restarted during read-only probe; retry")
	}
	if probe.SnapshotGeneration != snapshot.Generation || probe.SnapshotRevision != snapshot.Revision {
		return hardwareProbeOutput{}, errors.New("hardware changed during read-only probe; retry against the new generation")
	}
	return hardwareProbeOutput{Hello: hello, Snapshot: snapshot, Probe: probe}, nil
}

func bootstrapBrowserURL(baseURL, code string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", err
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("must be an absolute HTTP(S) origin")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf("must not contain credentials, a path, query, or fragment")
	}
	parsed.Path = "/setup"
	parsed.RawPath = ""
	parsed.Fragment = url.Values{"bootstrap": []string{code}}.Encode()
	return parsed.String(), nil
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func supportStatus() string {
	if runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") {
		return "supported-runtime"
	}
	if runtime.GOOS == "darwin" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") {
		return "development-only"
	}
	return "unsupported"
}
