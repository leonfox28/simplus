package feishu

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
	"testing"
	"time"
)

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

	waiting, err := client.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	pollCtx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := client.Poll(pollCtx, waiting); done <- err }()
	verification, err := url.Parse(waiting.VerificationURL)
	if err != nil || verification.Hostname() != "open.feishu.cn" || !waiting.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("waiting = %#v, verification parse err = %v", waiting, err)
	}
	pollCode := <-pollDeviceCode
	if pollCode != deviceCode || len(pollCode) <= 512 {
		t.Fatalf("opaque device code was not preserved: length = %d", len(pollCode))
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
