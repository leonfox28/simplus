package modem

import (
	"context"
	"strings"
	"testing"

	"github.com/leonfox28/simplus/internal/agentapi"
)

type fakeAgentEquipmentIdentityClient struct {
	snapshot agentapi.Snapshot
	request  agentapi.EquipmentIdentityReadRequest
}

func (client *fakeAgentEquipmentIdentityClient) Snapshot(context.Context, bool) (agentapi.Snapshot, error) {
	return client.snapshot, nil
}

func (client *fakeAgentEquipmentIdentityClient) ReadEquipmentIdentity(_ context.Context, request agentapi.EquipmentIdentityReadRequest) (agentapi.EquipmentIdentityReadResponse, error) {
	client.request = request
	return agentapi.EquipmentIdentityReadResponse{
		ProtocolVersion: agentapi.ProtocolVersion, AgentInstanceID: request.AgentInstanceID,
		DeviceID: request.DeviceID, IMEI: "490154203237518", Fingerprint: strings.Repeat("a", 64),
	}, nil
}

func TestAgentEquipmentIdentityReaderKeepsTargetAndSnapshotFenceInsideBackend(t *testing.T) {
	revision := strings.Repeat("b", 64)
	client := &fakeAgentEquipmentIdentityClient{snapshot: agentapi.Snapshot{
		ProtocolVersion: agentapi.ProtocolVersion, AgentInstanceID: "01234567-89ab-cdef-0123-456789abcdef",
		Generation: 7, Revision: revision,
		Devices: []agentapi.DeviceReport{{ID: "usb-1-3", Generation: 9}},
	}}
	reader := NewAgentEquipmentIdentityReader(client)
	identity, err := reader.Read(t.Context(), "agent-usb-1-3")
	if err != nil || identity.IMEI != "490154203237518" || identity.Fingerprint != strings.Repeat("a", 64) ||
		client.request.DeviceID != "usb-1-3" || client.request.DeviceGeneration != 9 ||
		client.request.SnapshotRevision != revision || client.request.AgentInstanceID != client.snapshot.AgentInstanceID {
		t.Fatalf("identity=%#v request=%#v error=%v", identity, client.request, err)
	}
}
