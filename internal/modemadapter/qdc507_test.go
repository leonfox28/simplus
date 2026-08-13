package modemadapter

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
)

func TestQDC507IdentityAndOperatorFixtures(t *testing.T) {
	commands := make([]string, 0, 7)
	query := func(_ context.Context, command string, _ time.Duration) ([]string, error) {
		commands = append(commands, command)
		switch command {
		case "AT+CGSN":
			return []string{"490154203237518", "OK"}, nil
		case "AT+QCCID":
			return []string{`+QCCID: "89860123456789012345"`, "OK"}, nil
		case "AT+CRSM=176,28486,0,0,17":
			return []string{`+CRSM: 144,0,"0043484E2D554E49434F4DFFFFFFFFFFFF"`, "OK"}, nil
		case "AT+CIMI":
			return []string{"460011234567890", "OK"}, nil
		case "AT+CRSM=176,28589,0,0,4":
			return []string{`+CRSM: 144,0,"00000002"`, "OK"}, nil
		default:
			return nil, errors.New("unexpected query")
		}
	}
	adapter := QDC507{}
	if got, err := adapter.ReadEquipmentIdentity(t.Context(), query); err != nil || got != "490154203237518" {
		t.Fatalf("equipment identity = %q, error = %v", got, err)
	}
	identity, err := adapter.ReadSIMIdentity(t.Context(), query, fixedIdentityPseudonymizer{value: strings.Repeat("b", 64)})
	if err != nil || identity.Fingerprint != strings.Repeat("b", 64) || identity.DisplayHint != "ICCID •••• 2345" ||
		identity.HomeOperatorName != "CHN-UNICOM" || identity.HomeOperatorCode != "460-01" {
		t.Fatalf("SIM identity = %#v, error = %v", identity, err)
	}
}

func TestQDC507IdentityRejectsMalformedFixtures(t *testing.T) {
	adapter := QDC507{}
	for _, response := range [][]string{{"490154203237519", "OK"}, {"not-an-imei", "OK"}, {"490154203237518", "ERROR"}} {
		if _, err := adapter.ReadEquipmentIdentity(t.Context(), func(context.Context, string, time.Duration) ([]string, error) { return response, nil }); err == nil {
			t.Fatalf("accepted equipment response %#v", response)
		}
	}
	for _, response := range [][]string{{`+QCCID: "123"`, "OK"}, {`+QCCID: 89860123456789012345`, "ERROR"}, {`+QCCID: not-digits`, "OK"}} {
		if _, err := adapter.ReadSIMIdentity(t.Context(), func(context.Context, string, time.Duration) ([]string, error) { return response, nil }, fixedIdentityPseudonymizer{value: strings.Repeat("b", 64)}); err == nil {
			t.Fatalf("accepted SIM response %#v", response)
		}
	}
}

func TestQDC507ModuleSerialRequiresAdvertisedDistinctBoundedValue(t *testing.T) {
	const syntheticIMEI = "490154203237518"
	tests := []struct {
		name      string
		responses map[string][]string
		want      string
		commands  string
	}{
		{
			name: "success", want: "SYNTHETIC-MODULE-0001", commands: "AT+CGSN=?,AT+CGSN=0,AT+CGSN=1",
			responses: map[string][]string{
				"AT+CGSN=?": {`+CGSN:"sn,imei"(0,1)`, "OK"},
				"AT+CGSN=0": {`+CGSN: "SYNTHETIC-MODULE-0001"`, "OK"},
				"AT+CGSN=1": {`+CGSN: "` + syntheticIMEI + `"`, "OK"},
			},
		},
		{name: "unsupported", commands: "AT+CGSN=?", responses: map[string][]string{"AT+CGSN=?": {"ERROR"}}},
		{
			name: "echoed support response", commands: "AT+CGSN=?",
			responses: map[string][]string{
				"AT+CGSN=?": {"AT+CGSN=?", `+CGSN:"sn,imei"(0,1)`, "OK"},
			},
		},
		{
			name: "equal to IMEI", commands: "AT+CGSN=?,AT+CGSN=0,AT+CGSN=1",
			responses: map[string][]string{
				"AT+CGSN=?": {`+CGSN: "sn,imei" (0,1)`, "OK"},
				"AT+CGSN=0": {`+CGSN:"` + syntheticIMEI + `"`, "OK"},
				"AT+CGSN=1": {`+CGSN:"` + syntheticIMEI + `"`, "OK"},
			},
		},
		{
			name: "malformed serial", commands: "AT+CGSN=?,AT+CGSN=0",
			responses: map[string][]string{
				"AT+CGSN=?": {`+CGSN:"sn,imei"(0,1)`, "OK"},
				"AT+CGSN=0": {`+CGSN: SYNTHETIC-MODULE-0001`, "OK"},
			},
		},
		{
			name: "overflow serial", commands: "AT+CGSN=?,AT+CGSN=0",
			responses: map[string][]string{
				"AT+CGSN=?": {`+CGSN:"sn,imei"(0,1)`, "OK"},
				"AT+CGSN=0": {`+CGSN:"` + strings.Repeat("S", 129) + `"`, "OK"},
			},
		},
		{
			name: "control serial", commands: "AT+CGSN=?,AT+CGSN=0",
			responses: map[string][]string{
				"AT+CGSN=?": {`+CGSN:"sn,imei"(0,1)`, "OK"},
				"AT+CGSN=0": {"+CGSN:\"SYNTHETIC\nSERIAL\"", "OK"},
			},
		},
		{
			name: "duplicate terminal", commands: "AT+CGSN=?,AT+CGSN=0",
			responses: map[string][]string{
				"AT+CGSN=?": {`+CGSN:"sn,imei"(0,1)`, "OK"},
				"AT+CGSN=0": {`+CGSN:"SYNTHETIC-MODULE-0001"`, "OK", "OK"},
			},
		},
		{
			name: "invalid IMEI", commands: "AT+CGSN=?,AT+CGSN=0,AT+CGSN=1",
			responses: map[string][]string{
				"AT+CGSN=?": {`+CGSN:"sn,imei"(0,1)`, "OK"},
				"AT+CGSN=0": {`+CGSN:"SYNTHETIC-MODULE-0001"`, "OK"},
				"AT+CGSN=1": {`+CGSN:"490154203237519"`, "OK"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			commands := []string{}
			serial, err := (QDC507{}).ReadModuleSerial(t.Context(), func(_ context.Context, command string, _ time.Duration) ([]string, error) {
				commands = append(commands, command)
				return test.responses[command], nil
			})
			if serial != test.want || (err == nil) != (test.want != "") || strings.Join(commands, ",") != test.commands {
				t.Fatalf("serial available=%t error=%v commands=%q", serial != "", err, commands)
			}
			if err != nil && err.Error() != "module serial is unavailable" {
				t.Fatalf("unbounded module serial error: %v", err)
			}
		})
	}
}

func TestQDC507SIMIdentityAcceptsBoundedQuotedAndUnquotedQCCID(t *testing.T) {
	for _, response := range []string{`+QCCID: "89860123456789012345"`, `+QCCID: 89860123456789012345`} {
		identity, err := (QDC507{}).ReadSIMIdentity(t.Context(), func(_ context.Context, command string, _ time.Duration) ([]string, error) {
			if command == "AT+QCCID" {
				return []string{response, "OK"}, nil
			}
			return nil, errors.New("operator metadata unavailable")
		}, fixedIdentityPseudonymizer{value: strings.Repeat("b", 64)})
		if err != nil || identity.DisplayHint != "ICCID •••• 2345" || identity.HomeOperatorName != "" || identity.HomeOperatorCode != "" {
			t.Fatalf("response=%q identity=%#v error=%v", response, identity, err)
		}
	}
}

func TestQDC507SIMIdentityDerivesTwoAndThreeDigitMNCFromEFAD(t *testing.T) {
	for _, test := range []struct {
		name, imsi, adminData, want string
	}{
		{name: "two digit MNC", imsi: "234150123456789", adminData: "00000002", want: "234-15"},
		{name: "three digit MNC", imsi: "310260123456789", adminData: "00000003", want: "310-260"},
	} {
		t.Run(test.name, func(t *testing.T) {
			identity, err := (QDC507{}).ReadSIMIdentity(t.Context(), func(_ context.Context, command string, _ time.Duration) ([]string, error) {
				switch command {
				case "AT+QCCID":
					return []string{`+QCCID: 89860123456789012345`, "OK"}, nil
				case "AT+CRSM=176,28486,0,0,17":
					return nil, errors.New("SPN unavailable")
				case "AT+CIMI":
					return []string{test.imsi, "OK"}, nil
				case "AT+CRSM=176,28589,0,0,4":
					return []string{`+CRSM: 144,0,"` + test.adminData + `"`, "OK"}, nil
				default:
					return nil, errors.New("unexpected query")
				}
			}, fixedIdentityPseudonymizer{value: strings.Repeat("b", 64)})
			if err != nil || identity.HomeOperatorName != "" || identity.HomeOperatorCode != test.want || identity.Fingerprint != strings.Repeat("b", 64) {
				t.Fatalf("identity=%#v error=%v", identity, err)
			}
		})
	}
}

func TestQDC507SIMIdentityKeepsStableIdentityWhenOperatorMetadataIsMalformed(t *testing.T) {
	identity, err := (QDC507{}).ReadSIMIdentity(t.Context(), func(_ context.Context, command string, _ time.Duration) ([]string, error) {
		switch command {
		case "AT+QCCID":
			return []string{`+QCCID: "89860123456789012345"`, "OK"}, nil
		case "AT+CRSM=176,28486,0,0,17":
			return []string{`+CRSM: 144,0,"GG"`, "OK"}, nil
		case "AT+CIMI":
			return []string{"not-an-imsi", "OK"}, nil
		case "AT+CRSM=176,28589,0,0,4":
			return []string{`+CRSM: 144,0,"00000009"`, "OK"}, nil
		default:
			return nil, errors.New("unexpected query")
		}
	}, fixedIdentityPseudonymizer{value: strings.Repeat("b", 64)})
	if err != nil || identity.Fingerprint != strings.Repeat("b", 64) || identity.DisplayHint != "ICCID •••• 2345" ||
		identity.HomeOperatorName != "" || identity.HomeOperatorCode != "" {
		t.Fatalf("identity=%#v error=%v", identity, err)
	}
}

func TestQDC507RFControlUsesFixedCommandsAndReadBack(t *testing.T) {
	commands := []string{}
	observed, err := (QDC507{}).SetRFState(t.Context(), func(_ context.Context, command string, _ time.Duration) ([]string, error) {
		commands = append(commands, command)
		switch command {
		case "AT", "AT+CFUN=1":
			return []string{"OK"}, nil
		case "AT+CFUN?":
			return []string{"+CFUN: 1", "OK"}, nil
		default:
			return nil, errors.New("unexpected query")
		}
	}, true)
	if err != nil || observed.State != agentapi.RFStateOn || strings.Join(commands, ",") != "AT,AT+CFUN=1,AT+CFUN?" {
		t.Fatalf("observation = %#v, commands = %#v, error = %v", observed, commands, err)
	}
}

func TestQDC507ImplementsSharedBaseCapabilities(t *testing.T) {
	var adapter any = QDC507{}
	for name, ok := range map[string]bool{
		"equipment": func() bool { _, ok := adapter.(EquipmentIdentityAdapter); return ok }(),
		"SIM":       func() bool { _, ok := adapter.(SIMIdentityAdapter); return ok }(),
		"serial":    func() bool { _, ok := adapter.(ModuleSerialAdapter); return ok }(),
		"RF":        func() bool { _, ok := adapter.(RFControlAdapter); return ok }(),
	} {
		if !ok {
			t.Fatalf("shared %s capability missing", name)
		}
	}
}
