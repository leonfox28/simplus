package hardwareprobe

import (
	"strings"
	"testing"

	"github.com/leonfox28/simplus/internal/agentapi"
)

type fixedIdentityPseudonymizer struct{ value string }

func (pseudonymizer fixedIdentityPseudonymizer) Pseudonym(string, []byte) (string, error) {
	return pseudonymizer.value, nil
}

func TestML307AICCIDIsOnlyReturnedAsAKeyedPseudonymAndMaskedHint(t *testing.T) {
	fingerprint := strings.Repeat("a", 64)
	actual, hint := pseudonymizedML307AICCID(
		[]string{"+MCCID: 89861118216007272115", "OK"},
		fixedIdentityPseudonymizer{value: fingerprint},
	)
	if actual != fingerprint || hint != "ICCID •••• 2115" {
		t.Fatalf("identity = (%q, %q)", actual, hint)
	}
	for _, lines := range [][]string{
		{"+MCCID: not-an-iccid", "OK"},
		{"+MCCID: 12345678901234", "OK"},
		{"89861118216007272115", "OK"},
	} {
		if value, display := pseudonymizedML307AICCID(lines, fixedIdentityPseudonymizer{value: fingerprint}); value != "" || display != "" {
			t.Fatalf("invalid identity was accepted: (%q, %q)", value, display)
		}
	}
}

func TestTypedRFAndSIMObservations(t *testing.T) {
	for _, test := range []struct {
		line  string
		state string
		mode  int
	}{
		{line: "+CFUN: 0", state: agentapi.RFStateMinimum, mode: 0},
		{line: "+CFUN: 1", state: agentapi.RFStateOn, mode: 1},
		{line: "+CFUN: 4", state: agentapi.RFStateOff, mode: 4},
	} {
		observation := rfObservation([]string{test.line, "OK"})
		if observation.State != test.state || observation.Mode == nil || *observation.Mode != test.mode {
			t.Fatalf("RF observation for %q = %#v", test.line, observation)
		}
	}
	if observation := rfObservation([]string{"+CFUN: invalid", "OK"}); observation.State != agentapi.RFStateUnknown || observation.Mode != nil {
		t.Fatalf("invalid RF observation = %#v", observation)
	}

	for _, test := range []struct {
		lines       []string
		simState    string
		primaryLock string
		lockType    string
	}{
		{lines: []string{"+CPIN: READY", "OK"}, simState: agentapi.SIMStatePresent, primaryLock: agentapi.PrimaryLockReady},
		{lines: []string{"+CPIN: SIM PIN", "OK"}, simState: agentapi.SIMStateLocked, primaryLock: agentapi.PrimaryLockPIN1Required, lockType: "pin1"},
		{lines: []string{"+CPIN: SIM PUK", "OK"}, simState: agentapi.SIMStateLocked, primaryLock: agentapi.PrimaryLockPUK1Required, lockType: "puk1"},
		{lines: []string{"+CPIN: SIM PIN2", "OK"}, simState: agentapi.SIMStateLocked, primaryLock: agentapi.PrimaryLockUnsupported, lockType: "unsupported"},
		{lines: []string{"+CPIN: SIM PUK2", "OK"}, simState: agentapi.SIMStateLocked, primaryLock: agentapi.PrimaryLockUnsupported, lockType: "unsupported"},
		{lines: []string{"+CPIN: PH-NET PIN", "OK"}, simState: agentapi.SIMStateLocked, primaryLock: agentapi.PrimaryLockUnsupported, lockType: "unsupported"},
		{lines: []string{"+CPIN: SIM PUK BLOCKED", "OK"}, simState: agentapi.SIMStateLocked, primaryLock: agentapi.PrimaryLockPermanentlyBlocked, lockType: "puk1-blocked"},
		{lines: []string{"+CME ERROR: 10"}, simState: agentapi.SIMStateAbsent, primaryLock: agentapi.PrimaryLockUnknown},
	} {
		observation := simObservation(test.lines, nil)
		if observation.State != test.simState || observation.PrimaryLockState != test.primaryLock || observation.LockType != test.lockType {
			t.Fatalf("SIM observation for %v = %#v", test.lines, observation)
		}
	}
}

func TestTypedRegistrationSignalAndNetworkObservations(t *testing.T) {
	registrations := registrationObservations(
		[]string{"+CREG: 0,1", "OK"},
		[]string{"+CGREG: 2", "OK"},
		[]string{`+CEREG: 2,5,"1234","00112233",7`, "OK"},
	)
	if len(registrations) != 3 || registrations[0].State != agentapi.RegistrationRegisteredHome ||
		registrations[1].State != agentapi.RegistrationSearching || registrations[2].State != agentapi.RegistrationRegisteredRoaming {
		t.Fatalf("registrations = %#v", registrations)
	}

	signal := signalObservation([]string{"+CSQ: 20,3", "OK"})
	if signal.State != agentapi.SignalStateMeasured || signal.RSSI == nil || *signal.RSSI != 20 ||
		signal.RSSIDBm == nil || *signal.RSSIDBm != -73 || signal.BER == nil || *signal.BER != 3 {
		t.Fatalf("signal = %#v", signal)
	}
	if unavailable := signalObservation([]string{"+CSQ: 99,99", "OK"}); unavailable.State != agentapi.SignalStateUnavailable || unavailable.RSSI != nil || unavailable.BER != nil {
		t.Fatalf("unavailable signal = %#v", unavailable)
	}

	network := networkObservation(
		[]string{`+COPS: 0,2,"23415",7`, "OK"},
		[]string{`+QNWINFO: "FDD LTE","23415","LTE BAND 3",1300`, "OK"},
	)
	if network.SelectionMode != agentapi.NetworkSelectionAutomatic || network.PLMN != "23415" || network.RAT != "lte" || network.OperatorName != "" {
		t.Fatalf("network = %#v", network)
	}
	fallback := networkObservation([]string{"+COPS: 4", "OK"}, []string{`+QNWINFO: "WCDMA","VOXI","WCDMA 2100",10564`, "OK"})
	if fallback.SelectionMode != agentapi.NetworkSelectionManualAutomatic || fallback.OperatorName != "VOXI" || fallback.RAT != "utran" {
		t.Fatalf("fallback network = %#v", fallback)
	}
	quoted := networkObservation([]string{`+COPS: 1,0,"China Mobile",7`, "OK"}, nil)
	if quoted.SelectionMode != agentapi.NetworkSelectionManual || quoted.OperatorName != "China Mobile" || quoted.RAT != agentapi.RATLTE {
		t.Fatalf("quoted operator = %#v", quoted)
	}
	numericLookingName := networkObservation([]string{`+COPS: 0,0,"12345",7`, "OK"}, nil)
	if numericLookingName.OperatorName != "12345" || numericLookingName.PLMN != "" {
		t.Fatalf("numeric-looking operator name = %#v", numericLookingName)
	}
	noService := networkObservation([]string{"+COPS: 0", "OK"}, []string{`+QNWINFO: "No Service"`, "OK"})
	if noService.SelectionMode != agentapi.NetworkSelectionAutomatic || noService.PLMN != "" || noService.OperatorName != "" || noService.RAT != "" {
		t.Fatalf("no-service network = %#v", noService)
	}
	if unknown := registrationObservation([]string{"+CREG: 0,4", "OK"}, "+CREG:", agentapi.RegistrationDomainCS, "at-creg"); unknown.State != agentapi.RegistrationUnknown {
		t.Fatalf("CREG stat 4 = %#v", unknown)
	}
	for value, expected := range map[int]string{10: agentapi.RATLTE5GC, 11: agentapi.RATNR5GC, 12: agentapi.RATNGRAN} {
		if actual := accessTechnologyName(value); actual != expected {
			t.Fatalf("access technology %d = %q, want %q", value, actual, expected)
		}
	}
}

func TestTypedParsersRejectMalformedAndOversizedCSV(t *testing.T) {
	if fields := csvPayload(`+COPS: 0,0,"unterminated`); fields != nil {
		t.Fatalf("malformed CSV = %#v", fields)
	}
	if fields := csvPayload("+COPS: 0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16"); fields != nil {
		t.Fatalf("oversized CSV = %#v", fields)
	}
	if network := networkObservation([]string{`+COPS: 0,0,"unterminated`, "OK"}, nil); network.SelectionMode != agentapi.NetworkSelectionUnknown {
		t.Fatalf("malformed network = %#v", network)
	}
}

func TestReadOnlyATParsingRedactsIdentityNumbersAndCallDestinations(t *testing.T) {
	if got := identityPayload([]string{"QDC507GLEFM21", "OK"}); got != "QDC507GLEFM21" {
		t.Fatalf("revision = %q", got)
	}
	for _, identity := range []string{"867530900000001", "89860012345678901234"} {
		if got := identityPayload([]string{identity, "OK"}); got != "" {
			t.Fatalf("identity-shaped payload leaked: %q", got)
		}
	}
	calls := []string{`+CLCC: 1,0,0,0,0,"+441234567890",145`, `+CLCC: 2,1,0,0,0,"+8613800138000",145`, "OK"}
	if got := activeCallCount(calls); got != 2 {
		t.Fatalf("call count = %d", got)
	}
	if observation := simObservation([]string{"+CPIN: SIM PIN", "OK"}, nil); observation.State != agentapi.SIMStateLocked || observation.LockType != "pin1" {
		t.Fatalf("SIM observation = %#v", observation)
	}
	if observation := simObservation([]string{"+CME ERROR: 10"}, nil); observation.State != agentapi.SIMStateAbsent {
		t.Fatalf("absent SIM observation = %#v", observation)
	}
}
