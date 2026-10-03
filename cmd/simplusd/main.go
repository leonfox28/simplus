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
	"sync"
	"syscall"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/agentinventory"
	"github.com/leonfox28/simplus/internal/api/httpapi"
	"github.com/leonfox28/simplus/internal/application/auth"
	"github.com/leonfox28/simplus/internal/application/calls"
	"github.com/leonfox28/simplus/internal/application/connectivity"
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
	"github.com/leonfox28/simplus/internal/connectivityagent"
	"github.com/leonfox28/simplus/internal/control"
	vowifidomain "github.com/leonfox28/simplus/internal/domain/vowifi"
	"github.com/leonfox28/simplus/internal/feishu"
	"github.com/leonfox28/simplus/internal/lifecycle"
	mihomoassets "github.com/leonfox28/simplus/internal/mihomoassets"
	"github.com/leonfox28/simplus/internal/modemagent"
	"github.com/leonfox28/simplus/internal/notificationwebhook"
	"github.com/leonfox28/simplus/internal/security/password"
	"github.com/leonfox28/simplus/internal/security/secretbox"
	"github.com/leonfox28/simplus/internal/simulator"
	"github.com/leonfox28/simplus/internal/smstransport"
	sqlitestore "github.com/leonfox28/simplus/internal/storage/sqlite"
	"github.com/leonfox28/simplus/internal/subscriptionhttp"
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

	if _, err := os.Lstat(filepath.Join(cfg.Storage.DataRoot, "db")); err == nil || !errors.Is(err, os.ErrNotExist) {
		logger.Error("legacy data layout detected; use a new data root and preserve the old data")
		return 2
	}
	databaseRoot := filepath.Join(cfg.Storage.DataRoot, "state")
	stores, err := sqlitestore.OpenSet(ctx, databaseRoot)
	if err != nil {
		logger.Error("database initialization failed", "error", err)
		return 1
	}

	defer stores.Close()
	var workers sync.WaitGroup
	defer func() { stop(); workers.Wait() }()

	instanceSecretKeyPath := filepath.Join(databaseRoot, ".simplus-secrets-key-v1")
	setupService, err := newSetupService(stores)
	if err != nil {
		logger.Error("Setup dependency configuration failed", "error", err)
		return 1
	}
	authService := auth.NewService(stores, stores, password.NewDefaultHasher())
	secretKeyring, err := secretbox.Open(instanceSecretKeyPath)
	if err != nil {
		logger.Error("instance secret key initialization failed", "error", err)
		return 1
	}
	var inventoryService *inventory.Service
	var hardwareAgentClient *agentapi.Client
	var messageTransports []messaging.SMSTransport
	mihomoSupervisorSocket := os.Getenv("SIMPLUS_MIHOMO_SUPERVISOR_SOCKET")
	var voWiFiSupervisor interface {
		vowifidomain.API
		connectivity.VoWiFiSource
	}
	var cellularSource connectivity.CellularSource
	modemOptions := modemapp.Options{RF: modemapp.DisabledHardware{}, Runtime: modemapp.DisabledHardware{}, Identity: modemapp.DisabledIdentity{}}
	switch cfg.Runtime.Backend {
	case config.BackendSimulator:
		inventoryService = inventory.NewMultiSimulator()
		simulatedRadio := simulator.NewCellular(inventoryService)
		cellularSource = simulatedRadio
		modemOptions.RF, modemOptions.Runtime = simulatedRadio, simulatedRadio
		voWiFiSupervisor = simulator.NewVoWiFi()
		const simulatorAgentInstanceID = "01234567-89ab-cdef-0123-456789abcdef"
		simulatorClient, clientErr := agentapi.NewLocalSMSClient(simulatorAgentInstanceID, agentapi.NewDefaultSimulatorSMSBackend())
		if clientErr != nil {
			logger.Error("simulator SMS client configuration rejected", "error", clientErr)
			return 2
		}
		simulatorGateway, gatewayErr := smstransport.NewAgentSMSGateway(simulatorClient)
		if gatewayErr != nil {
			logger.Error("simulator SMS gateway configuration rejected", "error", gatewayErr)
			return 2
		}
		simulatorTransport := messaging.AgentNativeSMSTransport(simulatorGateway, simulatorGateway)
		messageTransports = append(messageTransports, simulatorTransport)
	case config.BackendHardware:
		agentClient, clientErr := agentapi.NewClient(cfg.Runtime.AgentSocket)
		if clientErr != nil {
			logger.Error("hardware agent configuration rejected", "error", clientErr)
			return 2
		}
		helloCtx, cancelHello := context.WithTimeout(ctx, 5*time.Second)
		hello, helloErr := agentClient.Hello(helloCtx)
		cancelHello()
		if helloErr != nil {
			logger.Error("hardware agent unavailable", "socket", cfg.Runtime.AgentSocket, "error", helloErr)
			return 1
		}
		if policyErr := requireTypedHardwareAgent(hello); policyErr != nil {
			logger.Error("hardware Agent does not satisfy the typed capability policy", "error", policyErr)
			return 1
		}
		inventoryService = inventory.New(agentinventory.NewAgentSource(agentClient))
		hardwareAgentClient = agentClient
		agentSMSGateway, gatewayErr := smstransport.NewAgentSMSGateway(agentClient)
		if gatewayErr != nil {
			logger.Error("hardware Agent SMS gateway configuration rejected", "error", gatewayErr)
			return 2
		}
		messageTransports = append(messageTransports, messaging.AgentNativeSMSTransport(agentSMSGateway, agentSMSGateway))
		if mihomoSupervisorSocket != "" {
			voWiFiClient, clientErr := vowifisupervisor.NewClient(mihomoSupervisorSocket)
			voWiFiSupervisor = voWiFiClient
			if clientErr != nil {
				logger.Error("Host VoWiFi supervisor client configuration failed", "error", clientErr)
				return 2
			}
			voWiFiGateway, gatewayErr := smstransport.NewVoWiFiSMSGateway(voWiFiClient)
			if gatewayErr != nil {
				logger.Error("Host VoWiFi SMS gateway configuration failed", "error", gatewayErr)
				return 2
			}
			messageTransports = append(messageTransports, messaging.HostVoWiFiSMSTransport(nil, voWiFiGateway, voWiFiGateway))
		}
	default:
		logger.Error("unsupported backend", "backend", cfg.Runtime.Backend)
		return 2
	}
	if hardwareAgentClient != nil {
		cellularSource = connectivityagent.Source{Client: hardwareAgentClient}
		controller := modemagent.NewAgentRFController(hardwareAgentClient)
		modemOptions.RF, modemOptions.Runtime = controller, controller
		modemOptions.Identity = modemagent.NewAgentEquipmentIdentityReader(hardwareAgentClient)
	}
	managedModemService, err := modemapp.New(stores, inventoryService, modemOptions)
	if err != nil {
		logger.Error("managed modem initialization failed", "error", err)
		return 1
	}
	var phoneNumbers lineapp.PhoneNumberSource = lineapp.DisabledPhoneNumbers{}
	if voWiFiSupervisor != nil {
		phoneNumbers = lineapp.NewVoWiFiPhoneNumberSource(voWiFiSupervisor)
	}
	managedLineService, err := lineapp.New(stores, inventoryService, phoneNumbers)
	if err != nil {
		logger.Error("managed line initialization failed", "error", err)
		return 1
	}
	contactService, err := contacts.New(stores)
	if err != nil {
		logger.Error("contacts initialization failed", "error", err)
		return 1
	}
	var callService *calls.Service
	var euiccService *euicc.Service
	if cfg.Runtime.Backend == config.BackendSimulator {
		callService, err = calls.New(ctx, stores, managedLineService)
		if err != nil {
			logger.Error("calls initialization failed", "error", err)
			return 1
		}
		euiccService, err = euicc.New(stores)
		if err != nil {
			logger.Error("eUICC initialization failed", "error", err)
			return 1
		}
	}
	realtimeHub := realtime.NewHub()
	webhookClient := notificationwebhook.NewClient()
	feishuClient := feishu.NewFeishuClient()
	notificationService, err := notificationapp.New(notificationapp.Dependencies{
		Store: stores, Secrets: secretKeyring, Webhooks: webhookClient,
		ProcessContext: ctx, Registrar: feishuClient, Messenger: feishuClient, OnChange: func() { realtimeHub.Publish([]realtime.Topic{realtime.TopicNotifications}, "") },
	})
	if err != nil {
		logger.Error("notification dependency configuration failed", "error", err)
		return 1
	}
	defer notificationService.Close()

	workers.Go(func() {
		notificationService.Run(ctx, func() { realtimeHub.Publish([]realtime.Topic{realtime.TopicNotifications}, "") }, func(err error) { logger.Warn("notification worker failed", "error", err) })
	})
	mihomoRoot := filepath.Join(cfg.Storage.DataRoot, "mihomo")
	mihomoCoreManager := mihomoassets.NewCoreManager(mihomoRoot)
	mihomoConfigManager := mihomoassets.NewConfigManager(mihomoRoot, stores, mihomoCoreManager)
	mihomoController, controllerErr := mihomoControllerAddress(cfg.Server.Listen)
	if controllerErr != nil {
		logger.Error("Mihomo controller address derivation failed", "error", controllerErr)
		return 1
	}
	mihomoDashboardManager := mihomoassets.NewDashboardManager(mihomoRoot, mihomoController)
	mihomoDashboardStatus, dashboardErr := mihomoDashboardManager.Ensure()
	if dashboardErr != nil {
		logger.Error("Mihomo dashboard initialization failed", "error", dashboardErr)
		return 1
	}
	mihomoConfigManager.ConfigureDashboard(mihomoDashboardStatus)
	mihomoSupervisor, supervisorErr := newMihomoSupervisor(mihomoRoot, mihomoSupervisorSocket)
	if supervisorErr != nil {
		logger.Error("Mihomo supervisor configuration failed", "error", supervisorErr)
		return 2
	}
	if owned, ok := mihomoSupervisor.(interface{ Close(context.Context) error }); ok {
		defer func() {
			shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := owned.Close(shutdown); err != nil {
				logger.Warn("owned Mihomo cleanup failed", "error", err)
			}
		}()
	}
	mihomoRuntimeManager, runtimeManagerErr := mihomoapp.NewRuntimeManager(mihomoRoot, stores, mihomoConfigManager, mihomoCoreManager, mihomoSupervisor)
	if runtimeManagerErr != nil {
		logger.Error("Mihomo runtime manager dependency configuration failed", "error", runtimeManagerErr)
		return 1
	}
	mihomoSubscriptionService, err := mihomoapp.NewSubscriptionService(stores, secretKeyring, subscriptionhttp.New(), mihomoConfigManager)
	if err != nil {
		logger.Error("Mihomo subscription dependencies invalid", "error", err)
		return 2
	}
	lineEgressService := lineegressapp.New(stores, managedLineService, mihomoRuntimeManager)
	var voWiFiService *vowifiapp.Service
	if voWiFiSupervisor != nil {
		voWiFiService, err = vowifiapp.New(stores, managedLineService, lineEgressService, mihomoRuntimeManager, voWiFiSupervisor)
		if err != nil {
			logger.Error("Host VoWiFi service configuration failed", "error", err)
			return 2
		}
		workers.Go(func() {
			voWiFiService.Run(ctx, 10*time.Second, func(reconcileErr error) {
				if reconcileErr != nil {
					logger.Warn("Host VoWiFi desired-state reconciliation failed", "error", reconcileErr)
					return
				}
				realtimeHub.Publish([]realtime.Topic{realtime.TopicVoWiFi}, "")
			})
		})
		for index := range messageTransports {
			messageTransports[index] = messageTransports[index].UseHostVoWiFiAvailability(voWiFiService)
		}
	}
	messageService, err := messaging.NewService(ctx, stores, managedLineService, messageTransports...)
	if err != nil {
		logger.Error("messaging initialization failed", "error", err)
		return 1
	}
	smsSyncCoordinator, err := messaging.NewSyncCoordinator(messageService, realtimeHub)
	if err != nil {
		logger.Error("SMS synchronization coordinator configuration failed", "error", err)
		return 1
	}
	var agentChangeCoordinator *agentinventory.AgentChangeCoordinator
	if hardwareAgentClient != nil {
		agentChangeCoordinator, err = agentinventory.NewAgentChangeCoordinator(hardwareAgentClient, realtimeHub)
		if err != nil {
			logger.Error("hardware Agent change coordinator configuration failed", "error", err)
			return 1
		}
	}
	workers.Go(func() {
		smsSyncCoordinator.Run(ctx, 2*time.Second, func(report messaging.SyncReport) {
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
		})
	})
	if agentChangeCoordinator != nil {
		workers.Go(func() {
			agentChangeCoordinator.Run(ctx, func(report agentinventory.AgentChangeReport) {
				switch report.Operation {
				case agentinventory.AgentChangeSnapshot:
					logger.Warn("hardware Agent snapshot watch initialization failed", "error", report.Error)
				case agentinventory.AgentChangeWatch:
					logger.Warn("hardware Agent change watch failed", "error", report.Error)
				}
			})
		})
	}
	if cellularSource != nil {
		monitor, monitorErr := connectivity.NewCellularMonitor(stores, stores, cellularSource)
		if monitorErr != nil {
			logger.Error("cellular monitor configuration failed", "error", monitorErr)
			return 2
		}
		workers.Go(func() {
			monitor.Run(ctx, func(err error) { logger.Warn("cellular monitoring unavailable", "error", err) })
		})
	}
	if voWiFiSupervisor != nil {
		monitor, monitorErr := connectivity.NewVoWiFiMonitor(stores, stores, voWiFiSupervisor)
		if monitorErr != nil {
			logger.Error("VoWiFi monitor configuration failed", "error", monitorErr)
			return 2
		}
		workers.Go(func() {
			monitor.Run(ctx, func(err error) {
				if err != nil {
					logger.Warn("VoWiFi monitoring unavailable", "error", err)
				}
			})
		})
	}
	dependencies := httpapi.Dependencies{
		Health: health.New(stores, cfg.Runtime.Backend), Setup: setupService, Inventory: inventoryService,
		Auth: authService, Messages: messageService, Contacts: contactService, Logger: logger,
		Modems: managedModemService, Lines: managedLineService, LineEgress: lineEgressService,
		MihomoCore: mihomoCoreManager, MihomoSubscriptions: mihomoSubscriptionService,
		MihomoConfig: mihomoConfigManager, MihomoRuntime: mihomoRuntimeManager, MihomoDashboard: mihomoDashboardManager,
		Notifications: notificationService, Realtime: realtimeHub,
	}
	if callService != nil {
		dependencies.Calls = callService
	}
	if euiccService != nil {
		dependencies.Euicc = euiccService
	}
	if voWiFiService != nil {
		dependencies.Vowifi = voWiFiService
	}
	apiServer, err := httpapi.NewServer(dependencies)
	if err != nil {
		logger.Error("HTTP dependencies invalid", "error", err)
		return 2
	}
	handler, err := applicationHandler(httpapi.Router(apiServer), os.Getenv("SIMPLUS_WEB_ROOT"))
	if err != nil {
		logger.Error("Web root configuration failed", "error", err)
		return 2
	}
	var requests lifecycle.Requests
	server := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           requests.Handler(handler),
		BaseContext:       func(net.Listener) context.Context { return ctx },
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

		return 1
	}
	controlServer := &http.Server{
		Handler:           requests.Handler(control.NewProvisionHandler(setupService, logger)),
		BaseContext:       func(net.Listener) context.Context { return ctx },
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

	requests.StopAdmission()
	stop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := errors.Join(server.Shutdown(shutdownCtx), controlServer.Shutdown(shutdownCtx)); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		exitCode = 1
	}
	_ = server.Close()
	_ = controlServer.Close()
	cancel()
	requests.Wait()
	apiServer.Wait()
	workers.Wait()
	notificationService.Close()
	if exitCode == 0 {
		logger.Info("control plane stopped")
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
		case "radio.ensure-off", "durable-command-outcomes":
			return fmt.Errorf("Agent advertises forbidden mutation feature %q", feature)
		}
	}
	if !rfControl || !equipmentIdentity || !sms {
		return errors.New("Agent does not advertise the required RF, equipment identity, and SMS features")
	}
	return nil
}
