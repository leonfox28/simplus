package vowifi

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/application/inventory"
	lineegressapp "github.com/leonfox28/simplus/internal/application/lineegress"
	mihomoapp "github.com/leonfox28/simplus/internal/application/mihomo"
	"github.com/leonfox28/simplus/internal/domain/accessmode"
	"github.com/leonfox28/simplus/internal/domain/hardware"
	lineegressdomain "github.com/leonfox28/simplus/internal/domain/lineegress"
	domain "github.com/leonfox28/simplus/internal/domain/vowifi"
	"github.com/leonfox28/simplus/internal/vowifisupervisor"
)

const testLineID = "agent-line-0123456789abcdef0123456789abcdef"

type memoryStore struct{ desires map[string]domain.Desire }

func (store *memoryStore) ListVoWiFiDesires(context.Context) ([]domain.Desire, error) {
	values := make([]domain.Desire, 0, len(store.desires))
	for _, value := range store.desires {
		values = append(values, value)
	}
	return values, nil
}
func (store *memoryStore) PutVoWiFiDesire(_ context.Context, value domain.Desire) error {
	store.desires[value.LineID] = value
	return nil
}

type fixedInventory struct{ topology inventory.Topology }

func (source fixedInventory) Topology(context.Context) (inventory.Topology, error) {
	return source.topology, nil
}

type fixedEgress struct{ views []lineegressapp.View }

func (source fixedEgress) List(context.Context) ([]lineegressapp.View, error) {
	return append([]lineegressapp.View(nil), source.views...), nil
}

type fakeMihomo struct {
	starts, restarts int
}

func (fake *fakeMihomo) Start(context.Context) (mihomoapp.RuntimeStatus, error) {
	fake.starts++
	return mihomoapp.RuntimeStatus{State: "running"}, nil
}
func (fake *fakeMihomo) Restart(context.Context) (mihomoapp.RuntimeStatus, error) {
	fake.restarts++
	return mihomoapp.RuntimeStatus{State: "running"}, nil
}

type recoveringEgress struct{ runtime *fakeMihomo }

func (source recoveringEgress) List(context.Context) ([]lineegressapp.View, error) {
	view := lineegressapp.View{LineID: testLineID, Mode: lineegressdomain.ModeMihomoCountry, CountryCode: "GB", CountryName: "英国"}
	if source.runtime.starts == 0 && source.runtime.restarts == 0 {
		view.ReadinessReason = "MIHOMO_NOT_RUNNING"
	} else {
		view.Ready, view.ReadinessReason = true, "READY"
	}
	return []lineegressapp.View{view}, nil
}

type fakeSupervisor struct {
	statuses map[string]vowifisupervisor.Status
	starts   []vowifisupervisor.StartRequest
	stops    []string
}

func (fake *fakeSupervisor) List(context.Context) ([]vowifisupervisor.Status, error) {
	values := make([]vowifisupervisor.Status, 0, len(fake.statuses))
	for _, value := range fake.statuses {
		values = append(values, value)
	}
	return values, nil
}
func (fake *fakeSupervisor) Start(_ context.Context, request vowifisupervisor.StartRequest) (vowifisupervisor.Status, error) {
	if current, found := fake.statuses[request.LineID]; found && current.State != vowifisupervisor.StateStopped && current.State != vowifisupervisor.StateFailed {
		return current, vowifisupervisor.ErrAlreadyRunning
	}
	fake.starts = append(fake.starts, request)
	status := vowifisupervisor.Status{LineID: request.LineID, State: vowifisupervisor.StateStarting, EgressMode: request.EgressMode, CountryCode: request.CountryCode}
	fake.statuses[request.LineID] = status
	return status, nil
}

func TestRepeatedActivateReturnsObservedRuntimeState(t *testing.T) {
	service, _, supervisor := readyFixture()
	first, err := service.Activate(context.Background(), testLineID)
	if err != nil {
		t.Fatal(err)
	}
	supervisor.statuses[testLineID] = vowifisupervisor.Status{LineID: testLineID, State: vowifisupervisor.StateOnline, Online: true, EgressMode: vowifisupervisor.EgressMihomoCountry, CountryCode: "GB"}
	second, err := service.Activate(context.Background(), testLineID)
	if err != nil || first.State != vowifisupervisor.StateStarting || second.State != vowifisupervisor.StateOnline || !second.Online {
		t.Fatalf("first=%#v second=%#v err=%v", first, second, err)
	}
}
func (fake *fakeSupervisor) Stop(_ context.Context, lineID string) (vowifisupervisor.Status, error) {
	fake.stops = append(fake.stops, lineID)
	status, found := fake.statuses[lineID]
	if !found {
		return vowifisupervisor.Status{}, vowifisupervisor.ErrNotRunning
	}
	status.State, status.Online = vowifisupervisor.StateStopped, false
	fake.statuses[lineID] = status
	return status, nil
}

func readyFixture() (*Service, *memoryStore, *fakeSupervisor) {
	line := inventory.Line{
		ID: testLineID, AccessMode: accessmode.HostVoWiFiOnly, AccessModeConfigured: true,
		State: inventory.LineReady, RFSafety: inventory.RFSafetyOff,
		Capabilities: hardware.Capabilities{HostVoWiFiAuth: true},
	}
	store := &memoryStore{desires: make(map[string]domain.Desire)}
	supervisor := &fakeSupervisor{statuses: make(map[string]vowifisupervisor.Status)}
	service, _ := New(store, fixedInventory{topology: inventory.Topology{Lines: []inventory.Line{line}}}, fixedEgress{views: []lineegressapp.View{{
		LineID: testLineID, Mode: lineegressdomain.ModeMihomoCountry, CountryCode: "GB", CountryName: "英国", Ready: true, ReadinessReason: "READY",
	}}}, &fakeMihomo{}, supervisor)
	service.Now = func() time.Time { return time.Date(2026, 8, 5, 4, 0, 0, 0, time.UTC) }
	return service, store, supervisor
}

func TestActivatePersistsIntentAndStartsCountryGroup(t *testing.T) {
	service, store, supervisor := readyFixture()
	state, err := service.Activate(context.Background(), testLineID)
	if err != nil {
		t.Fatal(err)
	}
	if !state.DesiredActive || state.State != vowifisupervisor.StateStarting || len(supervisor.starts) != 1 {
		t.Fatalf("state=%#v starts=%#v", state, supervisor.starts)
	}
	request := supervisor.starts[0]
	if request.EgressMode != vowifisupervisor.EgressMihomoCountry || request.CountryCode != "GB" {
		t.Fatalf("request=%#v", request)
	}
	if desire := store.desires[testLineID]; !desire.DesiredActive || desire.UpdatedAt.IsZero() {
		t.Fatalf("desire=%#v", desire)
	}
}

func TestActivateFailsClosedWhenLineOrEgressIsNotReady(t *testing.T) {
	service, store, supervisor := readyFixture()
	service.Egress = fixedEgress{views: []lineegressapp.View{{LineID: testLineID, Mode: lineegressdomain.ModeMihomoCountry, CountryCode: "GB", ReadinessReason: "COUNTRY_NOT_FOUND"}}}
	if _, err := service.Activate(context.Background(), testLineID); !errors.Is(err, ErrLineNotReady) {
		t.Fatalf("error=%v", err)
	}
	if len(store.desires) != 0 || len(supervisor.starts) != 0 {
		t.Fatalf("side effects desires=%#v starts=%#v", store.desires, supervisor.starts)
	}
}

func TestDeactivateClearsIntentWhenRuntimeIsAlreadyAbsent(t *testing.T) {
	service, store, supervisor := readyFixture()
	store.desires[testLineID] = domain.Desire{LineID: testLineID, DesiredActive: true}

	state, err := service.Deactivate(context.Background(), testLineID)
	if err != nil {
		t.Fatal(err)
	}
	if state.DesiredActive || state.State != vowifisupervisor.StateStopped || state.Online {
		t.Fatalf("state=%#v", state)
	}
	if desire := store.desires[testLineID]; desire.DesiredActive {
		t.Fatalf("desire=%#v", desire)
	}
	if len(supervisor.stops) != 1 || supervisor.stops[0] != testLineID {
		t.Fatalf("stops=%#v", supervisor.stops)
	}
}

func TestActivateRecoversRequiredMihomoBeforeStartingLine(t *testing.T) {
	service, _, supervisor := readyFixture()
	runtime := &fakeMihomo{}
	service.Mihomo = runtime
	service.Egress = recoveringEgress{runtime: runtime}
	state, err := service.Activate(context.Background(), testLineID)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.starts != 1 || len(supervisor.starts) != 1 || state.State != vowifisupervisor.StateStarting {
		t.Fatalf("mihomo starts=%d supervisor starts=%#v state=%#v", runtime.starts, supervisor.starts, state)
	}
}

func TestReconcileRecoversMihomoAfterSupervisorRestart(t *testing.T) {
	service, store, supervisor := readyFixture()
	runtime := &fakeMihomo{}
	service.Mihomo = runtime
	service.Egress = recoveringEgress{runtime: runtime}
	store.desires[testLineID] = domain.Desire{LineID: testLineID, DesiredActive: true}
	if err := service.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if runtime.starts != 1 || len(supervisor.starts) != 1 || supervisor.starts[0].CountryCode != "GB" {
		t.Fatalf("mihomo starts=%d supervisor starts=%#v", runtime.starts, supervisor.starts)
	}
}

func TestReconcileRestartsChangedEgressAndStopsInvalidRuntime(t *testing.T) {
	service, store, supervisor := readyFixture()
	store.desires[testLineID] = domain.Desire{LineID: testLineID, DesiredActive: true}
	supervisor.statuses[testLineID] = vowifisupervisor.Status{
		LineID: testLineID, State: vowifisupervisor.StateOnline, Online: true,
		EgressMode: vowifisupervisor.EgressMihomoCountry, CountryCode: "JP",
	}
	if err := service.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(supervisor.stops) != 1 || len(supervisor.starts) != 1 || supervisor.starts[0].CountryCode != "GB" {
		t.Fatalf("stops=%#v starts=%#v", supervisor.stops, supervisor.starts)
	}

	service.Inventory = fixedInventory{topology: inventory.Topology{}}
	if err := service.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(supervisor.stops) != 2 {
		t.Fatalf("stops=%#v", supervisor.stops)
	}
}

func TestListUsesRuntimeFactWithoutInventingOnline(t *testing.T) {
	service, store, supervisor := readyFixture()
	store.desires[testLineID] = domain.Desire{LineID: testLineID, DesiredActive: true}
	supervisor.statuses[testLineID] = vowifisupervisor.Status{LineID: testLineID, State: vowifisupervisor.StateReconnecting, ErrorCode: "IMS_REFRESH_FAILED", EgressMode: vowifisupervisor.EgressMihomoCountry, CountryCode: "GB"}
	states, err := service.List(context.Background())
	if err != nil || len(states) != 1 {
		t.Fatalf("states=%#v err=%v", states, err)
	}
	if states[0].Online || states[0].State != vowifisupervisor.StateReconnecting || states[0].LastErrorCode != "IMS_REFRESH_FAILED" {
		t.Fatalf("state=%#v", states[0])
	}
}
