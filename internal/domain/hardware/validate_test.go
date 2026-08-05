package hardware

import (
	"errors"
	"testing"
	"time"
)

func TestNormalizeAndValidateAcceptsDynamicTopologyAndReturnsIndependentCopy(t *testing.T) {
	input := validSnapshot()
	input.Devices = append(input.Devices, PhysicalDevice{ID: "device-2", DisplayName: "Second modem", Transport: TransportUSB, State: DeviceAvailable, Generation: 1})
	inactive := input.SubscriptionProfiles[0]
	inactive.ID = "profile-2"
	inactive.DisplayName = "Installed inactive profile"
	inactive.State = ProfileInactive
	inactive.IdentityFingerprint = "1111111111111111111111111111111111111111111111111111111111111111"
	inactive.DisplayIdentityHint = "ICCID •••• 0202"
	input.SubscriptionProfiles = append(input.SubscriptionProfiles, inactive)

	normalized, err := NormalizeAndValidate(input)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Devices[0].ID != "device-1" || normalized.Devices[1].ID != "device-2" {
		t.Fatalf("devices = %#v", normalized.Devices)
	}
	input.Devices[0].DisplayName = "mutated"
	input.ResourceGroups[0].Resources[0] = "mutated"
	if normalized.Devices[0].DisplayName == "mutated" || normalized.ResourceGroups[0].Resources[0] == "mutated" {
		t.Fatal("normalized snapshot aliases input")
	}
}

func TestNormalizeAndValidateRejectsBrokenCrossReferencesAndDuplicateProfiles(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Snapshot)
	}{
		{name: "line device mismatch", mutate: func(snapshot *Snapshot) { snapshot.Lines[0].PhysicalDeviceID = "missing" }},
		{name: "line function not in group", mutate: func(snapshot *Snapshot) { snapshot.ResourceGroups[0].ModemFunctionIDs = []string{"missing"} }},
		{name: "line slot not in group", mutate: func(snapshot *Snapshot) { snapshot.ResourceGroups[0].SIMSlotIDs = []string{"missing"} }},
		{name: "active media mismatch", mutate: func(snapshot *Snapshot) { snapshot.SIMSlots[0].ActiveMediaID = "missing" }},
		{name: "media attached to absent slot", mutate: func(snapshot *Snapshot) { snapshot.SIMSlots[0].Presence = SlotAbsent }},
		{name: "line missing SIM access", mutate: func(snapshot *Snapshot) { snapshot.Lines[0].Capabilities.SIMAccess = false }},
		{name: "duplicate line profile", mutate: func(snapshot *Snapshot) {
			duplicate := snapshot.Lines[0]
			duplicate.ID = "line-2"
			snapshot.Lines = append(snapshot.Lines, duplicate)
		}},
		{name: "multiple active profiles on one media", mutate: func(snapshot *Snapshot) {
			duplicate := snapshot.SubscriptionProfiles[0]
			duplicate.ID = "profile-2"
			snapshot.SubscriptionProfiles = append(snapshot.SubscriptionProfiles, duplicate)
		}},
		{name: "full identity leaked into hint", mutate: func(snapshot *Snapshot) { snapshot.SubscriptionProfiles[0].DisplayIdentityHint = "\nsecret" }},
		{name: "entity generation ahead of snapshot", mutate: func(snapshot *Snapshot) { snapshot.ResourceGroups[0].Generation = 2 }},
		{name: "resource group generation behind profile binding", mutate: func(snapshot *Snapshot) {
			snapshot.Generation = 2
			snapshot.SubscriptionProfiles[0].Generation = 2
			snapshot.Lines[0].Generation = 2
		}},
		{name: "line capability escalation", mutate: func(snapshot *Snapshot) { snapshot.Lines[0].Capabilities.NetworkScan = true }},
		{name: "sms line without sms resource", mutate: func(snapshot *Snapshot) {
			snapshot.ResourceGroups[0].Resources = []string{ResourceSIMAccess, ResourceRadioControl}
		}},
		{name: "call capacity without media resource", mutate: func(snapshot *Snapshot) { snapshot.ResourceGroups[0].MaxActiveCalls = 1 }},
		{name: "resource capability mismatch", mutate: func(snapshot *Snapshot) {
			snapshot.ResourceGroups[0].Resources = append(snapshot.ResourceGroups[0].Resources, ResourceSIMAPDU)
		}},
		{name: "uac without cellular digital voice", mutate: func(snapshot *Snapshot) { snapshot.ModemFunctions[0].Capabilities.USBUAC = true }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := validSnapshot()
			test.mutate(&snapshot)
			if _, err := NormalizeAndValidate(snapshot); !errors.Is(err, ErrInvalidSnapshot) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestNormalizeAndValidateAcceptsHostVoWiFiCallCapacityWithoutModemVoiceMedia(t *testing.T) {
	snapshot := validSnapshot()
	snapshot.ModemFunctions[0].Capabilities.SIMAPDU = true
	snapshot.ModemFunctions[0].Capabilities.HostVoWiFiAuth = true
	snapshot.Lines[0].Capabilities.SIMAPDU = true
	snapshot.Lines[0].Capabilities.HostVoWiFiAuth = true
	snapshot.ResourceGroups[0].Resources = append(snapshot.ResourceGroups[0].Resources, ResourceSIMAPDU, ResourceHostVoWiFiAuth)
	snapshot.ResourceGroups[0].MaxActiveCalls = 1
	if _, err := NormalizeAndValidate(snapshot); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeAndValidateAllowsZeroDevicesAndLines(t *testing.T) {
	snapshot := Snapshot{Generation: 1, ObservedAt: time.Unix(1_700_000_000, 0)}
	normalized, err := NormalizeAndValidate(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(normalized.Devices) != 0 || len(normalized.Lines) != 0 {
		t.Fatalf("snapshot = %#v", normalized)
	}
}

func validSnapshot() Snapshot {
	fingerprint := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	return Snapshot{
		Generation: 1,
		ObservedAt: time.Unix(1_700_000_000, 0),
		Devices: []PhysicalDevice{{
			ID: "device-1", DisplayName: "Simulated modem", Transport: TransportSimulated, State: DeviceAvailable, Generation: 1,
		}},
		ModemFunctions: []ModemFunction{{
			ID: "function-1", PhysicalDeviceID: "device-1", DisplayName: "Cellular function", Backend: BackendSimulated, Generation: 1,
			Capabilities: Capabilities{SIMAccess: true, SMS: true, RFControl: true},
		}},
		SIMSlots: []SIMSlot{{
			ID: "slot-1", PhysicalDeviceID: "device-1", Index: 0, Presence: SlotPresent, ActiveMediaID: "media-1", Generation: 1,
		}},
		SIMMedia: []SIMMedia{{
			ID: "media-1", SIMSlotID: "slot-1", Kind: MediaUICC, IdentityState: MediaIdentityKnown,
			IdentityFingerprint: fingerprint, DisplayIdentityHint: "SIM •••• 0101", Generation: 1,
		}},
		SubscriptionProfiles: []SubscriptionProfile{{
			ID: "profile-1", SIMMediaID: "media-1", DisplayName: "Simulated profile", State: ProfileActive,
			IdentityFingerprint: fingerprint, DisplayIdentityHint: "ICCID •••• 0101", Generation: 1,
		}},
		ResourceGroups: []ResourceGroup{{
			ID: "resource-group-1", PhysicalDeviceID: "device-1", DisplayName: "Shared modem resources",
			Resources: []string{ResourceSIMAccess, ResourceRadioControl, ResourceSMSStorage}, ModemFunctionIDs: []string{"function-1"},
			SIMSlotIDs: []string{"slot-1"}, MaxActiveCalls: 0, MaxConcurrentOps: 1, Generation: 1,
		}},
		Lines: []Line{{
			ID: "line-1", PhysicalDeviceID: "device-1", ModemFunctionID: "function-1", SubscriptionProfileID: "profile-1",
			ResourceGroupID: "resource-group-1", DisplayName: "Simulated line", Generation: 1,
			Capabilities: Capabilities{SIMAccess: true, SMS: true, RFControl: true},
		}},
	}
}
