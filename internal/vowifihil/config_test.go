package vowifihil

import (
	"strings"
	"testing"

	"github.com/leonfox28/simplus/internal/agentapi"
)

func TestBuildCreatesBoundedEAPAKAConfigWithoutPersistentLogging(t *testing.T) {
	input := Input{
		Target: agentapi.SIMAKATarget{
			AgentInstanceID:     "01234567-89ab-cdef-0123-456789abcdef",
			SnapshotGeneration:  7,
			SnapshotRevision:    strings.Repeat("a", 64),
			DeviceID:            "usb-1-3",
			DeviceGeneration:    9,
			IdentityFingerprint: strings.Repeat("b", 64),
		},
		IMSI: "234150123456789",
	}
	config, err := Build(input)
	if err != nil {
		t.Fatal(err)
	}
	strongSwan := string(config.StrongSwan)
	for _, required := range []string{
		"path = /run/simplus-vowifi-hil/charon.pipe",
		"socket = unix:///run/simplus-vowifi-hil/charon.vici",
		"socket = /run/simplus-agent/sim-aka.sock",
		"expected_identity = 0234150123456789@nai.epc.mnc015.mcc234.3gppnetwork.org",
		"vowifi-ims = yes",
		"job = -1",
		"default = -1",
	} {
		if !strings.Contains(strongSwan, required) {
			t.Fatalf("strongSwan config is missing %q", required)
		}
	}
	if strings.Contains(strongSwan, "charon.log") || strings.Contains(strongSwan, "path = stdout") || strings.Contains(strongSwan, "path = stderr") {
		t.Fatal("HIL config must send diagnostics only to the transient redaction pipe")
	}
	connectionInput, err := ParseConnectionInput(config.VICI)
	if err != nil {
		t.Fatal(err)
	}
	message, err := ConnectionMessage(connectionInput)
	if err != nil {
		t.Fatal(err)
	}
	encoded := message.String()
	for _, required := range []string{
		"auth = eap-aka",
		"auth = eap",
		"id = fqdn:*.epdg.om.epc.mnc015.mcc234.3gppnetwork.org",
		"vips = 0.0.0.0",
		"send_certreq = no",
		"dpd_delay = 20s",
	} {
		if !strings.Contains(encoded, required) {
			t.Fatalf("VICI connection is missing %q", required)
		}
	}
}

func TestParseConnectionInputRejectsUnknownAndTrailingFields(t *testing.T) {
	for _, candidate := range [][]byte{
		[]byte(`{"version":1,"identity":"bad"}`),
		[]byte(`{"version":1,"identity":"0234150123456789@nai.epc.mnc015.mcc234.3gppnetwork.org","extra":true}`),
		[]byte("{\"version\":1,\"identity\":\"0234150123456789@nai.epc.mnc015.mcc234.3gppnetwork.org\"}\n{}"),
	} {
		if _, err := ParseConnectionInput(candidate); err == nil {
			t.Fatal("invalid transient VICI input must be rejected")
		}
	}
}

func TestBuildRejectsUnexpectedSIMAndUnfencedTarget(t *testing.T) {
	if _, err := Build(Input{}); err == nil {
		t.Fatal("empty input must be rejected")
	}
	input := Input{
		Target: agentapi.SIMAKATarget{
			AgentInstanceID:     "01234567-89ab-cdef-0123-456789abcdef",
			SnapshotGeneration:  1,
			SnapshotRevision:    strings.Repeat("a", 64),
			DeviceID:            "usb-1-3",
			DeviceGeneration:    1,
			IdentityFingerprint: strings.Repeat("b", 64),
		},
		IMSI: "310260123456789",
	}
	if _, err := Build(input); err == nil {
		t.Fatal("a non-VOXI/Vodafone UK SIM must be rejected")
	}
}
