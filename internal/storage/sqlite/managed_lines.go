package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/leonfox28/simplus/internal/domain/accessmode"
	domain "github.com/leonfox28/simplus/internal/domain/line"
)

func (set *Set) ListManagedLines(ctx context.Context) ([]domain.Record, error) {
	if set == nil || set.Core == nil {
		return nil, fmt.Errorf("core database is not open")
	}
	rows, err := set.Core.QueryContext(ctx, `
SELECT id, managed_modem_id, sim_slot_index, subscription_identity_fingerprint,
       subscription_display_hint, display_name, access_mode, created_at_utc, updated_at_utc
FROM managed_lines
ORDER BY created_at_utc, id`)
	if err != nil {
		return nil, fmt.Errorf("query managed lines: %w", err)
	}
	defer rows.Close()
	result := []domain.Record{}
	for rows.Next() {
		var record domain.Record
		var mode, created, updated string
		if err := rows.Scan(&record.ID, &record.ManagedModemID, &record.SIMSlotIndex,
			&record.SubscriptionIdentityFingerprint, &record.SubscriptionDisplayHint,
			&record.DisplayName, &mode, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan managed line: %w", err)
		}
		record.AccessMode = accessmode.Mode(mode)
		record.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, fmt.Errorf("parse managed line created time: %w", err)
		}
		record.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return nil, fmt.Errorf("parse managed line updated time: %w", err)
		}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate managed lines: %w", err)
	}
	return result, nil
}

func (set *Set) CreateManagedLine(ctx context.Context, record domain.Record) error {
	if set == nil || set.Core == nil {
		return fmt.Errorf("core database is not open")
	}
	_, err := set.Core.ExecContext(ctx, `
INSERT INTO managed_lines (
  id, managed_modem_id, sim_slot_index, subscription_identity_fingerprint,
  subscription_display_hint, display_name, access_mode, created_at_utc, updated_at_utc
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, record.ID, record.ManagedModemID, record.SIMSlotIndex,
		record.SubscriptionIdentityFingerprint, record.SubscriptionDisplayHint, record.DisplayName,
		string(record.AccessMode), record.CreatedAt.UTC().Format(time.RFC3339Nano), record.UpdatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert managed line: %w", err)
	}
	return nil
}

func (set *Set) UpdateManagedLine(ctx context.Context, lineID, displayName string, mode accessmode.Mode, updatedAt time.Time) error {
	if set == nil || set.Core == nil {
		return fmt.Errorf("core database is not open")
	}
	result, err := set.Core.ExecContext(ctx, `
UPDATE managed_lines
SET display_name = ?, access_mode = ?, updated_at_utc = ?
WHERE id = ?`, displayName, string(mode), updatedAt.UTC().Format(time.RFC3339Nano), lineID)
	if err != nil {
		return fmt.Errorf("update managed line: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read managed line update result: %w", err)
	}
	if changed != 1 {
		return domain.ErrNotFound
	}
	return nil
}
