package messaging

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/leonfox28/simplus/internal/application/realtime"
)

const (
	syncTimeout         = 20 * time.Second
	notificationTimeout = 15 * time.Second
	minimumSyncInterval = 2 * time.Second
	minimumRetryDelay   = 15 * time.Second
	maximumRetryDelay   = 5 * time.Minute
)

var ErrSyncCoordinatorConfiguration = errors.New("messaging sync coordinator dependencies are incomplete")

type InboundSyncer interface {
	SyncInbound(context.Context) (InboundSyncResult, error)
}

type NotificationSender interface {
	NotifyReceivedSMS(context.Context, string, string) error
	NotifyReceivedSMSSummary(context.Context, int) error
}

type RealtimePublisher interface {
	Publish([]realtime.Topic, realtime.Attention)
}

type SyncReport struct {
	Result            InboundSyncResult
	SyncError         error
	NotificationError error
	DurableChange     bool
}

type SyncCoordinator struct {
	syncer        InboundSyncer
	notifications NotificationSender
	publisher     RealtimePublisher
	wait          func(context.Context, time.Duration) bool
}

func NewSyncCoordinator(syncer InboundSyncer, notifications NotificationSender, publisher RealtimePublisher) (*SyncCoordinator, error) {
	if syncer == nil || notifications == nil || publisher == nil {
		return nil, ErrSyncCoordinatorConfiguration
	}
	return &SyncCoordinator{
		syncer:        syncer,
		notifications: notifications,
		publisher:     publisher,
		wait:          waitForSyncContext,
	}, nil
}

func (coordinator *SyncCoordinator) Run(ctx context.Context, interval time.Duration, report func(SyncReport)) {
	interval = normalizedSyncInterval(interval)
	retryDelay := time.Duration(0)
	for {
		cycleReport := coordinator.runCycle(ctx)
		if report != nil {
			report(cycleReport)
		}
		if cycleReport.SyncError == nil {
			retryDelay = 0
		} else {
			retryDelay = nextSyncRetryDelay(retryDelay, interval)
		}
		delay := interval
		if retryDelay > 0 {
			delay = retryDelay
		}
		if !coordinator.wait(ctx, delay) {
			return
		}
	}
}

func (coordinator *SyncCoordinator) runCycle(ctx context.Context) SyncReport {
	syncCtx, cancelSync := context.WithTimeout(ctx, syncTimeout)
	result, syncErr := coordinator.syncer.SyncInbound(syncCtx)
	cancelSync()

	report := SyncReport{Result: result, SyncError: syncErr, DurableChange: hasDurableMessageChange(result)}
	if report.DurableChange {
		attention := realtime.Attention("")
		if result.Persisted > 0 {
			attention = realtime.AttentionSMSReceived
		}
		coordinator.publisher.Publish([]realtime.Topic{realtime.TopicMessages}, attention)
	}
	if result.Persisted == 0 {
		return report
	}

	for index, received := range result.receivedSMS {
		deliveryCtx, cancelDelivery := context.WithTimeout(context.WithoutCancel(ctx), notificationTimeout)
		if err := coordinator.notifications.NotifyReceivedSMS(deliveryCtx, received.Sender, received.Body); err != nil {
			report.NotificationError = errors.Join(
				report.NotificationError,
				fmt.Errorf("received SMS %d: %w", index+1, err),
			)
		}
		cancelDelivery()
	}
	summaryCtx, cancelSummary := context.WithTimeout(context.WithoutCancel(ctx), notificationTimeout)
	if err := coordinator.notifications.NotifyReceivedSMSSummary(summaryCtx, result.Persisted); err != nil {
		report.NotificationError = errors.Join(report.NotificationError, fmt.Errorf("received SMS summary: %w", err))
	}
	cancelSummary()
	coordinator.publisher.Publish([]realtime.Topic{realtime.TopicNotifications}, "")
	return report
}

func hasDurableMessageChange(result InboundSyncResult) bool {
	return result.Persisted > 0 || result.AlreadyKnown > 0 || result.OutboundSent > 0 ||
		result.OutboundFailed > 0 || result.OutboundUnconfirmed > 0
}

func normalizedSyncInterval(interval time.Duration) time.Duration {
	if interval < time.Second {
		return minimumSyncInterval
	}
	return interval
}

func nextSyncRetryDelay(previous, interval time.Duration) time.Duration {
	interval = normalizedSyncInterval(interval)
	if previous <= 0 {
		delay := minimumRetryDelay
		if interval*4 > delay {
			delay = interval * 4
		}
		if delay > maximumRetryDelay {
			return maximumRetryDelay
		}
		return delay
	}
	if previous >= maximumRetryDelay/2 {
		return maximumRetryDelay
	}
	delay := previous * 2
	if delay > maximumRetryDelay {
		return maximumRetryDelay
	}
	return delay
}

func waitForSyncContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
