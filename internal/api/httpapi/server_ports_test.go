package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/leonfox28/simplus/internal/application/health"
	"github.com/leonfox28/simplus/internal/application/inventory"
	"github.com/leonfox28/simplus/internal/application/realtime"
	setupapp "github.com/leonfox28/simplus/internal/application/setup"
)

var (
	_ HealthReader    = (*health.Service)(nil)
	_ SetupManager    = (*setupapp.Service)(nil)
	_ InventoryReader = (*inventory.Service)(nil)
	_ RealtimeManager = (*realtime.Hub)(nil)
	_ HealthReader    = (*fakeHealthReader)(nil)
	_ SetupManager    = (*fakeSetupManager)(nil)
	_ InventoryReader = (*fakeInventoryReader)(nil)
	_ RealtimeManager = (*fakeRealtimeManager)(nil)
)

type fakeHealthReader struct{}

func (*fakeHealthReader) Snapshot(context.Context) (health.Snapshot, error) {
	return health.Snapshot{}, nil
}

type fakeSetupManager struct {
	status setupapp.Status
}

func (manager *fakeSetupManager) Status(context.Context) (setupapp.Status, error) {
	return manager.status, nil
}

func (*fakeSetupManager) ConsumeBootstrap(context.Context, string) (setupapp.SessionGrant, error) {
	return setupapp.SessionGrant{}, nil
}

func (*fakeSetupManager) ReadSession(context.Context, string) (setupapp.Session, error) {
	return setupapp.Session{}, nil
}

func (*fakeSetupManager) ConfigureAdministrator(context.Context, string, setupapp.AdministratorInput) (setupapp.Session, error) {
	return setupapp.Session{}, nil
}

func (*fakeSetupManager) ConfigureStorage(context.Context, string, setupapp.StorageInput) (setupapp.Session, error) {
	return setupapp.Session{}, nil
}

func (*fakeSetupManager) ConfigureHTTPS(context.Context, string, setupapp.HTTPSInput) (setupapp.Session, error) {
	return setupapp.Session{}, nil
}

func (*fakeSetupManager) ConfirmHTTPS(context.Context, string, string) (setupapp.Session, error) {
	return setupapp.Session{}, nil
}

func (*fakeSetupManager) ReadRootCertificate(context.Context, string) ([]byte, string, error) {
	return nil, "", nil
}

func (*fakeSetupManager) ConfirmHardwareReview(context.Context, string, setupapp.HardwareReviewInput) (setupapp.Session, error) {
	return setupapp.Session{}, nil
}

func (*fakeSetupManager) Complete(context.Context, string, setupapp.HardwareReviewInput) (setupapp.Completion, error) {
	return setupapp.Completion{}, nil
}

func (*fakeSetupManager) BeginAdministratorSetup(context.Context) (setupapp.SessionGrant, error) {
	return setupapp.SessionGrant{}, nil
}

type fakeInventoryReader struct{}

func (*fakeInventoryReader) Snapshot(context.Context) (inventory.Snapshot, error) {
	return inventory.Snapshot{}, nil
}

func (*fakeInventoryReader) Topology(context.Context) (inventory.Topology, error) {
	return inventory.Topology{}, nil
}

type fakeRealtimeManager struct {
	subscription   *realtime.Subscription
	subscribeCalls int
	publishCalls   int
	topics         []realtime.Topic
	attention      realtime.Attention
}

func (manager *fakeRealtimeManager) Subscribe() *realtime.Subscription {
	manager.subscribeCalls++
	return manager.subscription
}

func (manager *fakeRealtimeManager) Publish(topics []realtime.Topic, attention realtime.Attention) {
	manager.publishCalls++
	manager.topics = append([]realtime.Topic(nil), topics...)
	manager.attention = attention
}

func TestHTTPApplicationPortMethodSetsRemainNarrow(t *testing.T) {
	tests := []struct {
		name     string
		port     reflect.Type
		expected []string
	}{
		{name: "health", port: reflect.TypeOf((*HealthReader)(nil)).Elem(), expected: []string{"Snapshot"}},
		{name: "setup", port: reflect.TypeOf((*SetupManager)(nil)).Elem(), expected: []string{
			"Status", "ConsumeBootstrap", "ReadSession", "ConfigureAdministrator", "ConfigureStorage",
			"ConfigureHTTPS", "ConfirmHTTPS", "ReadRootCertificate", "ConfirmHardwareReview", "Complete",
			"BeginAdministratorSetup",
		}},
		{name: "inventory", port: reflect.TypeOf((*InventoryReader)(nil)).Elem(), expected: []string{"Snapshot", "Topology"}},
		{name: "realtime", port: reflect.TypeOf((*RealtimeManager)(nil)).Elem(), expected: []string{"Subscribe", "Publish"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := make([]string, 0, test.port.NumMethod())
			for index := 0; index < test.port.NumMethod(); index++ {
				actual = append(actual, test.port.Method(index).Name)
			}
			sort.Strings(actual)
			sort.Strings(test.expected)
			if !reflect.DeepEqual(actual, test.expected) {
				t.Fatalf("methods = %v, want %v", actual, test.expected)
			}
		})
	}
}

func TestServerAcceptsIndependentApplicationPortFakes(t *testing.T) {
	events := make(chan realtime.Event)
	close(events)
	realtimeManager := &fakeRealtimeManager{subscription: &realtime.Subscription{C: events}}
	server := WithRealtime(New(
		&fakeHealthReader{},
		&fakeSetupManager{status: setupapp.Status{BusinessAPIAvailable: true}},
		&fakeInventoryReader{},
		nil,
		acceptingAuthenticator{},
		nil,
	), realtimeManager)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	request.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: strings.Repeat("a", 43)})
	response := httptest.NewRecorder()
	server.StreamEvents(response, request)
	if realtimeManager.subscribeCalls != 1 {
		t.Fatalf("Subscribe calls = %d, want 1", realtimeManager.subscribeCalls)
	}
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream response status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}

	server.publish([]realtime.Topic{realtime.TopicInventory, realtime.TopicLines}, realtime.AttentionSMSReceived)
	if realtimeManager.publishCalls != 1 ||
		!reflect.DeepEqual(realtimeManager.topics, []realtime.Topic{realtime.TopicInventory, realtime.TopicLines}) ||
		realtimeManager.attention != realtime.AttentionSMSReceived {
		t.Fatalf("Publish calls=%d topics=%v attention=%q", realtimeManager.publishCalls, realtimeManager.topics, realtimeManager.attention)
	}
}

func TestServerTreatsNilApplicationPortsAsUnavailable(t *testing.T) {
	tests := []struct {
		name      string
		health    HealthReader
		setup     SetupManager
		inventory InventoryReader
		realtime  RealtimeManager
	}{
		{name: "raw nil"},
		{
			name:      "typed nil",
			health:    (*fakeHealthReader)(nil),
			setup:     (*fakeSetupManager)(nil),
			inventory: (*fakeInventoryReader)(nil),
			realtime:  (*fakeRealtimeManager)(nil),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := WithRealtime(New(test.health, test.setup, test.inventory, nil, acceptingAuthenticator{}, nil), test.realtime)
			if !httpDependencyMissing(server.health) || !httpDependencyMissing(server.setup) ||
				!httpDependencyMissing(server.inventory) || !httpDependencyMissing(server.realtime) {
				t.Fatalf("dependencies were not absent = health:%v setup:%v inventory:%v realtime:%v", server.health, server.setup, server.inventory, server.realtime)
			}
			if server.realtimeSessionValid(context.Background(), "synthetic") {
				t.Fatal("realtime session was valid without Setup")
			}
			server.publish([]realtime.Topic{realtime.TopicSystem}, "")

			loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"synthetic"}`))
			loginResponse := httptest.NewRecorder()
			server.Login(loginResponse, loginRequest)
			if loginResponse.Code != http.StatusOK {
				t.Fatalf("login status=%d body=%s", loginResponse.Code, loginResponse.Body.String())
			}

			streamServer := WithRealtime(New(
				&fakeHealthReader{},
				&fakeSetupManager{status: setupapp.Status{BusinessAPIAvailable: true}},
				&fakeInventoryReader{},
				nil,
				acceptingAuthenticator{},
				nil,
			), test.realtime)
			streamRequest := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
			streamRequest.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: strings.Repeat("a", 43)})
			streamResponse := httptest.NewRecorder()
			streamServer.StreamEvents(streamResponse, streamRequest)
			if streamResponse.Code != http.StatusInternalServerError || !strings.Contains(streamResponse.Body.String(), "EVENT_STREAM_UNAVAILABLE") {
				t.Fatalf("stream status=%d body=%s", streamResponse.Code, streamResponse.Body.String())
			}
		})
	}
}

func TestServerPreservesTypedNilReceiverErrorDispatch(t *testing.T) {
	var healthService *health.Service
	var setupService *setupapp.Service
	var inventoryService *inventory.Service
	server := New(healthService, setupService, inventoryService, nil, acceptingAuthenticator{}, nil)

	if _, err := server.health.Snapshot(context.Background()); err == nil {
		t.Fatal("typed-nil HealthReader returned no error")
	}
	if _, err := server.setup.Status(context.Background()); err == nil {
		t.Fatal("typed-nil SetupManager returned no error")
	}
	if _, err := server.inventory.Snapshot(context.Background()); err == nil {
		t.Fatal("typed-nil InventoryReader returned no error")
	}
}

func TestWithRealtimeKeepsNilServer(t *testing.T) {
	var manager *fakeRealtimeManager
	if server := WithRealtime(nil, manager); server != nil {
		t.Fatalf("server = %#v, want nil", server)
	}
}
