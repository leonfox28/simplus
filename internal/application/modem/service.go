package modem

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/leonfox28/simplus/internal/application/inventory"
	"github.com/leonfox28/simplus/internal/domain/hardware"
	domain "github.com/leonfox28/simplus/internal/domain/modem"
)

var (
	ErrCandidateNotFound            = errors.New("modem candidate was not found")
	ErrCandidateNotReady            = errors.New("modem candidate is not ready to add")
	ErrAlreadyManaged               = errors.New("modem candidate is already managed")
	ErrCandidateInvalid             = errors.New("modem candidate id is invalid")
	ErrModemNotFound                = errors.New("managed modem was not found")
	ErrRFUnavailable                = errors.New("managed modem RF control is unavailable")
	ErrIdentityConflict             = errors.New("modem equipment identity is not unique")
	ErrEquipmentIdentityUnavailable = errors.New("managed modem equipment identity is unavailable")
)

var (
	candidateIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	imeiPattern        = regexp.MustCompile(`^[0-9]{15}$`)
	fingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Repository interface {
	ListManagedModems(context.Context) ([]domain.Record, error)
	CreateManagedModem(context.Context, domain.Record) error
	BindManagedModemIdentity(context.Context, string, string, string, time.Time) error
}

type Inventory interface {
	Topology(context.Context) (inventory.Topology, error)
}

type RFController interface {
	State(context.Context, string) (string, error)
	Set(context.Context, string, bool) (string, error)
}

type RuntimeStatusReader interface {
	Read(context.Context, string) (domain.RuntimeStatus, error)
}

type RFSetter interface {
	Set(context.Context, string, bool) (string, error)
}

type EquipmentIdentity struct {
	IMEI        string
	Fingerprint string
}

type EquipmentIdentityReader interface {
	Read(context.Context, string) (EquipmentIdentity, error)
}

type Service struct {
	repository Repository
	inventory  Inventory
	random     io.Reader
	now        func() time.Time
	rf         RFSetter
	runtime    RuntimeStatusReader
	legacyRF   RFController
	identity   EquipmentIdentityReader

	mu sync.Mutex
}

func (service *Service) UseRFController(controller RFController) {
	if service != nil {
		service.rf = controller
		service.legacyRF = controller
		service.runtime, _ = controller.(RuntimeStatusReader)
	}
}

func (service *Service) UseRuntimeStatusReader(reader RuntimeStatusReader) {
	if service != nil {
		service.runtime = reader
	}
}
func (service *Service) UseRFSetter(setter RFSetter) {
	if service != nil {
		service.rf = setter
	}
}

func (service *Service) UseEquipmentIdentityReader(reader EquipmentIdentityReader) {
	if service != nil {
		service.identity = reader
	}
}

func New(repository Repository, inventoryService Inventory) (*Service, error) {
	if repository == nil || inventoryService == nil {
		return nil, errors.New("managed modem service is not configured")
	}
	return &Service{repository: repository, inventory: inventoryService, random: rand.Reader, now: time.Now}, nil
}

func (service *Service) List(ctx context.Context) ([]domain.View, error) {
	records, err := service.repository.ListManagedModems(ctx)
	if err != nil {
		return nil, fmt.Errorf("list managed modems: %w", err)
	}
	topology, topologyErr := service.inventory.Topology(ctx)
	observed := map[string]observation{}
	if topologyErr == nil {
		observed = observations(topology)
		records, err = service.promoteLegacyBindings(ctx, records, observed)
		if err != nil {
			return nil, err
		}
	}
	byEquipment := observationsByEquipment(observed)
	views := make([]domain.View, 0, len(records))
	for _, record := range records {
		view := domain.View{
			ID: record.ID, DisplayName: record.DisplayName, Model: "",
			Transport: record.Transport, State: domain.StateOffline,
			Capabilities: record.Capabilities, RFState: domain.RFStateUnknown,
			SIMPresence: domain.SIMPresenceUnknown, Cellular: domain.UnavailableCellularStatus(), AddedAt: record.CreatedAt,
		}
		if current, ok := managedObservation(record, observed, byEquipment); ok {
			view.State = domain.StateOnline
			view.Model = current.model
			view.SerialNumber = current.serialNumber
			view.Transport = current.transport
			view.Capabilities = current.capabilities
			view.SIMPresence = current.simPresence
			if service.runtime != nil {
				if status, statusErr := service.runtime.Read(ctx, current.id); statusErr == nil {
					view.RFState = status.RFState
					view.SIMPresence = status.SIMPresence
					view.Cellular = status.Cellular
				}
			} else if service.legacyRF != nil && view.Capabilities.RFControl {
				if state, stateErr := service.legacyRF.State(ctx, current.id); stateErr == nil {
					view.RFState = state
				}
			}
		}
		views = append(views, view)
	}
	sort.Slice(views, func(left, right int) bool {
		if views[left].AddedAt.Equal(views[right].AddedAt) {
			return views[left].ID < views[right].ID
		}
		return views[left].AddedAt.Before(views[right].AddedAt)
	})
	return views, nil
}

func (service *Service) Candidates(ctx context.Context) ([]domain.Candidate, error) {
	records, err := service.repository.ListManagedModems(ctx)
	if err != nil {
		return nil, fmt.Errorf("list managed modems before scan: %w", err)
	}
	topology, err := service.inventory.Topology(ctx)
	if err != nil {
		return nil, fmt.Errorf("scan modem candidates: %w", err)
	}
	observed := observations(topology)
	records, err = service.promoteLegacyBindings(ctx, records, observed)
	if err != nil {
		return nil, err
	}
	managedIdentities := make(map[string]struct{}, len(records))
	managedLegacyIDs := make(map[string]struct{}, len(records))
	for _, record := range records {
		if record.EquipmentIdentityFingerprint != "" {
			managedIdentities[record.EquipmentIdentityFingerprint] = struct{}{}
		}
		if record.LegacyHardwareDeviceID != "" {
			managedLegacyIDs[record.LegacyHardwareDeviceID] = struct{}{}
		}
	}
	byEquipment := observationsByEquipment(observed)
	candidates := make([]domain.Candidate, 0, len(observed))
	for id, current := range observed {
		if _, exists := managedLegacyIDs[id]; exists {
			continue
		}
		if _, exists := managedIdentities[current.equipmentIdentity]; current.equipmentIdentity != "" && exists {
			continue
		}
		readiness := candidateReadiness(current, byEquipment)
		addable := readiness == domain.ReadinessReady
		support := domain.SupportNotReady
		if addable {
			support = domain.SupportSupported
		}
		candidates = append(candidates, domain.Candidate{
			CandidateID: id, USBAddress: current.usbAddress,
			USBVendorID: current.usbVendorID, USBProductID: current.usbProductID,
			USBSerialHint: shortUSBSerialHint(current.usbSerialIdentity),
			Model:         current.model, Transport: current.transport,
			Support: support, Addable: addable, Readiness: readiness, Capabilities: current.capabilities,
			SIMPresence: current.simPresence,
		})
	}
	sort.Slice(candidates, func(left, right int) bool {
		if candidates[left].USBAddress != candidates[right].USBAddress {
			return candidates[left].USBAddress < candidates[right].USBAddress
		}
		if candidates[left].Model != candidates[right].Model {
			return candidates[left].Model < candidates[right].Model
		}
		return candidates[left].CandidateID < candidates[right].CandidateID
	})
	return candidates, nil
}

func (service *Service) Add(ctx context.Context, candidateID string) (domain.View, error) {
	if !candidateIDPattern.MatchString(candidateID) {
		return domain.View{}, ErrCandidateInvalid
	}
	service.mu.Lock()
	defer service.mu.Unlock()

	records, err := service.repository.ListManagedModems(ctx)
	if err != nil {
		return domain.View{}, fmt.Errorf("list managed modems before add: %w", err)
	}
	topology, err := service.inventory.Topology(ctx)
	if err != nil {
		return domain.View{}, fmt.Errorf("read candidate before add: %w", err)
	}
	observed := observations(topology)
	records, err = service.promoteLegacyBindings(ctx, records, observed)
	if err != nil {
		return domain.View{}, err
	}
	current, exists := observed[candidateID]
	if !exists {
		return domain.View{}, ErrCandidateNotFound
	}
	readiness := candidateReadiness(current, observationsByEquipment(observed))
	if readiness == domain.ReadinessIdentityConflict {
		return domain.View{}, ErrIdentityConflict
	}
	if readiness != domain.ReadinessReady {
		return domain.View{}, ErrCandidateNotReady
	}
	for _, record := range records {
		if record.EquipmentIdentityFingerprint == current.equipmentIdentity || record.LegacyHardwareDeviceID == candidateID {
			return domain.View{}, ErrAlreadyManaged
		}
	}
	id, err := service.newID()
	if err != nil {
		return domain.View{}, fmt.Errorf("create managed modem id: %w", err)
	}
	now := service.now().UTC()
	persistedModel := current.model
	if persistedModel == "" {
		persistedModel = current.displayName
	}
	record := domain.Record{
		ID: id, EquipmentIdentityFingerprint: current.equipmentIdentity, USBSerialFingerprint: current.usbSerialIdentity,
		DisplayName: current.displayName, Model: persistedModel,
		Transport: current.transport, Capabilities: current.capabilities, CreatedAt: now, UpdatedAt: now,
	}
	if err := service.repository.CreateManagedModem(ctx, record); err != nil {
		return domain.View{}, fmt.Errorf("persist managed modem: %w", err)
	}
	return domain.View{
		ID: id, DisplayName: record.DisplayName, Model: current.model, SerialNumber: current.serialNumber, Transport: record.Transport,
		State: domain.StateOnline, Capabilities: record.Capabilities, RFState: domain.RFStateUnknown,
		SIMPresence: current.simPresence, Cellular: domain.UnavailableCellularStatus(), AddedAt: now,
	}, nil
}

func (service *Service) SetRFState(ctx context.Context, modemID string, enabled bool) (domain.View, error) {
	if service == nil || service.rf == nil {
		return domain.View{}, ErrRFUnavailable
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	records, err := service.repository.ListManagedModems(ctx)
	if err != nil {
		return domain.View{}, fmt.Errorf("list managed modems before RF change: %w", err)
	}
	found := false
	for _, record := range records {
		if record.ID == modemID {
			found = true
			break
		}
	}
	if !found {
		return domain.View{}, ErrModemNotFound
	}
	topology, err := service.inventory.Topology(ctx)
	if err != nil {
		return domain.View{}, fmt.Errorf("read managed modem before RF change: %w", err)
	}
	observed := observations(topology)
	records, err = service.promoteLegacyBindings(ctx, records, observed)
	if err != nil {
		return domain.View{}, err
	}
	var selected *domain.Record
	for index := range records {
		if records[index].ID == modemID {
			selected = &records[index]
			break
		}
	}
	current, online := managedObservation(*selected, observed, observationsByEquipment(observed))
	if !online || !current.capabilities.RFControl {
		return domain.View{}, ErrRFUnavailable
	}
	state, err := service.rf.Set(ctx, current.id, enabled)
	if err != nil {
		return domain.View{}, fmt.Errorf("set managed modem RF state: %w", err)
	}
	return domain.View{
		ID: selected.ID, DisplayName: selected.DisplayName, Model: current.model, SerialNumber: current.serialNumber,
		Transport: current.transport, State: domain.StateOnline, Capabilities: current.capabilities,
		RFState: state, SIMPresence: current.simPresence, Cellular: domain.UnavailableCellularStatus(), AddedAt: selected.CreatedAt,
	}, nil
}

func (service *Service) ReadEquipmentIdentity(ctx context.Context, modemID string) (string, error) {
	if service == nil || service.identity == nil {
		return "", ErrEquipmentIdentityUnavailable
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	records, err := service.repository.ListManagedModems(ctx)
	if err != nil {
		return "", fmt.Errorf("list managed modems before identity read: %w", err)
	}
	var selected *domain.Record
	for index := range records {
		if records[index].ID == modemID {
			selected = &records[index]
			break
		}
	}
	if selected == nil {
		return "", ErrModemNotFound
	}
	topology, err := service.inventory.Topology(ctx)
	if err != nil {
		return "", fmt.Errorf("read managed modem before identity read: %w", err)
	}
	observed := observations(topology)
	records, err = service.promoteLegacyBindings(ctx, records, observed)
	if err != nil {
		return "", err
	}
	for index := range records {
		if records[index].ID == modemID {
			selected = &records[index]
			break
		}
	}
	current, online := managedObservation(*selected, observed, observationsByEquipment(observed))
	if !online {
		return "", ErrEquipmentIdentityUnavailable
	}
	identity, err := service.identity.Read(ctx, current.id)
	if err != nil {
		return "", fmt.Errorf("read managed modem equipment identity: %w", err)
	}
	if !imeiPattern.MatchString(identity.IMEI) || !fingerprintPattern.MatchString(identity.Fingerprint) {
		return "", ErrEquipmentIdentityUnavailable
	}
	if identity.Fingerprint != selected.EquipmentIdentityFingerprint {
		return "", ErrIdentityConflict
	}
	return identity.IMEI, nil
}

func (service *Service) newID() (string, error) {
	value := make([]byte, 16)
	if _, err := io.ReadFull(service.random, value); err != nil {
		return "", err
	}
	return "modem_" + base64.RawURLEncoding.EncodeToString(value), nil
}

type observation struct {
	id                string
	displayName       string
	usbAddress        string
	usbVendorID       string
	usbProductID      string
	model             string
	serialNumber      string
	transport         string
	equipmentIdentity string
	usbSerialIdentity string
	hasFunction       bool
	capabilities      hardware.Capabilities
	simPresence       string
}

func observations(topology inventory.Topology) map[string]observation {
	result := make(map[string]observation, len(topology.Devices))
	for _, device := range topology.Devices {
		if device.State != hardware.DeviceAvailable {
			continue
		}
		result[device.ID] = observation{
			id: device.ID, displayName: device.DisplayName, usbAddress: device.USBAddress,
			usbVendorID: device.USBVendorID, usbProductID: device.USBProductID,
			model: device.ModemModel, serialNumber: device.ObservedSerialNumber(), transport: device.Transport,
			equipmentIdentity: device.EquipmentIdentityFingerprint, usbSerialIdentity: device.USBSerialFingerprint,
			simPresence: domain.SIMPresenceUnknown,
		}
	}
	for _, function := range topology.ModemFunctions {
		current, exists := result[function.PhysicalDeviceID]
		if !exists {
			continue
		}
		current.hasFunction = true
		current.capabilities = mergeCapabilities(current.capabilities, function.Capabilities)
		result[function.PhysicalDeviceID] = current
	}
	for _, slot := range topology.SIMSlots {
		if slot.Index != 0 {
			continue
		}
		current, exists := result[slot.PhysicalDeviceID]
		if !exists {
			continue
		}
		switch slot.Presence {
		case hardware.SlotPresent:
			current.simPresence = domain.SIMPresencePresent
		case hardware.SlotAbsent:
			current.simPresence = domain.SIMPresenceAbsent
		default:
			current.simPresence = domain.SIMPresenceUnknown
		}
		result[slot.PhysicalDeviceID] = current
	}
	return result
}

func shortUSBSerialHint(fingerprint string) string {
	if len(fingerprint) != 64 {
		return ""
	}
	return "USB •••• " + strings.ToUpper(fingerprint[len(fingerprint)-8:])
}

func observationsByEquipment(observed map[string]observation) map[string][]observation {
	result := make(map[string][]observation, len(observed))
	for _, current := range observed {
		if current.equipmentIdentity != "" {
			result[current.equipmentIdentity] = append(result[current.equipmentIdentity], current)
		}
	}
	return result
}

func candidateReadiness(current observation, byEquipment map[string][]observation) string {
	switch {
	case !current.hasFunction:
		return domain.ReadinessControlUnavailable
	case !current.capabilities.SIMAccess:
		return domain.ReadinessSIMAccessUnavailable
	case current.equipmentIdentity == "":
		return domain.ReadinessEquipmentIdentityUnavailable
	case len(byEquipment[current.equipmentIdentity]) != 1:
		return domain.ReadinessIdentityConflict
	default:
		return domain.ReadinessReady
	}
}

// ResolveManagedModemDevices maps stable ManagedModem IDs to the current
// physical observations. Duplicate equipment identities deliberately produce
// no result. Consumers must not reproduce the IMEI/topology matching rules.
func ResolveManagedModemDevices(records []domain.Record, topology inventory.Topology) map[string]string {
	observed := observations(topology)
	byEquipment := observationsByEquipment(observed)
	result := make(map[string]string, len(records))
	for _, record := range records {
		if current, ok := managedObservation(record, observed, byEquipment); ok {
			result[record.ID] = current.id
		}
	}
	return result
}

func managedObservation(record domain.Record, byID map[string]observation, byEquipment map[string][]observation) (observation, bool) {
	if record.EquipmentIdentityFingerprint != "" {
		matches := byEquipment[record.EquipmentIdentityFingerprint]
		if len(matches) == 1 {
			return matches[0], true
		}
		return observation{}, false
	}
	if record.LegacyHardwareDeviceID != "" {
		current, ok := byID[record.LegacyHardwareDeviceID]
		return current, ok
	}
	return observation{}, false
}

func (service *Service) promoteLegacyBindings(ctx context.Context, records []domain.Record, observed map[string]observation) ([]domain.Record, error) {
	byEquipment := observationsByEquipment(observed)
	claimed := make(map[string]struct{}, len(records))
	for _, record := range records {
		if record.EquipmentIdentityFingerprint != "" {
			claimed[record.EquipmentIdentityFingerprint] = struct{}{}
		}
	}
	for index := range records {
		record := &records[index]
		if record.EquipmentIdentityFingerprint != "" || record.LegacyHardwareDeviceID == "" {
			continue
		}
		current, exists := observed[record.LegacyHardwareDeviceID]
		if !exists || current.equipmentIdentity == "" || len(byEquipment[current.equipmentIdentity]) != 1 {
			continue
		}
		if _, duplicate := claimed[current.equipmentIdentity]; duplicate {
			continue
		}
		now := service.now().UTC()
		if err := service.repository.BindManagedModemIdentity(ctx, record.ID, current.equipmentIdentity, current.usbSerialIdentity, now); err != nil {
			return nil, fmt.Errorf("promote managed modem equipment identity: %w", err)
		}
		record.EquipmentIdentityFingerprint = current.equipmentIdentity
		record.USBSerialFingerprint = current.usbSerialIdentity
		record.LegacyHardwareDeviceID = ""
		record.UpdatedAt = now
		claimed[current.equipmentIdentity] = struct{}{}
	}
	return records, nil
}

func mergeCapabilities(left, right hardware.Capabilities) hardware.Capabilities {
	return hardware.Capabilities{
		SIMAccess: left.SIMAccess || right.SIMAccess, SMS: left.SMS || right.SMS,
		CellularVoice:     left.CellularVoice || right.CellularVoice,
		DigitalVoiceMedia: left.DigitalVoiceMedia || right.DigitalVoiceMedia,
		USBUAC:            left.USBUAC || right.USBUAC, SIMAPDU: left.SIMAPDU || right.SIMAPDU,
		HostVoWiFiAuth: left.HostVoWiFiAuth || right.HostVoWiFiAuth,
		RFControl:      left.RFControl || right.RFControl, NetworkScan: left.NetworkScan || right.NetworkScan,
		ManualNetworkSelection: left.ManualNetworkSelection || right.ManualNetworkSelection,
		PrimarySIMLockState:    left.PrimarySIMLockState || right.PrimarySIMLockState,
		PIN1Verify:             left.PIN1Verify || right.PIN1Verify, PUK1Unblock: left.PUK1Unblock || right.PUK1Unblock,
		EUICCProfiles: left.EUICCProfiles || right.EUICCProfiles,
	}
}
