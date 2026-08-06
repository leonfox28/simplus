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
		case "AT+MCCID":
			return []string{"+MCCID: 89861118216007272115", "OK"}, nil
		default:
			return nil, errors.New("unexpected adapter query")
		}
	}
	opener := &scriptedATOpener{session: session}
	runtime := atRuntime{opener: opener, identities: deterministicPseudonymizer{}}

	probe := runtime.Probe(t.Context(), "/dev/fixture-ml307a", modemadapter.ML307A{})
	if probe.State != agentapi.ProbeStateFailed || probe.ErrorCode != agentapi.ErrorCallStateUnknown {
		t.Fatalf("probe state = %#v", probe)
	}
	if probe.Identity.Model != "ML307A" || probe.Identity.EquipmentIdentityFingerprint == "" || probe.SIM.IdentityFingerprint == "" || probe.SIM.DisplayIdentityHint != "ICCID •••• 2115" {
		t.Fatalf("independent identity observations = %#v", probe)
	}
	if opener.endpoint != "/dev/fixture-ml307a" || !session.closed {
		t.Fatalf("endpoint = %q, closed = %t", opener.endpoint, session.closed)
	}
}

func TestATRuntimeMapsUnsupportedTransportWithoutIssuingAdapterCommands(t *testing.T) {
	opener := &scriptedATOpener{err: errors.New("unavailable")}
	probe := (atRuntime{opener: opener}).Probe(t.Context(), "/dev/missing", modemadapter.ML307A{})
	if probe.State != agentapi.ProbeStateUnavailable || probe.ErrorCode != agentapi.ErrorControlEndpointOpen {
		t.Fatalf("probe = %#v", probe)
	}
}
