package mihomo

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	domain "github.com/leonfox28/simplus/internal/domain/mihomo"
)

const refreshTestSubscriptionID = "subscription_abcdefghijklmnopqrstuv"

type subscriptionRoundTripFunc func(*http.Request) (*http.Response, error)

func (function subscriptionRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type refreshStoreStub struct {
	item                            domain.Subscription
	selected                        string
	replaced                        []domain.Node
	replaceStatus, replaceErrorCode string
	markedCode                      string
	markErr                         error
}

func (store *refreshStoreStub) ListMihomoSubscriptions(context.Context) ([]domain.Subscription, error) {
	return []domain.Subscription{store.item}, nil
}
func (store *refreshStoreStub) ReadMihomoSubscription(_ context.Context, id string) (domain.Subscription, bool, error) {
	return store.item, id == store.item.ID, nil
}
func (store *refreshStoreStub) UpsertMihomoSubscription(_ context.Context, item domain.Subscription) error {
	store.item = item
	return nil
}
func (*refreshStoreStub) DeleteMihomoSubscription(context.Context, string) (bool, error) {
	return false, nil
}
func (store *refreshStoreStub) ReplaceMihomoSubscriptionNodes(_ context.Context, _ string, nodes []domain.Node, refreshedAt time.Time, status, errorCode string) error {
	store.replaced = append([]domain.Node(nil), nodes...)
	store.replaceStatus, store.replaceErrorCode = status, errorCode
	store.item.LastRefreshAt, store.item.LastRefreshStatus = refreshedAt, status
	store.item.LastErrorCode, store.item.NodeCount = errorCode, len(nodes)
	return nil
}
func (store *refreshStoreStub) ListMihomoSubscriptionNodes(context.Context, string) ([]domain.Node, error) {
	return append([]domain.Node(nil), store.replaced...), nil
}
func (store *refreshStoreStub) MarkMihomoSubscriptionRefreshFailure(_ context.Context, _ string, code string, refreshedAt time.Time) error {
	if store.markErr != nil {
		return store.markErr
	}
	store.markedCode = code
	store.item.LastRefreshAt, store.item.LastRefreshStatus, store.item.LastErrorCode = refreshedAt, "failed", code
	return nil
}
func (store *refreshStoreStub) ReadMihomoRuntimeSelection(context.Context) (string, string, error) {
	return store.selected, "", nil
}
func (store *refreshStoreStub) WriteMihomoSelectedSubscription(_ context.Context, id string, _ time.Time) error {
	store.selected = id
	return nil
}

type refreshArtifactStub struct {
	buildErr   error
	buildCalls int
	raw        []byte
	nodes      []domain.Node
}

func (stub *refreshArtifactStub) BuildSubscription(_ context.Context, _ string, raw []byte, nodes []domain.Node) (ArtifactMetadata, error) {
	stub.buildCalls++
	stub.raw = append([]byte(nil), raw...)
	stub.nodes = append([]domain.Node(nil), nodes...)
	return ArtifactMetadata{}, stub.buildErr
}
func (*refreshArtifactStub) Select(context.Context, string) (ConfigStatus, error) {
	return ConfigStatus{}, nil
}
func (*refreshArtifactStub) DeleteSubscriptionArtifacts(string) error { return nil }
func (*refreshArtifactStub) Artifact(string) (ArtifactMetadata, string, error) {
	return ArtifactMetadata{}, "", ErrConfigNotReady
}

func newRefreshTestService(transport http.RoundTripper, artifacts SubscriptionArtifactManager) (*SubscriptionService, *refreshStoreStub) {
	now := time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC)
	store := &refreshStoreStub{
		item: domain.Subscription{
			ID:                refreshTestSubscriptionID,
			DisplayName:       "Synthetic Subscription",
			URLPlaintext:      "https://subscription.example/api?token=private-url-marker",
			URLHint:           "subscription.example",
			Enabled:           true,
			LastRefreshStatus: "success",
			NodeCount:         1,
		},
		selected: refreshTestSubscriptionID,
	}
	service := NewSubscriptionService(store, nil)
	service.Artifacts = artifacts
	service.HTTPClient = &http.Client{Transport: transport}
	service.Now = func() time.Time { return now }
	return service, store
}

func subscriptionResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestParseSubscriptionNodesKeepsOnlyProxyEntriesAndClassifiesCountry(t *testing.T) {
	body := []byte("proxies:\n  - name: Tokyo A\n    type: ss\n    server: secret.example\n    password: do-not-store\n  - name: US B\n    type: vless\n    uuid: secret\n")
	nodes, err := parseSubscriptionNodes("subscription_abcdefghijklmnopqrstuv", body)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 || nodes[0].DisplayName != "Tokyo A" || nodes[1].DisplayName != "US B" {
		t.Fatalf("nodes = %#v", nodes)
	}
	for _, node := range nodes {
		if node.Kind != "ss" && node.Kind != "vless" {
			t.Fatalf("node = %#v", node)
		}
		if node.ProxyYAML == "" || strings.Contains(node.ProxyYAML, "listeners:") {
			t.Fatalf("node config = %#v", node)
		}
	}
	if nodes[1].CountryCode != "US" || nodes[1].CountryName != "美国" {
		t.Fatalf("country = %#v", nodes[1])
	}
}

func TestParseSubscriptionNodesSupportsBase64URIListAndRejectsEmpty(t *testing.T) {
	raw := "trojan://secret@example.com:443#Tokyo%20Trojan\nss://secret@example.com:443#US%20SS\n"
	nodes, err := parseSubscriptionNodes("subscription_abcdefghijklmnopqrstuv", []byte(base64.StdEncoding.EncodeToString([]byte(raw))))
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 || nodes[0].DisplayName != "Tokyo Trojan" || nodes[1].DisplayName != "US SS" {
		t.Fatalf("nodes = %#v", nodes)
	}
	if _, err := parseSubscriptionNodes("subscription_abcdefghijklmnopqrstuv", []byte("not a subscription")); err == nil {
		t.Fatal("invalid subscription accepted")
	}
}

func TestValidateSubscriptionInputRejectsPrivateTargets(t *testing.T) {
	for _, raw := range []string{"http://example.com/sub", "https://localhost/sub", "https://127.0.0.1/sub", "https://192.168.50.1/sub", "https://[::1]/sub"} {
		if _, _, err := validateSubscriptionInput("test", raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	if _, parsed, err := validateSubscriptionInput("test", "https://subscription.example/path?token=secret"); err != nil || parsed.Hostname() != "subscription.example" {
		t.Fatalf("public URL = %v, %v", parsed, err)
	}
}

func TestSubscriptionIdentifiersSeparateStableIdentityFromDefaultDisplayName(t *testing.T) {
	id, err := newSubscriptionID()
	if err != nil {
		t.Fatal(err)
	}
	if !subscriptionIDPattern.MatchString(id) {
		t.Fatalf("id=%q", id)
	}
	name := defaultSubscriptionDisplayName(id)
	if !subscriptionDefaultNamePattern.MatchString(name) || name != defaultSubscriptionDisplayName(id) {
		t.Fatalf("default display name=%q", name)
	}
}

func TestSubscriptionRefreshNegotiatesGeneratableMihomoYAML(t *testing.T) {
	const yamlFixture = "proxies:\n  - name: US Synthetic\n    type: ss\n    server: proxy.example\n    port: 443\n    cipher: aes-128-gcm\n    password: synthetic-secret\n"
	uriFixture := base64.StdEncoding.EncodeToString([]byte("trojan://synthetic-secret@proxy.example:443#US%20Synthetic\n"))
	var userAgent, accept string
	service, store := newRefreshTestService(subscriptionRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		userAgent, accept = request.UserAgent(), request.Header.Get("Accept")
		if userAgent != "clash.meta" {
			return subscriptionResponse(http.StatusOK, uriFixture), nil
		}
		return subscriptionResponse(http.StatusOK, yamlFixture), nil
	}), nil)
	artifacts := &ConfigManager{
		Root:  t.TempDir(),
		Store: store,
		Core:  coreStatusStub{CoreStatus{Installed: true, Version: "v1.19.29", BinaryPath: "/installed/mihomo"}},
		Run: func(context.Context, string, ...string) ([]byte, error) {
			return []byte("configuration test is successful"), nil
		},
		Now:               service.Now,
		ControllerAddress: "127.0.0.1:19090",
	}
	service.Artifacts = artifacts

	view, nodes, err := service.Refresh(context.Background(), refreshTestSubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	if subscriptionUserAgent != "clash.meta" {
		t.Fatalf("subscription User-Agent constant = %q", subscriptionUserAgent)
	}
	if userAgent != subscriptionUserAgent {
		t.Fatalf("User-Agent = %q", userAgent)
	}
	if accept != "application/yaml,text/yaml,text/plain,application/octet-stream" {
		t.Fatalf("Accept = %q", accept)
	}
	if len(nodes) != 1 || len(store.replaced) != 1 || store.replaceStatus != "success" || store.replaceErrorCode != "" {
		t.Fatalf("nodes=%d replaced=%d status=%q error=%q", len(nodes), len(store.replaced), store.replaceStatus, store.replaceErrorCode)
	}
	if view.LastRefreshStatus != "success" || view.LastErrorCode != "" || view.NodeCount != 1 {
		t.Fatalf("view = %#v", view)
	}
	if len(nodes) != 1 || nodes[0].ProxyYAML == "" {
		t.Fatalf("downloaded nodes are not usable for config generation: %#v", nodes)
	}
	if metadata, _, artifactErr := artifacts.Artifact(refreshTestSubscriptionID); artifactErr != nil || metadata.ConfigSHA256 == "" {
		t.Fatalf("artifact metadata=%#v err=%v", metadata, artifactErr)
	}
}

func TestSubscriptionRefreshReturnsCredentialSafeTypedFetchErrors(t *testing.T) {
	tests := []struct {
		name      string
		transport http.RoundTripper
		markers   []string
	}{
		{
			name: "upstream rejected",
			transport: subscriptionRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return subscriptionResponse(http.StatusForbidden, "provider-body-private-marker"), nil
			}),
			markers: []string{"provider-body-private-marker"},
		},
		{
			name: "transport failed",
			transport: subscriptionRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("transport-private-marker")
			}),
			markers: []string{"transport-private-marker"},
		},
		{
			name: "transport timeout",
			transport: subscriptionRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, context.DeadlineExceeded
			}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, store := newRefreshTestService(test.transport, &refreshArtifactStub{})
			_, _, err := service.Refresh(context.Background(), refreshTestSubscriptionID)
			var refreshError *SubscriptionRefreshError
			if !errors.As(err, &refreshError) || refreshError.Code != "SUBSCRIPTION_FETCH_FAILED" {
				t.Fatalf("error = %#v", err)
			}
			if err.Error() != "Mihomo subscription refresh failed: SUBSCRIPTION_FETCH_FAILED" {
				t.Fatalf("error text = %q", err)
			}
			for _, marker := range append(test.markers, "private-url-marker") {
				if strings.Contains(err.Error(), marker) {
					t.Fatalf("error leaked private marker %q", marker)
				}
			}
			if store.markedCode != "SUBSCRIPTION_FETCH_FAILED" || len(store.replaced) != 0 {
				t.Fatalf("marked=%q replaced=%d", store.markedCode, len(store.replaced))
			}
		})
	}
}

func TestSubscriptionRefreshReturnsTypedStageErrors(t *testing.T) {
	const validFixture = "proxies:\n  - name: US Synthetic\n    type: ss\n    server: proxy.example\n    port: 443\n    cipher: aes-128-gcm\n    password: synthetic-secret\n"
	tests := []struct {
		name      string
		body      string
		artifacts SubscriptionArtifactManager
		wantCode  string
	}{
		{name: "parse", body: "not a subscription", artifacts: &refreshArtifactStub{}, wantCode: "SUBSCRIPTION_PARSE_FAILED"},
		{name: "config unavailable", body: validFixture, wantCode: "SUBSCRIPTION_CONFIG_UNAVAILABLE"},
		{name: "config generation", body: validFixture, artifacts: &refreshArtifactStub{buildErr: errors.New("artifact-private-marker")}, wantCode: "SUBSCRIPTION_CONFIG_GENERATION_FAILED"},
		{name: "config validation", body: validFixture, artifacts: &refreshArtifactStub{buildErr: errors.Join(ErrConfigValidationFailed, errors.New("artifact-private-marker"))}, wantCode: "SUBSCRIPTION_CONFIG_VALIDATION_FAILED"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, store := newRefreshTestService(subscriptionRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return subscriptionResponse(http.StatusOK, test.body), nil
			}), test.artifacts)
			_, _, err := service.Refresh(context.Background(), refreshTestSubscriptionID)
			var refreshError *SubscriptionRefreshError
			if !errors.As(err, &refreshError) || refreshError.Code != test.wantCode || store.markedCode != test.wantCode {
				t.Fatalf("error=%#v marked=%q", err, store.markedCode)
			}
			if strings.Contains(err.Error(), "private-marker") {
				t.Fatalf("error leaked artifact detail: %q", err)
			}
		})
	}
}

func TestSubscriptionRefreshReturnsFailurePersistenceError(t *testing.T) {
	persistenceError := errors.New("persist refresh failure")
	service, store := newRefreshTestService(subscriptionRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return subscriptionResponse(http.StatusForbidden, ""), nil
	}), &refreshArtifactStub{})
	store.markErr = persistenceError

	_, _, err := service.Refresh(context.Background(), refreshTestSubscriptionID)
	var refreshError *SubscriptionRefreshError
	if !errors.Is(err, persistenceError) || errors.As(err, &refreshError) {
		t.Fatalf("error = %#v", err)
	}
}
