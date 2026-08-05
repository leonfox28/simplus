package mihomo

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/leonfox28/simplus/internal/mihomosupervisor"
)

var (
	ErrRuntimeAlreadyRunning = errors.New("Mihomo is already running")
	ErrRuntimeNotRunning     = errors.New("Mihomo is not running")
	ErrRuntimeStartupFailed  = errors.New("Mihomo listeners failed during startup")
)

type RuntimeStore interface {
	ReadMihomoRuntimeSelection(context.Context) (string, string, error)
	WriteMihomoRunningSubscription(context.Context, string, time.Time) error
}
type ArtifactResolver interface {
	Artifact(string) (ArtifactMetadata, string, error)
	Select(context.Context, string) (ConfigStatus, error)
}

type RuntimeStatus struct {
	State                  string    `json:"state"`
	PID                    int       `json:"pid"`
	SelectedSubscriptionID string    `json:"selectedSubscriptionId"`
	RunningSubscriptionID  string    `json:"runningSubscriptionId"`
	PendingRestart         bool      `json:"pendingRestart"`
	StartedAt              time.Time `json:"startedAt"`
	LastErrorCode          string    `json:"lastErrorCode"`
}
type RuntimeManager struct {
	Root       string
	Store      RuntimeStore
	Artifacts  ArtifactResolver
	Core       CoreStatusReader
	Supervisor mihomosupervisor.API
	Now        func() time.Time
	mu         sync.Mutex
}

func NewRuntimeManager(root string, store RuntimeStore, artifacts ArtifactResolver, core CoreStatusReader) *RuntimeManager {
	local, _ := mihomosupervisor.NewLocal(root)
	return NewRuntimeManagerWithSupervisor(root, store, artifacts, core, local)
}

func NewRuntimeManagerWithSupervisor(root string, store RuntimeStore, artifacts ArtifactResolver, core CoreStatusReader, supervisor mihomosupervisor.API) *RuntimeManager {
	return &RuntimeManager{Root: root, Store: store, Artifacts: artifacts, Core: core, Supervisor: supervisor, Now: time.Now}
}

func (manager *RuntimeManager) Status(ctx context.Context) (RuntimeStatus, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return manager.statusLocked(ctx)
}
func (manager *RuntimeManager) statusLocked(ctx context.Context) (RuntimeStatus, error) {
	selected, recordedRunning, err := manager.Store.ReadMihomoRuntimeSelection(ctx)
	if err != nil {
		return RuntimeStatus{}, err
	}
	status := RuntimeStatus{State: "stopped", SelectedSubscriptionID: selected}
	process, err := manager.Supervisor.Status(ctx)
	if err != nil {
		return RuntimeStatus{}, err
	}
	if !process.Running {
		if recordedRunning != "" {
			if err := manager.Store.WriteMihomoRunningSubscription(ctx, "", manager.Now().UTC()); err != nil {
				return RuntimeStatus{}, err
			}
		}
		return status, nil
	}
	if recordedRunning != process.SubscriptionID {
		if err := manager.Store.WriteMihomoRunningSubscription(ctx, process.SubscriptionID, manager.Now().UTC()); err != nil {
			return RuntimeStatus{}, err
		}
	}
	status.State = "running"
	status.PID = process.PID
	status.RunningSubscriptionID = process.SubscriptionID
	_, selectedPath, artifactErr := manager.Artifacts.Artifact(selected)
	status.PendingRestart = selected != process.SubscriptionID || artifactErr != nil || selectedPath != process.ConfigPath
	status.StartedAt = process.StartedAt
	return status, nil
}
func (manager *RuntimeManager) Start(ctx context.Context) (RuntimeStatus, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	current, err := manager.statusLocked(ctx)
	if err != nil {
		return RuntimeStatus{}, err
	}
	if current.State == "running" {
		return current, ErrRuntimeAlreadyRunning
	}
	if current.SelectedSubscriptionID == "" {
		return current, ErrConfigNotReady
	}
	if _, err := manager.Artifacts.Select(ctx, current.SelectedSubscriptionID); err != nil {
		return current, err
	}
	if err := manager.startLocked(ctx, current.SelectedSubscriptionID); err != nil {
		return RuntimeStatus{State: "fault", SelectedSubscriptionID: current.SelectedSubscriptionID, LastErrorCode: "MIHOMO_START_FAILED"}, err
	}
	return manager.statusLocked(ctx)
}
func (manager *RuntimeManager) Stop(ctx context.Context) (RuntimeStatus, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	status, err := manager.statusLocked(ctx)
	if err != nil {
		return RuntimeStatus{}, err
	}
	if status.State != "running" {
		return status, ErrRuntimeNotRunning
	}
	if err := manager.stopLocked(ctx); err != nil {
		status.State = "fault"
		status.LastErrorCode = "MIHOMO_STOP_FAILED"
		return status, err
	}
	return manager.statusLocked(ctx)
}
func (manager *RuntimeManager) Restart(ctx context.Context) (RuntimeStatus, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	status, err := manager.statusLocked(ctx)
	if err != nil {
		return RuntimeStatus{}, err
	}
	if status.SelectedSubscriptionID == "" {
		return status, ErrConfigNotReady
	}
	if _, err := manager.Artifacts.Select(ctx, status.SelectedSubscriptionID); err != nil {
		return status, err
	}
	oldProcess, _ := manager.Supervisor.Status(ctx)
	if status.State == "running" {
		if err := manager.stopLocked(ctx); err != nil {
			return RuntimeStatus{}, err
		}
	}
	if err := manager.startLocked(ctx, status.SelectedSubscriptionID); err != nil {
		// Subscription artifacts are immutable. If the newly selected artifact
		// cannot start, restore the exact config that was running before the
		// restart, including an older version of the same subscription.
		if oldProcess.SubscriptionID != "" {
			_ = manager.startConfigLocked(ctx, oldProcess.SubscriptionID, oldProcess.ConfigPath)
		}
		failed, _ := manager.statusLocked(ctx)
		failed.LastErrorCode = "MIHOMO_RESTART_FAILED"
		return failed, err
	}
	return manager.statusLocked(ctx)
}
func (manager *RuntimeManager) startLocked(ctx context.Context, subscriptionID string) error {
	_, configPath, err := manager.Artifacts.Artifact(subscriptionID)
	if err != nil {
		return err
	}
	return manager.startConfigLocked(ctx, subscriptionID, configPath)
}
func (manager *RuntimeManager) startConfigLocked(ctx context.Context, subscriptionID, configPath string) error {
	info, err := os.Stat(configPath)
	if err != nil || !info.Mode().IsRegular() {
		return ErrConfigNotReady
	}
	core, err := manager.Core.Status()
	if err != nil || !core.Installed {
		return ErrConfigNotReady
	}
	if _, err := manager.Supervisor.Start(ctx, mihomosupervisor.StartRequest{SubscriptionID: subscriptionID, BinaryPath: core.BinaryPath, ConfigPath: configPath}); err != nil {
		if errors.Is(err, mihomosupervisor.ErrStartupFailed) {
			return ErrRuntimeStartupFailed
		}
		return err
	}
	if err := manager.Store.WriteMihomoRunningSubscription(ctx, subscriptionID, manager.Now().UTC()); err != nil {
		_ = manager.Supervisor.Stop(context.WithoutCancel(ctx))
		return err
	}
	return nil
}
func (manager *RuntimeManager) stopLocked(ctx context.Context) error {
	if err := manager.Supervisor.Stop(ctx); err != nil && !errors.Is(err, mihomosupervisor.ErrNotRunning) {
		return err
	}
	return manager.clearRunning(ctx)
}
func (manager *RuntimeManager) clearRunning(ctx context.Context) error {
	return manager.Store.WriteMihomoRunningSubscription(ctx, "", manager.Now().UTC())
}
