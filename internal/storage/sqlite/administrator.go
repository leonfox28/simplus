package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type AdministratorCredential struct {
	Username          string
	PasswordHash      string
	SessionGeneration int64
	Found             bool
}

func (set *Set) ConfigureInitialAdministrator(
	ctx context.Context,
	username string,
	passwordHash string,
	locale string,
	now time.Time,
) error {
	if set == nil || set.Core == nil {
		return fmt.Errorf("core database is not open")
	}
	tx, err := set.Core.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin initial administrator transaction: %w", err)
	}
	defer tx.Rollback()

	var state string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM installation_state WHERE singleton = 1`).Scan(&state); err != nil {
		return fmt.Errorf("read installation state for administrator setup: %w", err)
	}
	if state != InstallationUninitialized {
		return fmt.Errorf("initial administrator setup requires uninitialized state, found %q", state)
	}

	timestamp := now.UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO administrators (
    singleton, username, password_hash, password_version, session_generation, created_at_utc, updated_at_utc
) VALUES (1, ?, ?, 1, 1, ?, ?)
ON CONFLICT(singleton) DO UPDATE SET
    username = excluded.username,
    password_hash = excluded.password_hash,
    password_version = administrators.password_version + 1,
    session_generation = administrators.session_generation + 1,
    updated_at_utc = excluded.updated_at_utc
`, username, passwordHash, timestamp, timestamp); err != nil {
		return fmt.Errorf("persist initial administrator: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE installation_state
SET instance_default_locale = ?
WHERE singleton = 1
`, locale); err != nil {
		return fmt.Errorf("persist initial instance locale: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit initial administrator: %w", err)
	}
	return nil
}

func (set *Set) ReadInitialAdministrator(ctx context.Context) (username string, locale string, configured bool, err error) {
	if set == nil || set.Core == nil {
		return "", "", false, fmt.Errorf("core database is not open")
	}
	var storedUsername sql.NullString
	err = set.Core.QueryRowContext(ctx, `
SELECT administrators.username, installation_state.instance_default_locale
FROM installation_state
LEFT JOIN administrators ON administrators.singleton = installation_state.singleton
WHERE installation_state.singleton = 1
`).Scan(&storedUsername, &locale)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, errors.New("installation state singleton is missing")
	}
	if err != nil {
		return "", "", false, fmt.Errorf("read initial administrator: %w", err)
	}
	return storedUsername.String, locale, storedUsername.Valid, nil
}

func (set *Set) ReadAdministratorCredential(ctx context.Context, username string) (AdministratorCredential, error) {
	if set == nil || set.Core == nil {
		return AdministratorCredential{}, fmt.Errorf("core database is not open")
	}
	var credential AdministratorCredential
	err := set.Core.QueryRowContext(ctx, `
SELECT username, password_hash, session_generation
FROM administrators
WHERE singleton = 1 AND username = ?
`, username).Scan(&credential.Username, &credential.PasswordHash, &credential.SessionGeneration)
	if errors.Is(err, sql.ErrNoRows) {
		return AdministratorCredential{}, nil
	}
	if err != nil {
		return AdministratorCredential{}, fmt.Errorf("read administrator credential: %w", err)
	}
	credential.Found = true
	return credential, nil
}

func (set *Set) ChangeAdministratorPassword(ctx context.Context, username, passwordHash string, expectedGeneration int64, now time.Time) (bool, error) {
	if set == nil || set.Core == nil {
		return false, fmt.Errorf("core database is not open")
	}
	result, err := set.Core.ExecContext(ctx, `
UPDATE administrators
SET password_hash = ?,
    password_version = password_version + 1,
    session_generation = session_generation + 1,
    updated_at_utc = ?
WHERE singleton = 1 AND username = ? AND session_generation = ?
`, passwordHash, now.UTC().Format(time.RFC3339Nano), username, expectedGeneration)
	if err != nil {
		return false, fmt.Errorf("replace administrator password: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read administrator password replacement result: %w", err)
	}
	if rows != 1 {
		return false, nil
	}
	if err := set.DeleteAllAdministratorSessions(ctx); err != nil {
		return false, fmt.Errorf("revoke administrator sessions after password replacement: %w", err)
	}
	return true, nil
}
