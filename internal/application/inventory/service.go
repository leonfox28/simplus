package inventory

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/leonfox28/simplus/internal/domain/accessmode"
	"github.com/leonfox28/simplus/internal/domain/hardware"
)

const (
	LineAwaitingAccessMode = "awaiting-access-mode"
	LineReady              = "ready"
	LineUnavailable        = "unavailable"

	RFSafetyOff = "off"
)

var (
	ErrSubscriptionProfileNotFound = errors.New("subscription profile not found")
	ErrInvalidAccessMode           = errors.New("invalid access mode")
)

type AccessModeRepository interface {
	SubscriptionProfileAccessModes(context.Context, []string) (map[string]accessmode.Mode, error)
	PutSubscriptionProfileAccessMode(context.Context, string, accessmode.Mode) error
}

type HardwareSource interface {
	Snapshot(context.Context) (hardware.Snapshot, error)
}

type PhysicalDevice struct {
	ID                 string
	DisplayName        string
	Transport          string
	State              string
	Generation         uint64
	ModemFunctionCount int
	SIMSlotCount       int
	ResourceGroupCount int
}

type SubscriptionProfile struct {
	hardware.SubscriptionProfile
	AccessMode           accessmode.Mode
	AccessModeConfigured bool
}

type Line struct {
	ID                    string
	PhysicalDeviceID      string
	ModemFunctionID       string
	SubscriptionProfileID string
	ResourceGroupID       string
	DisplayName           string
	Generation            uint64
	Capabilities          hardware.Capabilities
	AccessMode            accessmode.Mode
	AccessModeConfigured  bool
	State                 string
	RFSafety              string
}

type Snapshot struct {
	Generation uint64
	Revision   string
	ObservedAt time.Time
	Devices    []PhysicalDevice
	Lines      []Line
}

type Topology struct {
	Generation           uint64
	Revision             string
	ObservedAt           time.Time
	Devices              []hardware.PhysicalDevice
	ModemFunctions       []hardware.ModemFunction
	SIMSlots             []hardware.SIMSlot
	SIMMedia             []hardware.SIMMedia
	SubscriptionProfiles []SubscriptionProfile
	ResourceGroups       []hardware.ResourceGroup
	Lines                []Line
}

type Service struct {
	accessModes AccessModeRepository
	hardware    HardwareSource
}

func New(source HardwareSource, accessModes AccessModeRepository) *Service {
	return &Service{hardware: source, accessModes: accessModes}
}

func NewSimulator(accessModes AccessModeRepository) *Service {
	return New(simulatorSource{lineCount: 1}, accessModes)
}

// NewMultiSimulator is the V1 interactive Simulator topology. The single-line
// constructor remains available for focused fixtures, while the running
// product exposes two independent modem functions and lines.
func NewMultiSimulator(accessModes AccessModeRepository) *Service {
	return New(simulatorSource{lineCount: 2}, accessModes)
}

func (service *Service) Snapshot(ctx context.Context) (Snapshot, error) {
	topology, err := service.Topology(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	devices := make([]PhysicalDevice, 0, len(topology.Devices))
	for _, device := range topology.Devices {
		summary := PhysicalDevice{
			ID: device.ID, DisplayName: device.DisplayName, Transport: device.Transport, State: device.State, Generation: device.Generation,
		}
		for _, function := range topology.ModemFunctions {
			if function.PhysicalDeviceID == device.ID {
				summary.ModemFunctionCount++
			}
		}
		for _, slot := range topology.SIMSlots {
			if slot.PhysicalDeviceID == device.ID {
				summary.SIMSlotCount++
			}
		}
		for _, group := range topology.ResourceGroups {
			if group.PhysicalDeviceID == device.ID {
				summary.ResourceGroupCount++
			}
		}
		devices = append(devices, summary)
	}
	return Snapshot{
		Generation: topology.Generation,
		Revision:   topology.Revision,
		ObservedAt: topology.ObservedAt,
		Devices:    devices,
		Lines:      append([]Line(nil), topology.Lines...),
	}, nil
}

func (service *Service) Topology(ctx context.Context) (Topology, error) {
	if service == nil || service.accessModes == nil || service.hardware == nil {
		return Topology{}, fmt.Errorf("inventory service is not configured")
	}
	if err := ctx.Err(); err != nil {
		return Topology{}, fmt.Errorf("inventory topology: %w", err)
	}
	raw, err := service.hardware.Snapshot(ctx)
	if err != nil {
		return Topology{}, fmt.Errorf("read hardware topology: %w", err)
	}
	normalized, err := hardware.NormalizeAndValidate(raw)
	if err != nil {
		return Topology{}, fmt.Errorf("validate hardware topology: %w", err)
	}

	topology := Topology{
		Generation: normalized.Generation, ObservedAt: normalized.ObservedAt,
		Devices:        append([]hardware.PhysicalDevice(nil), normalized.Devices...),
		ModemFunctions: append([]hardware.ModemFunction(nil), normalized.ModemFunctions...),
		SIMSlots:       append([]hardware.SIMSlot(nil), normalized.SIMSlots...),
		SIMMedia:       append([]hardware.SIMMedia(nil), normalized.SIMMedia...),
		ResourceGroups: append([]hardware.ResourceGroup(nil), normalized.ResourceGroups...),
	}
	deviceStates := make(map[string]string, len(normalized.Devices))
	for _, device := range normalized.Devices {
		deviceStates[device.ID] = device.State
	}
	profileStates := make(map[string]string, len(normalized.SubscriptionProfiles))
	modes := make(map[string]struct {
		mode       accessmode.Mode
		configured bool
	}, len(normalized.SubscriptionProfiles))
	profileIDs := make([]string, 0, len(normalized.SubscriptionProfiles))
	for _, profile := range normalized.SubscriptionProfiles {
		profileIDs = append(profileIDs, profile.ID)
	}
	storedModes, err := service.accessModes.SubscriptionProfileAccessModes(ctx, profileIDs)
	if err != nil {
		return Topology{}, fmt.Errorf("load subscription profile access modes: %w", err)
	}
	for _, profile := range normalized.SubscriptionProfiles {
		mode, configured := storedModes[profile.ID]
		if !configured {
			mode = accessmode.HoldRFOff
		}
		profileStates[profile.ID] = profile.State
		modes[profile.ID] = struct {
			mode       accessmode.Mode
			configured bool
		}{mode: mode, configured: configured}
		topology.SubscriptionProfiles = append(topology.SubscriptionProfiles, SubscriptionProfile{
			SubscriptionProfile:  profile,
			AccessMode:           mode,
			AccessModeConfigured: configured,
		})
	}
	for _, hardwareLine := range normalized.Lines {
		selection := modes[hardwareLine.SubscriptionProfileID]
		state := LineAwaitingAccessMode
		if selection.configured {
			state = LineReady
		}
		if deviceStates[hardwareLine.PhysicalDeviceID] != hardware.DeviceAvailable || profileStates[hardwareLine.SubscriptionProfileID] == hardware.ProfileLocked {
			state = LineUnavailable
		}
		topology.Lines = append(topology.Lines, Line{
			ID: hardwareLine.ID, PhysicalDeviceID: hardwareLine.PhysicalDeviceID, ModemFunctionID: hardwareLine.ModemFunctionID,
			SubscriptionProfileID: hardwareLine.SubscriptionProfileID, ResourceGroupID: hardwareLine.ResourceGroupID,
			DisplayName: hardwareLine.DisplayName, Generation: hardwareLine.Generation, Capabilities: hardwareLine.Capabilities,
			AccessMode: selection.mode, AccessModeConfigured: selection.configured, State: state, RFSafety: RFSafetyOff,
		})
	}
	revision, err := Revision(topology)
	if err != nil {
		return Topology{}, fmt.Errorf("digest hardware topology: %w", err)
	}
	topology.Revision = revision
	return topology, nil
}

func (service *Service) PutAccessMode(ctx context.Context, profileID string, mode accessmode.Mode) (Snapshot, error) {
	if service == nil || service.accessModes == nil || service.hardware == nil {
		return Snapshot{}, fmt.Errorf("inventory service is not configured")
	}
	if !mode.Valid() {
		return Snapshot{}, fmt.Errorf("%w: %q", ErrInvalidAccessMode, mode)
	}
	topology, err := service.Topology(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	found := false
	for _, profile := range topology.SubscriptionProfiles {
		if profile.ID == profileID {
			found = true
			break
		}
	}
	if !found {
		return Snapshot{}, fmt.Errorf("%w: %s", ErrSubscriptionProfileNotFound, profileID)
	}
	if err := service.accessModes.PutSubscriptionProfileAccessMode(ctx, profileID, mode); err != nil {
		return Snapshot{}, fmt.Errorf("persist access mode for %s: %w", profileID, err)
	}
	return service.Snapshot(ctx)
}

type simulatorSource struct{ lineCount int }

func (simulator simulatorSource) Snapshot(ctx context.Context) (hardware.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return hardware.Snapshot{}, err
	}
	lineCount := simulator.lineCount
	if lineCount < 1 {
		lineCount = 1
	}
	capabilities := hardware.Capabilities{
		SIMAccess: true, SMS: true, CellularVoice: true, DigitalVoiceMedia: true, SIMAPDU: true, HostVoWiFiAuth: true,
		RFControl: true, NetworkScan: true, ManualNetworkSelection: true, PrimarySIMLockState: true,
		PIN1Verify: true, PUK1Unblock: true,
	}
	snapshot := hardware.Snapshot{Generation: 1, ObservedAt: time.Now().UTC()}
	for index := 1; index <= lineCount; index++ {
		suffix := fmt.Sprintf("%d", index)
		deviceID := "simulator-device-" + suffix
		functionID := "simulator-function-" + suffix
		slotID := "simulator-slot-" + suffix
		mediaID := "simulator-media-" + suffix
		profileID := "simulator-profile-" + suffix
		groupID := "simulator-resource-group-" + suffix
		identity := fmt.Sprintf("%064x", index)
		hint := fmt.Sprintf("%04d", 100+index)
		snapshot.Devices = append(snapshot.Devices, hardware.PhysicalDevice{
			ID: deviceID, DisplayName: "Simulator modem " + suffix, Transport: hardware.TransportSimulated,
			State: hardware.DeviceAvailable, Generation: 1,
		})
		snapshot.ModemFunctions = append(snapshot.ModemFunctions, hardware.ModemFunction{
			ID: functionID, PhysicalDeviceID: deviceID, DisplayName: "Simulator cellular function " + suffix,
			Backend: hardware.BackendSimulated, Generation: 1, Capabilities: capabilities,
		})
		snapshot.SIMSlots = append(snapshot.SIMSlots, hardware.SIMSlot{
			ID: slotID, PhysicalDeviceID: deviceID, Index: 0, Presence: hardware.SlotPresent,
			ActiveMediaID: mediaID, Generation: 1,
		})
		snapshot.SIMMedia = append(snapshot.SIMMedia, hardware.SIMMedia{
			ID: mediaID, SIMSlotID: slotID, Kind: hardware.MediaUICC,
			IdentityState: hardware.MediaIdentityKnown, IdentityFingerprint: identity, DisplayIdentityHint: "SIM •••• " + hint, Generation: 1,
		})
		snapshot.SubscriptionProfiles = append(snapshot.SubscriptionProfiles, hardware.SubscriptionProfile{
			ID: profileID, SIMMediaID: mediaID, DisplayName: "Simulator profile " + suffix,
			State: hardware.ProfileActive, IdentityFingerprint: identity, DisplayIdentityHint: "ICCID •••• " + hint, Generation: 1,
		})
		snapshot.ResourceGroups = append(snapshot.ResourceGroups, hardware.ResourceGroup{
			ID: groupID, PhysicalDeviceID: deviceID, DisplayName: "Simulator modem resources " + suffix,
			Resources: []string{
				hardware.ResourceRadioControl, hardware.ResourceSIMAccess, hardware.ResourceVoiceMedia, hardware.ResourceSMSStorage,
				hardware.ResourceSIMAPDU, hardware.ResourceHostVoWiFiAuth, hardware.ResourceNetworkSelection, hardware.ResourceSIMLock,
			},
			ModemFunctionIDs: []string{functionID}, SIMSlotIDs: []string{slotID}, MaxActiveCalls: 1, MaxConcurrentOps: 1, Generation: 1,
		})
		snapshot.Lines = append(snapshot.Lines, hardware.Line{
			ID: "simulator-line-" + suffix, PhysicalDeviceID: deviceID, ModemFunctionID: functionID,
			SubscriptionProfileID: profileID, ResourceGroupID: groupID,
			DisplayName: "Simulator line " + suffix, Generation: 1, Capabilities: capabilities,
		})
	}
	return snapshot, nil
}
