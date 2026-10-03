package notification

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
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
var allowedEvents = map[string]struct{}{"vowifi.connected": {}, "vowifi.disconnected": {}, "cellular.connected": {}, "cellular.disconnected": {}, "sms.received": {}, "sms.failed": {}, "call.incoming": {}, "call.missed": {}, "system.degraded": {}}

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
	DeliveryQueue
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
	ProcessContext context.Context
	Registrar      FeishuRegistrar
	Messenger      FeishuMessenger
	OnChange       func()
	Store          Store
	Secrets        SecretCipher
	Webhooks       WebhookPort
}
type ChannelView struct {
	PendingCount, FailedCount                           int64
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
	if dependencies.Store == nil {
		return nil, fmt.Errorf("%w: store is required", ErrDependenciesInvalid)
	}
	if dependencies.Secrets == nil {
		return nil, fmt.Errorf("%w: secret cipher is required", ErrDependenciesInvalid)
	}
	if dependencies.Webhooks == nil {
		return nil, fmt.Errorf("%w: webhook port is required", ErrDependenciesInvalid)
	}
	service := &Service{
		Store: dependencies.Store, Secrets: dependencies.Secrets, Webhooks: dependencies.Webhooks,
		Now: time.Now, binding: newBindingController(),
	}
	if dependencies.Registrar == nil && dependencies.Messenger == nil {
		dependencies.Registrar, dependencies.Messenger = DisabledFeishu{}, DisabledFeishu{}
	}
	if dependencies.Registrar == nil || dependencies.Messenger == nil {
		return nil, ErrDependenciesInvalid
	}
	service.configureFeishuBinding(dependencies.ProcessContext, dependencies.Registrar, dependencies.Messenger, dependencies.OnChange)
	return service, nil
}

func (s *Service) List(ctx context.Context) ([]ChannelView, error) {
	items, err := s.Store.ListNotificationChannels(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]ChannelView, 0, len(items))
	for _, item := range items {
		v := view(item)
		counts, countErr := s.Store.NotificationDeliveryCounts(ctx, item.ID)
		if countErr != nil {
			return nil, countErr
		}
		v.PendingCount, v.FailedCount = counts.Pending, counts.Failed
		result = append(result, v)
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
	if _, ok := allowedEvents[event]; !ok || message == "" || len([]rune(message)) > WebhookMessageRuneLimit {
		return ErrChannelInvalid
	}
	id, err := newChannelID()
	if err != nil {
		return err
	}
	return s.Enqueue(ctx, domain.Event{Key: id, Kind: event, ObjectID: "system", Message: message, ObservedAt: s.Now().UTC()})
}
func (s *Service) Enqueue(ctx context.Context, event domain.Event) error {
	if _, ok := allowedEvents[event.Kind]; !ok || event.Key == "" || event.ObjectID == "" || event.Message == "" || len([]rune(event.Message)) > WebhookMessageRuneLimit || event.ObservedAt.IsZero() {
		return ErrChannelInvalid
	}
	return s.Store.EnqueueNotification(ctx, event)
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
		return view(item), errors.Join(ErrDeliveryPermanent, err)
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
