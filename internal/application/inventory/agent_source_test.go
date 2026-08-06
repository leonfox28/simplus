package inventory

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/domain/hardware"
)

type fakeAgentClient struct {
	snapshot agentapi.Snapshot
	probe    agentapi.ProbeResponse
	probes   int
}

func (client *fakeAgentClient) Snapshot(context.Context, bool) (agentapi.Snapshot, error) {
	return client.snapshot, nil
}

func (client *fakeAgentClient) Probe(context.Context, agentapi.ProbeRequest) (agentapi.ProbeResponse, error) {
	client.probes++
	return client.probe, nil
}

func TestAgentSourceLeavesDescriptorOnlyCandidateNonOperable(t *testing.T) {
	revision := strings.Repeat("b", 64)
	instanceID := "fedcba98-7654-3210-fedc-ba9876543210"
	client := &fakeAgentClient{snapshot: agentapi.Snapshot{
		ProtocolVersion: agentapi.ProtocolVersion, AgentInstanceID: instanceID, Generation: 3, Revision: revision, ObservedAt: time.Now().UTC(),
		Devices: []agentapi.DeviceReport{{
			ID: "usb-1-3", Generation: 3, DisplayName: "China Mobile IoT ML307A", Profile: agentapi.ProfileML307A,
			Capabilities: []agentapi.CapabilityEvidence{
				{Capability: "at-control", Status: agentapi.EvidenceUnverified},
				{Capability: "sim-access", Status: agentapi.EvidenceUnverified},
				{Capability: "digital-voice-media", Status: agentapi.EvidenceUnsupported},
			},
		}},
	}, probe: agentapi.ProbeResponse{
		ProtocolVersion: agentapi.ProtocolVersion, AgentInstanceID: instanceID, SnapshotGeneration: 3, SnapshotRevision: revision,
		Devices: []agentapi.DeviceProbe{{DeviceID: "usb-1-3", State: agentapi.ProbeStateDescriptorOnly, SIM: agentapi.SIMObservation{State: agentapi.SIMStateUnknown}}},
	}}
	topology, err := NewAgentSource(client).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(topology.Devices) != 1 || len(topology.ModemFunctions) != 0 || len(topology.SIMSlots) != 0 || len(topology.ResourceGroups) != 0 || len(topology.Lines) != 0 {
		t.Fatalf("descriptor-only candidate became operable: %#v", topology)
	}
}

func TestAgentSourceMapsObservedHardwareWithoutClaimingUnverifiedVoiceOrSIMIdentity(t *testing.T) {
	revision := strings.Repeat("a", 64)
	instanceID := "01234567-89ab-cdef-0123-456789abcdef"
	client := &fakeAgentClient{
		snapshot: agentapi.Snapshot{
			ProtocolVersion: agentapi.ProtocolVersion, AgentInstanceID: instanceID, Generation: 2, Revision: revision, ObservedAt: time.Now().UTC(),
			Devices: []agentapi.DeviceReport{{
				ID: "usb-1-1", Generation: 2, PhysicalPath: "1-1", DisplayName: "DJI/Baiwang QDC507", Profile: agentapi.ProfileQDC507,
				USB: agentapi.USBIdentity{VendorID: "2c7c", ProductID: "0125", SerialNumber: "QDC507-SERIAL-0001", SerialFingerprint: strings.Repeat("9", 64)},
				Capabilities: []agentapi.CapabilityEvidence{
					{Capability: "at-control", Status: agentapi.EvidenceObserved},
					{Capability: "qmi-control", Status: agentapi.EvidenceObserved},
					{Capability: "sim-access", Status: agentapi.EvidenceObserved},
					{Capability: "rf-control", Status: agentapi.EvidenceObserved},
					{Capability: "sms-control", Status: agentapi.EvidenceDocumented},
					{Capability: "operator-selection", Status: agentapi.EvidenceDocumented},
					{Capability: "usb-uac", Status: agentapi.EvidenceObserved},
					{Capability: "digital-voice-media", Status: agentapi.EvidenceUnverified},
				},
			}},
		},
		probe: agentapi.ProbeResponse{
			ProtocolVersion: agentapi.ProtocolVersion, AgentInstanceID: instanceID, SnapshotGeneration: 2, SnapshotRevision: revision,
			Devices: []agentapi.DeviceProbe{{
				DeviceID: "usb-1-1", State: agentapi.ProbeStateComplete,
				Identity: agentapi.ModemIdentity{Model: "QDC507"}, SIM: agentapi.SIMObservation{State: agentapi.SIMStatePresent},
			}},
		},
	}
	source := NewAgentSource(client)
	now := time.Now()
	source.now = func() time.Time { return now }
	first, err := source.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Devices) != 1 || len(first.ModemFunctions) != 1 || len(first.SIMSlots) != 1 || len(first.ResourceGroups) != 1 {
		t.Fatalf("topology = %#v", first)
	}
	if first.Devices[0].ModemModel != "QDC507" || first.Devices[0].USBAddress != "1-1" || first.Devices[0].USBVendorID != "2c7c" ||
		first.Devices[0].USBProductID != "0125" || first.Devices[0].USBSerialNumber != "QDC507-SERIAL-0001" ||
		first.Devices[0].USBSerialFingerprint != strings.Repeat("9", 64) {
		t.Fatalf("USB display metadata = %#v", first.Devices[0])
	}
	if len(first.SIMMedia) != 0 || len(first.SubscriptionProfiles) != 0 || len(first.Lines) != 0 {
		t.Fatalf("unidentified SIM materialized into business identities: %#v", first)
	}
	function := first.ModemFunctions[0]
	if function.Backend != hardware.BackendDirectQMI || !function.Capabilities.SIMAccess || function.Capabilities.SMS || function.Capabilities.NetworkScan || function.Capabilities.ManualNetworkSelection {
		t.Fatalf("function = %#v", function)
	}
	if function.Capabilities.CellularVoice || function.Capabilities.DigitalVoiceMedia || function.Capabilities.USBUAC || function.Capabilities.HostVoWiFiAuth || function.Capabilities.SIMAPDU || function.Capabilities.PIN1Verify || function.Capabilities.PUK1Unblock {
		t.Fatalf("unverified capabilities were claimed: %#v", function.Capabilities)
	}
	if first.SIMSlots[0].Presence != hardware.SlotPresent {
		t.Fatalf("slot = %#v", first.SIMSlots[0])
	}
	if _, err := source.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if client.probes != 1 {
		t.Fatalf("probe calls = %d", client.probes)
	}
}

func TestAgentSourceMaterializesReadySIMAsStableHardwareLineIndependentlyOfRF(t *testing.T) {
	revision := strings.Repeat("c", 64)
	fingerprint := strings.Repeat("d", 64)
	instanceID := "12345678-1234-4234-8234-123456789abc"
	zeroCalls := 0
	client := &fakeAgentClient{
		snapshot: agentapi.Snapshot{
			ProtocolVersion: agentapi.ProtocolVersion, AgentInstanceID: instanceID, Generation: 5, Revision: revision, ObservedAt: time.Now().UTC(),
			Devices: []agentapi.DeviceReport{{
				ID: "usb-1-3", Generation: 5, DisplayName: "China Mobile IoT ML307A", Profile: agentapi.ProfileML307A,
				Capabilities: []agentapi.CapabilityEvidence{
					{Capability: "at-control", Status: agentapi.EvidenceObserved},
					{Capability: "rf-control", Status: agentapi.EvidenceObserved},
					{Capability: "sim-access", Status: agentapi.EvidenceObserved},
					{Capability: "sim-apdu", Status: agentapi.EvidenceObserved},
					{Capability: "host-vowifi-auth", Status: agentapi.EvidenceObserved},
				},
			}},
		},
		probe: agentapi.ProbeResponse{
			ProtocolVersion: agentapi.ProtocolVersion, AgentInstanceID: instanceID, SnapshotGeneration: 5, SnapshotRevision: revision,
			Devices: []agentapi.DeviceProbe{{
				DeviceID: "usb-1-3", State: agentapi.ProbeStateComplete, RF: agentapi.RFObservation{State: agentapi.RFStateOn},
				SIM: agentapi.SIMObservation{State: agentapi.SIMStatePresent, PrimaryLockState: agentapi.PrimaryLockReady,
					IdentityFingerprint: fingerprint, DisplayIdentityHint: "ICCID •••• 2115",
					HomeOperatorName: "VOXI", HomeOperatorCode: "234-15"},
				SignalMetrics: agentapi.SignalObservation{State: agentapi.SignalStateUnknown}, Registrations: []agentapi.RegistrationObservation{},
				CurrentNetwork: agentapi.NetworkObservation{SelectionMode: agentapi.NetworkSelectionUnknown}, ActiveCallCount: &zeroCalls,
			}},
		},
	}
	topology, err := NewAgentSource(client).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(topology.SIMMedia) != 1 || len(topology.SubscriptionProfiles) != 1 || len(topology.Lines) != 1 {
		t.Fatalf("materialized topology = %#v", topology)
	}
	line := topology.Lines[0]
	if line.ID != "agent-line-"+fingerprint[:32] || !line.Capabilities.SIMAccess || !line.Capabilities.SIMAPDU || !line.Capabilities.HostVoWiFiAuth {
		t.Fatalf("line = %#v", line)
	}
	if topology.SIMMedia[0].IdentityFingerprint != fingerprint || topology.SubscriptionProfiles[0].DisplayIdentityHint != "ICCID •••• 2115" ||
		topology.SubscriptionProfiles[0].HomeOperatorName != "VOXI" || topology.SubscriptionProfiles[0].HomeOperatorCode != "234-15" {
		t.Fatalf("identity topology = %#v", topology)
	}
}

func TestAgentSourceBuildsSIMLineWithoutInventingRFControl(t *testing.T) {
	revision := strings.Repeat("e", 64)
	fingerprint := strings.Repeat("f", 64)
	instanceID := "22345678-1234-4234-8234-123456789abc"
	client := &fakeAgentClient{
		snapshot: agentapi.Snapshot{
			ProtocolVersion: agentapi.ProtocolVersion, AgentInstanceID: instanceID, Generation: 6,
			Revision: revision, ObservedAt: time.Now().UTC(),
			Devices: []agentapi.DeviceReport{{
				ID: "usb-2-1", Generation: 6, DisplayName: "SIM-only fixture", Profile: "sim-only",
				Capabilities: []agentapi.CapabilityEvidence{
					{Capability: "at-control", Status: agentapi.EvidenceObserved},
					{Capability: "sim-access", Status: agentapi.EvidenceObserved},
					{Capability: "sim-presence", Status: agentapi.EvidenceObserved},
					{Capability: "rf-control", Status: agentapi.EvidenceUnavailable},
				},
			}},
		},
		probe: agentapi.ProbeResponse{
			ProtocolVersion: agentapi.ProtocolVersion, AgentInstanceID: instanceID,
			SnapshotGeneration: 6, SnapshotRevision: revision,
			Devices: []agentapi.DeviceProbe{{
				DeviceID: "usb-2-1", State: agentapi.ProbeStateComplete,
				SIM: agentapi.SIMObservation{
					State: agentapi.SIMStatePresent, PrimaryLockState: agentapi.PrimaryLockReady,
					IdentityFingerprint: fingerprint, DisplayIdentityHint: "ICCID •••• 1234",
				},
			}},
		},
	}
	topology, err := NewAgentSource(client).Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(topology.ModemFunctions) != 1 || len(topology.ResourceGroups) != 1 || len(topology.Lines) != 1 {
		t.Fatalf("SIM-only topology = %#v", topology)
	}
	if topology.ModemFunctions[0].Capabilities.RFControl || topology.Lines[0].Capabilities.RFControl {
		t.Fatalf("RF control was invented: function=%#v line=%#v", topology.ModemFunctions[0], topology.Lines[0])
	}
	for _, resource := range topology.ResourceGroups[0].Resources {
		if resource == hardware.ResourceRadioControl {
			t.Fatalf("RF resource was invented: %#v", topology.ResourceGroups[0])
		}
	}
	if !topology.ModemFunctions[0].Capabilities.PrimarySIMLockState {
		t.Fatalf("observed SIM presence did not map to the typed lock-state capability: %#v", topology.ModemFunctions[0])
	}
}

func TestAgentSourceKeepsControlOnlyDeviceWithoutInventingSIMGraph(t *testing.T) {
	revision := strings.Repeat("9", 64)
	instanceID := "32345678-1234-4234-8234-123456789abc"
	client := &fakeAgentClient{
		snapshot: agentapi.Snapshot{
			ProtocolVersion: agentapi.ProtocolVersion, AgentInstanceID: instanceID, Generation: 7,
			Revision: revision, ObservedAt: time.Now().UTC(),
			Devices: []agentapi.DeviceReport{{
				ID: "usb-3-1", Generation: 7, DisplayName: "control-only fixture", Profile: "control-only",
				Capabilities: []agentapi.CapabilityEvidence{{Capability: "at-control", Status: agentapi.EvidenceObserved}},
			}},
		},
		probe: agentapi.ProbeResponse{
			ProtocolVersion: agentapi.ProtocolVersion, AgentInstanceID: instanceID,
			SnapshotGeneration: 7, SnapshotRevision: revision, Devices: []agentapi.DeviceProbe{},
		},
	}
	topology, err := NewAgentSource(client).Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(topology.Devices) != 1 || len(topology.ModemFunctions) != 1 || len(topology.SIMSlots) != 0 ||
		len(topology.ResourceGroups) != 0 || len(topology.Lines) != 0 {
		t.Fatalf("control-only device gained a SIM graph: %#v", topology)
	}
}
