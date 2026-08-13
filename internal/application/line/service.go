package line

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/leonfox28/simplus/internal/application/inventory"
	modemapp "github.com/leonfox28/simplus/internal/application/modem"
	"github.com/leonfox28/simplus/internal/domain/hardware"
	domain "github.com/leonfox28/simplus/internal/domain/line"
	modemdomain "github.com/leonfox28/simplus/internal/domain/modem"
)

var (
	ErrCandidateNotFound = errors.New("line candidate was not found")
	ErrCandidateInvalid  = errors.New("line candidate is invalid")
	ErrAlreadyManaged    = errors.New("line candidate is already managed")
	ErrRequestInvalid    = errors.New("managed line request is invalid")
)

var (
	lineIDPattern      = regexp.MustCompile(`^line_[A-Za-z0-9_-]{22}$`)
	candidateIDPattern = regexp.MustCompile(`^line-candidate-[0-9a-f]{32}$`)
	fingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Repository interface {
	ListManagedLines(context.Context) ([]domain.Record, error)
	CreateManagedLine(context.Context, domain.Record) error
	UpdateManagedLine(context.Context, string, string, time.Time) error
	ListManagedModems(context.Context) ([]modemdomain.Record, error)
}

type Inventory interface {
	Topology(context.Context) (inventory.Topology, error)
}

type Service struct {
	repository Repository
	inventory  Inventory
	random     io.Reader
	now        func() time.Time

	mu sync.Mutex
}

func New(repository Repository, inventoryService Inventory) (*Service, error) {
	if repository == nil || inventoryService == nil {
		return nil, errors.New("managed line service is not configured")
	}
	return &Service{repository: repository, inventory: inventoryService, random: rand.Reader, now: time.Now}, nil
}

func (service *Service) List(ctx context.Context) ([]domain.View, error) {
	records, modems, topology, err := service.load(ctx)
	if err != nil {
		return nil, err
	}
	return views(records, modems, topology), nil
}

func (service *Service) Candidates(ctx context.Context) ([]domain.Candidate, error) {
	records, modems, topology, err := service.load(ctx)
	if err != nil {
		return nil, err
	}
	observed := candidateObservations(records, modems, topology)
	result := make([]domain.Candidate, 0, len(observed))
	for _, candidate := range observed {
		result = append(result, candidate.Candidate)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].ManagedModemDisplayName == result[right].ManagedModemDisplayName {
			return result[left].SubscriptionDisplayHint < result[right].SubscriptionDisplayHint
		}
		return result[left].ManagedModemDisplayName < result[right].ManagedModemDisplayName
	})
	return result, nil
}

func (service *Service) Add(ctx context.Context, candidateID, displayName string) (domain.View, error) {
	if !candidateIDPattern.MatchString(candidateID) || !validDisplayName(displayName) {
		return domain.View{}, ErrRequestInvalid
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	records, modems, topology, err := service.load(ctx)
	if err != nil {
		return domain.View{}, err
	}
	observed := candidateObservations(records, modems, topology)
	candidate, exists := observed[candidateID]
	if !exists {
		for _, record := range records {
			if candidateID == candidateIDFor(record.ManagedModemID, record.SubscriptionIdentityFingerprint) {
				return domain.View{}, ErrAlreadyManaged
			}
		}
		return domain.View{}, ErrCandidateNotFound
	}
	if candidate.Readiness == domain.CandidateAlreadyAdded {
		return domain.View{}, ErrAlreadyManaged
	}
	if !candidate.Addable {
		return domain.View{}, ErrCandidateInvalid
	}
	id, err := service.newID()
	if err != nil {
		return domain.View{}, fmt.Errorf("create managed line id: %w", err)
	}
	now := service.now().UTC()
	record := domain.Record{
		ID: id, ManagedModemID: candidate.ManagedModemID, SIMSlotIndex: candidate.slotIndex,
		SubscriptionIdentityFingerprint: candidate.identityFingerprint,
		SubscriptionDisplayHint:         candidate.SubscriptionDisplayHint,
		DisplayName:                     strings.TrimSpace(displayName), CreatedAt: now, UpdatedAt: now,
	}
	if err := service.repository.CreateManagedLine(ctx, record); err != nil {
		return domain.View{}, fmt.Errorf("persist managed line: %w", err)
	}
	return viewFor(record, modems, topology), nil
}

func (service *Service) Update(ctx context.Context, lineID, displayName string) (domain.View, error) {
	if !lineIDPattern.MatchString(lineID) || !validDisplayName(displayName) {
		return domain.View{}, ErrRequestInvalid
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	records, modems, topology, err := service.load(ctx)
	if err != nil {
		return domain.View{}, err
	}
	var selected *domain.Record
	for index := range records {
		if records[index].ID == lineID {
			selected = &records[index]
			break
		}
	}
	if selected == nil {
		return domain.View{}, domain.ErrNotFound
	}
	selected.DisplayName = strings.TrimSpace(displayName)
	selected.UpdatedAt = service.now().UTC()
	if err := service.repository.UpdateManagedLine(ctx, selected.ID, selected.DisplayName, selected.UpdatedAt); err != nil {
		return domain.View{}, err
	}
	return viewFor(*selected, modems, topology), nil
}

// Topology implements the business Line catalog. Hardware inventory remains
// the observation source, while only administrator-created Lines are exposed
// to SMS, calls, egress and Host VoWiFi consumers.
func (service *Service) Topology(ctx context.Context) (inventory.Topology, error) {
	records, modems, topology, err := service.load(ctx)
	if err != nil {
		return inventory.Topology{}, err
	}
	topology.Lines = resolvedLines(records, modems, topology)
	revision, err := inventory.Revision(topology)
	if err != nil {
		return inventory.Topology{}, fmt.Errorf("digest managed line topology: %w", err)
	}
	topology.Revision = revision
	return topology, nil
}

func (service *Service) load(ctx context.Context) ([]domain.Record, []modemdomain.Record, inventory.Topology, error) {
	records, err := service.repository.ListManagedLines(ctx)
	if err != nil {
		return nil, nil, inventory.Topology{}, fmt.Errorf("list managed lines: %w", err)
	}
	modems, err := service.repository.ListManagedModems(ctx)
	if err != nil {
		return nil, nil, inventory.Topology{}, fmt.Errorf("list managed modems for lines: %w", err)
	}
	topology, err := service.inventory.Topology(ctx)
	if err != nil {
		return nil, nil, inventory.Topology{}, fmt.Errorf("read hardware topology for lines: %w", err)
	}
	return records, modems, topology, nil
}

type candidateObservation struct {
	domain.Candidate
	identityFingerprint string
	slotIndex           int
}

func candidateObservations(records []domain.Record, modems []modemdomain.Record, topology inventory.Topology) map[string]candidateObservation {
	managed := make(map[string]struct{}, len(records))
	for _, record := range records {
		managed[record.ManagedModemID+"\x00"+record.SubscriptionIdentityFingerprint] = struct{}{}
	}
	devices := make(map[string]hardware.PhysicalDevice, len(topology.Devices))
	for _, device := range topology.Devices {
		devices[device.ID] = device
	}
	physicalByModem := modemapp.ResolveManagedModemDevices(modems, topology)
	profiles := profileObservations(topology)
	result := make(map[string]candidateObservation)
	for _, modem := range modems {
		physicalID := physicalByModem[modem.ID]
		if physicalID == "" {
			readiness := domain.CandidateModemOffline
			if managedModemIdentityConflict(modem, topology) {
				readiness = domain.CandidateBindingConflict
			}
			candidate := unavailableCandidate(modem, 0, hardware.SlotUnknown, readiness)
			result[candidate.CandidateID] = candidate
			continue
		}
		device := devices[physicalID]
		observed := make([]struct {
			line    inventory.Line
			profile profileObservation
		}, 0)
		counts := make(map[string]int)
		for _, line := range topology.Lines {
			profile, ok := profiles[line.SubscriptionProfileID]
			if line.PhysicalDeviceID != physicalID || !ok || !profile.active || !fingerprintPattern.MatchString(profile.identityFingerprint) {
				continue
			}
			observed = append(observed, struct {
				line    inventory.Line
				profile profileObservation
			}{line: line, profile: profile})
			counts[profile.identityFingerprint]++
		}
		if len(observed) == 0 {
			presence, slotIndex := simPresenceForDevice(physicalID, topology)
			readiness := domain.CandidateSIMUnavailable
			if presence == hardware.SlotAbsent {
				readiness = domain.CandidateSIMAbsent
			}
			candidate := unavailableCandidate(modem, slotIndex, presence, readiness)
			candidate.ManagedModemModel = device.ModemModel
			candidate.ManagedModemSerialNumber = device.ObservedSerialNumber()
			result[candidate.CandidateID] = candidate
			continue
		}
		for _, item := range observed {
			candidateID := candidateIDFor(modem.ID, item.profile.identityFingerprint)
			readiness := domain.CandidateReady
			if counts[item.profile.identityFingerprint] != 1 {
				readiness = domain.CandidateBindingConflict
			} else if _, exists := managed[modem.ID+"\x00"+item.profile.identityFingerprint]; exists {
				readiness = domain.CandidateAlreadyAdded
			} else if !item.line.Capabilities.SIMAccess {
				readiness = domain.CandidateSIMUnavailable
			}
			result[candidateID] = candidateObservation{
				Candidate: domain.Candidate{
					CandidateID: candidateID, ManagedModemID: modem.ID, ManagedModemDisplayName: modem.DisplayName,
					ManagedModemModel: device.ModemModel, ManagedModemSerialNumber: device.ObservedSerialNumber(),
					SubscriptionDisplayHint: item.profile.displayHint,
					HomeOperatorName:        item.profile.homeOperatorName, HomeOperatorCode: item.profile.homeOperatorCode,
					SIMPresence:  hardware.SlotPresent,
					Capabilities: item.line.Capabilities, Addable: readiness == domain.CandidateReady, Readiness: readiness,
				},
				identityFingerprint: item.profile.identityFingerprint, slotIndex: item.profile.slotIndex,
			}
		}
	}
	return result
}

func unavailableCandidate(modem modemdomain.Record, slotIndex int, presence, readiness string) candidateObservation {
	candidateID := candidateStatusIDFor(modem.ID, slotIndex, readiness)
	return candidateObservation{Candidate: domain.Candidate{
		CandidateID: candidateID, ManagedModemID: modem.ID, ManagedModemDisplayName: modem.DisplayName,
		SIMPresence: presence, Capabilities: modem.Capabilities, Addable: false, Readiness: readiness,
	}, slotIndex: slotIndex}
}

func managedModemIdentityConflict(modem modemdomain.Record, topology inventory.Topology) bool {
	if !fingerprintPattern.MatchString(modem.EquipmentIdentityFingerprint) {
		return false
	}
	matches := 0
	for _, device := range topology.Devices {
		if device.State == hardware.DeviceAvailable && device.EquipmentIdentityFingerprint == modem.EquipmentIdentityFingerprint {
			matches++
		}
	}
	return matches > 1
}

func simPresenceForDevice(physicalDeviceID string, topology inventory.Topology) (string, int) {
	presence, slotIndex := hardware.SlotUnknown, 0
	found := false
	for _, slot := range topology.SIMSlots {
		if slot.PhysicalDeviceID != physicalDeviceID || found && slot.Index >= slotIndex {
			continue
		}
		presence, slotIndex, found = slot.Presence, slot.Index, true
	}
	return presence, slotIndex
}

type profileObservation struct {
	identityFingerprint string
	displayHint         string
	homeOperatorName    string
	homeOperatorCode    string
	slotIndex           int
	active              bool
}

func profileObservations(topology inventory.Topology) map[string]profileObservation {
	mediaByID := make(map[string]hardware.SIMMedia, len(topology.SIMMedia))
	for _, media := range topology.SIMMedia {
		mediaByID[media.ID] = media
	}
	slotsByID := make(map[string]hardware.SIMSlot, len(topology.SIMSlots))
	for _, slot := range topology.SIMSlots {
		slotsByID[slot.ID] = slot
	}
	result := make(map[string]profileObservation, len(topology.SubscriptionProfiles))
	for _, profile := range topology.SubscriptionProfiles {
		media, mediaOK := mediaByID[profile.SIMMediaID]
		slot, slotOK := slotsByID[media.SIMSlotID]
		if !mediaOK || !slotOK {
			continue
		}
		result[profile.ID] = profileObservation{
			identityFingerprint: profile.IdentityFingerprint, displayHint: profile.DisplayIdentityHint,
			homeOperatorName: profile.HomeOperatorName, homeOperatorCode: profile.HomeOperatorCode,
			slotIndex: slot.Index, active: profile.State == hardware.ProfileActive,
		}
	}
	return result
}

func views(records []domain.Record, modems []modemdomain.Record, topology inventory.Topology) []domain.View {
	result := make([]domain.View, 0, len(records))
	for _, record := range records {
		result = append(result, viewFor(record, modems, topology))
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].CreatedAt.Equal(result[right].CreatedAt) {
			return result[left].ID < result[right].ID
		}
		return result[left].CreatedAt.Before(result[right].CreatedAt)
	})
	return result
}

func viewFor(record domain.Record, modems []modemdomain.Record, topology inventory.Topology) domain.View {
	modemByID := make(map[string]modemdomain.Record, len(modems))
	for _, modem := range modems {
		modemByID[modem.ID] = modem
	}
	state := domain.StateModemOffline
	capabilities := modemByID[record.ManagedModemID].Capabilities
	physicalByModem := modemapp.ResolveManagedModemDevices(modems, topology)
	physicalID := physicalByModem[record.ManagedModemID]
	model, serialNumber := "", ""
	if physicalID != "" {
		state = domain.StateSIMUnavailable
		for _, device := range topology.Devices {
			if device.ID == physicalID {
				model, serialNumber = device.ModemModel, device.ObservedSerialNumber()
				break
			}
		}
	}
	if resolved, ok := resolveLine(record, physicalID, topology); ok {
		state, capabilities = domain.StateReady, resolved.Capabilities
	}
	return domain.View{
		ID: record.ID, DisplayName: record.DisplayName, ManagedModemID: record.ManagedModemID,
		ManagedModemDisplayName: modemByID[record.ManagedModemID].DisplayName,
		ManagedModemModel:       model, ManagedModemSerialNumber: serialNumber,
		SubscriptionDisplayHint: record.SubscriptionDisplayHint,
		State:                   state, Capabilities: capabilities, CreatedAt: record.CreatedAt,
	}
}

func resolvedLines(records []domain.Record, modems []modemdomain.Record, topology inventory.Topology) []inventory.Line {
	physicalByModem := modemapp.ResolveManagedModemDevices(modems, topology)
	modemByID := make(map[string]modemdomain.Record, len(modems))
	for _, modem := range modems {
		modemByID[modem.ID] = modem
	}
	result := make([]inventory.Line, 0, len(records))
	for _, record := range records {
		line := inventory.Line{
			ID: record.ID, ManagedModemID: record.ManagedModemID, DisplayName: record.DisplayName,
			State: inventory.LineUnavailable, Capabilities: modemByID[record.ManagedModemID].Capabilities,
		}
		if current, ok := resolveLine(record, physicalByModem[record.ManagedModemID], topology); ok {
			line.RuntimeLineID = current.ID
			line.PhysicalDeviceID = current.PhysicalDeviceID
			line.ModemFunctionID = current.ModemFunctionID
			line.SubscriptionProfileID = current.SubscriptionProfileID
			line.ResourceGroupID = current.ResourceGroupID
			line.Generation = current.Generation
			line.Capabilities = current.Capabilities
			line.State = inventory.LineReady
		}
		result = append(result, line)
	}
	return result
}

func resolveLine(record domain.Record, physicalDeviceID string, topology inventory.Topology) (inventory.Line, bool) {
	if physicalDeviceID == "" {
		return inventory.Line{}, false
	}
	profiles := profileObservations(topology)
	matches := []inventory.Line{}
	for _, line := range topology.Lines {
		profile, exists := profiles[line.SubscriptionProfileID]
		if line.PhysicalDeviceID == physicalDeviceID && line.State != inventory.LineUnavailable && exists && profile.slotIndex == record.SIMSlotIndex &&
			profile.identityFingerprint == record.SubscriptionIdentityFingerprint && profile.active {
			matches = append(matches, line)
		}
	}
	if len(matches) != 1 {
		return inventory.Line{}, false
	}
	return matches[0], true
}

func candidateIDFor(modemID, fingerprint string) string {
	if !fingerprintPattern.MatchString(fingerprint) || modemID == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(modemID + "\x00" + fingerprint))
	return "line-candidate-" + hex.EncodeToString(digest[:16])
}

func candidateStatusIDFor(modemID string, slotIndex int, readiness string) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%s", modemID, slotIndex, readiness)))
	return "line-candidate-" + hex.EncodeToString(digest[:16])
}

func validDisplayName(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 120 || len(value) > 480 {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func (service *Service) newID() (string, error) {
	value := make([]byte, 16)
	if _, err := io.ReadFull(service.random, value); err != nil {
		return "", err
	}
	return "line_" + base64.RawURLEncoding.EncodeToString(value), nil
}
