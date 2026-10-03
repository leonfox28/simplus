package httpapi

import (
	"log/slog"
	"time"
)

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

func WithRealtime(server *Server, manager RealtimeManager) *Server {
	if server != nil {
		server.realtime = manager
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
	healthService HealthReader,
	setupService SetupManager,
	inventoryService InventoryReader,
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
