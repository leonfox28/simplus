package sqlite

import (
	"context"
	"fmt"
	"github.com/leonfox28/simplus/internal/domain/accesspath"
)

func (set *Set) ListAccessPathConfigurations(ctx context.Context) ([]accesspath.Configuration, error) {
	rows, err := set.Core.QueryContext(ctx, `SELECT line_id,mode,mihomo_state FROM simulator_vowifi_lines ORDER BY line_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []accesspath.Configuration{}
	for rows.Next() {
		var value accesspath.Configuration
		if err := rows.Scan(&value.LineID, &value.Mode, &value.MihomoState); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
func (set *Set) PutAccessPathConfiguration(ctx context.Context, value accesspath.Configuration) error {
	result, err := set.Core.ExecContext(ctx, `UPDATE simulator_vowifi_lines SET mode=?,mihomo_state=? WHERE line_id=?`, value.Mode, value.MihomoState, value.LineID)
	if err != nil {
		return fmt.Errorf("update access path: %w", err)
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return fmt.Errorf("access path line not found")
	}
	return nil
}
