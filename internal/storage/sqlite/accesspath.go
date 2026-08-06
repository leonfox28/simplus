package sqlite

import (
	"context"

	"github.com/leonfox28/simplus/internal/domain/accesspath"
)

func (set *Set) ListAccessPathConfigurations(ctx context.Context) ([]accesspath.Configuration, error) {
	rows, err := set.Core.QueryContext(ctx, `SELECT line_id,mode,mihomo_state FROM simulator_access_paths ORDER BY line_id`)
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
	_, err := set.Core.ExecContext(ctx, `INSERT INTO simulator_access_paths(line_id,mode,mihomo_state) VALUES(?,?,?)
		ON CONFLICT(line_id) DO UPDATE SET mode=excluded.mode,mihomo_state=excluded.mihomo_state`, value.LineID, value.Mode, value.MihomoState)
	return err
}
