package modem

import (
	"context"
	"strings"
	"testing"

	"github.com/leonfox28/simplus/internal/agentapi"
	domain "github.com/leonfox28/simplus/internal/domain/modem"
)

type fakeAgentRFClient struct {
	snapshot agentapi.Snapshot
	probe    agentapi.ProbeResponse
	request  agentapi.RFSetRequest
}

func (client *fakeAgentRFClient) Snapshot(context.Context, bool) (agentapi.Snapshot, error) {
	return client.snapshot, nil
}

func (client *fakeAgentRFClient) Probe(context.Context, agentapi.ProbeRequest) (agentapi.ProbeResponse, error) {
	return client.probe, nil
}

func (client *fakeAgentRFClient) SetRFState(_ context.Context, request agentapi.RFSetRequest) (agentapi.RFSetResponse, error) {
	client.request = request
	state := agentapi.RFStateOff
	if request.Enabled {
		state = agentapi.RFStateOn
	}
	return agentapi.RFSetResponse{
		ProtocolVersion: agentapi.ProtocolVersion, AgentInstanceID: request.AgentInstanceID,
		DeviceID: request.DeviceID, State: state, Applied: true,
	}, nil
}

func TestAgentRFControllerKeepsHardwareBindingAndFencesInsideBackend(t *testing.T) {
	revision := strings.Repeat("a", 64)
	client := &fakeAgentRFClient{snapshot: agentapi.Snapshot{
		ProtocolVersion: agentapi.ProtocolVersion, AgentInstanceID: "01234567-89ab-cdef-0123-456789abcdef",
		Generation: 7, Revision: revision,
		Devices: []agentapi.DeviceReport{{ID: "usb-1-3", Generation: 7}},
	}, probe: agentapi.ProbeResponse{
		ProtocolVersion: agentapi.ProtocolVersion, AgentInstanceID: "01234567-89ab-cdef-0123-456789abcdef",
		SnapshotGeneration: 7, SnapshotRevision: revision,
		Devices: []agentapi.DeviceProbe{{
			DeviceID: "usb-1-3", State: agentapi.ProbeStateComplete,
			RF: agentapi.RFObservation{State: agentapi.RFStateMinimum},
		}},
	}}
	controller := NewAgentRFController(client)
	state, err := controller.State(t.Context(), "agent-usb-1-3")
	if err != nil || state != domain.RFStateOff {
		t.Fatalf("state=%q error=%v", state, err)
	}
	state, err = controller.Set(t.Context(), "agent-usb-1-3", true)
	if err != nil || state != domain.RFStateOn || client.request.DeviceID != "usb-1-3" ||
		client.request.AgentInstanceID != client.snapshot.AgentInstanceID || client.request.SnapshotRevision != revision || !client.request.Enabled {
		t.Fatalf("state=%q request=%#v error=%v", state, client.request, err)
	}
}
