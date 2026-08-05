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
				ID: "usb-1-1", Generation: 2, DisplayName: "DJI/Baiwang QDC507", Profile: agentapi.ProfileQDC507,
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
			Devices: []agentapi.DeviceProbe{{DeviceID: "usb-1-1", State: agentapi.ProbeStateComplete, SIM: agentapi.SIMObservation{State: agentapi.SIMStatePresent}}},
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

func TestAgentSourceMaterializesReadyRFOffSIMAsStableHardwareLine(t *testing.T) {
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
				},
			}},
		},
		probe: agentapi.ProbeResponse{
			ProtocolVersion: agentapi.ProtocolVersion, AgentInstanceID: instanceID, SnapshotGeneration: 5, SnapshotRevision: revision,
			Devices: []agentapi.DeviceProbe{{
				DeviceID: "usb-1-3", State: agentapi.ProbeStateComplete, RF: agentapi.RFObservation{State: agentapi.RFStateOff},
				SIM:           agentapi.SIMObservation{State: agentapi.SIMStatePresent, PrimaryLockState: agentapi.PrimaryLockReady, IdentityFingerprint: fingerprint, DisplayIdentityHint: "ICCID •••• 2115"},
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
	if line.ID != "agent-line-"+fingerprint[:32] || !line.Capabilities.SIMAccess || !line.Capabilities.SIMAPDU || line.Capabilities.HostVoWiFiAuth {
		t.Fatalf("line = %#v", line)
	}
	if topology.SIMMedia[0].IdentityFingerprint != fingerprint || topology.SubscriptionProfiles[0].DisplayIdentityHint != "ICCID •••• 2115" {
		t.Fatalf("identity topology = %#v", topology)
	}
}
