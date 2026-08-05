package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/leonfox28/simplus/internal/domain/accessmode"
	coredb "github.com/leonfox28/simplus/internal/storage/sqlite/generated/core"
)

func (set *Set) SubscriptionProfileAccessMode(ctx context.Context, profileID string) (accessmode.Mode, bool, error) {
	if set == nil || set.Core == nil {
		return "", false, fmt.Errorf("core database is not open")
	}
	value, err := coredb.New(set.Core).GetSubscriptionProfileAccessMode(ctx, profileID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read subscription profile access mode: %w", err)
	}
	mode, err := accessmode.Parse(value)
	if err != nil {
		return "", false, fmt.Errorf("validate stored subscription profile access mode: %w", err)
	}
	return mode, true, nil
}

func (set *Set) SubscriptionProfileAccessModes(ctx context.Context, profileIDs []string) (map[string]accessmode.Mode, error) {
	if set == nil || set.Core == nil {
		return nil, fmt.Errorf("core database is not open")
	}
	if len(profileIDs) > 4096 {
		return nil, fmt.Errorf("too many subscription profiles requested")
	}
	tx, err := set.Core.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin subscription profile access mode snapshot: %w", err)
	}
	defer tx.Rollback()
	queries := coredb.New(tx)
	modes := make(map[string]accessmode.Mode, len(profileIDs))
	seen := make(map[string]struct{}, len(profileIDs))
	for _, profileID := range profileIDs {
		if _, duplicate := seen[profileID]; duplicate {
			continue
		}
		seen[profileID] = struct{}{}
		value, err := queries.GetSubscriptionProfileAccessMode(ctx, profileID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read subscription profile access mode for %s: %w", profileID, err)
		}
		mode, err := accessmode.Parse(value)
		if err != nil {
			return nil, fmt.Errorf("validate stored subscription profile access mode for %s: %w", profileID, err)
		}
		modes[profileID] = mode
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit subscription profile access mode snapshot: %w", err)
	}
	return modes, nil
}

func (set *Set) PutSubscriptionProfileAccessMode(ctx context.Context, profileID string, mode accessmode.Mode) error {
	if set == nil || set.Core == nil {
		return fmt.Errorf("core database is not open")
	}
	if !mode.Valid() {
		return fmt.Errorf("invalid subscription profile access mode %q", mode)
	}
	tx, err := set.Core.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin subscription profile access mode transaction: %w", err)
	}
	defer tx.Rollback()
	if err := coredb.New(tx).PutSubscriptionProfileAccessMode(ctx, coredb.PutSubscriptionProfileAccessModeParams{
		SubscriptionProfileID: profileID,
		AccessMode:            string(mode),
	}); err != nil {
		return fmt.Errorf("persist subscription profile access mode: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM setup_hardware_review WHERE singleton = 1`); err != nil {
		return fmt.Errorf("invalidate setup hardware review after access mode change: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit subscription profile access mode: %w", err)
	}
	return nil
}
