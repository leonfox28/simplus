package mihomo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/mihomosupervisor"
)

type runtimeSupervisorFake struct {
	status    mihomosupervisor.Status
	starts    []mihomosupervisor.StartRequest
	stopCalls int
}

func (fake *runtimeSupervisorFake) Status(context.Context) (mihomosupervisor.Status, error) {
	return fake.status, nil
}

func (fake *runtimeSupervisorFake) Start(_ context.Context, request mihomosupervisor.StartRequest) (mihomosupervisor.Status, error) {
	fake.starts = append(fake.starts, request)
	fake.status = mihomosupervisor.Status{
		Running:        true,
		PID:            42,
		SubscriptionID: request.SubscriptionID,
		BinaryPath:     request.BinaryPath,
		ConfigPath:     request.ConfigPath,
		StartedAt:      time.Date(2026, 8, 15, 1, 2, 3, 0, time.UTC),
	}
	return fake.status, nil
}

func (fake *runtimeSupervisorFake) Stop(context.Context) error {
	if !fake.status.Running {
		return mihomosupervisor.ErrNotRunning
	}
	fake.stopCalls++
	fake.status = mihomosupervisor.Status{}
	return nil
}

func TestNewRuntimeManagerRejectsInvalidDependencies(t *testing.T) {
	root := t.TempDir()
	config, store, _ := readyConfigFixture(root)
	supervisor := &runtimeSupervisorFake{}
	var typedNilStore *configStoreStub
	var typedNilArtifacts *ConfigManager
	var typedNilCore *CoreManager
	var typedNilSupervisor *runtimeSupervisorFake
	tests := []struct {
		name       string
		root       string
		store      RuntimeStore
		artifacts  ArtifactResolver
		core       CoreStatusReader
		supervisor mihomosupervisor.API
	}{
		{name: "empty root", root: "", store: store, artifacts: config, core: config.Core, supervisor: supervisor},
		{name: "relative root", root: "relative", store: store, artifacts: config, core: config.Core, supervisor: supervisor},
		{name: "missing store", root: root, artifacts: config, core: config.Core, supervisor: supervisor},
		{name: "missing artifact resolver", root: root, store: store, core: config.Core, supervisor: supervisor},
		{name: "missing core reader", root: root, store: store, artifacts: config, supervisor: supervisor},
		{name: "missing supervisor", root: root, store: store, artifacts: config, core: config.Core},
		{name: "typed nil store", root: root, store: typedNilStore, artifacts: config, core: config.Core, supervisor: supervisor},
		{name: "typed nil artifact resolver", root: root, store: store, artifacts: typedNilArtifacts, core: config.Core, supervisor: supervisor},
		{name: "typed nil core reader", root: root, store: store, artifacts: config, core: typedNilCore, supervisor: supervisor},
		{name: "typed nil supervisor", root: root, store: store, artifacts: config, core: config.Core, supervisor: typedNilSupervisor},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, err := NewRuntimeManager(test.root, test.store, test.artifacts, test.core, test.supervisor)
			if manager != nil || !errors.Is(err, ErrRuntimeManagerConfiguration) {
				t.Fatalf("manager=%#v err=%v", manager, err)
			}
		})
	}
}

func TestRuntimeStartAndStopUseSelectedArtifactWithoutImplicitRestart(t *testing.T) {
	root := t.TempDir()
	helper := filepath.Join(root, "versions", "v1.19.29", "mihomo")
	if err := os.MkdirAll(filepath.Dir(helper), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	config, store, nodes := readyConfigFixture(root)
	config.Core = coreStatusStub{CoreStatus{Installed: true, Version: "v1.19.29", BinaryPath: helper}}
	config.Run = func(context.Context, string, ...string) ([]byte, error) { return nil, nil }
	if _, err := config.BuildSubscription(context.Background(), configTestSubscriptionID, []byte("fixture"), nodes); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Select(context.Background(), configTestSubscriptionID); err != nil {
		t.Fatal(err)
	}
	supervisor := &runtimeSupervisorFake{}
	runtime, err := NewRuntimeManager(root, store, config, config.Core, supervisor)
	if err != nil {
		t.Fatal(err)
	}
	started, err := runtime.Start(context.Background())
	if err != nil || started.State != "running" || started.PID != 42 || started.RunningSubscriptionID != configTestSubscriptionID {
		t.Fatalf("started=%#v err=%v", started, err)
	}
	if len(supervisor.starts) != 1 || supervisor.starts[0].SubscriptionID != configTestSubscriptionID || supervisor.starts[0].BinaryPath != helper || !filepath.IsAbs(supervisor.starts[0].ConfigPath) {
		t.Fatalf("start requests=%#v", supervisor.starts)
	}
	if _, err := runtime.Start(context.Background()); err != ErrRuntimeAlreadyRunning {
		t.Fatalf("second start err=%v", err)
	}
	if _, err := config.BuildSubscription(context.Background(), configTestSubscriptionID, []byte("updated fixture"), nodes); err != nil {
		t.Fatal(err)
	}
	pending, err := runtime.Status(context.Background())
	if err != nil || !pending.PendingRestart || pending.State != "running" || pending.RunningSubscriptionID != configTestSubscriptionID {
		t.Fatalf("pending=%#v err=%v", pending, err)
	}
	stopped, err := runtime.Stop(context.Background())
	if err != nil || stopped.State != "stopped" || store.running != "" || supervisor.stopCalls != 1 {
		t.Fatalf("stopped=%#v running=%q stopCalls=%d err=%v", stopped, store.running, supervisor.stopCalls, err)
	}
}
