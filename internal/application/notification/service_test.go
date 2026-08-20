package notification

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	domain "github.com/leonfox28/simplus/internal/domain/notification"
)

type deliveryRecord struct {
	id, status, code string
	at               time.Time
}

type memoryStore struct {
	channels   map[string]domain.Channel
	records    []deliveryRecord
	recordErr  error
	operations *[]string
}

func (s *memoryStore) ListNotificationChannels(context.Context) ([]domain.Channel, error) {
	items := make([]domain.Channel, 0, len(s.channels))
	for _, item := range s.channels {
		items = append(items, item)
	}
	return items, nil
}
func (s *memoryStore) ReadNotificationChannel(_ context.Context, id string) (domain.Channel, bool, error) {
	item, ok := s.channels[id]
	return item, ok, nil
}
func (s *memoryStore) UpsertNotificationChannel(_ context.Context, item domain.Channel) error {
	if s.channels == nil {
		s.channels = map[string]domain.Channel{}
	}
	s.channels[item.ID] = item
	return nil
}
func (s *memoryStore) DeleteNotificationChannel(_ context.Context, id string) (bool, error) {
	_, ok := s.channels[id]
	delete(s.channels, id)
	return ok, nil
}
func (s *memoryStore) RecordNotificationDelivery(_ context.Context, id, status, code string, at time.Time) error {
	if s.operations != nil {
		*s.operations = append(*s.operations, "record")
	}
	s.records = append(s.records, deliveryRecord{id: id, status: status, code: code, at: at})
	if s.recordErr != nil {
		return s.recordErr
	}
	item := s.channels[id]
	item.LastDeliveryStatus, item.LastErrorCode, item.LastDeliveryAt = status, code, at
	s.channels[id] = item
	return nil
}

type testCipher struct{}

func (testCipher) Encrypt(label string, value []byte) ([]byte, error) {
	return append([]byte(label+"|"), value...), nil
}
func (testCipher) Decrypt(label string, value []byte) ([]byte, error) {
	return bytes.TrimPrefix(value, []byte(label+"|")), nil
}

type webhookPortFake struct {
	target        WebhookTarget
	validateErr   error
	result        WebhookDeliveryResult
	deliverErr    error
	operations    *[]string
	validateCalls []struct {
		provider WebhookProvider
		raw      string
	}
	deliverCalls []WebhookDeliveryRequest
}

func (fake *webhookPortFake) ValidateTarget(provider WebhookProvider, raw string) (WebhookTarget, error) {
	fake.validateCalls = append(fake.validateCalls, struct {
		provider WebhookProvider
		raw      string
	}{provider: provider, raw: raw})
	return fake.target, fake.validateErr
}
func (fake *webhookPortFake) Deliver(_ context.Context, request WebhookDeliveryRequest) (WebhookDeliveryResult, error) {
	if fake.operations != nil {
		*fake.operations = append(*fake.operations, "deliver")
	}
	fake.deliverCalls = append(fake.deliverCalls, request)
	return fake.result, fake.deliverErr
}

func newTestService(t *testing.T, store Store) *Service {
	t.Helper()
	service, err := New(Dependencies{Store: store, Secrets: testCipher{}, Webhooks: &webhookPortFake{}})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestNewRejectsMissingAndTypedNilDependencies(t *testing.T) {
	var typedNilStore *memoryStore
	var typedNilCipher *testCipher
	var typedNilWebhooks *webhookPortFake
	validStore := &memoryStore{}
	validCipher := testCipher{}
	validWebhooks := &webhookPortFake{}
	tests := []struct {
		name string
		deps Dependencies
	}{
		{name: "missing store", deps: Dependencies{Secrets: validCipher, Webhooks: validWebhooks}},
		{name: "typed nil store", deps: Dependencies{Store: typedNilStore, Secrets: validCipher, Webhooks: validWebhooks}},
		{name: "missing cipher", deps: Dependencies{Store: validStore, Webhooks: validWebhooks}},
		{name: "typed nil cipher", deps: Dependencies{Store: validStore, Secrets: typedNilCipher, Webhooks: validWebhooks}},
		{name: "missing webhooks", deps: Dependencies{Store: validStore, Secrets: validCipher}},
		{name: "typed nil webhooks", deps: Dependencies{Store: validStore, Secrets: validCipher, Webhooks: typedNilWebhooks}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, err := New(test.deps)
			if service != nil || !errors.Is(err, ErrDependenciesInvalid) {
				t.Fatalf("service = %#v, error = %v", service, err)
			}
		})
	}
	service, err := New(Dependencies{Store: validStore, Secrets: validCipher, Webhooks: validWebhooks})
	if err != nil || service == nil || service.Now == nil || service.binding == nil {
		t.Fatalf("service = %#v, error = %v", service, err)
	}
}

func TestCreateAndUpdateDelegateTargetNormalizationAndPreserveCredentials(t *testing.T) {
	store := &memoryStore{}
	webhooks := &webhookPortFake{target: WebhookTarget{URL: "https://normalized.invalid/synthetic", Hint: "normalized.invalid"}}
	service, err := New(Dependencies{Store: store, Secrets: testCipher{}, Webhooks: webhooks})
	if err != nil {
		t.Fatal(err)
	}
	service.Now = func() time.Time { return time.Unix(1700000000, 0).UTC() }
	created, err := service.Create(context.Background(), " FEISHU ", " Alerts ", " supplied-target ", "signing-secret", true, []string{"system.degraded"})
	if err != nil {
		t.Fatal(err)
	}
	if len(webhooks.validateCalls) != 1 || webhooks.validateCalls[0].provider != WebhookProviderFeishu || webhooks.validateCalls[0].raw != " supplied-target " {
		t.Fatalf("validation calls = %#v", webhooks.validateCalls)
	}
	stored := store.channels[created.ID]
	webhookPlaintext, _ := testCipher{}.Decrypt(secretLabel(created.ID, "webhook"), stored.WebhookCiphertext)
	if string(webhookPlaintext) != webhooks.target.URL || stored.WebhookHint != webhooks.target.Hint || created.WebhookHint != webhooks.target.Hint || !created.SigningSecretConfigured {
		t.Fatalf("created = %#v, stored = %#v", created, stored)
	}
	originalCiphertext := append([]byte(nil), stored.WebhookCiphertext...)
	if _, err := service.Update(context.Background(), created.ID, "feishu", "Renamed", "", "", false, []string{"sms.received"}); err != nil {
		t.Fatal(err)
	}
	if len(webhooks.validateCalls) != 1 || !bytes.Equal(store.channels[created.ID].WebhookCiphertext, originalCiphertext) {
		t.Fatalf("empty replacement revalidated or changed ciphertext: calls=%d", len(webhooks.validateCalls))
	}
	webhooks.target = WebhookTarget{URL: "https://replacement.invalid/synthetic", Hint: "replacement.invalid"}
	if _, err := service.Update(context.Background(), created.ID, "feishu", "Renamed", "replacement-target", "", true, []string{"sms.received"}); err != nil {
		t.Fatal(err)
	}
	if len(webhooks.validateCalls) != 2 || store.channels[created.ID].WebhookHint != "replacement.invalid" {
		t.Fatalf("replacement validation = %#v, stored = %#v", webhooks.validateCalls, store.channels[created.ID])
	}
}

func TestCreateTranslatesTargetFailureAndRejectsInvalidPortTarget(t *testing.T) {
	store := &memoryStore{}
	providerError := errors.New("synthetic target failure")
	for _, fake := range []*webhookPortFake{
		{validateErr: providerError},
		{target: WebhookTarget{URL: strings.Repeat("x", WebhookURLByteLimit+1), Hint: "synthetic.invalid"}},
		{target: WebhookTarget{URL: "https://synthetic.invalid", Hint: ""}},
	} {
		service, err := New(Dependencies{Store: store, Secrets: testCipher{}, Webhooks: fake})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.Create(context.Background(), "wecom", "Ops", "synthetic", "", true, []string{"sms.received"}); !errors.Is(err, ErrChannelInvalid) {
			t.Fatalf("Create error = %v", err)
		}
	}
	if len(store.channels) != 0 {
		t.Fatalf("invalid target persisted: %#v", store.channels)
	}
}

func TestWebhookDeliveryHandoffAndOutcomePersistence(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	deliveryError := errors.New("synthetic bounded delivery failure")
	tests := []struct {
		name       string
		result     WebhookDeliveryResult
		deliverErr error
		wantStatus string
		wantCode   string
		wantErr    error
	}{
		{name: "delivered", result: WebhookDeliveryResult{Outcome: WebhookDelivered}, wantStatus: "success"},
		{name: "network", result: WebhookDeliveryResult{Outcome: WebhookNetworkFailed}, deliverErr: deliveryError, wantStatus: "failed", wantCode: "DELIVERY_NETWORK_FAILED", wantErr: deliveryError},
		{name: "rejected", result: WebhookDeliveryResult{Outcome: WebhookRejected}, deliverErr: deliveryError, wantStatus: "failed", wantCode: "DELIVERY_REJECTED", wantErr: deliveryError},
		{name: "preflight", deliverErr: deliveryError, wantErr: deliveryError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var operations []string
			store := &memoryStore{channels: map[string]domain.Channel{}, operations: &operations}
			item := webhookChannel(t, store, "feishu")
			webhooks := &webhookPortFake{result: test.result, deliverErr: test.deliverErr, operations: &operations}
			service, err := New(Dependencies{Store: store, Secrets: testCipher{}, Webhooks: webhooks})
			if err != nil {
				t.Fatal(err)
			}
			service.Now = func() time.Time { return now }
			view, err := service.deliverWebhook(context.Background(), item, "bounded message")
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("delivery error = %v, want %v", err, test.wantErr)
			}
			if len(webhooks.deliverCalls) != 1 {
				t.Fatalf("delivery calls = %#v", webhooks.deliverCalls)
			}
			request := webhooks.deliverCalls[0]
			if request.Provider != WebhookProviderFeishu || request.URL != "https://synthetic.invalid/hook" || request.SigningSecret != "synthetic-signing" || request.Message != "bounded message" || request.Timestamp != now.Unix() {
				t.Fatalf("request = %#v", request)
			}
			if test.wantStatus == "" {
				if len(store.records) != 0 {
					t.Fatalf("pre-dispatch result persisted %#v", store.records)
				}
				if strings.Join(operations, ",") != "deliver" {
					t.Fatalf("operations = %#v", operations)
				}
				return
			}
			if len(store.records) != 1 || store.records[0].status != test.wantStatus || store.records[0].code != test.wantCode || !store.records[0].at.Equal(now) {
				t.Fatalf("records = %#v", store.records)
			}
			if test.wantStatus == "success" && (view.LastDeliveryStatus != "success" || !view.LastDeliveryAt.Equal(now)) {
				t.Fatalf("view = %#v", view)
			}
			if strings.Join(operations, ",") != "deliver,record" {
				t.Fatalf("operations = %#v", operations)
			}
		})
	}
}

type failingDecryptCipher struct{}

func (failingDecryptCipher) Encrypt(string, []byte) ([]byte, error) {
	return nil, errors.New("synthetic encrypt failure")
}
func (failingDecryptCipher) Decrypt(string, []byte) ([]byte, error) {
	return nil, errors.New("synthetic decrypt failure")
}

func TestWebhookDecryptionFailureDoesNotDispatchOrPersist(t *testing.T) {
	store := &memoryStore{channels: map[string]domain.Channel{}}
	item := webhookChannel(t, store, "wecom")
	webhooks := &webhookPortFake{result: WebhookDeliveryResult{Outcome: WebhookDelivered}}
	service, err := New(Dependencies{Store: store, Secrets: failingDecryptCipher{}, Webhooks: webhooks})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.deliverWebhook(context.Background(), item, "bounded message"); err == nil {
		t.Fatal("decryption failure was accepted")
	}
	if len(webhooks.deliverCalls) != 0 || len(store.records) != 0 {
		t.Fatalf("decrypt failure dispatched or persisted: calls=%#v records=%#v", webhooks.deliverCalls, store.records)
	}
}

func TestWebhookDeliveryRejectsContradictoryResultsWithoutPersistence(t *testing.T) {
	deliveryError := errors.New("synthetic bounded delivery failure")
	tests := []struct {
		name   string
		result WebhookDeliveryResult
		err    error
	}{
		{name: "delivered with error", result: WebhookDeliveryResult{Outcome: WebhookDelivered}, err: deliveryError},
		{name: "network without error", result: WebhookDeliveryResult{Outcome: WebhookNetworkFailed}},
		{name: "rejected without error", result: WebhookDeliveryResult{Outcome: WebhookRejected}},
		{name: "empty without error"},
		{name: "unknown with error", result: WebhookDeliveryResult{Outcome: "future"}, err: deliveryError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &memoryStore{channels: map[string]domain.Channel{}}
			item := webhookChannel(t, store, "wecom")
			service, err := New(Dependencies{Store: store, Secrets: testCipher{}, Webhooks: &webhookPortFake{result: test.result, deliverErr: test.err}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.deliverWebhook(context.Background(), item, "bounded message"); !errors.Is(err, ErrWebhookResultInvalid) {
				t.Fatalf("delivery error = %v", err)
			}
			if len(store.records) != 0 {
				t.Fatalf("invalid result persisted %#v", store.records)
			}
		})
	}
}

func TestWebhookDeliveryPersistenceFailurePolicy(t *testing.T) {
	persistenceError := errors.New("synthetic persistence failure")
	deliveryError := errors.New("synthetic bounded delivery failure")
	store := &memoryStore{channels: map[string]domain.Channel{}, recordErr: persistenceError}
	item := webhookChannel(t, store, "wecom")
	service, err := New(Dependencies{Store: store, Secrets: testCipher{}, Webhooks: &webhookPortFake{
		result: WebhookDeliveryResult{Outcome: WebhookNetworkFailed}, deliverErr: deliveryError,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.deliverWebhook(context.Background(), item, "bounded message"); !errors.Is(err, deliveryError) {
		t.Fatalf("failure persistence replaced primary error: %v", err)
	}
	service.Webhooks = &webhookPortFake{result: WebhookDeliveryResult{Outcome: WebhookDelivered}}
	if _, err := service.deliverWebhook(context.Background(), item, "bounded message"); !errors.Is(err, persistenceError) {
		t.Fatalf("success persistence error = %v", err)
	}
}

func TestNotifyFiltersEventsAndBoundsMessages(t *testing.T) {
	store := &memoryStore{channels: map[string]domain.Channel{}}
	item := webhookChannel(t, store, "wecom")
	item.Enabled = true
	item.EventKinds = []string{"sms.received"}
	store.channels[item.ID] = item
	webhooks := &webhookPortFake{result: WebhookDeliveryResult{Outcome: WebhookDelivered}}
	service, err := New(Dependencies{Store: store, Secrets: testCipher{}, Webhooks: webhooks})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Notify(context.Background(), "call.incoming", "ignored"); err != nil {
		t.Fatal(err)
	}
	if len(webhooks.deliverCalls) != 0 {
		t.Fatalf("filtered event delivered: %#v", webhooks.deliverCalls)
	}
	if err := service.Notify(context.Background(), "sms.received", "received"); err != nil {
		t.Fatal(err)
	}
	if len(webhooks.deliverCalls) != 1 {
		t.Fatalf("delivery calls = %d", len(webhooks.deliverCalls))
	}
	if err := service.Notify(context.Background(), "sms.received", strings.Repeat("界", WebhookMessageRuneLimit+1)); !errors.Is(err, ErrChannelInvalid) {
		t.Fatalf("oversize message error = %v", err)
	}
}

func TestNotifyReceivedSMSDeliversExactContentOnlyToFeishuModes(t *testing.T) {
	const (
		sender = "Service"
		body   = "第一行\nsecond line 🙂"
		want   = "[Simplus] 新短信\n发件人：Service\n内容：\n第一行\nsecond line 🙂"
	)
	t.Run("webhook", func(t *testing.T) {
		store := &memoryStore{channels: map[string]domain.Channel{}}
		feishu := webhookChannelWithID(t, store, "channel_AAAAAAAAAAAAAAAAAAAAAA", "feishu")
		feishu.Enabled = true
		store.channels[feishu.ID] = feishu
		wecom := webhookChannelWithID(t, store, "channel_BBBBBBBBBBBBBBBBBBBBBB", "wecom")
		wecom.Enabled = true
		store.channels[wecom.ID] = wecom
		webhooks := &webhookPortFake{result: WebhookDeliveryResult{Outcome: WebhookDelivered}}
		service, err := New(Dependencies{Store: store, Secrets: testCipher{}, Webhooks: webhooks})
		if err != nil {
			t.Fatal(err)
		}

		if err := service.NotifyReceivedSMS(context.Background(), sender, body); err != nil {
			t.Fatal(err)
		}
		if len(webhooks.deliverCalls) != 1 || webhooks.deliverCalls[0].Provider != WebhookProviderFeishu || webhooks.deliverCalls[0].Message != want {
			t.Fatalf("delivery calls = %#v", webhooks.deliverCalls)
		}
	})

	t.Run("private app", func(t *testing.T) {
		store := &memoryStore{channels: map[string]domain.Channel{}}
		item := feishuAppChannel(t, store, "channel_CCCCCCCCCCCCCCCCCCCCCC")
		var messages []string
		service, err := New(Dependencies{Store: store, Secrets: testCipher{}, Webhooks: &webhookPortFake{}})
		if err != nil {
			t.Fatal(err)
		}
		service.FeishuMessenger = messengerFunc(func(_ context.Context, _ FeishuRegistrationResult, message string) error {
			messages = append(messages, message)
			return nil
		})

		if err := service.NotifyReceivedSMS(context.Background(), sender, body); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(messages, []string{want}) || store.channels[item.ID].LastDeliveryStatus != "success" {
			t.Fatalf("messages = %#v, channel = %#v", messages, store.channels[item.ID])
		}
	})
}

func TestNotifyReceivedSMSSummaryPreservesNonFeishuBehavior(t *testing.T) {
	store := &memoryStore{channels: map[string]domain.Channel{}}
	feishu := webhookChannelWithID(t, store, "channel_AAAAAAAAAAAAAAAAAAAAAA", "feishu")
	feishu.Enabled = true
	store.channels[feishu.ID] = feishu
	wecom := webhookChannelWithID(t, store, "channel_BBBBBBBBBBBBBBBBBBBBBB", "wecom")
	wecom.Enabled = true
	store.channels[wecom.ID] = wecom
	webhooks := &webhookPortFake{result: WebhookDeliveryResult{Outcome: WebhookDelivered}}
	service, err := New(Dependencies{Store: store, Secrets: testCipher{}, Webhooks: webhooks})
	if err != nil {
		t.Fatal(err)
	}

	if err := service.NotifyReceivedSMSSummary(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	if len(webhooks.deliverCalls) != 1 || webhooks.deliverCalls[0].Provider != WebhookProviderWeCom || webhooks.deliverCalls[0].Message != "[Simplus] 收到 2 条新短信" {
		t.Fatalf("delivery calls = %#v", webhooks.deliverCalls)
	}
}

func TestNotifyReceivedSMSAttemptsKeepLatestStatusAndSafeErrors(t *testing.T) {
	const privateBody = "private-body-marker"
	store := &memoryStore{channels: map[string]domain.Channel{}}
	item := webhookChannel(t, store, "feishu")
	item.Enabled = true
	store.channels[item.ID] = item
	deliveryError := errors.New("synthetic bounded delivery failure")
	webhooks := &webhookPortFake{
		result:     WebhookDeliveryResult{Outcome: WebhookNetworkFailed},
		deliverErr: deliveryError,
	}
	service, err := New(Dependencies{Store: store, Secrets: testCipher{}, Webhooks: webhooks})
	if err != nil {
		t.Fatal(err)
	}

	err = service.NotifyReceivedSMS(context.Background(), "10086", privateBody)
	if !errors.Is(err, deliveryError) || strings.Contains(err.Error(), privateBody) || store.channels[item.ID].LastDeliveryStatus != "failed" {
		t.Fatalf("first error = %v, channel = %#v", err, store.channels[item.ID])
	}
	webhooks.result = WebhookDeliveryResult{Outcome: WebhookDelivered}
	webhooks.deliverErr = nil
	if err := service.NotifyReceivedSMS(context.Background(), "10086", "later success"); err != nil {
		t.Fatal(err)
	}
	if store.channels[item.ID].LastDeliveryStatus != "success" || store.channels[item.ID].LastErrorCode != "" {
		t.Fatalf("latest channel status = %#v", store.channels[item.ID])
	}
}

func webhookChannel(t *testing.T, store *memoryStore, provider string) domain.Channel {
	return webhookChannelWithID(t, store, "channel_AAAAAAAAAAAAAAAAAAAAAA", provider)
}

func webhookChannelWithID(t *testing.T, store *memoryStore, id, provider string) domain.Channel {
	t.Helper()
	cipher := testCipher{}
	webhook, err := cipher.Encrypt(secretLabel(id, "webhook"), []byte("https://synthetic.invalid/hook"))
	if err != nil {
		t.Fatal(err)
	}
	signing, err := cipher.Encrypt(secretLabel(id, "signing"), []byte("synthetic-signing"))
	if err != nil {
		t.Fatal(err)
	}
	if provider == "wecom" {
		signing = nil
	}
	item := domain.Channel{
		ID: id, Provider: provider, DeliveryMode: domain.DeliveryModeWebhook, DisplayName: "Synthetic",
		WebhookCiphertext: webhook, WebhookHint: "synthetic.invalid", SigningSecretCiphertext: signing,
		EventKinds: []string{"sms.received"}, LastDeliveryStatus: "never",
	}
	store.channels[id] = item
	return item
}

func feishuAppChannel(t *testing.T, store *memoryStore, id string) domain.Channel {
	t.Helper()
	cipher := testCipher{}
	appID, err := cipher.Encrypt(feishuSecretLabel(id, "app-id"), []byte("cli_synthetic"))
	if err != nil {
		t.Fatal(err)
	}
	appSecret, err := cipher.Encrypt(feishuSecretLabel(id, "app-secret"), []byte("synthetic-secret"))
	if err != nil {
		t.Fatal(err)
	}
	openID, err := cipher.Encrypt(feishuSecretLabel(id, "recipient-open-id"), []byte("ou_synthetic"))
	if err != nil {
		t.Fatal(err)
	}
	item := domain.Channel{
		ID: id, Provider: string(WebhookProviderFeishu), DeliveryMode: domain.DeliveryModeFeishuApp,
		DisplayName: "Synthetic app", Enabled: true, EventKinds: []string{"sms.received"},
		FeishuAppIDCiphertext: appID, FeishuAppSecretCiphertext: appSecret,
		FeishuRecipientOpenIDCiphertext: openID, LastDeliveryStatus: "never",
	}
	store.channels[id] = item
	return item
}
