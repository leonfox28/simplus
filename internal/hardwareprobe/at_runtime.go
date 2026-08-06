package hardwareprobe

import (
	"context"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/attransport"
	"github.com/leonfox28/simplus/internal/modemadapter"
	"github.com/leonfox28/simplus/internal/modemadapter/standardat"
)

type atRuntime struct {
	opener     attransport.Opener
	identities modemadapter.IdentityPseudonymizer
}

func NewATQuerier() ModemQuerier {
	return atRuntime{opener: attransport.NewOpener()}
}

func NewATQuerierWithIdentity(pseudonymizer IdentityPseudonymizer) ModemQuerier {
	return atRuntime{opener: attransport.NewOpener(), identities: pseudonymizer}
}

func (runtime atRuntime) Probe(ctx context.Context, endpoint string, adapter modemadapter.Adapter) agentapi.DeviceProbe {
	result := emptyProbe(endpoint)
	if runtime.opener == nil || adapter == nil {
		return probeFailure(result, agentapi.ProbeStateUnavailable, agentapi.ErrorLayerPlatform, agentapi.ErrorPlatformUnsupported, false, "AT runtime is unavailable")
	}
	session, err := runtime.opener.Open(endpoint)
	if err != nil {
		return probeOpenFailure(result, err)
	}
	defer session.Close()
	query := attransport.Query(session.Query)
	probeAdapter, ok := adapter.(modemadapter.ATProbeAdapter)
	if !ok {
		return probeFailure(result, agentapi.ProbeStateUnavailable, agentapi.ErrorLayerPlatform, agentapi.ErrorPlatformUnsupported, false, "adapter has no AT probe capability")
	}
	plan, ok := probeAdapter.ATProbePlan()
	if !ok {
		return probeFailure(result, agentapi.ProbeStateUnavailable, agentapi.ErrorLayerPlatform, agentapi.ErrorPlatformUnsupported, false, "adapter AT probe is unavailable")
	}
	presenceAdapter, ok := adapter.(modemadapter.SIMPresenceAdapter)
	if !ok {
		return probeFailure(result, agentapi.ProbeStateUnavailable, agentapi.ErrorLayerPlatform, agentapi.ErrorPlatformUnsupported, false, "adapter has no SIM presence capability")
	}
	result = standardat.ExecuteProbe(ctx, query, plan, presenceAdapter.ReadSIMPresence)
	result.Endpoint = endpoint
	if identityAdapter, supported := adapter.(modemadapter.EquipmentIdentityAdapter); supported {
		if imei, identityErr := identityAdapter.ReadEquipmentIdentity(ctx, query); identityErr == nil && runtime.identities != nil {
			if fingerprint, fingerprintErr := runtime.identities.Pseudonym("modem-imei-v1", []byte(imei)); fingerprintErr == nil && fingerprintPattern.MatchString(fingerprint) {
				result.Identity.EquipmentIdentityFingerprint = fingerprint
			}
		}
	}
	if result.SIM.State == agentapi.SIMStatePresent && result.SIM.PrimaryLockState == agentapi.PrimaryLockReady {
		if identityAdapter, supported := adapter.(modemadapter.SIMIdentityAdapter); supported {
			if identity, identityErr := identityAdapter.ReadSIMIdentity(ctx, query, runtime.identities); identityErr == nil {
				result.SIM.IdentityFingerprint = identity.Fingerprint
				result.SIM.DisplayIdentityHint = identity.DisplayHint
				result.SIM.HomeOperatorName = identity.HomeOperatorName
				result.SIM.HomeOperatorCode = identity.HomeOperatorCode
			}
		}
	}
	return result
}

func (runtime atRuntime) ReadEquipmentIdentity(ctx context.Context, endpoint string, adapter modemadapter.EquipmentIdentityAdapter) (agentapi.EquipmentIdentityObservation, error) {
	if runtime.opener == nil || runtime.identities == nil || adapter == nil {
		return agentapi.EquipmentIdentityObservation{}, agentapi.ErrEquipmentIdentityUnavailable
	}
	session, err := runtime.opener.Open(endpoint)
	if err != nil {
		return agentapi.EquipmentIdentityObservation{}, agentapi.ErrEquipmentIdentityUnavailable
	}
	defer session.Close()
	imei, err := adapter.ReadEquipmentIdentity(ctx, session.Query)
	if err != nil {
		return agentapi.EquipmentIdentityObservation{}, agentapi.ErrEquipmentIdentityUnavailable
	}
	fingerprint, err := runtime.identities.Pseudonym("modem-imei-v1", []byte(imei))
	if err != nil || !fingerprintPattern.MatchString(fingerprint) {
		return agentapi.EquipmentIdentityObservation{}, agentapi.ErrEquipmentIdentityUnavailable
	}
	return agentapi.EquipmentIdentityObservation{IMEI: imei, Fingerprint: fingerprint}, nil
}

func (runtime atRuntime) ReadSIMAKAIdentity(ctx context.Context, endpoint string, adapter modemadapter.SIMAuthAdapter, identityFingerprint string) (string, error) {
	session, err := runtime.openSIMAuthSession(endpoint, adapter)
	if err != nil {
		return "", err
	}
	defer session.Close()
	return adapter.ReadSIMAKAIdentity(ctx, session.Query, runtime.identities, identityFingerprint)
}

func (runtime atRuntime) AuthenticateSIMAKA(ctx context.Context, endpoint string, adapter modemadapter.SIMAuthAdapter, identityFingerprint string, challenge agentapi.SIMAKAChallenge) (agentapi.SIMAKAExecution, error) {
	session, err := runtime.openSIMAuthSession(endpoint, adapter)
	if err != nil {
		return agentapi.SIMAKAExecution{}, err
	}
	defer session.Close()
	return adapter.AuthenticateSIMAKA(ctx, session.Query, runtime.identities, identityFingerprint, challenge)
}

func (runtime atRuntime) ProbeSIMIMSProfile(ctx context.Context, endpoint string, adapter modemadapter.SIMAuthAdapter, identityFingerprint string) (bool, error) {
	session, err := runtime.openSIMAuthSession(endpoint, adapter)
	if err != nil {
		return false, err
	}
	defer session.Close()
	return adapter.ProbeSIMIMSProfile(ctx, session.Query, runtime.identities, identityFingerprint)
}

func (runtime atRuntime) ReadSIMIMSIdentity(ctx context.Context, endpoint string, adapter modemadapter.SIMAuthAdapter, identityFingerprint string) (agentapi.SIMIMSIdentityMaterial, error) {
	session, err := runtime.openSIMAuthSession(endpoint, adapter)
	if err != nil {
		return agentapi.SIMIMSIdentityMaterial{}, err
	}
	defer session.Close()
	return adapter.ReadSIMIMSIdentity(ctx, session.Query, runtime.identities, identityFingerprint)
}

func (runtime atRuntime) openSIMAuthSession(endpoint string, adapter modemadapter.SIMAuthAdapter) (attransport.Session, error) {
	if runtime.opener == nil || adapter == nil {
		return nil, agentapi.ErrSIMAKAUnsupported
	}
	session, err := runtime.opener.Open(endpoint)
	if err != nil {
		return nil, agentapi.ErrSIMAKAUnavailable
	}
	return session, nil
}

func (runtime atRuntime) SetRFState(ctx context.Context, endpoint string, adapter modemadapter.RFControlAdapter, enabled bool) (agentapi.RFObservation, error) {
	unknown := agentapi.RFObservation{State: agentapi.RFStateUnknown}
	if runtime.opener == nil || adapter == nil {
		return unknown, agentapi.ErrRFUnsupported
	}
	session, err := runtime.opener.Open(endpoint)
	if err != nil {
		return unknown, agentapi.ErrRFUnavailable
	}
	defer session.Close()
	return adapter.SetRFState(ctx, session.Query, enabled)
}

func emptyProbe(endpoint string) agentapi.DeviceProbe {
	return agentapi.DeviceProbe{
		State: agentapi.ProbeStateUnavailable, Endpoint: endpoint,
		RF:             agentapi.RFObservation{State: agentapi.RFStateUnknown},
		SIM:            agentapi.SIMObservation{State: agentapi.SIMStateUnknown, PrimaryLockState: agentapi.PrimaryLockUnknown},
		SignalMetrics:  agentapi.SignalObservation{State: agentapi.SignalStateUnknown},
		Registrations:  []agentapi.RegistrationObservation{},
		CurrentNetwork: agentapi.NetworkObservation{SelectionMode: agentapi.NetworkSelectionUnknown},
	}
}

func probeOpenFailure(result agentapi.DeviceProbe, err error) agentapi.DeviceProbe {
	kind, retryable, ok := attransport.OpenFailure(err)
	if !ok {
		return probeFailure(result, agentapi.ProbeStateUnavailable, agentapi.ErrorLayerTransport, agentapi.ErrorControlEndpointOpen, true, "AT endpoint could not be opened")
	}
	switch kind {
	case attransport.OpenBusy:
		return probeFailure(result, agentapi.ProbeStateBusy, agentapi.ErrorLayerTransport, agentapi.ErrorControlEndpointBusy, retryable, "AT endpoint is already open")
	case attransport.OpenPermission:
		return probeFailure(result, agentapi.ProbeStateUnavailable, agentapi.ErrorLayerDevice, agentapi.ErrorControlPermissionDenied, retryable, "AT endpoint permission was denied")
	case attransport.OpenConfigure:
		return probeFailure(result, agentapi.ProbeStateFailed, agentapi.ErrorLayerTransport, agentapi.ErrorControlEndpointConfigure, retryable, "AT endpoint could not be configured")
	case attransport.OpenUnsupported:
		return probeFailure(result, agentapi.ProbeStateUnavailable, agentapi.ErrorLayerPlatform, agentapi.ErrorPlatformUnsupported, false, "AT transport is unavailable on this platform")
	default:
		return probeFailure(result, agentapi.ProbeStateUnavailable, agentapi.ErrorLayerDevice, agentapi.ErrorControlEndpointOpen, retryable, "AT endpoint could not be opened")
	}
}

func probeFailure(result agentapi.DeviceProbe, state, layer, code string, retryable bool, detail string) agentapi.DeviceProbe {
	result.State = state
	result.Error = &agentapi.ProbeError{Layer: layer, Code: code, Retryable: retryable}
	result.ErrorCode = code
	result.ErrorDetail = detail
	return result
}
