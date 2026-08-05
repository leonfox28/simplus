package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/leonfox28/simplus/internal/domain/sms"
)

var (
	ErrSMSOperationConflict = sms.ErrOperationConflict
	ErrSMSMessageNotFound   = sms.ErrMessageNotFound
)

func (set *Set) CreateOutboundSMS(ctx context.Context, message sms.Message) (sms.Message, bool, error) {
	if set == nil || set.Messages == nil {
		return sms.Message{}, false, fmt.Errorf("messages database is not open")
	}
	if message.ID == "" || message.OperationID == "" || message.Direction != sms.DirectionOutbound ||
		message.LineID == "" || message.RemoteAddress == "" || message.Body == "" ||
		message.Status != sms.StatusQueued || message.CreatedAt.IsZero() || message.UpdatedAt.IsZero() {
		return sms.Message{}, false, fmt.Errorf("invalid queued outbound SMS")
	}
	createdAt := message.CreatedAt.UTC().UnixMilli()
	updatedAt := message.UpdatedAt.UTC().UnixMilli()
	result, err := set.Messages.ExecContext(ctx, `
INSERT INTO sms_messages (
    message_id, operation_id, direction, line_id, remote_address, body, status,
    provider_message_id, error_code, created_at_unix_ms, updated_at_unix_ms, sent_at_unix_ms
) VALUES (?, ?, 'outbound', ?, ?, ?, 'queued', '', '', ?, ?, NULL)
ON CONFLICT(operation_id) DO NOTHING
`, message.ID, message.OperationID, message.LineID, message.RemoteAddress, message.Body, createdAt, updatedAt)
	if err != nil {
		return sms.Message{}, false, fmt.Errorf("create outbound SMS: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return sms.Message{}, false, fmt.Errorf("read outbound SMS creation result: %w", err)
	}
	stored, found, err := set.smsByOperationID(ctx, message.OperationID)
	if err != nil {
		return sms.Message{}, false, err
	}
	if !found {
		return sms.Message{}, false, fmt.Errorf("created outbound SMS disappeared")
	}
	if rows == 0 {
		if stored.LineID != message.LineID || stored.RemoteAddress != message.RemoteAddress || stored.Body != message.Body || stored.Direction != sms.DirectionOutbound {
			return sms.Message{}, false, ErrSMSOperationConflict
		}
		return stored, true, nil
	}
	return stored, false, nil
}

func (set *Set) CreateInboundSMS(ctx context.Context, message sms.Message) (sms.Message, bool, error) {
	if set == nil || set.Messages == nil {
		return sms.Message{}, false, fmt.Errorf("messages database is not open")
	}
	if message.ID == "" || message.OperationID == "" || message.Direction != sms.DirectionInbound ||
		message.LineID == "" || message.RemoteAddress == "" || message.Body == "" || message.ProviderMessageID == "" ||
		message.Status != sms.StatusReceived || message.CreatedAt.IsZero() || message.UpdatedAt.IsZero() {
		return sms.Message{}, false, fmt.Errorf("invalid received SMS")
	}
	createdAt := message.CreatedAt.UTC().UnixMilli()
	updatedAt := message.UpdatedAt.UTC().UnixMilli()
	if updatedAt < createdAt {
		updatedAt = createdAt
	}
	result, err := set.Messages.ExecContext(ctx, `
INSERT INTO sms_messages (
    message_id, operation_id, direction, line_id, remote_address, body, status,
    provider_message_id, error_code, created_at_unix_ms, updated_at_unix_ms, sent_at_unix_ms
) VALUES (?, ?, 'inbound', ?, ?, ?, 'received', ?, '', ?, ?, NULL)
ON CONFLICT(line_id, provider_message_id) WHERE direction = 'inbound' AND provider_message_id <> '' DO NOTHING
`, message.ID, message.OperationID, message.LineID, message.RemoteAddress, message.Body, message.ProviderMessageID, createdAt, updatedAt)
	if err != nil {
		return sms.Message{}, false, fmt.Errorf("create inbound SMS: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return sms.Message{}, false, fmt.Errorf("read inbound SMS creation result: %w", err)
	}
	stored, found, err := set.smsInboundBySource(ctx, message.LineID, message.ProviderMessageID)
	if err != nil {
		return sms.Message{}, false, err
	}
	if !found {
		return sms.Message{}, false, fmt.Errorf("created inbound SMS disappeared")
	}
	if rows == 0 {
		if stored.RemoteAddress != message.RemoteAddress || stored.Body != message.Body || stored.Direction != sms.DirectionInbound {
			return sms.Message{}, false, sms.ErrSourceConflict
		}
		return stored, true, nil
	}
	return stored, false, nil
}

func (set *Set) MarkOutboundSMSSent(ctx context.Context, messageID, providerMessageID string, completedAt time.Time) (sms.Message, error) {
	if set == nil || set.Messages == nil {
		return sms.Message{}, fmt.Errorf("messages database is not open")
	}
	if messageID == "" || providerMessageID == "" || completedAt.IsZero() {
		return sms.Message{}, fmt.Errorf("invalid outbound SMS success")
	}
	completedAtUnixMilli := completedAt.UTC().UnixMilli()
	result, err := set.Messages.ExecContext(ctx, `
UPDATE sms_messages
SET status = 'sent', provider_message_id = ?,
    updated_at_unix_ms = MAX(created_at_unix_ms, ?),
    sent_at_unix_ms = MAX(created_at_unix_ms, ?)
WHERE message_id = ? AND direction = 'outbound' AND status = 'queued'
`, providerMessageID, completedAtUnixMilli, completedAtUnixMilli, messageID)
	if err != nil {
		return sms.Message{}, fmt.Errorf("mark outbound SMS sent: %w", err)
	}
	if err := requireOneSMSMutation(result); err != nil {
		return sms.Message{}, err
	}
	message, found, err := set.smsByID(ctx, messageID)
	if err != nil {
		return sms.Message{}, err
	}
	if !found {
		return sms.Message{}, ErrSMSMessageNotFound
	}
	return message, nil
}

func (set *Set) MarkOutboundSMSFailed(ctx context.Context, messageID, errorCode string, completedAt time.Time) (sms.Message, error) {
	if set == nil || set.Messages == nil {
		return sms.Message{}, fmt.Errorf("messages database is not open")
	}
	if messageID == "" || errorCode == "" || completedAt.IsZero() {
		return sms.Message{}, fmt.Errorf("invalid outbound SMS failure")
	}
	result, err := set.Messages.ExecContext(ctx, `
UPDATE sms_messages
SET status = 'failed', error_code = ?, updated_at_unix_ms = MAX(created_at_unix_ms, ?)
WHERE message_id = ? AND direction = 'outbound' AND status = 'queued'
`, errorCode, completedAt.UTC().UnixMilli(), messageID)
	if err != nil {
		return sms.Message{}, fmt.Errorf("mark outbound SMS failed: %w", err)
	}
	if err := requireOneSMSMutation(result); err != nil {
		return sms.Message{}, err
	}
	message, found, err := set.smsByID(ctx, messageID)
	if err != nil {
		return sms.Message{}, err
	}
	if !found {
		return sms.Message{}, ErrSMSMessageNotFound
	}
	return message, nil
}

func (set *Set) FailQueuedOutboundSMS(ctx context.Context, errorCode string, completedAt time.Time) (int64, error) {
	if set == nil || set.Messages == nil {
		return 0, fmt.Errorf("messages database is not open")
	}
	if errorCode == "" || completedAt.IsZero() {
		return 0, fmt.Errorf("invalid outbound SMS reconciliation")
	}
	result, err := set.Messages.ExecContext(ctx, `
UPDATE sms_messages
SET status = 'failed', error_code = ?, updated_at_unix_ms = MAX(created_at_unix_ms, ?)
WHERE direction = 'outbound' AND status = 'queued'
`, errorCode, completedAt.UTC().UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("reconcile queued outbound SMS: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read outbound SMS reconciliation result: %w", err)
	}
	return count, nil
}

func (set *Set) ListSMS(ctx context.Context, limit int) ([]sms.Message, error) {
	if set == nil || set.Messages == nil {
		return nil, fmt.Errorf("messages database is not open")
	}
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("SMS list limit must be from 1 through 100")
	}
	rows, err := set.Messages.QueryContext(ctx, `
SELECT message_id, operation_id, direction, line_id, remote_address, body, status,
       provider_message_id, error_code, created_at_unix_ms, updated_at_unix_ms, sent_at_unix_ms
FROM sms_messages
ORDER BY created_at_unix_ms DESC, message_id DESC
LIMIT ?
`, limit)
	if err != nil {
		return nil, fmt.Errorf("list SMS: %w", err)
	}
	defer rows.Close()
	messages := make([]sms.Message, 0)
	for rows.Next() {
		message, err := scanSMS(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate SMS: %w", err)
	}
	return messages, nil
}

func (set *Set) CountSMS(ctx context.Context) (int64, error) {
	if set == nil || set.Messages == nil {
		return 0, fmt.Errorf("messages database is not open")
	}
	var count int64
	if err := set.Messages.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_messages`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count SMS: %w", err)
	}
	return count, nil
}

func (set *Set) DeleteSMS(ctx context.Context, messageID string) error {
	if set == nil || set.Messages == nil || messageID == "" {
		return fmt.Errorf("invalid SMS deletion")
	}
	result, err := set.Messages.ExecContext(ctx, `DELETE FROM sms_messages WHERE message_id = ?`, messageID)
	if err != nil {
		return fmt.Errorf("delete SMS: %w", err)
	}
	return requireOneSMSMutation(result)
}

func (set *Set) smsByOperationID(ctx context.Context, operationID string) (sms.Message, bool, error) {
	row := set.Messages.QueryRowContext(ctx, `
SELECT message_id, operation_id, direction, line_id, remote_address, body, status,
       provider_message_id, error_code, created_at_unix_ms, updated_at_unix_ms, sent_at_unix_ms
FROM sms_messages
WHERE operation_id = ?
`, operationID)
	return scanOptionalSMS(row)
}

func (set *Set) smsByID(ctx context.Context, messageID string) (sms.Message, bool, error) {
	row := set.Messages.QueryRowContext(ctx, `
SELECT message_id, operation_id, direction, line_id, remote_address, body, status,
       provider_message_id, error_code, created_at_unix_ms, updated_at_unix_ms, sent_at_unix_ms
FROM sms_messages
WHERE message_id = ?
`, messageID)
	return scanOptionalSMS(row)
}

func (set *Set) smsInboundBySource(ctx context.Context, lineID, providerMessageID string) (sms.Message, bool, error) {
	row := set.Messages.QueryRowContext(ctx, `
SELECT message_id, operation_id, direction, line_id, remote_address, body, status,
       provider_message_id, error_code, created_at_unix_ms, updated_at_unix_ms, sent_at_unix_ms
FROM sms_messages
WHERE direction = 'inbound' AND line_id = ? AND provider_message_id = ?
`, lineID, providerMessageID)
	return scanOptionalSMS(row)
}

type smsScanner interface {
	Scan(...any) error
}

func scanOptionalSMS(scanner smsScanner) (sms.Message, bool, error) {
	message, err := scanSMS(scanner)
	if errors.Is(err, sql.ErrNoRows) {
		return sms.Message{}, false, nil
	}
	if err != nil {
		return sms.Message{}, false, err
	}
	return message, true, nil
}

func scanSMS(scanner smsScanner) (sms.Message, error) {
	var message sms.Message
	var createdAt, updatedAt int64
	var sentAt sql.NullInt64
	if err := scanner.Scan(
		&message.ID, &message.OperationID, &message.Direction, &message.LineID, &message.RemoteAddress, &message.Body, &message.Status,
		&message.ProviderMessageID, &message.ErrorCode, &createdAt, &updatedAt, &sentAt,
	); err != nil {
		return sms.Message{}, err
	}
	message.CreatedAt = time.UnixMilli(createdAt).UTC()
	message.UpdatedAt = time.UnixMilli(updatedAt).UTC()
	if sentAt.Valid {
		value := time.UnixMilli(sentAt.Int64).UTC()
		message.SentAt = &value
	}
	return message, nil
}

func requireOneSMSMutation(result sql.Result) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read SMS mutation result: %w", err)
	}
	if changed != 1 {
		return ErrSMSMessageNotFound
	}
	return nil
}
