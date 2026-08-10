package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/api/openapi"
	authapp "github.com/leonfox28/simplus/internal/application/auth"
	callapp "github.com/leonfox28/simplus/internal/application/calls"
	contactapp "github.com/leonfox28/simplus/internal/application/contacts"
	"github.com/leonfox28/simplus/internal/application/health"
	"github.com/leonfox28/simplus/internal/application/inventory"
	lineegressapp "github.com/leonfox28/simplus/internal/application/lineegress"
	messageapp "github.com/leonfox28/simplus/internal/application/messaging"
	"github.com/leonfox28/simplus/internal/application/realtime"
	setupapp "github.com/leonfox28/simplus/internal/application/setup"
	"github.com/leonfox28/simplus/internal/domain/hardware"
	linedomain "github.com/leonfox28/simplus/internal/domain/line"
	modemdomain "github.com/leonfox28/simplus/internal/domain/modem"
	vowifidomain "github.com/leonfox28/simplus/internal/domain/vowifi"
	"github.com/leonfox28/simplus/internal/security/password"
	sqlitestore "github.com/leonfox28/simplus/internal/storage/sqlite"
)

type failingStateStore struct{}

func (failingStateStore) InstallationState(context.Context) (string, error) {
	return "", errors.New("storage unavailable")
}

type fixedStateStore string

func (store fixedStateStore) InstallationState(context.Context) (string, error) {
	return string(store), nil
}

type testLineEgressManager struct {
	items   []lineegressapp.View
	lineID  string
	mode    string
	country string
}

type testVoWiFiManager struct {
	state       vowifidomain.State
	activated   string
	deactivated string
}

type testManagedModemManager struct {
	items           []modemdomain.View
	candidates      []modemdomain.Candidate
	added           string
	rfModemID       string
	rfEnabled       bool
	imei            string
	identityModemID string
}

func (manager *testManagedModemManager) ReadEquipmentIdentity(_ context.Context, modemID string) (string, error) {
	manager.identityModemID = modemID
	return manager.imei, nil
}

type testManagedLineManager struct {
	items       []linedomain.View
	candidates  []linedomain.Candidate
	addedID     string
	addedName   string
	updatedID   string
	updatedName string
}

func (manager *testManagedLineManager) List(context.Context) ([]linedomain.View, error) {
	return append([]linedomain.View(nil), manager.items...), nil
}

func (manager *testManagedLineManager) Candidates(context.Context) ([]linedomain.Candidate, error) {
	return append([]linedomain.Candidate(nil), manager.candidates...), nil
}

func (manager *testManagedLineManager) Add(_ context.Context, candidateID, displayName string) (linedomain.View, error) {
	manager.addedID, manager.addedName = candidateID, displayName
	return manager.items[0], nil
}

func (manager *testManagedLineManager) Update(_ context.Context, lineID, displayName string) (linedomain.View, error) {
	manager.updatedID, manager.updatedName = lineID, displayName
	result := manager.items[0]
	result.DisplayName = displayName
	return result, nil
}

func (manager *testManagedModemManager) SetRFState(_ context.Context, modemID string, enabled bool) (modemdomain.View, error) {
	manager.rfModemID, manager.rfEnabled = modemID, enabled
	result := manager.items[0]
	result.RFState = modemdomain.RFStateOff
	if enabled {
		result.RFState = modemdomain.RFStateOn
	}
	return result, nil
}

func (manager *testManagedModemManager) List(context.Context) ([]modemdomain.View, error) {
	return append([]modemdomain.View(nil), manager.items...), nil
}

func (manager *testManagedModemManager) Candidates(context.Context) ([]modemdomain.Candidate, error) {
	return append([]modemdomain.Candidate(nil), manager.candidates...), nil
}

func (manager *testManagedModemManager) Add(_ context.Context, candidateID string) (modemdomain.View, error) {
	manager.added = candidateID
	return manager.items[0], nil
}

func (manager *testVoWiFiManager) List(context.Context) ([]vowifidomain.State, error) {
	return []vowifidomain.State{manager.state}, nil
}

func (manager *testVoWiFiManager) Activate(_ context.Context, lineID string) (vowifidomain.State, error) {
	manager.activated = lineID
	manager.state.DesiredActive = true
	return manager.state, nil
}

func (manager *testVoWiFiManager) Deactivate(_ context.Context, lineID string) (vowifidomain.State, error) {
	manager.deactivated = lineID
	manager.state.DesiredActive, manager.state.Online = false, false
	manager.state.State, manager.state.Stage = "stopped", ""
	return manager.state, nil
}

func (manager *testLineEgressManager) List(context.Context) ([]lineegressapp.View, error) {
	return append([]lineegressapp.View(nil), manager.items...), nil
}

func (manager *testLineEgressManager) Put(_ context.Context, lineID, mode, country string) (lineegressapp.View, error) {
	manager.lineID, manager.mode, manager.country = lineID, mode, country
	return lineegressapp.View{LineID: lineID, Mode: mode, CountryCode: country, CountryName: "英国", ListenerPort: 20157, Ready: true, ReadinessReason: "READY"}, nil
}

type acceptingAuthenticator struct{}

func (acceptingAuthenticator) Login(context.Context, string, string) (authapp.LoginResult, error) {
	return authapp.LoginResult{}, nil
}
func (acceptingAuthenticator) Authenticate(context.Context, string, string, bool) (authapp.Session, error) {
	return authapp.Session{User: authapp.User{Username: "admin", Locale: "en-US"}, ExpiresAt: time.Now().Add(time.Hour)}, nil
}
func (acceptingAuthenticator) Logout(context.Context, string, string) error { return nil }

type expiringAuthenticator struct{ calls atomic.Int32 }

func (*expiringAuthenticator) Login(context.Context, string, string) (authapp.LoginResult, error) {
	return authapp.LoginResult{}, nil
}
func (authenticator *expiringAuthenticator) Authenticate(context.Context, string, string, bool) (authapp.Session, error) {
	if authenticator.calls.Add(1) > 1 {
		return authapp.Session{}, authapp.ErrUnauthorized
	}
	return authapp.Session{User: authapp.User{Username: "admin", Locale: "en-US"}, ExpiresAt: time.Now().Add(time.Hour)}, nil
}
func (*expiringAuthenticator) Logout(context.Context, string, string) error { return nil }

func withTestAdministratorSession(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie(adminSessionCookieName); err != nil {
			r.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: strings.Repeat("a", 43)})
		}
		if _, err := r.Cookie(csrfCookieName); err != nil {
			r.AddCookie(&http.Cookie{Name: csrfCookieName, Value: strings.Repeat("b", 43)})
		}
		if r.Header.Get(csrfHeaderName) == "" {
			r.Header.Set(csrfHeaderName, strings.Repeat("b", 43))
		}
		handler.ServeHTTP(w, r)
	})
}

func newTestInventory() *inventory.Service {
	return inventory.NewSimulator()
}

const testBusinessLineID = "line_AQEBAQEBAQEBAQEBAQEBAQ"

type testBusinessLineSource struct{ source *inventory.Service }

func (source testBusinessLineSource) Topology(ctx context.Context) (inventory.Topology, error) {
	topology, err := source.source.Topology(ctx)
	if err != nil {
		return inventory.Topology{}, err
	}
	for index := range topology.Lines {
		if topology.Lines[index].ID == "simulator-line-1" {
			topology.Lines[index].RuntimeLineID = topology.Lines[index].ID
			topology.Lines[index].ID = testBusinessLineID
		}
	}
	return topology, nil
}

func newTestHandler(store health.StateStore, inventoryService *inventory.Service) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return withTestAdministratorSession(Router(New(health.New(store, "simulator"), setupapp.New(store, nil), inventoryService, logger, acceptingAuthenticator{}, nil)))
}

func newAuthorizedTestHandler(stores *sqlitestore.Set) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return withTestAdministratorSession(Router(New(
		health.New(stores, "simulator"),
		setupapp.New(stores, stores),
		inventory.NewSimulator(),
		logger,
		acceptingAuthenticator{},
		nil,
	)))
}

func TestLineEgressHTTPContractUsesTypedCountryBinding(t *testing.T) {
	manager := &testLineEgressManager{items: []lineegressapp.View{{
		LineID: testBusinessLineID, Mode: "mihomo-country", CountryCode: "GB", CountryName: "英国",
		ListenerPort: 20157, Ready: true, ReadinessReason: "READY",
	}}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := New(health.New(fixedStateStore("ready"), "simulator"), setupapp.New(fixedStateStore("ready"), nil), newTestInventory(), logger, acceptingAuthenticator{}, nil)
	hub := realtime.NewHub()
	subscription := hub.Subscribe()
	defer subscription.Close()
	<-subscription.C
	handler := withTestAdministratorSession(Router(WithRealtime(WithLineEgress(server, manager), hub)))

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/line-egress-bindings", nil)
	listRequest.Host = "127.0.0.1:8080"
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"countryCode":"GB"`) {
		t.Fatalf("list status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}

	putRequest := httptest.NewRequest(http.MethodPut, "/api/v1/lines/"+testBusinessLineID+"/egress", strings.NewReader(`{"mode":"mihomo-country","countryCode":"GB"}`))
	putRequest.Header.Set("Content-Type", "application/json")
	putRequest.Host = "127.0.0.1:8080"
	putResponse := httptest.NewRecorder()
	handler.ServeHTTP(putResponse, putRequest)
	if putResponse.Code != http.StatusOK || manager.lineID != testBusinessLineID || manager.mode != "mihomo-country" || manager.country != "GB" {
		t.Fatalf("put status=%d body=%s call=(%q,%q,%q)", putResponse.Code, putResponse.Body.String(), manager.lineID, manager.mode, manager.country)
	}
	event := <-subscription.C
	if event.Kind != realtime.KindUpdate || len(event.Topics) != 3 || event.Attention != "" {
		t.Fatalf("line egress event=%#v", event)
	}
}

func TestManagedModemHTTPContractSeparatesCandidatesAndAddedRecords(t *testing.T) {
	addedAt := time.Date(2026, 8, 5, 13, 0, 0, 0, time.UTC)
	capabilities := hardware.Capabilities{SIMAccess: true, SIMAPDU: true, HostVoWiFiAuth: true, RFControl: true}
	manager := &testManagedModemManager{
		imei: "490154203237518",
		items: []modemdomain.View{{
			ID: "modem_AQEBAQEBAQEBAQEBAQEBAQ", DisplayName: "ML307A", Model: "ML307A",
			SerialNumber: "ML307A-SERIAL-0001",
			Transport:    hardware.TransportUSB, State: modemdomain.StateOnline, Capabilities: capabilities,
			RFState: modemdomain.RFStateOff, SIMPresence: modemdomain.SIMPresencePresent, AddedAt: addedAt,
		}},
		candidates: []modemdomain.Candidate{{
			CandidateID: "agent-usb-1-1", USBAddress: "1-1", USBVendorID: "2c7c", USBProductID: "0125",
			USBSerialHint: "USB •••• 01234567", Model: "QDC507", Transport: hardware.TransportUSB,
			Support: modemdomain.SupportSupported, Addable: true, Capabilities: hardware.Capabilities{SIMAccess: true},
			Readiness: modemdomain.ReadinessReady, SIMPresence: modemdomain.SIMPresenceAbsent,
		}},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := New(health.New(fixedStateStore("ready"), "hardware"), setupapp.New(fixedStateStore("ready"), nil), newTestInventory(), logger, acceptingAuthenticator{}, nil)
	handler := withTestAdministratorSession(Router(WithManagedModems(server, manager)))

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/modems", nil)
	listRequest.Host = "127.0.0.1:8080"
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"id":"modem_AQEBAQEBAQEBAQEBAQEBAQ"`) ||
		!strings.Contains(listResponse.Body.String(), `"serialNumber":"ML307A-SERIAL-0001"`) ||
		!strings.Contains(listResponse.Body.String(), `"simPresence":"present"`) ||
		strings.Contains(listResponse.Body.String(), "hardwareDeviceId") {
		t.Fatalf("list status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}

	scanRequest := httptest.NewRequest(http.MethodGet, "/api/v1/modem-candidates", nil)
	scanRequest.Host = "127.0.0.1:8080"
	scanResponse := httptest.NewRecorder()
	handler.ServeHTTP(scanResponse, scanRequest)
	if scanResponse.Code != http.StatusOK || !strings.Contains(scanResponse.Body.String(), `"candidateId":"agent-usb-1-1"`) ||
		!strings.Contains(scanResponse.Body.String(), `"usbAddress":"1-1"`) ||
		!strings.Contains(scanResponse.Body.String(), `"vendorId":"2c7c"`) ||
		!strings.Contains(scanResponse.Body.String(), `"productId":"0125"`) ||
		!strings.Contains(scanResponse.Body.String(), `"usbSerialHint":"USB •••• 01234567"`) ||
		!strings.Contains(scanResponse.Body.String(), `"supportStatus":"supported"`) ||
		!strings.Contains(scanResponse.Body.String(), `"readinessReason":"READY"`) ||
		!strings.Contains(scanResponse.Body.String(), `"simPresence":"absent"`) {
		t.Fatalf("scan status=%d body=%s", scanResponse.Code, scanResponse.Body.String())
	}

	addRequest := httptest.NewRequest(http.MethodPost, "/api/v1/modems", strings.NewReader(`{"candidateId":"agent-usb-1-1"}`))
	addRequest.Host = "127.0.0.1:8080"
	addRequest.Header.Set("Content-Type", "application/json")
	addResponse := httptest.NewRecorder()
	handler.ServeHTTP(addResponse, addRequest)
	if addResponse.Code != http.StatusCreated || manager.added != "agent-usb-1-1" || addResponse.Header().Get("Location") != "/api/v1/modems/modem_AQEBAQEBAQEBAQEBAQEBAQ" {
		t.Fatalf("add status=%d body=%s candidate=%q location=%q", addResponse.Code, addResponse.Body.String(), manager.added, addResponse.Header().Get("Location"))
	}

	rfRequest := httptest.NewRequest(http.MethodPut, "/api/v1/modems/modem_AQEBAQEBAQEBAQEBAQEBAQ/rf-state", strings.NewReader(`{"enabled":true}`))
	rfRequest.Host = "127.0.0.1:8080"
	rfRequest.Header.Set("Content-Type", "application/json")
	rfResponse := httptest.NewRecorder()
	handler.ServeHTTP(rfResponse, rfRequest)
	if rfResponse.Code != http.StatusOK || manager.rfModemID != "modem_AQEBAQEBAQEBAQEBAQEBAQ" || !manager.rfEnabled || !strings.Contains(rfResponse.Body.String(), `"rfState":"on"`) {
		t.Fatalf("RF status=%d body=%s call=(%q,%v)", rfResponse.Code, rfResponse.Body.String(), manager.rfModemID, manager.rfEnabled)
	}

	identityRequest := httptest.NewRequest(http.MethodPost, "/api/v1/modems/modem_AQEBAQEBAQEBAQEBAQEBAQ/equipment-identity", nil)
	identityRequest.Host = "127.0.0.1:8080"
	identityResponse := httptest.NewRecorder()
	handler.ServeHTTP(identityResponse, identityRequest)
	if identityResponse.Code != http.StatusOK || manager.identityModemID != "modem_AQEBAQEBAQEBAQEBAQEBAQ" ||
		identityResponse.Header().Get("Cache-Control") != "no-store" || identityResponse.Body.String() != `{"imei":"490154203237518"}`+"\n" {
		t.Fatalf("identity status=%d body=%s modem=%q cache=%q", identityResponse.Code, identityResponse.Body.String(), manager.identityModemID, identityResponse.Header().Get("Cache-Control"))
	}
}

func TestManagedLineHTTPContractUsesStableIdentityAndOpaqueCandidates(t *testing.T) {
	createdAt := time.Date(2026, 8, 5, 15, 0, 0, 0, time.UTC)
	modemID := "modem_AQEBAQEBAQEBAQEBAQEBAQ"
	candidateID := "line-candidate-0123456789abcdef0123456789abcdef"
	capabilities := hardware.Capabilities{SIMAccess: true, SMS: true, HostVoWiFiAuth: true}
	manager := &testManagedLineManager{
		items: []linedomain.View{{
			ID: testBusinessLineID, DisplayName: "VOXI primary", ManagedModemID: modemID,
			ManagedModemDisplayName: "ML307A", ManagedModemModel: "ML307A-DSLN", ManagedModemSerialNumber: "ML307A-SERIAL-0001",
			SubscriptionDisplayHint: "ICCID •••• 5553", State: linedomain.StateReady,
			Capabilities: capabilities, CreatedAt: createdAt,
		}},
		candidates: []linedomain.Candidate{{
			CandidateID: candidateID, ManagedModemID: modemID, ManagedModemDisplayName: "ML307A",
			ManagedModemModel: "ML307A-DSLN", ManagedModemSerialNumber: "ML307A-SERIAL-0001",
			SubscriptionDisplayHint: "ICCID •••• 5553", HomeOperatorName: "VOXI", HomeOperatorCode: "234-15",
			SIMPresence:  hardware.SlotPresent,
			Capabilities: capabilities, Addable: true, Readiness: linedomain.CandidateReady,
		}},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := New(health.New(fixedStateStore("ready"), "hardware"), setupapp.New(fixedStateStore("ready"), nil), newTestInventory(), logger, acceptingAuthenticator{}, nil)
	handler := withTestAdministratorSession(Router(WithManagedLines(server, manager)))

	request := func(method, target, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, target, strings.NewReader(body))
		request.Host = "127.0.0.1:8080"
		if body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	listed := request(http.MethodGet, "/api/v1/lines", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"id":"`+testBusinessLineID+`"`) ||
		strings.Contains(listed.Body.String(), "runtimeLineId") || strings.Contains(listed.Body.String(), "physicalDeviceId") ||
		strings.Contains(listed.Body.String(), "subscriptionIdentityFingerprint") || strings.Contains(listed.Body.String(), "agent-line-") ||
		strings.Contains(listed.Body.String(), "/dev/") {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}
	scanned := request(http.MethodGet, "/api/v1/line-candidates", "")
	if scanned.Code != http.StatusOK || !strings.Contains(scanned.Body.String(), `"candidateId":"`+candidateID+`"`) ||
		!strings.Contains(scanned.Body.String(), `"managedModemId":"`+modemID+`"`) ||
		!strings.Contains(scanned.Body.String(), `"homeOperatorName":"VOXI"`) || !strings.Contains(scanned.Body.String(), `"homeOperatorCode":"234-15"`) ||
		strings.Contains(scanned.Body.String(), "physicalDeviceId") ||
		strings.Contains(scanned.Body.String(), "subscriptionIdentityFingerprint") || strings.Contains(strings.ToLower(scanned.Body.String()), "imsi") ||
		strings.Contains(scanned.Body.String(), "/dev/") {
		t.Fatalf("scan status=%d body=%s", scanned.Code, scanned.Body.String())
	}
	added := request(http.MethodPost, "/api/v1/lines", `{"candidateId":"`+candidateID+`","displayName":"VOXI primary"}`)
	if added.Code != http.StatusCreated || manager.addedID != candidateID || manager.addedName != "VOXI primary" ||
		added.Header().Get("Location") != "/api/v1/lines/"+testBusinessLineID {
		t.Fatalf("add status=%d body=%s call=(%q,%q)", added.Code, added.Body.String(), manager.addedID, manager.addedName)
	}
	updated := request(http.MethodPut, "/api/v1/lines/"+testBusinessLineID, `{"displayName":"Backup"}`)
	if updated.Code != http.StatusOK || manager.updatedID != testBusinessLineID || manager.updatedName != "Backup" {
		t.Fatalf("update status=%d body=%s call=(%q,%q)", updated.Code, updated.Body.String(), manager.updatedID, manager.updatedName)
	}
	legacyCreate := request(http.MethodPost, "/api/v1/lines", `{"candidateId":"`+candidateID+`","displayName":"Legacy","accessMode":"host-vowifi-only"}`)
	if legacyCreate.Code != http.StatusBadRequest {
		t.Fatalf("legacy create status=%d body=%s", legacyCreate.Code, legacyCreate.Body.String())
	}
	rebind := request(http.MethodPut, "/api/v1/lines/"+testBusinessLineID, `{"displayName":"Rebind","managedModemId":"`+modemID+`"}`)
	if rebind.Code != http.StatusBadRequest || manager.updatedName != "Backup" {
		t.Fatalf("rebind status=%d body=%s update=%q", rebind.Code, rebind.Body.String(), manager.updatedName)
	}
}

func TestVoWiFiHTTPContractListsAndMutatesOnlyTypedState(t *testing.T) {
	lineID := testBusinessLineID
	manager := &testVoWiFiManager{state: vowifidomain.State{
		LineID: lineID, DesiredActive: true, Eligible: true, ReadinessCode: "READY",
		State: "online", Stage: "REGISTERED", Online: true, EgressMode: "mihomo-country",
		CountryCode: "GB", CountryName: "英国", Attempt: 1,
		RegisteredAt:  time.Date(2026, 8, 5, 5, 7, 41, 0, time.FixedZone("CST", 8*60*60)),
		NextRefreshAt: time.Date(2026, 8, 5, 5, 32, 41, 0, time.FixedZone("CST", 8*60*60)),
		PhoneNumber:   "+447700900123",
	}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := New(health.New(fixedStateStore("ready"), "hardware"), setupapp.New(fixedStateStore("ready"), nil), newTestInventory(), logger, acceptingAuthenticator{}, nil)
	handler := withTestAdministratorSession(Router(WithVoWiFi(server, manager)))

	request := httptest.NewRequest(http.MethodGet, "/api/v1/vowifi-lines", nil)
	request.Host = "127.0.0.1:8080"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"online"`) ||
		!strings.Contains(response.Body.String(), `"phoneNumber":"+447700900123"`) ||
		strings.Contains(response.Body.String(), "pid") || strings.Contains(response.Body.String(), "pcscf") || strings.Contains(response.Body.String(), "innerAddress") {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}

	post := func(action string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/vowifi-lines/"+lineID+"/"+action, nil)
		request.Host = "127.0.0.1:8080"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	if activated := post("activate"); activated.Code != http.StatusAccepted || manager.activated != lineID {
		t.Fatalf("activate status=%d body=%s line=%q", activated.Code, activated.Body.String(), manager.activated)
	}
	if deactivated := post("deactivate"); deactivated.Code != http.StatusOK || manager.deactivated != lineID || !strings.Contains(deactivated.Body.String(), `"state":"stopped"`) {
		t.Fatalf("deactivate status=%d body=%s line=%q", deactivated.Code, deactivated.Body.String(), manager.deactivated)
	}
}

func TestVoWiFiActivationRequiresCSRF(t *testing.T) {
	lineID := testBusinessLineID
	manager := &testVoWiFiManager{state: vowifidomain.State{LineID: lineID}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := New(health.New(fixedStateStore("ready"), "hardware"), setupapp.New(fixedStateStore("ready"), nil), newTestInventory(), logger, acceptingAuthenticator{}, nil)
	handler := Router(WithVoWiFi(server, manager))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/vowifi-lines/"+lineID+"/activate", nil)
	request.Host = "127.0.0.1:8080"
	request.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: strings.Repeat("a", 43)})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || manager.activated != "" {
		t.Fatalf("status=%d body=%s activated=%q", response.Code, response.Body.String(), manager.activated)
	}
}

func TestSystemHealthStartsFailClosedAndUninitialized(t *testing.T) {
	stores, err := sqlitestore.OpenSet(context.Background(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()

	handler := newTestHandler(stores, inventory.NewSimulator())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/system/health", nil)
	request.Host = "127.0.0.1:8080"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if got := response.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q", got)
	}
	if got := response.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q", got)
	}
	var body openapi.HealthResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.InstallationState != openapi.InstallationState("uninitialized") {
		t.Fatalf("installationState = %q", body.InstallationState)
	}
	if body.DatabaseCount != 5 {
		t.Fatalf("databaseCount = %d", body.DatabaseCount)
	}
}

func TestSetupStatusExposesBootstrapBoundary(t *testing.T) {
	stores, err := sqlitestore.OpenSet(context.Background(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()
	handler := newAuthorizedTestHandler(stores)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/setup/status", nil)
	request.Host = "127.0.0.1:8080"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body openapi.SetupStatusResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.InstallationState != openapi.InstallationStateUninitialized || body.Phase != openapi.SetupPhaseBootstrapRequired {
		t.Fatalf("setup status = %#v", body)
	}
	if !body.SetupRequired || body.BusinessApiAvailable || body.BootstrapGenerationAvailable {
		t.Fatalf("unexpected setup boundary = %#v", body)
	}
	if len(body.SupportedFlows) != 1 || body.SupportedFlows[0] != openapi.CreateNew {
		t.Fatalf("supported flows = %#v", body.SupportedFlows)
	}
}

func TestBootstrapExchangeCreatesOneTimePersistentSetupSession(t *testing.T) {
	stores, err := sqlitestore.OpenSet(context.Background(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()
	setupService := setupapp.New(stores, stores)
	grant, err := setupService.GenerateBootstrap(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	handler := newAuthorizedTestHandler(stores)

	consume := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/setup/bootstrap", strings.NewReader(`{"bootstrapCode":"`+grant.Code+`"}`))
		request.Host = "127.0.0.1:8080"
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	response := consume()
	if response.Code != http.StatusOK {
		t.Fatalf("consume status = %d, body = %s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %#v", cookies)
	}
	cookie := cookies[0]
	if cookie.Name != setupSessionCookieName || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/api/v1/setup" {
		t.Fatalf("setup cookie = %#v", cookie)
	}
	if strings.Contains(response.Body.String(), cookie.Value) || strings.Contains(response.Body.String(), grant.Code) {
		t.Fatal("setup response exposed an opaque credential in its body")
	}
	var session openapi.SetupSessionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if !bool(session.Authorized) || session.SelectedFlow != openapi.CreateNew {
		t.Fatalf("initial session = %#v", session)
	}

	if replay := consume(); replay.Code != http.StatusUnauthorized {
		t.Fatalf("bootstrap replay status = %d, body = %s", replay.Code, replay.Body.String())
	}

	resumedRequest := httptest.NewRequest(http.MethodGet, "/api/v1/setup/session", nil)
	resumedRequest.Host = "127.0.0.1:8080"
	resumedRequest.AddCookie(cookie)
	resumedResponse := httptest.NewRecorder()
	newAuthorizedTestHandler(stores).ServeHTTP(resumedResponse, resumedRequest)
	if resumedResponse.Code != http.StatusOK {
		t.Fatalf("resume status = %d, body = %s", resumedResponse.Code, resumedResponse.Body.String())
	}
	if err := json.Unmarshal(resumedResponse.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.SelectedFlow != openapi.CreateNew {
		t.Fatalf("resumed flow response = %#v", session)
	}

	if _, err := setupService.GenerateBootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	revokedResponse := httptest.NewRecorder()
	handler.ServeHTTP(revokedResponse, resumedRequest)
	if revokedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session status = %d, body = %s", revokedResponse.Code, revokedResponse.Body.String())
	}
}

func TestSetupAdministratorRequiresSessionAndPersistsArgon2idCredential(t *testing.T) {
	stores, err := sqlitestore.OpenSet(context.Background(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()
	setupService := setupapp.New(stores, stores)
	grant, err := setupService.GenerateBootstrap(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	handler := newAuthorizedTestHandler(stores)
	bootstrapRequest := httptest.NewRequest(http.MethodPost, "/api/v1/setup/bootstrap", strings.NewReader(`{"bootstrapCode":"`+grant.Code+`"}`))
	bootstrapRequest.Host = "127.0.0.1:8080"
	bootstrapRequest.Header.Set("Content-Type", "application/json")
	bootstrapResponse := httptest.NewRecorder()
	handler.ServeHTTP(bootstrapResponse, bootstrapRequest)
	if bootstrapResponse.Code != http.StatusOK {
		t.Fatalf("bootstrap status = %d, body = %s", bootstrapResponse.Code, bootstrapResponse.Body.String())
	}
	cookie := bootstrapResponse.Result().Cookies()[0]

	const requestBody = `{"username":"Leon","password":"correct horse battery staple","passwordConfirmation":"correct horse battery staple","instanceDefaultLocale":"zh-CN"}`
	unauthorizedRequest := httptest.NewRequest(http.MethodPut, "/api/v1/setup/administrator", strings.NewReader(requestBody))
	unauthorizedRequest.Host = "127.0.0.1:8080"
	unauthorizedRequest.Header.Set("Content-Type", "application/json")
	unauthorizedResponse := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedResponse, unauthorizedRequest)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, body = %s", unauthorizedResponse.Code, unauthorizedResponse.Body.String())
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/setup/administrator", strings.NewReader(requestBody))
	request.Host = "127.0.0.1:8080"
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "correct horse") || strings.Contains(response.Body.String(), "$argon2id$") {
		t.Fatal("administrator response exposed password material")
	}
	var session openapi.SetupSessionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if !session.AdministratorConfigured || session.AdministratorUsername != "leon" || session.InstanceDefaultLocale != "zh-CN" {
		t.Fatalf("administrator session = %#v", session)
	}
	credential, err := stores.ReadAdministratorCredential(context.Background(), "leon")
	if err != nil {
		t.Fatal(err)
	}
	if !credential.Found || !strings.HasPrefix(credential.PasswordHash, "$argon2id$v1$") || strings.Contains(credential.PasswordHash, "correct horse") {
		t.Fatalf("stored credential = %#v", credential)
	}

	storageBody, err := json.Marshal(openapi.ConfigureSetupStorageRequest{RecordingsRoot: filepath.Join(stores.Root, "recordings")})
	if err != nil {
		t.Fatal(err)
	}
	storageRequest := httptest.NewRequest(http.MethodPut, "/api/v1/setup/storage", bytes.NewReader(storageBody))
	storageRequest.Host = "127.0.0.1:8080"
	storageRequest.Header.Set("Content-Type", "application/json")
	storageRequest.AddCookie(cookie)
	storageResponse := httptest.NewRecorder()
	handler.ServeHTTP(storageResponse, storageRequest)
	if storageResponse.Code != http.StatusOK {
		t.Fatalf("storage status = %d, body = %s", storageResponse.Code, storageResponse.Body.String())
	}
	if err := json.Unmarshal(storageResponse.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if !session.StorageConfigured || session.DataRoot != stores.Root || session.RecordingsRoot != filepath.Join(stores.Root, "recordings") {
		t.Fatalf("storage session = %#v", session)
	}

	httpsRequest := httptest.NewRequest(http.MethodPut, "/api/v1/setup/https", strings.NewReader(`{"mode":"loopback-only","listenHost":"127.0.0.1","listenPort":8080,"subjectAlternativeNames":[]}`))
	httpsRequest.Host = "127.0.0.1:8080"
	httpsRequest.Header.Set("Content-Type", "application/json")
	httpsRequest.AddCookie(cookie)
	httpsResponse := httptest.NewRecorder()
	handler.ServeHTTP(httpsResponse, httpsRequest)
	if httpsResponse.Code != http.StatusOK {
		t.Fatalf("HTTPS status = %d, body = %s", httpsResponse.Code, httpsResponse.Body.String())
	}
	if err := json.Unmarshal(httpsResponse.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if !session.HttpsConfigured || !session.HttpsConfirmed || session.HttpsMode != "loopback-only" || session.HttpsListenUrl != "http://127.0.0.1:8080" {
		t.Fatalf("HTTPS session = %#v", session)
	}

	setupTopologyRequest := httptest.NewRequest(http.MethodGet, "/api/v1/setup/hardware/topology", nil)
	setupTopologyRequest.Host = "127.0.0.1:8080"
	setupTopologyRequest.AddCookie(cookie)
	setupTopologyResponse := httptest.NewRecorder()
	handler.ServeHTTP(setupTopologyResponse, setupTopologyRequest)
	if setupTopologyResponse.Code != http.StatusOK {
		t.Fatalf("setup topology status = %d, body = %s", setupTopologyResponse.Code, setupTopologyResponse.Body.String())
	}
	var setupTopology openapi.HardwareTopologyResponse
	if err := json.Unmarshal(setupTopologyResponse.Body.Bytes(), &setupTopology); err != nil {
		t.Fatal(err)
	}
	if len(setupTopology.SubscriptionProfiles) != 1 || setupTopology.SubscriptionProfiles[0].Id != "simulator-profile-1" {
		t.Fatalf("setup topology = %#v", setupTopology)
	}

	hardwareRequest := httptest.NewRequest(http.MethodPost, "/api/v1/setup/hardware/confirm", nil)
	hardwareRequest.Host = "127.0.0.1:8080"
	hardwareRequest.AddCookie(cookie)
	hardwareResponse := httptest.NewRecorder()
	handler.ServeHTTP(hardwareResponse, hardwareRequest)
	if hardwareResponse.Code != http.StatusOK {
		t.Fatalf("hardware confirmation status = %d, body = %s", hardwareResponse.Code, hardwareResponse.Body.String())
	}
	if err := json.Unmarshal(hardwareResponse.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if !session.HardwareReviewed || session.HardwareDeviceCount != 1 || session.HardwareLineCount != 1 || len(session.HardwareInventoryDigest) != 64 {
		t.Fatalf("hardware session = %#v", session)
	}
	completionRequest := httptest.NewRequest(http.MethodPost, "/api/v1/setup/complete", nil)
	completionRequest.Host = "127.0.0.1:8080"
	completionRequest.AddCookie(cookie)
	completionResponse := httptest.NewRecorder()
	handler.ServeHTTP(completionResponse, completionRequest)
	if completionResponse.Code != http.StatusOK {
		t.Fatalf("setup completion status = %d, body = %s", completionResponse.Code, completionResponse.Body.String())
	}
	var completion openapi.SetupCompletionResponse
	if err := json.Unmarshal(completionResponse.Body.Bytes(), &completion); err != nil {
		t.Fatal(err)
	}
	if completion.InstallationState != openapi.SetupCompletionResponseInstallationStateReady || !bool(completion.LoginRequired) {
		t.Fatalf("completion = %#v", completion)
	}
	status, err := stores.InstallationState(context.Background())
	if err != nil || status != setupapp.InstallationReady {
		t.Fatalf("installation state after completion = %q/%v", status, err)
	}
}

func TestBusinessAPIsStayLockedUntilSetupCompletes(t *testing.T) {
	handler := newTestHandler(fixedStateStore(setupapp.InstallationUninitialized), inventory.NewSimulator())
	for _, path := range []string{"/api/v1/inventory", "/api/v1/lines"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Host = "127.0.0.1:8080"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusConflict {
			t.Errorf("GET %s status = %d, body = %s", path, response.Code, response.Body.String())
			continue
		}
		var body openapi.ApiError
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Code != "INSTANCE_NOT_INITIALIZED" || body.Retryable {
			t.Errorf("GET %s body = %#v", path, body)
		}
	}
}

func TestSetupStatusReturnsStableErrorCode(t *testing.T) {
	handler := newTestHandler(failingStateStore{}, newTestInventory())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/setup/status", nil)
	request.Host = "127.0.0.1:8080"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body openapi.ApiError
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "SETUP_STATUS_UNAVAILABLE" || !body.Retryable {
		t.Fatalf("error body = %#v", body)
	}
}

func TestAdministratorLoginSessionCSRFAndLogout(t *testing.T) {
	ctx := context.Background()
	stores, err := sqlitestore.OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()
	hash, err := password.NewDefaultHasher().Hash("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if err := stores.ConfigureInitialAdministrator(ctx, "admin", hash, "zh-CN", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := stores.Core.ExecContext(ctx, `UPDATE installation_state SET state = 'ready' WHERE singleton = 1`); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	authService := authapp.NewService(stores, stores, password.NewDefaultHasher())
	handler := Router(New(
		health.New(stores, "simulator"),
		setupapp.New(stores, stores),
		inventory.NewSimulator(),
		logger,
		authService,
		nil,
	))

	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"correct horse battery staple"}`))
	loginRequest.Host = "127.0.0.1:8080"
	loginRequest.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", loginResponse.Code, loginResponse.Body.String())
	}
	var sessionCookie, csrfCookie *http.Cookie
	for _, cookie := range loginResponse.Result().Cookies() {
		switch cookie.Name {
		case adminSessionCookieName:
			sessionCookie = cookie
		case csrfCookieName:
			csrfCookie = cookie
		}
	}
	if sessionCookie == nil || csrfCookie == nil || !sessionCookie.HttpOnly || csrfCookie.HttpOnly || sessionCookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("login cookies = %#v / %#v", sessionCookie, csrfCookie)
	}

	inventoryRequest := httptest.NewRequest(http.MethodGet, "/api/v1/inventory", nil)
	inventoryRequest.Host = "127.0.0.1:8080"
	inventoryResponse := httptest.NewRecorder()
	handler.ServeHTTP(inventoryResponse, inventoryRequest)
	if inventoryResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated inventory status = %d, body = %s", inventoryResponse.Code, inventoryResponse.Body.String())
	}
	inventoryRequest = httptest.NewRequest(http.MethodGet, "/api/v1/inventory", nil)
	inventoryRequest.Host = "127.0.0.1:8080"
	inventoryRequest.AddCookie(sessionCookie)
	inventoryResponse = httptest.NewRecorder()
	handler.ServeHTTP(inventoryResponse, inventoryRequest)
	if inventoryResponse.Code != http.StatusOK {
		t.Fatalf("authenticated inventory status = %d, body = %s", inventoryResponse.Code, inventoryResponse.Body.String())
	}

	mutationRequest := httptest.NewRequest(http.MethodPost, "/api/v1/lines", strings.NewReader(`{"candidateId":"line-candidate-0123456789abcdef0123456789abcdef","displayName":"Line"}`))
	mutationRequest.Host = "127.0.0.1:8080"
	mutationRequest.Header.Set("Content-Type", "application/json")
	mutationRequest.AddCookie(sessionCookie)
	mutationResponse := httptest.NewRecorder()
	handler.ServeHTTP(mutationResponse, mutationRequest)
	if mutationResponse.Code != http.StatusForbidden {
		t.Fatalf("mutation without CSRF status = %d, body = %s", mutationResponse.Code, mutationResponse.Body.String())
	}

	logoutRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logoutRequest.Host = "127.0.0.1:8080"
	logoutRequest.AddCookie(sessionCookie)
	logoutRequest.AddCookie(csrfCookie)
	logoutRequest.Header.Set(csrfHeaderName, csrfCookie.Value)
	logoutResponse := httptest.NewRecorder()
	handler.ServeHTTP(logoutResponse, logoutRequest)
	if logoutResponse.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, body = %s", logoutResponse.Code, logoutResponse.Body.String())
	}
}

func TestInventoryReturnsDynamicDeviceAndLineSummaries(t *testing.T) {
	handler := newTestHandler(fixedStateStore(setupapp.InstallationReady), newTestInventory())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/inventory", nil)
	request.Host = "127.0.0.1:8080"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body openapi.InventoryResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Revision) != 64 || len(body.Devices) != 1 || len(body.Lines) != 1 {
		t.Fatalf("inventory body = %#v", body)
	}
	if body.Lines[0].PhysicalDeviceId != body.Devices[0].Id || body.Lines[0].SubscriptionProfileId != "simulator-profile-1" {
		t.Fatalf("line identity = %#v, device = %#v", body.Lines[0], body.Devices[0])
	}
	if body.Lines[0].State != openapi.LineStateReady {
		t.Fatalf("simulator line = %#v", body.Lines[0])
	}
}

func TestMessageSendAndHistoryUseBusinessAuthCSRFAndIdempotency(t *testing.T) {
	ctx := context.Background()
	stores, err := sqlitestore.OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()
	if _, err := stores.Core.ExecContext(ctx, `UPDATE installation_state SET state = 'ready' WHERE singleton = 1`); err != nil {
		t.Fatal(err)
	}
	inventoryService := inventory.NewSimulator()
	const simulatorAgentInstanceID = "01234567-89ab-cdef-0123-456789abcdef"
	simulatorClient, err := agentapi.NewLocalSMSClient(simulatorAgentInstanceID, agentapi.NewDefaultSimulatorSMSBackend())
	if err != nil {
		t.Fatal(err)
	}
	simulatorGateway, err := messageapp.NewAgentSMSGateway(simulatorClient, simulatorAgentInstanceID)
	if err != nil {
		t.Fatal(err)
	}
	messagingService, err := messageapp.NewService(ctx, stores, testBusinessLineSource{source: inventoryService}, simulatorGateway, simulatorGateway)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := Router(New(
		health.New(stores, "simulator"), setupapp.New(stores, stores), inventoryService,
		logger, acceptingAuthenticator{}, messagingService,
	))
	body := `{"operationId":"operation-0123456789abcdef","lineId":"line_AQEBAQEBAQEBAQEBAQEBAQ","destination":"+8613800138000","body":"HTTP simulator message"}`

	unauthorizedRequest := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
	unauthorizedRequest.Host = "127.0.0.1:8080"
	unauthorizedRequest.Header.Set("Content-Type", "application/json")
	unauthorizedResponse := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedResponse, unauthorizedRequest)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, body = %s", unauthorizedResponse.Code, unauthorizedResponse.Body.String())
	}

	csrfRequest := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(body))
	csrfRequest.Host = "127.0.0.1:8080"
	csrfRequest.Header.Set("Content-Type", "application/json")
	csrfRequest.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: strings.Repeat("a", 43)})
	csrfResponse := httptest.NewRecorder()
	handler.ServeHTTP(csrfResponse, csrfRequest)
	if csrfResponse.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d, body = %s", csrfResponse.Code, csrfResponse.Body.String())
	}

	authorized := withTestAdministratorSession(handler)
	send := func(payload string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(payload))
		request.Host = "127.0.0.1:8080"
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		authorized.ServeHTTP(response, request)
		return response
	}
	created := send(body)
	if created.Code != http.StatusCreated {
		t.Fatalf("created status = %d, body = %s", created.Code, created.Body.String())
	}
	var sent openapi.SMSMessage
	if err := json.Unmarshal(created.Body.Bytes(), &sent); err != nil {
		t.Fatal(err)
	}
	if sent.Status != openapi.SMSStatus("sent") || sent.LineId != testBusinessLineID || sent.ProviderMessageId == "" {
		t.Fatalf("sent message = %#v", sent)
	}
	replayed := send(body)
	if replayed.Code != http.StatusOK {
		t.Fatalf("replay status = %d, body = %s", replayed.Code, replayed.Body.String())
	}
	var replayMessage openapi.SMSMessage
	if err := json.Unmarshal(replayed.Body.Bytes(), &replayMessage); err != nil {
		t.Fatal(err)
	}
	if replayMessage.Id != sent.Id {
		t.Fatalf("replay id = %q, created id = %q", replayMessage.Id, sent.Id)
	}

	conflict := send(strings.Replace(body, "+8613800138000", "+8613900139000", 1))
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d, body = %s", conflict.Code, conflict.Body.String())
	}
	if result, err := messagingService.SyncInbound(ctx); err != nil || result.Persisted != 1 {
		t.Fatalf("background sync result=%#v error=%v", result, err)
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/messages", nil)
	listRequest.Host = "127.0.0.1:8080"
	listResponse := httptest.NewRecorder()
	authorized.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listResponse.Code, listResponse.Body.String())
	}
	var history openapi.SMSMessageListResponse
	if err := json.Unmarshal(listResponse.Body.Bytes(), &history); err != nil {
		t.Fatal(err)
	}
	foundSent := false
	foundInbound := false
	inboundRemote := ""
	for _, message := range history.Messages {
		foundSent = foundSent || message.Id == sent.Id
		if message.Direction == openapi.SMSDirection("inbound") {
			foundInbound = true
			inboundRemote = message.RemoteAddress
		}
	}
	if len(history.Messages) != 2 || !foundSent || !foundInbound {
		t.Fatalf("message history = %#v", history)
	}
	if history.TotalCount != 2 || history.Capacity != messageapp.HistoryCapacity || history.NearCapacity {
		t.Fatalf("message history capacity = %#v", history)
	}
	remoteRequest := httptest.NewRequest(http.MethodGet, "/api/v1/messages?remoteAddress="+url.QueryEscape(inboundRemote), nil)
	remoteRequest.Host = "127.0.0.1:8080"
	remoteResponse := httptest.NewRecorder()
	authorized.ServeHTTP(remoteResponse, remoteRequest)
	var remoteHistory openapi.SMSMessageListResponse
	if remoteResponse.Code != http.StatusOK || json.Unmarshal(remoteResponse.Body.Bytes(), &remoteHistory) != nil ||
		len(remoteHistory.Messages) != 1 || remoteHistory.Messages[0].Direction != openapi.SMSDirection("inbound") || remoteHistory.ReadThroughToken == nil {
		t.Fatalf("remote history status=%d body=%s", remoteResponse.Code, remoteResponse.Body.String())
	}
	conversationRequest := httptest.NewRequest(http.MethodGet, "/api/v1/message-conversations", nil)
	conversationRequest.Host = "127.0.0.1:8080"
	conversationResponse := httptest.NewRecorder()
	authorized.ServeHTTP(conversationResponse, conversationRequest)
	var conversations openapi.SMSConversationListResponse
	if conversationResponse.Code != http.StatusOK || json.Unmarshal(conversationResponse.Body.Bytes(), &conversations) != nil ||
		len(conversations.Conversations) != 2 || conversations.ConversationTotalCount != 2 || conversations.MessageTotalCount != 2 {
		t.Fatalf("conversation list status=%d body=%s", conversationResponse.Code, conversationResponse.Body.String())
	}
	unreadFound := false
	for _, conversation := range conversations.Conversations {
		unreadFound = unreadFound || (conversation.RemoteAddress == inboundRemote && conversation.UnreadCount == 1)
	}
	if !unreadFound {
		t.Fatalf("conversation unread state=%#v", conversations.Conversations)
	}
	readBody, err := json.Marshal(openapi.MarkSMSConversationReadRequest{
		RemoteAddress: inboundRemote, ReadThroughToken: *remoteHistory.ReadThroughToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	readRequest := httptest.NewRequest(http.MethodPut, "/api/v1/message-conversations/read-state", bytes.NewReader(readBody))
	readRequest.Host = "127.0.0.1:8080"
	readRequest.Header.Set("Content-Type", "application/json")
	readResponse := httptest.NewRecorder()
	authorized.ServeHTTP(readResponse, readRequest)
	if readResponse.Code != http.StatusNoContent {
		t.Fatalf("read state status=%d body=%s", readResponse.Code, readResponse.Body.String())
	}
	idempotentReadRequest := httptest.NewRequest(http.MethodPut, "/api/v1/message-conversations/read-state", bytes.NewReader(readBody))
	idempotentReadRequest.Host = "127.0.0.1:8080"
	idempotentReadRequest.Header.Set("Content-Type", "application/json")
	idempotentReadResponse := httptest.NewRecorder()
	authorized.ServeHTTP(idempotentReadResponse, idempotentReadRequest)
	if idempotentReadResponse.Code != http.StatusNoContent {
		t.Fatalf("idempotent read state status=%d body=%s", idempotentReadResponse.Code, idempotentReadResponse.Body.String())
	}
	conversationResponse = httptest.NewRecorder()
	authorized.ServeHTTP(conversationResponse, conversationRequest.Clone(context.Background()))
	if conversationResponse.Code != http.StatusOK || json.Unmarshal(conversationResponse.Body.Bytes(), &conversations) != nil {
		t.Fatalf("conversation refresh status=%d body=%s", conversationResponse.Code, conversationResponse.Body.String())
	}
	for _, conversation := range conversations.Conversations {
		if conversation.RemoteAddress == inboundRemote && conversation.UnreadCount != 0 {
			t.Fatalf("conversation remained unread=%#v", conversation)
		}
	}
	filteredRequest := httptest.NewRequest(http.MethodGet, "/api/v1/messages?lineId="+testBusinessLineID+"&remoteAddress=%2B8613800138000", nil)
	filteredRequest.Host = "127.0.0.1:8080"
	filteredResponse := httptest.NewRecorder()
	authorized.ServeHTTP(filteredResponse, filteredRequest)
	var filtered openapi.SMSMessageListResponse
	if filteredResponse.Code != http.StatusOK || json.Unmarshal(filteredResponse.Body.Bytes(), &filtered) != nil || len(filtered.Messages) != 1 || filtered.Messages[0].Id != sent.Id {
		t.Fatalf("filtered status=%d body=%s", filteredResponse.Code, filteredResponse.Body.String())
	}
	for _, test := range []struct {
		path string
		code string
	}{
		{path: "/api/v1/messages?lineId=" + testBusinessLineID, code: "MESSAGE_FILTER_INVALID"},
		{path: "/api/v1/messages?lineId=&remoteAddress=", code: "MESSAGE_FILTER_INVALID"},
		{path: "/api/v1/messages?cursor=invalid", code: "PAGE_CURSOR_INVALID"},
		{path: "/api/v1/messages?cursor=", code: "PAGE_CURSOR_INVALID"},
		{path: "/api/v1/messages?limit=0", code: "PAGE_LIMIT_INVALID"},
		{path: "/api/v1/messages?limit=51", code: "PAGE_LIMIT_INVALID"},
		{path: "/api/v1/messages?limit=not-an-integer", code: "PAGE_LIMIT_INVALID"},
		{path: "/api/v1/message-conversations?limit=0", code: "PAGE_LIMIT_INVALID"},
		{path: "/api/v1/message-conversations?limit=51", code: "PAGE_LIMIT_INVALID"},
		{path: "/api/v1/message-conversations?limit=not-an-integer", code: "PAGE_LIMIT_INVALID"},
		{path: "/api/v1/message-conversations?cursor=", code: "PAGE_CURSOR_INVALID"},
		{path: "/api/v1/message-conversations?cursor=invalid", code: "PAGE_CURSOR_INVALID"},
	} {
		invalidRequest := httptest.NewRequest(http.MethodGet, test.path, nil)
		invalidRequest.Host = "127.0.0.1:8080"
		invalidResponse := httptest.NewRecorder()
		authorized.ServeHTTP(invalidResponse, invalidRequest)
		if invalidResponse.Code != http.StatusBadRequest {
			t.Fatalf("invalid page %s status=%d body=%s", test.path, invalidResponse.Code, invalidResponse.Body.String())
		}
		var pageError openapi.ApiError
		if err := json.Unmarshal(invalidResponse.Body.Bytes(), &pageError); err != nil || pageError.Code != test.code || pageError.Retryable {
			t.Fatalf("invalid page %s error=%#v decode=%v body=%s", test.path, pageError, err, invalidResponse.Body.String())
		}
	}
	firstPageRequest := httptest.NewRequest(http.MethodGet, "/api/v1/messages?limit=1", nil)
	firstPageRequest.Host = "127.0.0.1:8080"
	firstPageResponse := httptest.NewRecorder()
	authorized.ServeHTTP(firstPageResponse, firstPageRequest)
	var firstPage openapi.SMSMessageListResponse
	if firstPageResponse.Code != http.StatusOK || json.Unmarshal(firstPageResponse.Body.Bytes(), &firstPage) != nil || len(firstPage.Messages) != 1 || firstPage.NextCursor == nil {
		t.Fatalf("first page status=%d body=%s", firstPageResponse.Code, firstPageResponse.Body.String())
	}
	secondPageRequest := httptest.NewRequest(http.MethodGet, "/api/v1/messages?limit=1&cursor="+*firstPage.NextCursor, nil)
	secondPageRequest.Host = "127.0.0.1:8080"
	secondPageResponse := httptest.NewRecorder()
	authorized.ServeHTTP(secondPageResponse, secondPageRequest)
	var secondPage openapi.SMSMessageListResponse
	if secondPageResponse.Code != http.StatusOK || json.Unmarshal(secondPageResponse.Body.Bytes(), &secondPage) != nil || len(secondPage.Messages) != 1 || secondPage.Messages[0].Id == firstPage.Messages[0].Id || secondPage.NextCursor != nil {
		t.Fatalf("second page status=%d body=%s", secondPageResponse.Code, secondPageResponse.Body.String())
	}
	deleteInboundRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/messages/"+remoteHistory.Messages[0].Id, nil)
	deleteInboundRequest.Host = "127.0.0.1:8080"
	deleteInboundResponse := httptest.NewRecorder()
	authorized.ServeHTTP(deleteInboundResponse, deleteInboundRequest)
	if deleteInboundResponse.Code != http.StatusNoContent {
		t.Fatalf("delete inbound status=%d body=%s", deleteInboundResponse.Code, deleteInboundResponse.Body.String())
	}
	deletedBoundaryRequest := httptest.NewRequest(http.MethodPut, "/api/v1/message-conversations/read-state", bytes.NewReader(readBody))
	deletedBoundaryRequest.Host = "127.0.0.1:8080"
	deletedBoundaryRequest.Header.Set("Content-Type", "application/json")
	deletedBoundaryResponse := httptest.NewRecorder()
	authorized.ServeHTTP(deletedBoundaryResponse, deletedBoundaryRequest)
	var deletedBoundaryError openapi.ApiError
	if deletedBoundaryResponse.Code != http.StatusNotFound || json.Unmarshal(deletedBoundaryResponse.Body.Bytes(), &deletedBoundaryError) != nil ||
		deletedBoundaryError.Code != "MESSAGE_READ_BOUNDARY_NOT_FOUND" || deletedBoundaryError.Retryable {
		t.Fatalf("deleted boundary status=%d body=%s", deletedBoundaryResponse.Code, deletedBoundaryResponse.Body.String())
	}
	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/messages/"+sent.Id, nil)
	deleteRequest.Host = "127.0.0.1:8080"
	deleteResponse := httptest.NewRecorder()
	authorized.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body = %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	remaining, err := stores.CountSMS(ctx)
	if err != nil || remaining != 0 {
		t.Fatalf("remaining message count = %d error = %v", remaining, err)
	}
}

func TestContactsCRUDUsesBusinessAuthCSRFAndDurableStore(t *testing.T) {
	ctx := context.Background()
	stores, err := sqlitestore.OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()
	if _, err := stores.Core.ExecContext(ctx, `UPDATE installation_state SET state = 'ready' WHERE singleton = 1`); err != nil {
		t.Fatal(err)
	}
	contacts, err := contactapp.New(stores)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	raw := Router(New(health.New(stores, "simulator"), setupapp.New(stores, stores), inventory.NewSimulator(), logger, acceptingAuthenticator{}, nil, contacts))

	unauthorized := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/contacts", nil)
	request.Host = "127.0.0.1:8080"
	raw.ServeHTTP(unauthorized, request)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized list status = %d", unauthorized.Code)
	}

	authorized := withTestAdministratorSession(raw)
	mutate := func(method, path, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Host = "127.0.0.1:8080"
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		authorized.ServeHTTP(response, request)
		return response
	}
	created := mutate(http.MethodPost, "/api/v1/contacts", `{"displayName":"张三","phoneNumber":"+8613800138000"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", created.Code, created.Body.String())
	}
	var value openapi.Contact
	if err := json.Unmarshal(created.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	duplicate := mutate(http.MethodPost, "/api/v1/contacts", `{"displayName":"重复","phoneNumber":"+8613800138000"}`)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d body = %s", duplicate.Code, duplicate.Body.String())
	}
	updated := mutate(http.MethodPut, "/api/v1/contacts/"+value.Id, `{"displayName":"李四","phoneNumber":"13900139000"}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("update status = %d body = %s", updated.Code, updated.Body.String())
	}
	listed := mutate(http.MethodGet, "/api/v1/contacts", "")
	var list openapi.ContactListResponse
	if listed.Code != http.StatusOK || json.Unmarshal(listed.Body.Bytes(), &list) != nil || len(list.Contacts) != 1 || list.Contacts[0].DisplayName != "李四" {
		t.Fatalf("list status = %d body = %s", listed.Code, listed.Body.String())
	}
	deleted := mutate(http.MethodDelete, "/api/v1/contacts/"+value.Id, "")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body = %s", deleted.Code, deleted.Body.String())
	}
}

func TestSimulatorCallHTTPFlowRejectsEmergencyAndPersistsHistory(t *testing.T) {
	ctx := context.Background()
	stores, err := sqlitestore.OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()
	if _, err := stores.Core.ExecContext(ctx, `UPDATE installation_state SET state = 'ready' WHERE singleton = 1`); err != nil {
		t.Fatal(err)
	}
	lines := inventory.NewSimulator()
	service, err := callapp.New(ctx, stores, testBusinessLineSource{source: lines})
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := withTestAdministratorSession(Router(WithCalls(New(health.New(stores, "simulator"), setupapp.New(stores, stores), lines, logger, acceptingAuthenticator{}, nil), service)))
	post := func(path, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		request.Host = "127.0.0.1:8080"
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	forbidden := post("/api/v1/calls/dial", `{"operationId":"operation-call-unsafe01","lineId":"line_AQEBAQEBAQEBAQEBAQEBAQ","remoteAddress":"112"}`)
	if forbidden.Code != http.StatusBadRequest {
		t.Fatalf("unsafe status=%d body=%s", forbidden.Code, forbidden.Body.String())
	}
	created := post("/api/v1/calls/dial", `{"operationId":"operation-call-safe-001","lineId":"line_AQEBAQEBAQEBAQEBAQEBAQ","remoteAddress":"13800138000"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("dial status=%d body=%s", created.Code, created.Body.String())
	}
	var active openapi.Call
	if err := json.Unmarshal(created.Body.Bytes(), &active); err != nil {
		t.Fatal(err)
	}
	if active.State != openapi.CallStateActive {
		t.Fatalf("active=%#v", active)
	}
	ended := post("/api/v1/calls/"+active.Id+"/action", `{"action":"hangup"}`)
	if ended.Code != http.StatusOK {
		t.Fatalf("hangup status=%d body=%s", ended.Code, ended.Body.String())
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/calls", nil)
	request.Host = "127.0.0.1:8080"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var history openapi.CallListResponse
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &history) != nil || len(history.Calls) != 1 || history.Calls[0].State != openapi.CallStateEnded {
		t.Fatalf("history status=%d body=%s", response.Code, response.Body.String())
	}
	for _, test := range []struct {
		query string
		code  string
	}{
		{query: "?limit=0", code: "PAGE_LIMIT_INVALID"},
		{query: "?limit=51", code: "PAGE_LIMIT_INVALID"},
		{query: "?limit=not-an-integer", code: "PAGE_LIMIT_INVALID"},
		{query: "?cursor=", code: "PAGE_CURSOR_INVALID"},
		{query: "?cursor=invalid", code: "PAGE_CURSOR_INVALID"},
	} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/calls"+test.query, nil)
		request.Host = "127.0.0.1:8080"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		var pageError openapi.ApiError
		if response.Code != http.StatusBadRequest || json.Unmarshal(response.Body.Bytes(), &pageError) != nil ||
			pageError.Code != test.code || pageError.Retryable {
			t.Fatalf("invalid call page %s status=%d body=%s", test.query, response.Code, response.Body.String())
		}
	}
}

func TestHardwareTopologyReturnsRelationalResourceModel(t *testing.T) {
	handler := newTestHandler(fixedStateStore(setupapp.InstallationReady), newTestInventory())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/hardware/topology", nil)
	request.Host = "127.0.0.1:8080"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body openapi.HardwareTopologyResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Generation != 1 || len(body.Revision) != 64 || len(body.Devices) != 1 || len(body.ModemFunctions) != 1 || len(body.SimSlots) != 1 ||
		len(body.SubscriptionProfiles) != 1 || len(body.ResourceGroups) != 1 || len(body.Lines) != 1 {
		t.Fatalf("topology body = %#v", body)
	}
	profile := body.SubscriptionProfiles[0]
	line := body.Lines[0]
	if profile.State != openapi.SubscriptionProfileStateActive || line.SubscriptionProfileId != profile.Id ||
		line.ResourceGroupId != body.ResourceGroups[0].Id || line.State != openapi.LineStateReady {
		t.Fatalf("topology profile=%#v line=%#v", profile, line)
	}
}

func TestInventoryReturnsStableErrorCode(t *testing.T) {
	handler := newTestHandler(fixedStateStore(setupapp.InstallationReady), nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/inventory", nil)
	request.Host = "127.0.0.1:8080"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body openapi.ApiError
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "INVENTORY_SNAPSHOT_UNAVAILABLE" || !body.Retryable {
		t.Fatalf("error body = %#v", body)
	}
}

func TestSystemHealthReturnsStableErrorCode(t *testing.T) {
	handler := newTestHandler(failingStateStore{}, newTestInventory())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/system/health", nil)
	request.Host = "localhost:8080"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body openapi.ApiError
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "HEALTH_SNAPSHOT_UNAVAILABLE" || !body.Retryable {
		t.Fatalf("error body = %#v", body)
	}
}

func TestRecoverJSONReturnsStableErrorAndDiscardsPartialBody(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	handler := recoverJSON(logger)(timeoutJSON(time.Second)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("partial secret"))
		panic("sensitive panic payload")
	})))
	request := httptest.NewRequest(http.MethodGet, "/panic-test?secret=must-not-be-logged", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", response.Code)
	}
	var body openapi.ApiError
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "API_INTERNAL_ERROR" || !body.Retryable {
		t.Fatalf("body = %#v", body)
	}
	if bytes.Contains(response.Body.Bytes(), []byte("partial secret")) {
		t.Fatal("panic response leaked a partial body")
	}
	logged := logs.String()
	for _, expected := range []string{`"method":"GET"`, `"path":"/panic-test"`, `"stack":"goroutine`} {
		if !strings.Contains(logged, expected) {
			t.Fatalf("panic log missing %q: %s", expected, logged)
		}
	}
	for _, forbidden := range []string{"sensitive panic payload", "must-not-be-logged"} {
		if strings.Contains(logged, forbidden) {
			t.Fatalf("panic log leaked %q: %s", forbidden, logged)
		}
	}
}

func TestTimeoutJSONReturnsStableError(t *testing.T) {
	handler := timeoutJSON(time.Millisecond)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body openapi.ApiError
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "API_TIMEOUT" || !body.Retryable {
		t.Fatalf("body = %#v", body)
	}
}

func TestEventStreamAuthenticatesFlushesAndCarriesOnlyBoundedHints(t *testing.T) {
	hub := realtime.NewHub()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := WithRealtime(New(
		health.New(fixedStateStore(setupapp.InstallationReady), "simulator"),
		setupapp.New(fixedStateStore(setupapp.InstallationReady), nil),
		newTestInventory(), logger, acceptingAuthenticator{}, nil,
	), hub)
	testServer := httptest.NewServer(Router(server))
	defer testServer.Close()

	requestContext, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, testServer.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: strings.Repeat("a", 43)})
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream" ||
		response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("event headers status=%d headers=%v", response.StatusCode, response.Header)
	}
	reader := bufio.NewReader(response.Body)
	if frame := readSSEFrame(t, reader); frame != "retry: 3000\n" {
		t.Fatalf("retry frame = %q", frame)
	}
	initial := readSSEFrame(t, reader)
	if !strings.Contains(initial, "event: resync\n") || !strings.Contains(initial, `"topics"`) {
		t.Fatalf("initial frame = %q", initial)
	}

	hub.Publish([]realtime.Topic{realtime.TopicMessages}, realtime.AttentionSMSReceived)
	update := readSSEFrame(t, reader)
	for _, expected := range []string{"event: update\n", `"topics":["messages"]`, `"attention":"sms.received"`} {
		if !strings.Contains(update, expected) {
			t.Fatalf("update missing %q: %q", expected, update)
		}
	}
	for _, forbidden := range []string{"body", "remoteAddress", "phone", "/dev/", "IMSI", "+8613800138000", "rawError"} {
		if strings.Contains(update, forbidden) {
			t.Fatalf("event leaked %q: %q", forbidden, update)
		}
	}
	cancel()
}

type realtimeDeadlineWriter struct {
	header    http.Header
	body      strings.Builder
	deadlines []time.Time
	flushes   int
}

func (writer *realtimeDeadlineWriter) Header() http.Header {
	return writer.header
}

func (writer *realtimeDeadlineWriter) WriteHeader(int) {}

func (writer *realtimeDeadlineWriter) Write(value []byte) (int, error) {
	return writer.body.Write(value)
}

func (writer *realtimeDeadlineWriter) FlushError() error {
	writer.flushes++
	return nil
}

func (writer *realtimeDeadlineWriter) SetWriteDeadline(deadline time.Time) error {
	writer.deadlines = append(writer.deadlines, deadline)
	return nil
}

func TestRealtimeWriteBoundsFlushAndClearsDeadline(t *testing.T) {
	writer := &realtimeDeadlineWriter{header: make(http.Header)}
	started := time.Now()
	if err := writeAndFlushRealtime(writer, func() error {
		_, err := io.WriteString(writer, ": synthetic heartbeat\n\n")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if writer.body.String() != ": synthetic heartbeat\n\n" || writer.flushes != 1 {
		t.Fatalf("body=%q flushes=%d", writer.body.String(), writer.flushes)
	}
	if len(writer.deadlines) != 2 || writer.deadlines[0].Before(started.Add(realtimeWriteTimeout-time.Second)) ||
		!writer.deadlines[1].IsZero() {
		t.Fatalf("write deadlines=%v", writer.deadlines)
	}
}

func TestEventStreamRejectsMissingSessionAndClosesWhenSessionExpires(t *testing.T) {
	hub := realtime.NewHub()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	authenticator := &expiringAuthenticator{}
	server := WithRealtime(New(
		health.New(fixedStateStore(setupapp.InstallationReady), "simulator"),
		setupapp.New(fixedStateStore(setupapp.InstallationReady), nil),
		newTestInventory(), logger, authenticator, nil,
	), hub)
	server.realtimeHeartbeat = 5 * time.Millisecond
	handler := Router(server)
	unauthorizedRequest := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	unauthorizedRequest.Host = "127.0.0.1:8080"
	unauthorizedResponse := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedResponse, unauthorizedRequest)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized event status=%d body=%s", unauthorizedResponse.Code, unauthorizedResponse.Body.String())
	}

	testServer := httptest.NewServer(handler)
	defer testServer.Close()
	request, err := http.NewRequest(http.MethodGet, testServer.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: strings.Repeat("a", 43)})
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, readErr := io.Copy(io.Discard, response.Body)
		done <- readErr
	}()
	select {
	case readErr := <-done:
		if readErr != nil {
			t.Fatalf("expired stream read error = %v", readErr)
		}
	case <-time.After(time.Second):
		t.Fatal("event stream remained open after session revalidation failed")
	}
	_ = response.Body.Close()
}

func readSSEFrame(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	var frame strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read SSE frame: %v", err)
		}
		if line == "\n" {
			return frame.String()
		}
		frame.WriteString(line)
	}
}

func TestTimeoutJSONBoundsHandlerThatIgnoresCancellation(t *testing.T) {
	release := make(chan struct{})
	finished := make(chan struct{})
	handler := timeoutJSON(10 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		defer close(finished)
		<-release
		_, _ = w.Write([]byte("late body"))
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	started := time.Now()
	handler.ServeHTTP(response, request)
	elapsed := time.Since(started)
	close(release)
	<-finished

	if elapsed >= 250*time.Millisecond {
		t.Fatalf("timeout returned after %s", elapsed)
	}
	if response.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if bytes.Contains(response.Body.Bytes(), []byte("late body")) {
		t.Fatal("timed-out handler wrote a late response body")
	}
}

func TestTrustedLANAuthorityValidation(t *testing.T) {
	for _, authority := range []string{"localhost", "localhost.:8080", "127.0.0.1:8080", "[::1]:8080", "192.168.50.10:8080", "192.168.1.2"} {
		if !isTrustedLANAuthority(authority) {
			t.Errorf("isTrustedLANAuthority(%q) = false", authority)
		}
	}
	for _, authority := range []string{"", "attacker.example:8080", "localhost.example:8080", "127.0.0.2.example:8080", "localhost:http", "8.8.8.8:8080"} {
		if isTrustedLANAuthority(authority) {
			t.Errorf("isTrustedLANAuthority(%q) = true", authority)
		}
	}
}

func TestRouterReturnsStableRoutingErrors(t *testing.T) {
	handler := newTestHandler(fixedStateStore(setupapp.InstallationReady), newTestInventory())
	for _, test := range []struct {
		method string
		path   string
		status int
		code   string
	}{
		{method: http.MethodGet, path: "/api/v1/missing", status: http.StatusNotFound, code: "API_ROUTE_NOT_FOUND"},
		{method: http.MethodPost, path: "/api/v1/system/health", status: http.StatusMethodNotAllowed, code: "API_METHOD_NOT_ALLOWED"},
	} {
		request := httptest.NewRequest(test.method, test.path, nil)
		request.Host = "127.0.0.1:8080"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Errorf("%s %s status = %d", test.method, test.path, response.Code)
			continue
		}
		var body openapi.ApiError
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Code != test.code || body.Retryable {
			t.Errorf("%s %s body = %#v", test.method, test.path, body)
		}
	}
}

func TestRouterRejectsUntrustedHostHeader(t *testing.T) {
	handler := newTestHandler(fixedStateStore(setupapp.InstallationReady), newTestInventory())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/system/health", nil)
	request.Host = "attacker.example:8080"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusMisdirectedRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body openapi.ApiError
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "TRUSTED_LAN_HOST_REQUIRED" || body.Retryable {
		t.Fatalf("error body = %#v", body)
	}
}

func TestRequestManagementURLUsesValidatedRequestAuthority(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "http://192.168.50.10:8080/api/v1/setup/complete", nil)
	request.Host = "192.168.50.10:8080"
	if actual := requestManagementURL(request); actual != "http://192.168.50.10:8080" {
		t.Fatalf("management URL = %q", actual)
	}
	request.TLS = &tls.ConnectionState{}
	if actual := requestManagementURL(request); actual != "https://192.168.50.10:8080" {
		t.Fatalf("TLS management URL = %q", actual)
	}
}
