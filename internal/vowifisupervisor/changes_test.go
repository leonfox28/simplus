package vowifisupervisor

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	stream "github.com/leonfox28/simplus/internal/domain/connectivity"
)

func TestWorkerOutputContinuesPastOneMiBAndIgnoresRegistrationRefresh(t *testing.T) {
	local := &Local{Now: time.Now, instances: map[string]*instance{}}
	current := &instance{request: StartRequest{LineID: testManagedLineID}, status: Status{LineID: testManagedLineID}, ready: make(chan struct{})}
	local.instances[testManagedLineID] = current
	baseline, _ := local.ConnectionSnapshot(t.Context())
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	for i := 0; i < 15000; i++ {
		if err := encoder.Encode(workerEvent{LineID: testManagedLineID, State: StateOnline, Online: true, RegisteredAt: time.Now(), Attempt: i}); err != nil {
			t.Fatal(err)
		}
	}
	if err := encoder.Encode(workerEvent{LineID: testManagedLineID, State: StateReconnecting, Attempt: 15000}); err != nil {
		t.Fatal(err)
	}
	if output.Len() <= 1<<20 {
		t.Fatal("fixture too small")
	}
	if err := local.readWorkerEvents(current, &output); err != nil {
		t.Fatal(err)
	}
	changes, err := local.ConnectionChanges(t.Context(), baseline.Cursor)
	if err != nil || changes.Reset || len(changes.Events) != 2 || !changes.Events[0].State.Connected || changes.Events[1].State.Connected {
		t.Fatalf("changes=%+v %v", changes, err)
	}
	if err := local.readWorkerEvents(current, strings.NewReader(strings.Repeat("x", 17<<10))); err == nil {
		t.Fatal("unbounded single message accepted")
	}
}

func TestConnectionStreamResetReplayAndSnapshotCursor(t *testing.T) {
	local := &Local{Now: time.Now, instances: map[string]*instance{}}
	current := &instance{status: Status{LineID: testManagedLineID}}
	local.instances[testManagedLineID] = current
	baseline, _ := local.ConnectionSnapshot(t.Context())
	for i := 0; i < stream.Capacity+1; i++ {
		before := current.status.Online
		current.status.Online = !before
		local.recordConnectionLocked(current, before, "reconnecting")
	}
	overflow, _ := local.ConnectionChanges(t.Context(), baseline.Cursor)
	if !overflow.Reset || len(overflow.Events) != 0 {
		t.Fatalf("overflow=%+v", overflow)
	}
	recent := baseline.Cursor
	recent.Sequence = 1
	events, _ := local.ConnectionChanges(t.Context(), recent)
	if events.Reset || len(events.Events) != stream.Capacity || events.Events[0].Sequence != 2 {
		t.Fatalf("retained events=%+v", events.Cursor)
	}
	snapshot, _ := local.ConnectionSnapshot(t.Context())
	replay, _ := local.ConnectionChanges(t.Context(), snapshot.Cursor)
	if replay.Reset || len(replay.Events) != 0 || snapshot.Cursor != events.Cursor || len(snapshot.States) != 1 || !snapshot.States[0].Connected {
		t.Fatalf("snapshot=%+v replay=%+v", snapshot, replay)
	}
	restarted := &Local{Now: time.Now}
	reset, _ := restarted.ConnectionChanges(t.Context(), snapshot.Cursor)
	if !reset.Reset || reset.Cursor.Instance == snapshot.Cursor.Instance {
		t.Fatalf("restart=%+v", reset)
	}
}
