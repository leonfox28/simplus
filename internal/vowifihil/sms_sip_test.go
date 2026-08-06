package vowifihil

import (
	"bytes"
	"net/netip"
	"strings"
	"testing"
)

func TestSMSIPMessageBuildParseAndTransactionMatch(t *testing.T) {
	body := []byte{0x00, 0x17, 0x00, 0x02, 0x91, 0x44, 0x01, 0x00}
	input := smsSIPRequestInput{
		Source: netip.MustParseAddr("10.255.0.42"), ViaPort: 42002,
		RequestURI: "tel:+447700900123", PublicIdentity: "sip:234150123456789@" + testIMSHomeDomain,
		Branch: "0123456789abcdef", FromTag: "1123456789abcdef",
		CallID: "2123456789abcdef@10.255.0.42", InReplyTo: "incoming0123456789@pcscf", Sequence: 7,
		Routes:         []string{"<sip:pcscf.example.invalid;lr>"},
		SecurityVerify: "ipsec-3gpp;alg=hmac-sha-1-96;ealg=aes-cbc;prot=esp;mod=trans;spi-c=1;spi-s=2;port-c=3;port-s=4",
		WLANNodeID:     "020000000001", Body: body,
	}
	packet, err := buildSMSIPMessage(input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(packet, body) || !strings.Contains(string(packet), "Content-Type: "+smsOverIPContentType+"\r\n") ||
		!strings.Contains(string(packet), "Route: <sip:pcscf.example.invalid;lr>\r\n") ||
		!strings.Contains(string(packet), "In-Reply-To: incoming0123456789@pcscf\r\n") ||
		!strings.Contains(string(packet), "Allow: MESSAGE\r\n") ||
		!strings.Contains(string(packet), "Request-Disposition: no-fork\r\n") {
		t.Fatalf("packet = %q", packet)
	}
	responseBytes := []byte("SIP/2.0 202 Accepted\r\nCall-ID: " + input.CallID + "\r\nCSeq: 7 MESSAGE\r\nContent-Length: 0\r\n\r\n")
	response, err := parseSIPPacket(responseBytes)
	if err != nil || !matchingSIPResponse(response, input.CallID, 7, "MESSAGE") {
		t.Fatalf("response=%#v error=%v", response, err)
	}
}

func TestSIPIdentityURIParsingPreservesAssertedIdentityParameters(t *testing.T) {
	const asserted = "sip:gateway.example.invalid;user=phone"
	if got := firstSIPAssertedIdentityURI([]string{asserted}); got != asserted {
		t.Fatalf("asserted identity = %q", got)
	}
	if got := firstSIPURI([]string{"sip:user@example.invalid;tag=network"}); got != "sip:user@example.invalid" {
		t.Fatalf("To identity = %q", got)
	}
}

func TestParseSMSIPRequestAndBuildResponsePreserveTransaction(t *testing.T) {
	body := []byte{0x01, 0x29, 0x02, 0x91, 0x44, 0x00, 0x01, 0x00}
	head := "MESSAGE sip:10.255.0.42:42002 SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP pcscf.example.invalid;branch=z9hG4bKone\r\n" +
		"Via: SIP/2.0/UDP ipsmgw.example.invalid;branch=z9hG4bKtwo\r\n" +
		"From: <sip:ipsmgw.example.invalid>;tag=remote\r\n" +
		"To: <sip:234150123456789@" + testIMSHomeDomain + ">\r\n" +
		"Call-ID: 2123456789abcdef@pcscf\r\n" +
		"CSeq: 8 MESSAGE\r\n" +
		"P-Asserted-Identity: <sip:ipsmgw.example.invalid>\r\n" +
		"Content-Type: application/vnd.3gpp.sms\r\n" +
		"Content-Length: 8\r\n\r\n"
	request, err := parseSMSIPRequest(append([]byte(head), body...))
	if err != nil || firstSIPURI(request.Headers["p-asserted-identity"]) != "sip:ipsmgw.example.invalid" {
		t.Fatalf("request=%#v error=%v", request, err)
	}
	response, err := buildSIPResponse(request, 200, "0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"SIP/2.0 200 OK\r\n", "Via: SIP/2.0/UDP pcscf.example.invalid", "Via: SIP/2.0/UDP ipsmgw.example.invalid",
		"To: <sip:234150123456789@" + testIMSHomeDomain + ">;tag=0123456789abcdef\r\n",
		"Call-ID: 2123456789abcdef@pcscf\r\n", "CSeq: 8 MESSAGE\r\n", "Content-Length: 0\r\n\r\n",
	} {
		if !strings.Contains(string(response), required) {
			t.Fatalf("response missing %q: %q", required, response)
		}
	}
}

func TestParseSMSIPRequestAcceptsNetworkCallIDWithoutHostPart(t *testing.T) {
	body := []byte{0x03, 0x07}
	packet := "MESSAGE sip:10.255.0.42:42002 SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP pcscf.example.invalid;branch=z9hG4bKone\r\n" +
		"From: <sip:ipsmgw.example.invalid>;tag=remote\r\n" +
		"To: <sip:user@example.invalid>\r\n" +
		"Call-ID: fy365h43g3f36f3f6fth74g3\r\nCSeq: 8 MESSAGE\r\n" +
		"Content-Type: application/vnd.3gpp.sms\r\nContent-Length: 2\r\n\r\n"
	if _, err := parseSMSIPRequest(append([]byte(packet), body...)); err != nil {
		t.Fatalf("network Call-ID rejected: %v", err)
	}
}

func TestSIPCallIDUsesRFC3261WordGrammar(t *testing.T) {
	for _, value := range []string{
		"x", "operator:[2001:db8::1]/report?{one}@host.example", `quoted\"word@host`,
	} {
		if !validSIPCallID(value) {
			t.Fatalf("valid Call-ID rejected: %q", value)
		}
	}
	for _, value := range []string{"", "a@b@c", "space value", "comma,value", "semi;value", "equal=value"} {
		if validSIPCallID(value) {
			t.Fatalf("invalid Call-ID accepted: %q", value)
		}
	}
}

func TestSMSIPParserRejectsContentAndHeaderSmuggling(t *testing.T) {
	if _, err := parseSMSIPRequest([]byte("MESSAGE tel:+123 SIP/2.0\r\nContent-Length: 0\r\n\r\n")); err == nil {
		t.Fatal("accepted incomplete SMS SIP request")
	}
	for _, packet := range [][]byte{
		[]byte("SIP/2.0 202 Accepted\r\nCall-ID: x\r\nContent-Length: 1\r\n\r\n"),
		[]byte("SIP/2.0 202 Accepted\r\nX-Test: ok\rInjected: yes\r\nContent-Length: 0\r\n\r\n"),
	} {
		if _, err := parseSIPPacket(packet); err == nil {
			t.Fatalf("accepted malformed SIP packet %q", packet)
		}
	}
}
