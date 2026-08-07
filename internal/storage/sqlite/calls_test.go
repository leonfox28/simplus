package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/domain/call"
	"github.com/leonfox28/simplus/internal/domain/pagination"
)

func TestCallKeysetPaginationUsesStableIDTieBreak(t *testing.T) {
	ctx := context.Background()
	set, err := OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	createdAt := time.Date(2026, 8, 7, 14, 0, 0, 456_000_000, time.UTC)
	for _, item := range []struct{ id, operation string }{
		{"call_page_000000000000", "operation-call-page-000"},
		{"call_page_000000000001", "operation-call-page-001"},
		{"call_page_000000000002", "operation-call-page-002"},
	} {
		if _, _, err := set.CreateCall(ctx, call.Record{
			ID: item.id, OperationID: item.operation, LineID: "line_AQEBAQEBAQEBAQEBAQEBAQ",
			RemoteAddress: "13800138000", Direction: call.DirectionOutbound, State: call.StateEnded,
			CreatedAt: createdAt, UpdatedAt: createdAt,
		}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := set.ListCallsPage(ctx, pagination.Request{Limit: 2})
	if err != nil || len(first.Items) != 2 || first.Items[0].ID != "call_page_000000000002" || first.Items[1].ID != "call_page_000000000001" || first.Next == nil {
		t.Fatalf("first page=%#v error=%v", first, err)
	}
	if _, _, err := set.CreateCall(ctx, call.Record{
		ID: "call_page_000000000003", OperationID: "operation-call-page-003", LineID: "line_AQEBAQEBAQEBAQEBAQEBAQ",
		RemoteAddress: "13900139000", Direction: call.DirectionInbound, State: call.StateIncoming,
		CreatedAt: createdAt.Add(time.Minute), UpdatedAt: createdAt.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	second, err := set.ListCallsPage(ctx, pagination.Request{Limit: 2, After: first.Next})
	if err != nil || len(second.Items) != 1 || second.Items[0].ID != "call_page_000000000000" || second.Next != nil {
		t.Fatalf("second page=%#v error=%v", second, err)
	}
}

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
