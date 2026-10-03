package httpapi

import (
	"errors"
	"log/slog"
	"time"

	"github.com/leonfox28/simplus/internal/application/realtime"
)

type Dependencies struct {
	Health              HealthReader
	Setup               SetupManager
	Inventory           InventoryReader
	Auth                Authenticator
	Messages            Messenger
	Contacts            ContactManager
	Calls               CallManager
	Euicc               EUICCManager
	LineEgress          LineEgressManager
	Vowifi              VoWiFiManager
	Modems              ManagedModemManager
	Lines               ManagedLineManager
	MihomoCore          MihomoCoreManager
	MihomoSubscriptions MihomoSubscriptionManager
	MihomoConfig        MihomoConfigManager
	MihomoRuntime       MihomoRuntimeManager
	MihomoDashboard     MihomoDashboardManager
	Notifications       NotificationManager
	Realtime            RealtimeManager
	Logger              *slog.Logger
}

func NewServer(d Dependencies) (*Server, error) {
	if d.Health == nil {
		return nil, errors.New("HTTP dependency health is required")
	}
	if d.Setup == nil {
		return nil, errors.New("HTTP dependency setup is required")
	}
	if d.Inventory == nil {
		return nil, errors.New("HTTP dependency inventory is required")
	}
	if d.Auth == nil {
		return nil, errors.New("HTTP dependency auth is required")
	}
	if d.Messages == nil {
		return nil, errors.New("HTTP dependency messages is required")
	}
	if d.Contacts == nil {
		return nil, errors.New("HTTP dependency contacts is required")
	}
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Realtime == nil {
		d.Realtime = realtime.NewHub()
	}
	if d.Calls == nil {
		d.Calls = disabledCallManager{}
	}
	if d.Euicc == nil {
		d.Euicc = disabledEUICCManager{}
	}
	if d.LineEgress == nil {
		d.LineEgress = disabledLineEgressManager{}
	}
	if d.Vowifi == nil {
		d.Vowifi = disabledVoWiFiManager{}
	}
	if d.Modems == nil {
		d.Modems = disabledManagedModemManager{}
	}
	if d.Lines == nil {
		d.Lines = disabledManagedLineManager{}
	}
	if d.MihomoCore == nil {
		d.MihomoCore = disabledMihomoCoreManager{}
	}
	if d.MihomoSubscriptions == nil {
		d.MihomoSubscriptions = disabledMihomoSubscriptionManager{}
	}
	if d.MihomoConfig == nil {
		d.MihomoConfig = disabledMihomoConfigManager{}
	}
	if d.MihomoRuntime == nil {
		d.MihomoRuntime = disabledMihomoRuntimeManager{}
	}
	if d.MihomoDashboard == nil {
		d.MihomoDashboard = disabledMihomoDashboardManager{}
	}
	if d.Notifications == nil {
		d.Notifications = disabledNotificationManager{}
	}
	return &Server{
		health:              d.Health,
		setup:               d.Setup,
		inventory:           d.Inventory,
		auth:                d.Auth,
		messages:            d.Messages,
		contacts:            d.Contacts,
		calls:               d.Calls,
		euicc:               d.Euicc,
		lineEgress:          d.LineEgress,
		vowifi:              d.Vowifi,
		modems:              d.Modems,
		lines:               d.Lines,
		mihomoCore:          d.MihomoCore,
		mihomoSubscriptions: d.MihomoSubscriptions,
		mihomoConfig:        d.MihomoConfig,
		mihomoRuntime:       d.MihomoRuntime,
		mihomoDashboard:     d.MihomoDashboard,
		notifications:       d.Notifications,
		realtime:            d.Realtime,
		logger:              d.Logger, realtimeHeartbeat: 15 * time.Second}, nil
}
