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
	Notify(context.Context, string, string) error
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

	deliveryCtx, cancelDelivery := context.WithTimeout(context.WithoutCancel(ctx), notificationTimeout)
	report.NotificationError = coordinator.notifications.Notify(
		deliveryCtx,
		"sms.received",
		fmt.Sprintf("[Simplus] 收到 %d 条新短信", result.Persisted),
	)
	cancelDelivery()
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
