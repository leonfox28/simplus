package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/leonfox28/simplus/internal/domain/call"
	"github.com/leonfox28/simplus/internal/domain/pagination"
)

func (set *Set) CreateCall(ctx context.Context, value call.Record) (call.Record, bool, error) {
	result, err := set.Calls.ExecContext(ctx, `
INSERT INTO call_records (call_id, operation_id, line_id, remote_address, direction, state, end_reason, created_at_unix_ms, updated_at_unix_ms)
VALUES (?, ?, ?, ?, ?, ?, '', ?, ?) ON CONFLICT(operation_id) DO NOTHING
`, value.ID, value.OperationID, value.LineID, value.RemoteAddress, value.Direction, value.State, value.CreatedAt.UnixMilli(), value.UpdatedAt.UnixMilli())
	if err != nil {
		return call.Record{}, false, fmt.Errorf("create call: %w", err)
	}
	changed, _ := result.RowsAffected()
	stored, found, err := set.callByOperation(ctx, value.OperationID)
	if err != nil || !found {
		return call.Record{}, false, fmt.Errorf("read created call: %w", err)
	}
	if changed == 0 && (stored.LineID != value.LineID || stored.RemoteAddress != value.RemoteAddress || stored.Direction != value.Direction) {
		return call.Record{}, false, call.ErrStateConflict
	}
	return stored, changed == 0, nil
}

func (set *Set) SetCallState(ctx context.Context, id, state, reason string, at time.Time) (call.Record, error) {
	var answered any
	var ended any
	if state == call.StateActive {
		answered = at.UTC().UnixMilli()
	}
	if state == call.StateEnded || state == call.StateFailed {
		ended = at.UTC().UnixMilli()
	}
	result, err := set.Calls.ExecContext(ctx, `
UPDATE call_records SET state = ?, end_reason = ?, updated_at_unix_ms = MAX(created_at_unix_ms, ?),
 answered_at_unix_ms = COALESCE(answered_at_unix_ms, ?), ended_at_unix_ms = COALESCE(ended_at_unix_ms, ?)
WHERE call_id = ?
`, state, reason, at.UTC().UnixMilli(), answered, ended, id)
	if err != nil {
		return call.Record{}, fmt.Errorf("update call state: %w", err)
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return call.Record{}, call.ErrNotFound
	}
	value, _, err := set.callByID(ctx, id)
	return value, err
}

func (set *Set) ListCalls(ctx context.Context, limit int) ([]call.Record, error) {
	page, err := set.ListCallsPage(ctx, pagination.Request{Limit: limit})
	return page.Items, err
}

func (set *Set) ListCallsPage(ctx context.Context, request pagination.Request) (pagination.Page[call.Record], error) {
	if set == nil || set.Calls == nil || request.Limit < 1 || request.Limit > pagination.MaximumLimit {
		return pagination.Page[call.Record]{}, fmt.Errorf("invalid call page request")
	}
	query := `
SELECT call_id, operation_id, line_id, remote_address, direction, state, end_reason,
 created_at_unix_ms, updated_at_unix_ms, answered_at_unix_ms, ended_at_unix_ms
FROM call_records
ORDER BY created_at_unix_ms DESC, call_id DESC LIMIT ?`
	args := []any{request.Limit + 1}
	if request.After != nil {
		query = `
SELECT call_id, operation_id, line_id, remote_address, direction, state, end_reason,
 created_at_unix_ms, updated_at_unix_ms, answered_at_unix_ms, ended_at_unix_ms
FROM call_records
WHERE (created_at_unix_ms, call_id) < (?, ?)
ORDER BY created_at_unix_ms DESC, call_id DESC LIMIT ?`
		args = []any{request.After.CreatedAt.UTC().UnixMilli(), request.After.ID, request.Limit + 1}
	}
	rows, err := set.Calls.QueryContext(ctx, query, args...)
	if err != nil {
		return pagination.Page[call.Record]{}, fmt.Errorf("list calls: %w", err)
	}
	defer rows.Close()
	values := make([]call.Record, 0)
	for rows.Next() {
		value, err := scanCall(rows)
		if err != nil {
			return pagination.Page[call.Record]{}, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return pagination.Page[call.Record]{}, err
	}
	page := pagination.Page[call.Record]{Items: values}
	if len(values) > request.Limit {
		page.Items = values[:request.Limit]
		last := page.Items[len(page.Items)-1]
		page.Next = &pagination.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return page, nil
}

func (set *Set) GetCallByOperation(ctx context.Context, operationID string) (call.Record, bool, error) {
	return set.callByOperation(ctx, operationID)
}

func (set *Set) GetCallByID(ctx context.Context, id string) (call.Record, bool, error) {
	return set.callByID(ctx, id)
}

func (set *Set) HasActiveCallForLine(ctx context.Context, lineID string) (bool, error) {
	var exists bool
	err := set.Calls.QueryRowContext(ctx, `
SELECT EXISTS(SELECT 1 FROM call_records WHERE line_id = ? AND state IN ('incoming', 'dialing', 'active'))
`, lineID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("read active call for line: %w", err)
	}
	return exists, nil
}

func (set *Set) ReconcileCalls(ctx context.Context, reason string, at time.Time) (int64, error) {
	result, err := set.Calls.ExecContext(ctx, `
UPDATE call_records SET state = 'failed', end_reason = ?, updated_at_unix_ms = MAX(created_at_unix_ms, ?), ended_at_unix_ms = ?
WHERE state IN ('incoming', 'dialing', 'active')
`, reason, at.UTC().UnixMilli(), at.UTC().UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("reconcile calls: %w", err)
	}
	return result.RowsAffected()
}

func (set *Set) callByOperation(ctx context.Context, operationID string) (call.Record, bool, error) {
	return scanOptionalCall(set.Calls.QueryRowContext(ctx, `SELECT call_id, operation_id, line_id, remote_address, direction, state, end_reason, created_at_unix_ms, updated_at_unix_ms, answered_at_unix_ms, ended_at_unix_ms FROM call_records WHERE operation_id = ?`, operationID))
}
func (set *Set) callByID(ctx context.Context, id string) (call.Record, bool, error) {
	return scanOptionalCall(set.Calls.QueryRowContext(ctx, `SELECT call_id, operation_id, line_id, remote_address, direction, state, end_reason, created_at_unix_ms, updated_at_unix_ms, answered_at_unix_ms, ended_at_unix_ms FROM call_records WHERE call_id = ?`, id))
}

type callScanner interface{ Scan(...any) error }

func scanOptionalCall(scanner callScanner) (call.Record, bool, error) {
	value, err := scanCall(scanner)
	if errors.Is(err, sql.ErrNoRows) {
		return call.Record{}, false, nil
	}
	return value, err == nil, err
}
func scanCall(scanner callScanner) (call.Record, error) {
	var value call.Record
	var created, updated int64
	var answered, ended sql.NullInt64
	if err := scanner.Scan(&value.ID, &value.OperationID, &value.LineID, &value.RemoteAddress, &value.Direction, &value.State, &value.EndReason, &created, &updated, &answered, &ended); err != nil {
		return call.Record{}, err
	}
	value.CreatedAt, value.UpdatedAt = time.UnixMilli(created).UTC(), time.UnixMilli(updated).UTC()
	if answered.Valid {
		at := time.UnixMilli(answered.Int64).UTC()
		value.AnsweredAt = &at
	}
	if ended.Valid {
		at := time.UnixMilli(ended.Int64).UTC()
		value.EndedAt = &at
	}
	return value, nil
}
