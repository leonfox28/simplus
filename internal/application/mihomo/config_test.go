package mihomo

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	domain "github.com/leonfox28/simplus/internal/domain/mihomo"
	"gopkg.in/yaml.v3"
)

const configTestSubscriptionID = "subscription_abcdefghijklmnopqrstuv"

type configStoreStub struct {
	sub               domain.Subscription
	nodes             []domain.Node
	selected, running string
}

func (stub *configStoreStub) ReadMihomoSubscription(context.Context, string) (domain.Subscription, bool, error) {
	return stub.sub, true, nil
}
func (stub *configStoreStub) ListMihomoSubscriptionNodes(context.Context, string) ([]domain.Node, error) {
	return stub.nodes, nil
}
func (stub *configStoreStub) ReadMihomoRuntimeSelection(context.Context) (string, string, error) {
	return stub.selected, stub.running, nil
}
func (stub *configStoreStub) WriteMihomoSelectedSubscription(_ context.Context, id string, _ time.Time) error {
	stub.selected = id
	return nil
}
func (stub *configStoreStub) WriteMihomoRunningSubscription(_ context.Context, id string, _ time.Time) error {
	stub.running = id
	return nil
}

type coreStatusStub struct{ status CoreStatus }

func (stub coreStatusStub) Status() (CoreStatus, error) { return stub.status, nil }

func readyConfigFixture(root string) (*ConfigManager, *configStoreStub, []domain.Node) {
	proxy := "name: \"🇬🇧 英国 01\"\ntype: anytls\nserver: example.com\nport: 443\npassword: test\nudp: true\n"
	nodes := []domain.Node{{SubscriptionID: configTestSubscriptionID, ID: "node_abcdefghijklmnopqrstuv", DisplayName: "🇬🇧 英国 01", Kind: "anytls", CountryCode: "GB", CountryName: "英国", ProxyYAML: proxy}}
	store := &configStoreStub{sub: domain.Subscription{ID: configTestSubscriptionID, Enabled: true, LastRefreshStatus: "success"}, nodes: nodes}
	manager := &ConfigManager{Root: root, Store: store, Core: coreStatusStub{CoreStatus{Installed: true, Version: "v1.19.29", BinaryPath: "/installed/mihomo"}}, Now: func() time.Time { return time.Date(2026, 8, 4, 3, 0, 0, 0, time.UTC) }, ControllerAddress: "127.0.0.1:19090"}
	return manager, store, nodes
}

func TestSubscriptionArtifactValidatesBeforeAtomicPublicationAndSelectionDoesNotStart(t *testing.T) {
	manager, store, nodes := readyConfigFixture(t.TempDir())
	manager.Run = func(_ context.Context, path string, args ...string) ([]byte, error) {
		if path != "/installed/mihomo" || !strings.Contains(strings.Join(args, " "), "-t -f") {
			t.Fatalf("probe = %s %v", path, args)
		}
		return []byte("configuration test is successful"), nil
	}
	metadata, err := manager.BuildSubscription(context.Background(), configTestSubscriptionID, []byte("proxies: fixture\n"), nodes)
	if err != nil || metadata.ConfigSHA256 == "" || len(metadata.Countries) != 1 || metadata.Countries[0].Code != "GB" {
		t.Fatalf("metadata=%#v err=%v", metadata, err)
	}
	_, configPath, err := manager.Artifact(configTestSubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, required := range []string{"type: tproxy", "name: country-gb", "英国", "MATCH,REJECT", "type: anytls", "enhanced-mode: redir-host", "respect-rules: true", "https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query"} {
		if !strings.Contains(text, required) {
			t.Fatalf("generated config lacks %q:\n%s", required, text)
		}
	}
	var document map[string]any
	if err := yaml.Unmarshal(body, &document); err != nil {
		t.Fatal(err)
	}
	groups, ok := document["proxy-groups"].([]any)
	if !ok || len(groups) != 2 {
		t.Fatalf("proxy-groups=%#v", document["proxy-groups"])
	}
	dnsGroup, ok := groups[0].(map[string]any)
	if !ok || dnsGroup["name"] != "🌐 DNS" || dnsGroup["type"] != "url-test" || dnsGroup["lazy"] != false {
		t.Fatalf("DNS group=%#v", groups[0])
	}
	members, ok := dnsGroup["proxies"].([]any)
	if !ok || len(members) != len(nodes) || members[0] != "🇬🇧 英国 01" {
		t.Fatalf("DNS members=%#v", dnsGroup["proxies"])
	}
	proxies, ok := document["proxies"].([]any)
	if !ok || len(proxies) != 1 || proxies[0].(map[string]any)["name"] != "🇬🇧 英国 01" {
		t.Fatalf("generated proxies changed upstream names: %#v", document["proxies"])
	}
	rules, ok := document["rules"].([]any)
	if !ok || len(rules) != 3 || rules[0] != "IP-CIDR,1.1.1.1/32,🌐 DNS,no-resolve" || rules[1] != "IP-CIDR,8.8.8.8/32,🌐 DNS,no-resolve" || rules[2] != "MATCH,REJECT" {
		t.Fatalf("rules=%#v", document["rules"])
	}
	status, err := manager.Select(context.Background(), configTestSubscriptionID)
	if err != nil || !status.Launchable || store.selected != configTestSubscriptionID || status.RunningSubscriptionID != "" {
		t.Fatalf("status=%#v selected=%q err=%v", status, store.selected, err)
	}
}

func TestGeneratedSubscriptionRejectsDuplicateUpstreamNamesInsteadOfRenaming(t *testing.T) {
	proxy := "name: duplicate\ntype: anytls\nserver: example.com\nport: 443\npassword: test\n"
	nodes := []domain.Node{
		{ID: "node_aaaaaaaaaaaaaaaaaaaaaa", DisplayName: "duplicate", CountryCode: "GB", CountryName: "英国", ProxyYAML: proxy},
		{ID: "node_bbbbbbbbbbbbbbbbbbbbbb", DisplayName: "duplicate", CountryCode: "US", CountryName: "美国", ProxyYAML: proxy},
	}
	if _, _, err := generateSubscriptionConfig(nodes, "127.0.0.1:19090", "", ""); !errors.Is(err, ErrConfigNotReady) {
		t.Fatalf("err=%v", err)
	}
}

func TestGeneratedSubscriptionIncludesPrivateLANZashboardController(t *testing.T) {
	manager, _, nodes := readyConfigFixture(t.TempDir())
	manager.Run = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("configuration test is successful"), nil
	}
	manager.ConfigureDashboard(DashboardStatus{ControllerAddress: "192.168.50.10:19090", Secret: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ", URL: "http://192.168.50.10:19090/ui/", Available: true})
	if _, err := manager.BuildSubscription(context.Background(), configTestSubscriptionID, []byte("proxies: fixture\n"), nodes); err != nil {
		t.Fatal(err)
	}
	_, path, err := manager.Artifact(configTestSubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, required := range []string{"external-controller: 192.168.50.10:19090", "external-ui: ui", "secret: abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"} {
		if !strings.Contains(text, required) {
			t.Fatalf("generated dashboard config lacks %q:\n%s", required, text)
		}
	}
}

func TestSelectionRebuildsArtifactForWildcardDashboardWithoutRestartingRuntime(t *testing.T) {
	manager, _, nodes := readyConfigFixture(t.TempDir())
	manager.Run = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("configuration test is successful"), nil
	}
	secret := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"
	manager.ConfigureDashboard(DashboardStatus{ControllerAddress: "192.168.50.10:19090", Secret: secret, Available: true})
	first, err := manager.BuildSubscription(context.Background(), configTestSubscriptionID, []byte("proxies: fixture\n"), nodes)
	if err != nil {
		t.Fatal(err)
	}
	manager.ConfigureDashboard(DashboardStatus{ControllerAddress: "0.0.0.0:19090", Secret: secret, Available: true})
	if _, err := manager.Select(context.Background(), configTestSubscriptionID); err != nil {
		t.Fatal(err)
	}
	second, path, err := manager.Artifact(configTestSubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	if second.Version == first.Version {
		t.Fatal("controller change did not create a new immutable artifact")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "external-controller: 0.0.0.0:19090") {
		t.Fatalf("rebuilt config does not use wildcard controller:\n%s", body)
	}
}

func TestArtifactValidationFailureKeepsPreviousVersionAndCannotBeSelected(t *testing.T) {
	manager, store, nodes := readyConfigFixture(t.TempDir())
	manager.Run = func(context.Context, string, ...string) ([]byte, error) { return nil, nil }
	first, err := manager.BuildSubscription(context.Background(), configTestSubscriptionID, []byte("first"), nodes)
	if err != nil {
		t.Fatal(err)
	}
	manager.Run = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("parse failed"), errors.New("exit 1")
	}
	if _, err := manager.BuildSubscription(context.Background(), configTestSubscriptionID, []byte("second"), nodes); !errors.Is(err, ErrConfigValidationFailed) {
		t.Fatalf("err=%v", err)
	}
	current, _, err := manager.Artifact(configTestSubscriptionID)
	if err != nil || current.Version != first.Version {
		t.Fatalf("current=%#v err=%v", current, err)
	}
	if store.selected != "" {
		t.Fatalf("failed artifact selected: %q", store.selected)
	}
}

func TestGeneratedConfigPassesInstalledMihomoWhenRequested(t *testing.T) {
	binary := os.Getenv("SIMPLUS_TEST_MIHOMO_BINARY")
	if binary == "" {
		t.Skip("set SIMPLUS_TEST_MIHOMO_BINARY for installed-core validation")
	}
	manager, _, nodes := readyConfigFixture(t.TempDir())
	manager.Core = coreStatusStub{CoreStatus{Installed: true, Version: "v1.19.29", BinaryPath: binary}}
	manager.Run = runCommand
	if _, err := manager.BuildSubscription(context.Background(), configTestSubscriptionID, []byte("proxies: fixture\n"), nodes); err != nil {
		t.Fatalf("installed core rejected generated config: %v", err)
	}
}
