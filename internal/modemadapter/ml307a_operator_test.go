package modemadapter

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestML307AHomeOperatorUsesProfileSPNAndReliableMCCMNC(t *testing.T) {
	commands := []string{}
	name, code := readML307AHomeOperator(t.Context(), func(_ context.Context, command string, _ time.Duration) ([]string, error) {
		commands = append(commands, command)
		switch command {
		case "AT+CRSM=176,28486,0,0,17":
			return []string{`+CRSM: 144,0,"00564F5849FFFFFFFFFFFFFFFFFFFFFFFF"`, "OK"}, nil
		case "AT+CIMI":
			return []string{"234150123456789", "OK"}, nil
		case "AT+CRSM=176,28589,0,0,4":
			return []string{`+CRSM: 144,0,"00000002"`, "OK"}, nil
		default:
			return nil, errors.New("unexpected query")
		}
	})
	if name != "VOXI" || code != "234-15" {
		t.Fatalf("home operator = (%q, %q)", name, code)
	}
	want := []string{"AT+CRSM=176,28486,0,0,17", "AT+CIMI", "AT+CRSM=176,28589,0,0,4"}
	if len(commands) != len(want) {
		t.Fatalf("commands = %#v", commands)
	}
	for index := range want {
		if commands[index] != want[index] {
			t.Fatalf("commands = %#v", commands)
		}
	}
}

func TestML307AHomeOperatorMetadataIsBestEffort(t *testing.T) {
	name, code := readML307AHomeOperator(t.Context(), func(context.Context, string, time.Duration) ([]string, error) {
		return nil, errors.New("not provisioned")
	})
	if name != "" || code != "" {
		t.Fatalf("home operator = (%q, %q)", name, code)
	}
}

func TestSIMServiceProviderNameDecoders(t *testing.T) {
	fixtures := []struct {
		name string
		data []byte
		want string
	}{
		{name: "GSM", data: []byte{'V', 'O', 'X', 'I', 0xff}, want: "VOXI"},
		{name: "UCS2", data: []byte{0x80, 0x00, 'V', 0x00, 'O', 0x00, 'X', 0x00, 'I', 0xff}, want: "VOXI"},
		{name: "compact UCS2", data: []byte{0x81, 0x04, 0x00, 'V', 'O', 'X', 'I'}, want: "VOXI"},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			if got := normalizeSIMOperatorName(decodeSIMAlphaIdentifier(fixture.data)); got != fixture.want {
				t.Fatalf("decoded = %q", got)
			}
		})
	}
}
