package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/domain/sms"
	"github.com/pressly/goose/v3"
)

func TestOpenSetCreatesFiveSecuredWALDatabases(t *testing.T) {
	root := filepath.Join(t.TempDir(), "db")
	set, err := OpenSet(context.Background(), root)
	if err != nil {
		t.Fatalf("OpenSet() error = %v", err)
	}
	defer set.Close()

	state, err := set.InstallationState(context.Background())
	if err != nil {
		t.Fatalf("InstallationState() error = %v", err)
	}
	if state != InstallationUninitialized {
		t.Fatalf("state = %q", state)
	}

	markerInfo, err := os.Stat(filepath.Join(root, storageMarkerName))
	if err != nil {
		t.Fatal(err)
	}
	if markerInfo.Mode().Perm() != 0o600 {
		t.Fatalf("marker mode = %o", markerInfo.Mode().Perm())
	}
	for _, name := range datasetNames {
		path := filepath.Join(root, name+".sqlite3")
		for _, artifact := range []string{path, path + "-wal", path + "-shm"} {
			info, err := os.Stat(artifact)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				t.Fatalf("stat %s: %v", artifact, err)
			}
			if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
				t.Fatalf("mode %s = %o", artifact, info.Mode().Perm())
			}
		}
	}

	var journalMode string
	if err := set.Core.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %q", journalMode)
	}
	var trustedSchema int
	if err := set.Core.QueryRow(`PRAGMA trusted_schema`).Scan(&trustedSchema); err != nil {
		t.Fatal(err)
	}
	if trustedSchema != 0 {
		t.Fatalf("trusted_schema = %d", trustedSchema)
	}
}

func TestOpenSetRejectsMissingRequiredSchemaObject(t *testing.T) {
	root := filepath.Join(t.TempDir(), "db")
	set, err := OpenSet(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := set.Core.Exec(`DROP TABLE installation_state`); err != nil {
		t.Fatal(err)
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := OpenSet(context.Background(), root); err == nil {
		t.Fatal("OpenSet() accepted a database missing a required table")
	} else if !strings.Contains(err.Error(), "schema manifest mismatch") {
		t.Fatalf("OpenSet() error = %v", err)
	}
}

func TestOpenSetRejectsAlteredMigrationSchema(t *testing.T) {
	root := filepath.Join(t.TempDir(), "db")
	set, err := OpenSet(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := set.Core.Exec(`ALTER TABLE goose_db_version ADD COLUMN unexpected TEXT`); err != nil {
		t.Fatal(err)
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := OpenSet(context.Background(), root); err == nil {
		t.Fatal("OpenSet() accepted an altered Goose migration table")
	} else if !strings.Contains(err.Error(), "schema manifest mismatch") {
		t.Fatalf("OpenSet() error = %v", err)
	}
}

func TestMessagesV3DatabaseUpgradesToUnconfirmedOutcome(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "db")
	set, err := OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}

	database, err := sql.Open("sqlite", filepath.Join(root, MessagesDataset+".sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	migrationMu.Lock()
	goose.SetLogger(goose.NopLogger())
	goose.SetBaseFS(migrationFiles)
	err = goose.SetDialect("sqlite3")
	if err == nil {
		err = goose.DownToContext(ctx, database, "migrations/messages", 3)
	}
	migrationMu.Unlock()
	if err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	if _, err := database.ExecContext(ctx, `
INSERT INTO sms_messages (
    message_id, operation_id, direction, line_id, remote_address, body, status,
    provider_message_id, error_code, created_at_unix_ms, updated_at_unix_ms, sent_at_unix_ms
) VALUES (?, ?, 'outbound', ?, ?, ?, 'queued', '', '', ?, ?, NULL)
`, "msg_upgrade0123456789012345", "operation-upgrade01234567", "simulator-line-1", "13800138000", "upgrade", now.UnixMilli(), now.UnixMilli()); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	set, err = OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	count, err := set.MarkQueuedOutboundSMSUnconfirmed(ctx, "SEND_OUTCOME_UNKNOWN_AFTER_RESTART", now.Add(time.Minute))
	if err != nil || count != 1 {
		t.Fatalf("mark upgraded queue unconfirmed: count=%d error=%v", count, err)
	}
	messages, err := set.ListSMS(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Status != sms.StatusUnconfirmed {
		t.Fatalf("upgraded messages = %#v", messages)
	}
}

func TestOpenSetRejectsCorruptDatabaseAtIntegrityPreflight(t *testing.T) {
	root := filepath.Join(t.TempDir(), "db")
	set, err := OpenSet(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(root, "core.sqlite3")
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt(make([]byte, 16), 0); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := OpenSet(context.Background(), root); err == nil {
		t.Fatal("OpenSet() accepted a corrupt database")
	}
}

func TestOpenSetAcceptsTrustedStickyAncestor(t *testing.T) {
	stickyParent := filepath.Join(t.TempDir(), "sticky")
	if err := os.Mkdir(stickyParent, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(stickyParent, os.ModeSticky|0o777); err != nil {
		t.Fatal(err)
	}
	set, err := OpenSet(context.Background(), filepath.Join(stickyParent, "db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenSetRejectsUntrustedWritableAncestor(t *testing.T) {
	sharedParent := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(sharedParent, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sharedParent, 0o777); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(sharedParent, "db")
	if _, err := OpenSet(context.Background(), root); err == nil {
		t.Fatal("OpenSet() accepted a data root below a world-writable ancestor")
	} else if !strings.Contains(err.Error(), "writable by group or other users without sticky protection") {
		t.Fatalf("OpenSet() error = %v", err)
	}
	if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("OpenSet() created a root below an untrusted ancestor: %v", err)
	}
}

func TestOpenSetRejectsRelativeRoot(t *testing.T) {
	if _, err := OpenSet(context.Background(), filepath.Join(".dev", "db")); err == nil {
		t.Fatal("OpenSet() accepted a relative root")
	}
}

func TestOpenSetRejectsSwappedDatasetFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "db")
	set, err := OpenSet(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}

	corePath := filepath.Join(root, "core.sqlite3")
	contactsPath := filepath.Join(root, "contacts.sqlite3")
	temporaryPath := filepath.Join(root, "swap.sqlite3")
	if err := os.Rename(corePath, temporaryPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(contactsPath, corePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temporaryPath, contactsPath); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(corePath)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := OpenSet(context.Background(), root); err == nil {
		t.Fatal("OpenSet() accepted swapped dataset files")
	}
	after, err := os.ReadFile(corePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("OpenSet() mutated a swapped dataset before rejecting it")
	}
}

func TestOpenSetRejectsUnidentifiedDatabaseWithoutMutation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "db")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareRoot(root); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "core.sqlite3")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE unrelated (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := OpenSet(context.Background(), root); err == nil {
		t.Fatal("OpenSet() accepted an unidentified non-empty database")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("OpenSet() mutated an unidentified database before rejecting it")
	}
}

func TestOpenSetRejectsSymlinkDataset(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	root := filepath.Join(t.TempDir(), "db")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareRoot(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "target.sqlite3"), filepath.Join(root, "core.sqlite3")); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSet(context.Background(), root); err == nil {
		t.Fatal("OpenSet() accepted a symlink dataset")
	}
}

func TestOpenSetRejectsViewOnlyDatabaseWithoutMutation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "db")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareRoot(root); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "core.sqlite3")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE VIEW unrelated_view AS SELECT 1 AS value`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSet(context.Background(), root); err == nil {
		t.Fatal("OpenSet() accepted a view-only unidentified database")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("OpenSet() mutated a view-only database before rejecting it")
	}
}

func TestOpenSetRejectsHardLinkedDataset(t *testing.T) {
	root := filepath.Join(t.TempDir(), "db")
	set, err := OpenSet(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}
	corePath := filepath.Join(root, "core.sqlite3")
	aliasPath := filepath.Join(t.TempDir(), "core-alias.sqlite3")
	if err := os.Link(corePath, aliasPath); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSet(context.Background(), root); err == nil {
		t.Fatal("OpenSet() accepted a hard-linked dataset")
	}
}

func TestOpenSetDoesNotChangeUntrustedRootPermissions(t *testing.T) {
	root := filepath.Join(t.TempDir(), "db")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSet(context.Background(), root); err == nil {
		t.Fatal("OpenSet() accepted an overly broad unmarked root")
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("OpenSet() changed root mode to %04o", info.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(root, storageMarkerName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("OpenSet() created a marker in an untrusted root: %v", err)
	}
}

func TestOpenSetCanonicalizesSymlinkedAncestor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	realParent := t.TempDir()
	aliasParent := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(realParent, aliasParent); err != nil {
		t.Fatal(err)
	}
	set, err := OpenSet(context.Background(), filepath.Join(aliasParent, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	canonicalParent, err := filepath.EvalSymlinks(realParent)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(canonicalParent, "db")
	if set.Root != want {
		t.Fatalf("canonical root = %q, want %q", set.Root, want)
	}
}

func TestOpenSetRejectsSymlinkRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "db-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSet(context.Background(), link); err == nil {
		t.Fatal("OpenSet() accepted a symlink root")
	}
}
