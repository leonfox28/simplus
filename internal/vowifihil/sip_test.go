package vowifihil

import (
	"net/netip"
	"strings"
	"testing"
)

func TestBuildAndParseIMSInitialRegistration(t *testing.T) {
	// 10.255.0.42 is a synthetic private IMS fixture, not an observed address.
	privateIdentity := "234150123456789@" + IMSHomeDomain
	input := IMSInitialRegisterInput{
		Source: netip.MustParseAddr("10.255.0.42"), UnprotectedPort: 42000,
		ProtectedClientPort: 42001, ProtectedServerPort: 42002,
		ClientSPI: 1234567890, ServerSPI: 2234567890,
		PrivateIdentity: privateIdentity, PublicIdentity: "sip:" + privateIdentity, HomeDomain: IMSHomeDomain,
		Branch: "0123456789abcdef", FromTag: "1123456789abcdef",
		CallID: "2123456789abcdef@10.255.0.42", ContactUser: "3123456789abcdef",
		WLANNodeID: "020000000001",
	}
	request, securityClient, err := BuildIMSInitialRegister(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"REGISTER sip:" + IMSHomeDomain + " SIP/2.0\r\n",
		"CSeq: 1 REGISTER\r\n", "expires=1800\r\n",
		"Authorization: Digest username=\"" + privateIdentity + "\", realm=\"" + IMSHomeDomain + "\", nonce=\"\"",
		"Security-Client: " + securityClient,
		"ealg=aes-cbc;prot=esp;mod=trans",
		"Require: sec-agree\r\n", "Proxy-Require: sec-agree\r\n",
		"P-Access-Network-Info: IEEE-802.11;i-wlan-node-id=020000000001\r\n",
	} {
		if !strings.Contains(string(request), required) {
			t.Fatalf("request missing %q", required)
		}
	}

	response := "SIP/2.0 401 Unauthorized\r\n" +
		"Call-ID: " + input.CallID + "\r\n" +
		"CSeq: 1 REGISTER\r\n" +
		"WWW-Authenticate: Digest realm=\"" + IMSHomeDomain + "\", nonce=\"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\", algorithm=AKAv1-MD5, qop=\"auth\"\r\n" +
		"Security-Server: ipsec-3gpp;q=0.1;alg=hmac-sha-1-96;ealg=aes-cbc;prot=esp;mod=trans;spi-c=3234567890;spi-s=4234567890;port-c=43001;port-s=43002\r\n" +
		"Content-Length: 0\r\n\r\n"
	summary, err := ParseIMSInitialResponse([]byte(response), input.CallID)
	if err != nil || summary.Status != 401 || summary.AKAAlgorithm != "AKAv1-MD5" || !summary.NonceValid ||
		summary.SecurityServerCandidates != 1 || !summary.UsableSecurityServer {
		t.Fatalf("summary=%#v error=%v", summary, err)
	}
}

func TestBuildAndParseIMSMinimumRegistrationInterval(t *testing.T) {
	privateIdentity := "234150123456789@" + IMSHomeDomain
	input := IMSInitialRegisterInput{
		Source: netip.MustParseAddr("10.255.0.42"), UnprotectedPort: 42000,
		ProtectedClientPort: 42001, ProtectedServerPort: 42002,
		ClientSPI: 1234567890, ServerSPI: 2234567890,
		PrivateIdentity: privateIdentity, PublicIdentity: "sip:" + privateIdentity, HomeDomain: IMSHomeDomain,
		Branch: "0123456789abcdef", FromTag: "1123456789abcdef",
		CallID: "2123456789abcdef@10.255.0.42", ContactUser: "3123456789abcdef",
		WLANNodeID: "020000000001",
	}
	request, _, err := BuildIMSInitialRegisterSequence(input, 2, 3600)
	if err != nil || !strings.Contains(string(request), "CSeq: 2 REGISTER\r\n") ||
		!strings.Contains(string(request), "expires=3600\r\n") {
		t.Fatalf("request=%q error=%v", request, err)
	}
	response := "SIP/2.0 423 Interval Too Brief\r\nCall-ID: " + input.CallID +
		"\r\nCSeq: 1 REGISTER\r\nMin-Expires: 3600\r\nContent-Length: 0\r\n\r\n"
	summary, err := ParseIMSInitialResponse([]byte(response), input.CallID)
	if err != nil || summary.Status != 423 || summary.MinExpires != 3600 {
		t.Fatalf("summary=%#v error=%v", summary, err)
	}
}

func TestBuildIMSInitialRegisterAdvertisesSMSCapabilityOnlyWhenProvisioned(t *testing.T) {
	privateIdentity := "234150123456789@" + IMSHomeDomain
	input := IMSInitialRegisterInput{
		Source: netip.MustParseAddr("10.255.0.42"), UnprotectedPort: 42000,
		ProtectedClientPort: 42001, ProtectedServerPort: 42002,
		ClientSPI: 1234567890, ServerSPI: 2234567890,
		PrivateIdentity: privateIdentity, PublicIdentity: "sip:" + privateIdentity, HomeDomain: IMSHomeDomain,
		Branch: "0123456789abcdef", FromTag: "1123456789abcdef",
		CallID: "2123456789abcdef@10.255.0.42", ContactUser: "3123456789abcdef",
		WLANNodeID: "020000000001", SMSCapable: true,
	}
	packet, _, err := BuildIMSInitialRegister(input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(packet), ";+g.3gpp.smsip\r\n") {
		t.Fatalf("SMS capability missing from Contact: %s", packet)
	}
	input.SMSCapable = false
	packet, _, err = BuildIMSInitialRegister(input)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(packet), "+g.3gpp.smsip") {
		t.Fatalf("unprovisioned SMS capability was advertised: %s", packet)
	}
}

func TestParseIMSInitialResponseRejectsMissingSecurityAndWrongTransaction(t *testing.T) {
	callID := "2123456789abcdef@10.255.0.42"
	response := "SIP/2.0 401 Unauthorized\r\nCall-ID: " + callID +
		"\r\nCSeq: 1 REGISTER\r\nWWW-Authenticate: Digest realm=\"" + IMSHomeDomain +
		"\", nonce=\"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\", algorithm=AKAv1-MD5\r\nContent-Length: 0\r\n\r\n"
	if _, err := ParseIMSInitialResponse([]byte(response), callID); err == nil {
		t.Fatal("accepted challenge without Security-Server")
	}
	if _, err := ParseIMSInitialResponse([]byte(response), "3123456789abcdef@10.255.0.42"); err == nil {
		t.Fatal("accepted wrong transaction")
	}
}
