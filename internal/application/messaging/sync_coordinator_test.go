package messaging

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/application/realtime"
)

type syncerFunc func(context.Context) (InboundSyncResult, error)

func (f syncerFunc) SyncInbound(ctx context.Context) (InboundSyncResult, error) { return f(ctx) }

type fakeSyncPublisher struct {
	topics    []realtime.Topic
	attention realtime.Attention
}

func (p *fakeSyncPublisher) Publish(topics []realtime.Topic, a realtime.Attention) {
	p.topics = append(p.topics, topics...)
	if a != "" {
		p.attention = a
	}
}
func TestCoordinatorPublishesCommittedProgressEvenWhenAcknowledgementFails(t *testing.T) {
	p := &fakeSyncPublisher{}
	c, err := NewSyncCoordinator(syncerFunc(func(context.Context) (InboundSyncResult, error) {
		return InboundSyncResult{Persisted: 1}, errors.New("ack failed")
	}), p)
	if err != nil {
		t.Fatal(err)
	}
	r := c.runCycle(t.Context())
	if !r.DurableChange || r.SyncError == nil || p.attention != realtime.AttentionSMSReceived || len(p.topics) != 2 {
		t.Fatalf("report=%+v publication=%+v", r, p)
	}
}
func TestCoordinatorStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	p := &fakeSyncPublisher{}
	c, _ := NewSyncCoordinator(syncerFunc(func(context.Context) (InboundSyncResult, error) { cancel(); return InboundSyncResult{}, nil }), p)
	done := make(chan struct{})
	go func() { defer close(done); c.Run(ctx, time.Hour, nil) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("coordinator did not exit")
	}
}
func TestSyncRetryDelayBacksOffAndCaps(t *testing.T) {
	delay := time.Duration(0)
	want := []time.Duration{15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 5 * time.Minute, 5 * time.Minute}
	for index, expected := range want {
		delay = nextSyncRetryDelay(delay, 2*time.Second)
		if delay != expected {
			t.Fatalf("delay %d = %s, want %s", index, delay, expected)
		}
	}
	if got := nextSyncRetryDelay(0, 10*time.Second); got != 40*time.Second {
		t.Fatalf("long interval retry = %s", got)
	}
	if got := nextSyncRetryDelay(0, 2*time.Minute); got != 5*time.Minute {
		t.Fatalf("capped initial retry = %s", got)
	}
}
