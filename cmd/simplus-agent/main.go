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
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/buildinfo"
	"github.com/leonfox28/simplus/internal/hardwareprobe"
	"github.com/leonfox28/simplus/internal/security/secretbox"
)

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
	flags := flag.NewFlagSet("simplus-agent", flag.ContinueOnError)
	flags.SetOutput(stderr)
	versionOnly := flags.Bool("version", false, "print version and exit")
	socketPath := flags.String("socket", envOrDefault("SIMPLUS_AGENT_SOCKET", "/run/simplus/simplus-agent.sock"), "absolute Unix socket path")
	simAKASocketPath := flags.String("sim-aka-socket", os.Getenv("SIMPLUS_AGENT_SIM_AKA_SOCKET"), "optional root-only Unix socket for the bounded ML307A SIM AKA HIL API")
	socketGID := flags.Int("socket-gid", -1, "group owner for the socket and parent directory")
	scanInterval := flags.Duration("scan-interval", time.Second, "USB hotplug scan interval")
	usbRoot := flags.String("sysfs-usb-root", "/sys/bus/usb/devices", "USB sysfs root")
	devRoot := flags.String("dev-root", "/dev", "device-node root")
	identityKeyPath := flags.String("identity-key", os.Getenv("SIMPLUS_AGENT_IDENTITY_KEY"), "absolute path to the Agent SIM identity pseudonym key")
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

	logger := slog.New(slog.NewJSONHandler(stdout, &slog.HandlerOptions{Level: slog.LevelInfo})).With("service", "simplus-agent")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	scanner := hardwareprobe.NewScanner()
	scanner.USBRoot = *usbRoot
	scanner.DevRoot = *devRoot
	if *identityKeyPath != "" {
		identityKeyring, keyErr := secretbox.Open(*identityKeyPath)
		if keyErr != nil {
			logger.Error("SIM identity key initialization failed", "error", keyErr)
			return 1
		}
		scanner.Querier = hardwareprobe.NewATQuerierWithIdentity(identityKeyring)
	}
	monitor := agentapi.NewMonitor(scanner)
	if _, err := monitor.Refresh(ctx); err != nil {
		logger.Error("initial hardware scan failed", "error", err)
		return 1
	}
	listener, err := agentapi.Listen(agentapi.ListenerOptions{
		Path: *socketPath, DirectoryMode: directoryMode.value, SocketMode: socketMode.value,
		OwnerUID: -1, OwnerGID: *socketGID, AllowedUIDs: allowedUIDs,
	})
	if err != nil {
		logger.Error("agent socket bind failed", "path", *socketPath, "error", err)
		return 1
	}
	server := &http.Server{
		Handler: agentapi.NewReadOnlyHardwareHandler(monitor, logger), ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10,
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
	logger.Info("read-only hardware agent listening", "socket", *socketPath, "protocol_version", agentapi.ProtocolVersion, "scan_interval", scanInterval.String())
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
	return exitCode
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
