package httpapi

import (
	"context"
	"errors"

	notificationdomain "github.com/leonfox28/simplus/internal/domain/notification"

	callapp "github.com/leonfox28/simplus/internal/application/calls"
	lineegressapp "github.com/leonfox28/simplus/internal/application/lineegress"
	mihomoapp "github.com/leonfox28/simplus/internal/application/mihomo"
	notificationapp "github.com/leonfox28/simplus/internal/application/notification"
	"github.com/leonfox28/simplus/internal/domain/call"
	domaineuicc "github.com/leonfox28/simplus/internal/domain/euicc"
	linedomain "github.com/leonfox28/simplus/internal/domain/line"
	mihomodomain "github.com/leonfox28/simplus/internal/domain/mihomo"
	modemdomain "github.com/leonfox28/simplus/internal/domain/modem"
	vowifidomain "github.com/leonfox28/simplus/internal/domain/vowifi"
)

var ErrFeatureDisabled = errors.New("feature is disabled for this backend")

func featureDisabled(value any) bool {
	if value == nil {
		return true
	}
	_, ok := value.(interface{ disabledFeature() })
	return ok
}

type disabledCallManager struct{}

func (disabledCallManager) disabledFeature() {}
func (disabledCallManager) List(context.Context, int, string) (callapp.PageResult, error) {
	var r0 callapp.PageResult
	return r0, ErrFeatureDisabled
}
func (disabledCallManager) Dial(context.Context, string, string, string) (call.Record, bool, error) {
	var r0 call.Record
	var r1 bool
	return r0, r1, ErrFeatureDisabled
}
func (disabledCallManager) Incoming(context.Context, string, string, string) (call.Record, bool, error) {
	var r0 call.Record
	var r1 bool
	return r0, r1, ErrFeatureDisabled
}
func (disabledCallManager) Answer(context.Context, string) (call.Record, error) {
	var r0 call.Record
	return r0, ErrFeatureDisabled
}
func (disabledCallManager) Reject(context.Context, string) (call.Record, error) {
	var r0 call.Record
	return r0, ErrFeatureDisabled
}
func (disabledCallManager) Hangup(context.Context, string) (call.Record, error) {
	var r0 call.Record
	return r0, ErrFeatureDisabled
}
func (disabledCallManager) DTMF(context.Context, string, string) (call.Record, error) {
	var r0 call.Record
	return r0, ErrFeatureDisabled
}

type disabledEUICCManager struct{}

func (disabledEUICCManager) disabledFeature() {}
func (disabledEUICCManager) State(context.Context) (domaineuicc.State, error) {
	var r0 domaineuicc.State
	return r0, ErrFeatureDisabled
}
func (disabledEUICCManager) Switch(context.Context, string) (domaineuicc.State, error) {
	var r0 domaineuicc.State
	return r0, ErrFeatureDisabled
}

type disabledLineEgressManager struct{}

func (disabledLineEgressManager) disabledFeature() {}
func (disabledLineEgressManager) List(context.Context) ([]lineegressapp.View, error) {
	var r0 []lineegressapp.View
	return r0, ErrFeatureDisabled
}
func (disabledLineEgressManager) Put(context.Context, string, string, string) (lineegressapp.View, error) {
	var r0 lineegressapp.View
	return r0, ErrFeatureDisabled
}

type disabledVoWiFiManager struct{}

func (disabledVoWiFiManager) disabledFeature() {}
func (disabledVoWiFiManager) List(context.Context) ([]vowifidomain.State, error) {
	var r0 []vowifidomain.State
	return r0, ErrFeatureDisabled
}
func (disabledVoWiFiManager) Activate(context.Context, string) (vowifidomain.State, error) {
	var r0 vowifidomain.State
	return r0, ErrFeatureDisabled
}
func (disabledVoWiFiManager) Deactivate(context.Context, string) (vowifidomain.State, error) {
	var r0 vowifidomain.State
	return r0, ErrFeatureDisabled
}

type disabledManagedModemManager struct{}

func (disabledManagedModemManager) disabledFeature() {}
func (disabledManagedModemManager) List(context.Context) ([]modemdomain.View, error) {
	var r0 []modemdomain.View
	return r0, ErrFeatureDisabled
}
func (disabledManagedModemManager) Candidates(context.Context) ([]modemdomain.Candidate, error) {
	var r0 []modemdomain.Candidate
	return r0, ErrFeatureDisabled
}
func (disabledManagedModemManager) Add(context.Context, string) (modemdomain.View, error) {
	var r0 modemdomain.View
	return r0, ErrFeatureDisabled
}
func (disabledManagedModemManager) SetRFState(context.Context, string, bool) (modemdomain.View, error) {
	var r0 modemdomain.View
	return r0, ErrFeatureDisabled
}
func (disabledManagedModemManager) ReadEquipmentIdentity(context.Context, string) (string, error) {
	var r0 string
	return r0, ErrFeatureDisabled
}

type disabledManagedLineManager struct{}

func (disabledManagedLineManager) disabledFeature() {}
func (disabledManagedLineManager) List(context.Context) ([]linedomain.View, error) {
	var r0 []linedomain.View
	return r0, ErrFeatureDisabled
}
func (disabledManagedLineManager) Candidates(context.Context) ([]linedomain.Candidate, error) {
	var r0 []linedomain.Candidate
	return r0, ErrFeatureDisabled
}
func (disabledManagedLineManager) Add(context.Context, string, string) (linedomain.View, error) {
	var r0 linedomain.View
	return r0, ErrFeatureDisabled
}
func (disabledManagedLineManager) Update(context.Context, string, string) (linedomain.View, error) {
	var r0 linedomain.View
	return r0, ErrFeatureDisabled
}

type disabledMihomoCoreManager struct{}

func (disabledMihomoCoreManager) disabledFeature() {}
func (disabledMihomoCoreManager) Status() (mihomoapp.CoreStatus, error) {
	var r0 mihomoapp.CoreStatus
	return r0, ErrFeatureDisabled
}
func (disabledMihomoCoreManager) CheckLatest(context.Context) (mihomoapp.Candidate, error) {
	var r0 mihomoapp.Candidate
	return r0, ErrFeatureDisabled
}
func (disabledMihomoCoreManager) InstallLatest(context.Context) (mihomoapp.CoreStatus, error) {
	var r0 mihomoapp.CoreStatus
	return r0, ErrFeatureDisabled
}

type disabledMihomoSubscriptionManager struct{}

func (disabledMihomoSubscriptionManager) disabledFeature() {}
func (disabledMihomoSubscriptionManager) List(context.Context) ([]mihomoapp.SubscriptionView, error) {
	var r0 []mihomoapp.SubscriptionView
	return r0, ErrFeatureDisabled
}
func (disabledMihomoSubscriptionManager) Create(context.Context, string, string, bool) (mihomoapp.SubscriptionView, error) {
	var r0 mihomoapp.SubscriptionView
	return r0, ErrFeatureDisabled
}
func (disabledMihomoSubscriptionManager) Update(context.Context, string, string, string, bool) (mihomoapp.SubscriptionView, error) {
	var r0 mihomoapp.SubscriptionView
	return r0, ErrFeatureDisabled
}
func (disabledMihomoSubscriptionManager) Delete(context.Context, string) error {
	return ErrFeatureDisabled
}
func (disabledMihomoSubscriptionManager) Refresh(context.Context, string) (mihomoapp.SubscriptionView, []mihomodomain.Node, error) {
	var r0 mihomoapp.SubscriptionView
	var r1 []mihomodomain.Node
	return r0, r1, ErrFeatureDisabled
}
func (disabledMihomoSubscriptionManager) Nodes(context.Context, string) ([]mihomodomain.Node, error) {
	var r0 []mihomodomain.Node
	return r0, ErrFeatureDisabled
}

type disabledMihomoConfigManager struct{}

func (disabledMihomoConfigManager) disabledFeature() {}
func (disabledMihomoConfigManager) Status(context.Context) (mihomoapp.ConfigStatus, error) {
	var r0 mihomoapp.ConfigStatus
	return r0, ErrFeatureDisabled
}
func (disabledMihomoConfigManager) GenerateAndPublish(context.Context) (mihomoapp.ConfigStatus, error) {
	var r0 mihomoapp.ConfigStatus
	return r0, ErrFeatureDisabled
}
func (disabledMihomoConfigManager) Select(context.Context, string) (mihomoapp.ConfigStatus, error) {
	var r0 mihomoapp.ConfigStatus
	return r0, ErrFeatureDisabled
}

type disabledMihomoRuntimeManager struct{}

func (disabledMihomoRuntimeManager) disabledFeature() {}
func (disabledMihomoRuntimeManager) Status(context.Context) (mihomoapp.RuntimeStatus, error) {
	var r0 mihomoapp.RuntimeStatus
	return r0, ErrFeatureDisabled
}
func (disabledMihomoRuntimeManager) Start(context.Context) (mihomoapp.RuntimeStatus, error) {
	var r0 mihomoapp.RuntimeStatus
	return r0, ErrFeatureDisabled
}
func (disabledMihomoRuntimeManager) Restart(context.Context) (mihomoapp.RuntimeStatus, error) {
	var r0 mihomoapp.RuntimeStatus
	return r0, ErrFeatureDisabled
}
func (disabledMihomoRuntimeManager) Stop(context.Context) (mihomoapp.RuntimeStatus, error) {
	var r0 mihomoapp.RuntimeStatus
	return r0, ErrFeatureDisabled
}

type disabledMihomoDashboardManager struct{}

func (disabledMihomoDashboardManager) disabledFeature() {}
func (disabledMihomoDashboardManager) Ensure() (mihomoapp.DashboardStatus, error) {
	var r0 mihomoapp.DashboardStatus
	return r0, ErrFeatureDisabled
}

type disabledNotificationManager struct{}

func (disabledNotificationManager) disabledFeature() {}
func (disabledNotificationManager) List(context.Context) ([]notificationapp.ChannelView, error) {
	var r0 []notificationapp.ChannelView
	return r0, ErrFeatureDisabled
}
func (disabledNotificationManager) Create(context.Context, string, string, string, string, bool, []string) (notificationapp.ChannelView, error) {
	var r0 notificationapp.ChannelView
	return r0, ErrFeatureDisabled
}
func (disabledNotificationManager) Update(context.Context, string, string, string, string, string, bool, []string) (notificationapp.ChannelView, error) {
	var r0 notificationapp.ChannelView
	return r0, ErrFeatureDisabled
}
func (disabledNotificationManager) Delete(context.Context, string) error {
	return ErrFeatureDisabled
}
func (disabledNotificationManager) Test(context.Context, string) (notificationapp.ChannelView, error) {
	var r0 notificationapp.ChannelView
	return r0, ErrFeatureDisabled
}
func (disabledNotificationManager) Enqueue(context.Context, notificationdomain.Event) error {
	return ErrFeatureDisabled
}
func (disabledNotificationManager) FeishuBindingStatus() notificationapp.BindingView {
	var r0 notificationapp.BindingView
	return r0
}
func (disabledNotificationManager) StartFeishuBinding(context.Context) (notificationapp.BindingView, error) {
	var r0 notificationapp.BindingView
	return r0, ErrFeatureDisabled
}
func (disabledNotificationManager) CancelFeishuBinding() (notificationapp.BindingView, error) {
	var r0 notificationapp.BindingView
	return r0, ErrFeatureDisabled
}
