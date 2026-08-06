package lineegress

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/application/inventory"
	mihomoapp "github.com/leonfox28/simplus/internal/application/mihomo"
	"github.com/leonfox28/simplus/internal/domain/hardware"
	domain "github.com/leonfox28/simplus/internal/domain/lineegress"
	mihomodomain "github.com/leonfox28/simplus/internal/domain/mihomo"
)

type memoryStore struct {
	bindings          map[string]domain.Binding
	selected, running string
	nodes             map[string][]mihomodomain.Node
}

func (store *memoryStore) ListLineEgressBindings(context.Context) ([]domain.Binding, error) {
	result := make([]domain.Binding, 0, len(store.bindings))
	for _, binding := range store.bindings {
		result = append(result, binding)
	}
	return result, nil
}

func (store *memoryStore) UpsertLineEgressBinding(_ context.Context, binding domain.Binding) error {
	store.bindings[binding.LineID] = binding
	return nil
}

func (store *memoryStore) ReadMihomoRuntimeSelection(context.Context) (string, string, error) {
	return store.selected, store.running, nil
}

func (store *memoryStore) ListMihomoSubscriptionNodes(_ context.Context, subscriptionID string) ([]mihomodomain.Node, error) {
	return store.nodes[subscriptionID], nil
}

type staticInventory struct{ topology inventory.Topology }

func (source staticInventory) Topology(context.Context) (inventory.Topology, error) {
	return source.topology, nil
}

type staticRuntime struct{ state string }

func (runtime staticRuntime) Status(context.Context) (mihomoapp.RuntimeStatus, error) {
	return mihomoapp.RuntimeStatus{State: runtime.state}, nil
}

type failingRuntime struct{}

func (failingRuntime) Status(context.Context) (mihomoapp.RuntimeStatus, error) {
	return mihomoapp.RuntimeStatus{}, errors.New("Mihomo status unavailable")
}

func TestCountryBindingUsesCurrentSubscriptionWithoutRewritingMihomo(t *testing.T) {
	store := &memoryStore{
		bindings: map[string]domain.Binding{}, selected: "subscription_AAAAAAAAAAAAAAAAAAAAAA", running: "subscription_AAAAAAAAAAAAAAAAAAAAAA",
		nodes: map[string][]mihomodomain.Node{
			"subscription_AAAAAAAAAAAAAAAAAAAAAA": {{CountryCode: "GB", CountryName: "英国"}},
			"subscription_BBBBBBBBBBBBBBBBBBBBBB": {{CountryCode: "US", CountryName: "美国"}},
		},
	}
	line := inventory.Line{ID: "line_AQEBAQEBAQEBAQEBAQEBAQ", Capabilities: hardware.Capabilities{SIMAccess: true, HostVoWiFiAuth: true}}
	service := New(store, staticInventory{topology: inventory.Topology{Lines: []inventory.Line{line}}}, staticRuntime{state: "running"})
	service.Now = func() time.Time { return time.Unix(100, 0).UTC() }

	created, err := service.Put(context.Background(), line.ID, domain.ModeMihomoCountry, "GB")
	if err != nil {
		t.Fatal(err)
	}
	if !created.Ready || created.CountryName != "英国" || created.ListenerPort != mihomoapp.CountryListenerPort("GB") {
		t.Fatalf("created binding = %#v", created)
	}
	store.selected = "subscription_BBBBBBBBBBBBBBBBBBBBBB"
	store.running = "subscription_BBBBBBBBBBBBBBBBBBBBBB"
	items, err := service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Ready || items[0].ReadinessReason != "COUNTRY_NOT_FOUND" || items[0].CountryCode != "GB" {
		t.Fatalf("binding after subscription switch = %#v", items)
	}
	if store.bindings[line.ID].CountryCode != "GB" {
		t.Fatalf("stored binding changed with subscription: %#v", store.bindings[line.ID])
	}
}

func TestLineEgressFailsClosedWhenUnsupportedUnconfiguredOrMihomoStops(t *testing.T) {
	store := &memoryStore{
		bindings: map[string]domain.Binding{}, selected: "subscription_AAAAAAAAAAAAAAAAAAAAAA", running: "subscription_AAAAAAAAAAAAAAAAAAAAAA",
		nodes: map[string][]mihomodomain.Node{"subscription_AAAAAAAAAAAAAAAAAAAAAA": {{CountryCode: "GB", CountryName: "英国"}}},
	}
	line := inventory.Line{ID: "line_AQEBAQEBAQEBAQEBAQEBAQ", Capabilities: hardware.Capabilities{SIMAccess: true}}
	service := New(store, staticInventory{topology: inventory.Topology{Lines: []inventory.Line{line}}}, staticRuntime{state: "stopped"})
	items, err := service.List(context.Background())
	if err != nil || len(items) != 1 || items[0].ReadinessReason != "LINE_VOWIFI_UNSUPPORTED" || items[0].Mode != domain.ModeUnconfigured {
		t.Fatalf("unsupported listing = %#v, error = %v", items, err)
	}
	if _, err := service.Put(context.Background(), line.ID, domain.ModeMihomoCountry, "GB"); err != ErrLineUnsupported {
		t.Fatalf("unsupported put error = %v", err)
	}
	line.Capabilities.HostVoWiFiAuth = true
	service.Inventory = staticInventory{topology: inventory.Topology{Lines: []inventory.Line{line}}}
	items, err = service.List(context.Background())
	if err != nil || items[0].Ready || items[0].ReadinessReason != "EGRESS_NOT_CONFIGURED" || items[0].Mode != domain.ModeUnconfigured {
		t.Fatalf("unconfigured listing = %#v, error = %v", items, err)
	}
	item, err := service.Put(context.Background(), line.ID, domain.ModeMihomoCountry, "GB")
	if err != nil {
		t.Fatal(err)
	}
	if item.Ready || item.ReadinessReason != "MIHOMO_NOT_RUNNING" {
		t.Fatalf("stopped Mihomo binding = %#v", item)
	}
	direct, err := service.Put(context.Background(), line.ID, domain.ModeDirect, "")
	if err != nil || !direct.Ready || direct.ReadinessReason != "READY" {
		t.Fatalf("direct binding = %#v, err = %v", direct, err)
	}
}

func TestDirectAndUnconfiguredEgressDoNotDependOnMihomoRuntime(t *testing.T) {
	store := &memoryStore{bindings: map[string]domain.Binding{}, nodes: map[string][]mihomodomain.Node{}}
	line := inventory.Line{
		ID:           "line_AQEBAQEBAQEBAQEBAQEBAQ",
		Capabilities: hardware.Capabilities{SIMAccess: true, HostVoWiFiAuth: true},
	}
	service := New(store, staticInventory{topology: inventory.Topology{Lines: []inventory.Line{line}}}, failingRuntime{})
	items, err := service.List(t.Context())
	if err != nil || len(items) != 1 || items[0].Mode != domain.ModeUnconfigured || items[0].ReadinessReason != "EGRESS_NOT_CONFIGURED" {
		t.Fatalf("unconfigured items=%#v error=%v", items, err)
	}
	direct, err := service.Put(t.Context(), line.ID, domain.ModeDirect, "")
	if err != nil || !direct.Ready || direct.Mode != domain.ModeDirect {
		t.Fatalf("direct=%#v error=%v", direct, err)
	}
}
