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

func TestFeishuBindingAcceptsCurrentOpaqueBeginResponse(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	deviceCode := strings.Repeat("synthetic +/%", 43)
	beginBody, err := json.Marshal(map[string]any{
		"device_code": deviceCode, "verification_uri_complete": "https://open.feishu.cn/verify?user_code=synthetic",
		"interval": 5, "expires_in": 3600,
	})
	if err != nil {
		t.Fatal(err)
	}
	client := NewFeishuClient()
	client.Now = func() time.Time { return now }
	waited := false
	client.Wait = func(ctx context.Context, _ time.Duration) error {
		if !waited {
			waited = true
			return nil
		}
		<-ctx.Done()
		return ctx.Err()
	}
	pollDeviceCode := make(chan string, 1)
	client.Client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != feishuRegistrationPath {
			return nil, errors.New("unexpected synthetic request path")
		}
		requestBody, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		form, err := url.ParseQuery(string(requestBody))
		if err != nil {
			return nil, err
		}
		if form.Get("action") == "poll" {
			pollDeviceCode <- form.Get("device_code")
			return &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":"authorization_pending"}`))}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(beginBody))}, nil
	})}

	service := New(&memoryStore{}, testCipher{})
	service.ConfigureFeishuBinding(context.Background(), client, messengerFunc(func(context.Context, FeishuRegistrationResult, string) error {
		t.Fatal("messenger called before authorization")
		return nil
	}), nil)
	waiting, err := service.StartFeishuBinding(context.Background())
	if err != nil || waiting.State != BindingStateWaiting {
		t.Fatalf("waiting = %#v, err = %v", waiting, err)
	}
	verification, err := url.Parse(waiting.VerificationURL)
	if err != nil || verification.Hostname() != "open.feishu.cn" || !waiting.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("waiting = %#v, verification parse err = %v", waiting, err)
	}
	pollCode := <-pollDeviceCode
	if pollCode != deviceCode || len(pollCode) <= 512 {
		t.Fatalf("opaque device code was not preserved: length = %d", len(pollCode))
	}
	if _, err := service.CancelFeishuBinding(); err != nil {
		t.Fatal(err)
	}
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
		status := http.StatusOK
		switch {
		case request.URL.Hostname() == "accounts.feishu.cn" && strings.Contains(string(body), "action=begin"):
			response = `{"device_code":"device_synthetic","verification_uri_complete":"https://accounts.feishu.cn/verify?user_code=synthetic","interval":1,"expire_in":60}`
		case request.URL.Hostname() == "accounts.feishu.cn":
			pollCount++
			if pollCount == 1 {
				status = http.StatusBadRequest
				response = `{"error":"authorization_pending"}`
			} else if pollCount == 2 {
				status = http.StatusBadRequest
				response = `{"error":"slow_down"}`
			} else {
				response = `{"client_id":"cli_synthetic","client_secret":"secret_synthetic","user_info":{"open_id":"ou_synthetic","tenant_brand":"feishu"}}`
			}
		case request.URL.Path == feishuTenantTokenPath:
			response = `{"code":0,"tenant_access_token":"token_synthetic"}`
		case request.URL.Path == feishuMessagePath:
			response = `{"code":0}`
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(response))}, nil
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
	if pollCount != 3 || len(requests) != 6 {
		t.Fatalf("requests = %d, polls = %d", len(requests), pollCount)
	}
	if !strings.Contains(string(bodies[5]), `"receive_id":"ou_synthetic"`) || requests[5].URL.Query().Get("receive_id_type") != "open_id" {
		t.Fatalf("message request = %s %s", requests[5].URL, bodies[5])
	}
}

func TestNormalizeFeishuRegistrationLifetime(t *testing.T) {
	limitSeconds := int(feishuRegistrationLifetimeLimit / time.Second)
	maxInt := int(^uint(0) >> 1)
	for _, test := range []struct {
		name            string
		currentSeconds  int
		legacySeconds   int
		intervalSeconds int
		want            time.Duration
		wantError       bool
	}{
		{name: "current", currentSeconds: 3600, intervalSeconds: 5, want: time.Hour},
		{name: "legacy", legacySeconds: 60, intervalSeconds: 5, want: time.Minute},
		{name: "matching", currentSeconds: 75, legacySeconds: 75, intervalSeconds: 5, want: 75 * time.Second},
		{name: "default", legacySeconds: -1, intervalSeconds: 5, want: 600 * time.Second},
		{name: "limit", currentSeconds: limitSeconds, intervalSeconds: 5, want: feishuRegistrationLifetimeLimit},
		{name: "over limit", currentSeconds: limitSeconds + 1, intervalSeconds: 5, wantError: true},
		{name: "conflict", currentSeconds: 60, legacySeconds: 61, intervalSeconds: 5, wantError: true},
		{name: "shorter than interval", currentSeconds: 4, intervalSeconds: 5, wantError: true},
		{name: "extreme integer", currentSeconds: maxInt, intervalSeconds: 5, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeFeishuRegistrationLifetime(test.currentSeconds, test.legacySeconds, test.intervalSeconds)
			if test.wantError {
				if !errors.Is(err, ErrFeishuProviderResultInvalid) {
					t.Fatalf("error = %v, want %v", err, ErrFeishuProviderResultInvalid)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("lifetime = %s, err = %v, want %s", got, err, test.want)
			}
		})
	}
}

func TestFeishuClientRejectsInvalidBeginResponses(t *testing.T) {
	oversizeDeviceCode, err := json.Marshal(map[string]any{
		"device_code": strings.Repeat("x", feishuDeviceCodeLimit+1), "verification_uri_complete": "https://open.feishu.cn/verify",
		"interval": 1, "expires_in": 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		status    int
		response  string
		wantError error
	}{
		{name: "conflicting expiry", status: http.StatusOK, response: `{"device_code":"device","verification_uri_complete":"https://open.feishu.cn/verify","interval":1,"expires_in":60,"expire_in":61}`, wantError: ErrFeishuProviderResultInvalid},
		{name: "empty device code", status: http.StatusOK, response: `{"device_code":"","verification_uri_complete":"https://open.feishu.cn/verify","interval":1,"expires_in":60}`, wantError: ErrFeishuProviderResultInvalid},
		{name: "oversize device code", status: http.StatusOK, response: string(oversizeDeviceCode), wantError: ErrFeishuProviderResultInvalid},
		{name: "empty verification URL", status: http.StatusOK, response: `{"device_code":"device","verification_uri_complete":"","interval":1,"expires_in":60}`, wantError: ErrFeishuProviderResultInvalid},
		{name: "non-https", status: http.StatusOK, response: `{"device_code":"device","verification_uri_complete":"http://open.feishu.cn/verify","interval":1,"expires_in":60}`, wantError: ErrFeishuProviderResultInvalid},
		{name: "untrusted host", status: http.StatusOK, response: `{"device_code":"device","verification_uri_complete":"https://example.invalid/verify","interval":1,"expires_in":60}`, wantError: ErrFeishuProviderResultInvalid},
		{name: "host suffix", status: http.StatusOK, response: `{"device_code":"device","verification_uri_complete":"https://open.feishu.cn.example.invalid/verify","interval":1,"expires_in":60}`, wantError: ErrFeishuProviderResultInvalid},
		{name: "userinfo", status: http.StatusOK, response: `{"device_code":"device","verification_uri_complete":"https://user@open.feishu.cn/verify","interval":1,"expires_in":60}`, wantError: ErrFeishuProviderResultInvalid},
		{name: "port", status: http.StatusOK, response: `{"device_code":"device","verification_uri_complete":"https://open.feishu.cn:443/verify","interval":1,"expires_in":60}`, wantError: ErrFeishuProviderResultInvalid},
		{name: "empty port", status: http.StatusOK, response: `{"device_code":"device","verification_uri_complete":"https://open.feishu.cn:/verify","interval":1,"expires_in":60}`, wantError: ErrFeishuProviderResultInvalid},
		{name: "fragment", status: http.StatusOK, response: `{"device_code":"device","verification_uri_complete":"https://open.feishu.cn/verify#fragment","interval":1,"expires_in":60}`, wantError: ErrFeishuProviderResultInvalid},
		{name: "oversize verification URL", status: http.StatusOK, response: `{"device_code":"device","verification_uri_complete":"https://open.feishu.cn/verify?value=` + strings.Repeat("x", 2048) + `","interval":1,"expires_in":60}`, wantError: ErrFeishuProviderResultInvalid},
		{name: "provider error", status: http.StatusBadRequest, response: `{"error":"invalid_request"}`, wantError: ErrFeishuProviderUnavailable},
		{name: "non-2xx without error", status: http.StatusInternalServerError, response: `{}`, wantError: ErrFeishuProviderResultInvalid},
		{name: "oversize response", status: http.StatusOK, response: strings.Repeat("x", providerResponseLimit+1), wantError: ErrFeishuProviderUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := NewFeishuClient()
			client.Client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: test.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(test.response))}, nil
			})}
			if _, err := client.Begin(context.Background()); !errors.Is(err, test.wantError) {
				t.Fatalf("Begin error = %v, want %v", err, test.wantError)
			}
		})
	}

	client := NewFeishuClient()
	client.Client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("synthetic network failure")
	})}
	if _, err := client.Begin(context.Background()); !errors.Is(err, ErrFeishuProviderUnavailable) {
		t.Fatalf("network error = %v", err)
	}
}

func TestFeishuClientMapsRegistrationErrorsFromHTTP400(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	for _, test := range []struct {
		name      string
		response  string
		wantError error
	}{
		{name: "denied", response: `{"error":"access_denied"}`, wantError: ErrFeishuAuthorizationDenied},
		{name: "expired", response: `{"error":"expired_token"}`, wantError: ErrFeishuAuthorizationExpired},
		{name: "unknown", response: `{"error":"synthetic_unknown"}`, wantError: ErrFeishuProviderUnavailable},
		{name: "missing error", response: `{}`, wantError: ErrFeishuProviderUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := NewFeishuClient()
			client.Now = func() time.Time { return now }
			client.Wait = func(context.Context, time.Duration) error { return nil }
			client.Client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(test.response))}, nil
			})}
			_, err := client.Poll(context.Background(), FeishuRegistration{
				DeviceCode: "device_synthetic", ExpiresAt: now.Add(time.Minute), PollInterval: time.Second,
			})
			if !errors.Is(err, test.wantError) {
				t.Fatalf("Poll error = %v, want %v", err, test.wantError)
			}
		})
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
		tokenStatus   int
		messageBody   string
		messageStatus int
	}{
		{name: "token missing code", tokenResponse: `{"tenant_access_token":"token_synthetic"}`, tokenStatus: http.StatusOK, messageBody: `{"code":0}`, messageStatus: http.StatusOK},
		{name: "token nonzero code", tokenResponse: `{"code":1,"tenant_access_token":"token_synthetic"}`, tokenStatus: http.StatusOK, messageBody: `{"code":0}`, messageStatus: http.StatusOK},
		{name: "token non-2xx", tokenResponse: `{"code":0,"tenant_access_token":"token_synthetic"}`, tokenStatus: http.StatusBadRequest, messageBody: `{"code":0}`, messageStatus: http.StatusOK},
		{name: "message missing code", tokenResponse: `{"code":0,"tenant_access_token":"token_synthetic"}`, tokenStatus: http.StatusOK, messageBody: `{}`, messageStatus: http.StatusOK},
		{name: "message nonzero code", tokenResponse: `{"code":0,"tenant_access_token":"token_synthetic"}`, tokenStatus: http.StatusOK, messageBody: `{"code":1}`, messageStatus: http.StatusOK},
		{name: "message non-2xx", tokenResponse: `{"code":0,"tenant_access_token":"token_synthetic"}`, tokenStatus: http.StatusOK, messageBody: `{"code":0}`, messageStatus: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := NewFeishuClient()
			client.Client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				body := test.messageBody
				status := test.messageStatus
				if request.URL.Path == feishuTenantTokenPath {
					body = test.tokenResponse
					status = test.tokenStatus
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			if err := client.SendText(context.Background(), credentials, "synthetic message"); !errors.Is(err, ErrFeishuProviderUnavailable) {
				t.Fatalf("SendText error = %v", err)
			}
		})
	}
}
