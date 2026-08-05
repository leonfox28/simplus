package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

func (set *Set) ReadAdministrator(ctx context.Context) (username, passwordHash, locale string, sessionGeneration int64, found bool, err error) {
	if set == nil || set.Core == nil {
		return "", "", "", 0, false, fmt.Errorf("core database is not open")
	}
	err = set.Core.QueryRowContext(ctx, `
SELECT administrators.username, administrators.password_hash, installation_state.instance_default_locale, administrators.session_generation
FROM administrators
JOIN installation_state ON installation_state.singleton = administrators.singleton
WHERE administrators.singleton = 1
`).Scan(&username, &passwordHash, &locale, &sessionGeneration)
	if err == sql.ErrNoRows {
		return "", "", "", 0, false, nil
	}
	if err != nil {
		return "", "", "", 0, false, fmt.Errorf("read administrator: %w", err)
	}
	return username, passwordHash, locale, sessionGeneration, true, nil
}

func (set *Set) CreateAdministratorSession(
	ctx context.Context,
	tokenHash, csrfHash [32]byte,
	username string,
	sessionGeneration int64,
	createdAtUnix, expiresAtUnix int64,
) error {
	if set == nil || set.Runtime == nil {
		return fmt.Errorf("runtime database is not open")
	}
	tx, err := set.Runtime.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin administrator session creation: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM administrator_sessions WHERE expires_at_unix <= ?`, createdAtUnix); err != nil {
		return fmt.Errorf("delete expired administrator sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO administrator_sessions (
    token_hash, csrf_hash, username, session_generation, created_at_unix, expires_at_unix, last_seen_at_unix
) VALUES (?, ?, ?, ?, ?, ?, ?)
`, tokenHash[:], csrfHash[:], username, sessionGeneration, createdAtUnix, expiresAtUnix, createdAtUnix); err != nil {
		return fmt.Errorf("create administrator session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit administrator session creation: %w", err)
	}
	return nil
}

func (set *Set) ReadAdministratorSession(ctx context.Context, tokenHash [32]byte, nowUnix int64) (username string, csrfHash [32]byte, sessionGeneration, expiresAtUnix int64, found bool, err error) {
	if set == nil || set.Runtime == nil {
		return "", csrfHash, 0, 0, false, fmt.Errorf("runtime database is not open")
	}
	var rawCSRF []byte
	err = set.Runtime.QueryRowContext(ctx, `
SELECT username, csrf_hash, session_generation, expires_at_unix
FROM administrator_sessions
WHERE token_hash = ? AND expires_at_unix > ?
`, tokenHash[:], nowUnix).Scan(&username, &rawCSRF, &sessionGeneration, &expiresAtUnix)
	if err == sql.ErrNoRows {
		return "", csrfHash, 0, 0, false, nil
	}
	if err != nil {
		return "", csrfHash, 0, 0, false, fmt.Errorf("read administrator session: %w", err)
	}
	if len(rawCSRF) != len(csrfHash) {
		return "", csrfHash, 0, 0, false, fmt.Errorf("stored administrator CSRF hash has invalid length")
	}
	copy(csrfHash[:], rawCSRF)
	if _, err := set.Runtime.ExecContext(ctx, `UPDATE administrator_sessions SET last_seen_at_unix = ? WHERE token_hash = ?`, nowUnix, tokenHash[:]); err != nil {
		return "", csrfHash, 0, 0, false, fmt.Errorf("touch administrator session: %w", err)
	}
	return username, csrfHash, sessionGeneration, expiresAtUnix, true, nil
}

func (set *Set) DeleteAdministratorSession(ctx context.Context, tokenHash [32]byte) error {
	if set == nil || set.Runtime == nil {
		return fmt.Errorf("runtime database is not open")
	}
	if _, err := set.Runtime.ExecContext(ctx, `DELETE FROM administrator_sessions WHERE token_hash = ?`, tokenHash[:]); err != nil {
		return fmt.Errorf("delete administrator session: %w", err)
	}
	return nil
}

func (set *Set) DeleteAllAdministratorSessions(ctx context.Context) error {
	if set == nil || set.Runtime == nil {
		return fmt.Errorf("runtime database is not open")
	}
	if _, err := set.Runtime.ExecContext(ctx, `DELETE FROM administrator_sessions`); err != nil {
		return fmt.Errorf("delete all administrator sessions: %w", err)
	}
	return nil
}
