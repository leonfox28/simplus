package agentapi

const (
	CellularRegisteredHome    = "registered-home"
	CellularRegisteredRoaming = "registered-roaming"
	CellularSearching         = "searching"
	CellularDenied            = "denied"
	CellularNotRegistered     = "not-registered"
	CellularRFOff             = "rf-off"
	CellularSIMNotReady       = "sim-not-ready"
	CellularUnavailable       = "unavailable"
	CellularUnknown           = "unknown"
)

const (
	ErrorCellularStatusUnavailable  = "CELLULAR_STATUS_UNAVAILABLE"
	ErrorCellularSIMNotReady        = "CELLULAR_SIM_NOT_READY"
	ErrorCellularRFOff              = "CELLULAR_RF_OFF"
	ErrorCellularRegistrationDenied = "CELLULAR_REGISTRATION_DENIED"
	ErrorCellularNotRegistered      = "CELLULAR_NOT_REGISTERED"
)

type CellularClassification struct {
	State       string
	ReadyForSMS bool
	ReasonCode  string
}

// ClassifyCellular is the single transport-neutral readiness classifier used
// by status presentation and SMS preflight. It performs no modem I/O.
func ClassifyCellular(probe DeviceProbe) CellularClassification {
	if probe.State != ProbeStateComplete || !validRegistrationSet(probe.Registrations) {
		return CellularClassification{State: CellularUnavailable, ReasonCode: ErrorCellularStatusUnavailable}
	}
	if probe.SIM.State != SIMStatePresent || probe.SIM.PrimaryLockState != PrimaryLockReady {
		return CellularClassification{State: CellularSIMNotReady, ReasonCode: ErrorCellularSIMNotReady}
	}
	if probe.RF.State != RFStateOn {
		return CellularClassification{State: CellularRFOff, ReasonCode: ErrorCellularRFOff}
	}
	if anyRegistration(probe.Registrations, RegistrationRegisteredHome, RegistrationRegisteredSMSHome, RegistrationHomeCSFBNotPreferred) {
		return CellularClassification{State: CellularRegisteredHome, ReadyForSMS: true}
	}
	if anyRegistration(probe.Registrations, RegistrationRegisteredRoaming, RegistrationRegisteredSMSRoaming, RegistrationRoamingCSFBNotPreferred) {
		return CellularClassification{State: CellularRegisteredRoaming, ReadyForSMS: true}
	}
	if anyRegistration(probe.Registrations, RegistrationDenied) {
		return CellularClassification{State: CellularDenied, ReasonCode: ErrorCellularRegistrationDenied}
	}
	if anyRegistration(probe.Registrations, RegistrationSearching) {
		return CellularClassification{State: CellularSearching, ReasonCode: ErrorCellularNotRegistered}
	}
	if anyRegistration(probe.Registrations, RegistrationNotRegistered, RegistrationEmergencyOnly) {
		return CellularClassification{State: CellularNotRegistered, ReasonCode: ErrorCellularNotRegistered}
	}
	return CellularClassification{State: CellularUnknown, ReasonCode: ErrorCellularStatusUnavailable}
}

func validRegistrationSet(registrations []RegistrationObservation) bool {
	if len(registrations) != 3 {
		return false
	}
	seen := map[string]bool{}
	for _, registration := range registrations {
		if registration.Domain != RegistrationDomainCS && registration.Domain != RegistrationDomainPacket && registration.Domain != RegistrationDomainEPS ||
			!validRegistrationState(registration.State) || seen[registration.Domain] {
			return false
		}
		seen[registration.Domain] = true
	}
	return len(seen) == 3
}

func validRegistrationState(state string) bool {
	switch state {
	case RegistrationNotRegistered, RegistrationSearching, RegistrationDenied,
		RegistrationRegisteredHome, RegistrationRegisteredRoaming, RegistrationRegisteredSMSHome,
		RegistrationRegisteredSMSRoaming, RegistrationEmergencyOnly, RegistrationHomeCSFBNotPreferred,
		RegistrationRoamingCSFBNotPreferred, RegistrationUnknown:
		return true
	default:
		return false
	}
}

func anyRegistration(registrations []RegistrationObservation, states ...string) bool {
	for _, registration := range registrations {
		for _, state := range states {
			if registration.State == state {
				return true
			}
		}
	}
	return false
}
