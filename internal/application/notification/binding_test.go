package notification

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	domain "github.com/leonfox28/simplus/internal/domain/notification"
)

type registrarFake struct {
	begin func(context.Context) (FeishuRegistration, error)
	poll  func(context.Context, FeishuRegistration) (FeishuRegistrationResult, error)
}

func (fake registrarFake) Begin(ctx context.Context) (FeishuRegistration, error) {
	return fake.begin(ctx)
}
func (fake registrarFake) Poll(ctx context.Context, registration FeishuRegistration) (FeishuRegistrationResult, error) {
	return fake.poll(ctx, registration)
}

type messengerFunc func(context.Context, FeishuRegistrationResult, string) error

func (function messengerFunc) SendText(ctx context.Context, result FeishuRegistrationResult, message string) error {
	return function(ctx, result, message)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type failingUpsertStore struct{ *memoryStore }

func (store failingUpsertStore) UpsertNotificationChannel(context.Context, domain.Channel) error {
	return errors.New("synthetic persistence failure")
}

func TestFeishuBindingTestsBeforePersistingAndSupportsAppDelivery(t *testing.T) {
	store := &memoryStore{}
	service := newTestService(t, store)
	now := time.Date(2026, 8, 11, 1, 2, 3, 0, time.UTC)
	service.Now = func() time.Time { return now }
	result := FeishuRegistrationResult{AppID: "cli_synthetic", AppSecret: "synthetic-secret", OpenID: "ou_synthetic", TenantBrand: "feishu"}
	poll := make(chan struct{})
	var attemptDone <-chan struct{}
	var mu sync.Mutex
	var calls []string
	registrar := registrarFake{
		begin: func(context.Context) (FeishuRegistration, error) {
			return FeishuRegistration{DeviceCode: "device-synthetic", VerificationURL: "https://accounts.feishu.cn/synthetic", ExpiresAt: now.Add(time.Minute)}, nil
		},
		poll: func(ctx context.Context, _ FeishuRegistration) (FeishuRegistrationResult, error) {
			attemptDone = ctx.Done()
			<-poll
			return result, nil
		},
	}
	messenger := messengerFunc(func(_ context.Context, credentials FeishuRegistrationResult, message string) error {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, credentials.OpenID+"|"+message)
		if len(calls) == 1 && len(store.channels) != 0 {
			t.Fatal("channel persisted before binding test")
		}
		return nil
	})
	changed := make(chan struct{}, 1)
	service.configureFeishuBinding(context.Background(), registrar, messenger, func() { changed <- struct{}{} })
	waiting, err := service.StartFeishuBinding(context.Background())
	if err != nil || waiting.State != BindingStateWaiting || !strings.HasPrefix(waiting.VerificationURL, "https://accounts.feishu.cn/") {
		t.Fatalf("waiting = %#v, err = %v", waiting, err)
	}
	if _, err := service.StartFeishuBinding(context.Background()); err != ErrBindingActive {
		t.Fatalf("duplicate start err = %v", err)
	}
	close(poll)
	waitBindingState(t, service, BindingStateSucceeded)
	select {
	case <-changed:
	case <-time.After(time.Second):
		t.Fatal("notification invalidation callback not called")
	}
	items, err := store.ListNotificationChannels(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatalf("items = %#v, err = %v", items, err)
	}
	item := items[0]
	if item.DeliveryMode != "feishu_app" || item.DisplayName != "飞书私聊" || !item.Enabled || item.LastDeliveryStatus != "success" || len(item.EventKinds) != 9 {
		t.Fatalf("item = %#v", item)
	}
	select {
	case <-attemptDone:
	case <-time.After(time.Second):
		t.Fatal("successful binding attempt context was not released")
	}
	if len(item.WebhookCiphertext) != 0 || len(item.FeishuAppIDCiphertext) == 0 || len(item.FeishuAppSecretCiphertext) == 0 || len(item.FeishuRecipientOpenIDCiphertext) == 0 {
		t.Fatalf("credential ownership = %#v", item)
	}
	if _, err := service.Update(context.Background(), item.ID, "feishu", item.DisplayName, "https://open.feishu.cn/open-apis/bot/v2/hook/replacement", "", true, item.EventKinds); !errors.Is(err, ErrChannelInvalid) {
		t.Fatalf("app credential replacement err = %v", err)
	}
	if _, err := service.Update(context.Background(), item.ID, "feishu", "飞书值班", "", "", true, item.EventKinds); err != nil {
		t.Fatalf("app settings update = %v", err)
	}
	if _, err := service.Test(context.Background(), item.ID); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 || !strings.HasPrefix(calls[0], "ou_synthetic|") {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestFeishuBindingCancelAndFailuresNeverPersist(t *testing.T) {
	tests := []struct {
		name         string
		pollError    error
		messageError error
		wantState    string
		wantCode     string
	}{
		{name: "denied", pollError: ErrFeishuAuthorizationDenied, wantState: BindingStateFailed, wantCode: BindingErrorDenied},
		{name: "expired", pollError: ErrFeishuAuthorizationExpired, wantState: BindingStateExpired, wantCode: BindingErrorExpired},
		{name: "lark", pollError: ErrFeishuLarkUnsupported, wantState: BindingStateFailed, wantCode: BindingErrorLarkUnsupported},
		{name: "invalid", pollError: ErrFeishuProviderResultInvalid, wantState: BindingStateFailed, wantCode: BindingErrorResultInvalid},
		{name: "test", messageError: ErrFeishuProviderUnavailable, wantState: BindingStateFailed, wantCode: BindingErrorTestFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &memoryStore{}
			service := newTestService(t, store)
			now := time.Now().UTC()
			service.configureFeishuBinding(context.Background(), registrarFake{
				begin: func(context.Context) (FeishuRegistration, error) {
					return FeishuRegistration{DeviceCode: "device", VerificationURL: "https://accounts.feishu.cn/synthetic", ExpiresAt: now.Add(time.Minute)}, nil
				},
				poll: func(context.Context, FeishuRegistration) (FeishuRegistrationResult, error) {
					return FeishuRegistrationResult{AppID: "cli_synthetic", AppSecret: "secret", OpenID: "ou_synthetic", TenantBrand: "feishu"}, test.pollError
				},
			}, messengerFunc(func(context.Context, FeishuRegistrationResult, string) error { return test.messageError }), nil)
			if _, err := service.StartFeishuBinding(context.Background()); err != nil {
				t.Fatal(err)
			}
			state := waitBindingState(t, service, test.wantState)
			if state.ErrorCode != test.wantCode || len(store.channels) != 0 || state.VerificationURL != "" {
				t.Fatalf("state = %#v, channels = %d", state, len(store.channels))
			}
		})
	}

	store := &memoryStore{}
	service := newTestService(t, store)
	blocked := make(chan struct{})
	messengerCalls := 0
	service.configureFeishuBinding(context.Background(), registrarFake{
		begin: func(context.Context) (FeishuRegistration, error) {
			return FeishuRegistration{DeviceCode: "device", VerificationURL: "https://accounts.feishu.cn/synthetic", ExpiresAt: time.Now().Add(time.Minute)}, nil
		},
		poll: func(ctx context.Context, _ FeishuRegistration) (FeishuRegistrationResult, error) {
			close(blocked)
			<-ctx.Done()
			return FeishuRegistrationResult{AppID: "cli_stale", AppSecret: "secret_stale", OpenID: "ou_stale", TenantBrand: "feishu"}, nil
		},
	}, messengerFunc(func(context.Context, FeishuRegistrationResult, string) error { messengerCalls++; return nil }), nil)
	if _, err := service.StartFeishuBinding(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-blocked
	state, err := service.CancelFeishuBinding()
	if err != nil || state.State != BindingStateCancelled || len(store.channels) != 0 {
		t.Fatalf("cancel = %#v, err = %v", state, err)
	}
	time.Sleep(10 * time.Millisecond)
	if messengerCalls != 0 || len(store.channels) != 0 {
		t.Fatalf("stale completion calls=%d channels=%d", messengerCalls, len(store.channels))
	}
}

func TestFeishuBindingPersistenceFailureDoesNotCreateChannel(t *testing.T) {
	memory := &memoryStore{}
	service := newTestService(t, failingUpsertStore{memory})
	now := time.Now().UTC()
	service.configureFeishuBinding(context.Background(), registrarFake{
		begin: func(context.Context) (FeishuRegistration, error) {
			return FeishuRegistration{DeviceCode: "device", VerificationURL: "https://accounts.feishu.cn/synthetic", ExpiresAt: now.Add(time.Minute)}, nil
		},
		poll: func(context.Context, FeishuRegistration) (FeishuRegistrationResult, error) {
			return FeishuRegistrationResult{AppID: "cli_synthetic", AppSecret: "secret", OpenID: "ou_synthetic", TenantBrand: "feishu"}, nil
		},
	}, messengerFunc(func(context.Context, FeishuRegistrationResult, string) error { return nil }), nil)
	if _, err := service.StartFeishuBinding(context.Background()); err != nil {
		t.Fatal(err)
	}
	state := waitBindingState(t, service, BindingStateFailed)
	if state.ErrorCode != BindingErrorPersistFailed || len(memory.channels) != 0 {
		t.Fatalf("state=%#v channels=%d", state, len(memory.channels))
	}
}

func TestFeishuBindingProcessCancellationStopsWaitingAndTestingWithoutPersistence(t *testing.T) {
	t.Run("waiting", func(t *testing.T) {
		store := &memoryStore{}
		service := newTestService(t, store)
		processCtx, cancelProcess := context.WithCancel(context.Background())
		pollStarted := make(chan struct{})
		service.configureFeishuBinding(processCtx, registrarFake{
			begin: func(context.Context) (FeishuRegistration, error) {
				return FeishuRegistration{DeviceCode: "device", VerificationURL: "https://accounts.feishu.cn/synthetic", ExpiresAt: time.Now().Add(time.Minute)}, nil
			},
			poll: func(ctx context.Context, _ FeishuRegistration) (FeishuRegistrationResult, error) {
				close(pollStarted)
				<-ctx.Done()
				return FeishuRegistrationResult{}, ctx.Err()
			},
		}, messengerFunc(func(context.Context, FeishuRegistrationResult, string) error {
			t.Fatal("messenger called after waiting attempt cancellation")
			return nil
		}), nil)
		if _, err := service.StartFeishuBinding(context.Background()); err != nil {
			t.Fatal(err)
		}
		<-pollStarted
		cancelProcess()
		state := waitBindingState(t, service, BindingStateFailed)
		if state.ErrorCode != BindingErrorProviderFailed || len(store.channels) != 0 {
			t.Fatalf("state = %#v, channels = %d", state, len(store.channels))
		}
		if restarted := newTestService(t, &memoryStore{}).FeishuBindingStatus(); restarted.State != BindingStateIdle {
			t.Fatalf("restarted state = %#v", restarted)
		}
	})

	t.Run("testing", func(t *testing.T) {
		store := &memoryStore{}
		service := newTestService(t, store)
		processCtx, cancelProcess := context.WithCancel(context.Background())
		testStarted := make(chan struct{})
		service.configureFeishuBinding(processCtx, registrarFake{
			begin: func(context.Context) (FeishuRegistration, error) {
				return FeishuRegistration{DeviceCode: "device", VerificationURL: "https://accounts.feishu.cn/synthetic", ExpiresAt: time.Now().Add(time.Minute)}, nil
			},
			poll: func(context.Context, FeishuRegistration) (FeishuRegistrationResult, error) {
				return FeishuRegistrationResult{AppID: "cli_synthetic", AppSecret: "secret", OpenID: "ou_synthetic", TenantBrand: "feishu"}, nil
			},
		}, messengerFunc(func(ctx context.Context, _ FeishuRegistrationResult, _ string) error {
			close(testStarted)
			<-ctx.Done()
			return ctx.Err()
		}), nil)
		if _, err := service.StartFeishuBinding(context.Background()); err != nil {
			t.Fatal(err)
		}
		<-testStarted
		if state, err := service.CancelFeishuBinding(); !errors.Is(err, ErrBindingNotCancelable) || state.State != BindingStateTesting {
			t.Fatalf("testing cancel = %#v, %v", state, err)
		}
		cancelProcess()
		state := waitBindingState(t, service, BindingStateFailed)
		if state.ErrorCode != BindingErrorTestFailed || len(store.channels) != 0 {
			t.Fatalf("state = %#v, channels = %d", state, len(store.channels))
		}
	})
}

func waitBindingState(t *testing.T, service *Service, wanted string) BindingView {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		state := service.FeishuBindingStatus()
		if state.State == wanted {
			return state
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("binding did not reach %s: %#v", wanted, service.FeishuBindingStatus())
	return BindingView{}
}
