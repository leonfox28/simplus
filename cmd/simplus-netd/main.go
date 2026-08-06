package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/buildinfo"
	"github.com/leonfox28/simplus/internal/mihomosupervisor"
	"github.com/leonfox28/simplus/internal/vowifisupervisor"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	_ = syscall.Umask(0o077)
	if len(args) > 0 && args[0] == "--vowifi-worker" {
		return runVoWiFiWorker(args[1:], stdout, stderr)
	}
	flags := flag.NewFlagSet("simplus-netd", flag.ContinueOnError)
	flags.SetOutput(stderr)
	versionOnly := flags.Bool("version", false, "print version and exit")
	socketPath := flags.String("socket", "/run/simplus-netd/mihomo.sock", "absolute supervisor Unix socket")
	mihomoRoot := flags.String("mihomo-root", "/var/lib/simplus/mihomo", "absolute Simplus Mihomo state root")
	vowifiRoot := flags.String("vowifi-root", "/run/simplus-netd/vowifi", "absolute Host VoWiFi runtime root")
	serviceUID := flags.Uint("service-uid", 0, "non-root UID allowed to use the supervisor and run Mihomo")
	serviceGID := flags.Uint("service-gid", 0, "non-root GID owning the supervisor socket and Mihomo state")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "simplus-netd accepts no positional arguments")
		return 2
	}
	if *versionOnly {
		info := buildinfo.Current()
		fmt.Fprintf(stdout, "simplus-netd %s (%s)\n", info.Version, info.Commit)
		return 0
	}
	if runtime.GOOS != "linux" || !filepath.IsAbs(*socketPath) || !filepath.IsAbs(*mihomoRoot) || !filepath.IsAbs(*vowifiRoot) {
		fmt.Fprintln(stderr, "simplus-netd requires Linux and absolute socket/root paths")
		return 2
	}
	if os.Geteuid() != 0 || *serviceUID == 0 || *serviceGID == 0 || uint64(*serviceUID) > math.MaxUint32 || uint64(*serviceGID) > math.MaxUint32 {
		fmt.Fprintln(stderr, "simplus-netd requires root supervisor ownership and an explicit non-root service uid/gid")
		return 2
	}
	logger := slog.New(slog.NewJSONHandler(stdout, nil)).With("service", "simplus-netd")
	local, err := mihomosupervisor.NewLocalForUser(*mihomoRoot, uint32(*serviceUID), uint32(*serviceGID))
	if err != nil {
		logger.Error("Mihomo supervisor configuration failed", "error", err)
		return 2
	}
	executable, err := os.Executable()
	if err != nil {
		logger.Error("Host VoWiFi worker executable resolution failed", "error", err)
		return 2
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		logger.Error("Host VoWiFi worker executable validation failed", "error", err)
		return 2
	}
	vowifiLocal, err := vowifisupervisor.NewLocal(*vowifiRoot, executable)
	if err != nil {
		logger.Error("Host VoWiFi supervisor configuration failed", "error", err)
		return 2
	}
	listener, err := agentapi.Listen(agentapi.ListenerOptions{
		Path: *socketPath, DirectoryMode: 0o750, SocketMode: 0o660,
		OwnerUID: 0, OwnerGID: int(*serviceGID), AllowedUIDs: []uint32{0, uint32(*serviceUID)},
	})
	if err != nil {
		logger.Error("Mihomo supervisor socket bind failed", "error", err)
		return 1
	}
	mux := http.NewServeMux()
	mux.Handle("/v1/vowifi/", http.StripPrefix("/v1/vowifi", vowifisupervisor.NewHandler(vowifiLocal, logger)))
	mux.Handle("/", mihomosupervisor.NewHandler(local, logger))
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: agentapi.SMSRequestTimeout, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.Serve(listener) }()
	logger.Info("Mihomo supervisor listening", "socket", *socketPath)
	exitCode := 0
	select {
	case <-ctx.Done():
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("Mihomo supervisor failed", "error", err)
			exitCode = 1
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := vowifiLocal.Close(shutdownCtx); err != nil {
		logger.Error("Host VoWiFi supervisor shutdown failed", "error", err)
		exitCode = 1
	}
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("Mihomo supervisor shutdown failed", "error", err)
		exitCode = 1
	}
	cancel()
	if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		logger.Error("Mihomo supervisor socket cleanup failed", "error", err)
		exitCode = 1
	}
	return exitCode
}

func runVoWiFiWorker(args []string, stdout, stderr io.Writer) int {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		fmt.Fprintln(stderr, "Host VoWiFi worker requires root on Linux")
		return 2
	}
	flags := flag.NewFlagSet("simplus-netd --vowifi-worker", flag.ContinueOnError)
	flags.SetOutput(stderr)
	runtimeDir := flags.String("runtime-dir", "", "fixed private runtime directory")
	lineID := flags.String("line-id", "", "stable managed Line ID")
	hardwareLineID := flags.String("hardware-line-id", "", "current hardware Line target")
	egressMode := flags.String("egress-mode", "", "direct or mihomo-country")
	countryCode := flags.String("country-code", "", "ISO country code for Mihomo egress")
	linkAddress := flags.String("link-address", "", "fixed namespace link address")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := vowifisupervisor.RunWorker(ctx, vowifisupervisor.WorkerConfig{
		LineID: *lineID, HardwareLineID: *hardwareLineID, RuntimeDir: *runtimeDir, LinkAddress: *linkAddress,
		EgressMode: *egressMode, CountryCode: *countryCode,
		CharonPath: "/usr/sbin/charon-systemd", IPPath: "/usr/sbin/ip",
	}, stdout)
	if err != nil {
		fmt.Fprintln(stderr, "Host VoWiFi worker rejected its fixed invocation")
		return 1
	}
	return 0
}
