package agentapi

import "testing"

func TestClassifyCellularRegistrationStatesAndPrecedence(t *testing.T) {
	allStates := []string{
		RegistrationNotRegistered, RegistrationSearching, RegistrationDenied, RegistrationRegisteredHome,
		RegistrationRegisteredRoaming, RegistrationRegisteredSMSHome, RegistrationRegisteredSMSRoaming,
		RegistrationEmergencyOnly, RegistrationHomeCSFBNotPreferred, RegistrationRoamingCSFBNotPreferred,
		RegistrationUnknown,
	}
	for _, state := range allStates {
		t.Run(state, func(t *testing.T) {
			probe := readyCellularProbe()
			for index := range probe.Registrations {
				probe.Registrations[index].State = state
			}
			got := ClassifyCellular(probe)
			switch state {
			case RegistrationRegisteredHome, RegistrationRegisteredSMSHome, RegistrationHomeCSFBNotPreferred:
				if got.State != CellularRegisteredHome || !got.ReadyForSMS {
					t.Fatalf("classification = %#v", got)
				}
			case RegistrationRegisteredRoaming, RegistrationRegisteredSMSRoaming, RegistrationRoamingCSFBNotPreferred:
				if got.State != CellularRegisteredRoaming || !got.ReadyForSMS {
					t.Fatalf("classification = %#v", got)
				}
			case RegistrationDenied:
				if got.State != CellularDenied || got.ReasonCode != ErrorCellularRegistrationDenied {
					t.Fatalf("classification = %#v", got)
				}
			case RegistrationSearching:
				if got.State != CellularSearching || got.ReasonCode != ErrorCellularNotRegistered {
					t.Fatalf("classification = %#v", got)
				}
			case RegistrationNotRegistered, RegistrationEmergencyOnly:
				if got.State != CellularNotRegistered || got.ReasonCode != ErrorCellularNotRegistered {
					t.Fatalf("classification = %#v", got)
				}
			default:
				if got.State != CellularUnknown || got.ReasonCode != ErrorCellularStatusUnavailable {
					t.Fatalf("classification = %#v", got)
				}
			}
		})
	}
	probe := readyCellularProbe()
	probe.Registrations[0].State = RegistrationDenied
	probe.Registrations[1].State = RegistrationRegisteredRoaming
	if got := ClassifyCellular(probe); got.State != CellularRegisteredRoaming || !got.ReadyForSMS {
		t.Fatalf("registered precedence = %#v", got)
	}
	probe.SIM.State = SIMStateAbsent
	if got := ClassifyCellular(probe); got.State != CellularSIMNotReady {
		t.Fatalf("SIM precedence = %#v", got)
	}
	probe.State = ProbeStateFailed
	if got := ClassifyCellular(probe); got.State != CellularUnavailable {
		t.Fatalf("probe precedence = %#v", got)
	}
}

func readyCellularProbe() DeviceProbe {
	return DeviceProbe{
		State: ProbeStateComplete, SIM: SIMObservation{State: SIMStatePresent, PrimaryLockState: PrimaryLockReady},
		RF: RFObservation{State: RFStateOn}, Registrations: []RegistrationObservation{
			{Domain: RegistrationDomainCS, State: RegistrationNotRegistered},
			{Domain: RegistrationDomainPacket, State: RegistrationNotRegistered},
			{Domain: RegistrationDomainEPS, State: RegistrationNotRegistered},
		},
	}
}
