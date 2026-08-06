package modemadapter

import (
	"strings"
	"testing"
)

type fixedIdentityPseudonymizer struct{ value string }

func (pseudonymizer fixedIdentityPseudonymizer) Pseudonym(string, []byte) (string, error) {
	return pseudonymizer.value, nil
}

func TestML307AICCIDIsOnlyReturnedAsAKeyedPseudonymAndMaskedHint(t *testing.T) {
	fingerprint := strings.Repeat("a", 64)
	actual, hint := pseudonymizedICCID(
		[]string{"+MCCID: 89861118216007272115", "OK"},
		"+MCCID:",
		fixedIdentityPseudonymizer{value: fingerprint},
	)
	if actual != fingerprint || hint != "ICCID •••• 2115" {
		t.Fatalf("identity = (%q, %q)", actual, hint)
	}
	for _, lines := range [][]string{
		{"+MCCID: not-an-iccid", "OK"},
		{"+MCCID: 12345678901234", "OK"},
		{"89861118216007272115", "OK"},
		{"+MCCID: 89861118216007272115", "ERROR"},
	} {
		if value, display := pseudonymizedICCID(lines, "+MCCID:", fixedIdentityPseudonymizer{value: fingerprint}); value != "" || display != "" {
			t.Fatalf("invalid identity was accepted: (%q, %q)", value, display)
		}
	}
}

func TestML307AIMEIParserRequiresAValidCheckDigitAndTerminalResponse(t *testing.T) {
	lines := []string{"+CGSN: 490154203237518", "OK"}
	if actual := equipmentIMEI(lines); actual != "490154203237518" {
		t.Fatalf("IMEI = %q", actual)
	}
	for _, invalid := range [][]string{
		{"+CGSN: 490154203237519", "OK"},
		{"+CGSN: 490154203237518", "ERROR"},
		{"4901542032375180", "OK"},
	} {
		if actual := equipmentIMEI(invalid); actual != "" {
			t.Fatalf("invalid IMEI was accepted: %q", actual)
		}
	}
}
