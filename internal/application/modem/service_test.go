package modem

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/application/inventory"
	"github.com/leonfox28/simplus/internal/domain/hardware"
	domain "github.com/leonfox28/simplus/internal/domain/modem"
)

type memoryRepository struct{ records []domain.Record }

func (repository *memoryRepository) ListManagedModems(context.Context) ([]domain.Record, error) {
	return append([]domain.Record(nil), repository.records...), nil
}

func (repository *memoryRepository) CreateManagedModem(_ context.Context, record domain.Record) error {
	repository.records = append(repository.records, record)
	return nil
}

func (repository *memoryRepository) BindManagedModemIdentity(_ context.Context, modemID, equipmentFingerprint, usbSerialFingerprint string, updatedAt time.Time) error {
	for index := range repository.records {
		if repository.records[index].ID == modemID {
			repository.records[index].LegacyHardwareDeviceID = ""
			repository.records[index].EquipmentIdentityFingerprint = equipmentFingerprint
			repository.records[index].USBSerialFingerprint = usbSerialFingerprint
			repository.records[index].UpdatedAt = updatedAt
			return nil
		}
	}
	return errors.New("managed modem not found")
}

type topologySource struct {
	topology inventory.Topology
	err      error
}

type fakeRFController struct {
	state      string
	hardwareID string
	enabled    bool
}

func (controller *fakeRFController) State(_ context.Context, hardwareID string) (string, error) {
	controller.hardwareID = hardwareID
	return controller.state, nil
}

func (controller *fakeRFController) Set(_ context.Context, hardwareID string, enabled bool) (string, error) {
	controller.hardwareID, controller.enabled = hardwareID, enabled
	if enabled {
		controller.state = domain.RFStateOn
	} else {
		controller.state = domain.RFStateOff
	}
	return controller.state, nil
}

func (source *topologySource) Topology(context.Context) (inventory.Topology, error) {
	return source.topology, source.err
}

func TestManagedModemAddSeparatesCandidatesFromPersistentConfiguration(t *testing.T) {
	equipmentIdentity := strings.Repeat("a", 64)
	usbSerialIdentity := strings.Repeat("b", 64)
	capabilities := hardware.Capabilities{SIMAccess: true, SIMAPDU: true, HostVoWiFiAuth: true, RFControl: true}
	source := &topologySource{topology: inventory.Topology{
		Devices: []hardware.PhysicalDevice{{
			ID: "agent-usb-1-3", DisplayName: "China Mobile IoT ML307A", Transport: hardware.TransportUSB,
			State: hardware.DeviceAvailable, EquipmentIdentityFingerprint: equipmentIdentity, USBSerialFingerprint: usbSerialIdentity,
		}},
		ModemFunctions: []hardware.ModemFunction{{
			ID: "agent-usb-1-3-modem", PhysicalDeviceID: "agent-usb-1-3", Capabilities: capabilities,
		}},
		SIMSlots: []hardware.SIMSlot{{
			ID: "agent-usb-1-3-slot-0", PhysicalDeviceID: "agent-usb-1-3", Index: 0, Presence: hardware.SlotPresent,
		}},
	}}
	repository := &memoryRepository{}
	service, err := New(repository, source)
	if err != nil {
		t.Fatal(err)
	}
	service.random = strings.NewReader(strings.Repeat("\x01", 16))
	now := time.Date(2026, 8, 5, 13, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	rf := &fakeRFController{state: domain.RFStateOff}
	service.UseRFController(rf)

	candidates, err := service.Candidates(t.Context())
	if err != nil || len(candidates) != 1 {
		t.Fatalf("candidates=%#v error=%v", candidates, err)
	}
	if !candidates[0].Addable || candidates[0].Support != domain.SupportSupported || candidates[0].Readiness != domain.ReadinessReady || !candidates[0].Capabilities.HostVoWiFiAuth ||
		candidates[0].SIMPresence != domain.SIMPresencePresent {
		t.Fatalf("candidate=%#v", candidates[0])
	}

	added, err := service.Add(t.Context(), candidates[0].CandidateID)
	if err != nil {
		t.Fatal(err)
	}
	if added.ID != "modem_AQEBAQEBAQEBAQEBAQEBAQ" || added.State != domain.StateOnline || added.RFState != domain.RFStateUnknown ||
		added.SIMPresence != domain.SIMPresencePresent || added.AddedAt != now {
		t.Fatalf("added=%#v", added)
	}
	if len(repository.records) != 1 || repository.records[0].EquipmentIdentityFingerprint != equipmentIdentity ||
		repository.records[0].USBSerialFingerprint != usbSerialIdentity || repository.records[0].LegacyHardwareDeviceID != "" {
		t.Fatalf("persisted record=%#v", repository.records)
	}
	if _, err := service.Add(t.Context(), candidates[0].CandidateID); !errors.Is(err, ErrAlreadyManaged) {
		t.Fatalf("duplicate error=%v", err)
	}
	candidates, err = service.Candidates(t.Context())
	if err != nil || len(candidates) != 0 {
		t.Fatalf("remaining candidates=%#v error=%v", candidates, err)
	}

	views, err := service.List(t.Context())
	if err != nil || len(views) != 1 || views[0].State != domain.StateOnline || views[0].RFState != domain.RFStateOff ||
		views[0].SIMPresence != domain.SIMPresencePresent || rf.hardwareID != "agent-usb-1-3" {
		t.Fatalf("online views=%#v error=%v", views, err)
	}
	changed, err := service.SetRFState(t.Context(), added.ID, true)
	if err != nil || changed.RFState != domain.RFStateOn || !rf.enabled || rf.hardwareID != "agent-usb-1-3" {
		t.Fatalf("RF change=%#v controller=%#v error=%v", changed, rf, err)
	}
	source.topology.Devices[0].ID = "agent-usb-2-4"
	source.topology.ModemFunctions[0].PhysicalDeviceID = "agent-usb-2-4"
	source.topology.SIMSlots[0].PhysicalDeviceID = "agent-usb-2-4"
	views, err = service.List(t.Context())
	if err != nil || len(views) != 1 || views[0].State != domain.StateOnline || rf.hardwareID != "agent-usb-2-4" {
		t.Fatalf("moved-port views=%#v controller=%#v error=%v", views, rf, err)
	}
	source.topology.SIMSlots[0].Presence = hardware.SlotAbsent
	views, err = service.List(t.Context())
	if err != nil || views[0].SIMPresence != domain.SIMPresenceAbsent {
		t.Fatalf("absent-SIM views=%#v error=%v", views, err)
	}
	source.topology = inventory.Topology{}
	views, err = service.List(t.Context())
	if err != nil || len(views) != 1 || views[0].State != domain.StateOffline || !views[0].Capabilities.RFControl ||
		views[0].SIMPresence != domain.SIMPresenceUnknown {
		t.Fatalf("offline views=%#v error=%v", views, err)
	}
}

func TestManagedModemCandidateMustRemainPresentAndOperable(t *testing.T) {
	source := &topologySource{topology: inventory.Topology{
		Devices: []hardware.PhysicalDevice{{ID: "agent-usb-1-1", DisplayName: "QDC507", Transport: hardware.TransportUSB, State: hardware.DeviceAvailable}},
	}}
	service, err := New(&memoryRepository{}, source)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := service.Candidates(t.Context())
	if err != nil || len(candidates) != 1 || candidates[0].Addable || candidates[0].Support != domain.SupportNotReady ||
		candidates[0].Readiness != domain.ReadinessControlUnavailable {
		t.Fatalf("candidates=%#v error=%v", candidates, err)
	}
	if _, err := service.Add(t.Context(), "agent-usb-1-1"); !errors.Is(err, ErrCandidateNotReady) {
		t.Fatalf("not-ready error=%v", err)
	}
	if _, err := service.Add(t.Context(), "agent-usb-9-9"); !errors.Is(err, ErrCandidateNotFound) {
		t.Fatalf("missing error=%v", err)
	}
	if _, err := service.Add(t.Context(), "../ttyUSB0"); !errors.Is(err, ErrCandidateInvalid) {
		t.Fatalf("invalid error=%v", err)
	}
}

func TestCandidateReadinessReportsTheSpecificBlockingBoundary(t *testing.T) {
	identity := strings.Repeat("a", 64)
	for _, test := range []struct {
		name    string
		current observation
		byID    map[string][]observation
		want    string
	}{
		{name: "control unavailable", current: observation{}, want: domain.ReadinessControlUnavailable},
		{name: "SIM access unavailable", current: observation{hasFunction: true}, want: domain.ReadinessSIMAccessUnavailable},
		{name: "equipment identity unavailable", current: observation{hasFunction: true, capabilities: hardware.Capabilities{SIMAccess: true}}, want: domain.ReadinessEquipmentIdentityUnavailable},
		{name: "identity conflict", current: observation{hasFunction: true, capabilities: hardware.Capabilities{SIMAccess: true}, equipmentIdentity: identity}, byID: map[string][]observation{identity: {{}, {}}}, want: domain.ReadinessIdentityConflict},
		{name: "ready", current: observation{hasFunction: true, capabilities: hardware.Capabilities{SIMAccess: true}, equipmentIdentity: identity}, byID: map[string][]observation{identity: {{}}}, want: domain.ReadinessReady},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := candidateReadiness(test.current, test.byID); got != test.want {
				t.Fatalf("readiness = %q, want %q", got, test.want)
			}
		})
	}
}

func TestManagedModemRejectsDuplicateEquipmentIdentity(t *testing.T) {
	identity := strings.Repeat("c", 64)
	capabilities := hardware.Capabilities{SIMAccess: true}
	source := &topologySource{topology: inventory.Topology{
		Devices: []hardware.PhysicalDevice{
			{ID: "agent-usb-1-1", DisplayName: "ML307A A", Transport: hardware.TransportUSB, State: hardware.DeviceAvailable, EquipmentIdentityFingerprint: identity},
			{ID: "agent-usb-1-2", DisplayName: "ML307A B", Transport: hardware.TransportUSB, State: hardware.DeviceAvailable, EquipmentIdentityFingerprint: identity},
		},
		ModemFunctions: []hardware.ModemFunction{
			{ID: "function-1", PhysicalDeviceID: "agent-usb-1-1", Capabilities: capabilities},
			{ID: "function-2", PhysicalDeviceID: "agent-usb-1-2", Capabilities: capabilities},
		},
	}}
	service, err := New(&memoryRepository{}, source)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := service.Candidates(t.Context())
	if err != nil || len(candidates) != 2 || candidates[0].Addable || candidates[1].Addable ||
		candidates[0].Readiness != domain.ReadinessIdentityConflict || candidates[1].Readiness != domain.ReadinessIdentityConflict {
		t.Fatalf("duplicate candidates=%#v error=%v", candidates, err)
	}
	if _, err := service.Add(t.Context(), "agent-usb-1-1"); !errors.Is(err, ErrIdentityConflict) {
		t.Fatalf("duplicate identity add error=%v", err)
	}
}

func TestManagedModemPromotesLegacyPortBindingToEquipmentIdentity(t *testing.T) {
	now := time.Date(2026, 8, 5, 13, 0, 0, 0, time.UTC)
	identity := strings.Repeat("d", 64)
	repository := &memoryRepository{records: []domain.Record{{
		ID: "modem_AQEBAQEBAQEBAQEBAQEBAQ", LegacyHardwareDeviceID: "agent-usb-1-3",
		DisplayName: "ML307A", Model: "ML307A", Transport: hardware.TransportUSB,
		Capabilities: hardware.Capabilities{SIMAccess: true}, CreatedAt: now, UpdatedAt: now,
	}}}
	source := &topologySource{topology: inventory.Topology{
		Devices: []hardware.PhysicalDevice{{
			ID: "agent-usb-1-3", DisplayName: "ML307A", Transport: hardware.TransportUSB,
			State: hardware.DeviceAvailable, EquipmentIdentityFingerprint: identity, USBSerialFingerprint: strings.Repeat("e", 64),
		}},
		ModemFunctions: []hardware.ModemFunction{{
			ID: "function-1", PhysicalDeviceID: "agent-usb-1-3", Capabilities: hardware.Capabilities{SIMAccess: true},
		}},
	}}
	service, err := New(repository, source)
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now.Add(time.Minute) }
	views, err := service.List(t.Context())
	if err != nil || len(views) != 1 || views[0].State != domain.StateOnline {
		t.Fatalf("legacy views=%#v error=%v", views, err)
	}
	if repository.records[0].EquipmentIdentityFingerprint != identity || repository.records[0].LegacyHardwareDeviceID != "" {
		t.Fatalf("legacy record was not promoted: %#v", repository.records[0])
	}
}

func TestManagedModemListSurvivesInventoryOutage(t *testing.T) {
	now := time.Date(2026, 8, 5, 13, 0, 0, 0, time.UTC)
	repository := &memoryRepository{records: []domain.Record{{
		ID: "modem_AQEBAQEBAQEBAQEBAQEBAQ", EquipmentIdentityFingerprint: strings.Repeat("a", 64),
		DisplayName: "Main modem", Model: "ML307A", Transport: hardware.TransportUSB,
		Capabilities: hardware.Capabilities{SIMAccess: true}, CreatedAt: now, UpdatedAt: now,
	}}}
	service, err := New(repository, &topologySource{err: errors.New("agent unavailable")})
	if err != nil {
		t.Fatal(err)
	}
	views, err := service.List(t.Context())
	if err != nil || len(views) != 1 || views[0].State != domain.StateOffline {
		t.Fatalf("views=%#v error=%v", views, err)
	}
}

func TestManagedModemDoesNotResolveUnavailableObservation(t *testing.T) {
	identity := strings.Repeat("f", 64)
	repository := &memoryRepository{records: []domain.Record{{
		ID: "modem_AQEBAQEBAQEBAQEBAQEBAQ", EquipmentIdentityFingerprint: identity,
		DisplayName: "Main modem", Model: "ML307A", Transport: hardware.TransportUSB,
		Capabilities: hardware.Capabilities{SIMAccess: true},
	}}}
	source := &topologySource{topology: inventory.Topology{Devices: []hardware.PhysicalDevice{{
		ID: "agent-usb-1-3", DisplayName: "ML307A", Transport: hardware.TransportUSB,
		State: hardware.DeviceUnavailable, EquipmentIdentityFingerprint: identity,
	}}}}
	service, err := New(repository, source)
	if err != nil {
		t.Fatal(err)
	}
	views, err := service.List(t.Context())
	if err != nil || len(views) != 1 || views[0].State != domain.StateOffline {
		t.Fatalf("views=%#v error=%v", views, err)
	}
	if candidates, err := service.Candidates(t.Context()); err != nil || len(candidates) != 0 {
		t.Fatalf("candidates=%#v error=%v", candidates, err)
	}
}
