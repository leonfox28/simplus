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
	var hardwareAgentClient *agentapi.Client
	var messageSender messaging.Sender
	var messageInbox messaging.Inbox
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
		if policyErr := requireTypedHardwareAgent(hello); policyErr != nil {
			logger.Error("hardware Agent does not satisfy the typed capability policy", "error", policyErr)
			_ = stores.Close()
			return 1
		}
		inventoryService = inventory.New(inventory.NewAgentSource(agentClient))
		hardwareAgentClient = agentClient
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
			messageSender, messageInbox = voWiFiGateway, voWiFiGateway
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
	messageService, err := messaging.NewService(ctx, stores, managedLineService, messageSender, messageInbox)
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
		messageService.UseHostVoWiFiTransport(voWiFiService)
	}
	go runSMSSync(ctx, messageService, notificationService, realtimeHub, logger, 2*time.Second)
	if hardwareAgentClient != nil {
		go runAgentChanges(ctx, hardwareAgentClient, realtimeHub, logger)
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

func runSMSSync(ctx context.Context, messages *messaging.Service, notifications *notificationapp.Service,
	publisher *realtime.Hub, logger *slog.Logger, interval time.Duration) {
	if interval < time.Second {
		interval = 2 * time.Second
	}
	sync := func() bool {
		syncCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		result, err := messages.SyncInbound(syncCtx)
		cancel()
		changed := publishSMSSyncResult(publisher, result)
		if err != nil {
			logger.Warn("SMS synchronization failed", "error", err)
		}
		if !changed {
			return err == nil
		}
		logger.Info("SMS synchronization completed",
			"inbound_persisted", result.Persisted, "inbound_already_known", result.AlreadyKnown,
			"inbound_acknowledged", result.Acknowledged, "outbound_sent", result.OutboundSent,
			"outbound_failed", result.OutboundFailed, "outbound_unconfirmed", result.OutboundUnconfirmed,
			"outbound_reports_acknowledged", result.OutboundReportsAcknowledged)
		if result.Persisted == 0 {
			return err == nil
		}
		deliveryCtx, cancelDelivery := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancelDelivery()
		if err := notifications.Notify(deliveryCtx, "sms.received", fmt.Sprintf("[Simplus] 收到 %d 条新短信", result.Persisted)); err != nil {
			logger.Warn("inbound SMS notification failed", "error", err)
		}
		publisher.Publish([]realtime.Topic{realtime.TopicNotifications}, "")
		return err == nil
	}
	retryDelay := time.Duration(0)
	for {
		if sync() {
			retryDelay = 0
		} else {
			retryDelay = nextSMSSyncRetryDelay(retryDelay, interval)
		}
		delay := interval
		if retryDelay > 0 {
			delay = retryDelay
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-timer.C:
		}
	}
}

func publishSMSSyncResult(publisher *realtime.Hub, result messaging.InboundSyncResult) bool {
	if result.Persisted == 0 && result.AlreadyKnown == 0 && result.OutboundSent == 0 &&
		result.OutboundFailed == 0 && result.OutboundUnconfirmed == 0 {
		return false
	}
	attention := realtime.Attention("")
	if result.Persisted > 0 {
		attention = realtime.AttentionSMSReceived
	}
	publisher.Publish([]realtime.Topic{realtime.TopicMessages}, attention)
	return true
}

type agentChangeSource interface {
	Snapshot(context.Context, bool) (agentapi.Snapshot, error)
	Changes(context.Context, string, uint64, int) (agentapi.ChangeResponse, error)
}

func runAgentChanges(ctx context.Context, source agentChangeSource, publisher *realtime.Hub, logger *slog.Logger) {
	const watchSeconds = 25
	var previous agentapi.Snapshot
	retryDelay := time.Second
	for ctx.Err() == nil {
		snapshot, err := source.Snapshot(ctx, false)
		if err != nil {
			logger.Warn("hardware Agent snapshot watch initialization failed", "error", err)
			if !waitForContext(ctx, retryDelay) {
				return
			}
			retryDelay = nextAgentRetryDelay(retryDelay)
			continue
		}
		if previous.AgentInstanceID != "" && (snapshot.AgentInstanceID != previous.AgentInstanceID || snapshot.Generation != previous.Generation) {
			publisher.Publish([]realtime.Topic{realtime.TopicInventory, realtime.TopicModems, realtime.TopicLines}, "")
		}
		previous = snapshot
		for ctx.Err() == nil {
			change, changeErr := source.Changes(ctx, snapshot.AgentInstanceID, snapshot.Generation, watchSeconds)
			if changeErr != nil {
				logger.Warn("hardware Agent change watch failed", "error", changeErr)
				break
			}
			snapshot = change.Snapshot
			previous = snapshot
			retryDelay = time.Second
			if change.Changed {
				publisher.Publish([]realtime.Topic{realtime.TopicInventory, realtime.TopicModems, realtime.TopicLines}, "")
			}
		}
		if !waitForContext(ctx, retryDelay) {
			return
		}
		retryDelay = nextAgentRetryDelay(retryDelay)
	}
}

func nextAgentRetryDelay(previous time.Duration) time.Duration {
	if previous < time.Second {
		return time.Second
	}
	if previous >= 30*time.Second {
		return 30 * time.Second
	}
	previous *= 2
	if previous > 30*time.Second {
		return 30 * time.Second
	}
	return previous
}

func waitForContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func nextSMSSyncRetryDelay(previous, interval time.Duration) time.Duration {
	const minimumRetry = 15 * time.Second
	const maximumRetry = 5 * time.Minute
	if interval < 2*time.Second {
		interval = 2 * time.Second
	}
	if previous <= 0 {
		previous = minimumRetry
		if interval*4 > previous {
			previous = interval * 4
		}
	} else if previous < maximumRetry/2 {
		previous *= 2
	} else {
		previous = maximumRetry
	}
	if previous > maximumRetry {
		return maximumRetry
	}
	return previous
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
	rfControl := false
	for _, feature := range hello.Features {
		switch feature {
		case agentapi.FeatureRFControl:
			rfControl = true
		case agentapi.FeatureSMS, agentapi.CommandRadioEnsureOff, "durable-command-outcomes":
			return fmt.Errorf("Agent advertises forbidden mutation feature %q", feature)
		}
	}
	if !rfControl {
		return errors.New("Agent does not advertise rf-control-v1")
	}
	return nil
}
