package modem

import (
	"context"

	"github.com/leonfox28/simplus/internal/agentapi"
)

type AgentEquipmentIdentityClient interface {
	Snapshot(context.Context, bool) (agentapi.Snapshot, error)
	ReadEquipmentIdentity(context.Context, agentapi.EquipmentIdentityReadRequest) (agentapi.EquipmentIdentityReadResponse, error)
}

type AgentEquipmentIdentityReader struct{ client AgentEquipmentIdentityClient }

func NewAgentEquipmentIdentityReader(client AgentEquipmentIdentityClient) *AgentEquipmentIdentityReader {
	return &AgentEquipmentIdentityReader{client: client}
}

func (reader *AgentEquipmentIdentityReader) Read(ctx context.Context, hardwareDeviceID string) (EquipmentIdentity, error) {
	if reader == nil {
		return EquipmentIdentity{}, ErrEquipmentIdentityUnavailable
	}
	snapshot, device, agentDeviceID, err := resolveAgentTarget(ctx, reader.client, hardwareDeviceID, ErrEquipmentIdentityUnavailable)
	if err != nil {
		return EquipmentIdentity{}, err
	}
	response, err := reader.client.ReadEquipmentIdentity(ctx, agentapi.EquipmentIdentityReadRequest{
		AgentInstanceID: snapshot.AgentInstanceID, SnapshotGeneration: snapshot.Generation,
		SnapshotRevision: snapshot.Revision, DeviceID: agentDeviceID, DeviceGeneration: device.Generation,
	})
	if err != nil {
		return EquipmentIdentity{}, err
	}
	return EquipmentIdentity{IMEI: response.IMEI, Fingerprint: response.Fingerprint}, nil
}
