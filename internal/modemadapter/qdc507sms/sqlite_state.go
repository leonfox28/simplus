package qdc507sms

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const sqliteStateSchemaVersion = 1

// SQLiteStateStore is the narrow durable state required to prevent duplicate
// sends and to finish partially acknowledged multipart messages after an
// Agent process restart. It is intentionally separate from the generic radio
// command outcome ledger.
type SQLiteStateStore struct {
	db *sql.DB
}

var _ StateStore = (*SQLiteStateStore)(nil)

func OpenSQLiteStateStore(ctx context.Context, path string) (*SQLiteStateStore, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) == string(filepath.Separator) {
		return nil, errors.New("QDC507 SMS state path must be an absolute non-root file")
	}
	path = filepath.Clean(path)
	directory := filepath.Dir(path)
	directoryInfo, err := os.Stat(directory)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil, fmt.Errorf("create QDC507 SMS state directory: %w", err)
		}
		if err := os.Chmod(directory, 0o700); err != nil {
			return nil, fmt.Errorf("set QDC507 SMS state directory permissions: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("inspect QDC507 SMS state directory: %w", err)
	} else if !directoryInfo.IsDir() || directoryInfo.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("QDC507 SMS state directory must already be private")
	}
	if pathInfo, err := os.Lstat(path); err == nil && (pathInfo.IsDir() || pathInfo.Mode()&os.ModeSymlink != 0) {
		return nil, errors.New("QDC507 SMS state path must be a regular file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect QDC507 SMS state path: %w", err)
	}
	dsn := (&url.URL{Scheme: "file", Path: path}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open QDC507 SMS state: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	closeOnError := func(err error) (*SQLiteStateStore, error) {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `
PRAGMA busy_timeout = 5000;
PRAGMA journal_mode = WAL;
PRAGMA synchronous = FULL;
`); err != nil {
		return closeOnError(fmt.Errorf("configure QDC507 SMS state: %w", err))
	}
	var version int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return closeOnError(fmt.Errorf("read QDC507 SMS state schema version: %w", err))
	}
	if version == 0 {
		if _, err := db.ExecContext(ctx, sqliteStateSchema); err != nil {
			return closeOnError(fmt.Errorf("initialize QDC507 SMS state: %w", err))
		}
		version = sqliteStateSchemaVersion
	}
	if version != sqliteStateSchemaVersion {
		return closeOnError(fmt.Errorf("unsupported QDC507 SMS state schema version %d", version))
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return closeOnError(fmt.Errorf("set QDC507 SMS state permissions: %w", err))
	}
	return &SQLiteStateStore{db: db}, nil
}

func (store *SQLiteStateStore) Close() error {
	if store == nil || store.db == nil {
		return nil
	}
	var joined error
	if _, err := store.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		joined = errors.Join(joined, err)
	}
	joined = errors.Join(joined, store.db.Close())
	return joined
}

func (store *SQLiteStateStore) PutInbound(ctx context.Context, record InboundRecord) (InboundRecord, bool, error) {
	if err := validInboundRecord(record); err != nil {
		return InboundRecord{}, false, err
	}
	tx, err := store.begin(ctx)
	if err != nil {
		return InboundRecord{}, false, err
	}
	defer tx.Rollback()
	existing, found, err := findSQLiteInbound(ctx, tx, record.DeviceID, record.MessageID)
	if err != nil {
		return InboundRecord{}, false, err
	}
	if found {
		if !sameInboundIdentity(existing, record) {
			return InboundRecord{}, false, ErrStateConflict
		}
		if err := tx.Commit(); err != nil {
			return InboundRecord{}, false, fmt.Errorf("commit QDC507 inbound SMS replay: %w", err)
		}
		return existing, true, nil
	}
	segments, err := json.Marshal(record.Segments)
	if err != nil {
		return InboundRecord{}, false, fmt.Errorf("encode QDC507 inbound SMS segments: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO inbound_messages (message_id, device_id, sender, body, received_at_ns, segments_json, acknowledged)
VALUES (?, ?, ?, ?, ?, ?, ?)
`, record.MessageID, record.DeviceID, record.Sender, record.Body, record.ReceivedAt.UnixNano(), segments, boolInteger(record.Acknowledged)); err != nil {
		return InboundRecord{}, false, fmt.Errorf("insert QDC507 inbound SMS: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return InboundRecord{}, false, fmt.Errorf("commit QDC507 inbound SMS: %w", err)
	}
	return cloneInbound(record), false, nil
}

func (store *SQLiteStateStore) FindInbound(ctx context.Context, deviceID, messageID string) (InboundRecord, bool, error) {
	if store == nil || store.db == nil {
		return InboundRecord{}, false, errors.New("QDC507 SMS state is unavailable")
	}
	return findSQLiteInbound(ctx, store.db, deviceID, messageID)
}

func (store *SQLiteStateStore) ListInbound(ctx context.Context, deviceID string) ([]InboundRecord, error) {
	if store == nil || store.db == nil {
		return nil, errors.New("QDC507 SMS state is unavailable")
	}
	rows, err := store.db.QueryContext(ctx, inboundSelect+`
 WHERE device_id = ? AND acknowledged = 0
 ORDER BY received_at_ns, message_id
`, deviceID)
	if err != nil {
		return nil, fmt.Errorf("query QDC507 inbound SMS: %w", err)
	}
	defer rows.Close()
	records := make([]InboundRecord, 0)
	for rows.Next() {
		record, err := scanSQLiteInbound(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate QDC507 inbound SMS: %w", err)
	}
	return records, nil
}

func (store *SQLiteStateStore) UpdateInbound(ctx context.Context, record InboundRecord) error {
	if err := validInboundRecord(record); err != nil {
		return err
	}
	tx, err := store.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	existing, found, err := findSQLiteInbound(ctx, tx, record.DeviceID, record.MessageID)
	if err != nil {
		return err
	}
	if !found || !sameInboundIdentity(existing, record) || !validInboundProgress(existing, record) {
		return ErrStateConflict
	}
	segments, err := json.Marshal(record.Segments)
	if err != nil {
		return fmt.Errorf("encode QDC507 inbound SMS progress: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE inbound_messages SET segments_json = ?, acknowledged = ? WHERE message_id = ? AND device_id = ?
`, segments, boolInteger(record.Acknowledged), record.MessageID, record.DeviceID); err != nil {
		return fmt.Errorf("update QDC507 inbound SMS progress: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit QDC507 inbound SMS progress: %w", err)
	}
	return nil
}

func (store *SQLiteStateStore) PutOperation(ctx context.Context, record operationRecord) (operationRecord, bool, error) {
	if !validOperation(record) || record.State != operationAccepted {
		return operationRecord{}, false, errors.New("invalid QDC507 SMS operation record")
	}
	tx, err := store.begin(ctx)
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
INSERT INTO operations (operation_id, kind, request_digest, state, submission_message_id, submitted_at_ns)
VALUES (?, ?, ?, ?, '', 0)
`, record.OperationID, record.Kind, record.RequestDigest[:], record.State); err != nil {
		return operationRecord{}, false, fmt.Errorf("insert QDC507 SMS operation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return operationRecord{}, false, fmt.Errorf("commit QDC507 SMS operation: %w", err)
	}
	return record, false, nil
}

func (store *SQLiteStateStore) UpdateOperation(ctx context.Context, record operationRecord) error {
	if !validOperation(record) || (record.State != operationSucceeded && record.State != operationUnknown) {
		return errors.New("invalid terminal QDC507 SMS operation record")
	}
	tx, err := store.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	existing, found, err := findSQLiteOperation(ctx, tx, record.OperationID)
	if err != nil {
		return err
	}
	if !found || existing.Kind != record.Kind || existing.RequestDigest != record.RequestDigest ||
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

func (store *SQLiteStateStore) DeleteOperation(ctx context.Context, record operationRecord) error {
	tx, err := store.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	existing, found, err := findSQLiteOperation(ctx, tx, record.OperationID)
	if err != nil {
		return err
	}
	if !found || existing.Kind != record.Kind || existing.RequestDigest != record.RequestDigest || existing.State != operationAccepted {
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

func (store *SQLiteStateStore) begin(ctx context.Context) (*sql.Tx, error) {
	if store == nil || store.db == nil {
		return nil, errors.New("QDC507 SMS state is unavailable")
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin QDC507 SMS state transaction: %w", err)
	}
	return tx, nil
}

type sqliteQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type sqliteScanner interface {
	Scan(...any) error
}

func findSQLiteInbound(ctx context.Context, query sqliteQuery, deviceID, messageID string) (InboundRecord, bool, error) {
	record, err := scanSQLiteInbound(query.QueryRowContext(ctx, inboundSelect+` WHERE device_id = ? AND message_id = ?`, deviceID, messageID))
	if errors.Is(err, sql.ErrNoRows) {
		return InboundRecord{}, false, nil
	}
	if err != nil {
		return InboundRecord{}, false, err
	}
	return record, true, nil
}

func scanSQLiteInbound(scanner sqliteScanner) (InboundRecord, error) {
	var record InboundRecord
	var receivedAt int64
	var segments []byte
	var acknowledged int
	if err := scanner.Scan(
		&record.MessageID, &record.DeviceID, &record.Sender, &record.Body, &receivedAt, &segments, &acknowledged,
	); err != nil {
		return InboundRecord{}, err
	}
	if acknowledged != 0 && acknowledged != 1 {
		return InboundRecord{}, errors.New("QDC507 inbound SMS has an invalid acknowledged state")
	}
	if err := json.Unmarshal(segments, &record.Segments); err != nil {
		return InboundRecord{}, fmt.Errorf("decode QDC507 inbound SMS segments: %w", err)
	}
	record.ReceivedAt = time.Unix(0, receivedAt).UTC()
	record.Acknowledged = acknowledged == 1
	if err := validInboundRecord(record); err != nil {
		return InboundRecord{}, fmt.Errorf("validate stored QDC507 inbound SMS: %w", err)
	}
	return record, nil
}

func findSQLiteOperation(ctx context.Context, query sqliteQuery, operationID string) (operationRecord, bool, error) {
	var record operationRecord
	var digest []byte
	var submittedAt int64
	if err := query.QueryRowContext(ctx, `
SELECT operation_id, kind, request_digest, state, submission_message_id, submitted_at_ns
FROM operations WHERE operation_id = ?
`, operationID).Scan(
		&record.OperationID, &record.Kind, &digest, &record.State, &record.Submission.MessageID, &submittedAt,
	); errors.Is(err, sql.ErrNoRows) {
		return operationRecord{}, false, nil
	} else if err != nil {
		return operationRecord{}, false, fmt.Errorf("read QDC507 SMS operation: %w", err)
	}
	if len(digest) != len(record.RequestDigest) || !validOperation(record) ||
		(record.State != operationAccepted && record.State != operationSucceeded && record.State != operationUnknown) {
		return operationRecord{}, false, errors.New("stored QDC507 SMS operation is invalid")
	}
	copy(record.RequestDigest[:], digest)
	if record.Kind == operationSend && record.State == operationSucceeded {
		record.Submission.OperationID = record.OperationID
		record.Submission.SubmittedAt = time.Unix(0, submittedAt).UTC()
		if record.Submission.MessageID == "" || submittedAt == 0 {
			return operationRecord{}, false, errors.New("stored QDC507 SMS submission is invalid")
		}
	} else if record.Submission.MessageID != "" || submittedAt != 0 {
		return operationRecord{}, false, errors.New("stored QDC507 SMS operation has an unexpected submission")
	}
	return record, true, nil
}

func boolInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}

const inboundSelect = `
SELECT message_id, device_id, sender, body, received_at_ns, segments_json, acknowledged
FROM inbound_messages`

const sqliteStateSchema = `
BEGIN;
CREATE TABLE inbound_messages (
    message_id TEXT PRIMARY KEY,
    device_id TEXT NOT NULL,
    sender TEXT NOT NULL,
    body TEXT NOT NULL,
    received_at_ns INTEGER NOT NULL,
    segments_json BLOB NOT NULL,
    acknowledged INTEGER NOT NULL CHECK (acknowledged IN (0, 1))
);
CREATE INDEX inbound_messages_pending ON inbound_messages (device_id, acknowledged, received_at_ns, message_id);

CREATE TABLE operations (
    operation_id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('send', 'acknowledge')),
    request_digest BLOB NOT NULL CHECK (length(request_digest) = 32),
    state TEXT NOT NULL CHECK (state IN ('accepted', 'succeeded', 'outcome-unknown')),
    submission_message_id TEXT NOT NULL,
    submitted_at_ns INTEGER NOT NULL
);
PRAGMA user_version = 1;
COMMIT;
`
