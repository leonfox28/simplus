package hardware

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	ErrInvalidSnapshot = errors.New("hardware snapshot is invalid")
	identifierPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	fingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	usbAddressPattern  = regexp.MustCompile(`^[0-9]+-[0-9]+(?:\.[0-9]+)*$`)
	usbIdentifier      = regexp.MustCompile(`^[0-9a-f]{4}$`)
)

const maxEntitiesPerKind = 1024

func NormalizeAndValidate(input Snapshot) (Snapshot, error) {
	if !validGeneration(input.Generation) || input.ObservedAt.IsZero() {
		return Snapshot{}, ErrInvalidSnapshot
	}
	if len(input.Devices) > maxEntitiesPerKind || len(input.ModemFunctions) > maxEntitiesPerKind ||
		len(input.SIMSlots) > maxEntitiesPerKind || len(input.SIMMedia) > maxEntitiesPerKind ||
		len(input.SubscriptionProfiles) > maxEntitiesPerKind || len(input.ResourceGroups) > maxEntitiesPerKind ||
		len(input.Lines) > maxEntitiesPerKind {
		return Snapshot{}, ErrInvalidSnapshot
	}
	snapshot := cloneSnapshot(input)
	snapshot.ObservedAt = snapshot.ObservedAt.UTC()
	sortSnapshot(&snapshot)

	devices := make(map[string]PhysicalDevice, len(snapshot.Devices))
	for _, device := range snapshot.Devices {
		if !validID(device.ID) || !validLabel(device.DisplayName) || !validEntityGeneration(device.Generation, snapshot.Generation) ||
			(device.ModemModel != "" && (!validLabel(device.ModemModel) || len(device.ModemModel) > 128)) ||
			!oneOf(device.Transport, TransportUSB, TransportUART, TransportSimulated) ||
			!oneOf(device.State, DeviceAvailable, DeviceUnavailable) ||
			(device.USBAddress != "" && !usbAddressPattern.MatchString(device.USBAddress)) ||
			(device.USBVendorID != "" && !usbIdentifier.MatchString(device.USBVendorID)) ||
			(device.USBProductID != "" && !usbIdentifier.MatchString(device.USBProductID)) ||
			(device.USBVendorID == "") != (device.USBProductID == "") ||
			(device.USBSerialNumber != "" && (!validLabel(device.USBSerialNumber) || len(device.USBSerialNumber) > 128)) ||
			(device.EquipmentIdentityFingerprint != "" && !fingerprintPattern.MatchString(device.EquipmentIdentityFingerprint)) ||
			(device.USBSerialFingerprint != "" && !fingerprintPattern.MatchString(device.USBSerialFingerprint)) {
			return Snapshot{}, ErrInvalidSnapshot
		}
		if _, duplicate := devices[device.ID]; duplicate {
			return Snapshot{}, ErrInvalidSnapshot
		}
		devices[device.ID] = device
	}

	functions := make(map[string]ModemFunction, len(snapshot.ModemFunctions))
	for _, function := range snapshot.ModemFunctions {
		if !validID(function.ID) || !validLabel(function.DisplayName) || !validID(function.PhysicalDeviceID) || !validEntityGeneration(function.Generation, snapshot.Generation) ||
			!oneOf(function.Backend, BackendSimulated, BackendDirectAT, BackendDirectQMI, BackendDirectMBIM, BackendModemManager, BackendPCSC) {
			return Snapshot{}, ErrInvalidSnapshot
		}
		if _, present := devices[function.PhysicalDeviceID]; !present {
			return Snapshot{}, ErrInvalidSnapshot
		}
		if (function.Capabilities.USBUAC && (!function.Capabilities.DigitalVoiceMedia || !function.Capabilities.CellularVoice)) ||
			(function.Capabilities.SIMAPDU && !function.Capabilities.SIMAccess) ||
			(function.Capabilities.HostVoWiFiAuth && !function.Capabilities.SIMAPDU) ||
			((function.Capabilities.PrimarySIMLockState || function.Capabilities.PIN1Verify || function.Capabilities.PUK1Unblock) && !function.Capabilities.SIMAccess) ||
			(function.Capabilities.EUICCProfiles && !function.Capabilities.SIMAPDU) {
			return Snapshot{}, ErrInvalidSnapshot
		}
		if _, duplicate := functions[function.ID]; duplicate {
			return Snapshot{}, ErrInvalidSnapshot
		}
		functions[function.ID] = function
	}

	slots := make(map[string]SIMSlot, len(snapshot.SIMSlots))
	slotIndexes := make(map[string]struct{}, len(snapshot.SIMSlots))
	for _, slot := range snapshot.SIMSlots {
		if !validID(slot.ID) || !validID(slot.PhysicalDeviceID) || slot.Index < 0 || slot.Index > 255 || !validEntityGeneration(slot.Generation, snapshot.Generation) ||
			!oneOf(slot.Presence, SlotPresent, SlotAbsent, SlotUnknown) {
			return Snapshot{}, ErrInvalidSnapshot
		}
		if _, present := devices[slot.PhysicalDeviceID]; !present {
			return Snapshot{}, ErrInvalidSnapshot
		}
		if _, duplicate := slots[slot.ID]; duplicate {
			return Snapshot{}, ErrInvalidSnapshot
		}
		indexKey := fmt.Sprintf("%s/%d", slot.PhysicalDeviceID, slot.Index)
		if _, duplicate := slotIndexes[indexKey]; duplicate {
			return Snapshot{}, ErrInvalidSnapshot
		}
		slotIndexes[indexKey] = struct{}{}
		slots[slot.ID] = slot
	}

	media := make(map[string]SIMMedia, len(snapshot.SIMMedia))
	mediaBySlot := make(map[string]string, len(snapshot.SIMMedia))
	mediaFingerprints := make(map[string]string, len(snapshot.SIMMedia))
	for _, item := range snapshot.SIMMedia {
		if !validID(item.ID) || !validID(item.SIMSlotID) || !validEntityGeneration(item.Generation, snapshot.Generation) ||
			!oneOf(item.Kind, MediaUICC, MediaRemovableEUICC) ||
			!oneOf(item.IdentityState, MediaIdentityKnown, MediaIdentityUnknown) || !validIdentity(item.IdentityState, item.IdentityFingerprint, item.DisplayIdentityHint) {
			return Snapshot{}, ErrInvalidSnapshot
		}
		slot, present := slots[item.SIMSlotID]
		if !present || slot.Presence != SlotPresent {
			return Snapshot{}, ErrInvalidSnapshot
		}
		if _, duplicate := media[item.ID]; duplicate {
			return Snapshot{}, ErrInvalidSnapshot
		}
		if item.IdentityFingerprint != "" {
			if mediaFingerprints[item.IdentityFingerprint] != "" {
				return Snapshot{}, ErrInvalidSnapshot
			}
			mediaFingerprints[item.IdentityFingerprint] = item.ID
		}
		if _, duplicate := mediaBySlot[item.SIMSlotID]; duplicate {
			return Snapshot{}, ErrInvalidSnapshot
		}
		mediaBySlot[item.SIMSlotID] = item.ID
		media[item.ID] = item
	}
	for _, slot := range snapshot.SIMSlots {
		if slot.ActiveMediaID == "" {
			if slot.Presence == SlotPresent && mediaBySlot[slot.ID] != "" {
				return Snapshot{}, ErrInvalidSnapshot
			}
			continue
		}
		item, present := media[slot.ActiveMediaID]
		if !present || item.SIMSlotID != slot.ID || slot.Presence != SlotPresent {
			return Snapshot{}, ErrInvalidSnapshot
		}
	}

	profiles := make(map[string]SubscriptionProfile, len(snapshot.SubscriptionProfiles))
	activeProfileByMedia := make(map[string]string, len(snapshot.SubscriptionProfiles))
	profileFingerprints := make(map[string]string, len(snapshot.SubscriptionProfiles))
	for _, profile := range snapshot.SubscriptionProfiles {
		if !validID(profile.ID) || !validID(profile.SIMMediaID) || !validLabel(profile.DisplayName) || !validEntityGeneration(profile.Generation, snapshot.Generation) ||
			!oneOf(profile.State, ProfileActive, ProfileInactive, ProfileLocked) || !validIdentity(MediaIdentityKnown, profile.IdentityFingerprint, profile.DisplayIdentityHint) ||
			!validHomeOperator(profile.HomeOperatorName, profile.HomeOperatorCode) {
			return Snapshot{}, ErrInvalidSnapshot
		}
		if _, present := media[profile.SIMMediaID]; !present {
			return Snapshot{}, ErrInvalidSnapshot
		}
		if _, duplicate := profiles[profile.ID]; duplicate {
			return Snapshot{}, ErrInvalidSnapshot
		}
		if profileFingerprints[profile.IdentityFingerprint] != "" {
			return Snapshot{}, ErrInvalidSnapshot
		}
		profileFingerprints[profile.IdentityFingerprint] = profile.ID
		if profile.State == ProfileActive || profile.State == ProfileLocked {
			if activeProfileByMedia[profile.SIMMediaID] != "" {
				return Snapshot{}, ErrInvalidSnapshot
			}
			activeProfileByMedia[profile.SIMMediaID] = profile.ID
		}
		profiles[profile.ID] = profile
	}

	groups := make(map[string]ResourceGroup, len(snapshot.ResourceGroups))
	for _, group := range snapshot.ResourceGroups {
		if !validID(group.ID) || !validID(group.PhysicalDeviceID) || !validLabel(group.DisplayName) || !validEntityGeneration(group.Generation, snapshot.Generation) ||
			group.MaxActiveCalls < 0 || group.MaxActiveCalls > 64 || group.MaxConcurrentOps < 1 || group.MaxConcurrentOps > 64 ||
			len(group.Resources) == 0 || !uniqueAllowed(group.Resources, ResourceRadioControl, ResourceSIMAccess, ResourceVoiceMedia, ResourceSMSStorage, ResourceSIMAPDU, ResourceHostVoWiFiAuth, ResourceNetworkSelection, ResourceSIMLock, ResourceEUICCProfiles) ||
			len(group.ModemFunctionIDs) == 0 || !uniqueIDs(group.ModemFunctionIDs) || len(group.SIMSlotIDs) == 0 || !uniqueIDs(group.SIMSlotIDs) {
			return Snapshot{}, ErrInvalidSnapshot
		}
		device, present := devices[group.PhysicalDeviceID]
		if !present || device.Generation > group.Generation {
			return Snapshot{}, ErrInvalidSnapshot
		}
		if group.MaxActiveCalls > 0 && !contains(group.Resources, ResourceVoiceMedia) && !contains(group.Resources, ResourceHostVoWiFiAuth) {
			return Snapshot{}, ErrInvalidSnapshot
		}
		boundFunctions := make([]ModemFunction, 0, len(group.ModemFunctionIDs))
		for _, functionID := range group.ModemFunctionIDs {
			function, present := functions[functionID]
			if !present || function.PhysicalDeviceID != group.PhysicalDeviceID || function.Generation > group.Generation {
				return Snapshot{}, ErrInvalidSnapshot
			}
			boundFunctions = append(boundFunctions, function)
		}
		for _, resource := range group.Resources {
			if !resourceSupported(resource, boundFunctions) {
				return Snapshot{}, ErrInvalidSnapshot
			}
		}
		for _, slotID := range group.SIMSlotIDs {
			slot, present := slots[slotID]
			if !present || slot.PhysicalDeviceID != group.PhysicalDeviceID || slot.Generation > group.Generation {
				return Snapshot{}, ErrInvalidSnapshot
			}
		}
		if _, duplicate := groups[group.ID]; duplicate {
			return Snapshot{}, ErrInvalidSnapshot
		}
		groups[group.ID] = group
	}

	lines := make(map[string]Line, len(snapshot.Lines))
	lineProfiles := make(map[string]struct{}, len(snapshot.Lines))
	for _, line := range snapshot.Lines {
		if !validID(line.ID) || !validID(line.PhysicalDeviceID) || !validID(line.ModemFunctionID) ||
			!validID(line.SubscriptionProfileID) || !validID(line.ResourceGroupID) || !validLabel(line.DisplayName) || !validEntityGeneration(line.Generation, snapshot.Generation) {
			return Snapshot{}, ErrInvalidSnapshot
		}
		function, functionPresent := functions[line.ModemFunctionID]
		profile, profilePresent := profiles[line.SubscriptionProfileID]
		group, groupPresent := groups[line.ResourceGroupID]
		if !functionPresent || !profilePresent || !groupPresent || !line.Capabilities.SIMAccess || function.PhysicalDeviceID != line.PhysicalDeviceID ||
			function.Generation > line.Generation || profile.Generation > line.Generation || group.Generation > line.Generation || profile.Generation > group.Generation ||
			group.PhysicalDeviceID != line.PhysicalDeviceID || !contains(group.ModemFunctionIDs, line.ModemFunctionID) ||
			(profile.State != ProfileActive && profile.State != ProfileLocked) || !capabilitiesSubset(line.Capabilities, function.Capabilities) ||
			(line.Capabilities.CellularVoice && !line.Capabilities.DigitalVoiceMedia) ||
			!contains(group.Resources, ResourceSIMAccess) ||
			(line.Capabilities.RFControl && !contains(group.Resources, ResourceRadioControl)) ||
			(line.Capabilities.SMS && !contains(group.Resources, ResourceSMSStorage)) ||
			((line.Capabilities.CellularVoice || line.Capabilities.DigitalVoiceMedia) && !contains(group.Resources, ResourceVoiceMedia)) ||
			(line.Capabilities.SIMAPDU && !contains(group.Resources, ResourceSIMAPDU)) ||
			(line.Capabilities.HostVoWiFiAuth && !contains(group.Resources, ResourceHostVoWiFiAuth)) ||
			((line.Capabilities.NetworkScan || line.Capabilities.ManualNetworkSelection) && !contains(group.Resources, ResourceNetworkSelection)) ||
			((line.Capabilities.PrimarySIMLockState || line.Capabilities.PIN1Verify || line.Capabilities.PUK1Unblock) && !contains(group.Resources, ResourceSIMLock)) ||
			(line.Capabilities.EUICCProfiles && !contains(group.Resources, ResourceEUICCProfiles)) {
			return Snapshot{}, ErrInvalidSnapshot
		}
		item := media[profile.SIMMediaID]
		slot := slots[item.SIMSlotID]
		if slot.PhysicalDeviceID != line.PhysicalDeviceID || slot.ActiveMediaID != item.ID || !contains(group.SIMSlotIDs, slot.ID) ||
			item.Generation > group.Generation || slot.Generation > group.Generation || item.Generation > line.Generation || slot.Generation > line.Generation {
			return Snapshot{}, ErrInvalidSnapshot
		}
		if _, duplicate := lines[line.ID]; duplicate {
			return Snapshot{}, ErrInvalidSnapshot
		}
		if _, duplicate := lineProfiles[line.SubscriptionProfileID]; duplicate {
			return Snapshot{}, ErrInvalidSnapshot
		}
		lines[line.ID] = line
		lineProfiles[line.SubscriptionProfileID] = struct{}{}
	}
	return snapshot, nil
}

func cloneSnapshot(input Snapshot) Snapshot {
	clone := input
	clone.Devices = append([]PhysicalDevice(nil), input.Devices...)
	clone.ModemFunctions = append([]ModemFunction(nil), input.ModemFunctions...)
	clone.SIMSlots = append([]SIMSlot(nil), input.SIMSlots...)
	clone.SIMMedia = append([]SIMMedia(nil), input.SIMMedia...)
	clone.SubscriptionProfiles = append([]SubscriptionProfile(nil), input.SubscriptionProfiles...)
	clone.ResourceGroups = append([]ResourceGroup(nil), input.ResourceGroups...)
	clone.Lines = append([]Line(nil), input.Lines...)
	for index := range clone.ResourceGroups {
		clone.ResourceGroups[index].Resources = append([]string(nil), input.ResourceGroups[index].Resources...)
		clone.ResourceGroups[index].ModemFunctionIDs = append([]string(nil), input.ResourceGroups[index].ModemFunctionIDs...)
		clone.ResourceGroups[index].SIMSlotIDs = append([]string(nil), input.ResourceGroups[index].SIMSlotIDs...)
	}
	return clone
}

func sortSnapshot(snapshot *Snapshot) {
	sort.Slice(snapshot.Devices, func(i, j int) bool { return snapshot.Devices[i].ID < snapshot.Devices[j].ID })
	sort.Slice(snapshot.ModemFunctions, func(i, j int) bool { return snapshot.ModemFunctions[i].ID < snapshot.ModemFunctions[j].ID })
	sort.Slice(snapshot.SIMSlots, func(i, j int) bool { return snapshot.SIMSlots[i].ID < snapshot.SIMSlots[j].ID })
	sort.Slice(snapshot.SIMMedia, func(i, j int) bool { return snapshot.SIMMedia[i].ID < snapshot.SIMMedia[j].ID })
	sort.Slice(snapshot.SubscriptionProfiles, func(i, j int) bool { return snapshot.SubscriptionProfiles[i].ID < snapshot.SubscriptionProfiles[j].ID })
	sort.Slice(snapshot.ResourceGroups, func(i, j int) bool { return snapshot.ResourceGroups[i].ID < snapshot.ResourceGroups[j].ID })
	sort.Slice(snapshot.Lines, func(i, j int) bool { return snapshot.Lines[i].ID < snapshot.Lines[j].ID })
	for index := range snapshot.ResourceGroups {
		sort.Strings(snapshot.ResourceGroups[index].Resources)
		sort.Strings(snapshot.ResourceGroups[index].ModemFunctionIDs)
		sort.Strings(snapshot.ResourceGroups[index].SIMSlotIDs)
	}
}

func validID(value string) bool { return identifierPattern.MatchString(value) }

func validGeneration(value uint64) bool { return value > 0 && value <= math.MaxInt64 }

func validEntityGeneration(value, snapshotGeneration uint64) bool {
	return validGeneration(value) && value <= snapshotGeneration
}

func capabilitiesSubset(candidate, available Capabilities) bool {
	return (!candidate.SIMAccess || available.SIMAccess) &&
		(!candidate.SMS || available.SMS) &&
		(!candidate.CellularVoice || available.CellularVoice) &&
		(!candidate.DigitalVoiceMedia || available.DigitalVoiceMedia) &&
		(!candidate.USBUAC || available.USBUAC) &&
		(!candidate.SIMAPDU || available.SIMAPDU) &&
		(!candidate.HostVoWiFiAuth || available.HostVoWiFiAuth) &&
		(!candidate.RFControl || available.RFControl) &&
		(!candidate.NetworkScan || available.NetworkScan) &&
		(!candidate.ManualNetworkSelection || available.ManualNetworkSelection) &&
		(!candidate.PrimarySIMLockState || available.PrimarySIMLockState) &&
		(!candidate.PIN1Verify || available.PIN1Verify) &&
		(!candidate.PUK1Unblock || available.PUK1Unblock) &&
		(!candidate.EUICCProfiles || available.EUICCProfiles)
}

func validLabel(value string) bool {
	if value == "" || len(value) > 256 || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return strings.TrimSpace(value) == value
}

func validIdentity(state, fingerprint, hint string) bool {
	if !validLabel(hint) || len(hint) > 64 {
		return false
	}
	if state == MediaIdentityUnknown {
		return fingerprint == ""
	}
	return fingerprintPattern.MatchString(fingerprint)
}

func validHomeOperator(name, code string) bool {
	if name != "" && (!validLabel(name) || len(name) > 64) {
		return false
	}
	if code == "" {
		return true
	}
	if len(code) != 6 && len(code) != 7 || code[3] != '-' {
		return false
	}
	for index, character := range code {
		if index == 3 {
			continue
		}
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func uniqueAllowed(values []string, allowed ...string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !oneOf(value, allowed...) {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func uniqueIDs(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validID(value) {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func resourceSupported(resource string, functions []ModemFunction) bool {
	for _, function := range functions {
		capabilities := function.Capabilities
		switch resource {
		case ResourceSIMAccess:
			if capabilities.SIMAccess {
				return true
			}
		case ResourceRadioControl:
			if capabilities.RFControl {
				return true
			}
		case ResourceVoiceMedia:
			if capabilities.CellularVoice && capabilities.DigitalVoiceMedia {
				return true
			}
		case ResourceSMSStorage:
			if capabilities.SMS {
				return true
			}
		case ResourceSIMAPDU:
			if capabilities.SIMAPDU {
				return true
			}
		case ResourceHostVoWiFiAuth:
			if capabilities.HostVoWiFiAuth {
				return true
			}
		case ResourceNetworkSelection:
			if capabilities.NetworkScan || capabilities.ManualNetworkSelection {
				return true
			}
		case ResourceSIMLock:
			if capabilities.PrimarySIMLockState || capabilities.PIN1Verify || capabilities.PUK1Unblock {
				return true
			}
		case ResourceEUICCProfiles:
			if capabilities.EUICCProfiles {
				return true
			}
		}
	}
	return false
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
