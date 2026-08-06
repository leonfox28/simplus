package agentapi

import (
	"context"
	"errors"
	"regexp"
)

const FeatureEquipmentIdentityRead = "equipment-identity-read-v1"

var (
	ErrEquipmentIdentityRequestInvalid = errors.New("equipment identity request is invalid")
	ErrEquipmentIdentityAgentStale     = errors.New("equipment identity Agent instance changed")
	ErrEquipmentIdentitySnapshotStale  = errors.New("equipment identity hardware snapshot changed")
	ErrEquipmentIdentityDeviceStale    = errors.New("equipment identity device generation changed")
	ErrEquipmentIdentityUnsupported    = errors.New("equipment identity read is unsupported for this device")
	ErrEquipmentIdentityUnavailable    = errors.New("equipment identity backend is unavailable")
)

var equipmentIMEIPattern = regexp.MustCompile(`^[0-9]{15}$`)

type EquipmentIdentityReadRequest struct {
	AgentInstanceID    string `json:"agentInstanceId"`
	SnapshotGeneration uint64 `json:"snapshotGeneration"`
	SnapshotRevision   string `json:"snapshotRevision"`
	DeviceID           string `json:"deviceId"`
	DeviceGeneration   uint64 `json:"deviceGeneration"`
}

type EquipmentIdentityObservation struct {
	IMEI        string
	Fingerprint string
}

type EquipmentIdentityReadResponse struct {
	ProtocolVersion int    `json:"protocolVersion"`
	AgentInstanceID string `json:"agentInstanceId"`
	DeviceID        string `json:"deviceId"`
	IMEI            string `json:"imei"`
	Fingerprint     string `json:"fingerprint"`
}

type EquipmentIdentityBackend interface {
	ReadEquipmentIdentity(context.Context, Snapshot, string) (EquipmentIdentityObservation, error)
}

type EquipmentIdentityService struct {
	monitor *Monitor
	backend EquipmentIdentityBackend
}

func NewEquipmentIdentityService(monitor *Monitor, backend EquipmentIdentityBackend) *EquipmentIdentityService {
	return &EquipmentIdentityService{monitor: monitor, backend: backend}
}

func (service *EquipmentIdentityService) Read(ctx context.Context, request EquipmentIdentityReadRequest) (EquipmentIdentityReadResponse, error) {
	if service == nil || service.monitor == nil || service.backend == nil {
		return EquipmentIdentityReadResponse{}, ErrEquipmentIdentityUnavailable
	}
	if !IsValidAgentInstanceID(request.AgentInstanceID) || request.SnapshotGeneration == 0 || len(request.SnapshotRevision) != 64 ||
		request.DeviceID == "" || len(request.DeviceID) > 128 || request.DeviceGeneration == 0 {
		return EquipmentIdentityReadResponse{}, ErrEquipmentIdentityRequestInvalid
	}
	snapshot := service.monitor.Snapshot()
	if request.AgentInstanceID != snapshot.AgentInstanceID {
		return EquipmentIdentityReadResponse{}, ErrEquipmentIdentityAgentStale
	}
	if request.SnapshotGeneration != snapshot.Generation || request.SnapshotRevision != snapshot.Revision {
		return EquipmentIdentityReadResponse{}, ErrEquipmentIdentitySnapshotStale
	}
	found := false
	for _, device := range snapshot.Devices {
		if device.ID != request.DeviceID {
			continue
		}
		found = true
		if device.Generation != request.DeviceGeneration {
			return EquipmentIdentityReadResponse{}, ErrEquipmentIdentityDeviceStale
		}
		break
	}
	if !found {
		return EquipmentIdentityReadResponse{}, ErrEquipmentIdentityDeviceStale
	}
	observation, err := service.backend.ReadEquipmentIdentity(ctx, snapshot, request.DeviceID)
	if err != nil {
		return EquipmentIdentityReadResponse{}, err
	}
	if !validIMEI(observation.IMEI) || !isSHA256Hex(observation.Fingerprint) {
		return EquipmentIdentityReadResponse{}, ErrEquipmentIdentityUnavailable
	}
	return EquipmentIdentityReadResponse{
		ProtocolVersion: ProtocolVersion, AgentInstanceID: snapshot.AgentInstanceID,
		DeviceID: request.DeviceID, IMEI: observation.IMEI, Fingerprint: observation.Fingerprint,
	}, nil
}

func validIMEI(value string) bool {
	if !equipmentIMEIPattern.MatchString(value) {
		return false
	}
	sum := 0
	for index, character := range value {
		digit := int(character - '0')
		if index%2 == 1 {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
	}
	return sum%10 == 0
}
