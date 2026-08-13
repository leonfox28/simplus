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

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/leonfox28/simplus/internal/api/openapi"
	authapp "github.com/leonfox28/simplus/internal/application/auth"
	callapp "github.com/leonfox28/simplus/internal/application/calls"
	contactapp "github.com/leonfox28/simplus/internal/application/contacts"
	euiccapp "github.com/leonfox28/simplus/internal/application/euicc"
	"github.com/leonfox28/simplus/internal/application/health"
	"github.com/leonfox28/simplus/internal/application/inventory"
	lineapp "github.com/leonfox28/simplus/internal/application/line"
	lineegressapp "github.com/leonfox28/simplus/internal/application/lineegress"
	messageapp "github.com/leonfox28/simplus/internal/application/messaging"
	mihomoapp "github.com/leonfox28/simplus/internal/application/mihomo"
	modemapp "github.com/leonfox28/simplus/internal/application/modem"
	notificationapp "github.com/leonfox28/simplus/internal/application/notification"
	"github.com/leonfox28/simplus/internal/application/realtime"
	setupapp "github.com/leonfox28/simplus/internal/application/setup"
	vowifiapp "github.com/leonfox28/simplus/internal/application/vowifi"
	"github.com/leonfox28/simplus/internal/domain/call"
	"github.com/leonfox28/simplus/internal/domain/contact"
	domaineuicc "github.com/leonfox28/simplus/internal/domain/euicc"
	"github.com/leonfox28/simplus/internal/domain/hardware"
	linedomain "github.com/leonfox28/simplus/internal/domain/line"
	mihomodomain "github.com/leonfox28/simplus/internal/domain/mihomo"
	modemdomain "github.com/leonfox28/simplus/internal/domain/modem"
	"github.com/leonfox28/simplus/internal/domain/pagination"
	"github.com/leonfox28/simplus/internal/domain/sms"
	vowifidomain "github.com/leonfox28/simplus/internal/domain/vowifi"
)

const (
	setupSessionCookieName = "simplus_setup_session"
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
	Notify(context.Context, string, string) error
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

type Server struct {
	health              *health.Service
	setup               *setupapp.Service
	inventory           *inventory.Service
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
	realtime            *realtime.Hub
	realtimeHeartbeat   time.Duration
	logger              *slog.Logger
}

func WithMihomoCore(server *Server, manager MihomoCoreManager) *Server {
	if server != nil {
		server.mihomoCore = manager
	}
	return server
}

func WithMihomoSubscriptions(server *Server, manager MihomoSubscriptionManager) *Server {
	if server != nil {
		server.mihomoSubscriptions = manager
	}
	return server
}

func WithLineEgress(server *Server, manager LineEgressManager) *Server {
	if server != nil {
		server.lineEgress = manager
	}
	return server
}

func WithVoWiFi(server *Server, manager VoWiFiManager) *Server {
	if server != nil {
		server.vowifi = manager
	}
	return server
}

func WithManagedModems(server *Server, manager ManagedModemManager) *Server {
	if server != nil {
		server.modems = manager
	}
	return server
}

func WithManagedLines(server *Server, manager ManagedLineManager) *Server {
	if server != nil {
		server.lines = manager
	}
	return server
}

func WithMihomoConfig(server *Server, manager MihomoConfigManager) *Server {
	if server != nil {
		server.mihomoConfig = manager
	}
	return server
}
func WithMihomoRuntime(server *Server, manager MihomoRuntimeManager) *Server {
	if server != nil {
		server.mihomoRuntime = manager
	}
	return server
}
func WithMihomoDashboard(server *Server, manager MihomoDashboardManager) *Server {
	if server != nil {
		server.mihomoDashboard = manager
	}
	return server
}
func WithNotifications(server *Server, manager NotificationManager) *Server {
	if server != nil {
		server.notifications = manager
	}
	return server
}

func WithRealtime(server *Server, hub *realtime.Hub) *Server {
	if server != nil {
		server.realtime = hub
	}
	return server
}

func WithCalls(server *Server, calls CallManager) *Server {
	if server != nil {
		server.calls = calls
	}
	return server
}

func WithEUICC(server *Server, manager EUICCManager) *Server {
	if server != nil {
		server.euicc = manager
	}
	return server
}
func New(
	healthService *health.Service,
	setupService *setupapp.Service,
	inventoryService *inventory.Service,
	logger *slog.Logger,
	authentication Authenticator,
	messages Messenger,
	contactManagers ...ContactManager,
) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	server := &Server{
		health: healthService, setup: setupService, inventory: inventoryService,
		auth: authentication, messages: messages, logger: logger, realtimeHeartbeat: 15 * time.Second,
	}
	if len(contactManagers) != 0 {
		server.contacts = contactManagers[0]
	}
	return server
}

func Router(server *Server) http.Handler {
	router := chi.NewRouter()
	router.Use(securityHeaders)
	router.Use(trustedLANHostOnly)
	router.Use(middleware.RequestID)
	router.Use(recoverJSON(server.logger))
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
	if server.realtime == nil {
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
	if server == nil || server.setup == nil || server.auth == nil {
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
	if server != nil && server.realtime != nil {
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
			go func() {
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

func (server *Server) GetSetupStatus(w http.ResponseWriter, r *http.Request) {
	status, err := server.setup.Status(r.Context())
	if err != nil {
		server.logger.ErrorContext(r.Context(), "setup status failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{
			Code:      "SETUP_STATUS_UNAVAILABLE",
			Retryable: true,
		})
		return
	}
	flows := make([]openapi.SetupFlow, 0, len(status.SupportedFlows))
	for _, flow := range status.SupportedFlows {
		flows = append(flows, openapi.SetupFlow(flow))
	}
	writeJSON(w, http.StatusOK, openapi.SetupStatusResponse{
		InstallationState:            openapi.InstallationState(status.InstallationState),
		Phase:                        openapi.SetupPhase(status.Phase),
		SetupRequired:                status.SetupRequired,
		BusinessApiAvailable:         status.BusinessAPIAvailable,
		BootstrapGenerationAvailable: status.BootstrapGenerationAvailable,
		SupportedFlows:               flows,
	})
}

func (server *Server) ConsumeSetupBootstrap(w http.ResponseWriter, r *http.Request) {
	var request openapi.ConsumeBootstrapRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "BOOTSTRAP_REQUEST_INVALID", Retryable: false})
		return
	}
	grant, err := server.setup.ConsumeBootstrap(r.Context(), request.BootstrapCode)
	if err != nil {
		server.writeSetupAuthorizationError(w, r, err, "bootstrap exchange failed")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     setupSessionCookieName,
		Value:    grant.Token,
		Path:     "/api/v1/setup",
		Expires:  grant.ExpiresAt,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, http.StatusOK, setupSessionResponse(grant.Session))
}

func (server *Server) GetSetupSession(w http.ResponseWriter, r *http.Request) {
	token, ok := setupSessionToken(r)
	if !ok {
		clearSetupSessionCookie(w, r)
		writeJSON(w, http.StatusUnauthorized, openapi.ApiError{Code: "SETUP_SESSION_UNAUTHORIZED", Retryable: false})
		return
	}
	session, err := server.setup.ReadSession(r.Context(), token)
	if err != nil {
		server.writeSetupAuthorizationError(w, r, err, "setup session read failed")
		return
	}
	writeJSON(w, http.StatusOK, setupSessionResponse(session))
}

func (server *Server) PutSetupAdministrator(w http.ResponseWriter, r *http.Request) {
	token, ok := setupSessionToken(r)
	if !ok {
		clearSetupSessionCookie(w, r)
		writeJSON(w, http.StatusUnauthorized, openapi.ApiError{Code: "SETUP_SESSION_UNAUTHORIZED", Retryable: false})
		return
	}
	var request openapi.ConfigureSetupAdministratorRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "SETUP_ADMINISTRATOR_REQUEST_INVALID", Retryable: false})
		return
	}
	session, err := server.setup.ConfigureAdministrator(r.Context(), token, setupapp.AdministratorInput{
		Username:              request.Username,
		Password:              request.Password,
		PasswordConfirmation:  request.PasswordConfirmation,
		InstanceDefaultLocale: string(request.InstanceDefaultLocale),
	})
	if err != nil {
		server.writeSetupAuthorizationError(w, r, err, "setup administrator configuration failed")
		return
	}
	writeJSON(w, http.StatusOK, setupSessionResponse(session))
}

func (server *Server) PutSetupStorage(w http.ResponseWriter, r *http.Request) {
	token, ok := setupSessionToken(r)
	if !ok {
		clearSetupSessionCookie(w, r)
		writeJSON(w, http.StatusUnauthorized, openapi.ApiError{Code: "SETUP_SESSION_UNAUTHORIZED", Retryable: false})
		return
	}
	var request openapi.ConfigureSetupStorageRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "SETUP_STORAGE_REQUEST_INVALID", Retryable: false})
		return
	}
	session, err := server.setup.ConfigureStorage(r.Context(), token, setupapp.StorageInput{RecordingsRoot: request.RecordingsRoot})
	if err != nil {
		server.writeSetupAuthorizationError(w, r, err, "setup storage configuration failed")
		return
	}
	writeJSON(w, http.StatusOK, setupSessionResponse(session))
}

func (server *Server) PutSetupHTTPS(w http.ResponseWriter, r *http.Request) {
	token, ok := setupSessionToken(r)
	if !ok {
		clearSetupSessionCookie(w, r)
		writeJSON(w, http.StatusUnauthorized, openapi.ApiError{Code: "SETUP_SESSION_UNAUTHORIZED", Retryable: false})
		return
	}
	var request openapi.ConfigureSetupHTTPSRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "SETUP_HTTPS_REQUEST_INVALID", Retryable: false})
		return
	}
	session, err := server.setup.ConfigureHTTPS(r.Context(), token, setupapp.HTTPSInput{
		Mode:                    string(request.Mode),
		ListenHost:              request.ListenHost,
		ListenPort:              request.ListenPort,
		SubjectAlternativeNames: request.SubjectAlternativeNames,
	})
	if err != nil {
		server.writeSetupAuthorizationError(w, r, err, "setup HTTPS configuration failed")
		return
	}
	writeJSON(w, http.StatusOK, setupSessionResponse(session))
}

func (server *Server) ConfirmSetupHTTPS(w http.ResponseWriter, r *http.Request) {
	token, ok := setupSessionToken(r)
	if !ok {
		clearSetupSessionCookie(w, r)
		writeJSON(w, http.StatusUnauthorized, openapi.ApiError{Code: "SETUP_SESSION_UNAUTHORIZED", Retryable: false})
		return
	}
	var request openapi.ConfirmSetupHTTPSRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "SETUP_HTTPS_CONFIRMATION_INVALID", Retryable: false})
		return
	}
	session, err := server.setup.ConfirmHTTPS(r.Context(), token, request.RootFingerprintSha256)
	if err != nil {
		server.writeSetupAuthorizationError(w, r, err, "setup HTTPS confirmation failed")
		return
	}
	writeJSON(w, http.StatusOK, setupSessionResponse(session))
}

func (server *Server) GetSetupHTTPSRootCertificate(w http.ResponseWriter, r *http.Request) {
	token, ok := setupSessionToken(r)
	if !ok {
		clearSetupSessionCookie(w, r)
		writeJSON(w, http.StatusUnauthorized, openapi.ApiError{Code: "SETUP_SESSION_UNAUTHORIZED", Retryable: false})
		return
	}
	certificate, fingerprint, err := server.setup.ReadRootCertificate(r.Context(), token)
	if err != nil {
		server.writeSetupAuthorizationError(w, r, err, "setup root certificate read failed")
		return
	}
	writeJSON(w, http.StatusOK, openapi.SetupRootCertificateResponse{
		Pem:                   string(certificate),
		RootFingerprintSha256: fingerprint,
	})
}

func (server *Server) GetSetupInventory(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireSetupHardwareStage(w, r); !ok {
		return
	}
	snapshot, err := server.inventory.Snapshot(r.Context())
	if err != nil {
		server.logger.ErrorContext(r.Context(), "setup inventory snapshot failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "SETUP_INVENTORY_UNAVAILABLE", Retryable: true})
		return
	}
	writeJSON(w, http.StatusOK, inventoryResponse(snapshot))
}

func (server *Server) GetSetupHardwareTopology(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireSetupHardwareStage(w, r); !ok {
		return
	}
	topology, err := server.inventory.Topology(r.Context())
	if err != nil {
		server.logger.ErrorContext(r.Context(), "setup hardware topology failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "SETUP_INVENTORY_UNAVAILABLE", Retryable: true})
		return
	}
	writeJSON(w, http.StatusOK, hardwareTopologyResponse(topology))
}

func (server *Server) ConfirmSetupHardware(w http.ResponseWriter, r *http.Request) {
	token, ok := server.requireSetupHardwareStage(w, r)
	if !ok {
		return
	}
	topology, err := server.inventory.Topology(r.Context())
	if err != nil {
		server.logger.ErrorContext(r.Context(), "setup hardware confirmation topology failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "SETUP_INVENTORY_UNAVAILABLE", Retryable: true})
		return
	}
	hardwareInput, err := setupHardwareReviewInput(topology)
	if err != nil {
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "SETUP_HARDWARE_REVIEW_INVALID", Retryable: false})
		return
	}
	session, err := server.setup.ConfirmHardwareReview(r.Context(), token, hardwareInput)
	if err != nil {
		server.writeSetupAuthorizationError(w, r, err, "setup hardware confirmation failed")
		return
	}
	writeJSON(w, http.StatusOK, setupSessionResponse(session))
}

func (server *Server) CompleteSetup(w http.ResponseWriter, r *http.Request) {
	token, ok := setupSessionToken(r)
	if !ok {
		clearSetupSessionCookie(w, r)
		writeJSON(w, http.StatusUnauthorized, openapi.ApiError{Code: "SETUP_SESSION_UNAUTHORIZED", Retryable: false})
		return
	}
	_, err := server.setup.Complete(r.Context(), token, setupapp.HardwareReviewInput{})
	if err != nil {
		server.writeSetupAuthorizationError(w, r, err, "setup completion failed")
		return
	}
	clearSetupSessionCookie(w, r)
	writeJSON(w, http.StatusOK, openapi.SetupCompletionResponse{
		InstallationState: openapi.SetupCompletionResponseInstallationStateReady,
		ManagementUrl:     requestManagementURL(r),
		LoginRequired:     openapi.SetupCompletionResponseLoginRequired(true),
	})
}

func requestManagementURL(r *http.Request) string {
	scheme := "http"
	if r != nil && r.TLS != nil {
		scheme = "https"
	}
	if r == nil {
		return scheme + "://127.0.0.1"
	}
	return scheme + "://" + r.Host
}

func (server *Server) requireSetupHardwareStage(w http.ResponseWriter, r *http.Request) (string, bool) {
	token, ok := setupSessionToken(r)
	if !ok {
		clearSetupSessionCookie(w, r)
		writeJSON(w, http.StatusUnauthorized, openapi.ApiError{Code: "SETUP_SESSION_UNAUTHORIZED", Retryable: false})
		return "", false
	}
	session, err := server.setup.ReadSession(r.Context(), token)
	if err != nil {
		server.writeSetupAuthorizationError(w, r, err, "setup hardware authorization failed")
		return "", false
	}
	if !session.HTTPSConfigured || !session.HTTPSConfirmed {
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "SETUP_PREREQUISITE_INCOMPLETE", Retryable: false})
		return "", false
	}
	return token, true
}

func (server *Server) writeSetupAuthorizationError(w http.ResponseWriter, r *http.Request, err error, logMessage string) {
	switch {
	case errors.Is(err, setupapp.ErrBootstrapInvalidOrExpired):
		writeJSON(w, http.StatusUnauthorized, openapi.ApiError{Code: "BOOTSTRAP_INVALID_OR_EXPIRED", Retryable: false})
	case errors.Is(err, setupapp.ErrSetupSessionUnauthorized):
		clearSetupSessionCookie(w, r)
		writeJSON(w, http.StatusUnauthorized, openapi.ApiError{Code: "SETUP_SESSION_UNAUTHORIZED", Retryable: false})
	case errors.Is(err, setupapp.ErrSetupUnavailable):
		clearSetupSessionCookie(w, r)
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "SETUP_UNAVAILABLE", Retryable: false})
	case errors.Is(err, setupapp.ErrAdministratorRequestInvalid):
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "SETUP_ADMINISTRATOR_REQUEST_INVALID", Retryable: false})
	case errors.Is(err, setupapp.ErrStorageRequestInvalid):
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "SETUP_STORAGE_REQUEST_INVALID", Retryable: false})
	case errors.Is(err, setupapp.ErrSetupPrerequisiteMissing):
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "SETUP_PREREQUISITE_INCOMPLETE", Retryable: false})
	case errors.Is(err, setupapp.ErrHTTPSRequestInvalid):
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "SETUP_HTTPS_REQUEST_INVALID", Retryable: false})
	case errors.Is(err, setupapp.ErrHTTPSConfirmationInvalid):
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "SETUP_HTTPS_CONFIRMATION_INVALID", Retryable: false})
	case errors.Is(err, setupapp.ErrHardwareReviewInvalid):
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "SETUP_HARDWARE_REVIEW_INVALID", Retryable: false})
	case errors.Is(err, setupapp.ErrSetupPreflightFailed):
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "SETUP_PREFLIGHT_FAILED", Retryable: false})
	default:
		server.logger.ErrorContext(r.Context(), logMessage, "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "SETUP_AUTHORIZATION_FAILED", Retryable: true})
	}
}

func setupSessionResponse(session setupapp.Session) openapi.SetupSessionResponse {
	leafNotAfter := ""
	if !session.HTTPSLeafNotAfter.IsZero() {
		leafNotAfter = session.HTTPSLeafNotAfter.UTC().Format(time.RFC3339)
	}
	return openapi.SetupSessionResponse{
		Authorized:              openapi.SetupSessionResponseAuthorized(true),
		ExpiresAt:               session.ExpiresAt,
		SelectedFlow:            openapi.CreateNew,
		SupportedFlows:          []openapi.SetupFlow{openapi.CreateNew},
		AdministratorConfigured: session.AdministratorConfigured,
		AdministratorUsername:   session.AdministratorUsername,
		InstanceDefaultLocale:   openapi.SetupSessionResponseInstanceDefaultLocale(session.InstanceDefaultLocale),
		StorageConfigured:       session.StorageConfigured,
		DataRoot:                session.DataRoot,
		RecordingsRoot:          session.RecordingsRoot,
		HttpsConfigured:         session.HTTPSConfigured,
		HttpsConfirmed:          session.HTTPSConfirmed,
		HttpsMode:               openapi.SetupSessionResponseHttpsMode(session.HTTPSMode),
		HttpsListenUrl:          session.HTTPSListenURL,
		HttpsRootFingerprint:    session.HTTPSRootFingerprint,
		HttpsLeafNotAfter:       leafNotAfter,
		HardwareReviewed:        session.HardwareReviewed,
		HardwareDeviceCount:     session.HardwareDeviceCount,
		HardwareLineCount:       session.HardwareLineCount,
		HardwareInventoryDigest: session.HardwareInventoryDigest,
	}
}

func setupSessionToken(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(setupSessionCookieName)
	if err != nil || cookie.Value == "" {
		return "", false
	}
	return cookie.Value, true
}

func clearSetupSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     setupSessionCookieName,
		Path:     "/api/v1/setup",
		MaxAge:   -1,
		Expires:  time.Unix(1, 0).UTC(),
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
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

func (server *Server) requireAdministrator(w http.ResponseWriter, r *http.Request, requireCSRF bool) (authapp.Session, bool) {
	if server.auth == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "AUTH_UNAVAILABLE", Retryable: true})
		return authapp.Session{}, false
	}
	token, csrfToken, ok := administratorTokens(r, requireCSRF)
	if !ok {
		if sessionCookie, err := r.Cookie(adminSessionCookieName); requireCSRF && err == nil && sessionCookie.Value != "" {
			writeJSON(w, http.StatusForbidden, openapi.ApiError{Code: "CSRF_INVALID", Retryable: false})
		} else {
			clearAdministratorCookies(w, r)
			writeJSON(w, http.StatusUnauthorized, openapi.ApiError{Code: "AUTH_SESSION_UNAUTHORIZED", Retryable: false})
		}
		return authapp.Session{}, false
	}
	session, err := server.auth.Authenticate(r.Context(), token, csrfToken, requireCSRF)
	if err != nil {
		server.writeAuthenticationError(w, err, "administrator session validation failed")
		return authapp.Session{}, false
	}
	return session, true
}

func (server *Server) writeAuthenticationError(w http.ResponseWriter, err error, message string) {
	switch {
	case errors.Is(err, authapp.ErrInvalidCredentials):
		writeJSON(w, http.StatusUnauthorized, openapi.ApiError{Code: "LOGIN_INVALID", Retryable: false})
	case errors.Is(err, authapp.ErrUnauthorized):
		writeJSON(w, http.StatusUnauthorized, openapi.ApiError{Code: "AUTH_SESSION_UNAUTHORIZED", Retryable: false})
	case errors.Is(err, authapp.ErrCSRFInvalid):
		writeJSON(w, http.StatusForbidden, openapi.ApiError{Code: "CSRF_INVALID", Retryable: false})
	case errors.Is(err, authapp.ErrLoginRateLimited):
		writeJSON(w, http.StatusTooManyRequests, openapi.ApiError{Code: "LOGIN_RATE_LIMITED", Retryable: true})
	case errors.Is(err, authapp.ErrInstanceNotReady):
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "INSTANCE_NOT_READY", Retryable: false})
	case errors.Is(err, authapp.ErrPasswordRequestInvalid):
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PASSWORD_REQUEST_INVALID", Retryable: false})
	case errors.Is(err, authapp.ErrCurrentPasswordInvalid):
		writeJSON(w, http.StatusUnauthorized, openapi.ApiError{Code: "CURRENT_PASSWORD_INVALID", Retryable: false})
	default:
		server.logger.Error(message, "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "AUTH_UNAVAILABLE", Retryable: true})
	}
}

func (server *Server) Login(w http.ResponseWriter, r *http.Request) {
	if server.auth == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "AUTH_UNAVAILABLE", Retryable: true})
		return
	}
	var request openapi.LoginRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "LOGIN_REQUEST_INVALID", Retryable: false})
		return
	}
	result, err := server.auth.Login(r.Context(), request.Username, request.Password)
	if err != nil {
		server.writeAuthenticationError(w, err, "administrator login failed")
		return
	}
	setAdministratorCookies(w, r, result.SessionToken, result.CSRFToken, result.ExpiresAt)
	if server.setup != nil {
		status, statusErr := server.setup.Status(r.Context())
		if statusErr != nil {
			server.logger.ErrorContext(r.Context(), "read setup status after login failed", "error", statusErr)
			writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "SETUP_STATUS_UNAVAILABLE", Retryable: true})
			return
		}
		if status.SetupRequired {
			grant, grantErr := server.setup.BeginAdministratorSetup(r.Context())
			if grantErr != nil {
				server.logger.ErrorContext(r.Context(), "create administrator setup session failed", "error", grantErr)
				writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "SETUP_SESSION_UNAVAILABLE", Retryable: true})
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name: setupSessionCookieName, Value: grant.Token, Path: "/api/v1/setup", HttpOnly: true,
				Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, Expires: grant.ExpiresAt,
			})
		}
	}
	writeJSON(w, http.StatusOK, authSessionResponse(result.User, result.ExpiresAt))
}

func (server *Server) GetAuthSession(w http.ResponseWriter, r *http.Request) {
	session, ok := server.requireAdministrator(w, r, false)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, authSessionResponse(session.User, session.ExpiresAt))
}

func (server *Server) Logout(w http.ResponseWriter, r *http.Request) {
	if server.auth == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "AUTH_UNAVAILABLE", Retryable: true})
		return
	}
	token, csrfToken, ok := administratorTokens(r, true)
	if !ok {
		clearAdministratorCookies(w, r)
		writeJSON(w, http.StatusUnauthorized, openapi.ApiError{Code: "AUTH_SESSION_UNAUTHORIZED", Retryable: false})
		return
	}
	if err := server.auth.Logout(r.Context(), token, csrfToken); err != nil {
		server.writeAuthenticationError(w, err, "administrator logout failed")
		return
	}
	clearAdministratorCookies(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (server *Server) ChangeAdministratorPassword(w http.ResponseWriter, r *http.Request) {
	if server.auth == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "AUTH_UNAVAILABLE", Retryable: true})
		return
	}
	token, csrfToken, ok := administratorTokens(r, true)
	if !ok {
		writeJSON(w, http.StatusForbidden, openapi.ApiError{Code: "CSRF_INVALID", Retryable: false})
		return
	}
	var request openapi.ChangeAdministratorPasswordRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PASSWORD_REQUEST_INVALID", Retryable: false})
		return
	}
	passwordAuth, ok := server.auth.(PasswordAuthenticator)
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "AUTH_UNAVAILABLE", Retryable: true})
		return
	}
	if err := passwordAuth.ChangePassword(r.Context(), token, csrfToken, request.CurrentPassword, request.NewPassword, request.NewPasswordConfirmation); err != nil {
		server.writeAuthenticationError(w, err, "administrator password replacement failed")
		return
	}
	clearAdministratorCookies(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (server *Server) GetInventory(w http.ResponseWriter, r *http.Request) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	snapshot, err := server.inventory.Snapshot(r.Context())
	if err != nil {
		server.logger.ErrorContext(r.Context(), "inventory snapshot failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{
			Code:      "INVENTORY_SNAPSHOT_UNAVAILABLE",
			Retryable: true,
		})
		return
	}
	writeJSON(w, http.StatusOK, inventoryResponse(snapshot))
}

func (server *Server) GetMihomoCoreStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r, false); !ok {
		return
	}
	if server.mihomoCore == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MIHOMO_CORE_UNAVAILABLE", Retryable: true})
		return
	}
	status, err := server.mihomoCore.Status()
	if err != nil {
		server.logger.ErrorContext(r.Context(), "read Mihomo core status failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MIHOMO_CORE_STATUS_FAILED", Retryable: true})
		return
	}
	writeJSON(w, http.StatusOK, mihomoCoreStatusResponse(status))
}

func (server *Server) GetMihomoDashboardStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r, false); !ok {
		return
	}
	if server.mihomoDashboard == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MIHOMO_DASHBOARD_UNAVAILABLE", Retryable: true})
		return
	}
	status, err := server.mihomoDashboard.Ensure()
	if err != nil {
		server.logger.ErrorContext(r.Context(), "read Mihomo dashboard status failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MIHOMO_DASHBOARD_STATUS_FAILED", Retryable: true})
		return
	}
	writeJSON(w, http.StatusOK, openapi.MihomoDashboardStatus{Available: status.Available, Version: status.Version, ControllerAddress: status.ControllerAddress, Url: status.URL, Secret: status.Secret})
}

func (server *Server) GetLatestMihomoCore(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r, false); !ok {
		return
	}
	if server.mihomoCore == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MIHOMO_CORE_UNAVAILABLE", Retryable: true})
		return
	}
	candidate, err := server.mihomoCore.CheckLatest(r.Context())
	if err != nil {
		server.logger.WarnContext(r.Context(), "check official Mihomo release failed", "error", err)
		writeJSON(w, http.StatusBadGateway, openapi.ApiError{Code: "MIHOMO_RELEASE_CHECK_FAILED", Retryable: true})
		return
	}
	writeJSON(w, http.StatusOK, openapi.MihomoCoreCandidate{Version: candidate.Version, AssetName: candidate.AssetName, Sha256: candidate.SHA256, Size: candidate.Size, Architecture: openapi.MihomoCoreCandidateArchitecture(candidate.Architecture)})
}

func (server *Server) InstallLatestMihomoCore(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r, true); !ok {
		return
	}
	if server.mihomoCore == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MIHOMO_CORE_UNAVAILABLE", Retryable: true})
		return
	}
	status, err := server.mihomoCore.InstallLatest(r.Context())
	if errors.Is(err, mihomoapp.ErrVersionAlreadyInstalled) {
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "MIHOMO_VERSION_ALREADY_INSTALLED", Retryable: false})
		return
	}
	if err != nil {
		server.logger.WarnContext(r.Context(), "install official Mihomo core failed", "error", err)
		writeJSON(w, http.StatusBadGateway, openapi.ApiError{Code: "MIHOMO_CORE_INSTALL_FAILED", Retryable: true})
		return
	}
	server.publish([]realtime.Topic{realtime.TopicMihomo}, "")
	writeJSON(w, http.StatusOK, mihomoCoreStatusResponse(status))
}

func mihomoCoreStatusResponse(status mihomoapp.CoreStatus) openapi.MihomoCoreStatus {
	installedAt := ""
	if !status.InstalledAt.IsZero() {
		installedAt = status.InstalledAt.UTC().Format(time.RFC3339)
	}
	return openapi.MihomoCoreStatus{Installed: status.Installed, Version: status.Version, Architecture: openapi.MihomoCoreStatusArchitecture(status.Architecture), Sha256: status.SHA256, InstalledAt: installedAt}
}

func (server *Server) ListMihomoSubscriptions(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r, false); !ok {
		return
	}
	if server.mihomoSubscriptions == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MIHOMO_SUBSCRIPTIONS_UNAVAILABLE", Retryable: true})
		return
	}
	items, err := server.mihomoSubscriptions.List(r.Context())
	if err != nil {
		server.writeMihomoSubscriptionError(w, r, err)
		return
	}
	response := make([]openapi.MihomoSubscription, 0, len(items))
	for _, item := range items {
		response = append(response, mihomoSubscriptionResponse(item))
	}
	writeJSON(w, http.StatusOK, openapi.MihomoSubscriptionList{Subscriptions: response})
}

func (server *Server) CreateMihomoSubscription(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r, true); !ok {
		return
	}
	if server.mihomoSubscriptions == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MIHOMO_SUBSCRIPTIONS_UNAVAILABLE", Retryable: true})
		return
	}
	var request openapi.MihomoSubscriptionCreateRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MIHOMO_SUBSCRIPTION_REQUEST_INVALID", Retryable: false})
		return
	}
	item, err := server.mihomoSubscriptions.Create(r.Context(), "", request.Url, true)
	if err != nil {
		server.writeMihomoSubscriptionError(w, r, err)
		return
	}
	item, _, err = server.mihomoSubscriptions.Refresh(r.Context(), item.ID)
	if err != nil {
		server.writeMihomoSubscriptionError(w, r, err)
		return
	}
	server.publish([]realtime.Topic{realtime.TopicMihomo}, "")
	writeJSON(w, http.StatusCreated, mihomoSubscriptionResponse(item))
}

func (server *Server) UpdateMihomoSubscription(w http.ResponseWriter, r *http.Request, subscriptionID string) {
	if _, ok := server.requireAdministrator(w, r, true); !ok {
		return
	}
	if server.mihomoSubscriptions == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MIHOMO_SUBSCRIPTIONS_UNAVAILABLE", Retryable: true})
		return
	}
	var request openapi.MihomoSubscriptionMutation
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MIHOMO_SUBSCRIPTION_REQUEST_INVALID", Retryable: false})
		return
	}
	item, err := server.mihomoSubscriptions.Update(r.Context(), subscriptionID, request.DisplayName, request.Url, request.Enabled)
	if err != nil {
		server.writeMihomoSubscriptionError(w, r, err)
		return
	}
	if strings.TrimSpace(request.Url) != "" {
		item, _, err = server.mihomoSubscriptions.Refresh(r.Context(), subscriptionID)
		if err != nil {
			server.writeMihomoSubscriptionError(w, r, err)
			return
		}
	}
	server.publish([]realtime.Topic{realtime.TopicMihomo}, "")
	writeJSON(w, http.StatusOK, mihomoSubscriptionResponse(item))
}

func (server *Server) DeleteMihomoSubscription(w http.ResponseWriter, r *http.Request, subscriptionID string) {
	if _, ok := server.requireAdministrator(w, r, true); !ok {
		return
	}
	if server.mihomoSubscriptions == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MIHOMO_SUBSCRIPTIONS_UNAVAILABLE", Retryable: true})
		return
	}
	if err := server.mihomoSubscriptions.Delete(r.Context(), subscriptionID); err != nil {
		server.writeMihomoSubscriptionError(w, r, err)
		return
	}
	server.publish([]realtime.Topic{realtime.TopicMihomo}, "")
	w.WriteHeader(http.StatusNoContent)
}

func (server *Server) RefreshMihomoSubscription(w http.ResponseWriter, r *http.Request, subscriptionID string) {
	if _, ok := server.requireAdministrator(w, r, true); !ok {
		return
	}
	if server.mihomoSubscriptions == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MIHOMO_SUBSCRIPTIONS_UNAVAILABLE", Retryable: true})
		return
	}
	item, nodes, err := server.mihomoSubscriptions.Refresh(r.Context(), subscriptionID)
	if err != nil {
		server.writeMihomoSubscriptionError(w, r, err)
		return
	}
	server.publish([]realtime.Topic{realtime.TopicMihomo}, "")
	writeJSON(w, http.StatusOK, openapi.MihomoSubscriptionRefresh{Subscription: mihomoSubscriptionResponse(item), Nodes: mihomoNodeResponses(nodes)})
}

func (server *Server) ListMihomoSubscriptionNodes(w http.ResponseWriter, r *http.Request, subscriptionID string) {
	if _, ok := server.requireAdministrator(w, r, false); !ok {
		return
	}
	if server.mihomoSubscriptions == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MIHOMO_SUBSCRIPTIONS_UNAVAILABLE", Retryable: true})
		return
	}
	nodes, err := server.mihomoSubscriptions.Nodes(r.Context(), subscriptionID)
	if err != nil {
		server.writeMihomoSubscriptionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, openapi.MihomoNodeList{Nodes: mihomoNodeResponses(nodes)})
}

func (server *Server) writeMihomoSubscriptionError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, mihomoapp.ErrSubscriptionInvalid):
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MIHOMO_SUBSCRIPTION_REQUEST_INVALID", Retryable: false})
	case errors.Is(err, mihomoapp.ErrSubscriptionNotFound):
		writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "MIHOMO_SUBSCRIPTION_NOT_FOUND", Retryable: false})
	default:
		server.logger.WarnContext(r.Context(), "Mihomo subscription operation failed", "error", err)
		if strings.Contains(err.Error(), "fetch Mihomo subscription") || strings.Contains(err.Error(), "subscription response") || errors.Is(err, context.DeadlineExceeded) {
			writeJSON(w, http.StatusBadGateway, openapi.ApiError{Code: "MIHOMO_SUBSCRIPTION_REFRESH_FAILED", Retryable: true})
			return
		}
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MIHOMO_SUBSCRIPTION_PERSIST_FAILED", Retryable: true})
	}
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

func (server *Server) ListLineEgressBindings(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r, false); !ok {
		return
	}
	if server.lineEgress == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "LINE_EGRESS_UNAVAILABLE", Retryable: true})
		return
	}
	items, err := server.lineEgress.List(r.Context())
	if err != nil {
		server.logger.WarnContext(r.Context(), "line egress list failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "LINE_EGRESS_READ_FAILED", Retryable: true})
		return
	}
	bindings := make([]openapi.LineEgressBinding, 0, len(items))
	for _, item := range items {
		bindings = append(bindings, lineEgressResponse(item))
	}
	writeJSON(w, http.StatusOK, openapi.LineEgressBindingList{Bindings: bindings})
}

func (server *Server) PutLineEgressBinding(w http.ResponseWriter, r *http.Request, lineID string) {
	if _, ok := server.requireAdministrator(w, r, true); !ok {
		return
	}
	if server.lineEgress == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "LINE_EGRESS_UNAVAILABLE", Retryable: true})
		return
	}
	var request openapi.LineEgressBindingMutation
	if err := decodeJSON(w, r, &request); err != nil || !request.Mode.Valid() {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "LINE_EGRESS_REQUEST_INVALID", Retryable: false})
		return
	}
	item, err := server.lineEgress.Put(r.Context(), lineID, string(request.Mode), request.CountryCode)
	if err != nil {
		switch {
		case errors.Is(err, lineegressapp.ErrInvalidBinding):
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "LINE_EGRESS_REQUEST_INVALID", Retryable: false})
		case errors.Is(err, lineegressapp.ErrLineNotFound):
			writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "LINE_NOT_FOUND", Retryable: false})
		case errors.Is(err, lineegressapp.ErrLineUnsupported):
			writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "LINE_VOWIFI_UNSUPPORTED", Retryable: false})
		default:
			server.logger.WarnContext(r.Context(), "line egress update failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "LINE_EGRESS_PERSIST_FAILED", Retryable: true})
		}
		return
	}
	server.publish([]realtime.Topic{realtime.TopicLines, realtime.TopicMihomo, realtime.TopicVoWiFi}, "")
	writeJSON(w, http.StatusOK, lineEgressResponse(item))
}

func lineEgressResponse(item lineegressapp.View) openapi.LineEgressBinding {
	return openapi.LineEgressBinding{
		LineId: item.LineID, Mode: openapi.LineEgressBindingMode(item.Mode), CountryCode: item.CountryCode,
		CountryName: item.CountryName, ListenerPort: item.ListenerPort, Ready: item.Ready,
		ReadinessReason: openapi.LineEgressBindingReadinessReason(item.ReadinessReason),
	}
}

func (server *Server) ListVoWiFiLines(w http.ResponseWriter, r *http.Request) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.vowifi == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "VOWIFI_UNAVAILABLE", Retryable: true})
		return
	}
	items, err := server.vowifi.List(r.Context())
	if err != nil {
		server.logger.WarnContext(r.Context(), "Host VoWiFi state list failed", "error", err)
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "VOWIFI_STATUS_UNAVAILABLE", Retryable: true})
		return
	}
	lines := make([]openapi.VoWiFiLineState, 0, len(items))
	for _, item := range items {
		lines = append(lines, voWiFiStateResponse(item))
	}
	writeJSON(w, http.StatusOK, openapi.VoWiFiLineStateList{Lines: lines})
}

func (server *Server) ActivateVoWiFiLine(w http.ResponseWriter, r *http.Request, lineID string) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.vowifi == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "VOWIFI_UNAVAILABLE", Retryable: true})
		return
	}
	state, err := server.vowifi.Activate(r.Context(), lineID)
	if err != nil {
		server.writeVoWiFiError(w, r, err, lineID, true)
		return
	}
	server.publish([]realtime.Topic{realtime.TopicVoWiFi}, "")
	writeJSON(w, http.StatusAccepted, voWiFiStateResponse(state))
}

func (server *Server) DeactivateVoWiFiLine(w http.ResponseWriter, r *http.Request, lineID string) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.vowifi == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "VOWIFI_UNAVAILABLE", Retryable: true})
		return
	}
	state, err := server.vowifi.Deactivate(r.Context(), lineID)
	if err != nil {
		server.writeVoWiFiError(w, r, err, lineID, false)
		return
	}
	server.publish([]realtime.Topic{realtime.TopicVoWiFi}, "")
	writeJSON(w, http.StatusOK, voWiFiStateResponse(state))
}

func (server *Server) writeVoWiFiError(w http.ResponseWriter, r *http.Request, err error, lineID string, activating bool) {
	switch {
	case errors.Is(err, vowifiapp.ErrLineNotFound):
		writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "VOWIFI_LINE_NOT_FOUND", Retryable: false})
	case errors.Is(err, vowifiapp.ErrLineNotReady):
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "VOWIFI_LINE_NOT_READY", Retryable: true})
	default:
		action := "deactivation"
		if activating {
			action = "activation"
		}
		server.logger.WarnContext(r.Context(), "Host VoWiFi "+action+" failed", "line_id", lineID, "error", err)
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "VOWIFI_RUNTIME_FAILED", Retryable: true})
	}
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

func (server *Server) GetMihomoConfigStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r, false); !ok {
		return
	}
	if server.mihomoConfig == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MIHOMO_CONFIG_UNAVAILABLE", Retryable: true})
		return
	}
	status, err := server.mihomoConfig.Status(r.Context())
	if err != nil {
		server.logger.WarnContext(r.Context(), "Mihomo config status failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MIHOMO_CONFIG_STATUS_FAILED", Retryable: true})
		return
	}
	writeJSON(w, http.StatusOK, mihomoConfigResponse(status))
}

func (server *Server) PublishMihomoConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r, true); !ok {
		return
	}
	if server.mihomoConfig == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MIHOMO_CONFIG_UNAVAILABLE", Retryable: true})
		return
	}
	status, err := server.mihomoConfig.GenerateAndPublish(r.Context())
	if errors.Is(err, mihomoapp.ErrConfigNotReady) {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MIHOMO_CONFIG_NOT_READY", Retryable: false})
		return
	}
	if errors.Is(err, mihomoapp.ErrConfigValidationFailed) {
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "MIHOMO_CONFIG_VALIDATION_FAILED", Retryable: false})
		return
	}
	if err != nil {
		server.logger.WarnContext(r.Context(), "Mihomo config publish failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MIHOMO_CONFIG_PUBLISH_FAILED", Retryable: true})
		return
	}
	server.publish([]realtime.Topic{realtime.TopicMihomo}, "")
	writeJSON(w, http.StatusOK, mihomoConfigResponse(status))
}

func (server *Server) SelectMihomoSubscription(w http.ResponseWriter, r *http.Request, subscriptionID string) {
	if _, ok := server.requireAdministrator(w, r, true); !ok {
		return
	}
	if server.mihomoConfig == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MIHOMO_CONFIG_UNAVAILABLE", Retryable: true})
		return
	}
	status, err := server.mihomoConfig.Select(r.Context(), subscriptionID)
	if errors.Is(err, mihomoapp.ErrConfigNotReady) {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MIHOMO_SUBSCRIPTION_ARTIFACT_NOT_READY", Retryable: false})
		return
	}
	if errors.Is(err, mihomoapp.ErrConfigValidationFailed) {
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "MIHOMO_CONFIG_VALIDATION_FAILED", Retryable: false})
		return
	}
	if err != nil {
		server.logger.WarnContext(r.Context(), "Mihomo subscription selection failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MIHOMO_SUBSCRIPTION_SELECT_FAILED", Retryable: true})
		return
	}
	server.publish([]realtime.Topic{realtime.TopicMihomo, realtime.TopicVoWiFi}, "")
	writeJSON(w, http.StatusOK, mihomoConfigResponse(status))
}

func mihomoConfigResponse(status mihomoapp.ConfigStatus) openapi.MihomoConfigStatus {
	generatedAt := ""
	if !status.GeneratedAt.IsZero() {
		generatedAt = status.GeneratedAt.UTC().Format(time.RFC3339)
	}
	return openapi.MihomoConfigStatus{Published: status.Published, Launchable: status.Launchable, Sha256: status.SHA256, GeneratedAt: generatedAt, ErrorCode: status.ErrorCode, SelectedSubscriptionId: status.SelectedSubscriptionID, RunningSubscriptionId: status.RunningSubscriptionID}
}

func (server *Server) GetMihomoRuntimeStatus(w http.ResponseWriter, r *http.Request) {
	server.handleMihomoRuntime(w, r, "status")
}
func (server *Server) StartMihomo(w http.ResponseWriter, r *http.Request) {
	server.handleMihomoRuntime(w, r, "start")
}
func (server *Server) RestartMihomo(w http.ResponseWriter, r *http.Request) {
	server.handleMihomoRuntime(w, r, "restart")
}
func (server *Server) StopMihomo(w http.ResponseWriter, r *http.Request) {
	server.handleMihomoRuntime(w, r, "stop")
}
func (server *Server) handleMihomoRuntime(w http.ResponseWriter, r *http.Request, action string) {
	mutation := action != "status"
	if _, ok := server.requireAdministrator(w, r, mutation); !ok {
		return
	}
	if server.mihomoRuntime == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MIHOMO_RUNTIME_UNAVAILABLE", Retryable: true})
		return
	}
	var status mihomoapp.RuntimeStatus
	var err error
	switch action {
	case "status":
		status, err = server.mihomoRuntime.Status(r.Context())
	case "start":
		status, err = server.mihomoRuntime.Start(r.Context())
	case "restart":
		status, err = server.mihomoRuntime.Restart(r.Context())
	case "stop":
		status, err = server.mihomoRuntime.Stop(r.Context())
	}
	if errors.Is(err, mihomoapp.ErrConfigNotReady) || errors.Is(err, mihomoapp.ErrConfigValidationFailed) || errors.Is(err, mihomoapp.ErrRuntimeAlreadyRunning) || errors.Is(err, mihomoapp.ErrRuntimeNotRunning) || errors.Is(err, mihomoapp.ErrRuntimeStartupFailed) {
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "MIHOMO_RUNTIME_STATE_CONFLICT", Retryable: false})
		return
	}
	if err != nil {
		server.logger.WarnContext(r.Context(), "Mihomo runtime operation failed", "action", action, "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MIHOMO_RUNTIME_OPERATION_FAILED", Retryable: true})
		return
	}
	if mutation {
		server.publish([]realtime.Topic{realtime.TopicMihomo, realtime.TopicVoWiFi}, "")
	}
	writeJSON(w, http.StatusOK, mihomoRuntimeResponse(status))
}
func mihomoRuntimeResponse(status mihomoapp.RuntimeStatus) openapi.MihomoRuntimeStatus {
	startedAt := ""
	if !status.StartedAt.IsZero() {
		startedAt = status.StartedAt.UTC().Format(time.RFC3339)
	}
	return openapi.MihomoRuntimeStatus{State: openapi.MihomoRuntimeStatusState(status.State), Pid: status.PID, SelectedSubscriptionId: status.SelectedSubscriptionID, RunningSubscriptionId: status.RunningSubscriptionID, PendingRestart: status.PendingRestart, StartedAt: startedAt, LastErrorCode: status.LastErrorCode}
}

func (server *Server) ListNotificationChannels(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r, false); !ok {
		return
	}
	if server.notifications == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "NOTIFICATIONS_UNAVAILABLE", Retryable: true})
		return
	}
	items, err := server.notifications.List(r.Context())
	if err != nil {
		server.writeNotificationError(w, r, err)
		return
	}
	response := make([]openapi.NotificationChannel, 0, len(items))
	for _, item := range items {
		response = append(response, notificationChannelResponse(item))
	}
	writeJSON(w, http.StatusOK, openapi.NotificationChannelList{Channels: response})
}
func (server *Server) CreateNotificationChannel(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r, true); !ok {
		return
	}
	if server.notifications == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "NOTIFICATIONS_UNAVAILABLE", Retryable: true})
		return
	}
	var request openapi.NotificationChannelMutation
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "NOTIFICATION_CHANNEL_REQUEST_INVALID", Retryable: false})
		return
	}
	item, err := server.notifications.Create(r.Context(), string(request.Provider), request.DisplayName, request.WebhookUrl, request.SigningSecret, request.Enabled, notificationEventStrings(request.EventKinds))
	if err != nil {
		server.writeNotificationError(w, r, err)
		return
	}
	server.publish([]realtime.Topic{realtime.TopicNotifications}, "")
	writeJSON(w, http.StatusCreated, notificationChannelResponse(item))
}
func (server *Server) GetFeishuNotificationBinding(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r, false); !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if server.notifications == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "FEISHU_BINDING_UNAVAILABLE", Retryable: true})
		return
	}
	writeJSON(w, http.StatusOK, feishuBindingResponse(server.notifications.FeishuBindingStatus()))
}
func (server *Server) StartFeishuNotificationBinding(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r, true); !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if server.notifications == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "FEISHU_BINDING_UNAVAILABLE", Retryable: true})
		return
	}
	state, err := server.notifications.StartFeishuBinding(r.Context())
	if err != nil {
		server.writeFeishuBindingError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, feishuBindingResponse(state))
}
func (server *Server) CancelFeishuNotificationBinding(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r, true); !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if server.notifications == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "FEISHU_BINDING_UNAVAILABLE", Retryable: true})
		return
	}
	state, err := server.notifications.CancelFeishuBinding()
	if err != nil {
		server.writeFeishuBindingError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, feishuBindingResponse(state))
}
func (server *Server) writeFeishuBindingError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, notificationapp.ErrBindingActive):
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "FEISHU_BINDING_ACTIVE", Retryable: false})
	case errors.Is(err, notificationapp.ErrBindingNotCancelable):
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "FEISHU_BINDING_NOT_CANCELLABLE", Retryable: false})
	case errors.Is(err, notificationapp.ErrBindingUnavailable):
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "FEISHU_BINDING_UNAVAILABLE", Retryable: true})
	case errors.Is(err, notificationapp.ErrFeishuProviderResultInvalid):
		writeJSON(w, http.StatusBadGateway, openapi.ApiError{Code: notificationapp.BindingErrorResultInvalid, Retryable: false})
	default:
		server.logger.WarnContext(r.Context(), "Feishu binding start failed")
		writeJSON(w, http.StatusBadGateway, openapi.ApiError{Code: notificationapp.BindingErrorProviderFailed, Retryable: true})
	}
}
func (server *Server) UpdateNotificationChannel(w http.ResponseWriter, r *http.Request, channelID string) {
	if _, ok := server.requireAdministrator(w, r, true); !ok {
		return
	}
	if server.notifications == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "NOTIFICATIONS_UNAVAILABLE", Retryable: true})
		return
	}
	var request openapi.NotificationChannelMutation
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "NOTIFICATION_CHANNEL_REQUEST_INVALID", Retryable: false})
		return
	}
	item, err := server.notifications.Update(r.Context(), channelID, string(request.Provider), request.DisplayName, request.WebhookUrl, request.SigningSecret, request.Enabled, notificationEventStrings(request.EventKinds))
	if err != nil {
		server.writeNotificationError(w, r, err)
		return
	}
	server.publish([]realtime.Topic{realtime.TopicNotifications}, "")
	writeJSON(w, http.StatusOK, notificationChannelResponse(item))
}
func (server *Server) DeleteNotificationChannel(w http.ResponseWriter, r *http.Request, channelID string) {
	if _, ok := server.requireAdministrator(w, r, true); !ok {
		return
	}
	if server.notifications == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "NOTIFICATIONS_UNAVAILABLE", Retryable: true})
		return
	}
	if err := server.notifications.Delete(r.Context(), channelID); err != nil {
		server.writeNotificationError(w, r, err)
		return
	}
	server.publish([]realtime.Topic{realtime.TopicNotifications}, "")
	w.WriteHeader(http.StatusNoContent)
}
func (server *Server) TestNotificationChannel(w http.ResponseWriter, r *http.Request, channelID string) {
	if _, ok := server.requireAdministrator(w, r, true); !ok {
		return
	}
	if server.notifications == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "NOTIFICATIONS_UNAVAILABLE", Retryable: true})
		return
	}
	item, err := server.notifications.Test(r.Context(), channelID)
	if err != nil {
		server.writeNotificationError(w, r, err)
		return
	}
	server.publish([]realtime.Topic{realtime.TopicNotifications}, "")
	writeJSON(w, http.StatusOK, notificationChannelResponse(item))
}
func (server *Server) writeNotificationError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, notificationapp.ErrChannelInvalid):
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "NOTIFICATION_CHANNEL_REQUEST_INVALID", Retryable: false})
	case errors.Is(err, notificationapp.ErrChannelNotFound):
		writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "NOTIFICATION_CHANNEL_NOT_FOUND", Retryable: false})
	default:
		server.logger.WarnContext(r.Context(), "notification channel operation failed", "error", err)
		writeJSON(w, http.StatusBadGateway, openapi.ApiError{Code: "NOTIFICATION_DELIVERY_FAILED", Retryable: true})
	}
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
	return openapi.NotificationChannel{Id: item.ID, Provider: openapi.NotificationChannelProvider(item.Provider), DeliveryMode: openapi.NotificationChannelDeliveryMode(item.DeliveryMode), TargetType: openapi.NotificationChannelTargetType(item.TargetType), DisplayName: item.DisplayName, WebhookHint: openapi.NotificationChannelWebhookHint(item.WebhookHint), SigningSecretConfigured: item.SigningSecretConfigured, Enabled: item.Enabled, EventKinds: events, LastDeliveryAt: lastAt, LastDeliveryStatus: openapi.NotificationChannelLastDeliveryStatus(item.LastDeliveryStatus), LastErrorCode: item.LastErrorCode}
}

func feishuBindingResponse(state notificationapp.BindingView) openapi.FeishuNotificationBinding {
	expiresAt := ""
	if !state.ExpiresAt.IsZero() {
		expiresAt = state.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return openapi.FeishuNotificationBinding{State: openapi.FeishuNotificationBindingState(state.State), VerificationUrl: state.VerificationURL, ExpiresAt: expiresAt, ChannelId: state.ChannelID, ErrorCode: state.ErrorCode}
}

func (server *Server) ListMessages(w http.ResponseWriter, r *http.Request, params openapi.ListMessagesParams) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.messages == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MESSAGING_UNAVAILABLE", Retryable: true})
		return
	}
	request := messageapp.PageRequest{}
	if params.Limit != nil {
		if *params.Limit == 0 {
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_LIMIT_INVALID", Retryable: false})
			return
		}
		request.Limit = *params.Limit
	}
	if params.Cursor != nil {
		if *params.Cursor == "" {
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_CURSOR_INVALID", Retryable: false})
			return
		}
		request.Cursor = *params.Cursor
	} else if _, present := r.URL.Query()["cursor"]; present {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_CURSOR_INVALID", Retryable: false})
		return
	}
	linePresent := false
	if params.LineId != nil {
		request.LineID = *params.LineId
		linePresent = true
	} else if _, present := r.URL.Query()["lineId"]; present {
		linePresent = true
	}
	remotePresent := false
	if params.RemoteAddress != nil {
		request.RemoteAddress = *params.RemoteAddress
		remotePresent = true
	} else if _, present := r.URL.Query()["remoteAddress"]; present {
		remotePresent = true
	}
	if (linePresent && !remotePresent) || (linePresent && request.LineID == "") || (remotePresent && request.RemoteAddress == "") {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MESSAGE_FILTER_INVALID", Retryable: false})
		return
	}
	page, err := server.messages.ListPage(r.Context(), request)
	if err != nil {
		if errors.Is(err, pagination.ErrCursorInvalid) {
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_CURSOR_INVALID", Retryable: false})
			return
		}
		if errors.Is(err, pagination.ErrLimitInvalid) {
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_LIMIT_INVALID", Retryable: false})
			return
		}
		if errors.Is(err, messageapp.ErrRequestInvalid) {
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MESSAGE_FILTER_INVALID", Retryable: false})
			return
		}
		server.logger.ErrorContext(r.Context(), "message history read failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MESSAGE_HISTORY_UNAVAILABLE", Retryable: true})
		return
	}
	response := make([]openapi.SMSMessage, 0, len(page.Messages))
	for _, message := range page.Messages {
		response = append(response, smsMessageResponse(message))
	}
	stats, err := server.messages.Stats(r.Context())
	if err != nil {
		server.logger.ErrorContext(r.Context(), "message history stats failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MESSAGE_HISTORY_UNAVAILABLE", Retryable: true})
		return
	}
	result := openapi.SMSMessageListResponse{
		Messages: response, TotalCount: stats.TotalCount, Capacity: stats.Capacity, NearCapacity: stats.NearCapacity,
	}
	if page.NextCursor != "" {
		result.NextCursor = &page.NextCursor
	}
	if page.ReadThroughToken != "" {
		result.ReadThroughToken = &page.ReadThroughToken
	}
	writeJSON(w, http.StatusOK, result)
}

func (server *Server) ListMessageConversations(w http.ResponseWriter, r *http.Request, params openapi.ListMessageConversationsParams) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.messages == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MESSAGING_UNAVAILABLE", Retryable: true})
		return
	}
	limit := 0
	if params.Limit != nil {
		if *params.Limit == 0 {
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_LIMIT_INVALID", Retryable: false})
			return
		}
		limit = *params.Limit
	}
	cursor := ""
	if params.Cursor != nil {
		if *params.Cursor == "" {
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_CURSOR_INVALID", Retryable: false})
			return
		}
		cursor = *params.Cursor
	} else if _, present := r.URL.Query()["cursor"]; present {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_CURSOR_INVALID", Retryable: false})
		return
	}
	page, err := server.messages.ListConversationPage(r.Context(), limit, cursor)
	if err != nil {
		switch {
		case errors.Is(err, pagination.ErrCursorInvalid):
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_CURSOR_INVALID", Retryable: false})
		case errors.Is(err, pagination.ErrLimitInvalid):
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_LIMIT_INVALID", Retryable: false})
		default:
			server.logger.ErrorContext(r.Context(), "message conversation read failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MESSAGE_HISTORY_UNAVAILABLE", Retryable: true})
		}
		return
	}
	stats, err := server.messages.Stats(r.Context())
	if err != nil {
		server.logger.ErrorContext(r.Context(), "message conversation stats failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MESSAGE_HISTORY_UNAVAILABLE", Retryable: true})
		return
	}
	conversations := make([]openapi.SMSConversationSummary, 0, len(page.Conversations))
	for _, item := range page.Conversations {
		response := openapi.SMSConversationSummary{
			RemoteAddress: item.RemoteAddress,
			LastMessage:   smsMessageResponse(item.LastMessage),
			UnreadCount:   item.UnreadCount,
		}
		if item.LastOutboundLineID != "" {
			response.LastOutboundLineId = &item.LastOutboundLineID
		}
		conversations = append(conversations, response)
	}
	result := openapi.SMSConversationListResponse{
		Conversations: conversations, ConversationTotalCount: page.TotalCount,
		MessageTotalCount: stats.TotalCount, Capacity: stats.Capacity, NearCapacity: stats.NearCapacity,
	}
	if page.NextCursor != "" {
		result.NextCursor = &page.NextCursor
	}
	writeJSON(w, http.StatusOK, result)
}

func (server *Server) MarkMessageConversationRead(w http.ResponseWriter, r *http.Request) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.messages == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MESSAGING_UNAVAILABLE", Retryable: true})
		return
	}
	var request openapi.MarkSMSConversationReadRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MESSAGE_READ_STATE_INVALID", Retryable: false})
		return
	}
	changed, err := server.messages.MarkConversationRead(r.Context(), request.RemoteAddress, request.ReadThroughToken)
	if err != nil {
		switch {
		case errors.Is(err, messageapp.ErrRequestInvalid):
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MESSAGE_READ_STATE_INVALID", Retryable: false})
		case errors.Is(err, sms.ErrMessageNotFound):
			writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "MESSAGE_READ_BOUNDARY_NOT_FOUND", Retryable: false})
		default:
			server.logger.ErrorContext(r.Context(), "message read state update failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MESSAGE_PERSIST_FAILED", Retryable: true})
		}
		return
	}
	if changed {
		server.publish([]realtime.Topic{realtime.TopicMessages}, "")
	}
	w.WriteHeader(http.StatusNoContent)
}

func (server *Server) DeleteMessage(w http.ResponseWriter, r *http.Request, messageID string) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.messages == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MESSAGING_UNAVAILABLE", Retryable: true})
		return
	}
	if err := server.messages.Delete(r.Context(), messageID); err != nil {
		switch {
		case errors.Is(err, messageapp.ErrRequestInvalid):
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MESSAGE_REQUEST_INVALID", Retryable: false})
		case errors.Is(err, sms.ErrMessageNotFound):
			writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "MESSAGE_NOT_FOUND", Retryable: false})
		default:
			server.logger.ErrorContext(r.Context(), "message deletion failed", "message_id", messageID, "error", err)
			writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MESSAGE_PERSIST_FAILED", Retryable: true})
		}
		return
	}
	server.publish([]realtime.Topic{realtime.TopicMessages}, "")
	w.WriteHeader(http.StatusNoContent)
}

func (server *Server) SendMessage(w http.ResponseWriter, r *http.Request) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.messages == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MESSAGING_UNAVAILABLE", Retryable: true})
		return
	}
	var request openapi.SendSMSRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MESSAGE_REQUEST_INVALID", Retryable: false})
		return
	}
	result, err := server.messages.Send(r.Context(), messageapp.SendRequest{
		OperationID: request.OperationId,
		LineID:      request.LineId,
		Destination: request.Destination,
		Body:        request.Body,
	})
	if err != nil {
		server.writeMessageError(w, r, err, request.OperationId, request.LineId)
		return
	}
	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	if !result.Replayed && result.Message.Status == sms.StatusFailed {
		server.notifyAsync("sms.failed", fmt.Sprintf("[Simplus] 短信发送失败 · 线路 %s · %s", result.Message.LineID, result.Message.ErrorCode))
	}
	server.publish([]realtime.Topic{realtime.TopicMessages}, "")
	writeJSON(w, status, smsMessageResponse(result.Message))
}

func (server *Server) ListCalls(w http.ResponseWriter, r *http.Request, params openapi.ListCallsParams) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.calls == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "CALLS_UNAVAILABLE", Retryable: false})
		return
	}
	limit := 0
	cursor := ""
	if params.Limit != nil {
		if *params.Limit == 0 {
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_LIMIT_INVALID", Retryable: false})
			return
		}
		limit = *params.Limit
	}
	if params.Cursor != nil {
		if *params.Cursor == "" {
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_CURSOR_INVALID", Retryable: false})
			return
		}
		cursor = *params.Cursor
	} else if _, present := r.URL.Query()["cursor"]; present {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_CURSOR_INVALID", Retryable: false})
		return
	}
	page, err := server.calls.List(r.Context(), limit, cursor)
	if err != nil {
		if errors.Is(err, pagination.ErrCursorInvalid) {
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_CURSOR_INVALID", Retryable: false})
			return
		}
		if errors.Is(err, pagination.ErrLimitInvalid) {
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_LIMIT_INVALID", Retryable: false})
			return
		}
		server.writeCallError(w, r, err)
		return
	}
	response := make([]openapi.Call, 0, len(page.Calls))
	for _, value := range page.Calls {
		response = append(response, callResponse(value))
	}
	result := openapi.CallListResponse{Calls: response}
	if page.NextCursor != "" {
		result.NextCursor = &page.NextCursor
	}
	writeJSON(w, http.StatusOK, result)
}

func (server *Server) GetEUICCState(w http.ResponseWriter, r *http.Request) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.euicc == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "EUICC_UNAVAILABLE", Retryable: false})
		return
	}
	state, err := server.euicc.State(r.Context())
	if err != nil {
		server.logger.ErrorContext(r.Context(), "eUICC state failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "EUICC_READ_FAILED", Retryable: true})
		return
	}
	writeJSON(w, http.StatusOK, euiccResponse(state))
}

func (server *Server) ActivateEUICCProfile(w http.ResponseWriter, r *http.Request, profileID string) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.euicc == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "EUICC_UNAVAILABLE", Retryable: false})
		return
	}
	state, err := server.euicc.Switch(r.Context(), profileID)
	if err != nil {
		switch {
		case errors.Is(err, euiccapp.ErrInvalid):
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "EUICC_REQUEST_INVALID", Retryable: false})
		case errors.Is(err, euiccapp.ErrNotFound):
			writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "EUICC_PROFILE_NOT_FOUND", Retryable: false})
		default:
			server.logger.ErrorContext(r.Context(), "eUICC switch failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "EUICC_SWITCH_FAILED", Retryable: true})
		}
		return
	}
	server.publish([]realtime.Topic{realtime.TopicEUICC}, "")
	writeJSON(w, http.StatusOK, euiccResponse(state))
}

func euiccResponse(state domaineuicc.State) openapi.EUICCState {
	profiles := make([]openapi.EUICCProfile, 0, len(state.Profiles))
	for _, profile := range state.Profiles {
		profiles = append(profiles, openapi.EUICCProfile{Id: profile.ID, DisplayName: profile.DisplayName, DisplayIdentityHint: profile.DisplayIdentityHint, Active: profile.Active})
	}
	return openapi.EUICCState{EidHint: state.EIDHint, Profiles: profiles}
}

func (server *Server) DialCall(w http.ResponseWriter, r *http.Request) { server.startCall(w, r, false) }
func (server *Server) SimulateIncomingCall(w http.ResponseWriter, r *http.Request) {
	server.startCall(w, r, true)
}
func (server *Server) startCall(w http.ResponseWriter, r *http.Request, incoming bool) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.calls == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "CALLS_UNAVAILABLE", Retryable: false})
		return
	}
	var request openapi.CallStartRequest
	if decodeJSON(w, r, &request) != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "CALL_REQUEST_INVALID", Retryable: false})
		return
	}
	var value call.Record
	var replayed bool
	var err error
	if incoming {
		value, replayed, err = server.calls.Incoming(r.Context(), request.OperationId, request.LineId, request.RemoteAddress)
	} else {
		value, replayed, err = server.calls.Dial(r.Context(), request.OperationId, request.LineId, request.RemoteAddress)
	}
	if err != nil {
		server.writeCallError(w, r, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	if incoming && !replayed {
		server.notifyAsync("call.incoming", fmt.Sprintf("[Simplus] 新来电 · 线路 %s", value.LineID))
	}
	attention := realtime.Attention("")
	if incoming && !replayed {
		attention = realtime.AttentionCallIncoming
	}
	server.publish([]realtime.Topic{realtime.TopicCalls}, attention)
	writeJSON(w, status, callResponse(value))
}

func (server *Server) ControlCall(w http.ResponseWriter, r *http.Request, callID string) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.calls == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "CALLS_UNAVAILABLE", Retryable: false})
		return
	}
	var request openapi.CallActionRequest
	if decodeJSON(w, r, &request) != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "CALL_REQUEST_INVALID", Retryable: false})
		return
	}
	var value call.Record
	var err error
	switch request.Action {
	case openapi.Answer:
		value, err = server.calls.Answer(r.Context(), callID)
	case openapi.Reject:
		value, err = server.calls.Reject(r.Context(), callID)
	case openapi.Hangup:
		value, err = server.calls.Hangup(r.Context(), callID)
	case openapi.Dtmf:
		if request.Digits == nil {
			err = callapp.ErrInvalid
		} else {
			value, err = server.calls.DTMF(r.Context(), callID, *request.Digits)
		}
	default:
		err = callapp.ErrInvalid
	}
	if err != nil {
		server.writeCallError(w, r, err)
		return
	}
	if request.Action == openapi.Reject && value.Direction == call.DirectionInbound {
		server.notifyAsync("call.missed", fmt.Sprintf("[Simplus] 未接来电 · 线路 %s", value.LineID))
	}
	server.publish([]realtime.Topic{realtime.TopicCalls}, "")
	writeJSON(w, http.StatusOK, callResponse(value))
}

func (server *Server) notifyAsync(event, message string) {
	if server == nil || server.notifications == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.notifications.Notify(ctx, event, message); err != nil {
			server.logger.Warn("notification delivery failed", "event", event, "error", err)
		}
		server.publish([]realtime.Topic{realtime.TopicNotifications}, "")
	}()
}

func (server *Server) writeCallError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, callapp.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "CALL_REQUEST_INVALID", Retryable: false})
	case errors.Is(err, callapp.ErrUnsafeNumber):
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "CALL_NUMBER_FORBIDDEN", Retryable: false})
	case errors.Is(err, callapp.ErrLineUnavailable):
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "CALL_LINE_UNAVAILABLE", Retryable: true})
	case errors.Is(err, callapp.ErrLineBusy):
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "CALL_LINE_BUSY", Retryable: true})
	case errors.Is(err, call.ErrNotFound):
		writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "CALL_NOT_FOUND", Retryable: false})
	case errors.Is(err, call.ErrStateConflict):
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "CALL_STATE_CONFLICT", Retryable: false})
	default:
		server.logger.ErrorContext(r.Context(), "call operation failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "CALL_PERSIST_FAILED", Retryable: true})
	}
}

func callResponse(value call.Record) openapi.Call {
	return openapi.Call{Id: value.ID, OperationId: value.OperationID, LineId: value.LineID, RemoteAddress: value.RemoteAddress, Direction: openapi.CallDirection(value.Direction), State: openapi.CallState(value.State), EndReason: value.EndReason, CreatedAt: value.CreatedAt.UTC(), UpdatedAt: value.UpdatedAt.UTC(), AnsweredAt: value.AnsweredAt, EndedAt: value.EndedAt}
}

func (server *Server) ListContacts(w http.ResponseWriter, r *http.Request) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.contacts == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "CONTACTS_UNAVAILABLE", Retryable: true})
		return
	}
	values, err := server.contacts.List(r.Context())
	if err != nil {
		server.logger.ErrorContext(r.Context(), "contacts read failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "CONTACTS_READ_FAILED", Retryable: true})
		return
	}
	response := make([]openapi.Contact, 0, len(values))
	for _, value := range values {
		response = append(response, contactResponse(value))
	}
	writeJSON(w, http.StatusOK, openapi.ContactListResponse{Contacts: response})
}

func (server *Server) CreateContact(w http.ResponseWriter, r *http.Request) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	var request openapi.ContactMutationRequest
	if server.contacts == nil || decodeJSON(w, r, &request) != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "CONTACT_REQUEST_INVALID", Retryable: false})
		return
	}
	value, err := server.contacts.Create(r.Context(), request.DisplayName, request.PhoneNumber)
	if err != nil {
		server.writeContactError(w, r, err)
		return
	}
	server.publish([]realtime.Topic{realtime.TopicContacts}, "")
	writeJSON(w, http.StatusCreated, contactResponse(value))
}

func (server *Server) UpdateContact(w http.ResponseWriter, r *http.Request, contactID string) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	var request openapi.ContactMutationRequest
	if server.contacts == nil || decodeJSON(w, r, &request) != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "CONTACT_REQUEST_INVALID", Retryable: false})
		return
	}
	value, err := server.contacts.Update(r.Context(), contactID, request.DisplayName, request.PhoneNumber)
	if err != nil {
		server.writeContactError(w, r, err)
		return
	}
	server.publish([]realtime.Topic{realtime.TopicContacts}, "")
	writeJSON(w, http.StatusOK, contactResponse(value))
}

func (server *Server) DeleteContact(w http.ResponseWriter, r *http.Request, contactID string) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.contacts == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "CONTACTS_UNAVAILABLE", Retryable: true})
		return
	}
	if err := server.contacts.Delete(r.Context(), contactID); err != nil {
		server.writeContactError(w, r, err)
		return
	}
	server.publish([]realtime.Topic{realtime.TopicContacts}, "")
	w.WriteHeader(http.StatusNoContent)
}

func (server *Server) writeContactError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, contactapp.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "CONTACT_REQUEST_INVALID", Retryable: false})
	case errors.Is(err, contact.ErrNotFound):
		writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "CONTACT_NOT_FOUND", Retryable: false})
	case errors.Is(err, contact.ErrPhoneConflict):
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "CONTACT_PHONE_CONFLICT", Retryable: false})
	default:
		server.logger.ErrorContext(r.Context(), "contact mutation failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "CONTACT_PERSIST_FAILED", Retryable: true})
	}
}

func contactResponse(value contact.Contact) openapi.Contact {
	return openapi.Contact{
		Id: value.ID, DisplayName: value.DisplayName, PhoneNumber: value.PhoneNumber,
		CreatedAt: value.CreatedAt.UTC(), UpdatedAt: value.UpdatedAt.UTC(),
	}
}

func (server *Server) writeMessageError(w http.ResponseWriter, r *http.Request, err error, operationID, lineID string) {
	switch {
	case errors.Is(err, messageapp.ErrRequestInvalid):
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MESSAGE_REQUEST_INVALID", Retryable: false})
	case errors.Is(err, sms.ErrOperationConflict):
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "MESSAGE_OPERATION_CONFLICT", Retryable: false})
	case errors.Is(err, messageapp.ErrLineNotFound):
		writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "MESSAGE_LINE_NOT_FOUND", Retryable: false})
	case errors.Is(err, messageapp.ErrLineUnavailable):
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "MESSAGE_LINE_UNAVAILABLE", Retryable: true})
	case errors.Is(err, messageapp.ErrLineUnsupported):
		writeJSON(w, http.StatusUnprocessableEntity, openapi.ApiError{Code: "MESSAGE_LINE_UNSUPPORTED", Retryable: false})
	case errors.Is(err, messageapp.ErrTransportUnavailable):
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MESSAGE_TRANSPORT_UNAVAILABLE", Retryable: true})
	case errors.Is(err, messageapp.ErrTransportAmbiguous):
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "MESSAGE_TRANSPORT_AMBIGUOUS", Retryable: false})
	case errors.Is(err, messageapp.ErrInventoryUnavailable):
		server.logger.ErrorContext(r.Context(), "message inventory unavailable", "operation_id", operationID, "line_id", lineID, "error", err)
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MESSAGE_INVENTORY_UNAVAILABLE", Retryable: true})
	default:
		server.logger.ErrorContext(r.Context(), "message operation failed", "operation_id", operationID, "line_id", lineID, "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MESSAGE_PERSIST_FAILED", Retryable: true})
	}
}

func (server *Server) ListManagedModems(w http.ResponseWriter, r *http.Request) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.modems == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MODEM_MANAGEMENT_UNAVAILABLE", Retryable: true})
		return
	}
	items, err := server.modems.List(r.Context())
	if err != nil {
		server.logger.ErrorContext(r.Context(), "managed modem list failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MODEM_LIST_UNAVAILABLE", Retryable: true})
		return
	}
	modems := make([]openapi.ManagedModem, 0, len(items))
	for _, item := range items {
		modems = append(modems, managedModemResponse(item))
	}
	writeJSON(w, http.StatusOK, openapi.ManagedModemList{Modems: modems})
}

func (server *Server) ListManagedLines(w http.ResponseWriter, r *http.Request) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.lines == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "LINE_MANAGEMENT_UNAVAILABLE", Retryable: true})
		return
	}
	items, err := server.lines.List(r.Context())
	if err != nil {
		server.logger.ErrorContext(r.Context(), "managed line list failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "LINE_LIST_UNAVAILABLE", Retryable: true})
		return
	}
	lines := make([]openapi.ManagedLine, 0, len(items))
	for _, item := range items {
		lines = append(lines, managedLineResponse(item))
	}
	writeJSON(w, http.StatusOK, openapi.ManagedLineList{Lines: lines})
}

func (server *Server) ListLineCandidates(w http.ResponseWriter, r *http.Request) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.lines == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "LINE_MANAGEMENT_UNAVAILABLE", Retryable: true})
		return
	}
	items, err := server.lines.Candidates(r.Context())
	if err != nil {
		server.logger.WarnContext(r.Context(), "managed line candidate resolution failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "LINE_CANDIDATES_UNAVAILABLE", Retryable: true})
		return
	}
	candidates := make([]openapi.LineCandidate, 0, len(items))
	for _, item := range items {
		candidates = append(candidates, openapi.LineCandidate{
			CandidateId: item.CandidateID, ManagedModemId: item.ManagedModemID,
			ManagedModemDisplayName: item.ManagedModemDisplayName,
			ManagedModemModel:       item.ManagedModemModel, ManagedModemSerialNumber: item.ManagedModemSerialNumber,
			SubscriptionDisplayHint: item.SubscriptionDisplayHint,
			HomeOperatorName:        item.HomeOperatorName, HomeOperatorCode: item.HomeOperatorCode,
			SimPresence:  openapi.ManagedModemSIMPresence(item.SIMPresence),
			Capabilities: hardwareCapabilitiesResponse(item.Capabilities), Addable: item.Addable,
			ReadinessReason: openapi.LineCandidateReadinessReason(item.Readiness),
		})
	}
	writeJSON(w, http.StatusOK, openapi.LineCandidateList{Candidates: candidates})
}

func (server *Server) AddManagedLine(w http.ResponseWriter, r *http.Request) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.lines == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "LINE_MANAGEMENT_UNAVAILABLE", Retryable: true})
		return
	}
	var request openapi.AddManagedLineRequest
	if decodeJSON(w, r, &request) != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "LINE_ADD_REQUEST_INVALID", Retryable: false})
		return
	}
	item, err := server.lines.Add(r.Context(), request.CandidateId, request.DisplayName)
	if err != nil {
		server.writeManagedLineError(w, r, err, true)
		return
	}
	server.publish([]realtime.Topic{realtime.TopicInventory, realtime.TopicLines}, "")
	w.Header().Set("Location", "/api/v1/lines/"+item.ID)
	writeJSON(w, http.StatusCreated, managedLineResponse(item))
}

func (server *Server) UpdateManagedLine(w http.ResponseWriter, r *http.Request, lineID string) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.lines == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "LINE_MANAGEMENT_UNAVAILABLE", Retryable: true})
		return
	}
	var request openapi.UpdateManagedLineRequest
	if decodeJSON(w, r, &request) != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "LINE_UPDATE_REQUEST_INVALID", Retryable: false})
		return
	}
	item, err := server.lines.Update(r.Context(), lineID, request.DisplayName)
	if err != nil {
		server.writeManagedLineError(w, r, err, false)
		return
	}
	server.publish([]realtime.Topic{realtime.TopicLines}, "")
	writeJSON(w, http.StatusOK, managedLineResponse(item))
}

func (server *Server) writeManagedLineError(w http.ResponseWriter, r *http.Request, err error, adding bool) {
	switch {
	case errors.Is(err, lineapp.ErrRequestInvalid):
		code := "LINE_UPDATE_REQUEST_INVALID"
		if adding {
			code = "LINE_ADD_REQUEST_INVALID"
		}
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: code, Retryable: false})
	case errors.Is(err, lineapp.ErrCandidateNotFound):
		writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "LINE_CANDIDATE_NOT_FOUND", Retryable: true})
	case errors.Is(err, linedomain.ErrNotFound):
		writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "LINE_NOT_FOUND", Retryable: false})
	case errors.Is(err, lineapp.ErrCandidateInvalid):
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "LINE_CANDIDATE_NOT_READY", Retryable: true})
	case errors.Is(err, lineapp.ErrAlreadyManaged):
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "LINE_ALREADY_ADDED", Retryable: false})
	default:
		server.logger.ErrorContext(r.Context(), "managed line mutation failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "LINE_PERSIST_FAILED", Retryable: true})
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

func (server *Server) ListModemCandidates(w http.ResponseWriter, r *http.Request) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.modems == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MODEM_MANAGEMENT_UNAVAILABLE", Retryable: true})
		return
	}
	items, err := server.modems.Candidates(r.Context())
	if err != nil {
		server.logger.WarnContext(r.Context(), "modem candidate scan failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MODEM_SCAN_FAILED", Retryable: true})
		return
	}
	candidates := make([]openapi.ModemCandidate, 0, len(items))
	for _, item := range items {
		candidates = append(candidates, openapi.ModemCandidate{
			CandidateId: item.CandidateID, UsbAddress: item.USBAddress,
			VendorId: item.USBVendorID, ProductId: item.USBProductID, UsbSerialHint: item.USBSerialHint,
			Model: item.Model, Transport: openapi.DeviceTransport(item.Transport),
			SupportStatus: openapi.ModemSupportStatus(item.Support), Addable: item.Addable,
			ReadinessReason: openapi.ModemCandidateReadinessReason(item.Readiness),
			Capabilities:    hardwareCapabilitiesResponse(item.Capabilities), SimPresence: openapi.ManagedModemSIMPresence(item.SIMPresence),
		})
	}
	writeJSON(w, http.StatusOK, openapi.ModemCandidateList{Candidates: candidates})
}

func (server *Server) AddManagedModem(w http.ResponseWriter, r *http.Request) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.modems == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MODEM_MANAGEMENT_UNAVAILABLE", Retryable: true})
		return
	}
	var request openapi.AddManagedModemRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MODEM_ADD_REQUEST_INVALID", Retryable: false})
		return
	}
	item, err := server.modems.Add(r.Context(), request.CandidateId)
	if err != nil {
		switch {
		case errors.Is(err, modemapp.ErrCandidateInvalid):
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MODEM_ADD_REQUEST_INVALID", Retryable: false})
		case errors.Is(err, modemapp.ErrCandidateNotFound):
			writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "MODEM_CANDIDATE_NOT_FOUND", Retryable: true})
		case errors.Is(err, modemapp.ErrCandidateNotReady):
			writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "MODEM_CANDIDATE_NOT_READY", Retryable: true})
		case errors.Is(err, modemapp.ErrAlreadyManaged):
			writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "MODEM_ALREADY_ADDED", Retryable: false})
		case errors.Is(err, modemapp.ErrIdentityConflict):
			writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "MODEM_IDENTITY_CONFLICT", Retryable: false})
		default:
			server.logger.ErrorContext(r.Context(), "managed modem add failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MODEM_ADD_FAILED", Retryable: true})
		}
		return
	}
	server.publish([]realtime.Topic{realtime.TopicInventory, realtime.TopicModems, realtime.TopicLines}, "")
	w.Header().Set("Location", "/api/v1/modems/"+item.ID)
	writeJSON(w, http.StatusCreated, managedModemResponse(item))
}

func (server *Server) SetManagedModemRFState(w http.ResponseWriter, r *http.Request, modemID string) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.modems == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MODEM_MANAGEMENT_UNAVAILABLE", Retryable: true})
		return
	}
	var request openapi.SetManagedModemRFStateRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MODEM_RF_REQUEST_INVALID", Retryable: false})
		return
	}
	item, err := server.modems.SetRFState(r.Context(), modemID, request.Enabled)
	if err != nil {
		switch {
		case errors.Is(err, modemapp.ErrModemNotFound):
			writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "MODEM_NOT_FOUND", Retryable: false})
		case errors.Is(err, modemapp.ErrRFUnavailable):
			writeJSON(w, http.StatusUnprocessableEntity, openapi.ApiError{Code: "MODEM_RF_UNAVAILABLE", Retryable: false})
		default:
			server.logger.WarnContext(r.Context(), "managed modem RF change failed", "modem_id", modemID, "error", err)
			writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "MODEM_RF_CHANGE_FAILED", Retryable: true})
		}
		return
	}
	server.publish([]realtime.Topic{realtime.TopicInventory, realtime.TopicModems, realtime.TopicLines}, "")
	writeJSON(w, http.StatusOK, managedModemResponse(item))
}

func (server *Server) ReadManagedModemEquipmentIdentity(w http.ResponseWriter, r *http.Request, modemID string) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.modems == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MODEM_MANAGEMENT_UNAVAILABLE", Retryable: true})
		return
	}
	imei, err := server.modems.ReadEquipmentIdentity(r.Context(), modemID)
	if err != nil {
		switch {
		case errors.Is(err, modemapp.ErrModemNotFound):
			writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "MODEM_NOT_FOUND", Retryable: false})
		case errors.Is(err, modemapp.ErrIdentityConflict):
			writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "MODEM_IDENTITY_CONFLICT", Retryable: true})
		case errors.Is(err, modemapp.ErrEquipmentIdentityUnavailable):
			writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "MODEM_IDENTITY_UNAVAILABLE", Retryable: true})
		default:
			server.logger.WarnContext(r.Context(), "managed modem identity read failed", "modem_id", modemID, "error", err)
			writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MODEM_IDENTITY_UNAVAILABLE", Retryable: true})
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, openapi.ManagedModemEquipmentIdentity{Imei: imei})
}

func managedModemResponse(item modemdomain.View) openapi.ManagedModem {
	if item.Cellular.State == "" {
		item.Cellular = modemdomain.UnavailableCellularStatus()
	}
	registrations := make([]openapi.CellularRegistration, 0, len(item.Cellular.Registrations))
	for _, registration := range item.Cellular.Registrations {
		registrations = append(registrations, openapi.CellularRegistration{
			Domain: openapi.CellularRegistrationDomain(registration.Domain), State: openapi.CellularRegistrationState(registration.State),
		})
	}
	observedAt := ""
	if !item.Cellular.ObservedAt.IsZero() {
		observedAt = item.Cellular.ObservedAt.UTC().Format(time.RFC3339)
	}
	return openapi.ManagedModem{
		Id: item.ID, DisplayName: item.DisplayName, Model: item.Model, SerialNumber: item.SerialNumber,
		Transport: openapi.DeviceTransport(item.Transport), State: openapi.ManagedModemState(item.State),
		Capabilities: hardwareCapabilitiesResponse(item.Capabilities), RfState: openapi.ManagedModemRFState(item.RFState),
		SimPresence: openapi.ManagedModemSIMPresence(item.SIMPresence), Cellular: openapi.ManagedModemCellularStatus{
			State: openapi.CellularState(item.Cellular.State), ErrorCode: openapi.CellularErrorCode(item.Cellular.ErrorCode),
			Registrations: registrations, OperatorName: item.Cellular.OperatorName, OperatorCode: item.Cellular.OperatorCode,
			Rat: item.Cellular.RAT, SignalState: openapi.CellularSignalState(item.Cellular.SignalState),
			SignalRssiDbm: item.Cellular.SignalRSSIDBm, ObservedAt: observedAt,
		}, AddedAt: item.AddedAt,
	}
}

func (server *Server) GetHardwareTopology(w http.ResponseWriter, r *http.Request) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	topology, err := server.inventory.Topology(r.Context())
	if err != nil {
		server.logger.ErrorContext(r.Context(), "hardware topology failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "HARDWARE_TOPOLOGY_UNAVAILABLE", Retryable: true})
		return
	}
	writeJSON(w, http.StatusOK, hardwareTopologyResponse(topology))
}

func setupHardwareReviewInput(topology inventory.Topology) (setupapp.HardwareReviewInput, error) {
	topologyDigest, err := inventory.SetupDigest(topology)
	if err != nil {
		return setupapp.HardwareReviewInput{}, err
	}
	summaries := make(map[string]inventory.PhysicalDevice, len(topology.Devices))
	for _, device := range topology.Devices {
		summaries[device.ID] = inventory.PhysicalDevice{ID: device.ID, DisplayName: device.DisplayName, Transport: device.Transport, State: device.State, Generation: device.Generation}
	}
	for _, function := range topology.ModemFunctions {
		summary := summaries[function.PhysicalDeviceID]
		summary.ModemFunctionCount++
		summaries[function.PhysicalDeviceID] = summary
	}
	for _, slot := range topology.SIMSlots {
		summary := summaries[slot.PhysicalDeviceID]
		summary.SIMSlotCount++
		summaries[slot.PhysicalDeviceID] = summary
	}
	for _, group := range topology.ResourceGroups {
		summary := summaries[group.PhysicalDeviceID]
		summary.ResourceGroupCount++
		summaries[group.PhysicalDeviceID] = summary
	}
	devices := make([]setupapp.HardwareDevice, 0, len(topology.Devices))
	for _, physical := range topology.Devices {
		device := summaries[physical.ID]
		devices = append(devices, setupapp.HardwareDevice{
			ID:                 device.ID,
			Transport:          device.Transport,
			State:              device.State,
			ModemFunctionCount: device.ModemFunctionCount,
			SIMSlotCount:       device.SIMSlotCount,
			ResourceGroupCount: device.ResourceGroupCount,
		})
	}
	lines := make([]setupapp.HardwareLine, 0, len(topology.Lines))
	for _, line := range topology.Lines {
		lines = append(lines, setupapp.HardwareLine{
			ID:                    line.ID,
			PhysicalDeviceID:      line.PhysicalDeviceID,
			SubscriptionProfileID: line.SubscriptionProfileID,
		})
	}
	return setupapp.HardwareReviewInput{TopologyDigest: topologyDigest, Devices: devices, Lines: lines}, nil
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

func smsMessageResponse(message sms.Message) openapi.SMSMessage {
	response := openapi.SMSMessage{
		Id: message.ID, OperationId: message.OperationID, Direction: openapi.SMSDirection(message.Direction),
		LineId: message.LineID, RemoteAddress: message.RemoteAddress, Body: message.Body,
		Status: openapi.SMSStatus(message.Status), ProviderMessageId: message.ProviderMessageID, ErrorCode: message.ErrorCode,
		CreatedAt: message.CreatedAt.UTC(), UpdatedAt: message.UpdatedAt.UTC(),
	}
	if message.SentAt != nil {
		sentAt := message.SentAt.UTC()
		response.SentAt = &sentAt
	}
	return response
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
