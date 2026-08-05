package sqlite

import (
	"context"
	"fmt"
	"time"
)

func (set *Set) CompleteInitialSetup(ctx context.Context, _ string, now time.Time) (bool, error) {
	if set == nil || set.Core == nil {
		return false, fmt.Errorf("core database is not open")
	}
	result, err := set.Core.ExecContext(ctx, `
UPDATE installation_state
SET
    state = 'ready',
    initialized_at_utc = ?,
    instance_generation = instance_generation + 1
WHERE singleton = 1
  AND state = 'uninitialized'
  AND EXISTS (SELECT 1 FROM administrators WHERE singleton = 1)
  AND EXISTS (SELECT 1 FROM setup_storage WHERE singleton = 1)
`, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return false, fmt.Errorf("complete initial setup: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read initial setup completion result: %w", err)
	}
	return rows == 1, nil
}
