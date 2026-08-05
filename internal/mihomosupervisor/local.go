package mihomosupervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

var (
	subscriptionIDPattern = regexp.MustCompile(`^subscription_[A-Za-z0-9_-]{22}$`)
	binaryRelativePattern = regexp.MustCompile(`^versions/v[0-9]+\.[0-9]+\.[0-9]+/mihomo$`)
	configRelativePattern = regexp.MustCompile(`^subscriptions/(subscription_[A-Za-z0-9_-]{22})/versions/[0-9a-f]{32}/generated\.yaml$`)
)

type Local struct {
	Root       string
	Now        func() time.Time
	processUID uint32
	processGID uint32
	dropUser   bool
	mu         sync.Mutex
}

func NewLocal(root string) (*Local, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("Mihomo supervisor root must be absolute")
	}
	return &Local{Root: filepath.Clean(root), Now: time.Now}, nil
}

// NewLocalForUser keeps the supervisor privileged while ensuring that the
// validated Mihomo child runs as the dedicated, non-root service identity.
// Only the three capabilities required by the fixed TPROXY listeners survive
// the credential transition.
func NewLocalForUser(root string, uid, gid uint32) (*Local, error) {
	if uid == 0 || gid == 0 {
		return nil, errors.New("Mihomo process identity must be non-root")
	}
	local, err := NewLocal(root)
	if err != nil {
		return nil, err
	}
	local.processUID, local.processGID, local.dropUser = uid, gid, true
	return local, nil
}

func (local *Local) Status(context.Context) (Status, error) {
	local.mu.Lock()
	defer local.mu.Unlock()
	return local.statusLocked()
}

func (local *Local) statusLocked() (Status, error) {
	manifest, err := local.readManifest()
	if err != nil || !manifestAlive(manifest) {
		if err == nil {
			_ = os.Remove(local.manifestPath())
		}
		return Status{}, nil
	}
	manifest.Running = true
	return manifest, nil
}

func (local *Local) Start(ctx context.Context, request StartRequest) (Status, error) {
	local.mu.Lock()
	defer local.mu.Unlock()
	if err := local.validateRequest(request); err != nil {
		return Status{}, err
	}
	if current, _ := local.statusLocked(); current.Running {
		return current, ErrAlreadyRunning
	}
	runtimeDir := filepath.Join(local.Root, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		return Status{}, err
	}
	logPath := filepath.Join(runtimeDir, "mihomo.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return Status{}, err
	}
	startOffset, _ := logFile.Seek(0, io.SeekEnd)
	command := exec.Command(request.BinaryPath, "-f", request.ConfigPath, "-d", runtimeDir)
	command.Env = []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent"}
	command.Stdout, command.Stderr = logFile, logFile
	command.SysProcAttr = local.processAttributes()
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		return Status{}, err
	}
	status := Status{Running: true, PID: command.Process.Pid, SubscriptionID: request.SubscriptionID, BinaryPath: request.BinaryPath, ConfigPath: request.ConfigPath, StartedAt: local.Now().UTC()}
	if err := writeAtomicPrivateFile(local.manifestPath(), status); err != nil {
		_ = command.Process.Kill()
		_ = logFile.Close()
		return Status{}, err
	}
	go func() {
		_ = command.Wait()
		_ = logFile.Close()
	}()
	timer := time.NewTimer(500 * time.Millisecond)
	select {
	case <-ctx.Done():
		timer.Stop()
		_ = terminate(status)
		_ = os.Remove(local.manifestPath())
		return Status{}, ctx.Err()
	case <-timer.C:
	}
	alive, listenerFailed := manifestAlive(status), startupListenerFailed(logPath, startOffset)
	if !alive || listenerFailed {
		_ = terminate(status)
		_ = os.Remove(local.manifestPath())
		return Status{}, fmt.Errorf("%w: process_alive=%t listener_failed=%t", ErrStartupFailed, alive, listenerFailed)
	}
	return status, nil
}

func (local *Local) processAttributes() *syscall.SysProcAttr {
	attributes := &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	if local.dropUser {
		attributes.Credential = &syscall.Credential{Uid: local.processUID, Gid: local.processGID}
		attributes.AmbientCaps = []uintptr{unix.CAP_NET_BIND_SERVICE, unix.CAP_NET_ADMIN, unix.CAP_NET_RAW}
	}
	return attributes
}

func (local *Local) Stop(context.Context) error {
	local.mu.Lock()
	defer local.mu.Unlock()
	status, err := local.statusLocked()
	if err != nil || !status.Running {
		return ErrNotRunning
	}
	if err := terminate(status); err != nil {
		return err
	}
	return os.Remove(local.manifestPath())
}

func (local *Local) validateRequest(request StartRequest) error {
	if !subscriptionIDPattern.MatchString(request.SubscriptionID) || !filepath.IsAbs(request.BinaryPath) || !filepath.IsAbs(request.ConfigPath) {
		return ErrRequestInvalid
	}
	for path, pattern := range map[string]*regexp.Regexp{request.BinaryPath: binaryRelativePattern, request.ConfigPath: configRelativePattern} {
		relative, err := filepath.Rel(local.Root, filepath.Clean(path))
		if err != nil || strings.HasPrefix(relative, "..") || !pattern.MatchString(filepath.ToSlash(relative)) {
			return ErrRequestInvalid
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return ErrRequestInvalid
		}
	}
	match := configRelativePattern.FindStringSubmatch(filepath.ToSlash(strings.TrimPrefix(filepath.Clean(request.ConfigPath), local.Root+string(filepath.Separator))))
	if len(match) != 2 || match[1] != request.SubscriptionID {
		return ErrRequestInvalid
	}
	if info, _ := os.Stat(request.BinaryPath); info == nil || info.Mode().Perm()&0o111 == 0 {
		return ErrRequestInvalid
	}
	return nil
}

func (local *Local) manifestPath() string {
	return filepath.Join(local.Root, "runtime", "process.json")
}

func (local *Local) readManifest() (Status, error) {
	body, err := os.ReadFile(local.manifestPath())
	if err != nil {
		return Status{}, err
	}
	var status Status
	if json.Unmarshal(body, &status) != nil || status.PID <= 1 || !subscriptionIDPattern.MatchString(status.SubscriptionID) {
		return Status{}, errors.New("invalid Mihomo process manifest")
	}
	return status, nil
}

func manifestAlive(status Status) bool {
	process, err := os.FindProcess(status.PID)
	if err != nil || process.Signal(syscall.Signal(0)) != nil {
		return false
	}
	cmdline, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(status.PID), "cmdline"))
	if err != nil {
		return false
	}
	text := strings.ReplaceAll(string(cmdline), "\x00", " ")
	return strings.Contains(text, status.BinaryPath) && strings.Contains(text, status.ConfigPath)
}

func terminate(status Status) error {
	if !manifestAlive(status) {
		return nil
	}
	process, err := os.FindProcess(status.PID)
	if err != nil {
		return err
	}
	if err := process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !manifestAlive(status) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}

func startupListenerFailed(logPath string, offset int64) bool {
	file, err := os.Open(logPath)
	if err != nil {
		return true
	}
	defer file.Close()
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return true
	}
	body, err := io.ReadAll(io.LimitReader(file, 1<<20))
	return err != nil || strings.Contains(string(body), " listen err:")
}

func writeAtomicPrivateFile(path string, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	temporary := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	if err := os.WriteFile(temporary, body, 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}
