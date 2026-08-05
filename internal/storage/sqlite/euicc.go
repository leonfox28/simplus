package sqlite

import (
	"context"
	"fmt"

	"github.com/leonfox28/simplus/internal/domain/euicc"
)

func (set *Set) ListSimulatorEUICCProfiles(ctx context.Context) ([]euicc.Profile, error) {
	rows, err := set.Core.QueryContext(ctx, `SELECT profile_id, display_name, display_identity_hint, active FROM simulator_euicc_profiles ORDER BY profile_id`)
	if err != nil {
		return nil, fmt.Errorf("list simulator eUICC profiles: %w", err)
	}
	defer rows.Close()
	values := make([]euicc.Profile, 0, 2)
	for rows.Next() {
		var value euicc.Profile
		var active int
		if err := rows.Scan(&value.ID, &value.DisplayName, &value.DisplayIdentityHint, &active); err != nil {
			return nil, err
		}
		value.Active = active == 1
		values = append(values, value)
	}
	return values, rows.Err()
}
func (set *Set) SwitchSimulatorEUICCProfile(ctx context.Context, id string) error {
	tx, err := set.Core.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE simulator_euicc_profiles SET active = 0 WHERE active = 1`); err != nil {
		return fmt.Errorf("deactivate simulator eUICC profile: %w", err)
	}
	result, err := tx.ExecContext(ctx, `UPDATE simulator_euicc_profiles SET active = 1 WHERE profile_id = ?`, id)
	if err != nil {
		return fmt.Errorf("switch simulator eUICC profile: %w", err)
	}
	if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
		return fmt.Errorf("switch simulator eUICC profile target not found")
	}
	return tx.Commit()
}
