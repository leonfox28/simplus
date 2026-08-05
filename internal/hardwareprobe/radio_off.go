package hardwareprobe

import (
	"context"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
)

type boundedATQuery func(context.Context, string, time.Duration) ([]string, error)

func ensureRadioOffWithQuery(ctx context.Context, query boundedATQuery) agentapi.RadioEnsureOffExecution {
	result := agentapi.RadioEnsureOffExecution{
		Observation: agentapi.RadioEnsureOffObservation{RF: agentapi.RFObservation{State: agentapi.RFStateUnknown}},
	}
	if lines, err := query(ctx, "AT", time.Second); err != nil || !hasTerminalOK(lines) {
		if ctx.Err() != nil {
			result.Error = &agentapi.ProbeError{Layer: agentapi.ErrorLayerTransport, Code: agentapi.ErrorProbeCancelled, Retryable: true}
		} else {
			result.Error = &agentapi.ProbeError{Layer: agentapi.ErrorLayerTransport, Code: agentapi.ErrorModemNoResponse, Retryable: true}
		}
		return result
	}
	calls, err := query(ctx, "AT+CLCC", 1500*time.Millisecond)
	if err != nil || !hasTerminalOK(calls) {
		result.Error = &agentapi.ProbeError{Layer: agentapi.ErrorLayerCall, Code: agentapi.ErrorCallStateUnknown, Retryable: true}
		return result
	}
	count := activeCallCount(calls)
	result.Observation.ActiveCallCount = &count
	if count != 0 {
		result.Error = &agentapi.ProbeError{Layer: agentapi.ErrorLayerCall, Code: agentapi.ErrorActiveCallPresent, Retryable: true}
		return result
	}
	before, err := query(ctx, "AT+CFUN?", 1500*time.Millisecond)
	if err != nil || !hasTerminalOK(before) {
		result.Error = &agentapi.ProbeError{Layer: agentapi.ErrorLayerRadio, Code: agentapi.ErrorRFStateUnavailable, Retryable: true}
		return result
	}
	result.Observation.RF = rfObservation(before)
	if result.Observation.RF.State == agentapi.RFStateUnknown && result.Observation.RF.Mode == nil {
		result.Error = &agentapi.ProbeError{Layer: agentapi.ErrorLayerRadio, Code: agentapi.ErrorRFStateUnavailable, Retryable: true}
		return result
	}
	if result.Observation.RF.State == agentapi.RFStateOff {
		return result
	}

	// Once dispatch begins, a missing response is uncertain and must only be reconciled by reading CFUN again.
	result.Dispatched = true
	dispatch, err := query(ctx, "AT+CFUN=4", 5*time.Second)
	if err != nil {
		result.Uncertain = true
		result.Error = &agentapi.ProbeError{Layer: agentapi.ErrorLayerRadio, Code: agentapi.ErrorRadioOffOutcomeUncertain}
		return result
	}
	if !hasTerminalOK(dispatch) {
		result.Error = &agentapi.ProbeError{Layer: agentapi.ErrorLayerRadio, Code: agentapi.ErrorRadioOffCommandRejected}
		return result
	}
	after, err := query(ctx, "AT+CFUN?", 3*time.Second)
	if err != nil || !hasTerminalOK(after) {
		result.Uncertain = true
		result.Error = &agentapi.ProbeError{Layer: agentapi.ErrorLayerRadio, Code: agentapi.ErrorRadioOffOutcomeUncertain}
		return result
	}
	result.Observation.RF = rfObservation(after)
	if result.Observation.RF.State != agentapi.RFStateOff {
		result.Error = &agentapi.ProbeError{Layer: agentapi.ErrorLayerRadio, Code: agentapi.ErrorRadioOffNotConfirmed, Retryable: true}
		return result
	}
	return result
}
