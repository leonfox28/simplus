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
	"github.com/leonfox28/simplus/internal/application/auth"
	"github.com/leonfox28/simplus/internal/application/calls"
	"github.com/leonfox28/simplus/internal/application/contacts"
	"github.com/leonfox28/simplus/internal/application/euicc"
	"github.com/leonfox28/simplus/internal/application/health"
	"github.com/leonfox28/simplus/internal/application/inventory"
	lineapp "github.com/leonfox28/simplus/internal/application/line"
	lineegressapp "github.com/leonfox28/simplus/internal/application/lineegress"
	"github.com/leonfox28/simplus/internal/application/messaging"
	mihomoapp "github.com/leonfox28/simplus/internal/application/mihomo"
	modemapp "github.com/leonfox28/simplus/internal/application/modem"
	notificationapp "github.com/leonfox28/simplus/internal/application/notification"
	"github.com/leonfox28/simplus/internal/application/realtime"
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

	databaseRoot := filepath.Join(cfg.Storage.DataRoot, "db")
	stores, err := sqlitestore.OpenSet(ctx, databaseRoot)
	if err != nil {
		logger.Error("database initialization failed", "error", err)
		return 1
	}

	instanceSecretKeyPath := filepath.Join(databaseRoot, ".simplus-secrets-key-v1")
	setupService, err := newSetupService(stores, instanceSecretKeyPath)
	if err != nil {
		logger.Error("Setup dependency configuration failed", "error", err)
		_ = stores.Close()
		return 1
	}
	authService := auth.NewService(stores, stores, password.NewDefaultHasher())
	secretKeyring, err := secretbox.Open(instanceSecretKeyPath)
	if err != nil {
		logger.Error("instance secret key initialization failed", "error", err)
		_ = stores.Close()
		return 1
	}
	var inventoryService *inventory.Service
	var hardwareAgentClient *agentapi.Client
	var messageTransports []messaging.SMSTransport
	mihomoSupervisorSocket := os.Getenv("SIMPLUS_MIHOMO_SUPERVISOR_SOCKET")
	var voWiFiSupervisor *vowifisupervisor.Client
	switch cfg.Runtime.Backend {
	case config.BackendSimulator:
		inventoryService = inventory.NewMultiSimulator()
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
		simulatorTransport := messaging.AgentNativeSMSTransport(simulatorGateway, simulatorGateway)
		messageTransports = append(messageTransports, simulatorTransport)
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
		if policyErr := requireTypedHardwareAgent(hello); policyErr != nil {
			logger.Error("hardware Agent does not satisfy the typed capability policy", "error", policyErr)
			_ = stores.Close()
			return 1
		}
		inventoryService = inventory.New(inventory.NewAgentSource(agentClient))
		hardwareAgentClient = agentClient
		agentSMSGateway, gatewayErr := messaging.NewAgentSMSGateway(agentClient, hello.AgentInstanceID)
		if gatewayErr != nil {
			logger.Error("hardware Agent SMS gateway configuration rejected", "error", gatewayErr)
			_ = stores.Close()
			return 2
		}
		messageTransports = append(messageTransports, messaging.AgentNativeSMSTransport(agentSMSGateway, agentSMSGateway))
		if mihomoSupervisorSocket != "" {
			voWiFiSupervisor, clientErr = vowifisupervisor.NewClient(mihomoSupervisorSocket)
			if clientErr != nil {
				logger.Error("Host VoWiFi supervisor client configuration failed", "error", clientErr)
				_ = stores.Close()
				return 2
			}
			voWiFiGateway, gatewayErr := messaging.NewVoWiFiSMSGateway(voWiFiSupervisor)
			if gatewayErr != nil {
				logger.Error("Host VoWiFi SMS gateway configuration failed", "error", gatewayErr)
				_ = stores.Close()
				return 2
			}
			messageTransports = append(messageTransports, messaging.HostVoWiFiSMSTransport(nil, voWiFiGateway, voWiFiGateway))
		}
	case config.BackendReplay:
		logger.Error("replay backend is not implemented", "backend", cfg.Runtime.Backend)
		_ = stores.Close()
		return 2
	default:
		logger.Error("unsupported backend", "backend", cfg.Runtime.Backend)
		_ = stores.Close()
		return 2
	}
	managedModemService, err := modemapp.New(stores, inventoryService)
	if err != nil {
		logger.Error("managed modem initialization failed", "error", err)
		_ = stores.Close()
		return 1
	}
	if hardwareAgentClient != nil {
		managedModemService.UseRFController(modemapp.NewAgentRFController(hardwareAgentClient))
		managedModemService.UseEquipmentIdentityReader(modemapp.NewAgentEquipmentIdentityReader(hardwareAgentClient))
	}
	managedLineService, err := lineapp.New(stores, inventoryService)
	if err != nil {
		logger.Error("managed line initialization failed", "error", err)
		_ = stores.Close()
		return 1
	}
	if voWiFiSupervisor != nil {
		managedLineService.UsePhoneNumberSource(lineapp.NewVoWiFiPhoneNumberSource(voWiFiSupervisor))
	}
	messageService, err := messaging.NewService(ctx, stores, managedLineService)
	if err != nil {
		logger.Error("messaging initialization failed", "error", err)
		_ = stores.Close()
		return 1
	}
	if cfg.Runtime.Backend == config.BackendSimulator {
		if err := messageService.UseTransports(messageTransports...); err != nil {
			logger.Error("Simulator SMS transport configuration failed", "error", err)
			_ = stores.Close()
			return 2
		}
	}
	contactService, err := contacts.New(stores)
	if err != nil {
		logger.Error("contacts initialization failed", "error", err)
		_ = stores.Close()
		return 1
	}
	var callService *calls.Service
	var euiccService *euicc.Service
	if cfg.Runtime.Backend == config.BackendSimulator {
		callService, err = calls.New(ctx, stores, managedLineService)
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
	}
	realtimeHub := realtime.NewHub()
	notificationService := notificationapp.New(stores, secretKeyring)
	feishuClient := notificationapp.NewFeishuClient()
	notificationService.ConfigureFeishuBinding(ctx, feishuClient, feishuClient, func() {
		realtimeHub.Publish([]realtime.Topic{realtime.TopicNotifications}, "")
	})
	mihomoRoot := filepath.Join(cfg.Storage.DataRoot, "mihomo")
	mihomoCoreManager := mihomoapp.NewCoreManager(mihomoRoot)
	mihomoConfigManager := mihomoapp.NewConfigManager(mihomoRoot, stores, mihomoCoreManager)
	mihomoController, controllerErr := mihomoControllerAddress(cfg.Server.Listen)
	if controllerErr != nil {
		logger.Error("Mihomo controller address derivation failed", "error", controllerErr)
		_ = stores.Close()
		return 1
	}
	mihomoDashboardManager := mihomoapp.NewDashboardManager(mihomoRoot, mihomoController)
	mihomoDashboardStatus, dashboardErr := mihomoDashboardManager.Ensure()
	if dashboardErr != nil {
		logger.Error("Mihomo dashboard initialization failed", "error", dashboardErr)
		_ = stores.Close()
		return 1
	}
	mihomoConfigManager.ConfigureDashboard(mihomoDashboardStatus)
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
	lineEgressService := lineegressapp.New(stores, managedLineService, mihomoRuntimeManager)
	var voWiFiService *vowifiapp.Service
	if cfg.Runtime.Backend == config.BackendHardware && voWiFiSupervisor != nil {
		voWiFiService, err = vowifiapp.New(stores, managedLineService, lineEgressService, mihomoRuntimeManager, voWiFiSupervisor)
		if err != nil {
			logger.Error("Host VoWiFi service configuration failed", "error", err)
			_ = stores.Close()
			return 2
		}
		go voWiFiService.Run(ctx, 10*time.Second, func(reconcileErr error) {
			if reconcileErr != nil {
				logger.Warn("Host VoWiFi desired-state reconciliation failed", "error", reconcileErr)
				return
			}
			realtimeHub.Publish([]realtime.Topic{realtime.TopicVoWiFi}, "")
		})
		for index := range messageTransports {
			messageTransports[index] = messageTransports[index].UseHostVoWiFiAvailability(voWiFiService)
		}
		if err := messageService.UseTransports(messageTransports...); err != nil {
			logger.Error("Host VoWiFi SMS transport configuration failed", "error", err)
			_ = stores.Close()
			return 2
		}
	}
	if cfg.Runtime.Backend == config.BackendHardware && voWiFiService == nil {
		if err := messageService.UseTransports(messageTransports...); err != nil {
			logger.Error("hardware Agent SMS transport configuration failed", "error", err)
			_ = stores.Close()
			return 2
		}
	}
	smsSyncCoordinator, err := messaging.NewSyncCoordinator(messageService, notificationService, realtimeHub)
	if err != nil {
		logger.Error("SMS synchronization coordinator configuration failed", "error", err)
		_ = stores.Close()
		return 1
	}
	var agentChangeCoordinator *inventory.AgentChangeCoordinator
	if hardwareAgentClient != nil {
		agentChangeCoordinator, err = inventory.NewAgentChangeCoordinator(hardwareAgentClient, realtimeHub)
		if err != nil {
			logger.Error("hardware Agent change coordinator configuration failed", "error", err)
			_ = stores.Close()
			return 1
		}
	}
	go smsSyncCoordinator.Run(ctx, 2*time.Second, func(report messaging.SyncReport) {
		if report.SyncError != nil {
			logger.Warn("SMS synchronization failed", "error", report.SyncError)
		}
		if report.DurableChange {
			logger.Info("SMS synchronization completed",
				"inbound_persisted", report.Result.Persisted, "inbound_already_known", report.Result.AlreadyKnown,
				"inbound_acknowledged", report.Result.Acknowledged, "outbound_sent", report.Result.OutboundSent,
				"outbound_failed", report.Result.OutboundFailed, "outbound_unconfirmed", report.Result.OutboundUnconfirmed,
				"outbound_reports_acknowledged", report.Result.OutboundReportsAcknowledged)
		}
		if report.NotificationError != nil {
			logger.Warn("inbound SMS notification failed", "error", report.NotificationError)
		}
	})
	if agentChangeCoordinator != nil {
		go agentChangeCoordinator.Run(ctx, func(report inventory.AgentChangeReport) {
			switch report.Operation {
			case inventory.AgentChangeSnapshot:
				logger.Warn("hardware Agent snapshot watch initialization failed", "error", report.Error)
			case inventory.AgentChangeWatch:
				logger.Warn("hardware Agent change watch failed", "error", report.Error)
			}
		})
	}
	apiServer := httpapi.New(health.New(stores, cfg.Runtime.Backend), setupService, inventoryService, logger, authService, messageService, contactService)
	apiServer = httpapi.WithManagedModems(apiServer, managedModemService)
	apiServer = httpapi.WithManagedLines(apiServer, managedLineService)
	if callService != nil {
		apiServer = httpapi.WithCalls(apiServer, callService)
	}
	if euiccService != nil {
		apiServer = httpapi.WithEUICC(apiServer, euiccService)
	}
	apiServer = httpapi.WithMihomoCore(apiServer, mihomoCoreManager)
	apiServer = httpapi.WithMihomoSubscriptions(apiServer, mihomoSubscriptionService)
	apiServer = httpapi.WithLineEgress(apiServer, lineEgressService)
	if voWiFiService != nil {
		apiServer = httpapi.WithVoWiFi(apiServer, voWiFiService)
	}
	apiServer = httpapi.WithMihomoConfig(apiServer, mihomoConfigManager)
	apiServer = httpapi.WithMihomoRuntime(apiServer, mihomoRuntimeManager)
	apiServer = httpapi.WithMihomoDashboard(apiServer, mihomoDashboardManager)
	apiServer = httpapi.WithNotifications(apiServer, notificationService)
	apiServer = httpapi.WithRealtime(apiServer, realtimeHub)
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

	listener, err := net.Listen(managementListenerNetwork(cfg.Server.Listen), cfg.Server.Listen)
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

func mihomoControllerAddress(managementAddress string) (string, error) {
	host, _, err := net.SplitHostPort(managementAddress)
	if err != nil || net.ParseIP(host) == nil {
		return "", fmt.Errorf("invalid management listen address %q", managementAddress)
	}
	return net.JoinHostPort(host, "19090"), nil
}

func managementListenerNetwork(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err == nil {
		if ip := net.ParseIP(host); ip != nil && ip.To4() != nil {
			return "tcp4"
		}
	}
	return "tcp6"
}

func requireTypedHardwareAgent(hello agentapi.Hello) error {
	rfControl, equipmentIdentity, sms := false, false, false
	for _, feature := range hello.Features {
		switch feature {
		case agentapi.FeatureRFControl:
			rfControl = true
		case agentapi.FeatureEquipmentIdentityRead:
			equipmentIdentity = true
		case agentapi.FeatureSMS:
			sms = true
		case agentapi.CommandRadioEnsureOff, "durable-command-outcomes":
			return fmt.Errorf("Agent advertises forbidden mutation feature %q", feature)
		}
	}
	if !rfControl || !equipmentIdentity || !sms {
		return errors.New("Agent does not advertise the required RF, equipment identity, and SMS features")
	}
	return nil
}
