package agentapi

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

const (
	outcomeSchemaVersion             = 1
	DefaultCommandOutcomeLimit       = 4096
	DefaultCommandResourceGroupLimit = 256
)

var (
	ErrOutcomeReplayConflict = errors.New("operation id was already used for a different command request")
	ErrOutcomeFenceStale     = errors.New("resource group generation or fencing token is stale")
	ErrOutcomeLedgerFull     = errors.New("command outcome ledger is full")
	ErrOutcomePending        = errors.New("a prior command outcome still requires reconciliation")
	ErrOutcomeNotPending     = errors.New("command outcome is already terminal")
)

type OutcomeStoreOptions struct {
	Directory         string
	MaxOutcomes       int
	MaxResourceGroups int
}

type OutcomeStore struct {
	db                *sql.DB
	maxOutcomes       int
	maxResourceGroups int
}

type outcomeRecord struct {
	Request       RadioEnsureOffRequest
	RequestDigest string
	Outcome       CommandOutcome
}

func OpenOutcomeStore(ctx context.Context, options OutcomeStoreOptions) (*OutcomeStore, error) {
	if !filepath.IsAbs(options.Directory) || filepath.Clean(options.Directory) == string(filepath.Separator) {
		return nil, errors.New("Agent state directory must be an absolute non-root path")
	}
	if options.MaxOutcomes < 1 || options.MaxOutcomes > 1_000_000 || options.MaxResourceGroups < 1 || options.MaxResourceGroups > 65_536 {
		return nil, errors.New("invalid Agent outcome ledger limits")
	}
	directory := filepath.Clean(options.Directory)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create Agent state directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return nil, fmt.Errorf("set Agent state directory permissions: %w", err)
	}
	path := filepath.Join(directory, "outcomes.sqlite3")
	dsn := (&url.URL{Scheme: "file", Path: path}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open Agent outcome ledger: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	closeOnError := func(err error) (*OutcomeStore, error) {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `
PRAGMA busy_timeout = 5000;
PRAGMA journal_mode = WAL;
PRAGMA synchronous = FULL;
PRAGMA foreign_keys = ON;
`); err != nil {
		return closeOnError(fmt.Errorf("configure Agent outcome ledger: %w", err))
	}
	var version int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return closeOnError(fmt.Errorf("read Agent outcome schema version: %w", err))
	}
	if version == 0 {
		if _, err := db.ExecContext(ctx, outcomeSchema); err != nil {
			return closeOnError(fmt.Errorf("initialize Agent outcome schema: %w", err))
		}
		version = outcomeSchemaVersion
	}
	if version != outcomeSchemaVersion {
		return closeOnError(fmt.Errorf("unsupported Agent outcome schema version %d", version))
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return closeOnError(fmt.Errorf("set Agent outcome ledger permissions: %w", err))
	}
	return &OutcomeStore{db: db, maxOutcomes: options.MaxOutcomes, maxResourceGroups: options.MaxResourceGroups}, nil
}

func (store *OutcomeStore) Close() error {
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

func (store *OutcomeStore) Find(ctx context.Context, operationID string) (outcomeRecord, bool, error) {
	if store == nil || store.db == nil || operationID == "" {
		return outcomeRecord{}, false, errors.New("Agent outcome store is unavailable")
	}
	record, err := scanOutcomeRecord(store.db.QueryRowContext(ctx, outcomeSelect+` WHERE operation_id = ?`, operationID))
	if errors.Is(err, sql.ErrNoRows) {
		return outcomeRecord{}, false, nil
	}
	if err != nil {
		return outcomeRecord{}, false, fmt.Errorf("read Agent command outcome: %w", err)
	}
	return record, true, nil
}

func (store *OutcomeStore) Pending(ctx context.Context) ([]outcomeRecord, error) {
	if store == nil || store.db == nil {
		return nil, errors.New("Agent outcome store is unavailable")
	}
	rows, err := store.db.QueryContext(ctx, outcomeSelect+` WHERE state = ? ORDER BY sequence`, CommandOutcomeAccepted)
	if err != nil {
		return nil, fmt.Errorf("query pending Agent outcomes: %w", err)
	}
	defer rows.Close()
	var records []outcomeRecord
	for rows.Next() {
		record, err := scanOutcomeRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("scan pending Agent outcome: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending Agent outcomes: %w", err)
	}
	return records, nil
}

func (store *OutcomeStore) Accept(ctx context.Context, request RadioEnsureOffRequest, requestDigest string, now time.Time) (outcomeRecord, bool, error) {
	if store == nil || store.db == nil {
		return outcomeRecord{}, false, errors.New("Agent outcome store is unavailable")
	}
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return outcomeRecord{}, false, fmt.Errorf("encode Agent command request: %w", err)
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return outcomeRecord{}, false, fmt.Errorf("begin Agent outcome transaction: %w", err)
	}
	defer tx.Rollback()
	if existing, found, err := findOutcomeTx(ctx, tx, request.OperationID); err != nil {
		return outcomeRecord{}, false, err
	} else if found {
		if existing.RequestDigest != requestDigest {
			return outcomeRecord{}, false, ErrOutcomeReplayConflict
		}
		if err := tx.Commit(); err != nil {
			return outcomeRecord{}, false, fmt.Errorf("commit replayed Agent outcome: %w", err)
		}
		return existing, true, nil
	}
	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM command_outcomes WHERE state = ?`, CommandOutcomeAccepted).Scan(&pending); err != nil {
		return outcomeRecord{}, false, fmt.Errorf("count pending Agent outcomes: %w", err)
	}
	if pending != 0 {
		return outcomeRecord{}, false, ErrOutcomePending
	}
	if err := store.advanceFence(ctx, tx, request); err != nil {
		return outcomeRecord{}, false, err
	}
	if err := store.makeOutcomeCapacity(ctx, tx); err != nil {
		return outcomeRecord{}, false, err
	}
	acceptedAt := now.UTC()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO command_outcomes (
    operation_id, request_digest, request_json, command, state, code,
    error_layer, retryable, reconciled, rf_state, accepted_at_unix_ms
) VALUES (?, ?, ?, ?, ?, ?, '', 0, 0, ?, ?)
`, request.OperationID, requestDigest, requestJSON, CommandRadioEnsureOff, CommandOutcomeAccepted,
		OutcomeCodeAccepted, RFStateUnknown, acceptedAt.UnixMilli()); err != nil {
		return outcomeRecord{}, false, fmt.Errorf("insert Agent command outcome: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return outcomeRecord{}, false, fmt.Errorf("commit accepted Agent outcome: %w", err)
	}
	return outcomeRecord{
		Request: request, RequestDigest: requestDigest,
		Outcome: CommandOutcome{
			OperationID: request.OperationID, Command: CommandRadioEnsureOff, State: CommandOutcomeAccepted,
			Code: OutcomeCodeAccepted, Observation: RadioEnsureOffObservation{RF: RFObservation{State: RFStateUnknown}},
			AcceptedAt: acceptedAt,
		},
	}, false, nil
}

func (store *OutcomeStore) Complete(ctx context.Context, outcome CommandOutcome) error {
	if store == nil || store.db == nil || outcome.OperationID == "" || outcome.CompletedAt == nil ||
		(outcome.State != CommandOutcomeSucceeded && outcome.State != CommandOutcomeFailed && outcome.State != CommandOutcomeUncertain) {
		return errors.New("invalid terminal Agent command outcome")
	}
	var rfMode, callCount any
	if outcome.Observation.RF.Mode != nil {
		rfMode = *outcome.Observation.RF.Mode
	}
	if outcome.Observation.ActiveCallCount != nil {
		callCount = *outcome.Observation.ActiveCallCount
	}
	result, err := store.db.ExecContext(ctx, `
UPDATE command_outcomes
SET state = ?, code = ?, error_layer = ?, retryable = ?, reconciled = ?,
    rf_state = ?, rf_mode = ?, active_call_count = ?, completed_at_unix_ms = ?
WHERE operation_id = ? AND state = ?
`, outcome.State, outcome.Code, outcome.ErrorLayer, outcome.Retryable, outcome.Reconciled,
		outcome.Observation.RF.State, rfMode, callCount, outcome.CompletedAt.UTC().UnixMilli(),
		outcome.OperationID, CommandOutcomeAccepted)
	if err != nil {
		return fmt.Errorf("complete Agent command outcome: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read completed Agent outcome count: %w", err)
	}
	if changed != 1 {
		return ErrOutcomeNotPending
	}
	return nil
}

func (store *OutcomeStore) advanceFence(ctx context.Context, tx *sql.Tx, request RadioEnsureOffRequest) error {
	var generation, token int64
	err := tx.QueryRowContext(ctx, `
SELECT resource_group_generation, fencing_token
FROM resource_group_fences WHERE resource_group_id = ?
`, request.ResourceGroupID).Scan(&generation, &token)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM resource_group_fences`).Scan(&count); err != nil {
			return fmt.Errorf("count Agent resource fences: %w", err)
		}
		if count >= store.maxResourceGroups {
			return ErrOutcomeLedgerFull
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO resource_group_fences (resource_group_id, resource_group_generation, fencing_token, operation_id)
VALUES (?, ?, ?, ?)
`, request.ResourceGroupID, request.ResourceGroupGeneration, request.FencingToken, request.OperationID); err != nil {
			return fmt.Errorf("insert Agent resource fence: %w", err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("read Agent resource fence: %w", err)
	case request.ResourceGroupGeneration < uint64(generation) || request.FencingToken <= uint64(token):
		return ErrOutcomeFenceStale
	default:
		if _, err := tx.ExecContext(ctx, `
UPDATE resource_group_fences
SET resource_group_generation = ?, fencing_token = ?, operation_id = ?
WHERE resource_group_id = ?
`, request.ResourceGroupGeneration, request.FencingToken, request.OperationID, request.ResourceGroupID); err != nil {
			return fmt.Errorf("advance Agent resource fence: %w", err)
		}
		return nil
	}
}

func (store *OutcomeStore) makeOutcomeCapacity(ctx context.Context, tx *sql.Tx) error {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM command_outcomes`).Scan(&count); err != nil {
		return fmt.Errorf("count Agent outcomes: %w", err)
	}
	remove := count - store.maxOutcomes + 1
	if remove <= 0 {
		return nil
	}
	result, err := tx.ExecContext(ctx, `
DELETE FROM command_outcomes
WHERE sequence IN (
    SELECT sequence FROM command_outcomes
    WHERE state != ?
    ORDER BY sequence
    LIMIT ?
)
`, CommandOutcomeAccepted, remove)
	if err != nil {
		return fmt.Errorf("prune terminal Agent outcomes: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read pruned Agent outcome count: %w", err)
	}
	if changed != int64(remove) {
		return ErrOutcomeLedgerFull
	}
	return nil
}

func findOutcomeTx(ctx context.Context, tx *sql.Tx, operationID string) (outcomeRecord, bool, error) {
	record, err := scanOutcomeRecord(tx.QueryRowContext(ctx, outcomeSelect+` WHERE operation_id = ?`, operationID))
	if errors.Is(err, sql.ErrNoRows) {
		return outcomeRecord{}, false, nil
	}
	if err != nil {
		return outcomeRecord{}, false, fmt.Errorf("read Agent command outcome: %w", err)
	}
	return record, true, nil
}

type outcomeScanner interface {
	Scan(...any) error
}

func scanOutcomeRecord(scanner outcomeScanner) (outcomeRecord, error) {
	var record outcomeRecord
	var requestJSON []byte
	var acceptedAt int64
	var completedAt, rfMode, callCount sql.NullInt64
	if err := scanner.Scan(
		&record.RequestDigest, &requestJSON, &record.Outcome.OperationID, &record.Outcome.Command,
		&record.Outcome.State, &record.Outcome.Code, &record.Outcome.ErrorLayer, &record.Outcome.Retryable,
		&record.Outcome.Reconciled, &record.Outcome.Observation.RF.State, &rfMode, &callCount,
		&acceptedAt, &completedAt,
	); err != nil {
		return outcomeRecord{}, err
	}
	if err := json.Unmarshal(requestJSON, &record.Request); err != nil {
		return outcomeRecord{}, fmt.Errorf("decode stored Agent command request: %w", err)
	}
	record.Outcome.AcceptedAt = time.UnixMilli(acceptedAt).UTC()
	if completedAt.Valid {
		value := time.UnixMilli(completedAt.Int64).UTC()
		record.Outcome.CompletedAt = &value
	}
	if rfMode.Valid {
		value := int(rfMode.Int64)
		record.Outcome.Observation.RF.Mode = &value
	}
	if callCount.Valid {
		value := int(callCount.Int64)
		record.Outcome.Observation.ActiveCallCount = &value
	}
	return record, nil
}

const outcomeSelect = `
SELECT request_digest, request_json, operation_id, command, state, code,
       error_layer, retryable, reconciled, rf_state, rf_mode, active_call_count,
       accepted_at_unix_ms, completed_at_unix_ms
FROM command_outcomes`

const outcomeSchema = `
BEGIN IMMEDIATE;

CREATE TABLE command_outcomes (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    operation_id TEXT NOT NULL UNIQUE,
    request_digest TEXT NOT NULL,
    request_json BLOB NOT NULL,
    command TEXT NOT NULL CHECK (command = 'radio.ensure-off'),
    state TEXT NOT NULL CHECK (state IN ('accepted', 'succeeded', 'failed', 'uncertain')),
    code TEXT NOT NULL,
    error_layer TEXT NOT NULL DEFAULT '',
    retryable INTEGER NOT NULL CHECK (retryable IN (0, 1)),
    reconciled INTEGER NOT NULL CHECK (reconciled IN (0, 1)),
    rf_state TEXT NOT NULL,
    rf_mode INTEGER,
    active_call_count INTEGER CHECK (active_call_count IS NULL OR active_call_count >= 0),
    accepted_at_unix_ms INTEGER NOT NULL CHECK (accepted_at_unix_ms > 0),
    completed_at_unix_ms INTEGER CHECK (completed_at_unix_ms IS NULL OR completed_at_unix_ms >= accepted_at_unix_ms),
    CHECK (length(operation_id) BETWEEN 1 AND 128),
    CHECK (length(request_digest) = 64)
);

CREATE INDEX command_outcomes_state_idx ON command_outcomes(state, sequence);

CREATE TABLE resource_group_fences (
    resource_group_id TEXT PRIMARY KEY,
    resource_group_generation INTEGER NOT NULL CHECK (resource_group_generation > 0),
    fencing_token INTEGER NOT NULL CHECK (fencing_token > 0),
    operation_id TEXT NOT NULL,
    CHECK (length(resource_group_id) BETWEEN 1 AND 128),
    CHECK (length(operation_id) BETWEEN 1 AND 128)
) WITHOUT ROWID;

PRAGMA user_version = 1;

COMMIT;
`
