package agentapi

import (
	"context"
	"errors"
	"fmt"
)

const FeatureRFControl = "rf-control-v1"

var (
	ErrRFRequestInvalid = errors.New("RF request is invalid")
	ErrRFAgentStale     = errors.New("RF Agent instance changed")
	ErrRFSnapshotStale  = errors.New("RF hardware snapshot changed")
	ErrRFDeviceStale    = errors.New("RF device generation changed")
	ErrRFUnsupported    = errors.New("RF control is unsupported for this device")
	ErrRFActiveCall     = errors.New("RF state cannot change during an active call")
	ErrRFNotConfirmed   = errors.New("RF state change was not confirmed")
	ErrRFUnavailable    = errors.New("RF control backend is unavailable")
)

type RFSetRequest struct {
	AgentInstanceID    string `json:"agentInstanceId"`
	SnapshotGeneration uint64 `json:"snapshotGeneration"`
	SnapshotRevision   string `json:"snapshotRevision"`
	DeviceID           string `json:"deviceId"`
	DeviceGeneration   uint64 `json:"deviceGeneration"`
	Enabled            bool   `json:"enabled"`
}

type RFSetResponse struct {
	ProtocolVersion int    `json:"protocolVersion"`
	AgentInstanceID string `json:"agentInstanceId"`
	DeviceID        string `json:"deviceId"`
	State           string `json:"state"`
	Applied         bool   `json:"applied"`
}

type RFBackend interface {
	SetRFState(context.Context, Snapshot, string, bool) (RFObservation, bool, error)
}

type RFService struct {
	monitor *Monitor
	backend RFBackend
}

func NewRFService(monitor *Monitor, backend RFBackend) *RFService {
	return &RFService{monitor: monitor, backend: backend}
}

func (service *RFService) Set(ctx context.Context, request RFSetRequest) (RFSetResponse, error) {
	if service == nil || service.monitor == nil || service.backend == nil {
		return RFSetResponse{}, ErrRFUnavailable
	}
	if !IsValidAgentInstanceID(request.AgentInstanceID) || request.SnapshotGeneration == 0 || len(request.SnapshotRevision) != 64 ||
		request.DeviceID == "" || len(request.DeviceID) > 128 || request.DeviceGeneration == 0 {
		return RFSetResponse{}, ErrRFRequestInvalid
	}
	snapshot := service.monitor.Snapshot()
	if request.AgentInstanceID != snapshot.AgentInstanceID {
		return RFSetResponse{}, ErrRFAgentStale
	}
	if request.SnapshotGeneration != snapshot.Generation || request.SnapshotRevision != snapshot.Revision {
		return RFSetResponse{}, ErrRFSnapshotStale
	}
	found := false
	for _, device := range snapshot.Devices {
		if device.ID != request.DeviceID {
			continue
		}
		found = true
		if device.Generation != request.DeviceGeneration {
			return RFSetResponse{}, ErrRFDeviceStale
		}
		if !observedAgentCapability(device, "rf-control") {
			return RFSetResponse{}, ErrRFUnsupported
		}
		break
	}
	if !found {
		return RFSetResponse{}, ErrRFDeviceStale
	}
	observation, applied, err := service.backend.SetRFState(ctx, snapshot, request.DeviceID, request.Enabled)
	if err != nil {
		return RFSetResponse{}, err
	}
	expected := RFStateOff
	if request.Enabled {
		expected = RFStateOn
	}
	if observation.State != expected {
		return RFSetResponse{}, fmt.Errorf("%w: observed %s", ErrRFNotConfirmed, observation.State)
	}
	return RFSetResponse{
		ProtocolVersion: ProtocolVersion, AgentInstanceID: snapshot.AgentInstanceID,
		DeviceID: request.DeviceID, State: observation.State, Applied: applied,
	}, nil
}
