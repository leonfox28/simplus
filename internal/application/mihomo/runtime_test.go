package mihomo

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeStartAndStopUseSelectedArtifactWithoutImplicitRestart(t *testing.T) {
	root := t.TempDir()
	helper := filepath.Join(root, "versions", "v1.19.29", "mihomo")
	if err := os.MkdirAll(filepath.Dir(helper), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, []byte("#!/bin/sh\ntrap 'exit 0' TERM INT\nwhile :; do sleep 1; done\n"), 0o700); err != nil {
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
	runtime := NewRuntimeManager(root, store, config, config.Core)
	started, err := runtime.Start(context.Background())
	if err != nil || started.State != "running" || started.PID <= 1 || started.RunningSubscriptionID != configTestSubscriptionID {
		t.Fatalf("started=%#v err=%v", started, err)
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
	if err != nil || stopped.State != "stopped" || store.running != "" {
		t.Fatalf("stopped=%#v running=%q err=%v", stopped, store.running, err)
	}
}
