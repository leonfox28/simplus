package resourcelease

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/application/inventory"
	"github.com/leonfox28/simplus/internal/domain/hardware"
	storage "github.com/leonfox28/simplus/internal/storage/sqlite"
)

type topologyProviderFunc func(context.Context) (inventory.Topology, error)

func (provider topologyProviderFunc) Topology(ctx context.Context) (inventory.Topology, error) {
	return provider(ctx)
}

func TestServiceBindsLeaseToCurrentTopologyGeneration(t *testing.T) {
	ctx := context.Background()
	set, err := storage.OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	service := New(inventory.NewSimulator(), set)
	service.now = func() time.Time { return time.Unix(1_700_000_000, 0) }

	request := AcquireRequest{
		OperationID: "scan-1", ResourceGroupID: "simulator-resource-group-1", ExpectedGroupGeneration: 1,
		Kind: storage.ResourceLeaseOperation, Purpose: "network-scan", Holder: "session-1", TTL: time.Minute,
	}
	lease, replayed, err := service.Acquire(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if replayed || lease.ResourceGroupGeneration != 1 || lease.FencingToken != 1 {
		t.Fatalf("lease = %#v replayed=%t", lease, replayed)
	}
	replayedLease, replayed, err := service.Acquire(ctx, request)
	if err != nil || !replayed || replayedLease != lease {
		t.Fatalf("replay = %#v replayed=%t err=%v", replayedLease, replayed, err)
	}
	request.OperationID = "scan-2"
	request.ExpectedGroupGeneration = 2
	if _, _, err := service.Acquire(ctx, request); !errors.Is(err, ErrResourceGeneration) {
		t.Fatalf("generation error = %v", err)
	}
	request.ExpectedGroupGeneration = 1
	request.ResourceGroupID = "missing-group"
	if _, _, err := service.Acquire(ctx, request); !errors.Is(err, ErrResourceGroupNotFound) {
		t.Fatalf("missing group error = %v", err)
	}
	request.ResourceGroupID = "simulator-resource-group-1"
	request.Purpose = "arbitrary-command"
	if _, _, err := service.Acquire(ctx, request); !errors.Is(err, ErrLeaseRequestInvalid) {
		t.Fatalf("unknown purpose error = %v", err)
	}

	topology, err := inventory.NewSimulator().Topology(ctx)
	if err != nil {
		t.Fatal(err)
	}
	topology.ResourceGroups[0].Resources = []string{"radio-control", "sim-access", "voice-media"}
	limited := New(topologyProviderFunc(func(context.Context) (inventory.Topology, error) { return topology, nil }), set)
	limited.now = service.now
	request.OperationID = "sms-1"
	request.Purpose = "sms-storage"
	if _, _, err := limited.Acquire(ctx, request); !errors.Is(err, ErrResourceCapability) {
		t.Fatalf("missing capability error = %v", err)
	}
}

func TestServiceGenerationChangeFencesExistingLease(t *testing.T) {
	ctx := context.Background()
	set, err := storage.OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	topology, err := inventory.NewSimulator().Topology(ctx)
	if err != nil {
		t.Fatal(err)
	}
	service := New(topologyProviderFunc(func(context.Context) (inventory.Topology, error) { return topology, nil }), set)
	service.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	first, _, err := service.Acquire(ctx, AcquireRequest{
		OperationID: "scan-1", ResourceGroupID: "simulator-resource-group-1", ExpectedGroupGeneration: 1,
		Kind: storage.ResourceLeaseOperation, Purpose: "network-scan", Holder: "session-1", TTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	topology.Generation = 2
	topology.ResourceGroups[0].Generation = 2
	second, _, err := service.Acquire(ctx, AcquireRequest{
		OperationID: "scan-2", ResourceGroupID: "simulator-resource-group-1", ExpectedGroupGeneration: 2,
		Kind: storage.ResourceLeaseOperation, Purpose: "network-scan", Holder: "session-2", TTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.FencingToken <= first.FencingToken || second.ResourceGroupGeneration != 2 {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	active, err := service.Active(ctx, "simulator-resource-group-1")
	if err != nil || len(active) != 1 || active[0] != second {
		t.Fatalf("active=%#v err=%v", active, err)
	}
	if err := service.Release(ctx, first); !errors.Is(err, storage.ErrResourceLeaseMissing) {
		t.Fatalf("old generation release error = %v", err)
	}
}

func TestServiceRenewsAndReleasesWithFence(t *testing.T) {
	ctx := context.Background()
	set, err := storage.OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	topology, err := inventory.NewSimulator().Topology(ctx)
	if err != nil {
		t.Fatal(err)
	}
	topology.ModemFunctions[0].Capabilities.CellularVoice = true
	topology.ModemFunctions[0].Capabilities.DigitalVoiceMedia = true
	topology.Lines[0].Capabilities.CellularVoice = true
	topology.Lines[0].Capabilities.DigitalVoiceMedia = true
	topology.ResourceGroups[0].Resources = append(topology.ResourceGroups[0].Resources, hardware.ResourceVoiceMedia)
	topology.ResourceGroups[0].MaxActiveCalls = 1
	service := New(topologyProviderFunc(func(context.Context) (inventory.Topology, error) { return topology, nil }), set)
	now := time.Unix(1_700_000_000, 0)
	service.now = func() time.Time { return now }
	lease, _, err := service.Acquire(ctx, AcquireRequest{
		OperationID: "call-1", ResourceGroupID: "simulator-resource-group-1", ExpectedGroupGeneration: 1,
		Kind: storage.ResourceLeaseCall, Purpose: "cellular-call", Holder: "call-session-1", TTL: 15 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now.Add(5 * time.Second) }
	renewed, err := service.Renew(ctx, lease, 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !renewed.ExpiresAt.Equal(now.Add(20 * time.Second)) {
		t.Fatalf("expiry = %s", renewed.ExpiresAt)
	}
	stale := renewed
	stale.FencingToken++
	if err := service.Release(ctx, stale); !errors.Is(err, storage.ErrResourceLeaseMissing) {
		t.Fatalf("stale release error = %v", err)
	}
	if err := service.Release(ctx, renewed); err != nil {
		t.Fatal(err)
	}
	active, err := service.Active(ctx, "simulator-resource-group-1")
	if err != nil || len(active) != 0 {
		t.Fatalf("active = %#v err=%v", active, err)
	}
}
