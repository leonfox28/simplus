package notification

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	domain "github.com/leonfox28/simplus/internal/domain/notification"
)

var channelIDPattern = regexp.MustCompile(`^channel_[A-Za-z0-9_-]{22}$`)
var ErrChannelInvalid = errors.New("notification channel request is invalid")
var ErrChannelNotFound = errors.New("notification channel not found")
var allowedEvents = map[string]struct{}{"sms.received": {}, "sms.failed": {}, "call.incoming": {}, "call.missed": {}, "system.degraded": {}}

type Store interface {
	ListNotificationChannels(context.Context) ([]domain.Channel, error)
	ReadNotificationChannel(context.Context, string) (domain.Channel, bool, error)
	UpsertNotificationChannel(context.Context, domain.Channel) error
	DeleteNotificationChannel(context.Context, string) (bool, error)
	RecordNotificationDelivery(context.Context, string, string, string, time.Time) error
}
type SecretCipher interface {
	Encrypt(string, []byte) ([]byte, error)
	Decrypt(string, []byte) ([]byte, error)
}
type ChannelView struct {
	ID, Provider, DisplayName, DeliveryMode, TargetType string
	WebhookHint, LastDeliveryStatus, LastErrorCode      string
	Enabled, SigningSecretConfigured                    bool
	EventKinds                                          []string
	LastDeliveryAt                                      time.Time
}
type Service struct {
	Store           Store
	Secrets         SecretCipher
	Client          *http.Client
	Now             func() time.Time
	FeishuRegistrar FeishuRegistrar
	FeishuMessenger FeishuMessenger
	binding         *bindingController
}

func New(store Store, secrets SecretCipher) *Service {
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("notification webhook redirects are not allowed")
	}}
	return &Service{Store: store, Secrets: secrets, Client: client, Now: time.Now, binding: newBindingController()}
}
func (s *Service) List(ctx context.Context) ([]ChannelView, error) {
	items, err := s.Store.ListNotificationChannels(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]ChannelView, 0, len(items))
	for _, item := range items {
		result = append(result, view(item))
	}
	return result, nil
}
func (s *Service) Create(ctx context.Context, provider, name, webhook, signingSecret string, enabled bool, events []string) (ChannelView, error) {
	provider, name, parsed, events, err := validateInput(provider, name, webhook, events)
	if err != nil {
		return ChannelView{}, err
	}
	id, err := newChannelID()
	if err != nil {
		return ChannelView{}, err
	}
	webhookCipher, err := s.Secrets.Encrypt(secretLabel(id, "webhook"), []byte(parsed.String()))
	if err != nil {
		return ChannelView{}, err
	}
	var signingCipher []byte
	if signingSecret != "" {
		if provider != "feishu" || len(signingSecret) > 512 {
			return ChannelView{}, ErrChannelInvalid
		}
		signingCipher, err = s.Secrets.Encrypt(secretLabel(id, "signing"), []byte(signingSecret))
		if err != nil {
			return ChannelView{}, err
		}
	}
	now := s.Now().UTC()
	item := domain.Channel{ID: id, Provider: provider, DeliveryMode: domain.DeliveryModeWebhook, DisplayName: name, WebhookCiphertext: webhookCipher, WebhookHint: parsed.Hostname(), SigningSecretCiphertext: signingCipher, Enabled: enabled, EventKinds: events, LastDeliveryStatus: "never", CreatedAt: now, UpdatedAt: now}
	if err := s.Store.UpsertNotificationChannel(ctx, item); err != nil {
		return ChannelView{}, err
	}
	return view(item), nil
}
func (s *Service) Update(ctx context.Context, id, provider, name, webhook, signingSecret string, enabled bool, events []string) (ChannelView, error) {
	if !channelIDPattern.MatchString(id) {
		return ChannelView{}, ErrChannelInvalid
	}
	item, found, err := s.Store.ReadNotificationChannel(ctx, id)
	if err != nil {
		return ChannelView{}, err
	}
	if !found {
		return ChannelView{}, ErrChannelNotFound
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	name, events, err = validateSettings(name, events)
	if err != nil || provider != item.Provider {
		return ChannelView{}, ErrChannelInvalid
	}
	item.DisplayName, item.Enabled, item.EventKinds, item.UpdatedAt = name, enabled, events, s.Now().UTC()
	switch item.DeliveryMode {
	case "", domain.DeliveryModeWebhook:
		parsed, parseErr := validateWebhook(provider, chooseWebhook(webhook, item.WebhookHint))
		if parseErr != nil && webhook != "" {
			return ChannelView{}, parseErr
		}
		if webhook != "" {
			item.WebhookCiphertext, err = s.Secrets.Encrypt(secretLabel(id, "webhook"), []byte(parsed.String()))
			if err != nil {
				return ChannelView{}, err
			}
			item.WebhookHint = parsed.Hostname()
		}
		if signingSecret != "" {
			if provider != "feishu" || len(signingSecret) > 512 {
				return ChannelView{}, ErrChannelInvalid
			}
			item.SigningSecretCiphertext, err = s.Secrets.Encrypt(secretLabel(id, "signing"), []byte(signingSecret))
			if err != nil {
				return ChannelView{}, err
			}
		}
	case domain.DeliveryModeFeishuApp:
		if webhook != "" || signingSecret != "" {
			return ChannelView{}, ErrChannelInvalid
		}
	default:
		return ChannelView{}, ErrChannelInvalid
	}
	if err := s.Store.UpsertNotificationChannel(ctx, item); err != nil {
		return ChannelView{}, err
	}
	return view(item), nil
}
func chooseWebhook(raw, hint string) string {
	if raw != "" {
		return raw
	}
	return "https://" + hint + placeholderPath(hint)
}
func placeholderPath(host string) string {
	if host == "qyapi.weixin.qq.com" {
		return "/cgi-bin/webhook/send?key=placeholder"
	}
	return "/open-apis/bot/v2/hook/placeholder"
}
func (s *Service) Delete(ctx context.Context, id string) error {
	if !channelIDPattern.MatchString(id) {
		return ErrChannelInvalid
	}
	deleted, err := s.Store.DeleteNotificationChannel(ctx, id)
	if err != nil {
		return err
	}
	if !deleted {
		return ErrChannelNotFound
	}
	return nil
}
func (s *Service) Test(ctx context.Context, id string) (ChannelView, error) {
	return s.deliverOne(ctx, id, "Simplus 通知渠道测试成功")
}
func (s *Service) Notify(ctx context.Context, event, message string) error {
	if _, ok := allowedEvents[event]; !ok || message == "" || len([]rune(message)) > 4000 {
		return ErrChannelInvalid
	}
	items, err := s.Store.ListNotificationChannels(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, item := range items {
		if !item.Enabled || !contains(item.EventKinds, event) {
			continue
		}
		if _, err := s.deliver(ctx, item, message); err != nil {
			failures = append(failures, fmt.Errorf("channel %s: %w", item.ID, err))
		}
	}
	return errors.Join(failures...)
}
func (s *Service) deliverOne(ctx context.Context, id, message string) (ChannelView, error) {
	item, found, err := s.Store.ReadNotificationChannel(ctx, id)
	if err != nil {
		return ChannelView{}, err
	}
	if !found {
		return ChannelView{}, ErrChannelNotFound
	}
	return s.deliver(ctx, item, message)
}
func (s *Service) deliver(ctx context.Context, item domain.Channel, message string) (ChannelView, error) {
	switch item.DeliveryMode {
	case "", domain.DeliveryModeWebhook:
		return s.deliverWebhook(ctx, item, message)
	case domain.DeliveryModeFeishuApp:
		return s.deliverFeishuApp(ctx, item, message)
	default:
		return view(item), ErrChannelInvalid
	}
}

func (s *Service) deliverWebhook(ctx context.Context, item domain.Channel, message string) (ChannelView, error) {
	webhookBytes, err := s.Secrets.Decrypt(secretLabel(item.ID, "webhook"), item.WebhookCiphertext)
	if err != nil {
		return view(item), err
	}
	var signing string
	if len(item.SigningSecretCiphertext) > 0 {
		secret, err := s.Secrets.Decrypt(secretLabel(item.ID, "signing"), item.SigningSecretCiphertext)
		if err != nil {
			return view(item), err
		}
		signing = string(secret)
	}
	body, err := deliveryBody(item.Provider, message, signing, s.Now().Unix())
	if err != nil {
		return view(item), err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, string(webhookBytes), bytes.NewReader(body))
	if err != nil {
		return view(item), err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "Simplus")
	response, err := s.Client.Do(request)
	now := s.Now().UTC()
	if err != nil {
		_ = s.Store.RecordNotificationDelivery(ctx, item.ID, "failed", "DELIVERY_NETWORK_FAILED", now)
		return view(item), err
	}
	defer response.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 64<<10+1))
	if readErr != nil || len(responseBody) > 64<<10 || response.StatusCode < 200 || response.StatusCode >= 300 || !deliverySucceeded(item.Provider, responseBody) {
		_ = s.Store.RecordNotificationDelivery(ctx, item.ID, "failed", "DELIVERY_REJECTED", now)
		return view(item), errors.New("notification provider rejected delivery")
	}
	if err := s.Store.RecordNotificationDelivery(ctx, item.ID, "success", "", now); err != nil {
		return view(item), err
	}
	item.LastDeliveryAt, item.LastDeliveryStatus, item.LastErrorCode = now, "success", ""
	return view(item), nil
}

func (s *Service) deliverFeishuApp(ctx context.Context, item domain.Channel, message string) (ChannelView, error) {
	if s.FeishuMessenger == nil {
		return view(item), ErrFeishuProviderUnavailable
	}
	appID, err := s.Secrets.Decrypt(feishuSecretLabel(item.ID, "app-id"), item.FeishuAppIDCiphertext)
	if err != nil {
		return view(item), err
	}
	appSecret, err := s.Secrets.Decrypt(feishuSecretLabel(item.ID, "app-secret"), item.FeishuAppSecretCiphertext)
	if err != nil {
		return view(item), err
	}
	openID, err := s.Secrets.Decrypt(feishuSecretLabel(item.ID, "recipient-open-id"), item.FeishuRecipientOpenIDCiphertext)
	if err != nil {
		return view(item), err
	}
	credentials := FeishuRegistrationResult{AppID: string(appID), AppSecret: string(appSecret), OpenID: string(openID), TenantBrand: "feishu"}
	now := s.Now().UTC()
	if err := s.FeishuMessenger.SendText(ctx, credentials, message); err != nil {
		_ = s.Store.RecordNotificationDelivery(ctx, item.ID, "failed", "DELIVERY_REJECTED", now)
		return view(item), err
	}
	if err := s.Store.RecordNotificationDelivery(ctx, item.ID, "success", "", now); err != nil {
		return view(item), err
	}
	item.LastDeliveryAt, item.LastDeliveryStatus, item.LastErrorCode = now, "success", ""
	return view(item), nil
}
func deliveryBody(provider, message, secret string, timestamp int64) ([]byte, error) {
	switch provider {
	case "wecom":
		return json.Marshal(map[string]any{"msgtype": "text", "text": map[string]string{"content": message}})
	case "feishu":
		payload := map[string]any{"msg_type": "text", "content": map[string]string{"text": message}}
		if secret != "" {
			text := strconv.FormatInt(timestamp, 10)
			mac := hmac.New(sha256.New, []byte(text+"\n"+secret))
			payload["timestamp"] = text
			payload["sign"] = base64.StdEncoding.EncodeToString(mac.Sum(nil))
		}
		return json.Marshal(payload)
	default:
		return nil, ErrChannelInvalid
	}
}
func deliverySucceeded(provider string, body []byte) bool {
	var response struct {
		ErrCode *int `json:"errcode"`
		Code    *int `json:"code"`
	}
	if json.Unmarshal(body, &response) != nil {
		return false
	}
	if provider == "wecom" {
		return response.ErrCode != nil && *response.ErrCode == 0
	}
	return response.Code != nil && *response.Code == 0
}
func validateInput(provider, name, rawWebhook string, events []string) (string, string, *url.URL, []string, error) {
	provider, name = strings.ToLower(strings.TrimSpace(provider)), strings.TrimSpace(name)
	if provider != "wecom" && provider != "feishu" {
		return "", "", nil, nil, ErrChannelInvalid
	}
	name, events, err := validateSettings(name, events)
	if err != nil {
		return "", "", nil, nil, err
	}
	parsed, err := validateWebhook(provider, rawWebhook)
	if err != nil {
		return "", "", nil, nil, err
	}
	return provider, name, parsed, events, nil
}

func validateWebhook(provider, rawWebhook string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawWebhook))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" {
		return nil, ErrChannelInvalid
	}
	switch provider {
	case "wecom":
		if parsed.Hostname() != "qyapi.weixin.qq.com" || parsed.Path != "/cgi-bin/webhook/send" || parsed.Query().Get("key") == "" {
			return nil, ErrChannelInvalid
		}
	case "feishu":
		if parsed.Hostname() != "open.feishu.cn" || !strings.HasPrefix(parsed.Path, "/open-apis/bot/v2/hook/") || strings.TrimPrefix(parsed.Path, "/open-apis/bot/v2/hook/") == "" || parsed.RawQuery != "" {
			return nil, ErrChannelInvalid
		}
	}
	return parsed, nil
}

func validateSettings(name string, events []string) (string, []string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 {
		return "", nil, ErrChannelInvalid
	}
	events = append([]string(nil), events...)
	sort.Strings(events)
	if len(events) == 0 || len(events) > len(allowedEvents) {
		return "", nil, ErrChannelInvalid
	}
	for index, event := range events {
		if _, ok := allowedEvents[event]; !ok || (index > 0 && events[index-1] == event) {
			return "", nil, ErrChannelInvalid
		}
	}
	return name, events, nil
}
func newChannelID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "channel_" + base64.RawURLEncoding.EncodeToString(raw), nil
}
func secretLabel(id, kind string) string { return "notification-channel:v1:" + id + ":" + kind }
func feishuSecretLabel(id, kind string) string {
	return "notification-channel:v2:" + id + ":feishu-" + kind
}
func view(item domain.Channel) ChannelView {
	mode, target := string(item.DeliveryMode), "webhook"
	if item.DeliveryMode == "" {
		mode = string(domain.DeliveryModeWebhook)
	}
	if item.DeliveryMode == domain.DeliveryModeFeishuApp {
		target = "authorized_user"
	}
	return ChannelView{ID: item.ID, Provider: item.Provider, DisplayName: item.DisplayName, DeliveryMode: mode, TargetType: target, WebhookHint: item.WebhookHint, Enabled: item.Enabled, SigningSecretConfigured: len(item.SigningSecretCiphertext) > 0, EventKinds: append([]string(nil), item.EventKinds...), LastDeliveryAt: item.LastDeliveryAt, LastDeliveryStatus: item.LastDeliveryStatus, LastErrorCode: item.LastErrorCode}
}
func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
