package vowifihil

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

const (
	IMSHomeDomain                 = "ims.mnc015.mcc234.3gppnetwork.org"
	IMSSIPPort                    = 5060
	DefaultIMSRegistrationExpires = uint32(1800)
)

type IMSInitialRegisterInput struct {
	Source              netip.Addr
	UnprotectedPort     uint16
	ProtectedClientPort uint16
	ProtectedServerPort uint16
	ClientSPI           uint32
	ServerSPI           uint32
	PrivateIdentity     string
	PublicIdentity      string
	HomeDomain          string
	Branch              string
	FromTag             string
	CallID              string
	ContactUser         string
	WLANNodeID          string
}

type IMSInitialResponseSummary struct {
	Status                   int    `json:"status"`
	MinExpires               uint32 `json:"minExpires,omitempty"`
	AKAAlgorithm             string `json:"akaAlgorithm,omitempty"`
	NonceValid               bool   `json:"nonceValid"`
	SecurityServerCandidates int    `json:"securityServerCandidates"`
	UsableSecurityServer     bool   `json:"usableSecurityServer"`
}

func BuildIMSInitialRegister(input IMSInitialRegisterInput) ([]byte, string, error) {
	return BuildIMSInitialRegisterSequence(input, 1, DefaultIMSRegistrationExpires)
}

func BuildIMSInitialRegisterSequence(input IMSInitialRegisterInput, sequence uint64, expires uint32) ([]byte, string, error) {
	if err := validateIMSInitialRegisterInput(input); err != nil {
		return nil, "", err
	}
	if sequence == 0 || sequence > 1<<31-1 || expires < 60 || expires > 86400 {
		return nil, "", errors.New("invalid IMS initial REGISTER interval")
	}
	securityClient := fmt.Sprintf("ipsec-3gpp;alg=hmac-sha-1-96;ealg=aes-cbc;prot=esp;mod=trans;spi-c=%010d;spi-s=%010d;port-c=%d;port-s=%d",
		input.ClientSPI, input.ServerSPI, input.ProtectedClientPort, input.ProtectedServerPort)
	requestURI := "sip:" + input.HomeDomain
	source := input.Source.String()
	var message strings.Builder
	fmt.Fprintf(&message, "REGISTER %s SIP/2.0\r\n", requestURI)
	fmt.Fprintf(&message, "Via: SIP/2.0/UDP %s:%d;branch=z9hG4bK%s;rport\r\n", source, input.UnprotectedPort, input.Branch)
	message.WriteString("Max-Forwards: 70\r\n")
	fmt.Fprintf(&message, "From: <%s>;tag=%s\r\n", input.PublicIdentity, input.FromTag)
	fmt.Fprintf(&message, "To: <%s>\r\n", input.PublicIdentity)
	fmt.Fprintf(&message, "Call-ID: %s\r\n", input.CallID)
	fmt.Fprintf(&message, "CSeq: %d REGISTER\r\n", sequence)
	fmt.Fprintf(&message, "Contact: <sip:%s:%d>;expires=%d\r\n", source, input.UnprotectedPort, expires)
	fmt.Fprintf(&message, "Authorization: Digest username=\"%s\", realm=\"%s\", nonce=\"\", uri=\"%s\", response=\"\"\r\n",
		input.PrivateIdentity, input.HomeDomain, requestURI)
	fmt.Fprintf(&message, "Security-Client: %s\r\n", securityClient)
	message.WriteString("Require: sec-agree\r\n")
	message.WriteString("Proxy-Require: sec-agree\r\n")
	message.WriteString("Supported: path\r\n")
	fmt.Fprintf(&message, "P-Access-Network-Info: IEEE-802.11;i-wlan-node-id=%s\r\n", input.WLANNodeID)
	message.WriteString("Content-Length: 0\r\n\r\n")
	return []byte(message.String()), securityClient, nil
}

func ParseIMSInitialResponse(packet []byte, expectedCallID string) (IMSInitialResponseSummary, error) {
	status, headers, err := parseSIPResponse(packet)
	if err != nil || !validIMSCallID(expectedCallID) {
		return IMSInitialResponseSummary{}, errors.New("invalid SIP response")
	}
	callIDs := headers["call-id"]
	if len(callIDs) != 1 || strings.TrimSpace(callIDs[0]) != expectedCallID {
		return IMSInitialResponseSummary{}, errors.New("unmatched SIP response")
	}
	cseq := headers["cseq"]
	if len(cseq) != 1 || !validRegisterCSeq(cseq[0]) {
		return IMSInitialResponseSummary{}, errors.New("invalid SIP transaction")
	}
	summary := IMSInitialResponseSummary{Status: status}
	if status == 423 {
		values := headers["min-expires"]
		if len(values) != 1 {
			return summary, errors.New("invalid IMS minimum registration interval")
		}
		minimum, parseErr := strconv.ParseUint(strings.TrimSpace(values[0]), 10, 32)
		if parseErr != nil || minimum < 60 || minimum > 86400 {
			return summary, errors.New("invalid IMS minimum registration interval")
		}
		summary.MinExpires = uint32(minimum)
		return summary, nil
	}
	if status != 401 {
		return summary, nil
	}
	algorithm, nonceValid := inspectAKAChallenge(headers["www-authenticate"])
	summary.AKAAlgorithm = algorithm
	summary.NonceValid = nonceValid
	for _, value := range headers["security-server"] {
		for _, mechanism := range splitSIPValue(value, ',') {
			if strings.TrimSpace(mechanism) == "" {
				continue
			}
			summary.SecurityServerCandidates++
			if usableIPSec3GPPServer(mechanism) {
				summary.UsableSecurityServer = true
			}
		}
	}
	if algorithm == "" || !nonceValid || !summary.UsableSecurityServer {
		return summary, errors.New("incomplete IMS AKA challenge")
	}
	return summary, nil
}

func validateIMSInitialRegisterInput(input IMSInitialRegisterInput) error {
	if !input.Source.Is4() || !input.Source.IsPrivate() || input.Source.IsLoopback() || input.Source.IsUnspecified() ||
		input.HomeDomain != IMSHomeDomain || input.UnprotectedPort == 0 || input.ProtectedClientPort == 0 ||
		input.ProtectedServerPort == 0 || input.ProtectedClientPort == input.ProtectedServerPort ||
		input.ProtectedClientPort == IMSSIPPort || input.ProtectedServerPort == IMSSIPPort ||
		input.ClientSPI < 1_000_000_000 || input.ServerSPI < 1_000_000_000 || input.ClientSPI == input.ServerSPI ||
		!strings.HasSuffix(input.PrivateIdentity, "@"+IMSHomeDomain) ||
		input.PublicIdentity != "sip:"+input.PrivateIdentity ||
		!validIMSToken(input.Branch, 16, 64) || !validIMSToken(input.FromTag, 16, 64) ||
		!validIMSCallID(input.CallID) || !validIMSToken(input.ContactUser, 16, 64) {
		return errors.New("invalid IMS initial REGISTER input")
	}
	if len(input.WLANNodeID) != 12 {
		return errors.New("invalid IMS initial REGISTER input")
	}
	for _, current := range input.WLANNodeID {
		if current < '0' || current > '9' && current < 'a' || current > 'f' {
			return errors.New("invalid IMS initial REGISTER input")
		}
	}
	return nil
}

func validIMSToken(value string, minimum, maximum int) bool {
	if len(value) < minimum || len(value) > maximum {
		return false
	}
	for _, current := range value {
		if current >= 'a' && current <= 'z' || current >= 'A' && current <= 'Z' ||
			current >= '0' && current <= '9' || current == '-' || current == '_' || current == '.' {
			continue
		}
		return false
	}
	return true
}

func validIMSCallID(value string) bool {
	if len(value) < 16 || len(value) > 128 || strings.Count(value, "@") != 1 {
		return false
	}
	parts := strings.SplitN(value, "@", 2)
	return validIMSToken(parts[0], 8, 64) && validIMSToken(parts[1], 1, 64)
}

func validRegisterCSeq(value string) bool {
	fields := strings.Fields(value)
	if len(fields) != 2 || !strings.EqualFold(fields[1], "REGISTER") {
		return false
	}
	sequence, err := strconv.ParseUint(fields[0], 10, 31)
	return err == nil && sequence >= 1
}

func parseSIPResponse(packet []byte) (int, map[string][]string, error) {
	if len(packet) < 16 || len(packet) > 64<<10 || !strings.Contains(string(packet), "\r\n\r\n") {
		return 0, nil, errors.New("invalid SIP response")
	}
	head := string(packet[:strings.Index(string(packet), "\r\n\r\n")])
	lines := strings.Split(head, "\r\n")
	if len(lines) < 2 {
		return 0, nil, errors.New("invalid SIP response")
	}
	statusFields := strings.Fields(lines[0])
	if len(statusFields) < 2 || statusFields[0] != "SIP/2.0" {
		return 0, nil, errors.New("invalid SIP status")
	}
	status, err := strconv.Atoi(statusFields[1])
	if err != nil || status < 100 || status > 699 {
		return 0, nil, errors.New("invalid SIP status")
	}
	unfolded := make([]string, 0, len(lines)-1)
	for _, line := range lines[1:] {
		if len(line) > 4096 {
			return 0, nil, errors.New("oversized SIP header")
		}
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			if len(unfolded) == 0 {
				return 0, nil, errors.New("invalid folded SIP header")
			}
			unfolded[len(unfolded)-1] += " " + strings.TrimSpace(line)
			continue
		}
		unfolded = append(unfolded, line)
	}
	if len(unfolded) > 128 {
		return 0, nil, errors.New("too many SIP headers")
	}
	headers := make(map[string][]string)
	for _, line := range unfolded {
		colon := strings.IndexByte(line, ':')
		if colon < 1 {
			return 0, nil, errors.New("invalid SIP header")
		}
		name := strings.ToLower(strings.TrimSpace(line[:colon]))
		if name == "i" {
			name = "call-id"
		}
		if name == "" || len(name) > 64 {
			return 0, nil, errors.New("invalid SIP header name")
		}
		headers[name] = append(headers[name], strings.TrimSpace(line[colon+1:]))
	}
	return status, headers, nil
}

func inspectAKAChallenge(values []string) (string, bool) {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if len(value) < 7 || !strings.EqualFold(value[:7], "Digest ") {
			continue
		}
		parameters := parseSIPParameters(value[7:], ',')
		algorithm := parameters["algorithm"]
		if algorithm != "AKAv1-MD5" && algorithm != "AKAv2-SHA-256" {
			continue
		}
		if parameters["realm"] != IMSHomeDomain || parameters["nonce"] == "" {
			return algorithm, false
		}
		decoded, err := base64.StdEncoding.DecodeString(parameters["nonce"])
		valid := err == nil && len(decoded) >= 32 && len(decoded) <= 256
		for index := range decoded {
			decoded[index] = 0
		}
		return algorithm, valid
	}
	return "", false
}

func usableIPSec3GPPServer(value string) bool {
	parts := splitSIPValue(value, ';')
	if len(parts) < 2 || !strings.EqualFold(strings.TrimSpace(parts[0]), "ipsec-3gpp") {
		return false
	}
	parameters := parseSIPParameters(strings.Join(parts[1:], ";"), ';')
	if parameters["alg"] != "hmac-sha-1-96" || parameters["ealg"] != "" && parameters["ealg"] != "aes-cbc" && parameters["ealg"] != "null" ||
		parameters["prot"] != "" && parameters["prot"] != "esp" ||
		parameters["mod"] != "" && parameters["mod"] != "trans" {
		return false
	}
	spiClient, clientErr := strconv.ParseUint(parameters["spi-c"], 10, 32)
	spiServer, serverErr := strconv.ParseUint(parameters["spi-s"], 10, 32)
	portClient, portClientErr := strconv.ParseUint(parameters["port-c"], 10, 16)
	portServer, portServerErr := strconv.ParseUint(parameters["port-s"], 10, 16)
	return clientErr == nil && serverErr == nil && portClientErr == nil && portServerErr == nil &&
		spiClient != 0 && spiServer != 0 && spiClient != spiServer && portClient != 0 && portServer != 0 &&
		portClient != IMSSIPPort && portServer != IMSSIPPort
}

func parseSIPParameters(value string, separator byte) map[string]string {
	parameters := make(map[string]string)
	for _, part := range splitSIPValue(value, separator) {
		part = strings.TrimSpace(part)
		equals := strings.IndexByte(part, '=')
		if equals < 1 {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(part[:equals]))
		parameterValue := strings.TrimSpace(part[equals+1:])
		if len(parameterValue) >= 2 && parameterValue[0] == '"' && parameterValue[len(parameterValue)-1] == '"' {
			parameterValue = parameterValue[1 : len(parameterValue)-1]
		}
		if _, exists := parameters[name]; !exists {
			parameters[name] = parameterValue
		}
	}
	return parameters
}

func splitSIPValue(value string, separator byte) []string {
	parts := make([]string, 0, 4)
	start := 0
	quoted := false
	escaped := false
	for index := 0; index < len(value); index++ {
		current := value[index]
		if escaped {
			escaped = false
			continue
		}
		if quoted && current == '\\' {
			escaped = true
			continue
		}
		if current == '"' {
			quoted = !quoted
			continue
		}
		if current == separator && !quoted {
			parts = append(parts, value[start:index])
			start = index + 1
		}
	}
	parts = append(parts, value[start:])
	return parts
}
