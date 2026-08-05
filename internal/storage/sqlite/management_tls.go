package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/leonfox28/simplus/internal/domain/managementtls"
)

func (set *Set) ConfigureManagementTLS(ctx context.Context, configuration managementtls.Configuration) error {
	if set == nil || set.Core == nil {
		return fmt.Errorf("core database is not open")
	}
	tx, err := set.Core.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin management TLS transaction: %w", err)
	}
	defer tx.Rollback()
	var state string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM installation_state WHERE singleton = 1`).Scan(&state); err != nil {
		return fmt.Errorf("read installation state for management TLS: %w", err)
	}
	if state != InstallationUninitialized {
		return fmt.Errorf("management TLS setup requires uninitialized state, found %q", state)
	}
	var leafNotAfter any
	if !configuration.LeafNotAfter.IsZero() {
		leafNotAfter = configuration.LeafNotAfter.UTC().Format(time.RFC3339Nano)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO management_tls (
    singleton,
    mode,
    listen_host,
    listen_port,
    subject_alternative_names,
    ca_certificate_pem,
    leaf_certificate_pem,
    encrypted_ca_private_key,
    encrypted_leaf_private_key,
    root_fingerprint_sha256,
    leaf_not_after_utc,
    confirmed,
    configured_at_utc
) VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(singleton) DO UPDATE SET
    mode = excluded.mode,
    listen_host = excluded.listen_host,
    listen_port = excluded.listen_port,
    subject_alternative_names = excluded.subject_alternative_names,
    ca_certificate_pem = excluded.ca_certificate_pem,
    leaf_certificate_pem = excluded.leaf_certificate_pem,
    encrypted_ca_private_key = excluded.encrypted_ca_private_key,
    encrypted_leaf_private_key = excluded.encrypted_leaf_private_key,
    root_fingerprint_sha256 = excluded.root_fingerprint_sha256,
    leaf_not_after_utc = excluded.leaf_not_after_utc,
    confirmed = excluded.confirmed,
    configured_at_utc = excluded.configured_at_utc
`,
		string(configuration.Mode),
		configuration.ListenHost,
		configuration.ListenPort,
		strings.Join(configuration.SubjectAlternativeNames, "\n"),
		nonNilBytes(configuration.CACertificatePEM),
		nonNilBytes(configuration.LeafCertificatePEM),
		nonNilBytes(configuration.EncryptedCAPrivateKey),
		nonNilBytes(configuration.EncryptedLeafPrivateKey),
		configuration.RootFingerprintSHA256,
		leafNotAfter,
		boolInteger(configuration.Confirmed),
		configuration.ConfiguredAt.UTC().Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("persist management TLS: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit management TLS: %w", err)
	}
	return nil
}

func (set *Set) ReadManagementTLS(ctx context.Context) (managementtls.Configuration, bool, error) {
	if set == nil || set.Core == nil {
		return managementtls.Configuration{}, false, fmt.Errorf("core database is not open")
	}
	var configuration managementtls.Configuration
	var mode string
	var sans string
	var leafNotAfter sql.NullString
	var confirmed int
	var configuredAt string
	err := set.Core.QueryRowContext(ctx, `
SELECT
    mode,
    listen_host,
    listen_port,
    subject_alternative_names,
    ca_certificate_pem,
    leaf_certificate_pem,
    encrypted_ca_private_key,
    encrypted_leaf_private_key,
    root_fingerprint_sha256,
    leaf_not_after_utc,
    confirmed,
    configured_at_utc
FROM management_tls
WHERE singleton = 1
`).Scan(
		&mode,
		&configuration.ListenHost,
		&configuration.ListenPort,
		&sans,
		&configuration.CACertificatePEM,
		&configuration.LeafCertificatePEM,
		&configuration.EncryptedCAPrivateKey,
		&configuration.EncryptedLeafPrivateKey,
		&configuration.RootFingerprintSHA256,
		&leafNotAfter,
		&confirmed,
		&configuredAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return managementtls.Configuration{}, false, nil
	}
	if err != nil {
		return managementtls.Configuration{}, false, fmt.Errorf("read management TLS: %w", err)
	}
	configuration.Mode = managementtls.Mode(mode)
	if sans != "" {
		configuration.SubjectAlternativeNames = strings.Split(sans, "\n")
	}
	if leafNotAfter.Valid {
		configuration.LeafNotAfter, err = time.Parse(time.RFC3339Nano, leafNotAfter.String)
		if err != nil {
			return managementtls.Configuration{}, false, fmt.Errorf("parse management TLS leaf expiry: %w", err)
		}
	}
	configuration.ConfiguredAt, err = time.Parse(time.RFC3339Nano, configuredAt)
	if err != nil {
		return managementtls.Configuration{}, false, fmt.Errorf("parse management TLS configured time: %w", err)
	}
	configuration.Confirmed = confirmed == 1
	return configuration, true, nil
}

func (set *Set) ConfirmManagementTLS(ctx context.Context, rootFingerprint string, now time.Time) (bool, error) {
	if set == nil || set.Core == nil {
		return false, fmt.Errorf("core database is not open")
	}
	result, err := set.Core.ExecContext(ctx, `
UPDATE management_tls
SET confirmed = 1, configured_at_utc = ?
WHERE singleton = 1
  AND mode IN ('local-ca', 'imported')
  AND root_fingerprint_sha256 = ?
`, now.UTC().Format(time.RFC3339Nano), rootFingerprint)
	if err != nil {
		return false, fmt.Errorf("confirm management TLS: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read management TLS confirmation result: %w", err)
	}
	return rows == 1, nil
}

func nonNilBytes(value []byte) []byte {
	if value == nil {
		return []byte{}
	}
	return value
}

func boolInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}
