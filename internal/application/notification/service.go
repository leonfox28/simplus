package notification

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	domain "github.com/leonfox28/simplus/internal/domain/notification"
)

var channelIDPattern = regexp.MustCompile(`^channel_[A-Za-z0-9_-]{22}$`)
var ErrChannelInvalid = errors.New("notification channel request is invalid")
var ErrChannelNotFound = errors.New("notification channel not found")
var ErrDependenciesInvalid = errors.New("notification dependencies are invalid")
var ErrWebhookResultInvalid = errors.New("notification webhook delivery result is invalid")
var allowedEvents = map[string]struct{}{"sms.received": {}, "sms.failed": {}, "call.incoming": {}, "call.missed": {}, "system.degraded": {}}

const (
	WebhookURLByteLimit       = 4096
	WebhookHintByteLimit      = 255
	WebhookSigningSecretLimit = 512
	WebhookMessageRuneLimit   = 4000
)

type WebhookProvider string

const (
	WebhookProviderWeCom  WebhookProvider = "wecom"
	WebhookProviderFeishu WebhookProvider = "feishu"
)

type WebhookTarget struct {
	URL  string
	Hint string
}

type WebhookDeliveryRequest struct {
	Provider      WebhookProvider
	URL           string
	SigningSecret string
	Message       string
	Timestamp     int64
}

type WebhookDeliveryOutcome string

const (
	WebhookDelivered     WebhookDeliveryOutcome = "delivered"
	WebhookNetworkFailed WebhookDeliveryOutcome = "network_failed"
	WebhookRejected      WebhookDeliveryOutcome = "rejected"
)

type WebhookDeliveryResult struct {
	Outcome WebhookDeliveryOutcome
}

type WebhookPort interface {
	ValidateTarget(WebhookProvider, string) (WebhookTarget, error)
	Deliver(context.Context, WebhookDeliveryRequest) (WebhookDeliveryResult, error)
}

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
type Dependencies struct {
	Store    Store
	Secrets  SecretCipher
	Webhooks WebhookPort
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
	Webhooks        WebhookPort
	Now             func() time.Time
	FeishuRegistrar FeishuRegistrar
	FeishuMessenger FeishuMessenger
	binding         *bindingController
}

func New(dependencies Dependencies) (*Service, error) {
	if notificationDependencyMissing(dependencies.Store) {
		return nil, fmt.Errorf("%w: store is required", ErrDependenciesInvalid)
	}
	if notificationDependencyMissing(dependencies.Secrets) {
		return nil, fmt.Errorf("%w: secret cipher is required", ErrDependenciesInvalid)
	}
	if notificationDependencyMissing(dependencies.Webhooks) {
		return nil, fmt.Errorf("%w: webhook port is required", ErrDependenciesInvalid)
	}
	return &Service{
		Store: dependencies.Store, Secrets: dependencies.Secrets, Webhooks: dependencies.Webhooks,
		Now: time.Now, binding: newBindingController(),
	}, nil
}

func notificationDependencyMissing(dependency any) bool {
	if dependency == nil {
		return true
	}
	value := reflect.ValueOf(dependency)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
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
	provider, name, events, err := validateInput(provider, name, events)
	if err != nil {
		return ChannelView{}, err
	}
	target, err := s.Webhooks.ValidateTarget(WebhookProvider(provider), webhook)
	if err != nil || !validWebhookTarget(target) {
		return ChannelView{}, ErrChannelInvalid
	}
	id, err := newChannelID()
	if err != nil {
		return ChannelView{}, err
	}
	webhookCipher, err := s.Secrets.Encrypt(secretLabel(id, "webhook"), []byte(target.URL))
	if err != nil {
		return ChannelView{}, err
	}
	var signingCipher []byte
	if signingSecret != "" {
		if provider != string(WebhookProviderFeishu) || len(signingSecret) > WebhookSigningSecretLimit {
			return ChannelView{}, ErrChannelInvalid
		}
		signingCipher, err = s.Secrets.Encrypt(secretLabel(id, "signing"), []byte(signingSecret))
		if err != nil {
			return ChannelView{}, err
		}
	}
	now := s.Now().UTC()
	item := domain.Channel{ID: id, Provider: provider, DeliveryMode: domain.DeliveryModeWebhook, DisplayName: name, WebhookCiphertext: webhookCipher, WebhookHint: target.Hint, SigningSecretCiphertext: signingCipher, Enabled: enabled, EventKinds: events, LastDeliveryStatus: "never", CreatedAt: now, UpdatedAt: now}
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
		if webhook != "" {
			target, validationErr := s.Webhooks.ValidateTarget(WebhookProvider(provider), webhook)
			if validationErr != nil || !validWebhookTarget(target) {
				return ChannelView{}, ErrChannelInvalid
			}
			item.WebhookCiphertext, err = s.Secrets.Encrypt(secretLabel(id, "webhook"), []byte(target.URL))
			if err != nil {
				return ChannelView{}, err
			}
			item.WebhookHint = target.Hint
		}
		if signingSecret != "" {
			if provider != string(WebhookProviderFeishu) || len(signingSecret) > WebhookSigningSecretLimit {
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
	return s.notify(ctx, event, message, nil)
}

func (s *Service) NotifyReceivedSMS(ctx context.Context, sender, body string) error {
	if sender == "" || body == "" {
		return ErrChannelInvalid
	}
	message := fmt.Sprintf("[Simplus] 新短信\n发件人：%s\n内容：\n%s", sender, body)
	return s.notify(ctx, "sms.received", message, func(item domain.Channel) bool {
		return item.Provider == string(WebhookProviderFeishu)
	})
}

func (s *Service) NotifyReceivedSMSSummary(ctx context.Context, count int) error {
	if count <= 0 {
		return ErrChannelInvalid
	}
	message := fmt.Sprintf("[Simplus] 收到 %d 条新短信", count)
	return s.notify(ctx, "sms.received", message, func(item domain.Channel) bool {
		return item.Provider != string(WebhookProviderFeishu)
	})
}

func (s *Service) notify(ctx context.Context, event, message string, include func(domain.Channel) bool) error {
	if _, ok := allowedEvents[event]; !ok || message == "" || len([]rune(message)) > WebhookMessageRuneLimit {
		return ErrChannelInvalid
	}
	items, err := s.Store.ListNotificationChannels(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, item := range items {
		if !item.Enabled || !contains(item.EventKinds, event) || include != nil && !include(item) {
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
	request := WebhookDeliveryRequest{
		Provider: WebhookProvider(item.Provider), URL: string(webhookBytes), SigningSecret: signing,
		Message: message, Timestamp: s.Now().Unix(),
	}
	result, err := s.Webhooks.Deliver(ctx, request)
	now := s.Now().UTC()
	switch result.Outcome {
	case WebhookDelivered:
		if err != nil {
			return view(item), ErrWebhookResultInvalid
		}
		if err := s.Store.RecordNotificationDelivery(ctx, item.ID, "success", "", now); err != nil {
			return view(item), err
		}
		item.LastDeliveryAt, item.LastDeliveryStatus, item.LastErrorCode = now, "success", ""
		return view(item), nil
	case WebhookNetworkFailed:
		if err == nil {
			return view(item), ErrWebhookResultInvalid
		}
		_ = s.Store.RecordNotificationDelivery(ctx, item.ID, "failed", "DELIVERY_NETWORK_FAILED", now)
		return view(item), err
	case WebhookRejected:
		if err == nil {
			return view(item), ErrWebhookResultInvalid
		}
		_ = s.Store.RecordNotificationDelivery(ctx, item.ID, "failed", "DELIVERY_REJECTED", now)
		return view(item), err
	case "":
		if err != nil {
			return view(item), err
		}
		return view(item), ErrWebhookResultInvalid
	default:
		return view(item), ErrWebhookResultInvalid
	}
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
func validateInput(provider, name string, events []string) (string, string, []string, error) {
	provider, name = strings.ToLower(strings.TrimSpace(provider)), strings.TrimSpace(name)
	if provider != string(WebhookProviderWeCom) && provider != string(WebhookProviderFeishu) {
		return "", "", nil, ErrChannelInvalid
	}
	name, events, err := validateSettings(name, events)
	if err != nil {
		return "", "", nil, err
	}
	return provider, name, events, nil
}

func validWebhookTarget(target WebhookTarget) bool {
	return target.URL != "" && len(target.URL) <= WebhookURLByteLimit &&
		target.Hint != "" && len(target.Hint) <= WebhookHintByteLimit
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
