package sqlite

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestResourceGroupLeaseFencesConflictsAndReplays(t *testing.T) {
	ctx := context.Background()
	set, err := OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	now := time.Unix(1_700_000_000, 0)
	request := leaseRequest("lease-operation-0001", "operation-1", ResourceLeaseOperation, now)
	lease, replayed, err := set.AcquireResourceGroupLease(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if replayed || lease.FencingToken != 1 {
		t.Fatalf("lease = %#v replayed=%t", lease, replayed)
	}
	replayedLease, replayed, err := set.AcquireResourceGroupLease(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed || replayedLease != lease {
		t.Fatalf("replayed lease = %#v replayed=%t", replayedLease, replayed)
	}
	changed := request
	changed.Holder = "different-holder"
	if _, _, err := set.AcquireResourceGroupLease(ctx, changed); !errors.Is(err, ErrResourceLeaseReplay) {
		t.Fatalf("changed replay error = %v", err)
	}
	call := leaseRequest("lease-call-00000001", "call-1", ResourceLeaseCall, now)
	if _, _, err := set.AcquireResourceGroupLease(ctx, call); !errors.Is(err, ErrResourceGroupBusy) {
		t.Fatalf("call during operation error = %v", err)
	}
	if err := set.ReleaseResourceGroupLease(ctx, lease.LeaseID, lease.FencingToken+1); !errors.Is(err, ErrResourceLeaseMissing) {
		t.Fatalf("stale release error = %v", err)
	}
	if err := set.ReleaseResourceGroupLease(ctx, lease.LeaseID, lease.FencingToken); err != nil {
		t.Fatal(err)
	}
	callLease, _, err := set.AcquireResourceGroupLease(ctx, call)
	if err != nil {
		t.Fatal(err)
	}
	if callLease.FencingToken != 2 {
		t.Fatalf("call fencing token = %d", callLease.FencingToken)
	}
	if _, _, err := set.AcquireResourceGroupLease(ctx, leaseRequest("lease-call-00000002", "call-2", ResourceLeaseCall, now)); !errors.Is(err, ErrResourceGroupBusy) {
		t.Fatalf("second call error = %v", err)
	}
	newGeneration := leaseRequest("lease-operation-0002", "operation-2", ResourceLeaseOperation, now)
	newGeneration.ResourceGroupGeneration = 2
	newLease, _, err := set.AcquireResourceGroupLease(ctx, newGeneration)
	if err != nil {
		t.Fatal(err)
	}
	if newLease.FencingToken != 3 || newLease.ResourceGroupGeneration != 2 {
		t.Fatalf("new-generation lease = %#v", newLease)
	}
	staleGeneration := leaseRequest("lease-operation-0003", "operation-3", ResourceLeaseOperation, now)
	if _, _, err := set.AcquireResourceGroupLease(ctx, staleGeneration); !errors.Is(err, ErrResourceLeaseStaleGeneration) {
		t.Fatalf("stale-generation acquire error = %v", err)
	}
	active, err := set.ActiveResourceGroupLeases(ctx, "group-1", now)
	if err != nil || len(active) != 1 || active[0].LeaseID != newLease.LeaseID {
		t.Fatalf("active after stale-generation acquire = %#v err=%v", active, err)
	}
	if err := set.ReleaseResourceGroupLease(ctx, callLease.LeaseID, callLease.FencingToken); !errors.Is(err, ErrResourceLeaseMissing) {
		t.Fatalf("stale-generation release error = %v", err)
	}
}

func TestResourceGroupLeaseRenewExpiryAndRestart(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "db")
	set, err := OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	lease, _, err := set.AcquireResourceGroupLease(ctx, leaseRequest("lease-operation-0001", "operation-1", ResourceLeaseOperation, now))
	if err != nil {
		t.Fatal(err)
	}
	renewed, err := set.RenewResourceGroupLease(ctx, lease.LeaseID, lease.FencingToken, now.Add(10*time.Second), now.Add(90*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !renewed.ExpiresAt.Equal(now.Add(90 * time.Second)) {
		t.Fatalf("renewed expiry = %s", renewed.ExpiresAt)
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}
	set, err = OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	active, err := set.ActiveResourceGroupLeases(ctx, "group-1", now.Add(20*time.Second))
	if err != nil || len(active) != 1 || active[0].FencingToken != lease.FencingToken {
		t.Fatalf("active after restart = %#v err=%v", active, err)
	}
	expiredReplay := leaseRequest("lease-operation-retry", "operation-1", ResourceLeaseOperation, now.Add(91*time.Second))
	if _, _, err := set.AcquireResourceGroupLease(ctx, expiredReplay); !errors.Is(err, ErrResourceLeaseClosed) {
		t.Fatalf("expired operation replay error = %v", err)
	}
	next, _, err := set.AcquireResourceGroupLease(ctx, leaseRequest("lease-operation-0002", "operation-2", ResourceLeaseOperation, now.Add(91*time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	if next.FencingToken != lease.FencingToken+1 {
		t.Fatalf("next fence = %d", next.FencingToken)
	}
	if _, err := set.RenewResourceGroupLease(ctx, lease.LeaseID, lease.FencingToken, now.Add(91*time.Second), now.Add(120*time.Second)); !errors.Is(err, ErrResourceLeaseMissing) {
		t.Fatalf("expired renewal error = %v", err)
	}
}

func TestResourceGroupLeaseConcurrentAcquisitionHasOneWinner(t *testing.T) {
	ctx := context.Background()
	set, err := OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	now := time.Unix(1_700_000_000, 0)
	const contenders = 12
	start := make(chan struct{})
	var wait sync.WaitGroup
	var mu sync.Mutex
	winners := 0
	busy := 0
	for index := 0; index < contenders; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			request := leaseRequest(fmt.Sprintf("lease-operation-%04d", index), fmt.Sprintf("operation-%d", index), ResourceLeaseOperation, now)
			_, _, err := set.AcquireResourceGroupLease(ctx, request)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				winners++
			case errors.Is(err, ErrResourceGroupBusy):
				busy++
			default:
				t.Errorf("contender %d error = %v", index, err)
			}
		}(index)
	}
	close(start)
	wait.Wait()
	if winners != 1 || busy != contenders-1 {
		t.Fatalf("winners=%d busy=%d", winners, busy)
	}
}

func leaseRequest(leaseID, operationID, kind string, now time.Time) ResourceLeaseAcquire {
	return ResourceLeaseAcquire{
		LeaseID: leaseID, OperationID: operationID, ResourceGroupID: "group-1", Kind: kind,
		Purpose: "test-operation", Holder: "test-holder", ResourceGroupGeneration: 1,
		MaxActiveCalls: 1, MaxConcurrentOperations: 1, Now: now, ExpiresAt: now.Add(time.Minute),
	}
}
