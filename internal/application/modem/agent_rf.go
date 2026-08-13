package modem

import (
	"context"
	"regexp"

	"github.com/leonfox28/simplus/internal/agentapi"
	domain "github.com/leonfox28/simplus/internal/domain/modem"
)

var plmnPattern = regexp.MustCompile(`^[0-9]{5,6}$`)

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
	status, err := controller.Read(ctx, hardwareDeviceID)
	if err != nil {
		return domain.RFStateUnknown, err
	}
	return status.RFState, nil
}

func (controller *AgentRFController) Read(ctx context.Context, hardwareDeviceID string) (domain.RuntimeStatus, error) {
	snapshot, device, agentDeviceID, err := controller.target(ctx, hardwareDeviceID)
	if err != nil {
		return domain.RuntimeStatus{}, err
	}
	probe, err := controller.client.Probe(ctx, agentapi.ProbeRequest{DeviceIDs: []string{agentDeviceID}})
	if err != nil || probe.ProtocolVersion != agentapi.ProtocolVersion ||
		probe.AgentInstanceID != snapshot.AgentInstanceID ||
		probe.SnapshotGeneration != snapshot.Generation || probe.SnapshotRevision != snapshot.Revision ||
		len(probe.Devices) != 1 || probe.Devices[0].DeviceID != device.ID {
		return domain.RuntimeStatus{}, ErrRFUnavailable
	}
	observation := probe.Devices[0]
	classification := agentapi.ClassifyCellular(observation)
	registrations := make([]domain.CellularRegistration, 0, 3)
	for _, domainName := range []string{agentapi.RegistrationDomainCS, agentapi.RegistrationDomainPacket, agentapi.RegistrationDomainEPS} {
		state := agentapi.RegistrationUnknown
		for _, registration := range observation.Registrations {
			if registration.Domain == domainName {
				state = registration.State
				break
			}
		}
		registrations = append(registrations, domain.CellularRegistration{Domain: domainName, State: state})
	}
	signalRSSI := 0
	if observation.SignalMetrics.State == agentapi.SignalStateMeasured && observation.SignalMetrics.RSSIDBm != nil {
		signalRSSI = *observation.SignalMetrics.RSSIDBm
	}
	return domain.RuntimeStatus{
		RFState: normalizeRFState(observation.RF.State), SIMPresence: normalizeSIMPresence(observation.SIM.State),
		Cellular: domain.CellularStatus{
			State: classification.State, ErrorCode: classification.ReasonCode, Registrations: registrations,
			OperatorName: observation.CurrentNetwork.OperatorName, OperatorCode: normalizePLMN(observation.CurrentNetwork.PLMN),
			RAT: observation.CurrentNetwork.RAT, SignalState: observation.SignalMetrics.State,
			SignalRSSIDBm: signalRSSI, ObservedAt: probe.ObservedAt.UTC(),
		},
	}, nil
}

func normalizePLMN(value string) string {
	if !plmnPattern.MatchString(value) {
		return ""
	}
	return value[:3] + "-" + value[3:]
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
	if controller == nil {
		return agentapi.Snapshot{}, agentapi.DeviceReport{}, "", ErrRFUnavailable
	}
	return resolveAgentTarget(ctx, controller.client, hardwareDeviceID, ErrRFUnavailable)
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

func normalizeSIMPresence(state string) string {
	switch state {
	case agentapi.SIMStatePresent, agentapi.SIMStateLocked:
		return domain.SIMPresencePresent
	case agentapi.SIMStateAbsent:
		return domain.SIMPresenceAbsent
	default:
		return domain.SIMPresenceUnknown
	}
}
