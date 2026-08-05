package main

import (
	"strings"
	"testing"
)

func TestIdentityMarkerReportsOnlyComparison(t *testing.T) {
	expected := "0234150123456789@nai.epc.mnc015.mcc234.3gppnetwork.org"
	marker, ok := identityMarker("quintuplets for '"+expected+"'", expected)
	if !ok || marker != "SAFE sim_card_identity_matches=true identity_had_type_prefix=false" || strings.Contains(marker, expected) {
		t.Fatalf("unexpected marker %q", marker)
	}
}

func TestRedactAssignmentsRemovesAKAMaterial(t *testing.T) {
	redacted := redactAssignments("RAND=0011 AUTN=2233 RES=4455 CK=6677 IK=8899")
	if strings.Contains(redacted, "0011") || strings.Contains(redacted, "2233") ||
		strings.Contains(redacted, "4455") || strings.Contains(redacted, "6677") || strings.Contains(redacted, "8899") {
		t.Fatalf("AKA material survived redaction: %q", redacted)
	}
}
