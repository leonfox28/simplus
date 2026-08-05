package vowifisupervisor

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"syscall"
	"time"
)

var safeStatusToken = regexp.MustCompile(`^[A-Z0-9_-]{0,64}$`)

type workerEvent struct {
	LineID       string    `json:"lineId"`
	State        string    `json:"state"`
	Stage        string    `json:"stage,omitempty"`
	Online       bool      `json:"online"`
	RegisteredAt time.Time `json:"registeredAt,omitempty"`
	NextRefresh  time.Time `json:"nextRefreshAt,omitempty"`
	Attempt      int       `json:"attempt"`
	ErrorCode    string    `json:"errorCode,omitempty"`
}

type instance struct {
	request    StartRequest
	plan       networkPlan
	status     Status
	command    *exec.Cmd
	ready      chan struct{}
	done       chan struct{}
	stop       bool
	cleanup    error
	runtimeDir string
	once       sync.Once
}

type Local struct {
	Root       string
	Executable string
	Now        func() time.Time
	Network    *networkManager

	mu        sync.Mutex
	instances map[string]*instance
}

func NewLocal(root, executable string) (*Local, error) {
	if !filepath.IsAbs(root) || !filepath.IsAbs(executable) {
		return nil, errors.New("Host VoWiFi supervisor paths must be absolute")
	}
	info, err := os.Stat(executable)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return nil, errors.New("Host VoWiFi worker executable is unavailable")
	}
	local := &Local{Root: filepath.Clean(root), Executable: filepath.Clean(executable), Now: time.Now,
		Network: newNetworkManager(), instances: make(map[string]*instance)}
	if err := os.MkdirAll(local.Root, 0o700); err != nil {
		return nil, err
	}
	if err := local.cleanupStale(); err != nil {
		return nil, err
	}
	return local, nil
}

func (local *Local) List(context.Context) ([]Status, error) {
	local.mu.Lock()
	defer local.mu.Unlock()
	statuses := make([]Status, 0, len(local.instances))
	for _, current := range local.instances {
		statuses = append(statuses, current.status)
	}
	sort.Slice(statuses, func(left, right int) bool { return statuses[left].LineID < statuses[right].LineID })
	return statuses, nil
}

func (local *Local) Start(ctx context.Context, request StartRequest) (Status, error) {
	if !validStartRequest(request) {
		return Status{}, ErrRequestInvalid
	}
	local.mu.Lock()
	if current := local.instances[request.LineID]; current != nil &&
		current.status.State != StateStopped && current.status.State != StateFailed {
		status := current.status
		local.mu.Unlock()
		return status, ErrAlreadyRunning
	}
	if stale := local.instances[request.LineID]; stale != nil && stale.cleanup != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		cleanupErr := local.Network.Cleanup(cleanupCtx, stale.plan)
		cancel()
		if cleanupErr != nil {
			status := stale.status
			local.mu.Unlock()
			return status, cleanupErr
		}
		stale.cleanup = nil
	}
	plan, err := buildNetworkPlan(request)
	if err != nil {
		local.mu.Unlock()
		return Status{}, err
	}
	runtimeDir := filepath.Join(local.Root, plan.Token)
	if err := os.RemoveAll(runtimeDir); err != nil {
		local.mu.Unlock()
		return Status{}, err
	}
	if err := os.Mkdir(runtimeDir, 0o700); err != nil {
		local.mu.Unlock()
		return Status{}, err
	}
	if err := writeNetworkManifest(filepath.Join(runtimeDir, "network.json"), plan); err != nil {
		_ = os.RemoveAll(runtimeDir)
		local.mu.Unlock()
		return Status{}, err
	}
	current := &instance{request: request, plan: plan, runtimeDir: runtimeDir, ready: make(chan struct{}), done: make(chan struct{}),
		status: Status{LineID: request.LineID, State: StateStarting, Stage: "network", EgressMode: request.EgressMode,
			CountryCode: request.CountryCode, StartedAt: local.Now().UTC()}}
	local.instances[request.LineID] = current
	local.mu.Unlock()

	setupCtx, cancelSetup := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	err = local.Network.Setup(setupCtx, plan)
	cancelSetup()
	if err != nil {
		local.failStart(current, "NETWORK_SETUP_FAILED", !errors.Is(err, errNetworkCleanupFailed))
		return current.status, fmt.Errorf("%w: %v", ErrStartupFailed, err)
	}
	stdout, command, err := local.workerCommand(current, runtimeDir)
	if err != nil {
		cleanupErr := local.Network.Cleanup(context.Background(), plan)
		code := "WORKER_START_FAILED"
		if cleanupErr != nil {
			code = "NETWORK_CLEANUP_FAILED"
		}
		local.failStart(current, code, cleanupErr == nil)
		if cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
		return current.status, fmt.Errorf("%w: %v", ErrStartupFailed, err)
	}
	local.mu.Lock()
	current.command = command
	local.mu.Unlock()
	go local.monitor(current, runtimeDir, stdout)

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		_, _ = local.Stop(context.Background(), request.LineID)
		return Status{}, ctx.Err()
	case <-timer.C:
		_, _ = local.Stop(context.Background(), request.LineID)
		return Status{}, ErrStartupFailed
	case <-current.ready:
		local.mu.Lock()
		status := current.status
		local.mu.Unlock()
		if status.State == StateFailed {
			return status, ErrStartupFailed
		}
		return status, nil
	}
}

func (local *Local) workerCommand(current *instance, runtimeDir string) (io.ReadCloser, *exec.Cmd, error) {
	command := exec.Command(local.Network.ipPath, "netns", "exec", current.plan.Namespace,
		local.Executable, "--vowifi-worker", "--runtime-dir", runtimeDir,
		"--line-id", current.request.LineID, "--egress-mode", current.request.EgressMode,
		"--country-code", current.request.CountryCode, "--link-address", current.plan.PeerAddress)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/nonexistent"}
	command.Stderr = io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := command.Start(); err != nil {
		_ = stdout.Close()
		return nil, nil, err
	}
	return stdout, command, nil
}

func (local *Local) monitor(current *instance, runtimeDir string, stdout io.ReadCloser) {
	defer stdout.Close()
	scanner := bufio.NewScanner(io.LimitReader(stdout, 1<<20))
	scanner.Buffer(make([]byte, 4096), 16<<10)
	for scanner.Scan() {
		var event workerEvent
		if json.Unmarshal(scanner.Bytes(), &event) != nil || !validWorkerEvent(event, current.request.LineID) {
			continue
		}
		local.mu.Lock()
		current.status.State = event.State
		current.status.Stage = event.Stage
		current.status.Online = event.Online
		current.status.RegisteredAt = event.RegisteredAt
		current.status.NextRefresh = event.NextRefresh
		current.status.Attempt = event.Attempt
		if event.ErrorCode != "" || event.Online {
			current.status.ErrorCode = event.ErrorCode
		}
		local.mu.Unlock()
		current.once.Do(func() { close(current.ready) })
	}
	waitErr := current.command.Wait()
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	cleanupErr := local.Network.Cleanup(cleanupCtx, current.plan)
	cancel()
	if cleanupErr == nil {
		_ = os.RemoveAll(runtimeDir)
	}

	local.mu.Lock()
	current.cleanup = cleanupErr
	if cleanupErr != nil {
		current.status.State, current.status.Stage, current.status.Online = StateFailed, "cleanup", false
		current.status.ErrorCode = "NETWORK_CLEANUP_FAILED"
	} else if current.stop {
		current.status.State, current.status.Stage, current.status.ErrorCode = StateStopped, "", ""
	} else {
		current.status.State, current.status.Stage, current.status.Online = StateFailed, "worker", false
		current.status.NextRefresh = time.Time{}
		if current.status.ErrorCode == "" {
			current.status.ErrorCode = "WORKER_EXITED"
		}
		_ = waitErr
	}
	local.mu.Unlock()
	current.once.Do(func() { close(current.ready) })
	close(current.done)
}

func (local *Local) Stop(ctx context.Context, lineID string) (Status, error) {
	if !hardwareLinePattern.MatchString(lineID) {
		return Status{}, ErrRequestInvalid
	}
	local.mu.Lock()
	current := local.instances[lineID]
	if current == nil || current.status.State == StateStopped || current.command == nil {
		local.mu.Unlock()
		return Status{}, ErrNotRunning
	}
	current.stop = true
	current.status.State, current.status.Stage, current.status.Online = StateStopping, "cleanup", false
	process := current.command.Process
	local.mu.Unlock()
	if process != nil {
		_ = process.Signal(syscall.SIGTERM)
	}
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return Status{}, ctx.Err()
	case <-timer.C:
		if process != nil {
			_ = process.Kill()
		}
		select {
		case <-current.done:
		case <-time.After(3 * time.Second):
			return Status{}, errors.New("Host VoWiFi worker did not stop")
		}
	case <-current.done:
	}
	local.mu.Lock()
	status := current.status
	cleanupErr := current.cleanup
	local.mu.Unlock()
	if cleanupErr != nil {
		return status, cleanupErr
	}
	return status, nil
}

func (local *Local) Close(ctx context.Context) error {
	statuses, _ := local.List(ctx)
	var closeErr error
	for _, status := range statuses {
		if status.State == StateStopped || status.State == StateFailed {
			continue
		}
		if _, err := local.Stop(ctx, status.LineID); err != nil && !errors.Is(err, ErrNotRunning) && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}

func (local *Local) failStart(current *instance, code string, removeRuntime bool) {
	local.mu.Lock()
	current.status.State, current.status.Stage, current.status.Online, current.status.ErrorCode = StateFailed, "startup", false, code
	local.mu.Unlock()
	current.once.Do(func() { close(current.ready) })
	close(current.done)
	if removeRuntime {
		_ = os.RemoveAll(filepath.Join(local.Root, current.plan.Token))
	}
}

func (local *Local) cleanupStale() error {
	entries, err := os.ReadDir(local.Root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !networkTokenPattern.MatchString(entry.Name()) {
			continue
		}
		directory := filepath.Join(local.Root, entry.Name())
		plan, readErr := readNetworkManifest(filepath.Join(directory, "network.json"))
		if readErr != nil || plan.Token != entry.Name() {
			return errors.New("invalid stale Host VoWiFi runtime directory")
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cleanupErr := local.Network.Cleanup(cleanupCtx, plan)
		cancel()
		if cleanupErr != nil {
			return cleanupErr
		}
		if err := os.RemoveAll(directory); err != nil {
			return err
		}
	}
	return nil
}

func validWorkerEvent(event workerEvent, lineID string) bool {
	if event.LineID != lineID || !safeStatusToken.MatchString(event.Stage) || !safeStatusToken.MatchString(event.ErrorCode) ||
		event.Attempt < 0 || event.Attempt > 1_000_000 {
		return false
	}
	switch event.State {
	case StateStarting, StateConnecting, StateRegistering, StateOnline, StateReconnecting, StateStopping, StateFailed:
	default:
		return false
	}
	return event.Online == (event.State == StateOnline)
}
