package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/domain/call"
)

func TestCallsPersistAcrossReopen(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "db")
	set, err := OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	created, replayed, err := set.CreateCall(ctx, call.Record{ID: "call_0123456789abcdef", OperationID: "operation-call-0000001", LineID: "simulator-line-1", RemoteAddress: "13800138000", Direction: call.DirectionOutbound, State: call.StateDialing, CreatedAt: now, UpdatedAt: now})
	if err != nil || replayed {
		t.Fatalf("create = %#v replayed=%v err=%v", created, replayed, err)
	}
	if _, err := set.SetCallState(ctx, created.ID, call.StateActive, "", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	values, err := reopened.ListCalls(ctx, 10)
	if err != nil || len(values) != 1 || values[0].State != call.StateActive || values[0].AnsweredAt == nil {
		t.Fatalf("calls = %#v err=%v", values, err)
	}
}
