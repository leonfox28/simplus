package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/buildinfo"
	"github.com/leonfox28/simplus/internal/hardwareprobe"
	"github.com/leonfox28/simplus/internal/modemadapter"
	"github.com/leonfox28/simplus/internal/modemadapter/qdc507sms"
	"github.com/leonfox28/simplus/internal/security/secretbox"
)

const (
	registerOptionDriverCommand = "register-option-driver"
	containerOptionNewIDPath    = "/host/sys/bus/usb-serial/drivers/option1/new_id"
)

type optionIDWriter func(string, modemadapter.USBSerialID) error

type uidList []uint32

func (list *uidList) String() string {
	values := make([]string, len(*list))
	for index, uid := range *list {
		values[index] = strconv.FormatUint(uint64(uid), 10)
	}
	return strings.Join(values, ",")
}

func (list *uidList) Set(value string) error {
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return fmt.Errorf("invalid uid %q", value)
	}
	*list = append(*list, uint32(parsed))
	return nil
}

type octalMode struct{ value os.FileMode }

func (mode *octalMode) String() string { return fmt.Sprintf("%04o", mode.value.Perm()) }
func (mode *octalMode) Set(value string) error {
	parsed, err := strconv.ParseUint(value, 8, 9)
	if err != nil {
		return fmt.Errorf("invalid octal mode %q", value)
	}
	mode.value = os.FileMode(parsed)
	return nil
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	_ = syscall.Umask(0o077)
	if len(args) == 1 && args[0] == registerOptionDriverCommand {
		return runRegisterOptionDriver(stdout, stderr, os.Geteuid(), modemadapter.DefaultRegistry(), writeOptionID)
	}
	flags := flag.NewFlagSet("simplus-agent", flag.ContinueOnError)
	flags.SetOutput(stderr)
	versionOnly := flags.Bool("version", false, "print version and exit")
	socketPath := flags.String("socket", envOrDefault("SIMPLUS_AGENT_SOCKET", "/run/simplus/simplus-agent.sock"), "absolute Unix socket path")
	simAKASocketPath := flags.String("sim-aka-socket", os.Getenv("SIMPLUS_AGENT_SIM_AKA_SOCKET"), "optional root-only Unix socket for the bounded SIM authentication API")
	socketGID := flags.Int("socket-gid", -1, "group owner for the socket and parent directory")
	scanInterval := flags.Duration("scan-interval", time.Second, "USB hotplug scan interval")
	usbRoot := flags.String("sysfs-usb-root", "/sys/bus/usb/devices", "USB sysfs root")
	devRoot := flags.String("dev-root", "/dev", "device-node root")
	identityKeyPath := flags.String("identity-key", os.Getenv("SIMPLUS_AGENT_IDENTITY_KEY"), "absolute path to the Agent SIM identity pseudonym key")
	stateRoot := flags.String("state-root", os.Getenv("SIMPLUS_AGENT_STATE_ROOT"), "required private Agent state root for durable hardware operations")
	directoryMode := &octalMode{value: 0o700}
	socketMode := &octalMode{value: 0o600}
	flags.Var(directoryMode, "directory-mode", "agent socket directory mode in octal")
	flags.Var(socketMode, "socket-mode", "agent socket mode in octal")
	var allowedUIDs uidList
	flags.Var(&allowedUIDs, "allowed-uid", "UID allowed to use the agent socket; repeat for multiple UIDs")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "simplus-agent accepts no positional arguments")
		return 2
	}
	if *versionOnly {
		info := buildinfo.Current()
		fmt.Fprintf(stdout, "simplus-agent %s (%s)\n", info.Version, info.Commit)
		return 0
	}
	if runtime.GOOS != "linux" {
		fmt.Fprintln(stderr, "simplus-agent hardware runtime is supported only on Linux")
		return 2
	}
	if len(allowedUIDs) == 0 {
		allowedUIDs = append(allowedUIDs, uint32(os.Geteuid()))
	}
	if *scanInterval < 250*time.Millisecond || *scanInterval > time.Minute {
		fmt.Fprintln(stderr, "scan-interval must be from 250ms through 1m")
		return 2
	}
	if *simAKASocketPath != "" {
		if *simAKASocketPath == *socketPath {
			fmt.Fprintln(stderr, "sim-aka-socket must be separate from the read-only Agent socket")
			return 2
		}
		if *identityKeyPath == "" {
			fmt.Fprintln(stderr, "sim-aka-socket requires identity-key")
			return 2
		}
	}
	if *identityKeyPath == "" || *stateRoot == "" || !filepath.IsAbs(*stateRoot) || filepath.Clean(*stateRoot) != *stateRoot || *stateRoot == string(filepath.Separator) {
		fmt.Fprintln(stderr, "identity-key and an absolute non-root state-root are required")
		return 2
	}

	logger := slog.New(slog.NewJSONHandler(stdout, &slog.HandlerOptions{Level: slog.LevelInfo})).With("service", "simplus-agent")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	identityKeyring, keyErr := secretbox.Open(*identityKeyPath)
	if keyErr != nil {
		logger.Error("SIM identity key initialization failed", "error", keyErr)
		return 1
	}
	stateStore, stateErr := qdc507sms.OpenSQLiteStateRoot(ctx, *stateRoot)
	if stateErr != nil {
		logger.Error("QDC507 SMS state initialization failed", "error", stateErr)
		return 1
	}
	closeState := func() bool {
		if stateStore == nil {
			return true
		}
		if closeErr := stateStore.Close(); closeErr != nil {
			logger.Error("QDC507 SMS state close failed", "error", closeErr)
			stateStore = nil
			return false
		}
		stateStore = nil
		return true
	}
	transport := qdc507sms.NewTTYTransport()
	driver, driverErr := qdc507sms.NewDriver(transport)
	if driverErr != nil {
		closeState()
		logger.Error("QDC507 SMS driver initialization failed", "error", driverErr)
		return 1
	}
	qdcAdapter, adapterErr := qdc507sms.NewAdapter(driver, stateStore)
	if adapterErr != nil {
		closeState()
		logger.Error("QDC507 SMS adapter initialization failed", "error", adapterErr)
		return 1
	}
	registry, registryErr := modemadapter.NewRegistry(qdcAdapter, modemadapter.ML307A{})
	if registryErr != nil {
		closeState()
		logger.Error("hardware adapter registry initialization failed", "error", registryErr)
		return 1
	}
	scanner := hardwareprobe.NewScanner()
	scanner.USBRoot = *usbRoot
	scanner.DevRoot = *devRoot
	scanner.Adapters = registry
	scanner.Identities = identityKeyring
	scanner.Querier = hardwareprobe.NewATQuerierWithIdentity(identityKeyring)
	monitor := agentapi.NewMonitor(scanner)
	scanner.CurrentSnapshot = monitor.Snapshot
	if _, err := monitor.Refresh(ctx); err != nil {
		closeState()
		logger.Error("initial hardware scan failed", "error", err)
		return 1
	}
	listener, err := agentapi.Listen(agentapi.ListenerOptions{
		Path: *socketPath, DirectoryMode: directoryMode.value, SocketMode: socketMode.value,
		OwnerUID: -1, OwnerGID: *socketGID, AllowedUIDs: allowedUIDs,
	})
	if err != nil {
		closeState()
		logger.Error("agent socket bind failed", "path", *socketPath, "error", err)
		return 1
	}
	rfService := agentapi.NewRFService(monitor, scanner)
	equipmentIdentityService := agentapi.NewEquipmentIdentityService(monitor, scanner)
	smsBackend, ok := registry.SMSBackend(monitor, modemadapter.SMSRuntimeDependencies{Gate: scanner.Gate, Resolver: scanner})
	if !ok {
		_ = listener.Close()
		closeState()
		logger.Error("QDC507 SMS backend initialization failed")
		return 1
	}
	server := &http.Server{
		Handler: agentapi.NewManagedHardwareHandler(monitor, rfService, equipmentIdentityService, logger, smsBackend), ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: agentapi.SMSRequestTimeout + 10*time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10,
	}
	var simAKAServer *http.Server
	var simAKAListener *agentapi.UIDListener
	if *simAKASocketPath != "" {
		simAKAListener, err = agentapi.Listen(agentapi.ListenerOptions{
			Path: *simAKASocketPath, DirectoryMode: directoryMode.value, SocketMode: 0o600,
			OwnerUID: -1, OwnerGID: -1, AllowedUIDs: []uint32{0},
		})
		if err != nil {
			_ = listener.Close()
			closeState()
			logger.Error("SIM AKA HIL socket bind failed", "path", *simAKASocketPath, "error", err)
			return 1
		}
		simAKAServer = &http.Server{
			Handler:           agentapi.NewSIMAKAHILHandler(agentapi.NewSIMAKAService(monitor, scanner), logger),
			ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second,
			IdleTimeout: 15 * time.Second, MaxHeaderBytes: 8 << 10,
		}
	}
	monitorErrors := make(chan error, 1)
	go func() { monitorErrors <- monitor.Run(ctx, *scanInterval) }()
	type serverFailure struct {
		name string
		err  error
	}
	serverErrors := make(chan serverFailure, 2)
	go func() { serverErrors <- serverFailure{name: "read-only", err: server.Serve(listener)} }()
	if simAKAServer != nil {
		go func() { serverErrors <- serverFailure{name: "SIM AKA HIL", err: simAKAServer.Serve(simAKAListener)} }()
	}
	logger.Info("typed hardware agent listening", "socket", *socketPath, "protocol_version", agentapi.ProtocolVersion, "scan_interval", scanInterval.String())
	if simAKAServer != nil {
		logger.Info("root-only SIM AKA HIL endpoint listening", "socket", *simAKASocketPath, "protocol_version", agentapi.ProtocolVersion)
	}

	exitCode := 0
	select {
	case <-ctx.Done():
	case err := <-monitorErrors:
		if err != nil {
			logger.Error("hardware monitor failed", "error", err)
			exitCode = 1
		}
	case failure := <-serverErrors:
		if !errors.Is(failure.err, http.ErrServerClosed) {
			logger.Error("agent server failed", "endpoint", failure.name, "error", failure.err)
			exitCode = 1
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("agent shutdown failed", "error", err)
		exitCode = 1
	}
	if simAKAServer != nil {
		if err := simAKAServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("SIM AKA HIL endpoint shutdown failed", "error", err)
			exitCode = 1
		}
	}
	cancel()
	if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		logger.Error("agent socket cleanup failed", "error", err)
		exitCode = 1
	}
	if simAKAListener != nil {
		if err := simAKAListener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			logger.Error("SIM AKA HIL socket cleanup failed", "error", err)
			exitCode = 1
		}
	}
	if exitCode == 0 {
		logger.Info("hardware agent stopped")
	}
	if !closeState() {
		exitCode = 1
	}
	return exitCode
}

func runRegisterOptionDriver(stdout, stderr io.Writer, effectiveUID int, registry *modemadapter.Registry, writer optionIDWriter) int {
	if runtime.GOOS != "linux" {
		fmt.Fprintln(stderr, "option driver registration is supported only on Linux")
		return 2
	}
	if effectiveUID != 0 {
		fmt.Fprintln(stderr, "option driver registration must be run as root")
		return 1
	}
	if registry == nil || writer == nil {
		fmt.Fprintln(stderr, "option driver registration is unavailable")
		return 1
	}
	ids := registry.USBSerialIDs()
	if len(ids) == 0 {
		fmt.Fprintln(stderr, "no verified option driver USB IDs are registered")
		return 1
	}
	for _, id := range ids {
		if err := writer(containerOptionNewIDPath, id); err != nil && !errors.Is(err, syscall.EEXIST) {
			fmt.Fprintf(stderr, "register verified option driver USB ID: %v\n", err)
			return 1
		}
	}
	fmt.Fprintf(stdout, "registered %d verified option driver USB ID(s)\n", len(ids))
	return 0
}

func writeOptionID(path string, id modemadapter.USBSerialID) error {
	if path != containerOptionNewIDPath {
		return errors.New("option driver new_id path is not the fixed container mount")
	}
	return writeOptionIDFile(path, id)
}

func writeOptionIDFile(path string, id modemadapter.USBSerialID) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect option driver new_id: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("option driver new_id must be a real sysfs attribute")
	}
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open option driver new_id: %w", err)
	}
	_, writeErr := io.WriteString(file, id.VendorID+" "+id.ProductID+"\n")
	closeErr := file.Close()
	if errors.Is(writeErr, syscall.EEXIST) {
		writeErr = nil
	}
	if err := errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("write option driver new_id: %w", err)
	}
	return nil
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
