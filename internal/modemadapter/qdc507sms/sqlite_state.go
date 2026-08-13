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
	"reflect"
	"syscall"
	"time"

	storagefs "github.com/leonfox28/simplus/internal/storage/filesystem"
	_ "modernc.org/sqlite"
)

const sqliteStateSchemaVersion = 2
const StateFilename = "qdc507-sms-v2.sqlite3"

func OpenSQLiteStateRoot(ctx context.Context, root string) (*SQLiteStateStore, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || root == string(filepath.Separator) {
		return nil, errors.New("QDC507 SMS state root must be an absolute non-root directory")
	}
	identity, err := storagefs.PreparePrivateDirectory(root)
	if err != nil {
		return nil, fmt.Errorf("prepare QDC507 SMS state root: %w", err)
	}
	return OpenSQLiteStateStore(ctx, filepath.Join(identity.Path, StateFilename))
}

// SQLiteStateStore is the narrow durable state required to prevent duplicate
// sends and to finish partially acknowledged multipart messages after an
// Agent process restart. It is intentionally separate from the generic radio
// command outcome ledger.
type SQLiteStateStore struct {
	db   *sql.DB
	path string
}

var _ StateStore = (*SQLiteStateStore)(nil)

func OpenSQLiteStateStore(ctx context.Context, path string) (*SQLiteStateStore, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == string(filepath.Separator) {
		return nil, errors.New("QDC507 SMS state path must be an absolute non-root file")
	}
	directory := filepath.Dir(path)
	directoryIdentity, err := storagefs.PreparePrivateDirectory(directory)
	if err != nil {
		return nil, fmt.Errorf("prepare QDC507 SMS state directory: %w", err)
	}
	existingArtifacts, err := inspectSQLiteArtifacts(path, true)
	if err != nil {
		return nil, err
	}
	mainIdentity, pathExists := existingArtifacts[path]
	if !pathExists {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err != nil {
			return nil, fmt.Errorf("create QDC507 SMS state: %w", err)
		}
		info, statErr := file.Stat()
		closeErr := file.Close()
		if statErr != nil {
			return nil, fmt.Errorf("stat new QDC507 SMS state: %w", statErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close new QDC507 SMS state: %w", closeErr)
		}
		mainIdentity, err = validateSQLiteArtifact(path, info, 0o600)
		if err != nil {
			return nil, err
		}
		if err := syncSQLiteDirectory(directory); err != nil {
			return nil, fmt.Errorf("sync new QDC507 SMS state directory entry: %w", err)
		}
	}
	query := make(url.Values)
	query.Set("mode", "rw")
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}).String()
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
	if err := db.PingContext(ctx); err != nil {
		return closeOnError(fmt.Errorf("ping QDC507 SMS state: %w", err))
	}
	if err := requireSQLiteArtifactIdentity(path, mainIdentity); err != nil {
		return closeOnError(fmt.Errorf("QDC507 SMS state changed before write access: %w", err))
	}
	for _, statement := range []string{
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA synchronous = FULL`,
		`PRAGMA trusted_schema = OFF`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return closeOnError(fmt.Errorf("configure QDC507 SMS state: %w", err))
		}
	}
	var journalMode string
	if err := db.QueryRowContext(ctx, `PRAGMA journal_mode = WAL`).Scan(&journalMode); err != nil || journalMode != "wal" {
		return closeOnError(fmt.Errorf("enable QDC507 SMS WAL mode: mode=%q error=%w", journalMode, err))
	}
	var version int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return closeOnError(fmt.Errorf("read QDC507 SMS state schema version: %w", err))
	}
	if version == 0 {
		if err := initializeSQLiteStateSchema(ctx, db); err != nil {
			return closeOnError(fmt.Errorf("initialize QDC507 SMS state: %w", err))
		}
		if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
			return closeOnError(fmt.Errorf("verify QDC507 SMS state schema version: %w", err))
		}
	}
	if version != sqliteStateSchemaVersion {
		return closeOnError(fmt.Errorf("unsupported QDC507 SMS state schema version %d", version))
	}
	if err := verifySQLiteStateSchema(ctx, db); err != nil {
		return closeOnError(err)
	}
	if err := secureNewSQLiteArtifacts(path, existingArtifacts); err != nil {
		return closeOnError(err)
	}
	if _, err := inspectSQLiteArtifacts(path, true); err != nil {
		return closeOnError(err)
	}
	if err := requireSQLiteArtifactIdentity(path, mainIdentity); err != nil {
		return closeOnError(fmt.Errorf("QDC507 SMS state changed while opening: %w", err))
	}
	directoryAfter, err := storagefs.PreparePrivateDirectory(directory)
	if err != nil || directoryAfter != directoryIdentity {
		return closeOnError(errors.New("QDC507 SMS state directory identity changed while opening"))
	}
	return &SQLiteStateStore{db: db, path: path}, nil
}

func ownedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func linkCount(info os.FileInfo) uint64 {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0
	}
	return uint64(stat.Nlink)
}

type sqliteArtifactIdentity struct {
	device uint64
	inode  uint64
}

type sqliteSchemaObject struct {
	kind  string
	name  string
	table string
	sql   string
}

func sqliteArtifactPaths(path string) []string {
	return []string{path, path + "-wal", path + "-shm", path + "-journal"}
}

func inspectSQLiteArtifacts(path string, requirePrivateMode bool) (map[string]sqliteArtifactIdentity, error) {
	artifacts := make(map[string]sqliteArtifactIdentity)
	for _, artifact := range sqliteArtifactPaths(path) {
		info, err := os.Lstat(artifact)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect QDC507 SMS state artifact %s: %w", artifact, err)
		}
		mode := os.FileMode(0)
		if requirePrivateMode {
			mode = 0o600
		}
		identity, err := validateSQLiteArtifact(artifact, info, mode)
		if err != nil {
			return nil, err
		}
		artifacts[artifact] = identity
	}
	return artifacts, nil
}

func validateSQLiteArtifact(path string, info os.FileInfo, requiredMode os.FileMode) (sqliteArtifactIdentity, error) {
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return sqliteArtifactIdentity{}, fmt.Errorf("QDC507 SMS state artifact is not a regular file: %s", path)
	}
	if !ownedByCurrentUser(info) {
		return sqliteArtifactIdentity{}, fmt.Errorf("QDC507 SMS state artifact is not owned by the current uid: %s", path)
	}
	if linkCount(info) != 1 {
		return sqliteArtifactIdentity{}, fmt.Errorf("QDC507 SMS state artifact must have exactly one hard link: %s", path)
	}
	if requiredMode != 0 && info.Mode().Perm() != requiredMode {
		return sqliteArtifactIdentity{}, fmt.Errorf("QDC507 SMS state artifact permissions must be %04o, found %04o: %s", requiredMode, info.Mode().Perm(), path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return sqliteArtifactIdentity{}, fmt.Errorf("QDC507 SMS state artifact identity is unavailable: %s", path)
	}
	return sqliteArtifactIdentity{device: uint64(stat.Dev), inode: uint64(stat.Ino)}, nil
}

func requireSQLiteArtifactIdentity(path string, expected sqliteArtifactIdentity) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	actual, err := validateSQLiteArtifact(path, info, 0o600)
	if err != nil {
		return err
	}
	if actual != expected {
		return errors.New("QDC507 SMS state artifact device/inode changed")
	}
	return nil
}

func secureNewSQLiteArtifacts(path string, existing map[string]sqliteArtifactIdentity) error {
	for _, artifact := range sqliteArtifactPaths(path) {
		info, err := os.Lstat(artifact)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect QDC507 SMS state artifact %s: %w", artifact, err)
		}
		identity, err := validateSQLiteArtifact(artifact, info, 0)
		if err != nil {
			return err
		}
		if expected, found := existing[artifact]; found {
			if identity != expected {
				return fmt.Errorf("QDC507 SMS state artifact identity changed while opening: %s", artifact)
			}
			continue
		}
		fd, err := syscall.Open(artifact, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if err != nil {
			return fmt.Errorf("open new QDC507 SMS state artifact securely: %w", err)
		}
		if err := syscall.Fchmod(fd, 0o600); err != nil {
			_ = syscall.Close(fd)
			return fmt.Errorf("secure new QDC507 SMS state artifact: %w", err)
		}
		file := os.NewFile(uintptr(fd), artifact)
		if file == nil {
			_ = syscall.Close(fd)
			return errors.New("open new QDC507 SMS state artifact: invalid descriptor")
		}
		securedInfo, statErr := file.Stat()
		closeErr := file.Close()
		if statErr != nil {
			return fmt.Errorf("stat secured QDC507 SMS state artifact: %w", statErr)
		}
		securedIdentity, validateErr := validateSQLiteArtifact(artifact, securedInfo, 0o600)
		if validateErr != nil {
			return validateErr
		}
		if closeErr != nil {
			return fmt.Errorf("close secured QDC507 SMS state artifact: %w", closeErr)
		}
		if err := requireSQLiteArtifactIdentity(artifact, securedIdentity); err != nil {
			return fmt.Errorf("revalidate secured QDC507 SMS state artifact: %w", err)
		}
	}
	return nil
}

func syncSQLiteDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func initializeSQLiteStateSchema(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range sqliteStateSchemaStatements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `PRAGMA user_version = 2`); err != nil {
		return err
	}
	return tx.Commit()
}

func verifySQLiteStateSchema(ctx context.Context, db *sql.DB) error {
	actual, err := readSQLiteSchema(ctx, db)
	if err != nil {
		return fmt.Errorf("read QDC507 SMS state schema: %w", err)
	}
	expectedDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return fmt.Errorf("construct expected QDC507 SMS state schema: %w", err)
	}
	defer expectedDB.Close()
	expectedDB.SetMaxOpenConns(1)
	if err := initializeSQLiteStateSchema(ctx, expectedDB); err != nil {
		return fmt.Errorf("construct expected QDC507 SMS state schema: %w", err)
	}
	expected, err := readSQLiteSchema(ctx, expectedDB)
	if err != nil {
		return fmt.Errorf("read expected QDC507 SMS state schema: %w", err)
	}
	if !reflect.DeepEqual(actual, expected) {
		return errors.New("QDC507 SMS state schema manifest mismatch")
	}
	var integrity string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		return fmt.Errorf("QDC507 SMS state integrity check failed: result=%q error=%w", integrity, err)
	}
	return nil
}

func readSQLiteSchema(ctx context.Context, db *sql.DB) ([]sqliteSchemaObject, error) {
	rows, err := db.QueryContext(ctx, `
SELECT type, name, tbl_name, COALESCE(sql, '')
FROM sqlite_schema
WHERE name NOT LIKE 'sqlite_%'
ORDER BY type, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	objects := []sqliteSchemaObject{}
	for rows.Next() {
		var object sqliteSchemaObject
		if err := rows.Scan(&object.kind, &object.name, &object.table, &object.sql); err != nil {
			return nil, err
		}
		objects = append(objects, object)
	}
	return objects, rows.Err()
}

func (store *SQLiteStateStore) Close() error {
	if store == nil || store.db == nil {
		return nil
	}
	var joined error
	artifactsSafe := true
	if store.path != "" {
		if _, err := inspectSQLiteArtifacts(store.path, true); err != nil {
			joined = errors.Join(joined, err)
			artifactsSafe = false
		}
	}
	if artifactsSafe {
		if _, err := store.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	joined = errors.Join(joined, store.db.Close())
	if store.path != "" {
		_, artifactErr := inspectSQLiteArtifacts(store.path, true)
		joined = errors.Join(joined, artifactErr)
	}
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
	existing, found, err := findSQLiteInbound(ctx, tx, record.SubscriptionKey, record.MessageID)
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
INSERT INTO inbound_messages (message_id, subscription_key, sender, body, received_at_ns, segments_json, acknowledged)
VALUES (?, ?, ?, ?, ?, ?, ?)
`, record.MessageID, record.SubscriptionKey, record.Sender, record.Body, record.ReceivedAt.UnixNano(), segments, boolInteger(record.Acknowledged)); err != nil {
		return InboundRecord{}, false, fmt.Errorf("insert QDC507 inbound SMS: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return InboundRecord{}, false, fmt.Errorf("commit QDC507 inbound SMS: %w", err)
	}
	return cloneInbound(record), false, nil
}

func (store *SQLiteStateStore) FindInbound(ctx context.Context, subscriptionKey, messageID string) (InboundRecord, bool, error) {
	if store == nil || store.db == nil {
		return InboundRecord{}, false, errors.New("QDC507 SMS state is unavailable")
	}
	return findSQLiteInbound(ctx, store.db, subscriptionKey, messageID)
}

func (store *SQLiteStateStore) ListInbound(ctx context.Context, subscriptionKey string) ([]InboundRecord, error) {
	if store == nil || store.db == nil {
		return nil, errors.New("QDC507 SMS state is unavailable")
	}
	rows, err := store.db.QueryContext(ctx, inboundSelect+`
 WHERE subscription_key = ? AND acknowledged = 0
 ORDER BY received_at_ns, message_id
`, subscriptionKey)
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
	existing, found, err := findSQLiteInbound(ctx, tx, record.SubscriptionKey, record.MessageID)
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
UPDATE inbound_messages SET segments_json = ?, acknowledged = ? WHERE message_id = ? AND subscription_key = ?
`, segments, boolInteger(record.Acknowledged), record.MessageID, record.SubscriptionKey); err != nil {
		return fmt.Errorf("update QDC507 inbound SMS progress: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit QDC507 inbound SMS progress: %w", err)
	}
	return nil
}

func (store *SQLiteStateStore) PutOperation(ctx context.Context, record operationRecord) (operationRecord, bool, error) {
	if store == nil {
		return operationRecord{}, false, errors.New("QDC507 SMS state is unavailable")
	}
	return putSQLiteOperation(ctx, store.db, record)
}

func (store *SQLiteStateStore) UpdateOperation(ctx context.Context, record operationRecord) error {
	if store == nil {
		return errors.New("QDC507 SMS state is unavailable")
	}
	return updateSQLiteOperation(ctx, store.db, record)
}

func (store *SQLiteStateStore) DeleteOperation(ctx context.Context, record operationRecord) error {
	if store == nil {
		return errors.New("QDC507 SMS state is unavailable")
	}
	return deleteSQLiteOperation(ctx, store.db, record)
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

func findSQLiteInbound(ctx context.Context, query sqliteQuery, subscriptionKey, messageID string) (InboundRecord, bool, error) {
	record, err := scanSQLiteInbound(query.QueryRowContext(ctx, inboundSelect+` WHERE subscription_key = ? AND message_id = ?`, subscriptionKey, messageID))
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
		&record.MessageID, &record.SubscriptionKey, &record.Sender, &record.Body, &receivedAt, &segments, &acknowledged,
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
SELECT operation_id, subscription_key, kind, request_digest, state, submission_message_id, submitted_at_ns
FROM operations WHERE operation_id = ?
`, operationID).Scan(
		&record.OperationID, &record.SubscriptionKey, &record.Kind, &digest, &record.State, &record.Submission.MessageID, &submittedAt,
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
SELECT message_id, subscription_key, sender, body, received_at_ns, segments_json, acknowledged
FROM inbound_messages`

var sqliteStateSchemaStatements = []string{`
CREATE TABLE inbound_messages (
    message_id TEXT NOT NULL,
    subscription_key TEXT NOT NULL CHECK (length(subscription_key) = 64 AND subscription_key NOT GLOB '*[^0-9a-f]*'),
    sender TEXT NOT NULL,
    body TEXT NOT NULL,
    received_at_ns INTEGER NOT NULL,
    segments_json BLOB NOT NULL,
    acknowledged INTEGER NOT NULL CHECK (acknowledged IN (0, 1)),
    PRIMARY KEY (subscription_key, message_id)
)`, `
CREATE INDEX inbound_messages_pending ON inbound_messages (subscription_key, acknowledged, received_at_ns, message_id)`, `
CREATE TABLE operations (
    operation_id TEXT PRIMARY KEY,
    subscription_key TEXT NOT NULL CHECK (length(subscription_key) = 64 AND subscription_key NOT GLOB '*[^0-9a-f]*'),
    kind TEXT NOT NULL CHECK (kind IN ('send', 'acknowledge')),
    request_digest BLOB NOT NULL CHECK (length(request_digest) = 32),
    state TEXT NOT NULL CHECK (state IN ('accepted', 'succeeded', 'outcome-unknown')),
    submission_message_id TEXT NOT NULL,
    submitted_at_ns INTEGER NOT NULL
)`,
}
