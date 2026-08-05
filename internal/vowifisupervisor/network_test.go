package vowifisupervisor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type recordedCommand struct {
	input []byte
	name  string
	args  []string
}

type recordingRunner struct {
	commands []recordedCommand
	failAt   int
}

func (runner *recordingRunner) Run(_ context.Context, input []byte, name string, args ...string) error {
	runner.commands = append(runner.commands, recordedCommand{input: append([]byte(nil), input...), name: name, args: append([]string(nil), args...)})
	if runner.failAt > 0 && len(runner.commands) == runner.failAt {
		return errors.New("fixture failure")
	}
	return nil
}

func TestNetworkPlanIsStableAndCountryPortIsDerived(t *testing.T) {
	request := StartRequest{LineID: "agent-line-0123456789abcdef0123456789abcdef", EgressMode: EgressMihomoCountry, CountryCode: "JP"}
	first, err := buildNetworkPlan(request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildNetworkPlan(request)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !first.valid() || first.ListenerPort != 20249 || first.Namespace == "" || len(first.HostInterface) > 15 || !strings.HasPrefix(first.Prefix, "169.254.") {
		t.Fatalf("unexpected plan: %#v", first)
	}
	if _, err := buildNetworkPlan(StartRequest{LineID: request.LineID, EgressMode: EgressMihomoCountry, CountryCode: "jp"}); !errors.Is(err, ErrRequestInvalid) {
		t.Fatalf("lowercase country error = %v", err)
	}
}

func TestCountryNetworkSetupUsesOnlyDerivedObjectsAndTPROXY(t *testing.T) {
	request := StartRequest{LineID: "agent-line-0123456789abcdef0123456789abcdef", EgressMode: EgressMihomoCountry, CountryCode: "JP"}
	plan, _ := buildNetworkPlan(request)
	runner := &recordingRunner{}
	manager := newNetworkManager()
	manager.runner = runner
	manager.netnsRoot = t.TempDir()
	if err := manager.Setup(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) < 12 {
		t.Fatalf("commands = %d", len(runner.commands))
	}
	nft := string(runner.commands[len(runner.commands)-1].input)
	for _, expected := range []string{plan.HostInterface, "127.0.0.1:20249", "meta l4proto tcp", "meta l4proto udp", " drop"} {
		if !strings.Contains(nft, expected) {
			t.Fatalf("nft program missing %q:\n%s", expected, nft)
		}
	}
	if strings.Contains(nft, request.LineID) {
		t.Fatalf("business ID leaked into kernel object program: %s", nft)
	}
}

func TestDirectNetworkFailsClosedWhenForwardingIsDisabled(t *testing.T) {
	request := StartRequest{LineID: "agent-line-fedcba9876543210fedcba9876543210", EgressMode: EgressDirect}
	plan, _ := buildNetworkPlan(request)
	runner := &recordingRunner{}
	manager := newNetworkManager()
	manager.runner = runner
	manager.netnsRoot = t.TempDir()
	manager.readFile = func(string) ([]byte, error) { return []byte("0\n"), nil }
	if err := manager.Setup(context.Background(), plan); err == nil || err.Error() != "DIRECT_FORWARDING_DISABLED" {
		t.Fatalf("setup error = %v", err)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("network changed before preflight: %#v", runner.commands)
	}
}

func TestNetworkManifestRejectsMutation(t *testing.T) {
	request := StartRequest{LineID: "agent-line-0123456789abcdef0123456789abcdef", EgressMode: EgressDirect}
	plan, _ := buildNetworkPlan(request)
	path := filepath.Join(t.TempDir(), "network.json")
	if err := writeNetworkManifest(path, plan); err != nil {
		t.Fatal(err)
	}
	loaded, err := readNetworkManifest(path)
	if err != nil || loaded != plan {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
	body, _ := os.ReadFile(path)
	body = []byte(strings.Replace(string(body), plan.TableName, "arbitrary", 1))
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readNetworkManifest(path); err == nil {
		t.Fatal("mutated manifest accepted")
	}
}

func TestCleanupIgnoresOnlyConfirmedMissingObjects(t *testing.T) {
	plan, _ := buildNetworkPlan(StartRequest{LineID: "agent-line-0123456789abcdef0123456789abcdef", EgressMode: EgressMihomoCountry, CountryCode: "GB"})
	runner := &recordingRunner{failAt: 1}
	manager := newNetworkManager()
	manager.runner = runner
	if err := manager.Cleanup(context.Background(), plan); !errors.Is(err, errNetworkCleanupFailed) {
		t.Fatalf("cleanup error = %v", err)
	}
	if len(runner.commands) != 5 {
		t.Fatalf("cleanup stopped early: %d commands", len(runner.commands))
	}

	missing := &missingRunner{}
	manager.runner = missing
	if err := manager.Cleanup(context.Background(), plan); err != nil {
		t.Fatalf("idempotent cleanup error = %v", err)
	}
}

type missingRunner struct{}

func (*missingRunner) Run(context.Context, []byte, string, ...string) error {
	return errNetworkObjectMissing
}

func TestNetworkObjectMissingClassification(t *testing.T) {
	for _, output := range []string{"RTNETLINK answers: No such file or directory", "Cannot find device svh1234", "Error: Could not process rule: No such file or directory"} {
		if !networkObjectMissing(output) {
			t.Fatalf("not classified: %q", output)
		}
	}
	if networkObjectMissing("Operation not permitted") {
		t.Fatal("permission failure classified as missing")
	}
}
