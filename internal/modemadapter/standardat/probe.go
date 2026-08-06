package standardat

import (
	"context"
	"strings"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/attransport"
)

// ProbePlan is selected by a model adapter. The reusable standard probe owns
// ordering and typed parsing, but it never assumes that an arbitrary modem
// supports these commands unless its adapter explicitly returns this plan.
type ProbePlan struct {
	Handshake          string
	Manufacturer       string
	Model              string
	Revision           string
	RFState            string
	CSRegistration     string
	PacketRegistration string
	EPSRegistration    string
	Operator           string
	Signal             string
	ActiveCalls        string
}

func Standard3GPPProbePlan() ProbePlan {
	return ProbePlan{
		Handshake: "AT", Manufacturer: "AT+CGMI", Model: "AT+CGMM", Revision: "AT+CGMR",
		RFState: "AT+CFUN?", CSRegistration: "AT+CREG?",
		PacketRegistration: "AT+CGREG?", EPSRegistration: "AT+CEREG?", Operator: "AT+COPS?",
		Signal: "AT+CSQ", ActiveCalls: "AT+CLCC",
	}
}

type SIMPresenceReader func(context.Context, attransport.Query) (agentapi.SIMObservation, error)

func ExecuteProbe(ctx context.Context, query attransport.Query, plan ProbePlan, readSIMPresence SIMPresenceReader) agentapi.DeviceProbe {
	result := agentapi.DeviceProbe{
		State:          agentapi.ProbeStateFailed,
		RF:             agentapi.RFObservation{State: agentapi.RFStateUnknown},
		SIM:            agentapi.SIMObservation{State: agentapi.SIMStateUnknown, PrimaryLockState: agentapi.PrimaryLockUnknown},
		SignalMetrics:  agentapi.SignalObservation{State: agentapi.SignalStateUnknown},
		Registrations:  []agentapi.RegistrationObservation{},
		CurrentNetwork: agentapi.NetworkObservation{SelectionMode: agentapi.NetworkSelectionUnknown},
	}
	if query == nil || readSIMPresence == nil || !validProbePlan(plan) {
		return probeFailure(result, agentapi.ProbeStateUnavailable, agentapi.ErrorLayerPlatform, agentapi.ErrorPlatformUnsupported, false, "AT probe plan is unavailable")
	}
	if lines, err := query(ctx, plan.Handshake, time.Second); err != nil || !attransport.HasTerminalOK(lines) {
		if ctx.Err() != nil {
			return probeFailure(result, agentapi.ProbeStateFailed, agentapi.ErrorLayerTransport, agentapi.ErrorProbeCancelled, true, "read-only probe was cancelled")
		}
		return probeFailure(result, agentapi.ProbeStateFailed, agentapi.ErrorLayerTransport, agentapi.ErrorModemNoResponse, true, "AT endpoint did not complete the bounded handshake")
	}
	read := func(command string) []string {
		if command == "" {
			return nil
		}
		lines, err := query(ctx, command, 1500*time.Millisecond)
		if err != nil {
			return nil
		}
		return lines
	}
	manufacturer := read(plan.Manufacturer)
	model := read(plan.Model)
	revision := read(plan.Revision)
	cfun := read(plan.RFState)
	sim, simErr := readSIMPresence(ctx, query)
	creg := read(plan.CSRegistration)
	cgreg := read(plan.PacketRegistration)
	cereg := read(plan.EPSRegistration)
	cops := read(plan.Operator)
	signal := read(plan.Signal)
	calls := read(plan.ActiveCalls)

	result.RF = RFObservation(cfun)
	result.RF.Signal = FirstPayload(signal, "+CSQ:")
	result.Identity = agentapi.ModemIdentity{
		Manufacturer: IdentityPayload(manufacturer), Model: IdentityPayload(model), Revision: IdentityPayload(revision),
	}
	result.SIM = sim
	result.SignalMetrics = signalObservation(signal)
	result.Registrations = registrationObservations(creg, cgreg, cereg)
	result.CurrentNetwork = networkObservation(cops, nil)
	if ctx.Err() != nil {
		return probeFailure(result, agentapi.ProbeStateFailed, agentapi.ErrorLayerTransport, agentapi.ErrorProbeCancelled, true, "read-only probe was cancelled")
	}
	if !attransport.HasTerminalOK(cfun) || result.RF.State == agentapi.RFStateUnknown {
		return probeFailure(result, agentapi.ProbeStateFailed, agentapi.ErrorLayerRadio, agentapi.ErrorRFStateUnavailable, true, "functional mode could not be read")
	}
	if simErr != nil {
		return probeFailure(result, agentapi.ProbeStateFailed, agentapi.ErrorLayerSIM, agentapi.ErrorSIMStateUnavailable, true, "SIM lock state could not be read")
	}
	if !attransport.HasTerminalOK(calls) {
		return probeFailure(result, agentapi.ProbeStateFailed, agentapi.ErrorLayerCall, agentapi.ErrorCallStateUnknown, true, "active call state could not be read")
	}
	count := ActiveCallCount(calls)
	result.State = agentapi.ProbeStateComplete
	result.ActiveCallCount = &count
	return result
}

func validProbePlan(plan ProbePlan) bool {
	commands := []string{
		plan.Handshake, plan.Manufacturer, plan.Model, plan.Revision, plan.RFState,
		plan.CSRegistration, plan.PacketRegistration, plan.EPSRegistration, plan.Operator, plan.Signal, plan.ActiveCalls,
	}
	for _, command := range commands {
		if strings.TrimSpace(command) == "" || strings.ContainsAny(command, "\r\n") {
			return false
		}
	}
	return true
}

func probeFailure(result agentapi.DeviceProbe, state, layer, code string, retryable bool, detail string) agentapi.DeviceProbe {
	result.State = state
	result.Error = &agentapi.ProbeError{Layer: layer, Code: code, Retryable: retryable}
	result.ErrorCode = code
	result.ErrorDetail = detail
	return result
}
