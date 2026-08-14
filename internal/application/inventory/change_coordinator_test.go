package inventory

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/application/realtime"
)

const (
	agentInstanceA = "01234567-89ab-cdef-0123-456789abcdef"
	agentInstanceB = "fedcba98-7654-3210-fedc-ba9876543210"
)

type agentSnapshotOutcome struct {
	snapshot agentapi.Snapshot
	err      error
}

type agentChangeOutcome struct {
	response agentapi.ChangeResponse
	err      error
	after    func()
}

type fakeAgentChangeSource struct {
	snapshots       []agentSnapshotOutcome
	changes         []agentChangeOutcome
	snapshotCalls   int
	changeCalls     int
	refreshValues   []bool
	changeInstances []string
	changeAfter     []uint64
	changeTimeouts  []int
}

func (fake *fakeAgentChangeSource) Snapshot(_ context.Context, refresh bool) (agentapi.Snapshot, error) {
	fake.refreshValues = append(fake.refreshValues, refresh)
	if fake.snapshotCalls >= len(fake.snapshots) {
		return agentapi.Snapshot{}, errors.New("unexpected snapshot call")
	}
	outcome := fake.snapshots[fake.snapshotCalls]
	fake.snapshotCalls++
	return outcome.snapshot, outcome.err
}

func (fake *fakeAgentChangeSource) Changes(_ context.Context, instanceID string, after uint64, timeoutSeconds int) (agentapi.ChangeResponse, error) {
	fake.changeInstances = append(fake.changeInstances, instanceID)
	fake.changeAfter = append(fake.changeAfter, after)
	fake.changeTimeouts = append(fake.changeTimeouts, timeoutSeconds)
	if fake.changeCalls >= len(fake.changes) {
		return agentapi.ChangeResponse{}, errors.New("unexpected change call")
	}
	outcome := fake.changes[fake.changeCalls]
	fake.changeCalls++
	if outcome.after != nil {
		outcome.after()
	}
	return outcome.response, outcome.err
}

type agentTestPublication struct {
	topics    []realtime.Topic
	attention realtime.Attention
}

type fakeAgentChangePublisher struct {
	publications []agentTestPublication
}

func (fake *fakeAgentChangePublisher) Publish(topics []realtime.Topic, attention realtime.Attention) {
	fake.publications = append(fake.publications, agentTestPublication{
		topics: append([]realtime.Topic(nil), topics...), attention: attention,
	})
}

func TestNewAgentChangeCoordinatorRequiresDependencies(t *testing.T) {
	source := &fakeAgentChangeSource{}
	publisher := &fakeAgentChangePublisher{}
	if _, err := NewAgentChangeCoordinator(nil, publisher); !errors.Is(err, ErrAgentChangeCoordinatorConfiguration) {
		t.Fatalf("missing source error = %v", err)
	}
	if _, err := NewAgentChangeCoordinator(source, nil); !errors.Is(err, ErrAgentChangeCoordinatorConfiguration) {
		t.Fatalf("missing publisher error = %v", err)
	}
}

func TestAgentChangeCoordinatorPublishesExplicitChange(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	initial := agentSnapshot(agentInstanceA, 7)
	changed := agentSnapshot(agentInstanceA, 8)
	source := &fakeAgentChangeSource{
		snapshots: []agentSnapshotOutcome{{snapshot: initial}},
		changes: []agentChangeOutcome{{
			response: agentapi.ChangeResponse{Changed: true, Snapshot: changed},
			after:    cancel,
		}},
	}
	publisher := &fakeAgentChangePublisher{}
	coordinator, err := NewAgentChangeCoordinator(source, publisher)
	if err != nil {
		t.Fatal(err)
	}
	coordinator.Run(ctx, nil)

	assertSingleInventoryPublication(t, publisher.publications)
	if !reflect.DeepEqual(source.refreshValues, []bool{false}) {
		t.Fatalf("snapshot refresh values = %v", source.refreshValues)
	}
	if !reflect.DeepEqual(source.changeInstances, []string{agentInstanceA}) ||
		!reflect.DeepEqual(source.changeAfter, []uint64{7}) ||
		!reflect.DeepEqual(source.changeTimeouts, []int{25}) {
		t.Fatalf("change arguments = instances %v after %v timeouts %v", source.changeInstances, source.changeAfter, source.changeTimeouts)
	}
}

func TestAgentChangeCoordinatorPublishesReconnectIdentityChanges(t *testing.T) {
	tests := []struct {
		name string
		next agentapi.Snapshot
		want int
	}{
		{name: "Agent restarted", next: agentSnapshot(agentInstanceB, 7), want: 1},
		{name: "generation changed", next: agentSnapshot(agentInstanceA, 8), want: 1},
		{name: "unchanged reconnect", next: agentSnapshot(agentInstanceA, 7), want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			watchErr := errors.New("watch disconnected")
			source := &fakeAgentChangeSource{
				snapshots: []agentSnapshotOutcome{
					{snapshot: agentSnapshot(agentInstanceA, 7)},
					{snapshot: test.next},
				},
				changes: []agentChangeOutcome{
					{err: watchErr},
					{err: watchErr, after: cancel},
				},
			}
			publisher := &fakeAgentChangePublisher{}
			coordinator, err := NewAgentChangeCoordinator(source, publisher)
			if err != nil {
				t.Fatal(err)
			}
			waitCalls := 0
			coordinator.wait = func(ctx context.Context, _ time.Duration) bool {
				waitCalls++
				return waitCalls == 1 && ctx.Err() == nil
			}
			var reports []AgentChangeReport
			coordinator.Run(ctx, func(report AgentChangeReport) {
				reports = append(reports, report)
			})
			if len(publisher.publications) != test.want {
				t.Fatalf("publication count = %d, want %d", len(publisher.publications), test.want)
			}
			if test.want == 1 {
				assertSingleInventoryPublication(t, publisher.publications)
			}
			if len(reports) != 2 || reports[0].Operation != AgentChangeWatch || reports[1].Operation != AgentChangeWatch {
				t.Fatalf("reports = %#v", reports)
			}
		})
	}
}

func TestAgentChangeCoordinatorReportsSnapshotAndWatchFailuresAndResetsRetry(t *testing.T) {
	snapshotErr := errors.New("snapshot unavailable")
	watchErr := errors.New("watch unavailable")
	initial := agentSnapshot(agentInstanceA, 9)
	source := &fakeAgentChangeSource{
		snapshots: []agentSnapshotOutcome{
			{err: snapshotErr}, {err: snapshotErr}, {err: snapshotErr},
			{err: snapshotErr}, {err: snapshotErr}, {err: snapshotErr},
			{snapshot: initial},
		},
		changes: []agentChangeOutcome{
			{response: agentapi.ChangeResponse{Changed: false, Snapshot: initial}},
			{err: watchErr},
		},
	}
	coordinator, err := NewAgentChangeCoordinator(source, &fakeAgentChangePublisher{})
	if err != nil {
		t.Fatal(err)
	}
	var delays []time.Duration
	coordinator.wait = func(_ context.Context, delay time.Duration) bool {
		delays = append(delays, delay)
		return len(delays) < 7
	}
	var reports []AgentChangeReport
	coordinator.Run(context.Background(), func(report AgentChangeReport) {
		reports = append(reports, report)
	})
	wantDelays := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, time.Second}
	if !reflect.DeepEqual(delays, wantDelays) {
		t.Fatalf("delays = %v, want %v", delays, wantDelays)
	}
	if len(reports) != 7 {
		t.Fatalf("reports = %#v", reports)
	}
	for index := 0; index < 6; index++ {
		if reports[index].Operation != AgentChangeSnapshot || !errors.Is(reports[index].Error, snapshotErr) {
			t.Fatalf("snapshot report %d = %#v", index, reports[index])
		}
	}
	if reports[6].Operation != AgentChangeWatch || !errors.Is(reports[6].Error, watchErr) {
		t.Fatalf("watch report = %#v", reports[6])
	}
	if got := nextAgentChangeRetryDelay(30 * time.Second); got != 30*time.Second {
		t.Fatalf("retry cap = %s", got)
	}
}

func TestAgentChangeCoordinatorStopsDuringRetryWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	source := &fakeAgentChangeSource{snapshots: []agentSnapshotOutcome{{err: errors.New("unavailable")}}}
	coordinator, err := NewAgentChangeCoordinator(source, &fakeAgentChangePublisher{})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		coordinator.Run(ctx, func(AgentChangeReport) {
			cancel()
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("coordinator did not stop after cancellation")
	}
}

func agentSnapshot(instanceID string, generation uint64) agentapi.Snapshot {
	return agentapi.Snapshot{
		ProtocolVersion: agentapi.ProtocolVersion,
		AgentInstanceID: instanceID,
		Generation:      generation,
	}
}

func assertSingleInventoryPublication(t *testing.T, publications []agentTestPublication) {
	t.Helper()
	wantTopics := []realtime.Topic{realtime.TopicInventory, realtime.TopicModems, realtime.TopicLines}
	if len(publications) != 1 || !reflect.DeepEqual(publications[0].topics, wantTopics) || publications[0].attention != "" {
		t.Fatalf("publications = %#v", publications)
	}
}
