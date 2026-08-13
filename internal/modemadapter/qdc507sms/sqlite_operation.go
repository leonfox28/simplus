package qdc507sms

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func putSQLiteOperation(ctx context.Context, db *sql.DB, record operationRecord) (operationRecord, bool, error) {
	if !validOperation(record) || record.State != operationAccepted {
		return operationRecord{}, false, errors.New("invalid QDC507 SMS operation record")
	}
	tx, err := beginSQLiteOperation(ctx, db)
	if err != nil {
		return operationRecord{}, false, err
	}
	defer tx.Rollback()
	existing, found, err := findSQLiteOperation(ctx, tx, record.OperationID)
	if err != nil {
		return operationRecord{}, false, err
	}
	if found {
		if err := tx.Commit(); err != nil {
			return operationRecord{}, false, fmt.Errorf("commit QDC507 SMS operation replay: %w", err)
		}
		return existing, true, nil
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO operations (operation_id, subscription_key, kind, request_digest, state, submission_message_id, submitted_at_ns)
VALUES (?, ?, ?, ?, ?, '', 0)
`, record.OperationID, record.SubscriptionKey, record.Kind, record.RequestDigest[:], record.State); err != nil {
		return operationRecord{}, false, fmt.Errorf("insert QDC507 SMS operation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return operationRecord{}, false, fmt.Errorf("commit QDC507 SMS operation: %w", err)
	}
	return record, false, nil
}

func updateSQLiteOperation(ctx context.Context, db *sql.DB, record operationRecord) error {
	if !validOperation(record) || (record.State != operationSucceeded && record.State != operationUnknown) {
		return errors.New("invalid terminal QDC507 SMS operation record")
	}
	tx, err := beginSQLiteOperation(ctx, db)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	existing, found, err := findSQLiteOperation(ctx, tx, record.OperationID)
	if err != nil {
		return err
	}
	if !found || existing.SubscriptionKey != record.SubscriptionKey || existing.Kind != record.Kind || existing.RequestDigest != record.RequestDigest ||
		(existing.State != operationAccepted && existing.State != record.State) {
		return ErrStateConflict
	}
	submittedAt := int64(0)
	if !record.Submission.SubmittedAt.IsZero() {
		submittedAt = record.Submission.SubmittedAt.UnixNano()
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE operations SET state = ?, submission_message_id = ?, submitted_at_ns = ? WHERE operation_id = ?
`, record.State, record.Submission.MessageID, submittedAt, record.OperationID); err != nil {
		return fmt.Errorf("update QDC507 SMS operation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit QDC507 SMS operation: %w", err)
	}
	return nil
}

func deleteSQLiteOperation(ctx context.Context, db *sql.DB, record operationRecord) error {
	tx, err := beginSQLiteOperation(ctx, db)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	existing, found, err := findSQLiteOperation(ctx, tx, record.OperationID)
	if err != nil {
		return err
	}
	if !found || existing.SubscriptionKey != record.SubscriptionKey || existing.Kind != record.Kind || existing.RequestDigest != record.RequestDigest || existing.State != operationAccepted {
		return ErrStateConflict
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM operations WHERE operation_id = ?`, record.OperationID); err != nil {
		return fmt.Errorf("delete QDC507 SMS operation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit QDC507 SMS operation deletion: %w", err)
	}
	return nil
}

func beginSQLiteOperation(ctx context.Context, db *sql.DB) (*sql.Tx, error) {
	if db == nil {
		return nil, errors.New("QDC507 SMS state is unavailable")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin QDC507 SMS state transaction: %w", err)
	}
	return tx, nil
}
