package vowifihil

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/leonfox28/simplus/internal/agentapi"
)

const (
	imsRANDLength = 16
	imsAUTNLength = 16
	imsCKLength   = 16
	imsIKLength   = 16
)

// IMSRegistrationChallenge contains transient IMS AKA authentication
// material. It must never be logged, persisted or returned through Web/API
// boundaries.
type IMSRegistrationChallenge struct {
	Algorithm      string
	Realm          string
	Nonce          string
	QOP            string
	Opaque         string
	RAND           [imsRANDLength]byte
	AUTN           [imsAUTNLength]byte
	SecurityServer IMSIPSecParameters
}

// IMSIPSecParameters is the negotiated Security-Server mechanism. Raw is
// retained only so the protected REGISTER can mirror it in Security-Verify.
type IMSIPSecParameters struct {
	Raw                 string
	Authentication      string
	Encryption          string
	Protocol            string
	Mode                string
	ClientSPI           uint32
	ServerSPI           uint32
	ProtectedClientPort uint16
	ProtectedServerPort uint16
}

// IMSAKAMaterial is transient root-only material returned by the SIM. Call
// Destroy as soon as the Gm SAs and Digest response have been constructed.
type IMSAKAMaterial struct {
	RES []byte
	CK  [imsCKLength]byte
	IK  [imsIKLength]byte
}

func (material *IMSAKAMaterial) Destroy() {
	if material == nil {
		return
	}
	zeroBytes(material.RES)
	zeroBytes(material.CK[:])
	zeroBytes(material.IK[:])
}

func ExtractIMSRegistrationChallenge(packet []byte, expectedCallID, expectedHomeDomain string) (IMSRegistrationChallenge, error) {
	status, headers, err := parseSIPResponse(packet)
	if err != nil || status != 401 || !matchingRegisterTransaction(headers, expectedCallID, 1) ||
		!agentapi.IsValidIMSHomeDomain(expectedHomeDomain) {
		return IMSRegistrationChallenge{}, errors.New("invalid IMS AKA challenge")
	}
	challenge, err := extractAKAv1Challenge(headers["www-authenticate"], expectedHomeDomain)
	if err != nil {
		return IMSRegistrationChallenge{}, err
	}
	server, err := selectIMSIPSecParameters(headers["security-server"])
	if err != nil {
		return IMSRegistrationChallenge{}, err
	}
	challenge.SecurityServer = server
	return challenge, nil
}

func extractAKAv1Challenge(values []string, expectedRealm string) (IMSRegistrationChallenge, error) {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if len(value) < 7 || !strings.EqualFold(value[:7], "Digest ") {
			continue
		}
		parameters := parseSIPParameters(value[7:], ',')
		if parameters["algorithm"] != "AKAv1-MD5" || parameters["realm"] != expectedRealm {
			continue
		}
		nonce := parameters["nonce"]
		decoded, err := base64.StdEncoding.DecodeString(nonce)
		if err != nil || len(decoded) < imsRANDLength+imsAUTNLength || len(decoded) > 256 {
			zeroBytes(decoded)
			return IMSRegistrationChallenge{}, errors.New("invalid IMS AKA nonce")
		}
		challenge := IMSRegistrationChallenge{
			Algorithm: "AKAv1-MD5", Realm: expectedRealm, Nonce: nonce,
			QOP: selectDigestQOP(parameters["qop"]), Opaque: parameters["opaque"],
		}
		copy(challenge.RAND[:], decoded[:imsRANDLength])
		copy(challenge.AUTN[:], decoded[imsRANDLength:imsRANDLength+imsAUTNLength])
		zeroBytes(decoded)
		if parameters["qop"] != "" && challenge.QOP == "" || !validDigestValue(challenge.Opaque, 1024) {
			zeroBytes(challenge.RAND[:])
			zeroBytes(challenge.AUTN[:])
			return IMSRegistrationChallenge{}, errors.New("unsupported IMS AKA challenge")
		}
		return challenge, nil
	}
	return IMSRegistrationChallenge{}, errors.New("supported IMS AKA challenge is absent")
}

func selectDigestQOP(value string) string {
	for _, qop := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(qop), "auth") {
			return "auth"
		}
	}
	for _, qop := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(qop), "auth-int") {
			return "auth-int"
		}
	}
	return ""
}

func selectIMSIPSecParameters(values []string) (IMSIPSecParameters, error) {
	for _, value := range values {
		for _, mechanism := range splitSIPValue(value, ',') {
			raw := strings.TrimSpace(mechanism)
			if raw == "" || len(raw) > 2048 || !validDigestValue(raw, 2048) {
				continue
			}
			parts := splitSIPValue(raw, ';')
			if len(parts) < 2 || !strings.EqualFold(strings.TrimSpace(parts[0]), "ipsec-3gpp") {
				continue
			}
			parameters := parseSIPParameters(strings.Join(parts[1:], ";"), ';')
			protocol := strings.ToLower(parameters["prot"])
			if protocol == "" {
				protocol = "esp"
			}
			mode := strings.ToLower(parameters["mod"])
			if mode == "" {
				mode = "trans"
			}
			parsed := IMSIPSecParameters{
				Raw: raw, Authentication: strings.ToLower(parameters["alg"]),
				Encryption: strings.ToLower(parameters["ealg"]), Protocol: protocol, Mode: mode,
			}
			if parsed.Authentication != "hmac-sha-1-96" || parsed.Encryption != "aes-cbc" ||
				parsed.Protocol != "esp" || parsed.Mode != "trans" ||
				!parseSecurityServerNumbers(parameters, &parsed) {
				continue
			}
			return parsed, nil
		}
	}
	return IMSIPSecParameters{}, errors.New("supported IMS IPsec mechanism is absent")
}

func parseSecurityServerNumbers(parameters map[string]string, output *IMSIPSecParameters) bool {
	spiClient, clientErr := parseUint32Decimal(parameters["spi-c"])
	spiServer, serverErr := parseUint32Decimal(parameters["spi-s"])
	portClient, portClientErr := parseUint16Decimal(parameters["port-c"])
	portServer, portServerErr := parseUint16Decimal(parameters["port-s"])
	if clientErr != nil || serverErr != nil || portClientErr != nil || portServerErr != nil ||
		spiClient == 0 || spiServer == 0 || spiClient == spiServer || portClient == 0 || portServer == 0 ||
		portClient == IMSSIPPort || portServer == IMSSIPPort || portClient == portServer {
		return false
	}
	output.ClientSPI = spiClient
	output.ServerSPI = spiServer
	output.ProtectedClientPort = portClient
	output.ProtectedServerPort = portServer
	return true
}

func AuthenticateIMSChallenge(ctx context.Context, target agentapi.SIMAKATarget, challenge IMSRegistrationChallenge) (IMSAKAMaterial, string, error) {
	client, err := agentapi.NewClient(SIMAKASocket)
	if err != nil {
		return IMSAKAMaterial{}, "unavailable", err
	}
	hello, err := client.Hello(ctx)
	if err != nil || hello.AgentInstanceID != target.AgentInstanceID ||
		!containsString(hello.Features, agentapi.FeatureSIMAKAHIL) {
		return IMSAKAMaterial{}, "unavailable", errors.New("SIM AKA socket is not current")
	}
	exchangeID, err := randomHexToken(16)
	if err != nil {
		return IMSAKAMaterial{}, "unavailable", err
	}
	request := agentapi.SIMAKAAuthenticationRequest{
		SIMAKATarget: target, ExchangeID: exchangeID,
		RAND: hex.EncodeToString(challenge.RAND[:]), AUTN: hex.EncodeToString(challenge.AUTN[:]),
	}
	response, err := client.AuthenticateSIMAKA(ctx, request)
	if err != nil {
		return IMSAKAMaterial{}, "unavailable", err
	}
	if response.Result.State != agentapi.SIMAKAStateSuccess {
		return IMSAKAMaterial{}, response.Result.State, errors.New("IMS AKA did not produce session keys")
	}
	res, resErr := hex.DecodeString(response.Result.RES)
	ck, ckErr := hex.DecodeString(response.Result.CK)
	ik, ikErr := hex.DecodeString(response.Result.IK)
	if resErr != nil || ckErr != nil || ikErr != nil || len(res) < 4 || len(res) > 16 ||
		len(ck) != imsCKLength || len(ik) != imsIKLength {
		zeroBytes(res)
		zeroBytes(ck)
		zeroBytes(ik)
		return IMSAKAMaterial{}, "unavailable", errors.New("invalid IMS AKA material")
	}
	material := IMSAKAMaterial{RES: res}
	copy(material.CK[:], ck)
	copy(material.IK[:], ik)
	zeroBytes(ck)
	zeroBytes(ik)
	return material, agentapi.SIMAKAStateSuccess, nil
}

func BuildIMSAuthenticatedRegister(input IMSInitialRegisterInput, challenge IMSRegistrationChallenge,
	res []byte, securityClient, branch, cnonce string) ([]byte, error) {
	return BuildIMSAuthenticatedRegisterSequence(input, challenge, res, securityClient, branch, cnonce, 2, 1, DefaultIMSRegistrationExpires)
}

func BuildIMSAuthenticatedRegisterSequence(input IMSInitialRegisterInput, challenge IMSRegistrationChallenge,
	res []byte, securityClient, branch, cnonce string, sequence, nonceCount uint64, expires uint32) ([]byte, error) {
	if err := validateIMSInitialRegisterInput(input); err != nil ||
		challenge.Algorithm != "AKAv1-MD5" || challenge.Realm != input.HomeDomain || challenge.Nonce == "" ||
		challenge.SecurityServer.Raw == "" || len(res) < 4 || len(res) > 16 ||
		!validIMSToken(branch, 16, 64) || challenge.QOP != "" && !validIMSToken(cnonce, 16, 64) ||
		securityClient == "" || len(securityClient) > 2048 || !validDigestValue(securityClient, 2048) ||
		sequence < 2 || sequence > 1<<31-1 || nonceCount == 0 || nonceCount > 0xffffffff || expires < 60 || expires > 86400 {
		return nil, errors.New("invalid authenticated IMS REGISTER input")
	}
	requestURI := "sip:" + input.HomeDomain
	nonceCountValue := fmt.Sprintf("%08x", nonceCount)
	response := digestAKAResponse(input.PrivateIdentity, challenge.Realm, res, challenge.Nonce,
		nonceCountValue, cnonce, challenge.QOP, "REGISTER", requestURI, nil)
	if response == "" {
		return nil, errors.New("build IMS AKA digest response")
	}
	var authorization strings.Builder
	fmt.Fprintf(&authorization, "Digest username=\"%s\", realm=\"%s\", nonce=\"%s\", uri=\"%s\", response=\"%s\", algorithm=%s",
		input.PrivateIdentity, challenge.Realm, challenge.Nonce, requestURI, response, challenge.Algorithm)
	if challenge.QOP != "" {
		fmt.Fprintf(&authorization, ", qop=%s, nc=%s, cnonce=\"%s\"", challenge.QOP, nonceCountValue, cnonce)
	}
	if challenge.Opaque != "" {
		fmt.Fprintf(&authorization, ", opaque=\"%s\"", challenge.Opaque)
	}

	source := input.Source.String()
	var message strings.Builder
	fmt.Fprintf(&message, "REGISTER %s SIP/2.0\r\n", requestURI)
	fmt.Fprintf(&message, "Via: SIP/2.0/UDP %s:%d;branch=z9hG4bK%s;rport\r\n", source, input.ProtectedServerPort, branch)
	message.WriteString("Max-Forwards: 70\r\n")
	fmt.Fprintf(&message, "From: <%s>;tag=%s\r\n", input.PublicIdentity, input.FromTag)
	fmt.Fprintf(&message, "To: <%s>\r\n", input.PublicIdentity)
	fmt.Fprintf(&message, "Call-ID: %s\r\n", input.CallID)
	fmt.Fprintf(&message, "CSeq: %d REGISTER\r\n", sequence)
	fmt.Fprintf(&message, "Contact: <sip:%s:%d>;expires=%d%s\r\n", source, input.ProtectedServerPort, expires, smsCapabilityParameter(input.SMSCapable))
	fmt.Fprintf(&message, "Authorization: %s\r\n", authorization.String())
	fmt.Fprintf(&message, "Security-Client: %s\r\n", securityClient)
	fmt.Fprintf(&message, "Security-Verify: %s\r\n", challenge.SecurityServer.Raw)
	message.WriteString("Require: sec-agree\r\n")
	message.WriteString("Proxy-Require: sec-agree\r\n")
	message.WriteString("Supported: path\r\n")
	fmt.Fprintf(&message, "P-Access-Network-Info: IEEE-802.11;i-wlan-node-id=%s\r\n", input.WLANNodeID)
	message.WriteString("Content-Length: 0\r\n\r\n")
	return []byte(message.String()), nil
}

func ParseIMSAuthenticatedResponse(packet []byte, expectedCallID string) (int, error) {
	return ParseIMSAuthenticatedResponseSequence(packet, expectedCallID, 2)
}

func ParseIMSAuthenticatedResponseSequence(packet []byte, expectedCallID string, sequence uint64) (int, error) {
	status, headers, err := parseSIPResponse(packet)
	if err != nil || !matchingRegisterTransaction(headers, expectedCallID, sequence) {
		return 0, errors.New("invalid authenticated IMS response")
	}
	return status, nil
}

func matchingRegisterTransaction(headers map[string][]string, expectedCallID string, sequence uint64) bool {
	if !validIMSCallID(expectedCallID) {
		return false
	}
	callIDs := headers["call-id"]
	if len(callIDs) != 1 || strings.TrimSpace(callIDs[0]) != expectedCallID {
		return false
	}
	cseq := headers["cseq"]
	if len(cseq) != 1 {
		return false
	}
	fields := strings.Fields(cseq[0])
	return len(fields) == 2 && fields[0] == fmt.Sprint(sequence) && strings.EqualFold(fields[1], "REGISTER")
}

func digestAKAResponse(username, realm string, password []byte, nonce, nonceCount, cnonce, qop,
	method, uri string, entity []byte) string {
	if username == "" || realm == "" || len(password) == 0 || nonce == "" || method == "" || uri == "" ||
		qop != "" && qop != "auth" && qop != "auth-int" ||
		qop != "" && (nonceCount == "" || cnonce == "") {
		return ""
	}
	ha1 := md5.New()
	ha1.Write([]byte(username))
	ha1.Write([]byte{':'})
	ha1.Write([]byte(realm))
	ha1.Write([]byte{':'})
	ha1.Write(password)
	ha1Hex := hex.EncodeToString(ha1.Sum(nil))

	ha2 := md5.New()
	ha2.Write([]byte(method))
	ha2.Write([]byte{':'})
	ha2.Write([]byte(uri))
	if qop == "auth-int" {
		entityHash := md5.Sum(entity)
		ha2.Write([]byte{':'})
		ha2.Write([]byte(hex.EncodeToString(entityHash[:])))
	}
	ha2Hex := hex.EncodeToString(ha2.Sum(nil))

	response := md5.New()
	response.Write([]byte(ha1Hex))
	response.Write([]byte{':'})
	response.Write([]byte(nonce))
	if qop != "" {
		response.Write([]byte{':'})
		response.Write([]byte(nonceCount))
		response.Write([]byte{':'})
		response.Write([]byte(cnonce))
		response.Write([]byte{':'})
		response.Write([]byte(qop))
	}
	response.Write([]byte{':'})
	response.Write([]byte(ha2Hex))
	return hex.EncodeToString(response.Sum(nil))
}

func randomHexToken(size int) (string, error) {
	if size < 8 || size > 64 {
		return "", errors.New("invalid random token size")
	}
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	defer zeroBytes(value)
	return hex.EncodeToString(value), nil
}

func parseUint32Decimal(value string) (uint32, error) {
	parsed, err := strconv.ParseUint(value, 10, 32)
	return uint32(parsed), err
}

func parseUint16Decimal(value string) (uint16, error) {
	parsed, err := strconv.ParseUint(value, 10, 16)
	return uint16(parsed), err
}

func zeroBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func validDigestValue(value string, maximum int) bool {
	if len(value) > maximum {
		return false
	}
	return strings.IndexFunc(value, func(current rune) bool {
		return current < 0x21 || current > 0x7e || current == '"' || current == '\\'
	}) < 0
}
