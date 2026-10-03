package notification

import (
	"context"
	"errors"
	"sync"
	"time"

	domain "github.com/leonfox28/simplus/internal/domain/notification"
)

type DeliveryQueue interface {
	EnqueueNotification(context.Context, domain.Event) error
	ClaimNotificationDeliveries(context.Context, time.Time, int) ([]domain.Delivery, error)
	FinishNotificationDelivery(context.Context, domain.Delivery, string, string, time.Time) error
	ResetInterruptedNotifications(context.Context, time.Time) error
	NotificationDeliveryCounts(context.Context, string) (domain.Counts, error)
}

var ErrDeliveryPermanent = errors.New("notification permanently rejected")

func RetryDelay(attempt int64) time.Duration {
	delay := 15 * time.Second
	for attempt > 1 && delay < 5*time.Minute {
		delay *= 2
		attempt--
	}
	if delay > 5*time.Minute {
		return 5 * time.Minute
	}
	return delay
}

// Run owns a bounded worker pool. Only the first pending event in each
// object/kind/channel stream is claimable; slow streams do not block others.
func (s *Service) Run(ctx context.Context, onChange func(), report func(error)) {
	for {
		if err := s.Store.ResetInterruptedNotifications(ctx, s.Now()); err == nil {
			break
		} else if report != nil {
			report(err)
		}
		timer := time.NewTimer(RetryDelay(1))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
	var workers sync.WaitGroup
	defer workers.Wait()
	complete := make(chan error, 20)
	active := 0
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	dispatch := func() {
		if active >= 20 || ctx.Err() != nil {
			return
		}
		items, err := s.Store.ClaimNotificationDeliveries(ctx, s.Now(), 20-active)
		if err != nil {
			if report != nil {
				report(err)
			}
			return
		}
		for _, item := range items {
			active++
			workers.Go(func() { complete <- s.deliverPending(ctx, item) })
		}
	}
	dispatch()
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-complete:
			active--
			if err != nil && report != nil {
				report(err)
			}
			if onChange != nil {
				onChange()
			}
			dispatch()
		case <-ticker.C:
			dispatch()
		}
	}
}

func (s *Service) deliverPending(ctx context.Context, item domain.Delivery) error {
	deliveryCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	channel, found, err := s.Store.ReadNotificationChannel(deliveryCtx, item.ChannelID)
	state, code := "delivered", ""
	if err == nil && (!found || !channel.Enabled || !contains(channel.EventKinds, item.EventKind)) {
		state, code = "cancelled", "SUBSCRIPTION_CANCELLED"
	} else {
		if err == nil {
			_, err = s.deliver(deliveryCtx, channel, item.Message)
		}
		if err != nil {
			state, code = "pending", "DELIVERY_TEMPORARY_FAILED"
			if errors.Is(err, ErrDeliveryPermanent) {
				state, code = "failed", "DELIVERY_REJECTED"
			}
		}
	}
	// A bounded final write outlives cancellation, but Run waits for it before
	// the database is closed. A lost provider response is deliberately retried.
	finalize, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer done()
	if finishErr := s.Store.FinishNotificationDelivery(finalize, item, state, code, s.Now().Add(RetryDelay(item.Attempts))); finishErr != nil {
		return finishErr
	}
	return nil
}
