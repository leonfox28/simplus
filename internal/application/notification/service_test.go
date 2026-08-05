package notification

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	domain "github.com/leonfox28/simplus/internal/domain/notification"
)

type memoryStore struct{ channels map[string]domain.Channel }

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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestChannelsValidateOfficialHostsHideSecretsAndDeliver(t *testing.T) {
	store := &memoryStore{}
	service := New(store, testCipher{})
	service.Now = func() time.Time { return time.Unix(1700000000, 0).UTC() }
	var bodies [][]byte
	service.Client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(request.Body)
		bodies = append(bodies, body)
		response := `{"errcode":0}`
		if request.URL.Hostname() == "open.feishu.cn" {
			response = `{"code":0,"msg":"success"}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(response)), Header: http.Header{}}, nil
	})}
	wecom, err := service.Create(context.Background(), "wecom", "Operations", "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=secret-key", "", true, []string{"system.degraded"})
	if err != nil {
		t.Fatal(err)
	}
	if wecom.WebhookHint != "qyapi.weixin.qq.com" || strings.Contains(wecom.WebhookHint, "secret") {
		t.Fatalf("view = %#v", wecom)
	}
	feishu, err := service.Create(context.Background(), "feishu", "Alerts", "https://open.feishu.cn/open-apis/bot/v2/hook/secret-hook", "signing-secret", true, []string{"system.degraded"})
	if err != nil {
		t.Fatal(err)
	}
	if !feishu.SigningSecretConfigured {
		t.Fatal("signing secret not marked")
	}
	if _, err := service.Test(context.Background(), wecom.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Test(context.Background(), feishu.ID); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 || !bytes.Contains(bodies[0], []byte(`"msgtype":"text"`)) || !bytes.Contains(bodies[1], []byte(`"msg_type":"text"`)) || !bytes.Contains(bodies[1], []byte(`"sign"`)) {
		t.Fatalf("bodies = %s", bodies)
	}
}

func TestNotifyFiltersEventsAndRejectsArbitraryWebhook(t *testing.T) {
	store := &memoryStore{}
	service := New(store, testCipher{})
	deliveries := 0
	service.Client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		deliveries++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"errcode":0}`)), Header: http.Header{}}, nil
	})}
	if _, err := service.Create(context.Background(), "wecom", "Ops", "https://example.com/hook", "", true, []string{"sms.received"}); err == nil {
		t.Fatal("arbitrary webhook accepted")
	}
	if _, err := service.Create(context.Background(), "wecom", "Ops", "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=secret", "", true, []string{"sms.received"}); err != nil {
		t.Fatal(err)
	}
	if err := service.Notify(context.Background(), "call.incoming", "ignored"); err != nil {
		t.Fatal(err)
	}
	if deliveries != 0 {
		t.Fatalf("deliveries = %d", deliveries)
	}
	if err := service.Notify(context.Background(), "sms.received", "received"); err != nil {
		t.Fatal(err)
	}
	if deliveries != 1 {
		t.Fatalf("deliveries = %d", deliveries)
	}
}
