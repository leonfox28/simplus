package sqlite

import (
	"context"
	"fmt"
	"time"

	domain "github.com/leonfox28/simplus/internal/domain/vowifi"
)

func (set *Set) ListVoWiFiDesires(ctx context.Context) ([]domain.Desire, error) {
	rows, err := set.Core.QueryContext(ctx, `SELECT line_id, desired_active, updated_at_utc FROM vowifi_line_desires ORDER BY line_id`)
	if err != nil {
		return nil, fmt.Errorf("list Host VoWiFi desires: %w", err)
	}
	defer rows.Close()
	values := make([]domain.Desire, 0)
	for rows.Next() {
		var value domain.Desire
		var desired int
		var updated string
		if err := rows.Scan(&value.LineID, &desired, &updated); err != nil {
			return nil, fmt.Errorf("scan Host VoWiFi desire: %w", err)
		}
		value.DesiredActive = desired == 1
		value.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return nil, fmt.Errorf("parse Host VoWiFi desire timestamp: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Host VoWiFi desires: %w", err)
	}
	return values, nil
}

func (set *Set) PutVoWiFiDesire(ctx context.Context, value domain.Desire) error {
	desired := 0
	if value.DesiredActive {
		desired = 1
	}
	_, err := set.Core.ExecContext(ctx, `
INSERT INTO vowifi_line_desires (line_id, desired_active, updated_at_utc)
VALUES (?, ?, ?)
ON CONFLICT(line_id) DO UPDATE SET
  desired_active = excluded.desired_active,
  updated_at_utc = excluded.updated_at_utc
`, value.LineID, desired, value.UpdatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("persist Host VoWiFi desire: %w", err)
	}
	return nil
}
