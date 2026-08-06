package modem

import (
	"context"
	"errors"
	"strings"

	"github.com/leonfox28/simplus/internal/agentapi"
	domain "github.com/leonfox28/simplus/internal/domain/modem"
)

type AgentRFClient interface {
	Snapshot(context.Context, bool) (agentapi.Snapshot, error)
	Probe(context.Context, agentapi.ProbeRequest) (agentapi.ProbeResponse, error)
	SetRFState(context.Context, agentapi.RFSetRequest) (agentapi.RFSetResponse, error)
}

type AgentRFController struct{ client AgentRFClient }

func NewAgentRFController(client AgentRFClient) *AgentRFController {
	return &AgentRFController{client: client}
}

func (controller *AgentRFController) State(ctx context.Context, hardwareDeviceID string) (string, error) {
	snapshot, device, agentDeviceID, err := controller.target(ctx, hardwareDeviceID)
	if err != nil {
		return domain.RFStateUnknown, err
	}
	probe, err := controller.client.Probe(ctx, agentapi.ProbeRequest{DeviceIDs: []string{agentDeviceID}})
	if err != nil || probe.ProtocolVersion != agentapi.ProtocolVersion ||
		probe.AgentInstanceID != snapshot.AgentInstanceID ||
		probe.SnapshotGeneration != snapshot.Generation || probe.SnapshotRevision != snapshot.Revision ||
		len(probe.Devices) != 1 || probe.Devices[0].DeviceID != device.ID {
		return domain.RFStateUnknown, ErrRFUnavailable
	}
	return normalizeRFState(probe.Devices[0].RF.State), nil
}

func (controller *AgentRFController) Set(ctx context.Context, hardwareDeviceID string, enabled bool) (string, error) {
	snapshot, device, agentDeviceID, err := controller.target(ctx, hardwareDeviceID)
	if err != nil {
		return domain.RFStateUnknown, err
	}
	response, err := controller.client.SetRFState(ctx, agentapi.RFSetRequest{
		AgentInstanceID: snapshot.AgentInstanceID, SnapshotGeneration: snapshot.Generation,
		SnapshotRevision: snapshot.Revision, DeviceID: agentDeviceID,
		DeviceGeneration: device.Generation, Enabled: enabled,
	})
	if err != nil {
		return domain.RFStateUnknown, err
	}
	return normalizeRFState(response.State), nil
}

func (controller *AgentRFController) target(ctx context.Context, hardwareDeviceID string) (agentapi.Snapshot, agentapi.DeviceReport, string, error) {
	if controller == nil || controller.client == nil || !strings.HasPrefix(hardwareDeviceID, "agent-") {
		return agentapi.Snapshot{}, agentapi.DeviceReport{}, "", ErrRFUnavailable
	}
	agentDeviceID := strings.TrimPrefix(hardwareDeviceID, "agent-")
	if agentDeviceID == "" || len(agentDeviceID) > 128 {
		return agentapi.Snapshot{}, agentapi.DeviceReport{}, "", ErrRFUnavailable
	}
	snapshot, err := controller.client.Snapshot(ctx, true)
	if err != nil {
		return agentapi.Snapshot{}, agentapi.DeviceReport{}, "", err
	}
	for _, device := range snapshot.Devices {
		if device.ID == agentDeviceID {
			return snapshot, device, agentDeviceID, nil
		}
	}
	return snapshot, agentapi.DeviceReport{}, agentDeviceID, errors.New("managed modem is offline")
}

func normalizeRFState(state string) string {
	if state == agentapi.RFStateOn {
		return domain.RFStateOn
	}
	if state == agentapi.RFStateOff || state == agentapi.RFStateMinimum {
		return domain.RFStateOff
	}
	return domain.RFStateUnknown
}
