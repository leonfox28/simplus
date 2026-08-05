package lineegress

import (
	"context"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/application/inventory"
	mihomoapp "github.com/leonfox28/simplus/internal/application/mihomo"
	"github.com/leonfox28/simplus/internal/domain/accessmode"
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

func TestCountryBindingUsesCurrentSubscriptionWithoutRewritingMihomo(t *testing.T) {
	store := &memoryStore{
		bindings: map[string]domain.Binding{}, selected: "subscription_AAAAAAAAAAAAAAAAAAAAAA", running: "subscription_AAAAAAAAAAAAAAAAAAAAAA",
		nodes: map[string][]mihomodomain.Node{
			"subscription_AAAAAAAAAAAAAAAAAAAAAA": {{CountryCode: "GB", CountryName: "英国"}},
			"subscription_BBBBBBBBBBBBBBBBBBBBBB": {{CountryCode: "US", CountryName: "美国"}},
		},
	}
	line := inventory.Line{ID: "agent-line-123", AccessMode: accessmode.HostVoWiFiOnly, Capabilities: hardware.Capabilities{SIMAccess: true}}
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

func TestLineEgressFailsClosedOutsideHostVoWiFiAndWhenMihomoStops(t *testing.T) {
	store := &memoryStore{
		bindings: map[string]domain.Binding{}, selected: "subscription_AAAAAAAAAAAAAAAAAAAAAA", running: "subscription_AAAAAAAAAAAAAAAAAAAAAA",
		nodes: map[string][]mihomodomain.Node{"subscription_AAAAAAAAAAAAAAAAAAAAAA": {{CountryCode: "GB", CountryName: "英国"}}},
	}
	line := inventory.Line{ID: "agent-line-123", AccessMode: accessmode.HoldRFOff, Capabilities: hardware.Capabilities{SIMAccess: true}}
	service := New(store, staticInventory{topology: inventory.Topology{Lines: []inventory.Line{line}}}, staticRuntime{state: "stopped"})
	if _, err := service.Put(context.Background(), line.ID, domain.ModeMihomoCountry, "GB"); err != ErrLineMode {
		t.Fatalf("non-Host VoWiFi put error = %v", err)
	}
	line.AccessMode = accessmode.HostVoWiFiOnly
	service.Inventory = staticInventory{topology: inventory.Topology{Lines: []inventory.Line{line}}}
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
