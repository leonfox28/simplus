package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

func (set *Set) ReplaceSetupBootstrap(ctx context.Context, tokenHash [32]byte, createdAtUnix, expiresAtUnix int64) error {
	if set == nil || set.Runtime == nil {
		return fmt.Errorf("runtime database is not open")
	}
	tx, err := set.Runtime.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin bootstrap replacement: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM setup_sessions`); err != nil {
		return fmt.Errorf("revoke setup sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO setup_bootstrap_grant (
			singleton, token_hash, created_at_unix, expires_at_unix, consumed_at_unix
		) VALUES (1, ?, ?, ?, NULL)
		ON CONFLICT(singleton) DO UPDATE SET
			token_hash = excluded.token_hash,
			created_at_unix = excluded.created_at_unix,
			expires_at_unix = excluded.expires_at_unix,
			consumed_at_unix = NULL
	`, tokenHash[:], createdAtUnix, expiresAtUnix); err != nil {
		return fmt.Errorf("store setup bootstrap grant: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit bootstrap replacement: %w", err)
	}
	return nil
}

func (set *Set) ConsumeSetupBootstrap(
	ctx context.Context,
	bootstrapHash [32]byte,
	sessionHash [32]byte,
	consumedAtUnix int64,
	sessionExpiresAtUnix int64,
) (bool, error) {
	if set == nil || set.Runtime == nil {
		return false, fmt.Errorf("runtime database is not open")
	}
	tx, err := set.Runtime.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin bootstrap consumption: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		UPDATE setup_bootstrap_grant
		SET consumed_at_unix = ?
		WHERE singleton = 1
		  AND token_hash = ?
		  AND consumed_at_unix IS NULL
		  AND expires_at_unix > ?
	`, consumedAtUnix, bootstrapHash[:], consumedAtUnix)
	if err != nil {
		return false, fmt.Errorf("consume setup bootstrap grant: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read bootstrap consumption result: %w", err)
	}
	if rows != 1 {
		return false, nil
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM setup_sessions WHERE expires_at_unix <= ?`, consumedAtUnix); err != nil {
		return false, fmt.Errorf("delete expired setup sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO setup_sessions (
			token_hash, created_at_unix, expires_at_unix, selected_flow, updated_at_unix
		) VALUES (?, ?, ?, 'create-new', ?)
	`, sessionHash[:], consumedAtUnix, sessionExpiresAtUnix, consumedAtUnix); err != nil {
		return false, fmt.Errorf("create setup session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit bootstrap consumption: %w", err)
	}
	return true, nil
}

func (set *Set) RevokeSetupAuthorization(ctx context.Context) error {
	if set == nil || set.Runtime == nil {
		return fmt.Errorf("runtime database is not open")
	}
	tx, err := set.Runtime.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin setup authorization revocation: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM setup_sessions`); err != nil {
		return fmt.Errorf("delete setup sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM setup_bootstrap_grant`); err != nil {
		return fmt.Errorf("delete setup bootstrap grant: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit setup authorization revocation: %w", err)
	}
	return nil
}

func (set *Set) ReadSetupSession(ctx context.Context, tokenHash [32]byte, nowUnix int64) (int64, string, bool, error) {
	if set == nil || set.Runtime == nil {
		return 0, "", false, fmt.Errorf("runtime database is not open")
	}
	var expiresAtUnix int64
	var selectedFlow string
	err := set.Runtime.QueryRowContext(ctx, `
		SELECT expires_at_unix, selected_flow
		FROM setup_sessions
		WHERE token_hash = ? AND expires_at_unix > ?
	`, tokenHash[:], nowUnix).Scan(&expiresAtUnix, &selectedFlow)
	if err == sql.ErrNoRows {
		return 0, "", false, nil
	}
	if err != nil {
		return 0, "", false, fmt.Errorf("read setup session: %w", err)
	}
	return expiresAtUnix, selectedFlow, true, nil
}
