package notificationwebhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	notification "github.com/leonfox28/simplus/internal/application/notification"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestNewClientUsesBoundedRedirectRefusingHTTP(t *testing.T) {
	client := NewClient()
	if client.client.Timeout != requestTimeout || client.client.CheckRedirect == nil {
		t.Fatalf("HTTP client = %#v", client.client)
	}
	if err := client.client.CheckRedirect(nil, nil); err != ErrNetworkFailed {
		t.Fatalf("redirect error = %v", err)
	}
}

func TestValidateTargetPreservesLegacyCompatibleShapes(t *testing.T) {
	client := NewClient()
	tests := []struct {
		name     string
		provider notification.WebhookProvider
		raw      string
		wantURL  string
		wantHint string
	}{
		{
			name: "wecom extra query and whitespace", provider: notification.WebhookProviderWeCom,
			raw:     "  https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic&trace=kept  ",
			wantURL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic&trace=kept", wantHint: "qyapi.weixin.qq.com",
		},
		{
			name: "wecom explicit port", provider: notification.WebhookProviderWeCom,
			raw:     "https://qyapi.weixin.qq.com:8443/cgi-bin/webhook/send?key=synthetic",
			wantURL: "https://qyapi.weixin.qq.com:8443/cgi-bin/webhook/send?key=synthetic", wantHint: "qyapi.weixin.qq.com",
		},
		{
			name: "feishu", provider: notification.WebhookProviderFeishu,
			raw:     "https://open.feishu.cn/open-apis/bot/v2/hook/synthetic-hook",
			wantURL: "https://open.feishu.cn/open-apis/bot/v2/hook/synthetic-hook", wantHint: "open.feishu.cn",
		},
		{
			name: "feishu explicit port", provider: notification.WebhookProviderFeishu,
			raw:     "https://open.feishu.cn:443/open-apis/bot/v2/hook/synthetic-hook",
			wantURL: "https://open.feishu.cn:443/open-apis/bot/v2/hook/synthetic-hook", wantHint: "open.feishu.cn",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target, err := client.ValidateTarget(test.provider, test.raw)
			if err != nil || target.URL != test.wantURL || target.Hint != test.wantHint {
				t.Fatalf("target = %#v, error = %v", target, err)
			}
		})
	}
}

func TestValidateTargetRejectsNonOfficialAndMalformedShapes(t *testing.T) {
	client := NewClient()
	tests := []struct {
		name     string
		provider notification.WebhookProvider
		raw      string
	}{
		{name: "empty", provider: notification.WebhookProviderWeCom},
		{name: "oversize", provider: notification.WebhookProviderWeCom, raw: strings.Repeat("x", notification.WebhookURLByteLimit+1)},
		{name: "unknown provider", provider: "future", raw: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic"},
		{name: "http", provider: notification.WebhookProviderWeCom, raw: "http://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic"},
		{name: "userinfo", provider: notification.WebhookProviderWeCom, raw: "https://user@qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic"},
		{name: "fragment", provider: notification.WebhookProviderWeCom, raw: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic#fragment"},
		{name: "wecom arbitrary host", provider: notification.WebhookProviderWeCom, raw: "https://example.invalid/cgi-bin/webhook/send?key=synthetic"},
		{name: "wecom host suffix", provider: notification.WebhookProviderWeCom, raw: "https://qyapi.weixin.qq.com.example.invalid/cgi-bin/webhook/send?key=synthetic"},
		{name: "wecom path", provider: notification.WebhookProviderWeCom, raw: "https://qyapi.weixin.qq.com/cgi-bin/webhook/other?key=synthetic"},
		{name: "wecom empty key", provider: notification.WebhookProviderWeCom, raw: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key="},
		{name: "feishu arbitrary host", provider: notification.WebhookProviderFeishu, raw: "https://example.invalid/open-apis/bot/v2/hook/synthetic"},
		{name: "feishu empty suffix", provider: notification.WebhookProviderFeishu, raw: "https://open.feishu.cn/open-apis/bot/v2/hook/"},
		{name: "feishu query", provider: notification.WebhookProviderFeishu, raw: "https://open.feishu.cn/open-apis/bot/v2/hook/synthetic?value=not-allowed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target, err := client.ValidateTarget(test.provider, test.raw)
			if target != (notification.WebhookTarget{}) || err != ErrTargetInvalid {
				t.Fatalf("target = %#v, error = %v", target, err)
			}
		})
	}
}

func TestDeliverBuildsProviderPayloadsAndHeaders(t *testing.T) {
	tests := []struct {
		name         string
		delivery     notification.WebhookDeliveryRequest
		response     string
		wantPayload  map[string]any
		wantEndpoint string
	}{
		{
			name: "wecom",
			delivery: notification.WebhookDeliveryRequest{
				Provider: notification.WebhookProviderWeCom,
				URL:      "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic&trace=kept",
				Message:  "synthetic message",
			},
			response: `{"errcode":0}`,
			wantPayload: map[string]any{
				"msgtype": "text", "text": map[string]any{"content": "synthetic message"},
			},
			wantEndpoint: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic&trace=kept",
		},
		{
			name: "feishu signed",
			delivery: notification.WebhookDeliveryRequest{
				Provider: notification.WebhookProviderFeishu,
				URL:      "https://open.feishu.cn/open-apis/bot/v2/hook/synthetic",
				Message:  "synthetic message", SigningSecret: "synthetic-signing", Timestamp: 1700000000,
			},
			response: `{"code":0}`,
			wantPayload: map[string]any{
				"msg_type": "text", "content": map[string]any{"text": "synthetic message"},
				"timestamp": "1700000000", "sign": "uwBtz8LlQii9Rm8FHcsEyLYJzomjyWk/djfg7vbN//w=",
			},
			wantEndpoint: "https://open.feishu.cn/open-apis/bot/v2/hook/synthetic",
		},
		{
			name: "feishu unsigned",
			delivery: notification.WebhookDeliveryRequest{
				Provider: notification.WebhookProviderFeishu,
				URL:      "https://open.feishu.cn/open-apis/bot/v2/hook/synthetic",
				Message:  "synthetic message",
			},
			response: `{"code":0}`,
			wantPayload: map[string]any{
				"msg_type": "text", "content": map[string]any{"text": "synthetic message"},
			},
			wantEndpoint: "https://open.feishu.cn/open-apis/bot/v2/hook/synthetic",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			client := NewClient()
			client.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				requests++
				if request.Method != http.MethodPost || request.URL.String() != test.wantEndpoint || request.Header.Get("Content-Type") != "application/json" || request.Header.Get("User-Agent") != "Simplus" {
					t.Fatalf("request = %s %s, headers = %#v", request.Method, request.URL, request.Header)
				}
				body, err := io.ReadAll(request.Body)
				if err != nil {
					t.Fatal(err)
				}
				var payload map[string]any
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatal(err)
				}
				want, _ := json.Marshal(test.wantPayload)
				got, _ := json.Marshal(payload)
				if !bytes.Equal(got, want) {
					t.Fatalf("payload = %s, want %s", got, want)
				}
				return response(http.StatusOK, test.response), nil
			})
			result, err := client.Deliver(context.Background(), test.delivery)
			if err != nil || result.Outcome != notification.WebhookDelivered || requests != 1 {
				t.Fatalf("result = %#v, error = %v, requests = %d", result, err, requests)
			}
		})
	}
}

func TestDeliverUsesRevalidatedNormalizedTarget(t *testing.T) {
	const normalizedURL = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic&trace=kept"
	client := NewClient()
	client.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != normalizedURL {
			t.Fatalf("request URL = %q", request.URL.String())
		}
		return response(http.StatusOK, `{"errcode":0}`), nil
	})
	result, err := client.Deliver(context.Background(), notification.WebhookDeliveryRequest{
		Provider: notification.WebhookProviderWeCom,
		URL:      "  " + normalizedURL + "  ",
		Message:  "synthetic",
	})
	if err != nil || result.Outcome != notification.WebhookDelivered {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestDeliverRevalidatesEveryRequestBeforeTransport(t *testing.T) {
	tests := []struct {
		name     string
		delivery notification.WebhookDeliveryRequest
	}{
		{name: "arbitrary target", delivery: notification.WebhookDeliveryRequest{Provider: notification.WebhookProviderWeCom, URL: "https://example.invalid/hook", Message: "synthetic"}},
		{name: "unknown provider", delivery: notification.WebhookDeliveryRequest{Provider: "future", URL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic", Message: "synthetic"}},
		{name: "empty message", delivery: notification.WebhookDeliveryRequest{Provider: notification.WebhookProviderWeCom, URL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic"}},
		{name: "oversize message", delivery: notification.WebhookDeliveryRequest{Provider: notification.WebhookProviderWeCom, URL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic", Message: strings.Repeat("界", notification.WebhookMessageRuneLimit+1)}},
		{name: "wecom signing", delivery: notification.WebhookDeliveryRequest{Provider: notification.WebhookProviderWeCom, URL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic", Message: "synthetic", SigningSecret: "not-supported"}},
		{name: "oversize signing", delivery: notification.WebhookDeliveryRequest{Provider: notification.WebhookProviderFeishu, URL: "https://open.feishu.cn/open-apis/bot/v2/hook/synthetic", Message: "synthetic", SigningSecret: strings.Repeat("x", notification.WebhookSigningSecretLimit+1)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			client := NewClient()
			client.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				requests++
				return response(http.StatusOK, `{"errcode":0}`), nil
			})
			result, err := client.Deliver(context.Background(), test.delivery)
			if result != (notification.WebhookDeliveryResult{}) || err != ErrRequestInvalid || requests != 0 {
				t.Fatalf("result = %#v, error = %v, requests = %d", result, err, requests)
			}
		})
	}
}

func TestDeliverRejectsNilContextBeforeTransport(t *testing.T) {
	requests := 0
	client := NewClient()
	client.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return response(http.StatusOK, `{"errcode":0}`), nil
	})
	result, err := client.Deliver(nil, notification.WebhookDeliveryRequest{
		Provider: notification.WebhookProviderWeCom,
		URL:      "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic",
		Message:  "synthetic",
	})
	if result != (notification.WebhookDeliveryResult{}) || err != ErrRequestInvalid || requests != 0 {
		t.Fatalf("result = %#v, error = %v, requests = %d", result, err, requests)
	}
}

func TestDeliverAcceptsExactRequestBounds(t *testing.T) {
	prefix := "https://open.feishu.cn/open-apis/bot/v2/hook/"
	delivery := notification.WebhookDeliveryRequest{
		Provider:      notification.WebhookProviderFeishu,
		URL:           prefix + strings.Repeat("x", notification.WebhookURLByteLimit-len(prefix)),
		Message:       strings.Repeat("界", notification.WebhookMessageRuneLimit),
		SigningSecret: strings.Repeat("s", notification.WebhookSigningSecretLimit),
		Timestamp:     1700000000,
	}
	client := NewClient()
	client.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"code":0}`), nil
	})
	result, err := client.Deliver(context.Background(), delivery)
	if err != nil || result.Outcome != notification.WebhookDelivered {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestDeliverClassifiesRedirectAndTransportFailuresWithoutCredentials(t *testing.T) {
	credentialURL := "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=credential-marker"
	secret := "signing-secret-marker"
	providerBody := "provider-body-marker"
	tests := []struct {
		name      string
		transport http.RoundTripper
	}{
		{
			name: "transport",
			transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New(credentialURL + " " + secret + " " + providerBody)
			}),
		},
		{
			name: "redirect",
			transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				redirect := response(http.StatusFound, providerBody)
				redirect.Header.Set("Location", "https://redirect.invalid/path?key=redirect-marker")
				return redirect, nil
			}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := NewClient()
			client.client.Transport = test.transport
			result, err := client.Deliver(context.Background(), notification.WebhookDeliveryRequest{
				Provider: notification.WebhookProviderWeCom, URL: credentialURL, Message: "synthetic",
			})
			if result.Outcome != notification.WebhookNetworkFailed || err != ErrNetworkFailed {
				t.Fatalf("result = %#v, error = %v", result, err)
			}
			for _, marker := range []string{"credential-marker", secret, providerBody, "redirect.invalid", "redirect-marker"} {
				if strings.Contains(err.Error(), marker) {
					t.Fatalf("error exposed private marker %q: %v", marker, err)
				}
			}
		})
	}
}

func TestDeliverRejectsBoundedProviderFailures(t *testing.T) {
	exactLimit := append([]byte(`{"errcode":0}`), bytes.Repeat([]byte(" "), providerResponseLimit-len(`{"errcode":0}`))...)
	tests := []struct {
		name       string
		provider   notification.WebhookProvider
		url        string
		status     int
		body       []byte
		bodyReader io.Reader
		wantOK     bool
	}{
		{name: "exact response limit", status: http.StatusOK, body: exactLimit, wantOK: true},
		{name: "response over limit", status: http.StatusOK, body: append(exactLimit, ' ')},
		{name: "read failure", status: http.StatusOK, bodyReader: failingReader{}},
		{name: "non 2xx", status: http.StatusBadGateway, body: []byte(`{"errcode":0}`)},
		{name: "malformed", status: http.StatusOK, body: []byte(`{`)},
		{name: "missing code", status: http.StatusOK, body: []byte(`{}`)},
		{name: "nonzero code", status: http.StatusOK, body: []byte(`{"errcode":1}`)},
		{name: "wrong provider field", status: http.StatusOK, body: []byte(`{"code":0}`)},
		{name: "string code", status: http.StatusOK, body: []byte(`{"errcode":"0"}`)},
		{name: "feishu nonzero code", provider: notification.WebhookProviderFeishu, url: "https://open.feishu.cn/open-apis/bot/v2/hook/synthetic", status: http.StatusOK, body: []byte(`{"code":1}`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := test.provider
			if provider == "" {
				provider = notification.WebhookProviderWeCom
			}
			targetURL := test.url
			if targetURL == "" {
				targetURL = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic"
			}
			client := NewClient()
			client.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				reader := test.bodyReader
				if reader == nil {
					reader = bytes.NewReader(test.body)
				}
				return &http.Response{StatusCode: test.status, Header: http.Header{}, Body: io.NopCloser(reader)}, nil
			})
			result, err := client.Deliver(context.Background(), notification.WebhookDeliveryRequest{
				Provider: provider,
				URL:      targetURL,
				Message:  "synthetic",
			})
			if test.wantOK {
				if err != nil || result.Outcome != notification.WebhookDelivered {
					t.Fatalf("result = %#v, error = %v", result, err)
				}
				return
			}
			if result.Outcome != notification.WebhookRejected || err != ErrProviderRejected {
				t.Fatalf("result = %#v, error = %v", result, err)
			}
		})
	}
}

func TestProviderRejectionErrorDoesNotExposeResponseBody(t *testing.T) {
	const bodyMarker = "provider-private-body-marker"
	client := NewClient()
	client.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusBadGateway, bodyMarker), nil
	})
	result, err := client.Deliver(context.Background(), notification.WebhookDeliveryRequest{
		Provider: notification.WebhookProviderWeCom,
		URL:      "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic",
		Message:  "synthetic",
	})
	if result.Outcome != notification.WebhookRejected || err != ErrProviderRejected {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	if strings.Contains(err.Error(), bodyMarker) {
		t.Fatalf("error exposed provider body: %v", err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("synthetic provider-body read failure")
}

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}
