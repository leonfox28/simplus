package modemadapter

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
)

func TestML307AIdentityCapabilitiesOwnTheirFixedQueries(t *testing.T) {
	adapter := ML307A{}
	fingerprint := strings.Repeat("a", 64)
	commands := make([]string, 0, 5)
	query := func(_ context.Context, command string, _ time.Duration) ([]string, error) {
		commands = append(commands, command)
		switch command {
		case "AT+CGSN=1":
			return []string{"+CGSN: 490154203237518", "OK"}, nil
		case "AT+MCCID":
			return []string{"+MCCID: 89861118216007272115", "OK"}, nil
		case "AT+CRSM=176,28486,0,0,17":
			return []string{`+CRSM: 144,0,"00564F5849FFFFFFFFFFFFFFFFFFFFFFFF"`, "OK"}, nil
		case "AT+CIMI":
			return []string{"234150123456789", "OK"}, nil
		case "AT+CRSM=176,28589,0,0,4":
			return []string{`+CRSM: 144,0,"00000002"`, "OK"}, nil
		default:
			return nil, errors.New("unexpected query")
		}
	}
	identity := fixedIdentityPseudonymizer{value: fingerprint}
	if got, err := adapter.ReadEquipmentIdentity(t.Context(), query); err != nil || got != "490154203237518" {
		t.Fatalf("equipment identity = %q, error = %v", got, err)
	}
	observed, err := adapter.ReadSIMIdentity(t.Context(), query, identity)
	if err != nil || observed.Fingerprint != fingerprint || observed.DisplayHint != "ICCID •••• 2115" ||
		observed.HomeOperatorName != "VOXI" || observed.HomeOperatorCode != "234-15" {
		t.Fatalf("SIM identity = %#v, error = %v", observed, err)
	}
	want := []string{"AT+CGSN=1", "AT+MCCID", "AT+CRSM=176,28486,0,0,17", "AT+CIMI", "AT+CRSM=176,28589,0,0,4"}
	if strings.Join(commands, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("identity commands = %#v", commands)
	}
}

func TestModelSIMPresenceCapabilitiesOwnTheirQueries(t *testing.T) {
	for _, fixture := range []struct {
		name     string
		adapter  SIMPresenceAdapter
		commands []string
	}{
		{name: "ML307A", adapter: ML307A{}, commands: []string{"AT+CPIN?"}},
		{name: "QDC507", adapter: QDC507{}, commands: []string{"AT+CPIN?", "AT+QSIMSTAT?"}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			commands := make([]string, 0, len(fixture.commands))
			observation, err := fixture.adapter.ReadSIMPresence(t.Context(), func(_ context.Context, command string, _ time.Duration) ([]string, error) {
				commands = append(commands, command)
				switch command {
				case "AT+CPIN?":
					return []string{"+CPIN: READY", "OK"}, nil
				case "AT+QSIMSTAT?":
					return []string{"+QSIMSTAT: 1,1", "OK"}, nil
				default:
					return nil, errors.New("unexpected query")
				}
			})
			if err != nil || observation.State != agentapi.SIMStatePresent || observation.PrimaryLockState != agentapi.PrimaryLockReady {
				t.Fatalf("SIM observation = %#v, error = %v", observation, err)
			}
			if strings.Join(commands, "\x00") != strings.Join(fixture.commands, "\x00") {
				t.Fatalf("SIM presence commands = %#v", commands)
			}
		})
	}
}

func TestML307ADerivesIMSIdentityFromIMSIAndUSIMMNCLength(t *testing.T) {
	for _, fixture := range []struct {
		name       string
		imsi       string
		adminData  string
		wantDomain string
	}{
		{name: "two digit MNC", imsi: "234150123456789", adminData: "00000002", wantDomain: "ims.mnc015.mcc234.3gppnetwork.org"},
		{name: "three digit MNC", imsi: "310260123456789", adminData: "00000003", wantDomain: "ims.mnc260.mcc310.3gppnetwork.org"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			commands := []string{}
			material, err := deriveML307AIMSIdentity(t.Context(), func(_ context.Context, command string, _ time.Duration) ([]string, error) {
				commands = append(commands, command)
				return []string{`+CRSM: 144,0,"` + fixture.adminData + `"`, "OK"}, nil
			}, fixture.imsi, agentapi.SIMIMSIdentityMaterial{
				ApplicationDiscovery: agentapi.SIMIMSDiscoveryGenericAID, ApplicationCandidates: 1,
			})
			if err != nil || material.Source != agentapi.SIMIMSIdentityDerived || material.HomeDomain != fixture.wantDomain ||
				material.PrivateIdentity != fixture.imsi+"@"+fixture.wantDomain ||
				len(material.PublicIdentities) != 1 || material.PublicIdentities[0] != "sip:"+material.PrivateIdentity {
				t.Fatalf("material = %#v, error = %v", material, err)
			}
			if len(commands) != 1 || commands[0] != "AT+CRSM=176,28589,0,0,4" {
				t.Fatalf("commands = %#v", commands)
			}
		})
	}
}

func TestML307ADerivedIMSIdentityFailsClosedWithoutReliableMNCLength(t *testing.T) {
	for _, adminData := range []string{"00000000", "00000004", "000000", "not-hex"} {
		if _, err := deriveML307AIMSIdentity(t.Context(), func(context.Context, string, time.Duration) ([]string, error) {
			return []string{`+CRSM: 144,0,"` + adminData + `"`, "OK"}, nil
		}, "234150123456789", agentapi.SIMIMSIdentityMaterial{}); !errors.Is(err, agentapi.ErrSIMAKAUnavailable) {
			t.Fatalf("administrative data %q produced error %v", adminData, err)
		}
	}
}

func TestML307ARFControlOwnsFixedCommandsAndConfirmsReadBack(t *testing.T) {
	commands := make([]string, 0, 3)
	query := func(_ context.Context, command string, _ time.Duration) ([]string, error) {
		commands = append(commands, command)
		switch command {
		case "AT":
			return []string{"OK"}, nil
		case "AT+CFUN=1":
			return []string{"OK"}, nil
		case "AT+CFUN?":
			return []string{"+CFUN: 1", "OK"}, nil
		default:
			return nil, errors.New("unexpected query")
		}
	}
	observed, err := (ML307A{}).SetRFState(t.Context(), query, true)
	if err != nil || observed.State != agentapi.RFStateOn {
		t.Fatalf("RF observation = %#v, error = %v", observed, err)
	}
	want := []string{"AT", "AT+CFUN=1", "AT+CFUN?"}
	if strings.Join(commands, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("RF commands = %#v", commands)
	}
}

func TestML307ARFControlUsesReadBackAfterLostSetResponse(t *testing.T) {
	queries := 0
	observed, err := (ML307A{}).SetRFState(t.Context(), func(_ context.Context, command string, _ time.Duration) ([]string, error) {
		queries++
		switch command {
		case "AT":
			return []string{"OK"}, nil
		case "AT+CFUN=4":
			return nil, errors.New("response lost")
		case "AT+CFUN?":
			return []string{"+CFUN: 4", "OK"}, nil
		default:
			return nil, errors.New("unexpected query")
		}
	}, false)
	if err != nil || observed.State != agentapi.RFStateOff || queries != 3 {
		t.Fatalf("RF observation = %#v, queries = %d, error = %v", observed, queries, err)
	}
}
