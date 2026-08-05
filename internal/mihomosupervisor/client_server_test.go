package mihomosupervisor

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"testing"
)

func TestUnixClientServerLifecycle(t *testing.T) {
	local, request := supervisorFixture(t, "#!/bin/sh\ntrap 'exit 0' TERM INT\nwhile :; do sleep 1; done\n")
	socketPath := filepath.Join(t.TempDir(), "supervisor.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: NewHandler(local, nil)}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
	})
	client, err := NewClient(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	started, err := client.Start(context.Background(), request)
	if err != nil || !started.Running || started.SubscriptionID != testSubscriptionID {
		t.Fatalf("started=%#v err=%v", started, err)
	}
	observed, err := client.Status(context.Background())
	if err != nil || observed.PID != started.PID {
		t.Fatalf("observed=%#v err=%v", observed, err)
	}
	if err := client.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}
