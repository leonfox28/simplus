package connectivity

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	stream "github.com/leonfox28/simplus/internal/domain/connectivity"
	line "github.com/leonfox28/simplus/internal/domain/line"
	modem "github.com/leonfox28/simplus/internal/domain/modem"
	domain "github.com/leonfox28/simplus/internal/domain/notification"
	"github.com/leonfox28/simplus/internal/storage/sqlite"
)

type testLines []line.Record

func (l testLines) ListManagedLines(context.Context) ([]line.Record, error) { return l, nil }

type testVoWiFi struct {
	snapshot stream.Snapshot
	changes  stream.Changes
	err      error
}

func (s *testVoWiFi) ConnectionSnapshot(context.Context) (stream.Snapshot, error) {
	return s.snapshot, s.err
}
func (s *testVoWiFi) ConnectionChanges(context.Context, stream.Cursor) (stream.Changes, error) {
	return s.changes, s.err
}

func TestVoWiFiMonitorReplaysAtomicallyAndRecalibratesGaps(t *testing.T) {
	s, err := sqlite.OpenSet(t.Context(), filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	at := time.Now().UTC()
	c := domain.Channel{ID: "channel_AAAAAAAAAAAAAAAAAAAAAA", Provider: "wecom", DeliveryMode: domain.DeliveryModeWebhook, DisplayName: "fixture", WebhookCiphertext: make([]byte, 40), WebhookHint: "qyapi.weixin.qq.com", Enabled: true, EventKinds: domain.EventKinds, CreatedAt: at, UpdatedAt: at, LastDeliveryStatus: "never"}
	if err := s.UpsertNotificationChannel(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	source := &testVoWiFi{snapshot: stream.Snapshot{Version: 1, Cursor: stream.Cursor{Instance: "netd-1", Sequence: 1}, States: []stream.State{{ObjectID: "line-A", Connected: true, ObservedAt: at}}}}
	m, _ := NewVoWiFiMonitor(s, testLines{{ID: "line-A", DisplayName: "线路 A"}, {ID: "line-B", DisplayName: "线路 B"}}, source)
	if err := m.Poll(t.Context()); err != nil {
		t.Fatal(err)
	}
	counts, _ := s.NotificationDeliveryCounts(t.Context(), c.ID)
	if counts.Pending != 1 {
		t.Fatalf("initial=%+v", counts)
	}
	source.changes = stream.Changes{Version: 1, Cursor: stream.Cursor{Instance: "netd-1", Sequence: 3}, Events: []stream.Change{
		{Sequence: 2, State: stream.State{ObjectID: "line-A", Connected: false, ObservedAt: at, Reason: "stopped"}},
		{Sequence: 3, State: stream.State{ObjectID: "line-A", Connected: true, ObservedAt: at, Reason: "registered"}},
	}}
	if _, err := s.DB.Exec(`CREATE TRIGGER fail_cursor BEFORE UPDATE ON connection_cursors BEGIN SELECT RAISE(ABORT,'test'); END`); err != nil {
		t.Fatal(err)
	}
	if err := m.Poll(t.Context()); err == nil {
		t.Fatal("failed cursor transaction committed")
	}
	counts, _ = s.NotificationDeliveryCounts(t.Context(), c.ID)
	if counts.Pending != 1 {
		t.Fatalf("partial outbox commit=%+v", counts)
	}
	if _, err := s.DB.Exec(`DROP TRIGGER fail_cursor`); err != nil {
		t.Fatal(err)
	}
	if err := m.Poll(t.Context()); err != nil {
		t.Fatal(err)
	}
	source.err = errors.New("source unavailable")
	if err := m.Poll(t.Context()); err == nil {
		t.Fatal("source error lost")
	}
	source.err = nil
	source.changes = stream.Changes{Reset: true}
	source.snapshot.Cursor = stream.Cursor{Instance: "netd-2"}
	if err := m.Poll(t.Context()); err != nil {
		t.Fatal(err)
	}
	cursor, _ := s.ReadConnectionCursor(t.Context(), "vowifi")
	counts, _ = s.NotificationDeliveryCounts(t.Context(), c.ID)
	if cursor.Instance != "netd-2" || cursor.Gaps != 1 || counts.Pending != 3 {
		t.Fatalf("cursor=%+v counts=%+v", cursor, counts)
	}
}

type memoryObservations struct {
	mu    sync.Mutex
	items []domain.Observation
}

func (m *memoryObservations) ReadConnectionCursor(context.Context, string) (domain.Cursor, error) {
	return domain.Cursor{}, nil
}
func (m *memoryObservations) CommitConnectionObservations(_ context.Context, _, _ domain.Cursor, o []domain.Observation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items = append(m.items, o...)
	return nil
}
func (m *memoryObservations) observations() []domain.Observation {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]domain.Observation(nil), m.items...)
}

type testModems []modem.Record

func (m testModems) ListManagedModems(context.Context) ([]modem.Record, error) { return m, nil }

type cellularFixture struct {
	mu      sync.Mutex
	targets []ProbeTarget
	err     error
	unknown bool
	calls   map[string]int
}

func (f *cellularFixture) Targets(context.Context) ([]ProbeTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ProbeTarget(nil), f.targets...), f.err
}
func (f *cellularFixture) Probe(ctx context.Context, t ProbeTarget) (CellularSample, error) {
	f.mu.Lock()
	f.calls[t.ID]++
	unknown := f.unknown
	f.mu.Unlock()
	if t.ID == "slow" {
		<-ctx.Done()
		return CellularSample{}, ctx.Err()
	}
	return CellularSample{Fingerprint: "fingerprint-A", Known: !unknown, Connected: true, Reason: "registered", ObservedAt: time.Now()}, nil
}

func TestCellularIndependentProbesUnknownAndSourceRestart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		repo := &memoryObservations{}
		source := &cellularFixture{targets: []ProbeTarget{{Instance: "first", ID: "fast", DeviceGeneration: 1}, {Instance: "first", ID: "slow", DeviceGeneration: 1}}, calls: map[string]int{}}
		m, _ := NewCellularMonitor(repo, testModems{{ID: "modem-A", EquipmentIdentityFingerprint: "fingerprint-A", DisplayName: "模组 A"}}, source)
		done := make(chan struct{})
		go func() { defer close(done); m.Run(ctx, nil) }()
		synctest.Wait()
		if got := repo.observations(); len(got) != 1 || !got[0].Connected {
			t.Fatalf("slow modem blocked initial observation: %+v", got)
		}
		time.Sleep(7 * time.Second)
		synctest.Wait()
		source.mu.Lock()
		fast, slow := source.calls["fast"], source.calls["slow"]
		source.targets = []ProbeTarget{{Instance: "restarted", ID: "fast", DeviceGeneration: 1}}
		source.unknown = true
		source.mu.Unlock()
		if fast < 2 || slow != 1 {
			t.Fatalf("independent calls fast=%d slow=%d", fast, slow)
		}
		time.Sleep(7 * time.Second)
		synctest.Wait()
		for _, o := range repo.observations() {
			if !o.Connected {
				t.Fatalf("restart/unknown invented loss: %+v", o)
			}
		}
		source.mu.Lock()
		source.err = errors.New("agent unavailable")
		source.targets = nil
		source.mu.Unlock()
		time.Sleep(2 * time.Second)
		synctest.Wait()
		for _, o := range repo.observations() {
			if !o.Connected {
				t.Fatal("transport failure invented loss")
			}
		}
		source.mu.Lock()
		source.err = nil
		source.mu.Unlock()
		time.Sleep(2 * time.Second)
		synctest.Wait()
		got := repo.observations()
		if len(got) == 0 || got[len(got)-1].Connected || got[len(got)-1].Reason != "device_removed" {
			t.Fatalf("confirmed removal missing: %+v", got)
		}
		cancel()
		<-done
	})
}

func (m *memoryObservations) HasConnectionCheckpoint(context.Context, string, string) (bool, error) {
	return false, nil
}

func TestNewManagedLineGetsBaselineWithoutReplayingHistoricalChanges(t *testing.T) {
	s, err := sqlite.OpenSet(t.Context(), filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	at := time.Now().UTC()
	source := &testVoWiFi{snapshot: stream.Snapshot{Version: 1, Cursor: stream.Cursor{Instance: "first", Sequence: 1}, States: []stream.State{{ObjectID: "line-A", Connected: true, ObservedAt: at}}}}
	m, _ := NewVoWiFiMonitor(s, testLines{}, source)
	if err := m.Poll(t.Context()); err != nil {
		t.Fatal(err)
	}
	m.lines = testLines{{ID: "line-A", DisplayName: "A"}, {ID: "line-B", DisplayName: "B"}}
	source.changes = stream.Changes{Version: 1, Cursor: source.snapshot.Cursor}
	if err := m.Poll(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"line-A", "line-B"} {
		known, err := s.HasConnectionCheckpoint(t.Context(), id, "vowifi")
		if err != nil || !known {
			t.Fatalf("missing baseline %s: %v", id, err)
		}
	}
	source.snapshot.States = nil // A stale snapshot is now irrelevant: the cursor owns future changes.
	if err := m.Poll(t.Context()); err != nil {
		t.Fatal(err)
	}
	var connected bool
	if err := s.DB.QueryRow(`SELECT connected FROM connection_checkpoints WHERE object_id='line-A'`).Scan(&connected); err != nil || !connected {
		t.Fatalf("repeated snapshot changed known state: %v %v", connected, err)
	}
}
