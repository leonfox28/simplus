package modemadapter

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/attransport"
	"github.com/leonfox28/simplus/internal/modemadapter/standardat"
)

type ML307A struct{}

var (
	_ SIMAuthAdapter           = ML307A{}
	_ ATProbeAdapter           = ML307A{}
	_ SIMPresenceAdapter       = ML307A{}
	_ SIMIdentityAdapter       = ML307A{}
	_ RFControlAdapter         = ML307A{}
	_ EquipmentIdentityAdapter = ML307A{}
	_ USBSerialBindingAdapter  = ML307A{}
)

func (ML307A) Profile() string { return agentapi.ProfileML307A }

func (ML307A) DisplayName() string { return "China Mobile IoT ML307A" }

func (ML307A) USBSerialIDs() []USBSerialID {
	return []USBSerialID{{VendorID: "2ecc", ProductID: "3012"}}
}

func (ML307A) Matches(descriptor USBDescriptor) bool {
	if normalizedUSBIdentity(descriptor) != "2ecc:3012" {
		return false
	}
	productText := strings.ToLower(descriptor.Manufacturer + " " + descriptor.Product)
	return strings.Contains(productText, "ml307") || strings.Contains(productText, "cmiot")
}

func (ML307A) Endpoint(device agentapi.DeviceReport, role EndpointRole) (agentapi.Endpoint, bool) {
	if role == EndpointPrimaryAT {
		return endpoint(device, agentapi.EndpointTTY, 2)
	}
	return agentapi.Endpoint{}, false
}

func (adapter ML307A) SIMAuthEndpoint(device agentapi.DeviceReport) (agentapi.Endpoint, bool) {
	return adapter.Endpoint(device, EndpointPrimaryAT)
}

func (ML307A) ATProbePlan() (standardat.ProbePlan, bool) {
	return standardat.Standard3GPPProbePlan(), true
}

func (ML307A) ReadSIMPresence(ctx context.Context, query attransport.Query) (agentapi.SIMObservation, error) {
	unknown := agentapi.SIMObservation{State: agentapi.SIMStateUnknown, PrimaryLockState: agentapi.PrimaryLockUnknown}
	if query == nil {
		return unknown, errors.New("SIM presence is unavailable")
	}
	lines, err := query(ctx, "AT+CPIN?", 1500*time.Millisecond)
	if err != nil || !attransport.HasTerminalResponse(lines) {
		return unknown, errors.New("SIM presence query failed")
	}
	return standardat.SIMObservation(lines, nil), nil
}

func (ML307A) ReadEquipmentIdentity(ctx context.Context, query attransport.Query) (string, error) {
	if query == nil {
		return "", errors.New("equipment identity is unavailable")
	}
	lines, err := query(ctx, "AT+CGSN=1", 2*time.Second)
	if err != nil {
		return "", errors.New("equipment identity query failed")
	}
	imei := equipmentIMEI(lines)
	if imei == "" {
		return "", errors.New("equipment identity response is invalid")
	}
	return imei, nil
}

func (ML307A) ReadSIMIdentity(ctx context.Context, query attransport.Query, identities IdentityPseudonymizer) (SIMProfileIdentity, error) {
	if query == nil || identities == nil {
		return SIMProfileIdentity{}, errors.New("SIM identity is unavailable")
	}
	lines, err := query(ctx, "AT+MCCID", 2*time.Second)
	if err != nil {
		return SIMProfileIdentity{}, errors.New("SIM identity query failed")
	}
	fingerprint, hint := pseudonymizedICCID(lines, "+MCCID:", identities)
	if fingerprint == "" || hint == "" {
		return SIMProfileIdentity{}, errors.New("SIM identity response is invalid")
	}
	operatorName, operatorCode := readML307AHomeOperator(ctx, query)
	return SIMProfileIdentity{
		Fingerprint: fingerprint, DisplayHint: hint,
		HomeOperatorName: operatorName, HomeOperatorCode: operatorCode,
	}, nil
}

func (ML307A) SetRFState(ctx context.Context, query attransport.Query, enabled bool) (agentapi.RFObservation, error) {
	unknown := agentapi.RFObservation{State: agentapi.RFStateUnknown}
	if query == nil {
		return unknown, agentapi.ErrRFUnsupported
	}
	if lines, err := query(ctx, "AT", time.Second); err != nil || !attransport.HasTerminalOK(lines) {
		return unknown, agentapi.ErrRFUnavailable
	}
	command := "AT+CFUN=4"
	expected := agentapi.RFStateOff
	if enabled {
		command = "AT+CFUN=1"
		expected = agentapi.RFStateOn
	}
	_, dispatchErr := query(ctx, command, 12*time.Second)
	after, readErr := query(ctx, "AT+CFUN?", 3*time.Second)
	if readErr != nil || !attransport.HasTerminalOK(after) {
		return unknown, agentapi.ErrRFNotConfirmed
	}
	observation := standardat.RFObservation(after)
	if observation.State == expected {
		return observation, nil
	}
	if dispatchErr != nil {
		return observation, agentapi.ErrRFNotConfirmed
	}
	return observation, agentapi.ErrRFNotConfirmed
}

func (ML307A) Capabilities(device agentapi.DeviceReport) []agentapi.CapabilityEvidence {
	hasAT := hasEndpoint(device, agentapi.EndpointTTY, 2)
	hasUAC := hasEndpoint(device, agentapi.EndpointALSA, -1)
	result := make([]agentapi.CapabilityEvidence, 0, 12)
	add := func(capability, status string, evidence ...string) {
		result = append(result, agentapi.CapabilityEvidence{Capability: capability, Status: status, Evidence: evidence})
	}
	if hasAT {
		add("at-control", agentapi.EvidenceObserved, "official ML307A interface 2 mapping confirmed by bounded HIL")
		add("rf-control", agentapi.EvidenceObserved, "CFUN state observation is verified; typed writes require explicit user confirmation and read-back")
		add("sim-presence", agentapi.EvidenceObserved, "fixed read-only CPIN status query distinguishes present, locked and absent SIM states")
		add("sim-access", agentapi.EvidenceObserved, "CPIN read-back confirmed SIM ready")
		add("sim-apdu", agentapi.EvidenceObserved, "CRSM/CSIM/CCHO/CGLA forms and SIM AKA exchange accepted by bounded HIL")
		add("sim-auth", agentapi.EvidenceObserved, "SIM identity, IMS identity and AKA challenge exchange accepted by bounded HIL")
		add("host-vowifi-auth", agentapi.EvidenceObserved, "VOXI SIM AKA, ePDG and protected IMS REGISTER accepted by bounded HIL")
		add("sms-control", agentapi.EvidenceUnverified, "requires designated-SIM HIL")
		add("operator-selection", agentapi.EvidenceUnverified, "requires RF-armed HIL")
	} else {
		add("at-control", agentapi.EvidenceUnavailable, "expected interface 2 AT endpoint not present")
		addUnavailableControlCapabilities(add)
		add("sim-apdu", agentapi.EvidenceUnavailable, "no supported control endpoint")
		add("sim-auth", agentapi.EvidenceUnavailable, "no supported control endpoint")
		add("host-vowifi-auth", agentapi.EvidenceUnavailable, "no supported control endpoint")
	}
	add("qmi-control", agentapi.EvidenceUnsupported, "purchased ML307A exposes RNDIS and AT, not a verified QMI control path")
	if hasUAC {
		add("usb-uac", agentapi.EvidenceObserved, "snd-usb-audio endpoint")
	} else {
		add("usb-uac", agentapi.EvidenceUnsupported, "purchased ML307A exposes no supported digital media path")
	}
	add("digital-voice-media", agentapi.EvidenceUnsupported, "analog media paths are excluded by Simplus")
	sortCapabilities(result)
	return result
}
