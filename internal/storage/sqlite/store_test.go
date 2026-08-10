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

	"github.com/leonfox28/simplus/internal/domain/call"
	"github.com/leonfox28/simplus/internal/domain/pagination"
	"github.com/leonfox28/simplus/internal/domain/sms"
	"github.com/pressly/goose/v3"
)

func TestKeysetIndexMigrationsDownAndReopenPreserveHistory(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "db")
	set, err := OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 7, 16, 0, 0, 0, time.UTC)
	if _, _, err := set.CreateOutboundSMS(ctx, sms.Message{
		ID: "msg_migration_000000000", OperationID: "operation-migration-msg-01", Direction: sms.DirectionOutbound,
		LineID: "line_AQEBAQEBAQEBAQEBAQEBAQ", RemoteAddress: "13800138000", Body: "preserved",
		Status: sms.StatusQueued, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := set.CreateCall(ctx, call.Record{
		ID: "call_migration_0000000", OperationID: "operation-migration-call1", LineID: "line_AQEBAQEBAQEBAQEBAQEBAQ",
		RemoteAddress: "13800138000", Direction: call.DirectionOutbound, State: call.StateDialing,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}
	for _, dataset := range []struct {
		name, migrationPath string
		indexes             []string
		version             int64
	}{
		{MessagesDataset, "migrations/messages", []string{"sms_messages_page_idx", "sms_messages_conversation_page_idx"}, 5},
		{CallsDataset, "migrations/calls", []string{"call_records_page_idx"}, 2},
	} {
		database, err := sql.Open("sqlite", filepath.Join(root, dataset.name+".sqlite3"))
		if err != nil {
			t.Fatal(err)
		}
		migrationMu.Lock()
		goose.SetLogger(goose.NopLogger())
		err = goose.SetDialect("sqlite3")
		if err == nil {
			err = goose.DownToContext(ctx, database, dataset.migrationPath, dataset.version)
		}
		migrationMu.Unlock()
		if err != nil {
			_ = database.Close()
			t.Fatal(err)
		}
		var schemaVersion int64
		if err := database.QueryRowContext(ctx, `SELECT schema_version FROM dataset_metadata WHERE singleton = 1`).Scan(&schemaVersion); err != nil {
			t.Fatal(err)
		}
		if schemaVersion != dataset.version {
			t.Fatalf("%s down migration version=%d, want %d", dataset.name, schemaVersion, dataset.version)
		}
		for _, index := range dataset.indexes {
			var indexCount int
			if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, index).Scan(&indexCount); err != nil {
				t.Fatal(err)
			}
			if indexCount != 0 {
				t.Fatalf("%s down migration retained index %s", dataset.name, index)
			}
		}
		if err := database.Close(); err != nil {
			t.Fatal(err)
		}
	}
	set, err = OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	if messages, err := set.ListSMS(ctx, 10); err != nil || len(messages) != 1 || messages[0].Body != "preserved" {
		t.Fatalf("messages after reopen=%#v error=%v", messages, err)
	}
	if calls, err := set.ListCalls(ctx, 10); err != nil || len(calls) != 1 || calls[0].ID != "call_migration_0000000" {
		t.Fatalf("calls after reopen=%#v error=%v", calls, err)
	}
	for _, check := range []struct {
		database *sql.DB
		index    string
	}{
		{set.Messages, "sms_messages_remote_sequence_idx"},
		{set.Messages, "sms_messages_line_remote_sequence_idx"},
		{set.Calls, "call_records_page_idx"},
	} {
		var count int
		if err := check.database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, check.index).Scan(&count); err != nil || count != 1 {
			t.Fatalf("reopened index %s count=%d error=%v", check.index, count, err)
		}
	}
}

func TestMessagesRecordSequenceMigrationPreservesUnreadAndRepairsHistoricalOrder(t *testing.T) {
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
	database.SetMaxOpenConns(1)
	migrationMu.Lock()
	goose.SetLogger(goose.NopLogger())
	err = goose.SetDialect("sqlite3")
	if err == nil {
		err = goose.DownToContext(ctx, database, "migrations/messages", 7)
	}
	migrationMu.Unlock()
	if err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	base := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC).UnixMilli()
	if _, err := database.ExecContext(ctx, `
INSERT INTO sms_messages (
    message_id, operation_id, direction, line_id, remote_address, body, status,
    provider_message_id, error_code, created_at_unix_ms, updated_at_unix_ms, sent_at_unix_ms
) VALUES
    ('msg_v8_outbound_00000001', 'operation-v8-outbound-0001', 'outbound', 'line_AQEBAQEBAQEBAQEBAQEBAQ', '+12025550123', 'outbound', 'queued', '', '', ?, ?, NULL),
    ('msg_v8_inbound_000000001', 'operation-v8-inbound-00001', 'inbound', 'line_AgICAgICAgICAgICAgICAg', '+12025550123', 'inbound', 'received', 'provider-v8-inbound-1', '', ?, ?, NULL)
`, base+100, base+100, base, base+500); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO sms_message_unread (unread_id, message_id, remote_address)
VALUES (41, 'msg_v8_inbound_000000001', '+12025550123')
`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	set, err = OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	var schemaVersion int64
	if err := set.Messages.QueryRowContext(ctx, `SELECT schema_version FROM dataset_metadata WHERE singleton = 1`).Scan(&schemaVersion); err != nil || schemaVersion != 8 {
		t.Fatalf("upgraded schema version=%d error=%v", schemaVersion, err)
	}
	for index, want := range map[string]int{
		"sms_messages_remote_sequence_idx":      1,
		"sms_messages_line_remote_sequence_idx": 1,
		"sms_messages_page_idx":                 0,
		"sms_messages_conversation_page_idx":    0,
		"sms_messages_remote_page_idx":          0,
	} {
		var count int
		if err := set.Messages.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, index).Scan(&count); err != nil || count != want {
			t.Fatalf("upgraded index %s count=%d want=%d error=%v", index, count, want, err)
		}
	}
	page, err := set.ListSMSPage(ctx, pagination.Request{Limit: 10}, "", "")
	if err != nil || len(page.Items) != 2 || page.Items[0].ID != "msg_v8_inbound_000000001" || page.Items[1].ID != "msg_v8_outbound_00000001" {
		t.Fatalf("upgraded page=%#v error=%v", page, err)
	}
	var outboundSequence, inboundSequence, unreadID int64
	if err := set.Messages.QueryRowContext(ctx, `SELECT record_sequence FROM sms_messages WHERE message_id = 'msg_v8_outbound_00000001'`).Scan(&outboundSequence); err != nil {
		t.Fatal(err)
	}
	if err := set.Messages.QueryRowContext(ctx, `SELECT record_sequence FROM sms_messages WHERE message_id = 'msg_v8_inbound_000000001'`).Scan(&inboundSequence); err != nil {
		t.Fatal(err)
	}
	if outboundSequence != 1 || inboundSequence != 2 {
		t.Fatalf("backfilled sequences outbound=%d inbound=%d", outboundSequence, inboundSequence)
	}
	if err := set.Messages.QueryRowContext(ctx, `SELECT unread_id FROM sms_message_unread WHERE message_id = 'msg_v8_inbound_000000001'`).Scan(&unreadID); err != nil || unreadID != 41 {
		t.Fatalf("preserved unread id=%d error=%v", unreadID, err)
	}
	if _, _, err := set.CreateInboundSMS(ctx, sms.Message{
		ID: "msg_v8_inbound_000000002", OperationID: "operation-v8-inbound-00002", Direction: sms.DirectionInbound,
		LineID: "line_AgICAgICAgICAgICAgICAg", RemoteAddress: "+12025550123", Body: "new inbound", Status: sms.StatusReceived,
		ProviderMessageID: "provider-v8-inbound-2", CreatedAt: time.UnixMilli(base - 100).UTC(), UpdatedAt: time.UnixMilli(base + 600).UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	var newSequence, newUnreadID int64
	if err := set.Messages.QueryRowContext(ctx, `
SELECT message.record_sequence, unread.unread_id
FROM sms_messages message JOIN sms_message_unread unread USING (message_id)
WHERE message.message_id = 'msg_v8_inbound_000000002'
`).Scan(&newSequence, &newUnreadID); err != nil || newSequence <= inboundSequence || newUnreadID <= unreadID {
		t.Fatalf("new sequence=%d unread=%d error=%v", newSequence, newUnreadID, err)
	}
	var foreignKeyViolations int
	rows, err := set.Messages.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		foreignKeyViolations++
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if foreignKeyViolations != 0 {
		t.Fatalf("foreign key violations=%d", foreignKeyViolations)
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = sql.Open("sqlite", filepath.Join(root, MessagesDataset+".sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	migrationMu.Lock()
	err = goose.SetDialect("sqlite3")
	if err == nil {
		err = goose.DownToContext(ctx, database, "migrations/messages", 7)
	}
	migrationMu.Unlock()
	if err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT schema_version FROM dataset_metadata WHERE singleton = 1`).Scan(&schemaVersion); err != nil || schemaVersion != 7 {
		t.Fatalf("down schema version=%d error=%v", schemaVersion, err)
	}
	for index, want := range map[string]int{
		"sms_messages_created_at_idx":           1,
		"sms_messages_line_created_at_idx":      1,
		"sms_messages_page_idx":                 1,
		"sms_messages_conversation_page_idx":    1,
		"sms_messages_remote_page_idx":          1,
		"sms_messages_remote_sequence_idx":      0,
		"sms_messages_line_remote_sequence_idx": 0,
		"sms_message_unread_remote_idx":         1,
	} {
		var count int
		if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, index).Scan(&count); err != nil || count != want {
			t.Fatalf("down index %s count=%d want=%d error=%v", index, count, want, err)
		}
	}
	var messages, unread int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_messages`).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_message_unread`).Scan(&unread); err != nil {
		t.Fatal(err)
	}
	if messages != 3 || unread != 2 {
		t.Fatalf("down migration messages=%d unread=%d", messages, unread)
	}
	var preservedUnreadID int64
	if err := database.QueryRowContext(ctx, `SELECT MAX(unread_id) FROM sms_message_unread`).Scan(&preservedUnreadID); err != nil || preservedUnreadID != newUnreadID {
		t.Fatalf("down unread watermark=%d want=%d error=%v", preservedUnreadID, newUnreadID, err)
	}
	if _, err := database.ExecContext(ctx, `SELECT record_sequence FROM sms_messages LIMIT 1`); err == nil {
		t.Fatal("v7 table retained record_sequence")
	}
	var foreignKeysEnabled int
	if err := database.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeysEnabled); err != nil || foreignKeysEnabled != 1 {
		t.Fatalf("down foreign_keys=%d error=%v", foreignKeysEnabled, err)
	}
	rows, err = database.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	foreignKeyViolations = 0
	for rows.Next() {
		foreignKeyViolations++
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if foreignKeyViolations != 0 {
		t.Fatalf("down foreign key violations=%d", foreignKeyViolations)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	set, err = OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	if err := set.Messages.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_messages`).Scan(&messages); err != nil || messages != 3 {
		t.Fatalf("re-upgraded messages=%d error=%v", messages, err)
	}
	if err := set.Messages.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_message_unread`).Scan(&unread); err != nil || unread != 2 {
		t.Fatalf("re-upgraded unread=%d error=%v", unread, err)
	}
	if err := set.Messages.QueryRowContext(ctx, `SELECT MAX(unread_id) FROM sms_message_unread`).Scan(&preservedUnreadID); err != nil || preservedUnreadID != newUnreadID {
		t.Fatalf("re-upgraded unread watermark=%d want=%d error=%v", preservedUnreadID, newUnreadID, err)
	}
	var maximumSequence int64
	if err := set.Messages.QueryRowContext(ctx, `SELECT MAX(record_sequence) FROM sms_messages`).Scan(&maximumSequence); err != nil {
		t.Fatal(err)
	}
	if _, _, err := set.CreateInboundSMS(ctx, sms.Message{
		ID: "msg_v8_inbound_000000003", OperationID: "operation-v8-inbound-00003", Direction: sms.DirectionInbound,
		LineID: "line_AgICAgICAgICAgICAgICAg", RemoteAddress: "+12025550123", Body: "after re-upgrade", Status: sms.StatusReceived,
		ProviderMessageID: "provider-v8-inbound-3", CreatedAt: time.UnixMilli(base - 200).UTC(), UpdatedAt: time.UnixMilli(base + 700).UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := set.Messages.QueryRowContext(ctx, `
SELECT message.record_sequence, unread.unread_id
FROM sms_messages message JOIN sms_message_unread unread USING (message_id)
WHERE message.message_id = 'msg_v8_inbound_000000003'
`).Scan(&newSequence, &newUnreadID); err != nil || newSequence <= maximumSequence || newUnreadID <= preservedUnreadID {
		t.Fatalf("post-re-upgrade sequence=%d unread=%d previous sequence=%d unread=%d error=%v",
			newSequence, newUnreadID, maximumSequence, preservedUnreadID, err)
	}
}

func TestMessagesConversationMigrationInitializesOldHistoryReadAndPreservesMessages(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "db")
	set, err := OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	old, _, err := set.CreateInboundSMS(ctx, sms.Message{
		ID: "msg_migration_v7_old0001", OperationID: "operation-migration-v7-old", Direction: sms.DirectionInbound,
		LineID: "line_AQEBAQEBAQEBAQEBAQEBAQ", RemoteAddress: "+447700900123", Body: "old history",
		Status: sms.StatusReceived, ProviderMessageID: "provider-migration-v7-old", CreatedAt: now, UpdatedAt: now,
	})
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
	err = goose.SetDialect("sqlite3")
	if err == nil {
		err = goose.DownToContext(ctx, database, "migrations/messages", 6)
	}
	migrationMu.Unlock()
	if err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	for _, object := range []string{"sms_message_unread", "sms_messages_remote_page_idx", "sms_message_unread_remote_idx"} {
		var count int
		if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name = ?`, object).Scan(&count); err != nil || count != 0 {
			t.Fatalf("down migration object %s count=%d error=%v", object, count, err)
		}
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	set, err = OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	conversations, err := set.ListSMSConversationPage(ctx, pagination.Request{Limit: 10})
	if err != nil || len(conversations.Items) != 1 || conversations.Items[0].UnreadCount != 0 || conversations.Items[0].LastMessage.ID != old.ID {
		t.Fatalf("upgraded old conversations=%#v error=%v", conversations, err)
	}
	if _, _, err := set.CreateInboundSMS(ctx, sms.Message{
		ID: "msg_migration_v7_new0001", OperationID: "operation-migration-v7-new", Direction: sms.DirectionInbound,
		LineID: old.LineID, RemoteAddress: old.RemoteAddress, Body: "new history", Status: sms.StatusReceived,
		ProviderMessageID: "provider-migration-v7-new", CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}
	set, err = OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	conversations, err = set.ListSMSConversationPage(ctx, pagination.Request{Limit: 10})
	if err != nil || len(conversations.Items) != 1 || conversations.Items[0].UnreadCount != 1 {
		t.Fatalf("reopened conversations=%#v error=%v", conversations, err)
	}
	messages, err := set.ListSMS(ctx, 10)
	if err != nil || len(messages) != 2 {
		t.Fatalf("preserved messages=%#v error=%v", messages, err)
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}
	database, err = sql.Open("sqlite", filepath.Join(root, MessagesDataset+".sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	migrationMu.Lock()
	err = goose.SetDialect("sqlite3")
	if err == nil {
		err = goose.DownToContext(ctx, database, "migrations/messages", 6)
	}
	migrationMu.Unlock()
	if err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	var preserved int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_messages`).Scan(&preserved); err != nil || preserved != 2 {
		t.Fatalf("down-migrated message count=%d error=%v", preserved, err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	set, err = OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	conversations, err = set.ListSMSConversationPage(ctx, pagination.Request{Limit: 10})
	if err != nil || len(conversations.Items) != 1 || conversations.Items[0].UnreadCount != 0 {
		t.Fatalf("re-upgraded conversations=%#v error=%v", conversations, err)
	}
	messages, err = set.ListSMS(ctx, 10)
	if err != nil || len(messages) != 2 {
		t.Fatalf("re-upgraded messages=%#v error=%v", messages, err)
	}
}

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

func TestCoreV21UpgradeSeparatesLineIdentityFromAccessPath(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "db")
	set, err := OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(root, CoreDataset+".sqlite3")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	migrationMu.Lock()
	goose.SetLogger(goose.NopLogger())
	err = goose.SetDialect("sqlite3")
	if err == nil {
		err = goose.DownToContext(ctx, database, "migrations/core", 21)
	}
	migrationMu.Unlock()
	if err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 6, 8, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	const modemID = "modem_AQEBAQEBAQEBAQEBAQEBAQ"
	if _, err := database.ExecContext(ctx, `
INSERT INTO managed_modems (
  id, legacy_hardware_device_id, equipment_identity_fingerprint, usb_serial_fingerprint,
  display_name, model, transport, capability_mask, created_at_utc, updated_at_utc
) VALUES (?, '', ?, '', 'ML307A', 'ML307A', 'usb', 0, ?, ?)`, modemID, strings.Repeat("a", 64), now, now); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	lines := []struct {
		id, fingerprint, hint, mode string
	}{
		{"line_AQEBAQEBAQEBAQEBAQEBAQ", strings.Repeat("b", 64), "ICCID •••• 0001", "host-vowifi-only"},
		{"line_AgICAgICAgICAgICAgICAg", strings.Repeat("c", 64), "ICCID •••• 0002", "host-vowifi-only"},
		{"line_AwMDAwMDAwMDAwMDAwMDAw", strings.Repeat("d", 64), "ICCID •••• 0003", "host-vowifi-only"},
	}
	for _, line := range lines {
		if _, err := database.ExecContext(ctx, `
INSERT INTO managed_lines (
  id, managed_modem_id, sim_slot_index, subscription_identity_fingerprint,
  subscription_display_hint, display_name, access_mode, created_at_utc, updated_at_utc
) VALUES (?, ?, 0, ?, ?, ?, ?, ?, ?)`, line.id, modemID, line.fingerprint, line.hint, line.hint, line.mode, now, now); err != nil {
			_ = database.Close()
			t.Fatal(err)
		}
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO simulator_access_paths (line_id, mode, mihomo_state)
VALUES (?, 'mihomo-required', 'failed')`, lines[0].id); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	const subscriptionID = "subscription_AQEBAQEBAQEBAQEBAQEBAQ"
	if _, err := database.ExecContext(ctx, `
INSERT INTO mihomo_subscriptions (
  id, display_name, url_ciphertext, url_plaintext, url_hint, enabled,
  last_refresh_at_utc, last_refresh_status, node_count, last_error_code,
  created_at_utc, updated_at_utc
) VALUES (?, 'Legacy', ?, 'https://example.invalid/subscription', 'example.invalid', 1,
          ?, 'success', 1, '', ?, ?)`, subscriptionID, make([]byte, 32), now, now, now, now); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO mihomo_egress_profiles (
  id, display_name, subscription_id, selected_node_id, enabled,
  created_at_utc, updated_at_utc, selection_type, selected_country_code,
  source_cidr, line_id
) VALUES ('egress_AQEBAQEBAQEBAQEBAQEBAQ', 'Legacy JP', ?, '', 1,
          ?, ?, 'country', 'JP', '', ?)`, subscriptionID, now, now, lines[2].id); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO line_egress_bindings (line_id, mode, country_code, updated_at_utc)
VALUES (?, 'mihomo-country', 'GB', ?)`, lines[1].id, now); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO vowifi_line_desires (line_id, desired_active, updated_at_utc)
VALUES (?, 1, ?)`, lines[1].id, now); err != nil {
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
	var accessModeColumns int
	if err := set.Core.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('managed_lines') WHERE name = 'access_mode'`).Scan(&accessModeColumns); err != nil {
		t.Fatal(err)
	}
	if accessModeColumns != 0 {
		t.Fatal("managed_lines.access_mode survived the v22 migration")
	}
	var legacyTables int
	if err := set.Core.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = 'subscription_profile_access_modes'`).Scan(&legacyTables); err != nil {
		t.Fatal(err)
	}
	if legacyTables != 0 {
		t.Fatal("legacy access-mode table survived the v22 migration")
	}
	var legacyAccessPathTables int
	if err := set.Core.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = 'simulator_access_paths'`).Scan(&legacyAccessPathTables); err != nil {
		t.Fatal(err)
	}
	if legacyAccessPathTables != 0 {
		t.Fatal("legacy Simulator access-path table survived the v22 migration")
	}
	var legacyMihomoEgressTables int
	if err := set.Core.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = 'mihomo_egress_profiles'`).Scan(&legacyMihomoEgressTables); err != nil {
		t.Fatal(err)
	}
	if legacyMihomoEgressTables != 0 {
		t.Fatal("legacy Mihomo egress-profile table survived the v22 migration")
	}
	managedLines, err := set.ListManagedLines(ctx)
	if err != nil || len(managedLines) != len(lines) {
		t.Fatalf("managed lines=%#v error=%v", managedLines, err)
	}
	for index, record := range managedLines {
		if record.ID != lines[index].id || record.SubscriptionIdentityFingerprint != lines[index].fingerprint ||
			record.DisplayName != lines[index].hint || record.SubscriptionDisplayHint != lines[index].hint {
			t.Fatalf("managed line %d=%#v", index, record)
		}
	}
	bindings, err := set.ListLineEgressBindings(ctx)
	if err != nil || len(bindings) != 3 {
		t.Fatalf("bindings=%#v error=%v", bindings, err)
	}
	byLine := make(map[string]string, len(bindings))
	for _, binding := range bindings {
		byLine[binding.LineID] = binding.Mode + ":" + binding.CountryCode
	}
	if byLine[lines[0].id] != "direct:" || byLine[lines[1].id] != "mihomo-country:GB" ||
		byLine[lines[2].id] != "mihomo-country:JP" {
		t.Fatalf("migrated bindings=%#v", byLine)
	}
	desires, err := set.ListVoWiFiDesires(ctx)
	if err != nil || len(desires) != 1 || !desires[0].DesiredActive || desires[0].LineID != lines[1].id {
		t.Fatalf("migrated desires=%#v error=%v", desires, err)
	}
	rows, err := set.Core.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("v22 migration left a foreign-key violation")
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
