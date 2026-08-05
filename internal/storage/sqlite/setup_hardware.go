package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func (set *Set) ConfirmSetupHardware(ctx context.Context, digest string, deviceCount, lineCount int, now time.Time) error {
	if set == nil || set.Core == nil {
		return fmt.Errorf("core database is not open")
	}
	tx, err := set.Core.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin setup hardware transaction: %w", err)
	}
	defer tx.Rollback()
	var state string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM installation_state WHERE singleton = 1`).Scan(&state); err != nil {
		return fmt.Errorf("read installation state for hardware review: %w", err)
	}
	if state != InstallationUninitialized {
		return fmt.Errorf("hardware review requires uninitialized state, found %q", state)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO setup_hardware_review (
    singleton, inventory_digest_sha256, device_count, line_count, reviewed_at_utc
) VALUES (1, ?, ?, ?, ?)
ON CONFLICT(singleton) DO UPDATE SET
    inventory_digest_sha256 = excluded.inventory_digest_sha256,
    device_count = excluded.device_count,
    line_count = excluded.line_count,
    reviewed_at_utc = excluded.reviewed_at_utc
`, digest, deviceCount, lineCount, now.UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("persist setup hardware review: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit setup hardware review: %w", err)
	}
	return nil
}

func (set *Set) ReadSetupHardwareReview(ctx context.Context) (digest string, deviceCount, lineCount int, reviewed bool, err error) {
	if set == nil || set.Core == nil {
		return "", 0, 0, false, fmt.Errorf("core database is not open")
	}
	err = set.Core.QueryRowContext(ctx, `
SELECT inventory_digest_sha256, device_count, line_count
FROM setup_hardware_review
WHERE singleton = 1
`).Scan(&digest, &deviceCount, &lineCount)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, 0, false, nil
	}
	if err != nil {
		return "", 0, 0, false, fmt.Errorf("read setup hardware review: %w", err)
	}
	return digest, deviceCount, lineCount, true, nil
}

func (set *Set) InvalidateSetupHardwareReview(ctx context.Context) error {
	if set == nil || set.Core == nil {
		return fmt.Errorf("core database is not open")
	}
	if _, err := set.Core.ExecContext(ctx, `DELETE FROM setup_hardware_review WHERE singleton = 1`); err != nil {
		return fmt.Errorf("invalidate setup hardware review: %w", err)
	}
	return nil
}
