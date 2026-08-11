package notification

import (
	"context"
	"errors"
	"sync"
	"time"

	domain "github.com/leonfox28/simplus/internal/domain/notification"
)

const (
	BindingStateIdle      = "idle"
	BindingStateWaiting   = "waiting"
	BindingStateTesting   = "testing"
	BindingStateSucceeded = "succeeded"
	BindingStateFailed    = "failed"
	BindingStateExpired   = "expired"
	BindingStateCancelled = "cancelled"
)

const (
	BindingErrorDenied          = "FEISHU_BINDING_DENIED"
	BindingErrorExpired         = "FEISHU_BINDING_EXPIRED"
	BindingErrorLarkUnsupported = "FEISHU_BINDING_LARK_UNSUPPORTED"
	BindingErrorResultInvalid   = "FEISHU_BINDING_RESULT_INVALID"
	BindingErrorProviderFailed  = "FEISHU_BINDING_PROVIDER_FAILED"
	BindingErrorTestFailed      = "FEISHU_BINDING_TEST_FAILED"
	BindingErrorPersistFailed   = "FEISHU_BINDING_PERSIST_FAILED"
)

var (
	ErrBindingActive        = errors.New("feishu binding is already active")
	ErrBindingNotCancelable = errors.New("feishu binding cannot be cancelled")
	ErrBindingUnavailable   = errors.New("feishu binding is unavailable")
)

var defaultBindingEvents = []string{"call.incoming", "call.missed", "sms.failed", "sms.received", "system.degraded"}

type BindingView struct {
	State, VerificationURL, ChannelID, ErrorCode string
	ExpiresAt                                    time.Time
}

type bindingController struct {
	mu         sync.Mutex
	generation uint64
	state      BindingView
	cancel     context.CancelFunc
	processCtx context.Context
	onChange   func()
}

func newBindingController() *bindingController {
	return &bindingController{processCtx: context.Background(), state: BindingView{State: BindingStateIdle}}
}

func (s *Service) ConfigureFeishuBinding(processCtx context.Context, registrar FeishuRegistrar, messenger FeishuMessenger, onChange func()) {
	if processCtx == nil {
		processCtx = context.Background()
	}
	s.FeishuRegistrar, s.FeishuMessenger = registrar, messenger
	s.binding.mu.Lock()
	s.binding.processCtx, s.binding.onChange = processCtx, onChange
	s.binding.mu.Unlock()
}

func (s *Service) FeishuBindingStatus() BindingView {
	s.binding.mu.Lock()
	defer s.binding.mu.Unlock()
	return s.binding.state
}

func (s *Service) StartFeishuBinding(_ context.Context) (BindingView, error) {
	if s.FeishuRegistrar == nil || s.FeishuMessenger == nil {
		return BindingView{}, ErrBindingUnavailable
	}
	s.binding.mu.Lock()
	if s.binding.state.State == BindingStateWaiting || s.binding.state.State == BindingStateTesting {
		state := s.binding.state
		s.binding.mu.Unlock()
		return state, ErrBindingActive
	}
	s.binding.generation++
	generation := s.binding.generation
	attemptCtx, cancel := context.WithCancel(s.binding.processCtx)
	s.binding.cancel = cancel
	s.binding.state = BindingView{State: BindingStateWaiting}
	s.binding.mu.Unlock()

	registration, err := s.FeishuRegistrar.Begin(attemptCtx)
	if err != nil {
		state := s.finishBindingFailure(generation, err)
		return state, err
	}
	if registration.VerificationURL == "" || registration.ExpiresAt.IsZero() {
		state := s.finishBindingFailure(generation, ErrFeishuProviderResultInvalid)
		return state, ErrFeishuProviderResultInvalid
	}
	s.binding.mu.Lock()
	if generation != s.binding.generation || s.binding.state.State != BindingStateWaiting {
		state := s.binding.state
		s.binding.mu.Unlock()
		cancel()
		return state, nil
	}
	s.binding.state.VerificationURL = registration.VerificationURL
	s.binding.state.ExpiresAt = registration.ExpiresAt.UTC()
	state := s.binding.state
	s.binding.mu.Unlock()
	go s.completeFeishuBinding(attemptCtx, generation, registration)
	return state, nil
}

func (s *Service) CancelFeishuBinding() (BindingView, error) {
	s.binding.mu.Lock()
	defer s.binding.mu.Unlock()
	if s.binding.state.State == BindingStateTesting {
		return s.binding.state, ErrBindingNotCancelable
	}
	if s.binding.state.State != BindingStateWaiting {
		return s.binding.state, nil
	}
	s.binding.generation++
	if s.binding.cancel != nil {
		s.binding.cancel()
	}
	s.binding.cancel = nil
	s.binding.state = BindingView{State: BindingStateCancelled}
	return s.binding.state, nil
}

func (s *Service) completeFeishuBinding(ctx context.Context, generation uint64, registration FeishuRegistration) {
	result, err := s.FeishuRegistrar.Poll(ctx, registration)
	if err != nil {
		s.finishBindingFailure(generation, err)
		return
	}
	if err := validateFeishuResult(result); err != nil {
		s.finishBindingFailure(generation, err)
		return
	}
	s.binding.mu.Lock()
	if generation != s.binding.generation || s.binding.state.State != BindingStateWaiting {
		s.binding.mu.Unlock()
		return
	}
	s.binding.state = BindingView{State: BindingStateTesting}
	s.binding.mu.Unlock()

	if err := s.FeishuMessenger.SendText(ctx, result, "Simplus 飞书私聊通知绑定成功"); err != nil {
		s.finishBindingFailure(generation, errors.Join(ErrFeishuProviderUnavailable, err), BindingErrorTestFailed)
		return
	}
	if ctx.Err() != nil {
		return
	}
	id, err := newChannelID()
	if err != nil {
		s.finishBindingFailure(generation, err, BindingErrorPersistFailed)
		return
	}
	appID, err := s.Secrets.Encrypt(feishuSecretLabel(id, "app-id"), []byte(result.AppID))
	if err != nil {
		s.finishBindingFailure(generation, err, BindingErrorPersistFailed)
		return
	}
	appSecret, err := s.Secrets.Encrypt(feishuSecretLabel(id, "app-secret"), []byte(result.AppSecret))
	if err != nil {
		s.finishBindingFailure(generation, err, BindingErrorPersistFailed)
		return
	}
	openID, err := s.Secrets.Encrypt(feishuSecretLabel(id, "recipient-open-id"), []byte(result.OpenID))
	if err != nil {
		s.finishBindingFailure(generation, err, BindingErrorPersistFailed)
		return
	}
	now := s.Now().UTC()
	item := domain.Channel{
		ID: id, Provider: "feishu", DeliveryMode: domain.DeliveryModeFeishuApp,
		DisplayName: "飞书私聊", FeishuAppIDCiphertext: appID,
		FeishuAppSecretCiphertext: appSecret, FeishuRecipientOpenIDCiphertext: openID,
		WebhookHint: "open.feishu.cn", Enabled: true,
		EventKinds:     append([]string(nil), defaultBindingEvents...),
		LastDeliveryAt: now, LastDeliveryStatus: "success", CreatedAt: now, UpdatedAt: now,
	}
	s.binding.mu.Lock()
	owned := generation == s.binding.generation && s.binding.state.State == BindingStateTesting
	s.binding.mu.Unlock()
	if !owned {
		return
	}
	if err := s.Store.UpsertNotificationChannel(ctx, item); err != nil {
		s.finishBindingFailure(generation, err, BindingErrorPersistFailed)
		return
	}
	s.binding.mu.Lock()
	if generation != s.binding.generation || s.binding.state.State != BindingStateTesting {
		s.binding.mu.Unlock()
		_, _ = s.Store.DeleteNotificationChannel(context.Background(), id)
		return
	}
	attemptCancel := s.binding.cancel
	s.binding.cancel = nil
	s.binding.state = BindingView{State: BindingStateSucceeded, ChannelID: id}
	onChange := s.binding.onChange
	s.binding.mu.Unlock()
	if attemptCancel != nil {
		attemptCancel()
	}
	if onChange != nil {
		onChange()
	}
}

func (s *Service) finishBindingFailure(generation uint64, err error, forcedCode ...string) BindingView {
	s.binding.mu.Lock()
	if generation != s.binding.generation {
		state := s.binding.state
		s.binding.mu.Unlock()
		return state
	}
	code, state := bindingFailureCode(err), BindingStateFailed
	if len(forcedCode) != 0 {
		code = forcedCode[0]
	}
	if errors.Is(err, ErrFeishuAuthorizationExpired) {
		state = BindingStateExpired
	}
	attemptCancel := s.binding.cancel
	s.binding.cancel = nil
	s.binding.state = BindingView{State: state, ErrorCode: code}
	result := s.binding.state
	s.binding.mu.Unlock()
	if attemptCancel != nil {
		attemptCancel()
	}
	return result
}

func bindingFailureCode(err error) string {
	switch {
	case errors.Is(err, ErrFeishuAuthorizationDenied):
		return BindingErrorDenied
	case errors.Is(err, ErrFeishuAuthorizationExpired):
		return BindingErrorExpired
	case errors.Is(err, ErrFeishuLarkUnsupported):
		return BindingErrorLarkUnsupported
	case errors.Is(err, ErrFeishuProviderResultInvalid):
		return BindingErrorResultInvalid
	default:
		return BindingErrorProviderFailed
	}
}
