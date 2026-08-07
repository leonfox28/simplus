package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/leonfox28/simplus/internal/domain/pagination"
	"github.com/leonfox28/simplus/internal/domain/sms"
)

var (
	ErrSMSOperationConflict = sms.ErrOperationConflict
	ErrSMSMessageNotFound   = sms.ErrMessageNotFound
)

const inboundSMSFragmentAssemblyWindow = 10 * time.Minute

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
    error_code = '',
    updated_at_unix_ms = MAX(updated_at_unix_ms, ?),
    sent_at_unix_ms = COALESCE(sent_at_unix_ms, MAX(created_at_unix_ms, ?))
WHERE message_id = ? AND direction = 'outbound' AND (
    status = 'queued' OR
    (status = 'unconfirmed' AND (provider_message_id = '' OR provider_message_id = ?)) OR
    (status = 'sent' AND provider_message_id = ?)
)
`, providerMessageID, completedAtUnixMilli, completedAtUnixMilli, messageID, providerMessageID, providerMessageID)
	if err != nil {
		return sms.Message{}, fmt.Errorf("mark outbound SMS sent: %w", err)
	}
	if err := set.requireOneOutboundSMSMutation(ctx, messageID, result); err != nil {
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

func (set *Set) MarkOutboundSMSFailed(ctx context.Context, messageID, providerMessageID, errorCode string,
	completedAt time.Time) (sms.Message, error) {
	if set == nil || set.Messages == nil {
		return sms.Message{}, fmt.Errorf("messages database is not open")
	}
	if messageID == "" || errorCode == "" || completedAt.IsZero() {
		return sms.Message{}, fmt.Errorf("invalid outbound SMS failure")
	}
	result, err := set.Messages.ExecContext(ctx, `
UPDATE sms_messages
SET status = 'failed',
    provider_message_id = CASE WHEN ? = '' THEN provider_message_id ELSE ? END,
    error_code = ?, updated_at_unix_ms = MAX(updated_at_unix_ms, ?)
WHERE message_id = ? AND direction = 'outbound' AND (
    status = 'queued' OR
    (status = 'unconfirmed' AND (? = '' OR provider_message_id = '' OR provider_message_id = ?)) OR
    (status = 'failed' AND error_code = ? AND (? = '' OR provider_message_id = ?))
)
`, providerMessageID, providerMessageID, errorCode, completedAt.UTC().UnixMilli(), messageID,
		providerMessageID, providerMessageID, errorCode, providerMessageID, providerMessageID)
	if err != nil {
		return sms.Message{}, fmt.Errorf("mark outbound SMS failed: %w", err)
	}
	if err := set.requireOneOutboundSMSMutation(ctx, messageID, result); err != nil {
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

func (set *Set) MarkOutboundSMSUnconfirmed(ctx context.Context, messageID, providerMessageID, errorCode string,
	completedAt time.Time) (sms.Message, error) {
	if set == nil || set.Messages == nil {
		return sms.Message{}, fmt.Errorf("messages database is not open")
	}
	if messageID == "" || errorCode == "" || completedAt.IsZero() {
		return sms.Message{}, fmt.Errorf("invalid unconfirmed outbound SMS")
	}
	result, err := set.Messages.ExecContext(ctx, `
UPDATE sms_messages
SET status = 'unconfirmed',
    provider_message_id = CASE WHEN ? = '' THEN provider_message_id ELSE ? END,
    error_code = ?, updated_at_unix_ms = MAX(updated_at_unix_ms, ?)
WHERE message_id = ? AND direction = 'outbound' AND (
    status = 'queued' OR
    (status = 'unconfirmed' AND (? = '' OR provider_message_id = '' OR provider_message_id = ?))
)
`, providerMessageID, providerMessageID, errorCode, completedAt.UTC().UnixMilli(), messageID,
		providerMessageID, providerMessageID)
	if err != nil {
		return sms.Message{}, fmt.Errorf("mark outbound SMS unconfirmed: %w", err)
	}
	if err := set.requireOneOutboundSMSMutation(ctx, messageID, result); err != nil {
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

func (set *Set) MarkQueuedOutboundSMSUnconfirmed(ctx context.Context, errorCode string, completedAt time.Time) (int64, error) {
	if set == nil || set.Messages == nil {
		return 0, fmt.Errorf("messages database is not open")
	}
	if errorCode == "" || completedAt.IsZero() {
		return 0, fmt.Errorf("invalid outbound SMS reconciliation")
	}
	result, err := set.Messages.ExecContext(ctx, `
UPDATE sms_messages
SET status = 'unconfirmed', error_code = ?, updated_at_unix_ms = MAX(updated_at_unix_ms, ?)
WHERE direction = 'outbound' AND status = 'queued'
`, errorCode, completedAt.UTC().UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("mark queued outbound SMS unconfirmed: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read outbound SMS reconciliation result: %w", err)
	}
	return count, nil
}

func (set *Set) ListSMS(ctx context.Context, limit int) ([]sms.Message, error) {
	page, err := set.ListSMSPage(ctx, pagination.Request{Limit: limit}, "", "")
	return page.Items, err
}

func (set *Set) ListSMSPage(ctx context.Context, request pagination.Request, lineID, remoteAddress string) (pagination.Page[sms.Message], error) {
	if set == nil || set.Messages == nil {
		return pagination.Page[sms.Message]{}, fmt.Errorf("messages database is not open")
	}
	if request.Limit < 1 || request.Limit > pagination.MaximumLimit || (lineID == "") != (remoteAddress == "") {
		return pagination.Page[sms.Message]{}, fmt.Errorf("invalid SMS page request")
	}
	var rows *sql.Rows
	var err error
	if lineID != "" && request.After != nil {
		rows, err = set.Messages.QueryContext(ctx, `
SELECT message_id, operation_id, direction, line_id, remote_address, body, status,
       provider_message_id, error_code, created_at_unix_ms, updated_at_unix_ms, sent_at_unix_ms
FROM sms_messages
WHERE line_id = ? AND remote_address = ?
  AND (created_at_unix_ms, message_id) < (?, ?)
ORDER BY created_at_unix_ms DESC, message_id DESC
LIMIT ?
`, lineID, remoteAddress, request.After.CreatedAt.UTC().UnixMilli(), request.After.ID, request.Limit+1)
	} else if lineID != "" {
		rows, err = set.Messages.QueryContext(ctx, `
SELECT message_id, operation_id, direction, line_id, remote_address, body, status,
       provider_message_id, error_code, created_at_unix_ms, updated_at_unix_ms, sent_at_unix_ms
FROM sms_messages
WHERE line_id = ? AND remote_address = ?
ORDER BY created_at_unix_ms DESC, message_id DESC
LIMIT ?
`, lineID, remoteAddress, request.Limit+1)
	} else if request.After != nil {
		rows, err = set.Messages.QueryContext(ctx, `
SELECT message_id, operation_id, direction, line_id, remote_address, body, status,
       provider_message_id, error_code, created_at_unix_ms, updated_at_unix_ms, sent_at_unix_ms
FROM sms_messages
WHERE (created_at_unix_ms, message_id) < (?, ?)
ORDER BY created_at_unix_ms DESC, message_id DESC
LIMIT ?
`, request.After.CreatedAt.UTC().UnixMilli(), request.After.ID, request.Limit+1)
	} else {
		rows, err = set.Messages.QueryContext(ctx, `
SELECT message_id, operation_id, direction, line_id, remote_address, body, status,
       provider_message_id, error_code, created_at_unix_ms, updated_at_unix_ms, sent_at_unix_ms
FROM sms_messages
ORDER BY created_at_unix_ms DESC, message_id DESC
LIMIT ?
`, request.Limit+1)
	}
	if err != nil {
		return pagination.Page[sms.Message]{}, fmt.Errorf("list SMS: %w", err)
	}
	defer rows.Close()
	messages := make([]sms.Message, 0)
	for rows.Next() {
		message, err := scanSMS(rows)
		if err != nil {
			return pagination.Page[sms.Message]{}, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return pagination.Page[sms.Message]{}, fmt.Errorf("iterate SMS: %w", err)
	}
	page := pagination.Page[sms.Message]{Items: messages}
	if len(messages) > request.Limit {
		page.Items = messages[:request.Limit]
		last := page.Items[len(page.Items)-1]
		page.Next = &pagination.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return page, nil
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

func (set *Set) StoreInboundSMSFragment(ctx context.Context, fragment sms.InboundFragment) ([]sms.InboundFragment, bool, error) {
	if set == nil || set.Messages == nil {
		return nil, false, fmt.Errorf("messages database is not open")
	}
	if !validInboundSMSFragment(fragment) {
		return nil, false, fmt.Errorf("invalid inbound SMS fragment")
	}
	tx, err := set.Messages.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, false, fmt.Errorf("begin inbound SMS fragment transaction: %w", err)
	}
	defer tx.Rollback()
	if existingGroupID, found, err := inboundSMSFragmentGroupBySource(ctx, tx, fragment.LineID, fragment.SourceMessageID); err != nil {
		return nil, false, err
	} else if found {
		fragments, err := listInboundSMSFragments(ctx, tx, existingGroupID)
		if err != nil {
			return nil, false, err
		}
		if !matchingStoredInboundSMSFragment(fragments, fragment) {
			return nil, false, sms.ErrSourceConflict
		}
		if err := tx.Commit(); err != nil {
			return nil, false, fmt.Errorf("commit replayed inbound SMS fragment: %w", err)
		}
		return fragments, true, nil
	}
	candidateIDs, err := inboundSMSFragmentCandidateGroups(ctx, tx, fragment)
	if err != nil {
		return nil, false, err
	}
	selectedGroups := make([]string, 0, len(candidateIDs))
	for _, groupID := range candidateIDs {
		fragments, err := listInboundSMSFragments(ctx, tx, groupID)
		if err != nil {
			return nil, false, err
		}
		candidate := fragment
		candidate.GroupID = groupID
		if !consistentInboundSMSFragmentGroup(fragments, candidate) {
			return nil, false, sms.ErrSourceConflict
		}
		storedPart, found := inboundSMSFragmentPart(fragments, fragment.Part)
		if found {
			if storedPart.ReceivedAt.Equal(fragment.ReceivedAt) && storedPart.UnitCount == fragment.UnitCount &&
				bytes.Equal(storedPart.UserData, fragment.UserData) {
				selectedGroups = append(selectedGroups, groupID)
			}
			continue
		}
		if len(fragments) < fragment.Total {
			selectedGroups = append(selectedGroups, groupID)
		}
	}
	if len(selectedGroups) > 1 {
		return nil, false, sms.ErrSourceConflict
	}
	if len(selectedGroups) == 1 {
		fragment.GroupID = selectedGroups[0]
	}
	result, err := tx.ExecContext(ctx, `
INSERT INTO sms_inbound_fragments (
    group_id, part, source_message_id, line_id, sender, encoding,
    concat_reference, total, unit_count, user_data, received_at_unix_ms
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(group_id, part) DO NOTHING
`, fragment.GroupID, fragment.Part, fragment.SourceMessageID, fragment.LineID, fragment.Sender, fragment.Encoding,
		int(fragment.Reference), fragment.Total, fragment.UnitCount, fragment.UserData, fragment.ReceivedAt.UTC().UnixMilli())
	if err != nil {
		return nil, false, fmt.Errorf("store inbound SMS fragment: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, false, fmt.Errorf("read inbound SMS fragment result: %w", err)
	}
	fragments, err := listInboundSMSFragments(ctx, tx, fragment.GroupID)
	if err != nil {
		return nil, false, err
	}
	if len(fragments) == 0 || !consistentInboundSMSFragmentGroup(fragments, fragment) {
		return nil, false, sms.ErrSourceConflict
	}
	if rows == 0 {
		existing, found := inboundSMSFragmentPart(fragments, fragment.Part)
		if !found || !existing.ReceivedAt.Equal(fragment.ReceivedAt) || existing.UnitCount != fragment.UnitCount ||
			!bytes.Equal(existing.UserData, fragment.UserData) {
			return nil, false, sms.ErrSourceConflict
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, false, fmt.Errorf("commit inbound SMS fragment: %w", err)
	}
	return fragments, rows == 0, nil
}

func inboundSMSFragmentGroupBySource(ctx context.Context, tx *sql.Tx, lineID, sourceMessageID string) (string, bool, error) {
	var groupID string
	err := tx.QueryRowContext(ctx, `
SELECT group_id FROM sms_inbound_fragments WHERE line_id = ? AND source_message_id = ?
`, lineID, sourceMessageID).Scan(&groupID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("find inbound SMS fragment source: %w", err)
	}
	return groupID, true, nil
}

func inboundSMSFragmentCandidateGroups(ctx context.Context, tx *sql.Tx, fragment sms.InboundFragment) ([]string, error) {
	window := inboundSMSFragmentAssemblyWindow.Milliseconds()
	centre := fragment.ReceivedAt.UTC().UnixMilli()
	rows, err := tx.QueryContext(ctx, `
SELECT DISTINCT group_id
FROM sms_inbound_fragments
WHERE line_id = ? AND sender = ? AND encoding = ? AND concat_reference = ? AND total = ?
  AND received_at_unix_ms BETWEEN ? AND ?
ORDER BY group_id
`, fragment.LineID, fragment.Sender, fragment.Encoding, int(fragment.Reference), fragment.Total, centre-window, centre+window)
	if err != nil {
		return nil, fmt.Errorf("find inbound SMS fragment candidates: %w", err)
	}
	defer rows.Close()
	var groupIDs []string
	for rows.Next() {
		var groupID string
		if err := rows.Scan(&groupID); err != nil {
			return nil, fmt.Errorf("read inbound SMS fragment candidate: %w", err)
		}
		groupIDs = append(groupIDs, groupID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate inbound SMS fragment candidates: %w", err)
	}
	return groupIDs, nil
}

func matchingStoredInboundSMSFragment(fragments []sms.InboundFragment, expected sms.InboundFragment) bool {
	if !consistentInboundSMSFragmentGroup(fragments, expected) {
		return false
	}
	stored, found := inboundSMSFragmentPart(fragments, expected.Part)
	return found && stored.SourceMessageID == expected.SourceMessageID && stored.ReceivedAt.Equal(expected.ReceivedAt) &&
		stored.UnitCount == expected.UnitCount && bytes.Equal(stored.UserData, expected.UserData)
}

func inboundSMSFragmentPart(fragments []sms.InboundFragment, part int) (sms.InboundFragment, bool) {
	for _, fragment := range fragments {
		if fragment.Part == part {
			return fragment, true
		}
	}
	return sms.InboundFragment{}, false
}

func (set *Set) PruneInboundSMSFragments(ctx context.Context, before time.Time) (int64, error) {
	if set == nil || set.Messages == nil || before.IsZero() {
		return 0, fmt.Errorf("invalid inbound SMS fragment pruning")
	}
	result, err := set.Messages.ExecContext(ctx, `DELETE FROM sms_inbound_fragments WHERE received_at_unix_ms < ?`, before.UTC().UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("prune inbound SMS fragments: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read inbound SMS fragment pruning result: %w", err)
	}
	return count, nil
}

type smsFragmentQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func listInboundSMSFragments(ctx context.Context, querier smsFragmentQuerier, groupID string) ([]sms.InboundFragment, error) {
	rows, err := querier.QueryContext(ctx, `
SELECT group_id, source_message_id, line_id, sender, encoding, concat_reference,
       part, total, unit_count, user_data, received_at_unix_ms
FROM sms_inbound_fragments
WHERE group_id = ?
ORDER BY part
`, groupID)
	if err != nil {
		return nil, fmt.Errorf("list inbound SMS fragments: %w", err)
	}
	defer rows.Close()
	fragments := make([]sms.InboundFragment, 0)
	for rows.Next() {
		var fragment sms.InboundFragment
		var reference int
		var receivedAt int64
		if err := rows.Scan(&fragment.GroupID, &fragment.SourceMessageID, &fragment.LineID, &fragment.Sender,
			&fragment.Encoding, &reference, &fragment.Part, &fragment.Total, &fragment.UnitCount,
			&fragment.UserData, &receivedAt); err != nil {
			return nil, fmt.Errorf("read inbound SMS fragment: %w", err)
		}
		fragment.Reference = byte(reference)
		fragment.ReceivedAt = time.UnixMilli(receivedAt).UTC()
		fragments = append(fragments, fragment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate inbound SMS fragments: %w", err)
	}
	return fragments, nil
}

func validInboundSMSFragment(fragment sms.InboundFragment) bool {
	return len(fragment.GroupID) >= 16 && len(fragment.GroupID) <= 128 &&
		len(fragment.SourceMessageID) >= 16 && len(fragment.SourceMessageID) <= 128 &&
		fragment.LineID != "" && len(fragment.LineID) <= 64 && fragment.Sender != "" && len(fragment.Sender) <= 21 &&
		(fragment.Encoding == "gsm7" || fragment.Encoding == "ucs2") && fragment.Part >= 1 &&
		fragment.Total >= 2 && fragment.Total <= 255 && fragment.Part <= fragment.Total &&
		fragment.UnitCount >= 1 && fragment.UnitCount <= 255 && len(fragment.UserData) >= 1 && len(fragment.UserData) <= 140 &&
		!fragment.ReceivedAt.IsZero()
}

func consistentInboundSMSFragmentGroup(fragments []sms.InboundFragment, expected sms.InboundFragment) bool {
	if len(fragments) > expected.Total {
		return false
	}
	seen := make(map[int]struct{}, len(fragments))
	for _, fragment := range fragments {
		if fragment.GroupID != expected.GroupID || fragment.LineID != expected.LineID || fragment.Sender != expected.Sender ||
			fragment.Encoding != expected.Encoding || fragment.Reference != expected.Reference || fragment.Total != expected.Total ||
			fragment.Part < 1 || fragment.Part > fragment.Total {
			return false
		}
		if _, duplicate := seen[fragment.Part]; duplicate {
			return false
		}
		seen[fragment.Part] = struct{}{}
	}
	return true
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

func (set *Set) requireOneOutboundSMSMutation(ctx context.Context, messageID string, result sql.Result) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read SMS mutation result: %w", err)
	}
	if changed == 1 {
		return nil
	}
	if _, found, err := set.smsByID(ctx, messageID); err != nil {
		return err
	} else if !found {
		return ErrSMSMessageNotFound
	}
	return sms.ErrStateConflict
}
