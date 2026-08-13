package modemadapter

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/attransport"
	"github.com/leonfox28/simplus/internal/modemadapter/standardat"
)

type QDC507 struct{}

// QDC507SMS is the evidence-promoted production model surface. The SMS
// implementation embeds it so discovery, identity/RF probing, and SMS all
// resolve through one registry entry without changing the safe default
// registry used before production composition.
type QDC507SMS struct{ QDC507 }

var (
	qdc507ModuleSerialSupportPattern = regexp.MustCompile(`^\+CGSN: *"sn,imei" *\(0,1\)$`)
	qdc507ModuleSerialPattern        = regexp.MustCompile(`^\+CGSN: *"([\x20-\x21\x23-\x7e]{1,128})" *$`)
	qdc507ParameterizedIMEIPattern   = regexp.MustCompile(`^\+CGSN: *"([0-9]{15})" *$`)
)

var (
	_ ATProbeAdapter           = QDC507{}
	_ SIMPresenceAdapter       = QDC507{}
	_ SIMIdentityAdapter       = QDC507{}
	_ ModuleSerialAdapter      = QDC507{}
	_ RFControlAdapter         = QDC507{}
	_ EquipmentIdentityAdapter = QDC507{}
	_ SMSCallSafetyAdapter     = QDC507SMS{}
	_ SMSCallSafetyAdapter     = QDC507{}
)

func (QDC507) Profile() string    { return agentapi.ProfileQDC507 }
func (QDC507SMS) Profile() string { return agentapi.ProfileQDC507 }

func (QDC507) DisplayName() string    { return "DJI/Baiwang QDC507" }
func (QDC507SMS) DisplayName() string { return "DJI/Baiwang QDC507" }

func (QDC507) Matches(descriptor USBDescriptor) bool {
	return matchesQDC507(descriptor)
}

func (QDC507SMS) Matches(descriptor USBDescriptor) bool { return matchesQDC507(descriptor) }

func matchesQDC507(descriptor USBDescriptor) bool {
	identity := normalizedUSBIdentity(descriptor)
	if identity == "2ca3:4006" {
		return true
	}
	if identity != "2c7c:0125" {
		return false
	}
	productText := strings.ToLower(descriptor.Manufacturer + " " + descriptor.Product)
	return strings.Contains(productText, "baiwang") || strings.Contains(productText, "qdc507")
}

func (QDC507) Endpoint(device agentapi.DeviceReport, role EndpointRole) (agentapi.Endpoint, bool) {
	switch role {
	case EndpointPrimaryAT:
		return endpoint(device, agentapi.EndpointTTY, 2)
	case EndpointQMI:
		return endpoint(device, agentapi.EndpointQMI, 4)
	default:
		return agentapi.Endpoint{}, false
	}
}

func (QDC507SMS) Endpoint(device agentapi.DeviceReport, role EndpointRole) (agentapi.Endpoint, bool) {
	return QDC507{}.Endpoint(device, role)
}

func (QDC507) ATProbePlan() (standardat.ProbePlan, bool) {
	return standardat.Standard3GPPProbePlan(), true
}

func (QDC507SMS) ATProbePlan() (standardat.ProbePlan, bool) {
	return standardat.Standard3GPPProbePlan(), true
}

func readQDC507SMSBlockingCallCount(ctx context.Context, query attransport.Query) (int, bool) {
	if query == nil {
		return 0, false
	}
	lines, err := query(ctx, "AT+CLCC", 1500*time.Millisecond)
	if err != nil {
		return 0, false
	}
	return standardat.ActiveNonDataCallCount(lines)
}

func (QDC507SMS) ReadSMSBlockingCallCount(ctx context.Context, query attransport.Query) (int, bool) {
	return readQDC507SMSBlockingCallCount(ctx, query)
}

func (QDC507) ReadSMSBlockingCallCount(ctx context.Context, query attransport.Query) (int, bool) {
	return readQDC507SMSBlockingCallCount(ctx, query)
}

func (QDC507SMS) Capabilities(device agentapi.DeviceReport) []agentapi.CapabilityEvidence {
	capabilities := QDC507{}.Capabilities(device)
	for index := range capabilities {
		if capabilities[index].Capability != "sms-control" {
			continue
		}
		if hasEndpoint(device, agentapi.EndpointTTY, 2) {
			capabilities[index].Status = agentapi.EvidenceObserved
			capabilities[index].Evidence = []string{"designated-SIM cellular SMS HIL accepted on the primary AT endpoint"}
		}
		break
	}
	return capabilities
}

func (QDC507) ReadSIMPresence(ctx context.Context, query attransport.Query) (agentapi.SIMObservation, error) {
	return readQDC507SIMPresence(ctx, query)
}

func readQDC507SIMPresence(ctx context.Context, query attransport.Query) (agentapi.SIMObservation, error) {
	unknown := agentapi.SIMObservation{State: agentapi.SIMStateUnknown, PrimaryLockState: agentapi.PrimaryLockUnknown}
	if query == nil {
		return unknown, errors.New("SIM presence is unavailable")
	}
	lockState, err := query(ctx, "AT+CPIN?", 1500*time.Millisecond)
	if err != nil || !attransport.HasTerminalResponse(lockState) {
		return unknown, errors.New("SIM presence query failed")
	}
	presenceState, _ := query(ctx, "AT+QSIMSTAT?", 1500*time.Millisecond)
	return standardat.SIMObservation(lockState, presenceState), nil
}

func (QDC507) ReadEquipmentIdentity(ctx context.Context, query attransport.Query) (string, error) {
	return readQDC507EquipmentIdentity(ctx, query)
}

func readQDC507EquipmentIdentity(ctx context.Context, query attransport.Query) (string, error) {
	if query == nil {
		return "", errors.New("equipment identity is unavailable")
	}
	lines, err := query(ctx, "AT+CGSN", 2*time.Second)
	if err != nil {
		return "", errors.New("equipment identity query failed")
	}
	imei := equipmentIMEI(lines)
	if imei == "" {
		return "", errors.New("equipment identity response is invalid")
	}
	return imei, nil
}

// ReadModuleSerial uses only the QDC507 firmware's advertised parameterized
// CGSN form. The parameter-1 IMEI is checked solely to prevent exposing an
// equipment identity as a module serial; it remains owned by the separate
// equipment-identity capability.
func (QDC507) ReadModuleSerial(ctx context.Context, query attransport.Query) (string, error) {
	if query == nil {
		return "", errors.New("module serial is unavailable")
	}
	support, err := query(ctx, "AT+CGSN=?", 2*time.Second)
	if err != nil || len(support) != 2 || !qdc507ModuleSerialSupportPattern.MatchString(support[0]) || support[1] != "OK" {
		return "", errors.New("module serial is unavailable")
	}
	serialLines, err := query(ctx, "AT+CGSN=0", 2*time.Second)
	if err != nil || len(serialLines) != 2 || serialLines[1] != "OK" {
		return "", errors.New("module serial is unavailable")
	}
	serialMatch := qdc507ModuleSerialPattern.FindStringSubmatch(serialLines[0])
	if len(serialMatch) != 2 || strings.Trim(serialMatch[1], " ") != serialMatch[1] {
		return "", errors.New("module serial is unavailable")
	}
	imeiLines, err := query(ctx, "AT+CGSN=1", 2*time.Second)
	if err != nil || len(imeiLines) != 2 || imeiLines[1] != "OK" {
		return "", errors.New("module serial is unavailable")
	}
	imeiMatch := qdc507ParameterizedIMEIPattern.FindStringSubmatch(imeiLines[0])
	if len(imeiMatch) != 2 || !validIMEICheckDigit(imeiMatch[1]) || serialMatch[1] == imeiMatch[1] {
		return "", errors.New("module serial is unavailable")
	}
	return serialMatch[1], nil
}

func (QDC507) ReadSIMIdentity(ctx context.Context, query attransport.Query, identities IdentityPseudonymizer) (SIMProfileIdentity, error) {
	return readQDC507SIMIdentity(ctx, query, identities)
}

func readQDC507BaseSIMIdentity(ctx context.Context, query attransport.Query, identities IdentityPseudonymizer) (SIMProfileIdentity, error) {
	if query == nil || identities == nil {
		return SIMProfileIdentity{}, errors.New("SIM identity is unavailable")
	}
	lines, err := query(ctx, "AT+QCCID", 2*time.Second)
	if err != nil {
		return SIMProfileIdentity{}, errors.New("SIM identity query failed")
	}
	fingerprint, hint := pseudonymizedICCID(lines, "+QCCID:", identities)
	if fingerprint == "" || hint == "" {
		return SIMProfileIdentity{}, errors.New("SIM identity response is invalid")
	}
	return SIMProfileIdentity{Fingerprint: fingerprint, DisplayHint: hint}, nil
}

func readQDC507SIMIdentity(ctx context.Context, query attransport.Query, identities IdentityPseudonymizer) (SIMProfileIdentity, error) {
	identity, err := readQDC507BaseSIMIdentity(ctx, query, identities)
	if err != nil {
		return SIMProfileIdentity{}, err
	}
	operatorName, operatorCode := readSIMHomeOperator(ctx, query)
	identity.HomeOperatorName = operatorName
	identity.HomeOperatorCode = operatorCode
	return identity, nil
}

func (QDC507) SetRFState(ctx context.Context, query attransport.Query, enabled bool) (agentapi.RFObservation, error) {
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
	_ = dispatchErr
	return observation, agentapi.ErrRFNotConfirmed
}

func (QDC507) Capabilities(device agentapi.DeviceReport) []agentapi.CapabilityEvidence {
	hasAT := hasEndpoint(device, agentapi.EndpointTTY, 2)
	hasQMI := hasEndpoint(device, agentapi.EndpointQMI, 4)
	hasUAC := hasEndpoint(device, agentapi.EndpointALSA, -1)
	result := make([]agentapi.CapabilityEvidence, 0, 11)
	add := func(capability, status string, evidence ...string) {
		result = append(result, agentapi.CapabilityEvidence{Capability: capability, Status: status, Evidence: evidence})
	}
	if hasAT {
		add("at-control", agentapi.EvidenceObserved, "known QDC507 interface 2 tty endpoint")
	} else {
		add("at-control", agentapi.EvidenceUnavailable, "expected AT endpoint not present")
	}
	if hasQMI {
		add("qmi-control", agentapi.EvidenceObserved, "known QDC507 cdc-wdm endpoint on interface 4")
	} else {
		add("qmi-control", agentapi.EvidenceUnavailable, "QMI endpoint not present")
	}
	if hasUAC {
		add("usb-uac", agentapi.EvidenceObserved, "snd-usb-audio endpoint")
	} else {
		add("usb-uac", agentapi.EvidenceUnavailable, "USB Audio endpoint not present")
	}
	if hasAT || hasQMI {
		if hasAT {
			add("rf-control", agentapi.EvidenceObserved, "fixed CFUN state transition is available on the primary AT endpoint with immediate read-back")
			add("sim-presence", agentapi.EvidenceObserved, "fixed CPIN and QSIMSTAT read-only status queries")
		} else {
			add("rf-control", agentapi.EvidenceUnavailable, "primary AT endpoint required for fixed RF control")
			add("sim-presence", agentapi.EvidenceUnavailable, "primary AT endpoint required for SIM presence queries")
		}
		if hasAT {
			add("sim-access", agentapi.EvidenceObserved, "fixed CPIN and QCCID identity queries available")
		} else {
			add("sim-access", agentapi.EvidenceUnavailable, "primary AT endpoint required for SIM access")
		}
		add("sms-control", agentapi.EvidenceDocumented, "requires designated-SIM HIL")
		add("operator-selection", agentapi.EvidenceDocumented, "requires RF-armed HIL")
	} else {
		addUnavailableControlCapabilities(add)
	}
	if hasUAC {
		add("digital-voice-media", agentapi.EvidenceUnverified, "UAC gadget observed; in-call media not yet accepted")
	} else {
		add("digital-voice-media", agentapi.EvidenceUnavailable, "UAC gadget not present")
	}
	add("host-vowifi-auth", agentapi.EvidenceUnverified, "APDU command surface documented; SIM AKA HIL pending")
	sortCapabilities(result)
	return result
}

func normalizedUSBIdentity(descriptor USBDescriptor) string {
	return strings.ToLower(strings.TrimSpace(descriptor.VendorID) + ":" + strings.TrimSpace(descriptor.ProductID))
}

func addUnavailableControlCapabilities(add func(string, string, ...string)) {
	add("rf-control", agentapi.EvidenceUnavailable, "no supported control endpoint")
	add("sim-presence", agentapi.EvidenceUnavailable, "no supported control endpoint")
	add("sim-access", agentapi.EvidenceUnavailable, "no supported control endpoint")
	add("sms-control", agentapi.EvidenceUnavailable, "no supported control endpoint")
	add("operator-selection", agentapi.EvidenceUnavailable, "no supported control endpoint")
}

func sortCapabilities(capabilities []agentapi.CapabilityEvidence) {
	sort.Slice(capabilities, func(left, right int) bool {
		return capabilities[left].Capability < capabilities[right].Capability
	})
}
