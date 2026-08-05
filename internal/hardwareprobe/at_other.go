//go:build !linux

package hardwareprobe

import (
	"context"

	"github.com/leonfox28/simplus/internal/agentapi"
)

type unsupportedATQuerier struct{}

func NewATQuerier() ModemQuerier { return unsupportedATQuerier{} }

func NewATQuerierWithIdentity(IdentityPseudonymizer) ModemQuerier { return unsupportedATQuerier{} }

func (unsupportedATQuerier) Probe(context.Context, string, string) agentapi.DeviceProbe {
	return agentapi.DeviceProbe{
		State:          agentapi.ProbeStateUnavailable,
		RF:             agentapi.RFObservation{State: agentapi.RFStateUnknown},
		SIM:            agentapi.SIMObservation{State: agentapi.SIMStateUnknown, PrimaryLockState: agentapi.PrimaryLockUnknown},
		SignalMetrics:  agentapi.SignalObservation{State: agentapi.SignalStateUnknown},
		Registrations:  []agentapi.RegistrationObservation{},
		CurrentNetwork: agentapi.NetworkObservation{SelectionMode: agentapi.NetworkSelectionUnknown},
		Error:          &agentapi.ProbeError{Layer: agentapi.ErrorLayerPlatform, Code: agentapi.ErrorPlatformUnsupported, Retryable: false},
		ErrorCode:      agentapi.ErrorPlatformUnsupported, ErrorDetail: "read-only AT probing is available only on Linux",
	}
}

func (unsupportedATQuerier) EnsureRadioOff(context.Context, string, string) agentapi.RadioEnsureOffExecution {
	return agentapi.RadioEnsureOffExecution{
		Observation: agentapi.RadioEnsureOffObservation{RF: agentapi.RFObservation{State: agentapi.RFStateUnknown}},
		Error: &agentapi.ProbeError{
			Layer: agentapi.ErrorLayerPlatform, Code: agentapi.ErrorPlatformUnsupported, Retryable: false,
		},
	}
}
