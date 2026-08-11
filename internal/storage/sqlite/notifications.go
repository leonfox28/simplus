package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domain "github.com/leonfox28/simplus/internal/domain/notification"
)

const webhookNotificationColumns = `id,provider,display_name,webhook_ciphertext,webhook_hint,signing_secret_ciphertext,enabled,event_kinds,last_delivery_at_utc,last_delivery_status,last_error_code,created_at_utc,updated_at_utc`
const feishuAppNotificationColumns = `id,display_name,app_id_ciphertext,app_secret_ciphertext,recipient_open_id_ciphertext,enabled,event_kinds,last_delivery_at_utc,last_delivery_status,last_error_code,created_at_utc,updated_at_utc`

func (set *Set) ListNotificationChannels(ctx context.Context) ([]domain.Channel, error) {
	webhookRows, err := set.Core.QueryContext(ctx, `SELECT `+webhookNotificationColumns+` FROM notification_channels`)
	if err != nil {
		return nil, err
	}
	items := make([]domain.Channel, 0)
	for webhookRows.Next() {
		item, scanErr := scanWebhookNotificationChannel(webhookRows)
		if scanErr != nil {
			_ = webhookRows.Close()
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := webhookRows.Close(); err != nil {
		return nil, err
	}
	if err := webhookRows.Err(); err != nil {
		return nil, err
	}

	appRows, err := set.Core.QueryContext(ctx, `SELECT `+feishuAppNotificationColumns+` FROM feishu_app_notification_channels`)
	if err != nil {
		return nil, err
	}
	defer appRows.Close()
	for appRows.Next() {
		item, scanErr := scanFeishuAppNotificationChannel(appRows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := appRows.Err(); err != nil {
		return nil, err
	}
	sortNotificationChannels(items)
	return items, nil
}

func sortNotificationChannels(items []domain.Channel) {
	for index := 1; index < len(items); index++ {
		for current := index; current > 0; current-- {
			left, right := items[current-1], items[current]
			if left.DisplayName < right.DisplayName || (left.DisplayName == right.DisplayName && left.ID < right.ID) {
				break
			}
			items[current-1], items[current] = right, left
		}
	}
}

func (set *Set) ReadNotificationChannel(ctx context.Context, id string) (domain.Channel, bool, error) {
	item, err := scanWebhookNotificationChannel(set.Core.QueryRowContext(ctx, `SELECT `+webhookNotificationColumns+` FROM notification_channels WHERE id=?`, id))
	if err == nil {
		return item, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.Channel{}, false, err
	}
	item, err = scanFeishuAppNotificationChannel(set.Core.QueryRowContext(ctx, `SELECT `+feishuAppNotificationColumns+` FROM feishu_app_notification_channels WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Channel{}, false, nil
	}
	if err != nil {
		return domain.Channel{}, false, err
	}
	return item, true, nil
}

func scanWebhookNotificationChannel(row rowScanner) (domain.Channel, error) {
	var item domain.Channel
	var signing []byte
	var enabled int
	var events string
	var delivery, created, updated sql.NullString
	if err := row.Scan(&item.ID, &item.Provider, &item.DisplayName, &item.WebhookCiphertext, &item.WebhookHint, &signing, &enabled, &events, &delivery, &item.LastDeliveryStatus, &item.LastErrorCode, &created, &updated); err != nil {
		return item, err
	}
	item.DeliveryMode = domain.DeliveryModeWebhook
	item.SigningSecretCiphertext = signing
	if err := finishNotificationScan(&item, enabled, events, delivery, created, updated); err != nil {
		return domain.Channel{}, err
	}
	return item, nil
}

func scanFeishuAppNotificationChannel(row rowScanner) (domain.Channel, error) {
	var item domain.Channel
	var enabled int
	var events string
	var delivery, created, updated sql.NullString
	if err := row.Scan(&item.ID, &item.DisplayName, &item.FeishuAppIDCiphertext, &item.FeishuAppSecretCiphertext, &item.FeishuRecipientOpenIDCiphertext, &enabled, &events, &delivery, &item.LastDeliveryStatus, &item.LastErrorCode, &created, &updated); err != nil {
		return item, err
	}
	item.Provider = "feishu"
	item.DeliveryMode = domain.DeliveryModeFeishuApp
	item.WebhookHint = "open.feishu.cn"
	if err := finishNotificationScan(&item, enabled, events, delivery, created, updated); err != nil {
		return domain.Channel{}, err
	}
	return item, nil
}

func finishNotificationScan(item *domain.Channel, enabled int, events string, delivery, created, updated sql.NullString) error {
	item.Enabled = enabled == 1
	if err := json.Unmarshal([]byte(events), &item.EventKinds); err != nil {
		return err
	}
	if delivery.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, delivery.String)
		if err != nil {
			return err
		}
		item.LastDeliveryAt = parsed
	}
	createdAt, err := time.Parse(time.RFC3339Nano, created.String)
	if err != nil {
		return err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updated.String)
	if err != nil {
		return err
	}
	item.CreatedAt, item.UpdatedAt = createdAt, updatedAt
	return nil
}

func (set *Set) UpsertNotificationChannel(ctx context.Context, item domain.Channel) error {
	events, err := json.Marshal(item.EventKinds)
	if err != nil {
		return err
	}
	tx, err := set.Core.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var otherTable string
	switch item.DeliveryMode {
	case "", domain.DeliveryModeWebhook:
		otherTable = "feishu_app_notification_channels"
	case domain.DeliveryModeFeishuApp:
		otherTable = "notification_channels"
	default:
		return fmt.Errorf("unsupported notification delivery mode")
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+otherTable+` WHERE id=?)`, item.ID).Scan(&exists); err != nil {
		return err
	}
	if exists != 0 {
		return fmt.Errorf("notification channel id collision")
	}
	var deliveryAt any
	if !item.LastDeliveryAt.IsZero() {
		deliveryAt = item.LastDeliveryAt.UTC().Format(time.RFC3339Nano)
	}
	status := item.LastDeliveryStatus
	if status == "" {
		status = "never"
	}

	switch item.DeliveryMode {
	case "", domain.DeliveryModeWebhook:
		_, err = tx.ExecContext(ctx, `INSERT INTO notification_channels(id,provider,display_name,webhook_ciphertext,webhook_hint,signing_secret_ciphertext,enabled,event_kinds,last_delivery_at_utc,last_delivery_status,last_error_code,created_at_utc,updated_at_utc)VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id)DO UPDATE SET provider=excluded.provider,display_name=excluded.display_name,webhook_ciphertext=excluded.webhook_ciphertext,webhook_hint=excluded.webhook_hint,signing_secret_ciphertext=excluded.signing_secret_ciphertext,enabled=excluded.enabled,event_kinds=excluded.event_kinds,updated_at_utc=excluded.updated_at_utc`, item.ID, item.Provider, item.DisplayName, item.WebhookCiphertext, item.WebhookHint, item.SigningSecretCiphertext, boolInt(item.Enabled), string(events), deliveryAt, status, item.LastErrorCode, item.CreatedAt.UTC().Format(time.RFC3339Nano), item.UpdatedAt.UTC().Format(time.RFC3339Nano))
	case domain.DeliveryModeFeishuApp:
		_, err = tx.ExecContext(ctx, `INSERT INTO feishu_app_notification_channels(id,display_name,app_id_ciphertext,app_secret_ciphertext,recipient_open_id_ciphertext,enabled,event_kinds,last_delivery_at_utc,last_delivery_status,last_error_code,created_at_utc,updated_at_utc)VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id)DO UPDATE SET display_name=excluded.display_name,app_id_ciphertext=excluded.app_id_ciphertext,app_secret_ciphertext=excluded.app_secret_ciphertext,recipient_open_id_ciphertext=excluded.recipient_open_id_ciphertext,enabled=excluded.enabled,event_kinds=excluded.event_kinds,updated_at_utc=excluded.updated_at_utc`, item.ID, item.DisplayName, item.FeishuAppIDCiphertext, item.FeishuAppSecretCiphertext, item.FeishuRecipientOpenIDCiphertext, boolInt(item.Enabled), string(events), deliveryAt, status, item.LastErrorCode, item.CreatedAt.UTC().Format(time.RFC3339Nano), item.UpdatedAt.UTC().Format(time.RFC3339Nano))
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (set *Set) DeleteNotificationChannel(ctx context.Context, id string) (bool, error) {
	tx, err := set.Core.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	webhook, err := tx.ExecContext(ctx, `DELETE FROM notification_channels WHERE id=?`, id)
	if err != nil {
		return false, err
	}
	app, err := tx.ExecContext(ctx, `DELETE FROM feishu_app_notification_channels WHERE id=?`, id)
	if err != nil {
		return false, err
	}
	webhookRows, err := webhook.RowsAffected()
	if err != nil {
		return false, err
	}
	appRows, err := app.RowsAffected()
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return webhookRows+appRows == 1, nil
}

func (set *Set) RecordNotificationDelivery(ctx context.Context, id, status, errorCode string, at time.Time) error {
	formatted := at.UTC().Format(time.RFC3339Nano)
	tx, err := set.Core.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	webhook, err := tx.ExecContext(ctx, `UPDATE notification_channels SET last_delivery_at_utc=?,last_delivery_status=?,last_error_code=?,updated_at_utc=? WHERE id=?`, formatted, status, errorCode, formatted, id)
	if err != nil {
		return err
	}
	app, err := tx.ExecContext(ctx, `UPDATE feishu_app_notification_channels SET last_delivery_at_utc=?,last_delivery_status=?,last_error_code=?,updated_at_utc=? WHERE id=?`, formatted, status, errorCode, formatted, id)
	if err != nil {
		return err
	}
	webhookRows, err := webhook.RowsAffected()
	if err != nil {
		return err
	}
	appRows, err := app.RowsAffected()
	if err != nil {
		return err
	}
	if webhookRows+appRows != 1 {
		return fmt.Errorf("notification channel not found")
	}
	return tx.Commit()
}
