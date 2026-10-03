package notification_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	app "github.com/leonfox28/simplus/internal/application/notification"
	domain "github.com/leonfox28/simplus/internal/domain/notification"
	"github.com/leonfox28/simplus/internal/notificationwebhook"
	"github.com/leonfox28/simplus/internal/security/secretbox"
	"github.com/leonfox28/simplus/internal/storage/sqlite"
)

type workerWebhooks struct {
	*notificationwebhook.Client
	mu    sync.Mutex
	calls map[string]int
}

func (w *workerWebhooks) Deliver(ctx context.Context, r app.WebhookDeliveryRequest) (app.WebhookDeliveryResult, error) {
	w.mu.Lock()
	w.calls[r.Message]++
	attempt := w.calls[r.Message]
	w.mu.Unlock()
	if r.Message == "slow" {
		<-ctx.Done()
		return app.WebhookDeliveryResult{Outcome: app.WebhookNetworkFailed}, ctx.Err()
	}
	if r.Message == "lost reply" && attempt == 1 {
		return app.WebhookDeliveryResult{Outcome: app.WebhookNetworkFailed}, errors.New("reply lost after delivery")
	}
	if r.Message == "rejected" {
		return app.WebhookDeliveryResult{Outcome: app.WebhookRejected}, errors.New("permanent rejection")
	}
	return app.WebhookDeliveryResult{Outcome: app.WebhookDelivered}, nil
}
func TestWorkerIndependentStreamsRetryOrderPermanentFailureAndShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	root := t.TempDir()
	store, err := sqlite.OpenSet(ctx, filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	keys, err := secretbox.Open(filepath.Join(root, "key"))
	if err != nil {
		t.Fatal(err)
	}
	webhooks := &workerWebhooks{Client: notificationwebhook.NewClient(), calls: map[string]int{}}
	s, err := app.New(app.Dependencies{Store: store, Secrets: keys, Webhooks: webhooks})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var clock atomic.Int64
	clock.Store(time.Now().UnixMilli())
	s.Now = func() time.Time { return time.UnixMilli(clock.Load()) }
	channel, err := s.Create(ctx, "wecom", "fixture", "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic", "", true, []string{"vowifi.connected"})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ message, object string }{{"slow", "slow"}, {"lost reply", "retry"}, {"after retry", "retry"}, {"rejected", "rejected"}, {"fast", "fast"}} {
		if err := store.EnqueueNotification(ctx, domain.Event{Key: item.message, Kind: "vowifi.connected", ObjectID: item.object, ConnectionKind: "vowifi", Message: item.message, ObservedAt: s.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan struct{})
	go func() { defer close(done); s.Run(ctx, nil, nil) }()
	waitState := func(message, state string) {
		t.Helper()
		deadline := time.After(4 * time.Second)
		for {
			var got string
			err := store.DB.QueryRow(`SELECT state FROM notification_deliveries WHERE event_key=?`, message).Scan(&got)
			if err == nil && got == state {
				return
			}
			select {
			case <-deadline:
				t.Fatalf("%s state=%s err=%v want=%s", message, got, err, state)
			case <-time.After(5 * time.Millisecond):
			}
		}
	}
	waitState("fast", "delivered")
	waitState("rejected", "failed")
	var attempt, next int64
	for deadline := time.Now().Add(4 * time.Second); ; {
		err = store.DB.QueryRow(`SELECT attempts,next_at FROM notification_deliveries WHERE event_key='lost reply' AND state='pending'`).Scan(&attempt, &next)
		if err == nil && attempt == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("retry not persisted: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if next-clock.Load() != 15000 {
		t.Fatalf("first retry delay=%d", next-clock.Load())
	}
	webhooks.mu.Lock()
	tail := webhooks.calls["after retry"]
	webhooks.mu.Unlock()
	if tail != 0 {
		t.Fatal("later event overtook failed event")
	}
	clock.Add(16000)
	waitState("after retry", "delivered")
	webhooks.mu.Lock()
	repeated := webhooks.calls["lost reply"]
	webhooks.mu.Unlock()
	if repeated != 2 {
		t.Fatalf("lost response attempts=%d", repeated)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not drain")
	}
	waitState("slow", "pending")
	counts, err := store.NotificationDeliveryCounts(t.Context(), channel.ID)
	if err != nil || counts.Pending != 1 || counts.Failed != 1 {
		t.Fatalf("final counts=%+v %v", counts, err)
	}
}
