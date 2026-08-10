package calls

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/application/inventory"
	"github.com/leonfox28/simplus/internal/domain/call"
	"github.com/leonfox28/simplus/internal/domain/pagination"
	sqlitestore "github.com/leonfox28/simplus/internal/storage/sqlite"
)

const testManagedLineID = "line_AQEBAQEBAQEBAQEBAQEBAQ"

type managedCallLineSource struct{ source LineSource }

func (source managedCallLineSource) Topology(ctx context.Context) (inventory.Topology, error) {
	topology, err := source.source.Topology(ctx)
	if err != nil {
		return inventory.Topology{}, err
	}
	for index := range topology.Lines {
		if topology.Lines[index].ID == "simulator-line-1" {
			topology.Lines[index].RuntimeLineID = topology.Lines[index].ID
			topology.Lines[index].ID = testManagedLineID
		}
	}
	return topology, nil
}

func newCallService(t *testing.T) (*Service, *sqlitestore.Set) {
	t.Helper()
	ctx := context.Background()
	stores, err := sqlitestore.OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(ctx, stores, managedCallLineSource{source: inventory.NewSimulator()})
	if err != nil {
		t.Fatal(err)
	}
	service.random = strings.NewReader(strings.Repeat("a", 16) + strings.Repeat("b", 16) + strings.Repeat("c", 16) + strings.Repeat("d", 16))
	clock := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { clock = clock.Add(time.Second); return clock }
	t.Cleanup(func() { _ = stores.Close() })
	return service, stores
}

func TestCallsRejectSMSSequenceCursor(t *testing.T) {
	service, _ := newCallService(t)
	cursor, err := pagination.EncodeSMS(pagination.Cursor{RecordSequence: 1, ID: "message_abcdefghijklmnop"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.List(context.Background(), 20, cursor); !errors.Is(err, pagination.ErrCursorInvalid) {
		t.Fatalf("Calls accepted SMS sequence cursor: %v", err)
	}
}

func TestSimulatorCallLifecycleAndSafety(t *testing.T) {
	service, _ := newCallService(t)
	ctx := context.Background()
	if _, _, err := service.Dial(ctx, "operation-emergency-0001", testManagedLineID, "112"); !errors.Is(err, ErrUnsafeNumber) {
		t.Fatalf("emergency error = %v", err)
	}
	outbound, replayed, err := service.Dial(ctx, "operation-call-out-0001", testManagedLineID, "13800138000")
	if err != nil || replayed || outbound.State != call.StateActive {
		t.Fatalf("outbound = %#v replayed=%v err=%v", outbound, replayed, err)
	}
	if _, err := service.DTMF(ctx, outbound.ID, "12#"); err != nil {
		t.Fatal(err)
	}
	if replay, wasReplay, err := service.Dial(ctx, "operation-call-out-0001", testManagedLineID, "13800138000"); err != nil || !wasReplay || replay.ID != outbound.ID {
		t.Fatalf("replay=%#v wasReplay=%v err=%v", replay, wasReplay, err)
	}
	if _, _, err := service.Incoming(ctx, "operation-call-busy-0001", testManagedLineID, "13900139000"); !errors.Is(err, ErrLineBusy) {
		t.Fatalf("busy error=%v", err)
	}
	ended, err := service.Hangup(ctx, outbound.ID)
	if err != nil || ended.State != call.StateEnded {
		t.Fatalf("ended = %#v err=%v", ended, err)
	}
	inbound, _, err := service.Incoming(ctx, "operation-call-in-00001", testManagedLineID, "13900139000")
	if err != nil {
		t.Fatal(err)
	}
	answered, err := service.Answer(ctx, inbound.ID)
	if err != nil || answered.State != call.StateActive {
		t.Fatalf("answered = %#v err=%v", answered, err)
	}
}

func TestRestartReconcilesUnfinishedCall(t *testing.T) {
	service, stores := newCallService(t)
	inbound, _, err := service.Incoming(context.Background(), "operation-call-in-00002", testManagedLineID, "13900139000")
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := New(context.Background(), stores, managedCallLineSource{source: inventory.NewSimulator()})
	if err != nil {
		t.Fatal(err)
	}
	page, err := restarted.List(context.Background(), 20, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Calls) != 1 || page.Calls[0].ID != inbound.ID || page.Calls[0].State != call.StateFailed || page.Calls[0].EndReason != ErrorInterruptedByRestart {
		t.Fatalf("reconciled = %#v", page.Calls)
	}
}
