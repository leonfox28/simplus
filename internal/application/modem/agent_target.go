package modem

import (
	"context"
	"errors"
	"strings"

	"github.com/leonfox28/simplus/internal/agentapi"
)

type agentSnapshotClient interface {
	Snapshot(context.Context, bool) (agentapi.Snapshot, error)
}

func resolveAgentTarget(ctx context.Context, client agentSnapshotClient, hardwareDeviceID string, unavailable error) (agentapi.Snapshot, agentapi.DeviceReport, string, error) {
	if client == nil || !strings.HasPrefix(hardwareDeviceID, "agent-") {
		return agentapi.Snapshot{}, agentapi.DeviceReport{}, "", unavailable
	}
	agentDeviceID := strings.TrimPrefix(hardwareDeviceID, "agent-")
	if agentDeviceID == "" || len(agentDeviceID) > 128 {
		return agentapi.Snapshot{}, agentapi.DeviceReport{}, "", unavailable
	}
	snapshot, err := client.Snapshot(ctx, true)
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
