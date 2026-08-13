package line

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/application/inventory"
	"github.com/leonfox28/simplus/internal/domain/hardware"
	domain "github.com/leonfox28/simplus/internal/domain/line"
	modemdomain "github.com/leonfox28/simplus/internal/domain/modem"
)

type memoryRepository struct {
	lines  []domain.Record
	modems []modemdomain.Record
}

func (repository *memoryRepository) ListManagedLines(context.Context) ([]domain.Record, error) {
	return append([]domain.Record(nil), repository.lines...), nil
}

func (repository *memoryRepository) CreateManagedLine(_ context.Context, record domain.Record) error {
	repository.lines = append(repository.lines, record)
	return nil
}

func (repository *memoryRepository) UpdateManagedLine(_ context.Context, lineID, displayName string, updatedAt time.Time) error {
	for index := range repository.lines {
		if repository.lines[index].ID == lineID {
			repository.lines[index].DisplayName = displayName
			repository.lines[index].UpdatedAt = updatedAt
			return nil
		}
	}
	return domain.ErrNotFound
}

func (repository *memoryRepository) ListManagedModems(context.Context) ([]modemdomain.Record, error) {
	return append([]modemdomain.Record(nil), repository.modems...), nil
}

type topologySource struct{ topology inventory.Topology }

func (source *topologySource) Topology(context.Context) (inventory.Topology, error) {
	return source.topology, nil
}

type fixedPhoneNumberSource struct {
	numbers map[string]string
	err     error
	calls   *int
}

func (source fixedPhoneNumberSource) CurrentPhoneNumbers(context.Context) (map[string]string, error) {
	if source.calls != nil {
		*source.calls++
	}
	return source.numbers, source.err
}

func TestManagedLineOwnsStableBusinessIdentityAndResolvesRuntimeTarget(t *testing.T) {
	modemIdentity := strings.Repeat("a", 64)
	simIdentity := strings.Repeat("b", 64)
	capabilities := hardware.Capabilities{SIMAccess: true, SIMAPDU: true, HostVoWiFiAuth: true}
	modem := modemdomain.Record{
		ID: "modem_AQEBAQEBAQEBAQEBAQEBAQ", EquipmentIdentityFingerprint: modemIdentity,
		DisplayName: "Main ML307A", Model: "ML307A", Transport: hardware.TransportUSB,
		Capabilities: capabilities,
	}
	repository := &memoryRepository{modems: []modemdomain.Record{modem}}
	source := &topologySource{topology: readyTopology("agent-usb-1-2", modemIdentity, simIdentity, capabilities)}
	service, err := New(repository, source)
	if err != nil {
		t.Fatal(err)
	}
	service.random = strings.NewReader(strings.Repeat("\x01", 16))
	now := time.Date(2026, 8, 5, 14, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	candidates, err := service.Candidates(t.Context())
	if err != nil || len(candidates) != 1 || !candidates[0].Addable || candidates[0].ManagedModemID != modem.ID ||
		candidates[0].HomeOperatorName != "VOXI" || candidates[0].HomeOperatorCode != "234-15" {
		t.Fatalf("candidates=%#v error=%v", candidates, err)
	}
	created, err := service.Add(t.Context(), candidates[0].CandidateID, "VOXI primary")
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "line_AQEBAQEBAQEBAQEBAQEBAQ" || created.State != domain.StateReady || created.ManagedModemID != modem.ID ||
		len(created.PhoneNumbers) != 1 || created.PhoneNumbers[0].Number != "+12025550123" || created.PhoneNumbers[0].Sources[0] != domain.PhoneNumberSourceCellularSIM {
		t.Fatalf("created=%#v", created)
	}
	if len(repository.lines) != 1 || repository.lines[0].SubscriptionIdentityFingerprint != simIdentity ||
		repository.lines[0].SubscriptionDisplayHint != "ICCID •••• 1234" {
		t.Fatalf("persisted=%#v", repository.lines)
	}
	if _, err := service.Add(t.Context(), candidates[0].CandidateID, "duplicate"); !errors.Is(err, ErrAlreadyManaged) {
		t.Fatalf("duplicate add error=%v", err)
	}
	phoneSourceCalls := 0
	service.UsePhoneNumberSource(fixedPhoneNumberSource{numbers: map[string]string{created.ID: "+447700900123"}, calls: &phoneSourceCalls})
	candidates, err = service.Candidates(t.Context())
	if err != nil || len(candidates) != 1 || candidates[0].Readiness != domain.CandidateAlreadyAdded || candidates[0].Addable {
		t.Fatalf("remaining candidates=%#v error=%v", candidates, err)
	}

	topology, err := service.Topology(t.Context())
	if err != nil || len(topology.Lines) != 1 || topology.Lines[0].ID != created.ID || topology.Lines[0].CellularPhoneNumber != "+12025550123" ||
		topology.Lines[0].RuntimeLineID != "agent-line-"+simIdentity[:32] || topology.Lines[0].ManagedModemID != modem.ID {
		t.Fatalf("managed topology=%#v error=%v", topology.Lines, err)
	}
	if phoneSourceCalls != 0 {
		t.Fatalf("business topology consulted display-only IMS source %d times", phoneSourceCalls)
	}

	source.topology = readyTopology("agent-usb-2-4", modemIdentity, simIdentity, capabilities)
	views, err := service.List(t.Context())
	if err != nil || len(views) != 1 || views[0].State != domain.StateReady ||
		views[0].ManagedModemModel != "ML307A-DSLN" || views[0].ManagedModemSerialNumber != "SYNTHETIC-MODULE-0001" {
		t.Fatalf("moved-port views=%#v error=%v", views, err)
	}
	source.topology.Devices[0].ModemSerialNumber = ""
	views, err = service.List(t.Context())
	if err != nil || len(views) != 1 || views[0].ManagedModemSerialNumber != "ML307A-SERIAL-0001" {
		t.Fatalf("USB serial fallback views=%#v error=%v", views, err)
	}

	source.topology = readyTopology("agent-usb-2-4", modemIdentity, strings.Repeat("c", 64), capabilities)
	views, err = service.List(t.Context())
	if err != nil || views[0].State != domain.StateSIMUnavailable || len(views[0].PhoneNumbers) != 0 {
		t.Fatalf("changed-SIM views=%#v error=%v", views, err)
	}
	source.topology = readyTopology("agent-usb-2-4", modemIdentity, simIdentity, capabilities)
	source.topology.Devices[0].State = hardware.DeviceUnavailable
	views, err = service.List(t.Context())
	if err != nil || views[0].State != domain.StateModemOffline {
		t.Fatalf("offline-modem views=%#v error=%v", views, err)
	}
	if views[0].ManagedModemModel != "" || views[0].ManagedModemSerialNumber != "" {
		t.Fatalf("offline view leaked stale model or serial=%#v", views[0])
	}
	updated, err := service.Update(t.Context(), created.ID, "VOXI standby")
	if err != nil || updated.DisplayName != "VOXI standby" {
		t.Fatalf("updated=%#v error=%v", updated, err)
	}
}

func TestManagedLineRejectsCandidateAfterSIMChanges(t *testing.T) {
	modemIdentity := strings.Repeat("a", 64)
	capabilities := hardware.Capabilities{SIMAccess: true}
	modem := modemdomain.Record{
		ID: "modem_AQEBAQEBAQEBAQEBAQEBAQ", EquipmentIdentityFingerprint: modemIdentity,
		DisplayName: "Main ML307A", Capabilities: capabilities,
	}
	source := &topologySource{topology: readyTopology("agent-usb-1-2", modemIdentity, strings.Repeat("b", 64), capabilities)}
	service, err := New(&memoryRepository{modems: []modemdomain.Record{modem}}, source)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := service.Candidates(t.Context())
	if err != nil || len(candidates) != 1 || !candidates[0].Addable {
		t.Fatalf("initial candidates=%#v error=%v", candidates, err)
	}
	staleID := candidates[0].CandidateID
	source.topology = readyTopology("agent-usb-1-2", modemIdentity, strings.Repeat("c", 64), capabilities)
	if _, err := service.Add(t.Context(), staleID, "stale SIM"); !errors.Is(err, ErrCandidateNotFound) {
		t.Fatalf("stale candidate error=%v", err)
	}
}

func TestManagedLineRequiresAnExplicitlyManagedModem(t *testing.T) {
	capabilities := hardware.Capabilities{SIMAccess: true}
	source := &topologySource{topology: readyTopology("agent-usb-1-2", strings.Repeat("a", 64), strings.Repeat("b", 64), capabilities)}
	service, err := New(&memoryRepository{}, source)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := service.Candidates(t.Context())
	if err != nil || len(candidates) != 0 {
		t.Fatalf("unmanaged modem candidates=%#v error=%v", candidates, err)
	}
	if _, err := service.Add(t.Context(), "line-candidate-0123456789abcdef0123456789abcdef", "line"); !errors.Is(err, ErrCandidateNotFound) {
		t.Fatalf("add error=%v", err)
	}
	if _, err := service.Add(t.Context(), "line-candidate-short", "line"); !errors.Is(err, ErrRequestInvalid) {
		t.Fatalf("invalid candidate error=%v", err)
	}
}

func readyTopology(deviceID, modemIdentity, simIdentity string, capabilities hardware.Capabilities) inventory.Topology {
	profileID := "agent-profile-" + simIdentity[:32]
	lineID := "agent-line-" + simIdentity[:32]
	return inventory.Topology{
		Generation: 1, ObservedAt: time.Unix(1, 0),
		Devices: []hardware.PhysicalDevice{{
			ID: deviceID, DisplayName: "ML307A", Transport: hardware.TransportUSB, State: hardware.DeviceAvailable,
			ModemModel: "ML307A-DSLN", ModemSerialNumber: "SYNTHETIC-MODULE-0001", USBSerialNumber: "ML307A-SERIAL-0001",
			EquipmentIdentityFingerprint: modemIdentity, Generation: 1,
		}},
		ModemFunctions: []hardware.ModemFunction{{
			ID: deviceID + "-modem", PhysicalDeviceID: deviceID, DisplayName: "ML307A modem",
			Backend: hardware.BackendDirectAT, Capabilities: capabilities, Generation: 1,
		}},
		SIMSlots: []hardware.SIMSlot{{ID: deviceID + "-slot-0", PhysicalDeviceID: deviceID, Index: 0, Presence: hardware.SlotPresent, ActiveMediaID: deviceID + "-media", Generation: 1}},
		SIMMedia: []hardware.SIMMedia{{
			ID: deviceID + "-media", SIMSlotID: deviceID + "-slot-0", Kind: hardware.MediaUICC,
			IdentityState: hardware.MediaIdentityKnown, IdentityFingerprint: simIdentity, DisplayIdentityHint: "ICCID •••• 1234", Generation: 1,
		}},
		SubscriptionProfiles: []inventory.SubscriptionProfile{{SubscriptionProfile: hardware.SubscriptionProfile{
			ID: profileID, SIMMediaID: deviceID + "-media", DisplayName: "Active SIM", State: hardware.ProfileActive,
			IdentityFingerprint: simIdentity, DisplayIdentityHint: "ICCID •••• 1234",
			HomeOperatorName: "VOXI", HomeOperatorCode: "234-15", Generation: 1,
			CellularPhoneNumber: "+12025550123",
		}}},
		ResourceGroups: []hardware.ResourceGroup{{
			ID: deviceID + "-resources", PhysicalDeviceID: deviceID, DisplayName: "resources",
			Resources: []string{hardware.ResourceSIMAccess}, ModemFunctionIDs: []string{deviceID + "-modem"},
			SIMSlotIDs: []string{deviceID + "-slot-0"}, MaxConcurrentOps: 1, Generation: 1,
		}},
		Lines: []inventory.Line{{
			ID: lineID, PhysicalDeviceID: deviceID, ModemFunctionID: deviceID + "-modem",
			SubscriptionProfileID: profileID, ResourceGroupID: deviceID + "-resources", DisplayName: "Observed line",
			Generation: 1, Capabilities: capabilities, CellularPhoneNumber: "+12025550123", State: inventory.LineReady,
		}},
	}
}

func TestManagedLineMergesCurrentCellularAndIMSObservations(t *testing.T) {
	const lineID = "line_AQEBAQEBAQEBAQEBAQEBAQ"
	modemIdentity, simIdentity := strings.Repeat("a", 64), strings.Repeat("b", 64)
	capabilities := hardware.Capabilities{SIMAccess: true, SIMAPDU: true, HostVoWiFiAuth: true}
	modem := modemdomain.Record{ID: "modem_AQEBAQEBAQEBAQEBAQEBAQ", EquipmentIdentityFingerprint: modemIdentity, DisplayName: "Synthetic modem", Capabilities: capabilities}
	record := domain.Record{ID: lineID, ManagedModemID: modem.ID, SIMSlotIndex: 0, SubscriptionIdentityFingerprint: simIdentity, SubscriptionDisplayHint: "ICCID •••• 1234", DisplayName: "Synthetic line", CreatedAt: time.Unix(1, 0)}
	for _, test := range []struct {
		name, cellular, ims string
		failure             bool
		want                []domain.PhoneNumberObservation
	}{
		{name: "empty", want: []domain.PhoneNumberObservation{}},
		{name: "cellular only", cellular: "+12025550123", want: []domain.PhoneNumberObservation{{Number: "+12025550123", Sources: []string{domain.PhoneNumberSourceCellularSIM}}}},
		{name: "IMS only", ims: "+447700900123", want: []domain.PhoneNumberObservation{{Number: "+447700900123", Sources: []string{domain.PhoneNumberSourceIMS}}}},
		{name: "same value", cellular: "+12025550123", ims: "+12025550123", want: []domain.PhoneNumberObservation{{Number: "+12025550123", Sources: []string{domain.PhoneNumberSourceCellularSIM, domain.PhoneNumberSourceIMS}}}},
		{name: "different sorted values", cellular: "+447700900123", ims: "+12025550123", want: []domain.PhoneNumberObservation{{Number: "+12025550123", Sources: []string{domain.PhoneNumberSourceIMS}}, {Number: "+447700900123", Sources: []string{domain.PhoneNumberSourceCellularSIM}}}},
		{name: "IMS failure is best effort", cellular: "+12025550123", failure: true, want: []domain.PhoneNumberObservation{{Number: "+12025550123", Sources: []string{domain.PhoneNumberSourceCellularSIM}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			topology := readyTopology("agent-usb-1-2", modemIdentity, simIdentity, capabilities)
			topology.Lines[0].CellularPhoneNumber = test.cellular
			topology.SubscriptionProfiles[0].CellularPhoneNumber = test.cellular
			service, err := New(&memoryRepository{lines: []domain.Record{record}, modems: []modemdomain.Record{modem}}, &topologySource{topology: topology})
			if err != nil {
				t.Fatal(err)
			}
			source := fixedPhoneNumberSource{numbers: map[string]string{lineID: test.ims}}
			if test.failure {
				source.err = errors.New("supervisor unavailable")
			}
			service.UsePhoneNumberSource(source)
			views, err := service.List(t.Context())
			if err != nil || len(views) != 1 || !reflect.DeepEqual(views[0].PhoneNumbers, test.want) {
				t.Fatalf("views=%#v error=%v", views, err)
			}
		})
	}
}

func TestLineCandidatesExplainUnavailableManagedModems(t *testing.T) {
	modemIdentity := strings.Repeat("a", 64)
	simIdentity := strings.Repeat("b", 64)
	capabilities := hardware.Capabilities{SIMAccess: true, HostVoWiFiAuth: true}
	modem := modemdomain.Record{
		ID: "modem_AQEBAQEBAQEBAQEBAQEBAQ", EquipmentIdentityFingerprint: modemIdentity,
		DisplayName: "Main ML307A", Capabilities: capabilities,
	}

	for _, test := range []struct {
		name      string
		readiness string
		mutate    func(*inventory.Topology)
	}{
		{name: "offline", readiness: domain.CandidateModemOffline, mutate: func(topology *inventory.Topology) {
			topology.Devices[0].State = hardware.DeviceUnavailable
		}},
		{name: "SIM absent", readiness: domain.CandidateSIMAbsent, mutate: func(topology *inventory.Topology) {
			topology.SIMSlots[0].Presence, topology.SIMSlots[0].ActiveMediaID = hardware.SlotAbsent, ""
			topology.SIMMedia, topology.SubscriptionProfiles, topology.Lines = nil, nil, nil
		}},
		{name: "SIM identity unavailable", readiness: domain.CandidateSIMUnavailable, mutate: func(topology *inventory.Topology) {
			topology.SIMSlots[0].ActiveMediaID = ""
			topology.SIMMedia, topology.SubscriptionProfiles, topology.Lines = nil, nil, nil
		}},
		{name: "binding conflict", readiness: domain.CandidateBindingConflict, mutate: func(topology *inventory.Topology) {
			duplicate := topology.Devices[0]
			duplicate.ID = "agent-usb-9-9"
			topology.Devices = append(topology.Devices, duplicate)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			topology := readyTopology("agent-usb-1-2", modemIdentity, simIdentity, capabilities)
			test.mutate(&topology)
			service, err := New(&memoryRepository{modems: []modemdomain.Record{modem}}, &topologySource{topology: topology})
			if err != nil {
				t.Fatal(err)
			}
			candidates, err := service.Candidates(t.Context())
			if err != nil || len(candidates) != 1 || candidates[0].Addable || candidates[0].Readiness != test.readiness {
				t.Fatalf("candidates=%#v error=%v", candidates, err)
			}
		})
	}
}
