package agentapi

import (
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	simIdentityHintPattern  = regexp.MustCompile(`^ICCID •••• [0-9]{4}$`)
	homeOperatorCodePattern = regexp.MustCompile(`^[0-9]{3}-[0-9]{2,3}$`)
	subscriberNumberPattern = regexp.MustCompile(`^\+[1-9][0-9]{2,14}$`)
)

// validateProbeResponse validates the typed wire contract. ProbeStateComplete
// means the RF state, a terminal SIM query, and active-call count were read;
// registration and signal observations may still be unknown or unavailable.
func validateProbeResponse(response ProbeResponse) error {
	if response.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("unsupported probe protocol version %d", response.ProtocolVersion)
	}
	if !IsValidAgentInstanceID(response.AgentInstanceID) {
		return errors.New("invalid Agent instance id")
	}
	if response.SnapshotGeneration == 0 || !isSHA256Hex(response.SnapshotRevision) || response.ObservedAt.IsZero() {
		return errors.New("invalid probe snapshot envelope")
	}
	if response.Devices == nil {
		return errors.New("devices must be an array")
	}
	seenDevices := make(map[string]struct{}, len(response.Devices))
	for index, device := range response.Devices {
		if _, exists := seenDevices[device.DeviceID]; exists {
			return fmt.Errorf("device %d duplicates deviceId %q", index, device.DeviceID)
		}
		if err := validateDeviceProbe(device); err != nil {
			return fmt.Errorf("device %d: %w", index, err)
		}
		seenDevices[device.DeviceID] = struct{}{}
	}
	return nil
}

func validateDeviceProbe(device DeviceProbe) error {
	if strings.TrimSpace(device.DeviceID) == "" {
		return errors.New("deviceId is required")
	}
	if !oneOf(device.State, ProbeStateComplete, ProbeStateDescriptorOnly, ProbeStateBusy, ProbeStateUnavailable, ProbeStateFailed) {
		return fmt.Errorf("invalid probe state %q", device.State)
	}
	if !oneOf(device.RF.State, RFStateOff, RFStateMinimum, RFStateOn, RFStateUnknown) {
		return fmt.Errorf("invalid RF state %q", device.RF.State)
	}
	if device.Identity.EquipmentIdentityFingerprint != "" && !isSHA256Hex(device.Identity.EquipmentIdentityFingerprint) {
		return errors.New("equipment identity must be an instance-scoped fingerprint")
	}
	if !validOptionalProbeText(device.Identity.Manufacturer, 128) || !validOptionalProbeText(device.Identity.Model, 128) ||
		!validOptionalProbeText(device.Identity.Revision, 128) || !validOptionalProbeText(device.Identity.SerialNumber, 128) {
		return errors.New("modem identity text is invalid")
	}
	if !oneOf(device.SIM.State, SIMStatePresent, SIMStateAbsent, SIMStateLocked, SIMStateUnknown) {
		return fmt.Errorf("invalid SIM state %q", device.SIM.State)
	}
	if !oneOf(device.SIM.PrimaryLockState, PrimaryLockReady, PrimaryLockPIN1Required, PrimaryLockPUK1Required, PrimaryLockPermanentlyBlocked, PrimaryLockUnsupported, PrimaryLockUnknown) {
		return fmt.Errorf("invalid primary SIM lock state %q", device.SIM.PrimaryLockState)
	}
	if device.SIM.PrimaryLockState == PrimaryLockReady && device.SIM.State != SIMStatePresent {
		return errors.New("ready primary SIM lock requires a present SIM")
	}
	if oneOf(device.SIM.PrimaryLockState, PrimaryLockPIN1Required, PrimaryLockPUK1Required, PrimaryLockPermanentlyBlocked, PrimaryLockUnsupported) && device.SIM.State != SIMStateLocked {
		return errors.New("non-ready primary SIM lock requires a locked SIM")
	}
	if device.SIM.AttemptsRemaining == nil && device.SIM.AttemptsSource != "" {
		return errors.New("attemptsSource requires attemptsRemaining")
	}
	if device.SIM.AttemptsRemaining != nil && (*device.SIM.AttemptsRemaining < 0 || strings.TrimSpace(device.SIM.AttemptsSource) == "") {
		return errors.New("attemptsRemaining requires a nonnegative value and source")
	}
	if device.SIM.IdentityFingerprint == "" {
		if device.SIM.DisplayIdentityHint != "" || device.SIM.HomeOperatorName != "" || device.SIM.HomeOperatorCode != "" || device.SIM.SubscriberNumber != "" {
			return errors.New("SIM profile metadata requires an identity fingerprint")
		}
	} else if !isSHA256Hex(device.SIM.IdentityFingerprint) || !simIdentityHintPattern.MatchString(device.SIM.DisplayIdentityHint) ||
		device.SIM.State != SIMStatePresent || device.SIM.PrimaryLockState != PrimaryLockReady {
		return errors.New("SIM identity requires a ready present SIM, keyed fingerprint, and masked hint")
	}
	if !validOptionalProbeText(device.SIM.HomeOperatorName, 64) ||
		(device.SIM.HomeOperatorCode != "" && !homeOperatorCodePattern.MatchString(device.SIM.HomeOperatorCode)) {
		return errors.New("SIM home operator metadata is invalid")
	}
	if device.SIM.SubscriberNumber != "" && !subscriberNumberPattern.MatchString(device.SIM.SubscriberNumber) {
		return errors.New("SIM subscriber number is invalid")
	}
	if !oneOf(device.SignalMetrics.State, SignalStateMeasured, SignalStateUnavailable, SignalStateUnknown) {
		return fmt.Errorf("invalid signal state %q", device.SignalMetrics.State)
	}
	if device.SignalMetrics.RSSI != nil && (*device.SignalMetrics.RSSI < 0 || *device.SignalMetrics.RSSI > 31) {
		return errors.New("RSSI must be from 0 through 31")
	}
	if device.SignalMetrics.RSSIDBm != nil && (*device.SignalMetrics.RSSIDBm < -113 || *device.SignalMetrics.RSSIDBm > -51) {
		return errors.New("rssiDbm must be from -113 through -51")
	}
	if device.SignalMetrics.BER != nil && (*device.SignalMetrics.BER < 0 || *device.SignalMetrics.BER > 7) {
		return errors.New("BER must be from 0 through 7")
	}
	if device.SignalMetrics.State == SignalStateMeasured && (device.SignalMetrics.RSSI == nil || device.SignalMetrics.RSSIDBm == nil || strings.TrimSpace(device.SignalMetrics.Source) == "") {
		return errors.New("measured signal requires RSSI, rssiDbm, and source")
	}
	if device.SignalMetrics.State == SignalStateUnavailable && strings.TrimSpace(device.SignalMetrics.Source) == "" {
		return errors.New("unavailable signal requires a source")
	}
	if device.Registrations == nil {
		return errors.New("registrations must be an array")
	}
	seenDomains := make(map[string]struct{}, len(device.Registrations))
	for index, registration := range device.Registrations {
		if !oneOf(registration.Domain, RegistrationDomainCS, RegistrationDomainPacket, RegistrationDomainEPS) {
			return fmt.Errorf("registration %d has invalid domain %q", index, registration.Domain)
		}
		if !oneOf(registration.State, RegistrationNotRegistered, RegistrationSearching, RegistrationDenied, RegistrationRegisteredHome,
			RegistrationRegisteredRoaming, RegistrationRegisteredSMSHome, RegistrationRegisteredSMSRoaming, RegistrationEmergencyOnly,
			RegistrationHomeCSFBNotPreferred, RegistrationRoamingCSFBNotPreferred, RegistrationUnknown) {
			return fmt.Errorf("registration %d has invalid state %q", index, registration.State)
		}
		if strings.TrimSpace(registration.Source) == "" {
			return fmt.Errorf("registration %d requires a source", index)
		}
		if _, exists := seenDomains[registration.Domain]; exists {
			return fmt.Errorf("registration %d duplicates domain %q", index, registration.Domain)
		}
		seenDomains[registration.Domain] = struct{}{}
	}
	if !oneOf(device.CurrentNetwork.SelectionMode, NetworkSelectionAutomatic, NetworkSelectionManual, NetworkSelectionDeregistered, NetworkSelectionManualAutomatic, NetworkSelectionUnknown) {
		return fmt.Errorf("invalid network selection mode %q", device.CurrentNetwork.SelectionMode)
	}
	if device.CurrentNetwork.PLMN != "" && !isPLMN(device.CurrentNetwork.PLMN) {
		return errors.New("PLMN must contain five or six digits")
	}
	if device.CurrentNetwork.RAT != "" && !oneOf(device.CurrentNetwork.RAT, RATGSM, RATGSMCompact, RATUTRAN, RATGSMEdge,
		RATUTRANHSDPA, RATUTRANHSUPA, RATUTRANHSPA, RATLTE, RATECGSMIoT, RATNBIoT, RATLTE5GC, RATNR5GC,
		RATNGRAN, RATNR, RATCDMA) {
		return fmt.Errorf("invalid RAT %q", device.CurrentNetwork.RAT)
	}
	if device.ActiveCallCount != nil && *device.ActiveCallCount < 0 {
		return errors.New("activeCallCount must be nonnegative")
	}
	if device.Error == nil {
		if oneOf(device.State, ProbeStateBusy, ProbeStateUnavailable, ProbeStateFailed) {
			return errors.New("non-terminal probe state requires a typed error")
		}
		if device.ErrorCode != "" {
			return errors.New("legacy errorCode requires a typed error")
		}
	} else {
		if oneOf(device.State, ProbeStateComplete, ProbeStateDescriptorOnly) {
			return errors.New("successful or descriptor-only probe must not contain an error")
		}
		if !oneOf(device.Error.Layer, ErrorLayerPlatform, ErrorLayerDevice, ErrorLayerTransport, ErrorLayerRadio, ErrorLayerSIM, ErrorLayerCall) {
			return fmt.Errorf("invalid error layer %q", device.Error.Layer)
		}
		if !oneOf(device.Error.Code, ErrorPlatformUnsupported, ErrorControlEndpointBusy, ErrorControlEndpointMissing, ErrorControlPermissionDenied,
			ErrorControlEndpointOpen, ErrorControlEndpointConfigure, ErrorModemNoResponse, ErrorProbeCancelled,
			ErrorRFStateUnavailable, ErrorSIMStateUnavailable, ErrorCallStateUnknown) {
			return fmt.Errorf("invalid error code %q", device.Error.Code)
		}
		if device.ErrorCode != "" && device.ErrorCode != device.Error.Code {
			return errors.New("legacy errorCode conflicts with typed error code")
		}
	}
	if device.State == ProbeStateComplete && (device.RF.State == RFStateUnknown || device.ActiveCallCount == nil) {
		return errors.New("complete probe requires a known RF state and activeCallCount")
	}
	return nil
}

func validOptionalProbeText(value string, limit int) bool {
	if value == "" {
		return true
	}
	if len(value) > limit || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func isSHA256Hex(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func isPLMN(value string) bool {
	if len(value) != 5 && len(value) != 6 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func validateRadioEnsureOffResponse(response RadioEnsureOffResponse, operationID string) error {
	if response.ProtocolVersion != ProtocolVersion || !IsValidAgentInstanceID(response.AgentInstanceID) {
		return errors.New("invalid command response envelope")
	}
	outcome := response.Outcome
	if outcome.OperationID == "" || outcome.OperationID != operationID || outcome.Command != CommandRadioEnsureOff ||
		!oneOf(outcome.State, CommandOutcomeSucceeded, CommandOutcomeFailed, CommandOutcomeUncertain) ||
		outcome.Code == "" || outcome.AcceptedAt.IsZero() || outcome.CompletedAt == nil || outcome.CompletedAt.Before(outcome.AcceptedAt) {
		return errors.New("invalid command outcome")
	}
	if !oneOf(outcome.Observation.RF.State, RFStateOff, RFStateMinimum, RFStateOn, RFStateUnknown) {
		return errors.New("invalid command RF observation")
	}
	if outcome.Observation.ActiveCallCount != nil && *outcome.Observation.ActiveCallCount < 0 {
		return errors.New("invalid command active-call count")
	}
	if outcome.State == CommandOutcomeSucceeded &&
		(outcome.Code != OutcomeCodeRadioOffConfirmed || outcome.ErrorLayer != "" || outcome.Observation.RF.State != RFStateOff ||
			outcome.Observation.ActiveCallCount == nil || *outcome.Observation.ActiveCallCount != 0) {
		return errors.New("successful command outcome is not confirmed RF-off")
	}
	if outcome.State != CommandOutcomeSucceeded && !oneOf(outcome.ErrorLayer, ErrorLayerPlatform, ErrorLayerDevice, ErrorLayerTransport, ErrorLayerRadio, ErrorLayerSIM, ErrorLayerCall) {
		return errors.New("failed command outcome requires an error layer")
	}
	if outcome.State != CommandOutcomeSucceeded && !oneOf(outcome.Code,
		ErrorPlatformUnsupported, ErrorControlEndpointBusy, ErrorControlEndpointMissing, ErrorControlPermissionDenied,
		ErrorControlEndpointOpen, ErrorControlEndpointConfigure, ErrorModemNoResponse, ErrorProbeCancelled,
		ErrorRFStateUnavailable, ErrorCallStateUnknown, ErrorActiveCallPresent, ErrorRadioOffCommandRejected,
		ErrorRadioOffNotConfirmed, ErrorRadioOffOutcomeUncertain, ErrorHardwareChanged) {
		return errors.New("failed command outcome has an unknown code")
	}
	if outcome.State == CommandOutcomeUncertain && outcome.Retryable {
		return errors.New("uncertain command outcome must not request blind retry")
	}
	return nil
}
