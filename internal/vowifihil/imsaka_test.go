package vowifihil

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/netip"
	"strings"
	"testing"
)

func TestExtractChallengeAndBuildAuthenticatedRegister(t *testing.T) {
	// 10.255.0.42 is a synthetic private IMS fixture, not an observed address.
	callID := "2123456789abcdef@10.255.0.42"
	nonceBytes := make([]byte, 40)
	for index := range nonceBytes {
		nonceBytes[index] = byte(index + 1)
	}
	nonce := base64.StdEncoding.EncodeToString(nonceBytes)
	response := "SIP/2.0 401 Unauthorized\r\n" +
		"Call-ID: " + callID + "\r\n" +
		"CSeq: 1 REGISTER\r\n" +
		"WWW-Authenticate: Digest realm=\"" + testIMSHomeDomain + "\", nonce=\"" + nonce +
		"\", algorithm=AKAv1-MD5, qop=\"auth,auth-int\", opaque=\"opaque-token\"\r\n" +
		"Security-Server: ipsec-3gpp;q=0.1;alg=hmac-sha-1-96;ealg=aes-cbc;prot=esp;mod=trans;" +
		"spi-c=3234567890;spi-s=4234567890;port-c=43001;port-s=43002\r\n" +
		"Content-Length: 0\r\n\r\n"
	challenge, err := ExtractIMSRegistrationChallenge([]byte(response), callID, testIMSHomeDomain)
	if err != nil {
		t.Fatal(err)
	}
	if challenge.QOP != "auth" || challenge.RAND[0] != 1 || challenge.AUTN[0] != 17 ||
		challenge.SecurityServer.ClientSPI != 3234567890 || challenge.SecurityServer.ProtectedServerPort != 43002 {
		t.Fatalf("unexpected challenge metadata: qop=%q", challenge.QOP)
	}
	if _, err := ExtractIMSRegistrationChallenge([]byte(response), callID, alternateIMSHomeDomain); err == nil {
		t.Fatal("accepted an IMS challenge for a different Home Domain")
	}
	alternateResponse := strings.ReplaceAll(response, testIMSHomeDomain, alternateIMSHomeDomain)
	if alternate, err := ExtractIMSRegistrationChallenge([]byte(alternateResponse), callID, alternateIMSHomeDomain); err != nil || alternate.Realm != alternateIMSHomeDomain {
		t.Fatalf("alternate challenge = %#v, error = %v", alternate, err)
	}

	privateIdentity := "234150123456789@" + testIMSHomeDomain
	input := IMSInitialRegisterInput{
		Source: netip.MustParseAddr("10.255.0.42"), UnprotectedPort: 5060,
		ProtectedClientPort: 42001, ProtectedServerPort: 42002,
		ClientSPI: 1234567890, ServerSPI: 2234567890,
		PrivateIdentity: privateIdentity, PublicIdentity: "sip:" + privateIdentity, HomeDomain: testIMSHomeDomain,
		Branch: "0123456789abcdef", FromTag: "1123456789abcdef", CallID: callID,
		ContactUser: "3123456789abcdef", WLANNodeID: "020000000001",
	}
	securityClient := "ipsec-3gpp;alg=hmac-sha-1-96;ealg=aes-cbc;prot=esp;mod=trans;" +
		"spi-c=1234567890;spi-s=2234567890;port-c=42001;port-s=42002"
	request, err := BuildIMSAuthenticatedRegister(input, challenge, []byte("binary-res"), securityClient,
		"4123456789abcdef", "5123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"CSeq: 2 REGISTER\r\n",
		"Via: SIP/2.0/UDP 10.255.0.42:42002;",
		"Contact: <sip:10.255.0.42:42002>",
		"algorithm=AKAv1-MD5, qop=auth, nc=00000001",
		"Security-Client: " + securityClient + "\r\n",
		"Security-Verify: " + challenge.SecurityServer.Raw + "\r\n",
	} {
		if !strings.Contains(string(request), expected) {
			t.Fatalf("authenticated request missing %q", expected)
		}
	}

	ok := "SIP/2.0 200 OK\r\nCall-ID: " + callID + "\r\nCSeq: 2 REGISTER\r\nContent-Length: 0\r\n\r\n"
	status, err := ParseIMSAuthenticatedResponse([]byte(ok), callID)
	if err != nil || status != 200 {
		t.Fatalf("status=%d error=%v", status, err)
	}
}

func TestBuildIMSSynchronizationRegisterUsesAUTSAndUnprotectedFlow(t *testing.T) {
	privateIdentity := "234150123456789@" + testIMSHomeDomain
	input := IMSInitialRegisterInput{
		Source: netip.MustParseAddr("10.255.0.42"), UnprotectedPort: 42000,
		ProtectedClientPort: 42001, ProtectedServerPort: 42002,
		ClientSPI: 1234567890, ServerSPI: 2234567890,
		PrivateIdentity: privateIdentity, PublicIdentity: "sip:" + privateIdentity, HomeDomain: testIMSHomeDomain,
		Branch: "0123456789abcdef", FromTag: "1123456789abcdef",
		CallID: "2123456789abcdef@10.255.0.42", ContactUser: "3123456789abcdef",
		WLANNodeID: "020000000001", SMSCapable: true,
	}
	challenge := IMSRegistrationChallenge{
		Algorithm: "AKAv1-MD5", Realm: testIMSHomeDomain, Nonce: "synthetic-nonce",
		QOP: "auth", Opaque: "synthetic-opaque",
	}
	securityClient := "ipsec-3gpp;alg=hmac-sha-1-96;ealg=aes-cbc;prot=esp;mod=trans;" +
		"spi-c=1234567890;spi-s=2234567890;port-c=42001;port-s=42002"
	var auts [imsAUTSLength]byte
	for index := range auts {
		auts[index] = byte(index + 1)
	}
	packet, err := buildIMSSynchronizationRegisterSequence(input, challenge, auts, securityClient,
		"4123456789abcdef", "5123456789abcdef", 2, 1, DefaultIMSRegistrationExpires)
	if err != nil {
		t.Fatal(err)
	}
	request := string(packet)
	for _, expected := range []string{
		"CSeq: 2 REGISTER\r\n",
		"Via: SIP/2.0/UDP 10.255.0.42:42000;",
		"Contact: <sip:10.255.0.42:42000>;expires=1800;+g.3gpp.smsip",
		"auts=\"AQIDBAUGBwgJCgsMDQ4=\"",
		"qop=auth, nc=00000001, cnonce=\"5123456789abcdef\"",
		"Security-Client: " + securityClient + "\r\n",
	} {
		if !strings.Contains(request, expected) {
			t.Fatalf("synchronization request missing %q", expected)
		}
	}
	if strings.Contains(request, hex.EncodeToString(auts[:])) || strings.Contains(request, "Security-Verify:") {
		t.Fatal("synchronization request exposed hex AUTS or used a protected-flow header")
	}
	parameters := authorizationParameters(t, request)
	const wantResponse = "14b9eb1ee911aa22c68c1db90ba77310"
	if parameters["response"] != wantResponse || parameters["response"] == "" {
		t.Fatalf("synchronization digest response = %q, want empty-password digest %q", parameters["response"], wantResponse)
	}
	if got := digestAKAResponse(privateIdentity, testIMSHomeDomain, nil, challenge.Nonce,
		"00000001", "5123456789abcdef", "auth", "REGISTER", "sip:"+testIMSHomeDomain, nil); got != "" {
		t.Fatalf("ordinary AKA digest accepted an empty password: %q", got)
	}
}

func TestIMSAKASynchronizationFailureConsumesSecretWithoutLoggingIt(t *testing.T) {
	failure := &imsAKASynchronizationFailure{}
	for index := range failure.auts {
		failure.auts[index] = byte(index + 1)
	}
	if strings.Contains(failure.Error(), hex.EncodeToString(failure.auts[:])) ||
		strings.Contains(failure.Error(), base64.StdEncoding.EncodeToString(failure.auts[:])) {
		t.Fatal("AUTS escaped through the synchronization error")
	}
	var typed *imsAKASynchronizationFailure
	if !errors.As(error(failure), &typed) {
		t.Fatal("synchronization failure is not discoverable internally")
	}
	auts := typed.consumeAUTS()
	defer zeroBytes(auts[:])
	if auts[0] != 1 || auts[len(auts)-1] != imsAUTSLength {
		t.Fatalf("consumed AUTS = %x", auts)
	}
	for _, current := range typed.auts {
		if current != 0 {
			t.Fatal("AUTS remained in the error after consumption")
		}
	}
	second := &imsAKASynchronizationFailure{}
	second.auts[0] = 42
	if !DiscardIMSAKASynchronizationFailure(second) || second.auts[0] != 0 ||
		DiscardIMSAKASynchronizationFailure(errors.New("ordinary error")) {
		t.Fatal("synchronization failure discard did not erase exactly the typed error")
	}
}

func TestExtractIMSRegistrationChallengeRequiresMatchingSequence(t *testing.T) {
	callID := "2123456789abcdef@10.255.0.42"
	nonce := base64.StdEncoding.EncodeToString(make([]byte, 32))
	response := "SIP/2.0 401 Unauthorized\r\nCall-ID: " + callID +
		"\r\nCSeq: 2 REGISTER\r\nWWW-Authenticate: Digest realm=\"" + testIMSHomeDomain +
		"\", nonce=\"" + nonce + "\", algorithm=AKAv1-MD5\r\n" +
		"Security-Server: ipsec-3gpp;alg=hmac-sha-1-96;ealg=aes-cbc;prot=esp;mod=trans;" +
		"spi-c=3234567890;spi-s=4234567890;port-c=43001;port-s=43002\r\nContent-Length: 0\r\n\r\n"
	if _, err := ExtractIMSRegistrationChallenge([]byte(response), callID, testIMSHomeDomain); err == nil {
		t.Fatal("default challenge parser accepted a different CSeq")
	}
	if _, err := extractIMSRegistrationChallengeSequence([]byte(response), callID, testIMSHomeDomain, 2); err != nil {
		t.Fatalf("sequence-aware challenge parser rejected CSeq 2: %v", err)
	}
}

func authorizationParameters(t *testing.T, request string) map[string]string {
	t.Helper()
	const prefix = "Authorization: Digest "
	start := strings.Index(request, prefix)
	if start < 0 {
		t.Fatal("Authorization header is absent")
	}
	value := request[start+len(prefix):]
	if end := strings.Index(value, "\r\n"); end >= 0 {
		value = value[:end]
	}
	return parseSIPParameters(value, ',')
}

func TestDigestAKAResponseMatchesRFC2617Example(t *testing.T) {
	response := digestAKAResponse("Mufasa", "testrealm@host.com", []byte("Circle Of Life"),
		"dcd98b7102dd2f0e8b11d0f600bfb0c093", "00000001", "0a4f113b", "auth",
		"GET", "/dir/index.html", nil)
	if response != "6629fae49393a05397450978507c4ef1" {
		t.Fatalf("unexpected digest response %q", response)
	}
}

func TestExtractChallengeRejectsIncompleteIPSecMechanism(t *testing.T) {
	callID := "2123456789abcdef@10.255.0.42"
	nonce := base64.StdEncoding.EncodeToString(make([]byte, 32))
	response := "SIP/2.0 401 Unauthorized\r\nCall-ID: " + callID +
		"\r\nCSeq: 1 REGISTER\r\nWWW-Authenticate: Digest realm=\"" + testIMSHomeDomain +
		"\", nonce=\"" + nonce + "\", algorithm=AKAv1-MD5\r\n" +
		"Security-Server: ipsec-3gpp;alg=hmac-sha-1-96;spi-c=3234567890;spi-s=4234567890;" +
		"port-c=43001;port-s=43002\r\nContent-Length: 0\r\n\r\n"
	if _, err := ExtractIMSRegistrationChallenge([]byte(response), callID, testIMSHomeDomain); err == nil {
		t.Fatal("accepted Security-Server without negotiated encryption")
	}
}
