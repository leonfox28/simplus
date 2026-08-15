package notificationwebhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	notification "github.com/leonfox28/simplus/internal/application/notification"
)

const (
	requestTimeout        = 15 * time.Second
	providerResponseLimit = 64 << 10
	wecomHostname         = "qyapi.weixin.qq.com"
	wecomPath             = "/cgi-bin/webhook/send"
	feishuHostname        = "open.feishu.cn"
	feishuPathPrefix      = "/open-apis/bot/v2/hook/"
)

var (
	ErrTargetInvalid    = errors.New("notification webhook target is invalid")
	ErrRequestInvalid   = errors.New("notification webhook delivery request is invalid")
	ErrNetworkFailed    = errors.New("notification webhook delivery network failed")
	ErrProviderRejected = errors.New("notification webhook delivery rejected")
)

type Client struct {
	client *http.Client
}

func NewClient() *Client {
	return &Client{client: &http.Client{
		Timeout: requestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return ErrNetworkFailed
		},
	}}
}

func (client *Client) ValidateTarget(provider notification.WebhookProvider, raw string) (notification.WebhookTarget, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > notification.WebhookURLByteLimit {
		return notification.WebhookTarget{}, ErrTargetInvalid
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" {
		return notification.WebhookTarget{}, ErrTargetInvalid
	}
	switch provider {
	case notification.WebhookProviderWeCom:
		if parsed.Hostname() != wecomHostname || parsed.Path != wecomPath || parsed.Query().Get("key") == "" {
			return notification.WebhookTarget{}, ErrTargetInvalid
		}
	case notification.WebhookProviderFeishu:
		if parsed.Hostname() != feishuHostname || !strings.HasPrefix(parsed.Path, feishuPathPrefix) ||
			strings.TrimPrefix(parsed.Path, feishuPathPrefix) == "" || parsed.RawQuery != "" {
			return notification.WebhookTarget{}, ErrTargetInvalid
		}
	default:
		return notification.WebhookTarget{}, ErrTargetInvalid
	}
	return notification.WebhookTarget{URL: parsed.String(), Hint: parsed.Hostname()}, nil
}

func (client *Client) Deliver(ctx context.Context, delivery notification.WebhookDeliveryRequest) (notification.WebhookDeliveryResult, error) {
	if client == nil || client.client == nil {
		return notification.WebhookDeliveryResult{}, ErrRequestInvalid
	}
	target, err := client.ValidateTarget(delivery.Provider, delivery.URL)
	if err != nil ||
		delivery.Message == "" || len([]rune(delivery.Message)) > notification.WebhookMessageRuneLimit ||
		len(delivery.SigningSecret) > notification.WebhookSigningSecretLimit ||
		(delivery.Provider == notification.WebhookProviderWeCom && delivery.SigningSecret != "") {
		return notification.WebhookDeliveryResult{}, ErrRequestInvalid
	}
	body, err := deliveryBody(delivery)
	if err != nil {
		return notification.WebhookDeliveryResult{}, ErrRequestInvalid
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.URL, bytes.NewReader(body))
	if err != nil {
		return notification.WebhookDeliveryResult{}, ErrRequestInvalid
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "Simplus")
	response, err := client.client.Do(request)
	if err != nil {
		return notification.WebhookDeliveryResult{Outcome: notification.WebhookNetworkFailed}, ErrNetworkFailed
	}
	defer response.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, providerResponseLimit+1))
	if readErr != nil || len(responseBody) > providerResponseLimit || response.StatusCode < 200 || response.StatusCode >= 300 ||
		!deliverySucceeded(delivery.Provider, responseBody) {
		return notification.WebhookDeliveryResult{Outcome: notification.WebhookRejected}, ErrProviderRejected
	}
	return notification.WebhookDeliveryResult{Outcome: notification.WebhookDelivered}, nil
}

func deliveryBody(delivery notification.WebhookDeliveryRequest) ([]byte, error) {
	switch delivery.Provider {
	case notification.WebhookProviderWeCom:
		return json.Marshal(map[string]any{"msgtype": "text", "text": map[string]string{"content": delivery.Message}})
	case notification.WebhookProviderFeishu:
		payload := map[string]any{"msg_type": "text", "content": map[string]string{"text": delivery.Message}}
		if delivery.SigningSecret != "" {
			timestamp := strconv.FormatInt(delivery.Timestamp, 10)
			mac := hmac.New(sha256.New, []byte(timestamp+"\n"+delivery.SigningSecret))
			payload["timestamp"] = timestamp
			payload["sign"] = base64.StdEncoding.EncodeToString(mac.Sum(nil))
		}
		return json.Marshal(payload)
	default:
		return nil, ErrRequestInvalid
	}
}

func deliverySucceeded(provider notification.WebhookProvider, body []byte) bool {
	var response struct {
		ErrCode *int `json:"errcode"`
		Code    *int `json:"code"`
	}
	if json.Unmarshal(body, &response) != nil {
		return false
	}
	if provider == notification.WebhookProviderWeCom {
		return response.ErrCode != nil && *response.ErrCode == 0
	}
	return provider == notification.WebhookProviderFeishu && response.Code != nil && *response.Code == 0
}

var _ notification.WebhookPort = (*Client)(nil)
