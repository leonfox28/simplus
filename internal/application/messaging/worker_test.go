package messaging

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/application/inventory"
)

type independentInbox struct {
	noopInbox
	mu          sync.Mutex
	slowStarted chan struct{}
	fastRead    chan struct{}
	once        sync.Once
}

func (i *independentInbox) ListSMS(ctx context.Context, target InboxTarget) ([]InboxMessageReference, error) {
	if target.LineID == testManagedLineID1 {
		i.once.Do(func() { close(i.slowStarted) })
		<-ctx.Done()
		return nil, ctx.Err()
	}
	select {
	case i.fastRead <- struct{}{}:
	default:
	}
	return nil, nil
}
func TestInboundWorkerProgressAndShutdownDoNotWaitForSlowLine(t *testing.T) {
	service, _ := newTestService(t, nil)
	service.lines = managedTestLines(inventory.NewMultiSimulator())
	inbox := &independentInbox{slowStarted: make(chan struct{}), fastRead: make(chan struct{}, 2)}
	sender := senderFunc(func(context.Context, SendSMSCommand) (SendSMSResult, error) { return SendSMSResult{}, nil })
	if err := service.configureTransports(AgentNativeSMSTransport(sender, inbox)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	progress := make(chan struct{}, 2)
	go func() {
		defer close(done)
		service.RunInbound(ctx, time.Millisecond, func(_ InboundSyncResult, err error) {
			if err == nil {
				select {
				case progress <- struct{}{}:
				default:
				}
			}
		})
	}()
	for _, channel := range []<-chan struct{}{inbox.slowStarted, inbox.fastRead, progress} {
		select {
		case <-channel:
		case <-time.After(time.Second):
			t.Fatal("one slow line blocked another's progress")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not drain on cancellation")
	}
}
