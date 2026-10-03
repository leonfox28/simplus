package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	domain "github.com/leonfox28/simplus/internal/domain/notification"
	db "github.com/leonfox28/simplus/internal/storage/sqlite/generated/core"
)

func enqueueNotification(ctx context.Context, q *db.Queries, event domain.Event) error {
	if !slices.Contains(domain.EventKinds, event.Kind) || event.Key == "" || event.ObjectID == "" || event.Message == "" || event.ObservedAt.IsZero() {
		return errors.New("invalid notification event")
	}
	// Connection observations may be consumed after a monitoring gap. Do not
	// deliver observations older than the current subscription. Other events
	// originate at this durable commit (SMS provider timestamps may be older).
	subscriptionAt := time.Now().UnixMilli()
	if event.ConnectionKind != "" {
		subscriptionAt = event.ObservedAt.UnixMilli()
	}
	return q.EnqueueNotification(ctx, db.EnqueueNotificationParams{EventKey: event.Key, EventKind: event.Kind, ObjectID: event.ObjectID, ConnectionKind: event.ConnectionKind, Message: event.Message, FeishuMessage: event.FeishuMessage, ObservedAt: event.ObservedAt.UnixMilli(), Now: 0, SubscriptionAt: subscriptionAt})
}
func (set *Set) EnqueueNotification(ctx context.Context, event domain.Event) error {
	return enqueueNotification(ctx, db.New(set.DB), event)
}
func (set *Set) ClaimNotificationDeliveries(ctx context.Context, now time.Time, limit int) ([]domain.Delivery, error) {
	rows, err := db.New(set.DB).ClaimNotificationDeliveries(ctx, db.ClaimNotificationDeliveriesParams{Now: now.UnixMilli(), LeaseUntil: now.Add(30 * time.Second).UnixMilli(), BatchLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	items := make([]domain.Delivery, 0, len(rows))
	for _, r := range rows {
		items = append(items, domain.Delivery{ID: r.ID, ChannelID: r.ChannelID, EventKind: r.EventKind, ObjectID: r.ObjectID, ConnectionKind: r.ConnectionKind, Message: r.Message, Attempts: r.Attempts})
	}
	return items, nil
}
func (set *Set) FinishNotificationDelivery(ctx context.Context, item domain.Delivery, state, code string, next time.Time) error {
	return db.New(set.DB).FinishNotificationDelivery(ctx, db.FinishNotificationDeliveryParams{ID: item.ID, Attempts: item.Attempts, State: state, LastError: code, NextAt: next.UnixMilli()})
}
func (set *Set) ResetInterruptedNotifications(ctx context.Context, now time.Time) error {
	return db.New(set.DB).ResetInterruptedNotifications(ctx, now.UnixMilli())
}
func (set *Set) NotificationDeliveryCounts(ctx context.Context, id string) (domain.Counts, error) {
	c, err := db.New(set.DB).CountNotificationDeliveries(ctx, id)
	return domain.Counts{Pending: c.Pending, Failed: c.Failed}, err
}
func (set *Set) ReadConnectionCursor(ctx context.Context, source string) (domain.Cursor, error) {
	c, err := db.New(set.DB).ReadConnectionCursor(ctx, source)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Cursor{Source: source}, nil
	}
	return domain.Cursor{Source: source, Instance: c.Instance, Sequence: uint64(c.Sequence), Gaps: c.Gaps}, err
}

// CommitConnectionObservations atomically advances the source cursor, updates
// confirmed state, and selects the subscriptions that exist at that instant.
func (set *Set) CommitConnectionObservations(ctx context.Context, previous, next domain.Cursor, observations []domain.Observation) error {
	tx, err := set.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := db.New(tx)
	if next.Source != "" {
		stored, readErr := q.ReadConnectionCursor(ctx, next.Source)
		if readErr != nil && !errors.Is(readErr, sql.ErrNoRows) {
			return readErr
		}
		if stored.Instance != previous.Instance || uint64(stored.Sequence) != previous.Sequence || stored.Gaps != previous.Gaps {
			return errors.New("connection cursor conflict")
		}
	}
	for _, o := range observations {
		if o.ObjectID == "" || o.DisplayName == "" || (o.Kind != "cellular" && o.Kind != "vowifi") || o.ObservedAt.IsZero() {
			return errors.New("invalid connection observation")
		}
		old, readErr := q.ReadConnectionCheckpoint(ctx, db.ReadConnectionCheckpointParams{ObjectID: o.ObjectID, Kind: o.Kind})
		found := readErr == nil
		if readErr != nil && !errors.Is(readErr, sql.ErrNoRows) {
			return readErr
		}
		if next.Source == "" && found && o.ObservedAt.UnixMilli() <= old.ObservedAt {
			continue
		}
		revision := old.Revision
		changed := (!found && o.Connected) || (found && (old.Connected == 1) != o.Connected)
		if changed {
			revision++
			state := "disconnected"
			if o.Connected {
				state = "connected"
			}
			if err := enqueueNotification(ctx, q, domain.Event{Key: fmt.Sprintf("connection:%s:%s:%d", o.Kind, o.ObjectID, revision), Kind: o.Kind + "." + state, ObjectID: o.ObjectID, ConnectionKind: o.Kind, Message: domain.ConnectionMessage(o), ObservedAt: o.ObservedAt}); err != nil {
				return err
			}
		}
		if err := q.WriteConnectionCheckpoint(ctx, db.WriteConnectionCheckpointParams{ObjectID: o.ObjectID, Kind: o.Kind, Connected: int64(boolInt(o.Connected)), Revision: revision, ObservedAt: o.ObservedAt.UnixMilli()}); err != nil {
			return err
		}
	}
	if next.Source != "" {
		if err := q.WriteConnectionCursor(ctx, db.WriteConnectionCursorParams{Source: next.Source, Instance: next.Instance, Sequence: int64(next.Sequence), Gaps: next.Gaps}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (set *Set) HasConnectionCheckpoint(ctx context.Context, objectID, kind string) (bool, error) {
	_, err := db.New(set.DB).ReadConnectionCheckpoint(ctx, db.ReadConnectionCheckpointParams{ObjectID: objectID, Kind: kind})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
