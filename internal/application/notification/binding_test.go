package notification

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
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

type failingUpsertStore struct{ *memoryStore }

func (store failingUpsertStore) UpsertNotificationChannel(context.Context, domain.Channel) error {
	return errors.New("synthetic persistence failure")
}

func TestFeishuBindingTestsBeforePersistingAndSupportsAppDelivery(t *testing.T) {
	store := &memoryStore{}
	service := New(store, testCipher{})
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
	service.ConfigureFeishuBinding(context.Background(), registrar, messenger, func() { changed <- struct{}{} })
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
	if item.DeliveryMode != "feishu_app" || item.DisplayName != "飞书私聊" || !item.Enabled || item.LastDeliveryStatus != "success" || len(item.EventKinds) != 5 {
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
			service := New(store, testCipher{})
			now := time.Now().UTC()
			service.ConfigureFeishuBinding(context.Background(), registrarFake{
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
	service := New(store, testCipher{})
	blocked := make(chan struct{})
	messengerCalls := 0
	service.ConfigureFeishuBinding(context.Background(), registrarFake{
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
	service := New(failingUpsertStore{memory}, testCipher{})
	now := time.Now().UTC()
	service.ConfigureFeishuBinding(context.Background(), registrarFake{
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
		service := New(store, testCipher{})
		processCtx, cancelProcess := context.WithCancel(context.Background())
		pollStarted := make(chan struct{})
		service.ConfigureFeishuBinding(processCtx, registrarFake{
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
		if restarted := New(&memoryStore{}, testCipher{}).FeishuBindingStatus(); restarted.State != BindingStateIdle {
			t.Fatalf("restarted state = %#v", restarted)
		}
	})

	t.Run("testing", func(t *testing.T) {
		store := &memoryStore{}
		service := New(store, testCipher{})
		processCtx, cancelProcess := context.WithCancel(context.Background())
		testStarted := make(chan struct{})
		service.ConfigureFeishuBinding(processCtx, registrarFake{
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

func TestFeishuClientUsesMinimalCreateOnlyFlowAndPrivateOpenIDDelivery(t *testing.T) {
	var requests []*http.Request
	var bodies [][]byte
	pollCount := 0
	client := NewFeishuClient()
	client.Now = func() time.Time { return time.Unix(1000, 0).UTC() }
	client.Wait = func(context.Context, time.Duration) error { return nil }
	client.Client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(request.Body)
		requests = append(requests, request.Clone(context.Background()))
		bodies = append(bodies, body)
		response := `{}`
		switch {
		case request.URL.Hostname() == "accounts.feishu.cn" && strings.Contains(string(body), "action=begin"):
			response = `{"device_code":"device_synthetic","verification_uri_complete":"https://accounts.feishu.cn/verify?user_code=synthetic","interval":1,"expire_in":60}`
		case request.URL.Hostname() == "accounts.feishu.cn":
			pollCount++
			if pollCount == 1 {
				response = `{"error":"slow_down"}`
			} else {
				response = `{"client_id":"cli_synthetic","client_secret":"secret_synthetic","user_info":{"open_id":"ou_synthetic","tenant_brand":"feishu"}}`
			}
		case request.URL.Path == feishuTenantTokenPath:
			response = `{"code":0,"tenant_access_token":"token_synthetic"}`
		case request.URL.Path == feishuMessagePath:
			response = `{"code":0}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(response))}, nil
	})}
	registration, err := client.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	verification, _ := url.Parse(registration.VerificationURL)
	if verification.Hostname() != "accounts.feishu.cn" || verification.Query().Get("createOnly") != "true" {
		t.Fatalf("verification URL = %s", registration.VerificationURL)
	}
	compressed, err := base64.RawURLEncoding.DecodeString(verification.Query().Get("addons"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	addonsBody, _ := io.ReadAll(reader)
	var addons map[string]json.RawMessage
	if err := json.Unmarshal(addonsBody, &addons); err != nil {
		t.Fatal(err)
	}
	var preset bool
	var scopes map[string][]string
	if len(addons) != 2 || json.Unmarshal(addons["preset"], &preset) != nil || json.Unmarshal(addons["scopes"], &scopes) != nil || preset || len(scopes) != 1 || len(scopes["tenant"]) != 1 || scopes["tenant"][0] != "im:message:send_as_bot" {
		t.Fatalf("addons = %s", addonsBody)
	}
	beginForm, err := url.ParseQuery(string(bodies[0]))
	if err != nil {
		t.Fatal(err)
	}
	if len(beginForm) != 4 || beginForm.Get("action") != "begin" || beginForm.Get("archetype") != "PersonalAgent" || beginForm.Get("auth_method") != "client_secret" || beginForm.Get("request_user_info") != "open_id" {
		t.Fatalf("begin form = %#v", beginForm)
	}
	result, err := client.Poll(context.Background(), registration)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SendText(context.Background(), result, "synthetic binding test"); err != nil {
		t.Fatal(err)
	}
	if pollCount != 2 || len(requests) != 5 {
		t.Fatalf("requests = %d, polls = %d", len(requests), pollCount)
	}
	if !strings.Contains(string(bodies[4]), `"receive_id":"ou_synthetic"`) || requests[4].URL.Query().Get("receive_id_type") != "open_id" {
		t.Fatalf("message request = %s %s", requests[4].URL, bodies[4])
	}
}

func TestFeishuClientRejectsUntrustedVerificationURLAndOversizeResponse(t *testing.T) {
	for _, response := range []string{
		`{"device_code":"device","verification_uri_complete":"https://example.invalid/verify","interval":1,"expire_in":60}`,
		strings.Repeat("x", providerResponseLimit+1),
	} {
		client := NewFeishuClient()
		client.Client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(response))}, nil
		})}
		if _, err := client.Begin(context.Background()); err == nil {
			t.Fatalf("response accepted: %.40s", response)
		}
	}
	client := NewFeishuClient()
	client.Client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("synthetic network failure")
	})}
	if _, err := client.Begin(context.Background()); !errors.Is(err, ErrFeishuProviderUnavailable) {
		t.Fatalf("network error = %v", err)
	}
}

func TestFeishuClientDoesNotPollAfterRegistrationExpiry(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	requests := 0
	client := NewFeishuClient()
	client.Now = func() time.Time { return now }
	client.Wait = func(_ context.Context, duration time.Duration) error {
		if duration != time.Second {
			t.Fatalf("wait duration = %s", duration)
		}
		now = now.Add(duration)
		return nil
	}
	client.Client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":"authorization_pending"}`))}, nil
	})}
	_, err := client.Poll(context.Background(), FeishuRegistration{
		DeviceCode: "device_synthetic", ExpiresAt: now.Add(time.Second), PollInterval: 5 * time.Second,
	})
	if !errors.Is(err, ErrFeishuAuthorizationExpired) || requests != 0 {
		t.Fatalf("poll err = %v, requests = %d", err, requests)
	}
}

func TestFeishuClientRequiresExplicitProviderSuccessCodes(t *testing.T) {
	credentials := FeishuRegistrationResult{AppID: "cli_synthetic", AppSecret: "secret_synthetic", OpenID: "ou_synthetic", TenantBrand: "feishu"}
	for _, test := range []struct {
		name          string
		tokenResponse string
		messageBody   string
	}{
		{name: "token", tokenResponse: `{"tenant_access_token":"token_synthetic"}`, messageBody: `{"code":0}`},
		{name: "message", tokenResponse: `{"code":0,"tenant_access_token":"token_synthetic"}`, messageBody: `{}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := NewFeishuClient()
			client.Client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				body := test.messageBody
				if request.URL.Path == feishuTenantTokenPath {
					body = test.tokenResponse
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			if err := client.SendText(context.Background(), credentials, "synthetic message"); !errors.Is(err, ErrFeishuProviderUnavailable) {
				t.Fatalf("SendText error = %v", err)
			}
		})
	}
}
