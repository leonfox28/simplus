package hardwareprobe

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/attransport"
	"github.com/leonfox28/simplus/internal/modemadapter"
)

type scriptedATSession struct {
	commands []string
	closed   bool
	query    attransport.Query
}

func (session *scriptedATSession) Query(ctx context.Context, command string, timeout time.Duration) ([]string, error) {
	session.commands = append(session.commands, command)
	return session.query(ctx, command, timeout)
}

func (session *scriptedATSession) Close() { session.closed = true }

type scriptedATOpener struct {
	endpoint string
	session  attransport.Session
	err      error
}

type moduleSerialProbeAdapter struct{ modemadapter.ML307A }

func (moduleSerialProbeAdapter) ReadModuleSerial(ctx context.Context, query attransport.Query) (string, error) {
	lines, err := query(ctx, "AT+SYNTHETIC-SERIAL", time.Second)
	if err != nil || len(lines) != 2 || lines[0] != "SYNTHETIC-MODULE-0001" || lines[1] != "OK" {
		return "", errors.New("module serial unavailable")
	}
	return lines[0], nil
}

func (moduleSerialProbeAdapter) ReadSubscriberNumber(ctx context.Context, query attransport.Query) (string, error) {
	lines, err := query(ctx, "AT+SYNTHETIC-NUMBER", time.Second)
	if err != nil || len(lines) != 2 || lines[0] != "+12025550123" || lines[1] != "OK" {
		return "", errors.New("subscriber number unavailable")
	}
	return lines[0], nil
}

func (opener *scriptedATOpener) Open(endpoint string) (attransport.Session, error) {
	opener.endpoint = endpoint
	if opener.err != nil {
		return nil, opener.err
	}
	return opener.session, nil
}

func TestATRuntimeDelegatesCommandsToAdapterCapabilitiesAndClosesSession(t *testing.T) {
	session := &scriptedATSession{}
	session.query = func(_ context.Context, command string, _ time.Duration) ([]string, error) {
		switch command {
		case "AT":
			return []string{"OK"}, nil
		case "AT+CGMI":
			return []string{"CMIOT", "OK"}, nil
		case "AT+CGMM":
			return []string{"ML307A", "OK"}, nil
		case "AT+CGMR":
			return []string{"revision", "OK"}, nil
		case "AT+CFUN?":
			return []string{"+CFUN: 4", "OK"}, nil
		case "AT+CPIN?":
			return []string{"+CPIN: READY", "OK"}, nil
		case "AT+CREG?", "AT+CGREG?", "AT+CEREG?":
			return []string{"OK"}, nil
		case "AT+COPS?":
			return []string{"+COPS: 0", "OK"}, nil
		case "AT+CSQ":
			return []string{"+CSQ: 99,99", "OK"}, nil
		case "AT+CLCC":
			return []string{"ERROR"}, nil
		case "AT+CGSN=1":
			return []string{"+CGSN: 490154203237518", "OK"}, nil
		case "AT+SYNTHETIC-SERIAL":
			return []string{"SYNTHETIC-MODULE-0001", "OK"}, nil
		case "AT+SYNTHETIC-NUMBER":
			return []string{"+12025550123", "OK"}, nil
		case "AT+MCCID":
			return []string{"+MCCID: 89861118216007272115", "OK"}, nil
		case "AT+CRSM=176,28486,0,0,17":
			return []string{`+CRSM: 144,0,"00564F5849FFFFFFFFFFFFFFFFFFFFFFFF"`, "OK"}, nil
		case "AT+CIMI":
			return []string{"234150123456789", "OK"}, nil
		case "AT+CRSM=176,28589,0,0,4":
			return []string{`+CRSM: 144,0,"00000002"`, "OK"}, nil
		default:
			return nil, errors.New("unexpected adapter query")
		}
	}
	opener := &scriptedATOpener{session: session}
	runtime := atRuntime{opener: opener, identities: deterministicPseudonymizer{}}

	probe := runtime.Probe(t.Context(), "/dev/fixture-ml307a", moduleSerialProbeAdapter{})
	if probe.State != agentapi.ProbeStateFailed || probe.ErrorCode != agentapi.ErrorCallStateUnknown {
		t.Fatalf("probe state = %#v", probe)
	}
	if probe.Identity.Model != "ML307A" || probe.Identity.SerialNumber != "SYNTHETIC-MODULE-0001" ||
		probe.Identity.EquipmentIdentityFingerprint == "" || probe.SIM.IdentityFingerprint == "" ||
		probe.SIM.DisplayIdentityHint != "ICCID •••• 2115" || probe.SIM.HomeOperatorName != "VOXI" || probe.SIM.HomeOperatorCode != "234-15" ||
		probe.SIM.SubscriberNumber != "+12025550123" {
		t.Fatalf("independent identity observations = %#v", probe)
	}
	if opener.endpoint != "/dev/fixture-ml307a" || !session.closed {
		t.Fatalf("endpoint = %q, closed = %t", opener.endpoint, session.closed)
	}
}

type subscriberProbeAdapter struct {
	modemadapter.ML307A
	identityErr bool
	numberErr   bool
	numberCalls *int
}

func (adapter subscriberProbeAdapter) ReadSIMIdentity(ctx context.Context, query attransport.Query, identities modemadapter.IdentityPseudonymizer) (modemadapter.SIMProfileIdentity, error) {
	if adapter.identityErr {
		return modemadapter.SIMProfileIdentity{}, errors.New("identity unavailable")
	}
	return adapter.ML307A.ReadSIMIdentity(ctx, query, identities)
}

func (adapter subscriberProbeAdapter) ReadSubscriberNumber(context.Context, attransport.Query) (string, error) {
	if adapter.numberCalls != nil {
		*adapter.numberCalls++
	}
	if adapter.numberErr {
		return "", errors.New("number unavailable")
	}
	return "+12025550123", nil
}

func TestATRuntimeSubscriberNumberRequiresReadyIdentifiedSIMAndIsBestEffort(t *testing.T) {
	query := func(cpin string) attransport.Query {
		return func(_ context.Context, command string, _ time.Duration) ([]string, error) {
			switch command {
			case "AT":
				return []string{"OK"}, nil
			case "AT+CGMI":
				return []string{"CMIOT", "OK"}, nil
			case "AT+CGMM":
				return []string{"ML307A", "OK"}, nil
			case "AT+CGMR":
				return []string{"revision", "OK"}, nil
			case "AT+CFUN?":
				return []string{"+CFUN: 4", "OK"}, nil
			case "AT+CPIN?":
				return []string{cpin, "OK"}, nil
			case "AT+CREG?", "AT+CGREG?", "AT+CEREG?":
				return []string{"OK"}, nil
			case "AT+COPS?":
				return []string{"+COPS: 0", "OK"}, nil
			case "AT+CSQ":
				return []string{"+CSQ: 99,99", "OK"}, nil
			case "AT+CLCC":
				return []string{"OK"}, nil
			case "AT+CGSN=1":
				return []string{"+CGSN: 490154203237518", "OK"}, nil
			case "AT+MCCID":
				return []string{"+MCCID: 89861118216007272115", "OK"}, nil
			default:
				return nil, errors.New("optional metadata unavailable")
			}
		}
	}
	for _, test := range []struct {
		name, cpin, want       string
		identityErr, numberErr bool
		wantCalls              int
	}{
		{name: "number succeeds", cpin: "+CPIN: READY", want: "+12025550123", wantCalls: 1},
		{name: "number unavailable does not degrade probe", cpin: "+CPIN: READY", numberErr: true, wantCalls: 1},
		{name: "identity unavailable suppresses number", cpin: "+CPIN: READY", identityErr: true},
		{name: "locked SIM suppresses number", cpin: "+CPIN: SIM PIN"},
		{name: "absent SIM suppresses number", cpin: "+CPIN: NOT INSERTED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			adapter := subscriberProbeAdapter{identityErr: test.identityErr, numberErr: test.numberErr, numberCalls: &calls}
			probe := executeATProbe(t.Context(), query(test.cpin), "/dev/synthetic", adapter, deterministicPseudonymizer{})
			if probe.State != agentapi.ProbeStateComplete || probe.SIM.SubscriberNumber != test.want || calls != test.wantCalls {
				t.Fatalf("probe=%#v", probe)
			}
		})
	}
}

func TestATRuntimeMapsUnsupportedTransportWithoutIssuingAdapterCommands(t *testing.T) {
	opener := &scriptedATOpener{err: errors.New("unavailable")}
	probe := (atRuntime{opener: opener}).Probe(t.Context(), "/dev/missing", modemadapter.ML307A{})
	if probe.State != agentapi.ProbeStateUnavailable || probe.ErrorCode != agentapi.ErrorControlEndpointOpen {
		t.Fatalf("probe = %#v", probe)
	}
}
