package hardwareprobe

import (
	"context"
	"errors"
	"strings"
	"sync"
)

type gateEntry struct {
	token chan struct{}
	refs  int
}

// OperationGate serializes modem endpoint ownership per current Agent device.
// Entries are reference-counted so cancelled/finished targets do not grow the
// map without bound.
type OperationGate struct {
	mu      sync.Mutex
	entries map[string]*gateEntry
}

func NewOperationGate() *OperationGate { return &OperationGate{entries: make(map[string]*gateEntry)} }

func (gate *OperationGate) Acquire(ctx context.Context, deviceID string) (func(), error) {
	if gate == nil || strings.TrimSpace(deviceID) == "" || len(deviceID) > 128 {
		return nil, errors.New("invalid modem operation target")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	gate.mu.Lock()
	entry := gate.entries[deviceID]
	if entry == nil {
		entry = &gateEntry{token: make(chan struct{}, 1)}
		entry.token <- struct{}{}
		gate.entries[deviceID] = entry
	}
	entry.refs++
	gate.mu.Unlock()
	select {
	case <-ctx.Done():
		gate.drop(deviceID, entry)
		return nil, ctx.Err()
	case <-entry.token:
		var once sync.Once
		return func() {
			once.Do(func() {
				entry.token <- struct{}{}
				gate.drop(deviceID, entry)
			})
		}, nil
	}
}

func (gate *OperationGate) drop(deviceID string, entry *gateEntry) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	entry.refs--
	if entry.refs == 0 && gate.entries[deviceID] == entry {
		delete(gate.entries, deviceID)
	}
}
