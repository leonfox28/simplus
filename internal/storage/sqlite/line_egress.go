package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/leonfox28/simplus/internal/domain/lineegress"
)

func (set *Set) ListLineEgressBindings(ctx context.Context) ([]lineegress.Binding, error) {
	rows, err := set.Core.QueryContext(ctx, `SELECT line_id, mode, country_code, updated_at_utc FROM line_egress_bindings ORDER BY line_id`)
	if err != nil {
		return nil, fmt.Errorf("list line egress bindings: %w", err)
	}
	defer rows.Close()
	bindings := make([]lineegress.Binding, 0)
	for rows.Next() {
		var binding lineegress.Binding
		var updated string
		if err := rows.Scan(&binding.LineID, &binding.Mode, &binding.CountryCode, &updated); err != nil {
			return nil, fmt.Errorf("scan line egress binding: %w", err)
		}
		binding.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return nil, fmt.Errorf("parse line egress binding timestamp: %w", err)
		}
		bindings = append(bindings, binding)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate line egress bindings: %w", err)
	}
	return bindings, nil
}

func (set *Set) UpsertLineEgressBinding(ctx context.Context, binding lineegress.Binding) error {
	_, err := set.Core.ExecContext(ctx, `
INSERT INTO line_egress_bindings (line_id, mode, country_code, updated_at_utc)
VALUES (?, ?, ?, ?)
ON CONFLICT(line_id) DO UPDATE SET
  mode = excluded.mode,
  country_code = excluded.country_code,
  updated_at_utc = excluded.updated_at_utc
`, binding.LineID, binding.Mode, binding.CountryCode, binding.UpdatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("upsert line egress binding: %w", err)
	}
	return nil
}
