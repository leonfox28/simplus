package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	notificationdomain "github.com/leonfox28/simplus/internal/domain/notification"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/leonfox28/simplus/internal/api/openapi"
	authapp "github.com/leonfox28/simplus/internal/application/auth"
	callapp "github.com/leonfox28/simplus/internal/application/calls"
	"github.com/leonfox28/simplus/internal/application/health"
	"github.com/leonfox28/simplus/internal/application/inventory"
	lineegressapp "github.com/leonfox28/simplus/internal/application/lineegress"
	messageapp "github.com/leonfox28/simplus/internal/application/messaging"
	mihomoapp "github.com/leonfox28/simplus/internal/application/mihomo"
	notificationapp "github.com/leonfox28/simplus/internal/application/notification"
	"github.com/leonfox28/simplus/internal/application/realtime"
	setupapp "github.com/leonfox28/simplus/internal/application/setup"
	"github.com/leonfox28/simplus/internal/domain/call"
	"github.com/leonfox28/simplus/internal/domain/contact"
	domaineuicc "github.com/leonfox28/simplus/internal/domain/euicc"
	"github.com/leonfox28/simplus/internal/domain/hardware"
	linedomain "github.com/leonfox28/simplus/internal/domain/line"
	mihomodomain "github.com/leonfox28/simplus/internal/domain/mihomo"
	modemdomain "github.com/leonfox28/simplus/internal/domain/modem"
	vowifidomain "github.com/leonfox28/simplus/internal/domain/vowifi"
)

const (
	adminSessionCookieName = "simplus_admin_session"
	csrfCookieName         = "simplus_csrf"
	csrfHeaderName         = "X-Simplus-CSRF"
	realtimeAuthTimeout    = 3 * time.Second
	realtimeWriteTimeout   = 5 * time.Second
)

type Authenticator interface {
	Login(context.Context, string, string) (authapp.LoginResult, error)
	Authenticate(context.Context, string, string, bool) (authapp.Session, error)
	Logout(context.Context, string, string) error
}

type PasswordAuthenticator interface {
	ChangePassword(context.Context, string, string, string, string, string) error
}

type MihomoCoreManager interface {
	Status() (mihomoapp.CoreStatus, error)
	CheckLatest(context.Context) (mihomoapp.Candidate, error)
	InstallLatest(context.Context) (mihomoapp.CoreStatus, error)
}

type MihomoSubscriptionManager interface {
	List(context.Context) ([]mihomoapp.SubscriptionView, error)
	Create(context.Context, string, string, bool) (mihomoapp.SubscriptionView, error)
	Update(context.Context, string, string, string, bool) (mihomoapp.SubscriptionView, error)
	Delete(context.Context, string) error
	Refresh(context.Context, string) (mihomoapp.SubscriptionView, []mihomodomain.Node, error)
	Nodes(context.Context, string) ([]mihomodomain.Node, error)
}

type MihomoConfigManager interface {
	Status(context.Context) (mihomoapp.ConfigStatus, error)
	GenerateAndPublish(context.Context) (mihomoapp.ConfigStatus, error)
	Select(context.Context, string) (mihomoapp.ConfigStatus, error)
}
type MihomoRuntimeManager interface {
	Status(context.Context) (mihomoapp.RuntimeStatus, error)
	Start(context.Context) (mihomoapp.RuntimeStatus, error)
	Restart(context.Context) (mihomoapp.RuntimeStatus, error)
	Stop(context.Context) (mihomoapp.RuntimeStatus, error)
}
type MihomoDashboardManager interface {
	Ensure() (mihomoapp.DashboardStatus, error)
}
type NotificationManager interface {
	List(context.Context) ([]notificationapp.ChannelView, error)
	Create(context.Context, string, string, string, string, bool, []string) (notificationapp.ChannelView, error)
	Update(context.Context, string, string, string, string, string, bool, []string) (notificationapp.ChannelView, error)
	Delete(context.Context, string) error
	Test(context.Context, string) (notificationapp.ChannelView, error)
	Enqueue(context.Context, notificationdomain.Event) error
	FeishuBindingStatus() notificationapp.BindingView
	StartFeishuBinding(context.Context) (notificationapp.BindingView, error)
	CancelFeishuBinding() (notificationapp.BindingView, error)
}

type Messenger interface {
	Send(context.Context, messageapp.SendRequest) (messageapp.SendResult, error)
	ListPage(context.Context, messageapp.PageRequest) (messageapp.PageResult, error)
	ListConversationPage(context.Context, int, string) (messageapp.ConversationPageResult, error)
	MarkConversationRead(context.Context, string, string) (bool, error)
	Stats(context.Context) (messageapp.HistoryStats, error)
	Delete(context.Context, string) error
}

type ContactManager interface {
	List(context.Context) ([]contact.Contact, error)
	Create(context.Context, string, string) (contact.Contact, error)
	Update(context.Context, string, string, string) (contact.Contact, error)
	Delete(context.Context, string) error
}
type CallManager interface {
	List(context.Context, int, string) (callapp.PageResult, error)
	Dial(context.Context, string, string, string) (call.Record, bool, error)
	Incoming(context.Context, string, string, string) (call.Record, bool, error)
	Answer(context.Context, string) (call.Record, error)
	Reject(context.Context, string) (call.Record, error)
	Hangup(context.Context, string) (call.Record, error)
	DTMF(context.Context, string, string) (call.Record, error)
}
type EUICCManager interface {
	State(context.Context) (domaineuicc.State, error)
	Switch(context.Context, string) (domaineuicc.State, error)
}
type LineEgressManager interface {
	List(context.Context) ([]lineegressapp.View, error)
	Put(context.Context, string, string, string) (lineegressapp.View, error)
}

type VoWiFiManager interface {
	List(context.Context) ([]vowifidomain.State, error)
	Activate(context.Context, string) (vowifidomain.State, error)
	Deactivate(context.Context, string) (vowifidomain.State, error)
}

type ManagedModemManager interface {
	List(context.Context) ([]modemdomain.View, error)
	Candidates(context.Context) ([]modemdomain.Candidate, error)
	Add(context.Context, string) (modemdomain.View, error)
	SetRFState(context.Context, string, bool) (modemdomain.View, error)
	ReadEquipmentIdentity(context.Context, string) (string, error)
}

type ManagedLineManager interface {
	List(context.Context) ([]linedomain.View, error)
	Candidates(context.Context) ([]linedomain.Candidate, error)
	Add(context.Context, string, string) (linedomain.View, error)
	Update(context.Context, string, string) (linedomain.View, error)
}

type HealthReader interface {
	Snapshot(context.Context) (health.Snapshot, error)
}

type SetupManager interface {
	Status(context.Context) (setupapp.Status, error)
}

type InventoryReader interface {
	Snapshot(context.Context) (inventory.Snapshot, error)
	Topology(context.Context) (inventory.Topology, error)
}

type RealtimeManager interface {
	Subscribe() *realtime.Subscription
	Publish([]realtime.Topic, realtime.Attention)
}

type Server struct {
	work                sync.WaitGroup
	health              HealthReader
	setup               SetupManager
	inventory           InventoryReader
	auth                Authenticator
	messages            Messenger
	contacts            ContactManager
	calls               CallManager
	euicc               EUICCManager
	lineEgress          LineEgressManager
	vowifi              VoWiFiManager
	modems              ManagedModemManager
	lines               ManagedLineManager
	mihomoCore          MihomoCoreManager
	mihomoSubscriptions MihomoSubscriptionManager
	mihomoConfig        MihomoConfigManager
	mihomoRuntime       MihomoRuntimeManager
	mihomoDashboard     MihomoDashboardManager
	notifications       NotificationManager
	realtime            RealtimeManager
	realtimeHeartbeat   time.Duration
	logger              *slog.Logger
}

// httpDependencyMissing lets required ports retain their nil-receiver error
// behavior while optional Setup/realtime checks treat typed nil as absent.

func Router(server *Server) http.Handler {
	router := chi.NewRouter()
	router.Use(securityHeaders)
	router.Use(trustedLANHostOnly)
	router.Use(middleware.RequestID)
	router.Use(recoverJSON(server.logger))
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			server.work.Add(1)
			defer server.work.Done()
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestWorkKey{}, &server.work)))
		})
	})
	router.Use(apiTimeout)
	router.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "API_ROUTE_NOT_FOUND", Retryable: false})
	})
	router.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusMethodNotAllowed, openapi.ApiError{Code: "API_METHOD_NOT_ALLOWED", Retryable: false})
	})
	return openapi.HandlerWithOptions(server, openapi.ChiServerOptions{
		BaseRouter:       router,
		ErrorHandlerFunc: writeOpenAPIParameterError,
	})
}

func writeOpenAPIParameterError(w http.ResponseWriter, r *http.Request, err error) {
	code := "API_REQUEST_INVALID"
	var invalid *openapi.InvalidParamFormatError
	var required *openapi.RequiredParamError
	if errors.As(err, &invalid) {
		code = paginationParameterErrorCode(r, invalid.ParamName)
	} else if errors.As(err, &required) {
		code = paginationParameterErrorCode(r, required.ParamName)
	}
	writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: code, Retryable: false})
}

func paginationParameterErrorCode(r *http.Request, parameter string) string {
	if r != nil && r.Method == http.MethodGet &&
		(r.URL.Path == "/api/v1/messages" || r.URL.Path == "/api/v1/message-conversations" || r.URL.Path == "/api/v1/calls") {
		switch parameter {
		case "limit":
			return "PAGE_LIMIT_INVALID"
		case "cursor":
			return "PAGE_CURSOR_INVALID"
		case "lineId", "remoteAddress":
			return "MESSAGE_FILTER_INVALID"
		}
	}
	return "API_REQUEST_INVALID"
}

func apiTimeout(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/events" {
			next.ServeHTTP(w, r)
			return
		}
		timeout := 15 * time.Second
		if r.URL.Path == "/api/v1/mihomo/core/install" {
			timeout = 2 * time.Minute
		} else if r.URL.Path == "/api/v1/mihomo/subscriptions" && r.Method == http.MethodPost {
			timeout = 45 * time.Second
		} else if strings.HasPrefix(r.URL.Path, "/api/v1/mihomo/subscriptions/") && r.Method == http.MethodPut {
			timeout = 45 * time.Second
		} else if r.URL.Path == "/api/v1/mihomo/config" && r.Method == http.MethodPost {
			timeout = 30 * time.Second
		} else if strings.HasPrefix(r.URL.Path, "/api/v1/mihomo/subscriptions/") && strings.HasSuffix(r.URL.Path, "/refresh") {
			timeout = 45 * time.Second
		} else if r.URL.Path == "/api/v1/messages" && r.Method == http.MethodPost {
			// A multipart SMS may require several independent modem or SIP
			// transactions. The transport budgets 120 seconds for dispatch;
			// RP submit reports remain asynchronous and do not consume this.
			timeout = 130 * time.Second
		}
		timeoutJSON(timeout)(next).ServeHTTP(w, r)
	})
}

func (server *Server) StreamEvents(w http.ResponseWriter, r *http.Request) {
	authCtx, cancelAuth := context.WithTimeout(r.Context(), realtimeAuthTimeout)
	authorized := server.requireBusinessAPI(w, r.WithContext(authCtx))
	cancelAuth()
	if !authorized {
		return
	}
	if featureDisabled(server.realtime) {
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "EVENT_STREAM_UNAVAILABLE", Retryable: true})
		return
	}
	if _, ok := w.(http.Flusher); !ok {
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "EVENT_STREAM_UNAVAILABLE", Retryable: true})
		return
	}
	sessionToken, _, ok := administratorTokens(r, false)
	if !ok {
		clearAdministratorCookies(w, r)
		writeJSON(w, http.StatusUnauthorized, openapi.ApiError{Code: "AUTH_SESSION_UNAUTHORIZED", Retryable: false})
		return
	}
	subscription := server.realtime.Subscribe()
	defer subscription.Close()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if err := writeAndFlushRealtime(w, func() error {
		_, err := io.WriteString(w, "retry: 3000\n\n")
		return err
	}); err != nil {
		return
	}
	heartbeat := server.realtimeHeartbeat
	if heartbeat <= 0 {
		heartbeat = 15 * time.Second
	}
	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, open := <-subscription.C:
			if !open || writeAndFlushRealtime(w, func() error { return writeRealtimeEvent(w, event) }) != nil {
				return
			}
		case <-ticker.C:
			if !server.realtimeSessionValid(r.Context(), sessionToken) {
				return
			}
			if err := writeAndFlushRealtime(w, func() error {
				_, err := io.WriteString(w, ": heartbeat\n\n")
				return err
			}); err != nil {
				return
			}
		}
	}
}

func (server *Server) realtimeSessionValid(ctx context.Context, token string) bool {
	if server == nil || featureDisabled(server.setup) || server.auth == nil {
		return false
	}
	checkCtx, cancel := context.WithTimeout(ctx, realtimeAuthTimeout)
	defer cancel()
	status, err := server.setup.Status(checkCtx)
	if err != nil || !status.BusinessAPIAvailable {
		return false
	}
	_, err = server.auth.Authenticate(checkCtx, token, "", false)
	return err == nil
}

func writeAndFlushRealtime(w http.ResponseWriter, write func() error) error {
	controller := http.NewResponseController(w)
	deadlineSet := false
	if err := controller.SetWriteDeadline(time.Now().Add(realtimeWriteTimeout)); err != nil {
		if !errors.Is(err, http.ErrNotSupported) {
			return err
		}
	} else {
		deadlineSet = true
	}
	if err := write(); err != nil {
		return err
	}
	if err := controller.Flush(); err != nil {
		return err
	}
	if deadlineSet {
		return controller.SetWriteDeadline(time.Time{})
	}
	return nil
}

func writeRealtimeEvent(w io.Writer, event realtime.Event) error {
	topics := make([]openapi.RealtimeTopic, 0, len(event.Topics))
	for _, topic := range event.Topics {
		topics = append(topics, openapi.RealtimeTopic(topic))
	}
	payload := openapi.RealtimeEvent{Topics: topics}
	if event.Attention != "" {
		attention := openapi.RealtimeAttention(event.Attention)
		payload.Attention = &attention
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.ID, event.Kind, data)
	return err
}

func (server *Server) publish(topics []realtime.Topic, attention realtime.Attention) {
	if server != nil && !featureDisabled(server.realtime) {
		server.realtime.Publish(topics, attention)
	}
}

type bufferedResponse struct {
	mu       sync.Mutex
	header   http.Header
	body     bytes.Buffer
	status   int
	timedOut bool
}

type handlerPanic struct{ stack []byte }

func (value handlerPanic) preservedPanicStack() []byte { return value.stack }

func newBufferedResponse() *bufferedResponse {
	return &bufferedResponse{header: make(http.Header)}
}

func (response *bufferedResponse) Header() http.Header {
	return response.header
}

func (response *bufferedResponse) WriteHeader(status int) {
	response.mu.Lock()
	defer response.mu.Unlock()
	if !response.timedOut && response.status == 0 {
		response.status = status
	}
}

func (response *bufferedResponse) Write(body []byte) (int, error) {
	response.mu.Lock()
	defer response.mu.Unlock()
	if response.timedOut {
		return 0, http.ErrHandlerTimeout
	}
	if response.status == 0 {
		response.status = http.StatusOK
	}
	return response.body.Write(body)
}

func (response *bufferedResponse) markTimedOut() {
	response.mu.Lock()
	response.timedOut = true
	response.mu.Unlock()
}

func (response *bufferedResponse) commit(target http.ResponseWriter) {
	response.mu.Lock()
	defer response.mu.Unlock()
	if response.timedOut {
		return
	}
	for key, values := range response.header {
		target.Header()[key] = append([]string(nil), values...)
	}
	status := response.status
	if status == 0 {
		status = http.StatusOK
	}
	target.WriteHeader(status)
	_, _ = target.Write(response.body.Bytes())
}

type requestWorkKey struct{}

// Wait is called after admission stops; it also waits for timed-out handlers.
func (server *Server) Wait() { server.work.Wait() }

func timeoutJSON(timeout time.Duration) func(http.Handler) http.Handler {
	type completion struct {
		panicValue any
		panicStack []byte
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			buffer := newBufferedResponse()
			completed := make(chan completion, 1)
			work, _ := r.Context().Value(requestWorkKey{}).(*sync.WaitGroup)
			if work != nil {
				work.Add(1)
			}
			go func() {
				if work != nil {
					defer work.Done()
				}
				result := completion{}
				defer func() {
					result.panicValue = recover()
					if result.panicValue != nil {
						result.panicStack = debug.Stack()
					}
					completed <- result
				}()
				next.ServeHTTP(buffer, r.WithContext(ctx))
			}()

			select {
			case result := <-completed:
				if result.panicValue != nil {
					panic(handlerPanic{stack: result.panicStack})
				}
				buffer.commit(w)
			case <-ctx.Done():
				buffer.markTimedOut()
				if ctx.Err() == context.DeadlineExceeded {
					writeJSON(w, http.StatusGatewayTimeout, openapi.ApiError{
						Code:      "API_TIMEOUT",
						Retryable: true,
					})
				}
			}
		})
	}
}

func recoverJSON(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					stack := debug.Stack()
					if preserved, ok := recovered.(interface{ preservedPanicStack() []byte }); ok {
						stack = preserved.preservedPanicStack()
					}
					logger.ErrorContext(
						r.Context(),
						"HTTP handler panic",
						"request_id", middleware.GetReqID(r.Context()),
						"method", r.Method,
						"path", r.URL.Path,
						"stack", string(stack),
					)
					writeJSON(w, http.StatusInternalServerError, openapi.ApiError{
						Code:      "API_INTERNAL_ERROR",
						Retryable: true,
					})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func trustedLANHostOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isTrustedLANAuthority(r.Host) {
			writeJSON(w, http.StatusMisdirectedRequest, openapi.ApiError{
				Code:      "TRUSTED_LAN_HOST_REQUIRED",
				Retryable: false,
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isTrustedLANAuthority(authority string) bool {
	host := authority
	if parsedHost, portText, err := net.SplitHostPort(authority); err == nil {
		port, err := strconv.Atoi(portText)
		if err != nil || port < 1 || port > 65535 {
			return false
		}
		host = parsedHost
	} else if strings.HasPrefix(authority, "[") && strings.HasSuffix(authority, "]") {
		host = strings.TrimSuffix(strings.TrimPrefix(authority, "["), "]")
	} else if strings.Contains(authority, ":") {
		return false
	}

	host = strings.TrimSuffix(host, ".")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func (server *Server) requireBusinessAPI(w http.ResponseWriter, r *http.Request) bool {
	status, err := server.setup.Status(r.Context())
	if err != nil {
		server.logger.ErrorContext(r.Context(), "business API gate failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{
			Code:      "INSTALLATION_STATE_UNAVAILABLE",
			Retryable: true,
		})
		return false
	}
	if status.BusinessAPIAvailable {
		_, ok := server.requireAdministrator(w, r, r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions)
		return ok
	}
	code := "INSTANCE_MAINTENANCE"
	if status.SetupRequired {
		code = "INSTANCE_NOT_INITIALIZED"
	}
	writeJSON(w, http.StatusConflict, openapi.ApiError{Code: code, Retryable: false})
	return false
}

func setAdministratorCookies(w http.ResponseWriter, r *http.Request, sessionToken, csrfToken string, expiresAt time.Time) {
	secure := r.TLS != nil
	http.SetCookie(w, &http.Cookie{Name: adminSessionCookieName, Value: sessionToken, Path: "/api/v1", Expires: expiresAt, HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
	http.SetCookie(w, &http.Cookie{Name: csrfCookieName, Value: csrfToken, Path: "/", Expires: expiresAt, HttpOnly: false, Secure: secure, SameSite: http.SameSiteStrictMode})
}

func clearAdministratorCookies(w http.ResponseWriter, r *http.Request) {
	secure := r.TLS != nil
	for _, cookie := range []http.Cookie{
		{Name: adminSessionCookieName, Path: "/api/v1", HttpOnly: true},
		{Name: csrfCookieName, Path: "/", HttpOnly: false},
	} {
		cookie.MaxAge = -1
		cookie.Expires = time.Unix(1, 0).UTC()
		cookie.Secure = secure
		cookie.SameSite = http.SameSiteStrictMode
		http.SetCookie(w, &cookie)
	}
}

func administratorTokens(r *http.Request, requireCSRF bool) (string, string, bool) {
	sessionCookie, err := r.Cookie(adminSessionCookieName)
	if err != nil || sessionCookie.Value == "" {
		return "", "", false
	}
	if !requireCSRF {
		return sessionCookie.Value, "", true
	}
	csrfCookie, err := r.Cookie(csrfCookieName)
	csrfHeader := r.Header.Get(csrfHeaderName)
	if err != nil || csrfCookie.Value == "" || csrfHeader == "" || csrfCookie.Value != csrfHeader {
		return "", "", false
	}
	return sessionCookie.Value, csrfHeader, true
}

func authSessionResponse(user authapp.User, expiresAt time.Time) openapi.AuthSessionResponse {
	return openapi.AuthSessionResponse{
		Username:  user.Username,
		Locale:    openapi.AuthSessionResponseLocale(user.Locale),
		ExpiresAt: expiresAt,
	}
}

func mihomoCoreStatusResponse(status mihomoapp.CoreStatus) openapi.MihomoCoreStatus {
	installedAt := ""
	if !status.InstalledAt.IsZero() {
		installedAt = status.InstalledAt.UTC().Format(time.RFC3339)
	}
	return openapi.MihomoCoreStatus{Installed: status.Installed, Version: status.Version, Architecture: openapi.MihomoCoreStatusArchitecture(status.Architecture), Sha256: status.SHA256, InstalledAt: installedAt}
}

func mihomoSubscriptionResponse(item mihomoapp.SubscriptionView) openapi.MihomoSubscription {
	refreshAt := ""
	if !item.LastRefreshAt.IsZero() {
		refreshAt = item.LastRefreshAt.UTC().Format(time.RFC3339)
	}
	return openapi.MihomoSubscription{Id: item.ID, DisplayName: item.DisplayName, Url: item.URL, UrlHint: item.URLHint, Enabled: item.Enabled, Selected: item.Selected, ArtifactReady: item.ArtifactReady, LastRefreshAt: refreshAt, LastRefreshStatus: openapi.MihomoSubscriptionLastRefreshStatus(item.LastRefreshStatus), NodeCount: item.NodeCount, LastErrorCode: item.LastErrorCode}
}
func mihomoNodeResponses(nodes []mihomodomain.Node) []openapi.MihomoNode {
	result := make([]openapi.MihomoNode, 0, len(nodes))
	for _, node := range nodes {
		result = append(result, openapi.MihomoNode{Id: node.ID, DisplayName: node.DisplayName, Kind: node.Kind, CountryCode: node.CountryCode, CountryName: node.CountryName})
	}
	return result
}

func voWiFiStateResponse(item vowifidomain.State) openapi.VoWiFiLineState {
	registeredAt, nextRefresh := "", ""
	if !item.RegisteredAt.IsZero() {
		registeredAt = item.RegisteredAt.UTC().Format(time.RFC3339)
	}
	if !item.NextRefreshAt.IsZero() {
		nextRefresh = item.NextRefreshAt.UTC().Format(time.RFC3339)
	}
	return openapi.VoWiFiLineState{
		LineId: item.LineID, DesiredActive: item.DesiredActive, Eligible: item.Eligible,
		ReadinessCode: openapi.VoWiFiLineStateReadinessCode(item.ReadinessCode),
		State:         openapi.VoWiFiLineStateState(item.State), Stage: item.Stage, Online: item.Online,
		EgressMode: openapi.VoWiFiLineStateEgressMode(item.EgressMode), CountryCode: item.CountryCode,
		CountryName: item.CountryName, RegisteredAt: registeredAt, NextRefreshAt: nextRefresh,
		Attempt: item.Attempt, LastErrorCode: item.LastErrorCode,
	}
}

func mihomoConfigResponse(status mihomoapp.ConfigStatus) openapi.MihomoConfigStatus {
	generatedAt := ""
	if !status.GeneratedAt.IsZero() {
		generatedAt = status.GeneratedAt.UTC().Format(time.RFC3339)
	}
	return openapi.MihomoConfigStatus{Published: status.Published, Launchable: status.Launchable, Sha256: status.SHA256, GeneratedAt: generatedAt, ErrorCode: status.ErrorCode, SelectedSubscriptionId: status.SelectedSubscriptionID, RunningSubscriptionId: status.RunningSubscriptionID}
}

func mihomoRuntimeResponse(status mihomoapp.RuntimeStatus) openapi.MihomoRuntimeStatus {
	startedAt := ""
	if !status.StartedAt.IsZero() {
		startedAt = status.StartedAt.UTC().Format(time.RFC3339)
	}
	return openapi.MihomoRuntimeStatus{State: openapi.MihomoRuntimeStatusState(status.State), Pid: status.PID, SelectedSubscriptionId: status.SelectedSubscriptionID, RunningSubscriptionId: status.RunningSubscriptionID, PendingRestart: status.PendingRestart, StartedAt: startedAt, LastErrorCode: status.LastErrorCode}
}

func notificationEventStrings(events []openapi.NotificationEventKind) []string {
	result := make([]string, 0, len(events))
	for _, event := range events {
		result = append(result, string(event))
	}
	return result
}
func notificationChannelResponse(item notificationapp.ChannelView) openapi.NotificationChannel {
	lastAt := ""
	if !item.LastDeliveryAt.IsZero() {
		lastAt = item.LastDeliveryAt.UTC().Format(time.RFC3339)
	}
	events := make([]openapi.NotificationEventKind, 0, len(item.EventKinds))
	for _, event := range item.EventKinds {
		events = append(events, openapi.NotificationEventKind(event))
	}
	return openapi.NotificationChannel{PendingCount: item.PendingCount, FailedCount: item.FailedCount, Id: item.ID, Provider: openapi.NotificationChannelProvider(item.Provider), DeliveryMode: openapi.NotificationChannelDeliveryMode(item.DeliveryMode), TargetType: openapi.NotificationChannelTargetType(item.TargetType), DisplayName: item.DisplayName, WebhookHint: openapi.NotificationChannelWebhookHint(item.WebhookHint), SigningSecretConfigured: item.SigningSecretConfigured, Enabled: item.Enabled, EventKinds: events, LastDeliveryAt: lastAt, LastDeliveryStatus: openapi.NotificationChannelLastDeliveryStatus(item.LastDeliveryStatus), LastErrorCode: item.LastErrorCode}
}

func feishuBindingResponse(state notificationapp.BindingView) openapi.FeishuNotificationBinding {
	expiresAt := ""
	if !state.ExpiresAt.IsZero() {
		expiresAt = state.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return openapi.FeishuNotificationBinding{State: openapi.FeishuNotificationBindingState(state.State), VerificationUrl: state.VerificationURL, ExpiresAt: expiresAt, ChannelId: state.ChannelID, ErrorCode: state.ErrorCode}
}

func euiccResponse(state domaineuicc.State) openapi.EUICCState {
	profiles := make([]openapi.EUICCProfile, 0, len(state.Profiles))
	for _, profile := range state.Profiles {
		profiles = append(profiles, openapi.EUICCProfile{Id: profile.ID, DisplayName: profile.DisplayName, DisplayIdentityHint: profile.DisplayIdentityHint, Active: profile.Active})
	}
	return openapi.EUICCState{EidHint: state.EIDHint, Profiles: profiles}
}

func callResponse(value call.Record) openapi.Call {
	return openapi.Call{Id: value.ID, OperationId: value.OperationID, LineId: value.LineID, RemoteAddress: value.RemoteAddress, Direction: openapi.CallDirection(value.Direction), State: openapi.CallState(value.State), EndReason: value.EndReason, CreatedAt: value.CreatedAt.UTC(), UpdatedAt: value.UpdatedAt.UTC(), AnsweredAt: value.AnsweredAt, EndedAt: value.EndedAt}
}

func contactResponse(value contact.Contact) openapi.Contact {
	return openapi.Contact{
		Id: value.ID, DisplayName: value.DisplayName, PhoneNumber: value.PhoneNumber,
		CreatedAt: value.CreatedAt.UTC(), UpdatedAt: value.UpdatedAt.UTC(),
	}
}

func managedLineResponse(item linedomain.View) openapi.ManagedLine {
	phoneNumbers := make([]openapi.PhoneNumberObservation, 0, len(item.PhoneNumbers))
	for _, observation := range item.PhoneNumbers {
		sources := make([]openapi.PhoneNumberSource, 0, len(observation.Sources))
		for _, source := range observation.Sources {
			sources = append(sources, openapi.PhoneNumberSource(source))
		}
		phoneNumbers = append(phoneNumbers, openapi.PhoneNumberObservation{Number: observation.Number, Sources: sources})
	}
	return openapi.ManagedLine{
		Id: item.ID, DisplayName: item.DisplayName, ManagedModemId: item.ManagedModemID,
		ManagedModemDisplayName: item.ManagedModemDisplayName,
		ManagedModemModel:       item.ManagedModemModel, ManagedModemSerialNumber: item.ManagedModemSerialNumber,
		SubscriptionDisplayHint: item.SubscriptionDisplayHint,
		PhoneNumbers:            phoneNumbers,
		State:                   openapi.ManagedLineState(item.State), Capabilities: hardwareCapabilitiesResponse(item.Capabilities),
		CreatedAt: item.CreatedAt.UTC(),
	}
}

func inventoryResponse(snapshot inventory.Snapshot) openapi.InventoryResponse {
	devices := make([]openapi.PhysicalDeviceSummary, 0, len(snapshot.Devices))
	for _, device := range snapshot.Devices {
		devices = append(devices, openapi.PhysicalDeviceSummary{
			Id:                 device.ID,
			DisplayName:        device.DisplayName,
			Transport:          openapi.DeviceTransport(device.Transport),
			State:              openapi.PhysicalDeviceState(device.State),
			Generation:         int64(device.Generation),
			ModemFunctionCount: device.ModemFunctionCount,
			SimSlotCount:       device.SIMSlotCount,
			ResourceGroupCount: device.ResourceGroupCount,
		})
	}
	lines := make([]openapi.LineSummary, 0, len(snapshot.Lines))
	for _, line := range snapshot.Lines {
		lines = append(lines, openapi.LineSummary{
			Id:                    line.ID,
			PhysicalDeviceId:      line.PhysicalDeviceID,
			SubscriptionProfileId: line.SubscriptionProfileID,
			DisplayName:           line.DisplayName,
			Generation:            int64(line.Generation),
			State:                 openapi.LineState(line.State),
		})
	}
	return openapi.InventoryResponse{Generation: int64(snapshot.Generation), Revision: snapshot.Revision, ObservedAt: snapshot.ObservedAt, Devices: devices, Lines: lines}
}

func hardwareTopologyResponse(topology inventory.Topology) openapi.HardwareTopologyResponse {
	devices := make([]openapi.PhysicalDeviceDetail, 0, len(topology.Devices))
	for _, device := range topology.Devices {
		devices = append(devices, openapi.PhysicalDeviceDetail{
			Id: device.ID, DisplayName: device.DisplayName, Transport: openapi.DeviceTransport(device.Transport),
			State: openapi.PhysicalDeviceState(device.State), Generation: int64(device.Generation),
		})
	}
	functions := make([]openapi.ModemFunctionDetail, 0, len(topology.ModemFunctions))
	for _, function := range topology.ModemFunctions {
		functions = append(functions, openapi.ModemFunctionDetail{
			Id: function.ID, PhysicalDeviceId: function.PhysicalDeviceID, DisplayName: function.DisplayName,
			Backend: openapi.HardwareBackend(function.Backend), Generation: int64(function.Generation),
			Capabilities: hardwareCapabilitiesResponse(function.Capabilities),
		})
	}
	slots := make([]openapi.SIMSlotDetail, 0, len(topology.SIMSlots))
	for _, slot := range topology.SIMSlots {
		slots = append(slots, openapi.SIMSlotDetail{
			Id: slot.ID, PhysicalDeviceId: slot.PhysicalDeviceID, Index: slot.Index, Presence: openapi.SIMSlotPresence(slot.Presence),
			ActiveMediaId: slot.ActiveMediaID, Generation: int64(slot.Generation),
		})
	}
	media := make([]openapi.SIMMediaDetail, 0, len(topology.SIMMedia))
	for _, item := range topology.SIMMedia {
		media = append(media, openapi.SIMMediaDetail{
			Id: item.ID, SimSlotId: item.SIMSlotID, Kind: openapi.SIMMediaKind(item.Kind), IdentityState: openapi.SIMIdentityState(item.IdentityState),
			DisplayIdentityHint: item.DisplayIdentityHint, Generation: int64(item.Generation),
		})
	}
	profiles := make([]openapi.SubscriptionProfileDetail, 0, len(topology.SubscriptionProfiles))
	for _, profile := range topology.SubscriptionProfiles {
		profiles = append(profiles, openapi.SubscriptionProfileDetail{
			Id: profile.ID, SimMediaId: profile.SIMMediaID, DisplayName: profile.DisplayName,
			State: openapi.SubscriptionProfileState(profile.State), DisplayIdentityHint: profile.DisplayIdentityHint,
			Generation: int64(profile.Generation),
		})
	}
	groups := make([]openapi.ResourceGroupDetail, 0, len(topology.ResourceGroups))
	for _, group := range topology.ResourceGroups {
		resources := make([]openapi.ResourceKind, 0, len(group.Resources))
		for _, resource := range group.Resources {
			resources = append(resources, openapi.ResourceKind(resource))
		}
		groups = append(groups, openapi.ResourceGroupDetail{
			Id: group.ID, PhysicalDeviceId: group.PhysicalDeviceID, DisplayName: group.DisplayName,
			Resources: resources, ModemFunctionIds: append([]string(nil), group.ModemFunctionIDs...), SimSlotIds: append([]string(nil), group.SIMSlotIDs...),
			MaxActiveCalls: group.MaxActiveCalls, MaxConcurrentOps: group.MaxConcurrentOps, Generation: int64(group.Generation),
		})
	}
	lines := make([]openapi.HardwareLineDetail, 0, len(topology.Lines))
	for _, line := range topology.Lines {
		lines = append(lines, openapi.HardwareLineDetail{
			Id: line.ID, PhysicalDeviceId: line.PhysicalDeviceID, ModemFunctionId: line.ModemFunctionID,
			SubscriptionProfileId: line.SubscriptionProfileID, ResourceGroupId: line.ResourceGroupID,
			DisplayName: line.DisplayName, Generation: int64(line.Generation), Capabilities: hardwareCapabilitiesResponse(line.Capabilities),
			State: openapi.LineState(line.State),
		})
	}
	return openapi.HardwareTopologyResponse{
		Generation: int64(topology.Generation), Revision: topology.Revision, ObservedAt: topology.ObservedAt, Devices: devices, ModemFunctions: functions,
		SimSlots: slots, SimMedia: media, SubscriptionProfiles: profiles, ResourceGroups: groups, Lines: lines,
	}
}

func hardwareCapabilitiesResponse(capabilities hardware.Capabilities) openapi.HardwareCapabilities {
	return openapi.HardwareCapabilities{
		SimAccess: capabilities.SIMAccess, Sms: capabilities.SMS, CellularVoice: capabilities.CellularVoice, DigitalVoiceMedia: capabilities.DigitalVoiceMedia,
		UsbUac: capabilities.USBUAC, SimApdu: capabilities.SIMAPDU, HostVoWifiAuth: capabilities.HostVoWiFiAuth,
		RfControl: capabilities.RFControl, NetworkScan: capabilities.NetworkScan,
		ManualNetworkSelection: capabilities.ManualNetworkSelection, PrimarySimLockState: capabilities.PrimarySIMLockState,
		Pin1Verify: capabilities.PIN1Verify, Puk1Unblock: capabilities.PUK1Unblock, EuiccProfiles: capabilities.EUICCProfiles,
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain exactly one JSON value")
	}
	return nil
}

func (server *Server) GetSystemHealth(w http.ResponseWriter, r *http.Request) {
	snapshot, err := server.health.Snapshot(r.Context())
	if err != nil {
		server.logger.ErrorContext(r.Context(), "health snapshot failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{
			Code:      "HEALTH_SNAPSHOT_UNAVAILABLE",
			Retryable: true,
		})
		return
	}

	writeJSON(w, http.StatusOK, openapi.HealthResponse{
		Status:            openapi.HealthStatus(snapshot.Status),
		Version:           snapshot.Version,
		ApiVersion:        openapi.HealthResponseApiVersion(snapshot.APIVersion),
		InstallationState: openapi.InstallationState(snapshot.InstallationState),
		Backend:           openapi.BackendKind(snapshot.Backend),
		DatabaseCount:     snapshot.DatabaseCount,
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
