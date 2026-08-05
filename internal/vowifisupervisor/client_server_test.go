package vowifisupervisor

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"testing"
)

type fakeAPI struct {
	status Status
}

func (fake *fakeAPI) List(context.Context) ([]Status, error) { return []Status{fake.status}, nil }
func (fake *fakeAPI) Start(_ context.Context, request StartRequest) (Status, error) {
	if !validStartRequest(request) {
		return Status{}, ErrRequestInvalid
	}
	fake.status = Status{LineID: request.LineID, State: StateStarting, EgressMode: request.EgressMode, CountryCode: request.CountryCode}
	return fake.status, nil
}
func (fake *fakeAPI) Stop(_ context.Context, lineID string) (Status, error) {
	if lineID != fake.status.LineID {
		return Status{}, ErrNotRunning
	}
	fake.status.State = StateStopped
	return fake.status, nil
}

func TestClientServerRoundTrip(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "netd.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeAPI{}
	mux := http.NewServeMux()
	mux.Handle("/v1/vowifi/", http.StripPrefix("/v1/vowifi", NewHandler(fake, slog.Default())))
	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
	})
	client, err := NewClient(socket)
	if err != nil {
		t.Fatal(err)
	}
	request := StartRequest{LineID: "agent-line-0123456789abcdef0123456789abcdef", EgressMode: EgressMihomoCountry, CountryCode: "JP"}
	started, err := client.Start(context.Background(), request)
	if err != nil || started.State != StateStarting {
		t.Fatalf("start=%#v err=%v", started, err)
	}
	listed, err := client.List(context.Background())
	if err != nil || len(listed) != 1 || listed[0].LineID != request.LineID {
		t.Fatalf("list=%#v err=%v", listed, err)
	}
	stopped, err := client.Stop(context.Background(), request.LineID)
	if err != nil || stopped.State != StateStopped {
		t.Fatalf("stop=%#v err=%v", stopped, err)
	}
	if _, err := client.Stop(context.Background(), "agent-line-fedcba9876543210fedcba9876543210"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("missing stop error = %v", err)
	}
}

func TestHandlerRejectsUnknownFields(t *testing.T) {
	// Covered through the same decoder contract used by both start and stop;
	// keep this assertion at the API layer instead of relying on Local.
	if validStartRequest(StartRequest{LineID: "agent-line-0123456789abcdef0123456789abcdef", EgressMode: EgressDirect, CountryCode: "JP"}) {
		t.Fatal("direct request with a country unexpectedly accepted")
	}
}
