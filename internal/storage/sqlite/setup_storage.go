package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func (set *Set) SetupDataRoot() string {
	if set == nil {
		return ""
	}
	return set.Root
}

func (set *Set) ConfigureSetupStorage(
	ctx context.Context,
	dataRoot string,
	recordingsRoot string,
	dataDevice uint64,
	dataInode uint64,
	recordingsDevice uint64,
	recordingsInode uint64,
	now time.Time,
) error {
	if set == nil || set.Core == nil {
		return fmt.Errorf("core database is not open")
	}
	if dataRoot != set.Root {
		return errors.New("setup data root does not match the open database root")
	}
	tx, err := set.Core.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin setup storage transaction: %w", err)
	}
	defer tx.Rollback()
	var state string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM installation_state WHERE singleton = 1`).Scan(&state); err != nil {
		return fmt.Errorf("read installation state for storage setup: %w", err)
	}
	if state != InstallationUninitialized {
		return fmt.Errorf("storage setup requires uninitialized state, found %q", state)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO setup_storage (
    singleton,
    data_root,
    recordings_root,
    data_device,
    data_inode,
    recordings_device,
    recordings_inode,
    configured_at_utc
) VALUES (1, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(singleton) DO UPDATE SET
    data_root = excluded.data_root,
    recordings_root = excluded.recordings_root,
    data_device = excluded.data_device,
    data_inode = excluded.data_inode,
    recordings_device = excluded.recordings_device,
    recordings_inode = excluded.recordings_inode,
    configured_at_utc = excluded.configured_at_utc
`,
		dataRoot,
		recordingsRoot,
		int64(dataDevice),
		int64(dataInode),
		int64(recordingsDevice),
		int64(recordingsInode),
		now.UTC().Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("persist setup storage: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit setup storage: %w", err)
	}
	return nil
}

func (set *Set) ReadSetupStorage(ctx context.Context) (
	dataRoot string,
	recordingsRoot string,
	dataDevice uint64,
	dataInode uint64,
	recordingsDevice uint64,
	recordingsInode uint64,
	configured bool,
	err error,
) {
	if set == nil || set.Core == nil {
		return "", "", 0, 0, 0, 0, false, fmt.Errorf("core database is not open")
	}
	var storedDataDevice, storedDataInode, storedRecordingsDevice, storedRecordingsInode int64
	err = set.Core.QueryRowContext(ctx, `
SELECT data_root, recordings_root, data_device, data_inode, recordings_device, recordings_inode
FROM setup_storage
WHERE singleton = 1
`).Scan(
		&dataRoot,
		&recordingsRoot,
		&storedDataDevice,
		&storedDataInode,
		&storedRecordingsDevice,
		&storedRecordingsInode,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return set.Root, "", 0, 0, 0, 0, false, nil
	}
	if err != nil {
		return "", "", 0, 0, 0, 0, false, fmt.Errorf("read setup storage: %w", err)
	}
	return dataRoot,
		recordingsRoot,
		uint64(storedDataDevice),
		uint64(storedDataInode),
		uint64(storedRecordingsDevice),
		uint64(storedRecordingsInode),
		true,
		nil
}
