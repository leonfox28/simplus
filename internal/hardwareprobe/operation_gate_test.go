package hardwareprobe

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestOperationGateSerializesByDeviceAndReleasesCancelledEntries(t *testing.T) {
	gate := NewOperationGate()
	release, err := gate.Acquire(t.Context(), "device-a")
	if err != nil {
		t.Fatal(err)
	}
	other, err := gate.Acquire(t.Context(), "device-b")
	if err != nil {
		t.Fatal(err)
	}
	other()
	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	if _, err := gate.Acquire(ctx, "device-a"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued acquire error = %v", err)
	}
	release()
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if len(gate.entries) != 0 {
		t.Fatalf("retained gate entries = %d", len(gate.entries))
	}
}

func TestOperationGateRejectsInvalidTargetsWithoutAllocation(t *testing.T) {
	gate := NewOperationGate()
	for _, target := range []string{"", " ", string(make([]byte, 129))} {
		if _, err := gate.Acquire(t.Context(), target); err == nil {
			t.Fatalf("accepted target %q", target)
		}
	}
	if len(gate.entries) != 0 {
		t.Fatalf("retained invalid gate entries = %d", len(gate.entries))
	}
}

func TestOperationGateRejectsAnAlreadyCancelledFreeAcquire(t *testing.T) {
	gate := NewOperationGate()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := gate.Acquire(ctx, "device-a"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled acquire error = %v", err)
	}
	if len(gate.entries) != 0 {
		t.Fatalf("cancelled acquire retained %d entries", len(gate.entries))
	}
}
