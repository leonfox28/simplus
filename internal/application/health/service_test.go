package health

import (
	"context"
	"testing"
)

func TestSnapshotRejectsMissingStore(t *testing.T) {
	if _, err := New(nil, "simulator").Snapshot(context.Background()); err == nil {
		t.Fatal("Snapshot() accepted a missing state store")
	}
}
