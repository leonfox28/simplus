package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/api/httpapi"
	"github.com/leonfox28/simplus/internal/application/accesspath"
	"github.com/leonfox28/simplus/internal/application/auth"
	"github.com/leonfox28/simplus/internal/application/calls"
	"github.com/leonfox28/simplus/internal/application/contacts"
	"github.com/leonfox28/simplus/internal/application/euicc"
	"github.com/leonfox28/simplus/internal/application/health"
	"github.com/leonfox28/simplus/internal/application/inventory"
	lineegressapp "github.com/leonfox28/simplus/internal/application/lineegress"
	"github.com/leonfox28/simplus/internal/application/messaging"
	mihomoapp "github.com/leonfox28/simplus/internal/application/mihomo"
	notificationapp "github.com/leonfox28/simplus/internal/application/notification"
	"github.com/leonfox28/simplus/internal/application/setup"
	vowifiapp "github.com/leonfox28/simplus/internal/application/vowifi"
	"github.com/leonfox28/simplus/internal/buildinfo"
	"github.com/leonfox28/simplus/internal/config"
	"github.com/leonfox28/simplus/internal/control"
	"github.com/leonfox28/simplus/internal/mihomosupervisor"
	"github.com/leonfox28/simplus/internal/security/password"
	"github.com/leonfox28/simplus/internal/security/secretbox"
	sqlitestore "github.com/leonfox28/simplus/internal/storage/sqlite"
	"github.com/leonfox28/simplus/internal/vowifisupervisor"
)

func main() {
	os.Exit(run())
}

func run() int {
	_ = syscall.Umask(0o077)

	configPath := flag.String("config", os.Getenv("SIMPLUS_CONFIG"), "optional YAML configuration path")
	versionOnly := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *versionOnly {
		info := buildinfo.Current()
		fmt.Printf("simplusd %s (%s)\n", info.Version, info.Commit)
		return 0
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})).With("service", "simplusd")
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("configuration rejected", "error", err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	stores, err := sqlitestore.OpenSet(ctx, filepath.Join(cfg.Storage.DataRoot, "db"))
	if err != nil {
		logger.Error("database initialization failed", "error", err)
		return 1
	}

	setupService := setup.New(stores, stores)
	authService := auth.NewService(stores, stores, password.NewDefaultHasher())
	secretKeyring, err := secretbox.Open(filepath.Join(cfg.Storage.DataRoot, "db", ".simplus-secrets-key-v1"))
	if err != nil {
		logger.Error("instance secret key initialization failed", "error", err)
		_ = stores.Close()
		return 1
	}
	var inventoryService *inventory.Service
	var messageSender messaging.Sender
	var messageInbox messaging.Inbox
	switch cfg.Runtime.Backend {
	case config.BackendSimulator:
		inventoryService = inventory.NewMultiSimulator(stores)
		const simulatorAgentInstanceID = "01234567-89ab-cdef-0123-456789abcdef"
		simulatorClient, clientErr := agentapi.NewLocalSMSClient(simulatorAgentInstanceID, agentapi.NewDefaultSimulatorSMSBackend())
		if clientErr != nil {
			logger.Error("simulator SMS client configuration rejected", "error", clientErr)
			_ = stores.Close()
			return 2
		}
		simulatorGateway, gatewayErr := messaging.NewAgentSMSGateway(simulatorClient, simulatorAgentInstanceID)
		if gatewayErr != nil {
			logger.Error("simulator SMS gateway configuration rejected", "error", gatewayErr)
			_ = stores.Close()
			return 2
		}
		messageSender = simulatorGateway
		messageInbox = simulatorGateway
	case config.BackendHardware:
		agentClient, clientErr := agentapi.NewClient(cfg.Runtime.AgentSocket)
		if clientErr != nil {
			logger.Error("hardware agent configuration rejected", "error", clientErr)
			_ = stores.Close()
			return 2
		}
		helloCtx, cancelHello := context.WithTimeout(ctx, 5*time.Second)
		hello, helloErr := agentClient.Hello(helloCtx)
		cancelHello()
		if helloErr != nil {
			logger.Error("hardware agent unavailable", "socket", cfg.Runtime.AgentSocket, "error", helloErr)
			_ = stores.Close()
			return 1
		}
		if policyErr := requireReadOnlyAgent(hello); policyErr != nil {
			logger.Error("hardware Agent does not satisfy the read-only V1 policy", "error", policyErr)
			_ = stores.Close()
			return 1
		}
		inventoryService = inventory.New(inventory.NewAgentSource(agentClient), stores)
	case config.BackendReplay:
		logger.Error("replay backend is not implemented", "backend", cfg.Runtime.Backend)
		_ = stores.Close()
		return 2
	default:
		logger.Error("unsupported backend", "backend", cfg.Runtime.Backend)
		_ = stores.Close()
		return 2
	}
	messageService, err := messaging.NewService(ctx, stores, inventoryService, messageSender, messageInbox)
	if err != nil {
		logger.Error("messaging initialization failed", "error", err)
		_ = stores.Close()
		return 1
	}
	contactService, err := contacts.New(stores)
	if err != nil {
		logger.Error("contacts initialization failed", "error", err)
		_ = stores.Close()
		return 1
	}
	var callService *calls.Service
	var euiccService *euicc.Service
	var accessPathService *accesspath.Service
	if cfg.Runtime.Backend == config.BackendSimulator {
		callService, err = calls.New(ctx, stores, inventoryService)
		if err != nil {
			logger.Error("calls initialization failed", "error", err)
			_ = stores.Close()
			return 1
		}
		euiccService, err = euicc.New(stores)
		if err != nil {
			logger.Error("eUICC initialization failed", "error", err)
			_ = stores.Close()
			return 1
		}
		accessPathService, err = accesspath.New(stores)
		if err != nil {
			logger.Error("access paths initialization failed", "error", err)
			_ = stores.Close()
			return 1
		}
		messageService.UseAccessPathGuard(accessPathService)
		callService.UseAccessPathGuard(accessPathService)
	}
	notificationService := notificationapp.New(stores, secretKeyring)
	if result, syncErr := messageService.SyncInbound(ctx); syncErr != nil {
		logger.Warn("initial inbound SMS synchronization failed", "error", syncErr)
	} else if result.Persisted != 0 || result.AlreadyKnown != 0 {
		logger.Info("initial inbound SMS synchronization completed",
			"persisted", result.Persisted, "already_known", result.AlreadyKnown, "acknowledged", result.Acknowledged)
		if result.Persisted > 0 {
			go func(count int) {
				deliveryCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				if err := notificationService.Notify(deliveryCtx, "sms.received", fmt.Sprintf("[Simplus] 收到 %d 条新短信", count)); err != nil {
					logger.Warn("inbound SMS notification failed", "error", err)
				}
			}(result.Persisted)
		}
	}
	mihomoRoot := filepath.Join(cfg.Storage.DataRoot, "mihomo")
	mihomoCoreManager := mihomoapp.NewCoreManager(mihomoRoot)
	mihomoConfigManager := mihomoapp.NewConfigManager(mihomoRoot, stores, mihomoCoreManager)
	mihomoDashboardManager := mihomoapp.NewDashboardManager(mihomoRoot, os.Getenv("SIMPLUS_MIHOMO_CONTROLLER_ADDR"))
	mihomoDashboardStatus, dashboardErr := mihomoDashboardManager.Ensure()
	if dashboardErr != nil {
		logger.Error("Mihomo dashboard initialization failed", "error", dashboardErr)
		_ = stores.Close()
		return 1
	}
	mihomoConfigManager.ConfigureDashboard(mihomoDashboardStatus)
	mihomoSupervisorSocket := os.Getenv("SIMPLUS_MIHOMO_SUPERVISOR_SOCKET")
	var mihomoRuntimeManager *mihomoapp.RuntimeManager
	if mihomoSupervisorSocket == "" {
		mihomoRuntimeManager = mihomoapp.NewRuntimeManager(mihomoRoot, stores, mihomoConfigManager, mihomoCoreManager)
	} else {
		mihomoSupervisor, supervisorErr := mihomosupervisor.NewClient(mihomoSupervisorSocket)
		if supervisorErr != nil {
			logger.Error("Mihomo supervisor client configuration failed", "error", supervisorErr)
			_ = stores.Close()
			return 2
		}
		mihomoRuntimeManager = mihomoapp.NewRuntimeManagerWithSupervisor(mihomoRoot, stores, mihomoConfigManager, mihomoCoreManager, mihomoSupervisor)
	}
	mihomoSubscriptionService := mihomoapp.NewSubscriptionService(stores, secretKeyring, mihomoConfigManager)
	mihomoEgressService := mihomoapp.NewEgressService(stores, mihomoCoreManager)
	lineEgressService := lineegressapp.New(stores, inventoryService, mihomoRuntimeManager)
	var voWiFiService *vowifiapp.Service
	if cfg.Runtime.Backend == config.BackendHardware && mihomoSupervisorSocket != "" {
		voWiFiSupervisor, supervisorErr := vowifisupervisor.NewClient(mihomoSupervisorSocket)
		if supervisorErr != nil {
			logger.Error("Host VoWiFi supervisor client configuration failed", "error", supervisorErr)
			_ = stores.Close()
			return 2
		}
		voWiFiService, err = vowifiapp.New(stores, inventoryService, lineEgressService, mihomoRuntimeManager, voWiFiSupervisor)
		if err != nil {
			logger.Error("Host VoWiFi service configuration failed", "error", err)
			_ = stores.Close()
			return 2
		}
		go voWiFiService.Run(ctx, 10*time.Second, func(reconcileErr error) {
			logger.Warn("Host VoWiFi desired-state reconciliation failed", "error", reconcileErr)
		})
	}
	apiServer := httpapi.New(health.New(stores, cfg.Runtime.Backend), setupService, inventoryService, logger, authService, messageService, contactService)
	if callService != nil {
		apiServer = httpapi.WithCalls(apiServer, callService)
	}
	if euiccService != nil {
		apiServer = httpapi.WithEUICC(apiServer, euiccService)
	}
	if accessPathService != nil {
		apiServer = httpapi.WithAccessPaths(apiServer, accessPathService)
	}
	apiServer = httpapi.WithMihomoCore(apiServer, mihomoCoreManager)
	apiServer = httpapi.WithMihomoSubscriptions(apiServer, mihomoSubscriptionService)
	apiServer = httpapi.WithMihomoEgress(apiServer, mihomoEgressService)
	apiServer = httpapi.WithLineEgress(apiServer, lineEgressService)
	if voWiFiService != nil {
		apiServer = httpapi.WithVoWiFi(apiServer, voWiFiService)
	}
	apiServer = httpapi.WithMihomoConfig(apiServer, mihomoConfigManager)
	apiServer = httpapi.WithMihomoRuntime(apiServer, mihomoRuntimeManager)
	apiServer = httpapi.WithMihomoDashboard(apiServer, mihomoDashboardManager)
	apiServer = httpapi.WithNotifications(apiServer, notificationService)
	handler, err := applicationHandler(httpapi.Router(apiServer), os.Getenv("SIMPLUS_WEB_ROOT"))
	if err != nil {
		logger.Error("Web root configuration failed", "error", err)
		_ = stores.Close()
		return 2
	}
	server := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      agentapi.SMSRequestTimeout,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	controlPath := os.Getenv("SIMPLUS_CONTROL_SOCKET")
	if controlPath == "" {
		controlPath = control.SocketPath(cfg.Storage.DataRoot)
	}
	controlListener, err := control.ListenRootOnly(controlPath, 0)
	if err != nil {
		logger.Error("root control socket bind failed", "path", controlPath, "error", err)
		if closeErr := stores.Close(); closeErr != nil {
			logger.Error("database close failed after control socket error", "error", closeErr)
		}
		return 1
	}
	controlServer := &http.Server{
		Handler:           control.NewBootstrapHandler(setupService, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       15 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	listener, err := net.Listen("tcp", cfg.Server.Listen)
	if err != nil {
		logger.Error("control plane bind failed", "address", cfg.Server.Listen, "error", err)
		_ = controlListener.Close()
		if closeErr := stores.Close(); closeErr != nil {
			logger.Error("database close failed after bind error", "error", closeErr)
		}
		return 1
	}
	logger.Info("control plane listening",
		"address", listener.Addr().String(),
		"root_control_socket", controlPath,
		"backend", cfg.Runtime.Backend,
		"storage_root", stores.Root,
	)

	type serverResult struct {
		name string
		err  error
	}
	serverErrors := make(chan serverResult, 2)
	go func() {
		serverErrors <- serverResult{name: "control plane", err: server.Serve(listener)}
	}()
	go func() {
		serverErrors <- serverResult{name: "root control socket", err: controlServer.Serve(controlListener)}
	}()

	exitCode := 0
	select {
	case <-ctx.Done():
	case result := <-serverErrors:
		if !errors.Is(result.err, http.ErrServerClosed) {
			logger.Error(result.name+" failed", "error", result.err)
			exitCode = 1
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := errors.Join(server.Shutdown(shutdownCtx), controlServer.Shutdown(shutdownCtx)); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		exitCode = 1
	}
	cancel()
	if exitCode == 0 {
		logger.Info("control plane stopped")
	}
	if err := stores.Close(); err != nil {
		logger.Error("database close failed", "error", err)
		exitCode = 1
	}
	return exitCode
}

func requireReadOnlyAgent(hello agentapi.Hello) error {
	readOnly := false
	for _, feature := range hello.Features {
		switch feature {
		case agentapi.FeatureHardwareReadOnly:
			readOnly = true
		case agentapi.FeatureSMS, agentapi.CommandRadioEnsureOff, "durable-command-outcomes":
			return fmt.Errorf("Agent advertises forbidden mutation feature %q", feature)
		}
	}
	if !readOnly {
		return errors.New("Agent does not advertise hardware-read-only-policy-v1")
	}
	return nil
}
