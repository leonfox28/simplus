package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	domain "github.com/leonfox28/simplus/internal/domain/notification"
)

func (set *Set) ListNotificationChannels(ctx context.Context) ([]domain.Channel, error) {
	rows, err := set.Core.QueryContext(ctx, `SELECT id,provider,display_name,webhook_ciphertext,webhook_hint,signing_secret_ciphertext,enabled,event_kinds,last_delivery_at_utc,last_delivery_status,last_error_code,created_at_utc,updated_at_utc FROM notification_channels ORDER BY display_name,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.Channel, 0)
	for rows.Next() {
		item, err := scanNotificationChannel(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (set *Set) ReadNotificationChannel(ctx context.Context, id string) (domain.Channel, bool, error) {
	item, err := scanNotificationChannel(set.Core.QueryRowContext(ctx, `SELECT id,provider,display_name,webhook_ciphertext,webhook_hint,signing_secret_ciphertext,enabled,event_kinds,last_delivery_at_utc,last_delivery_status,last_error_code,created_at_utc,updated_at_utc FROM notification_channels WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return domain.Channel{}, false, nil
	}
	if err != nil {
		return domain.Channel{}, false, err
	}
	return item, true, nil
}
func scanNotificationChannel(row rowScanner) (domain.Channel, error) {
	var item domain.Channel
	var signing []byte
	var enabled int
	var events string
	var delivery, created, updated sql.NullString
	if err := row.Scan(&item.ID, &item.Provider, &item.DisplayName, &item.WebhookCiphertext, &item.WebhookHint, &signing, &enabled, &events, &delivery, &item.LastDeliveryStatus, &item.LastErrorCode, &created, &updated); err != nil {
		return item, err
	}
	item.SigningSecretCiphertext = signing
	item.Enabled = enabled == 1
	if err := json.Unmarshal([]byte(events), &item.EventKinds); err != nil {
		return item, err
	}
	var err error
	if delivery.Valid {
		item.LastDeliveryAt, err = time.Parse(time.RFC3339Nano, delivery.String)
		if err != nil {
			return item, err
		}
	}
	item.CreatedAt, err = time.Parse(time.RFC3339Nano, created.String)
	if err != nil {
		return item, err
	}
	item.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated.String)
	return item, err
}
func (set *Set) UpsertNotificationChannel(ctx context.Context, item domain.Channel) error {
	events, err := json.Marshal(item.EventKinds)
	if err != nil {
		return err
	}
	_, err = set.Core.ExecContext(ctx, `INSERT INTO notification_channels(id,provider,display_name,webhook_ciphertext,webhook_hint,signing_secret_ciphertext,enabled,event_kinds,last_delivery_at_utc,last_delivery_status,last_error_code,created_at_utc,updated_at_utc)VALUES(?,?,?,?,?,?,?,?,NULL,'never','',?,?) ON CONFLICT(id)DO UPDATE SET provider=excluded.provider,display_name=excluded.display_name,webhook_ciphertext=excluded.webhook_ciphertext,webhook_hint=excluded.webhook_hint,signing_secret_ciphertext=excluded.signing_secret_ciphertext,enabled=excluded.enabled,event_kinds=excluded.event_kinds,updated_at_utc=excluded.updated_at_utc`, item.ID, item.Provider, item.DisplayName, item.WebhookCiphertext, item.WebhookHint, item.SigningSecretCiphertext, boolInt(item.Enabled), string(events), item.CreatedAt.UTC().Format(time.RFC3339Nano), item.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}
func (set *Set) DeleteNotificationChannel(ctx context.Context, id string) (bool, error) {
	result, err := set.Core.ExecContext(ctx, `DELETE FROM notification_channels WHERE id=?`, id)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}
func (set *Set) RecordNotificationDelivery(ctx context.Context, id, status, errorCode string, at time.Time) error {
	result, err := set.Core.ExecContext(ctx, `UPDATE notification_channels SET last_delivery_at_utc=?,last_delivery_status=?,last_error_code=?,updated_at_utc=? WHERE id=?`, at.UTC().Format(time.RFC3339Nano), status, errorCode, at.UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("notification channel not found")
	}
	return nil
}
