package vowifihil

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

const smsOverIPContentType = "application/vnd.3gpp.sms"

type sipPacket struct {
	RequestURI string
	Method     string
	Status     int
	Headers    map[string][]string
	Body       []byte
}

type smsSIPRequestInput struct {
	Source         netip.Addr
	ViaPort        uint16
	RequestURI     string
	PublicIdentity string
	Branch         string
	FromTag        string
	CallID         string
	InReplyTo      string
	Sequence       uint64
	Routes         []string
	SecurityVerify string
	WLANNodeID     string
	Body           []byte
}

func buildSMSIPMessage(input smsSIPRequestInput) ([]byte, error) {
	if !validIMSPrivateIPv4(input.Source) || input.ViaPort == 0 || !validIMSURI(input.RequestURI) ||
		!validIMSURI(input.PublicIdentity) || !validIMSToken(input.Branch, 16, 64) ||
		!validIMSToken(input.FromTag, 16, 64) || !validIMSCallID(input.CallID) ||
		input.Sequence == 0 || input.Sequence > 1<<31-1 || len(input.Body) < 2 || len(input.Body) > 255 ||
		input.SecurityVerify == "" || len(input.SecurityVerify) > 2048 || !validSIPHeaderValue(input.SecurityVerify, 2048) ||
		len(input.WLANNodeID) != 12 {
		return nil, errors.New("invalid SMS over IMS SIP input")
	}
	if input.InReplyTo != "" && !validSIPCallID(input.InReplyTo) {
		return nil, errors.New("invalid SMS over IMS reply correlation")
	}
	for _, route := range input.Routes {
		if route == "" || !validSIPHeaderValue(route, 2048) {
			return nil, errors.New("invalid SMS over IMS route")
		}
	}
	var message strings.Builder
	fmt.Fprintf(&message, "MESSAGE %s SIP/2.0\r\n", input.RequestURI)
	fmt.Fprintf(&message, "Via: SIP/2.0/UDP %s:%d;branch=z9hG4bK%s;rport\r\n", input.Source, input.ViaPort, input.Branch)
	message.WriteString("Max-Forwards: 70\r\n")
	for _, route := range input.Routes {
		fmt.Fprintf(&message, "Route: %s\r\n", route)
	}
	fmt.Fprintf(&message, "P-Preferred-Identity: <%s>\r\n", input.PublicIdentity)
	fmt.Fprintf(&message, "From: <%s>;tag=%s\r\n", input.PublicIdentity, input.FromTag)
	fmt.Fprintf(&message, "To: <%s>\r\n", input.RequestURI)
	fmt.Fprintf(&message, "Call-ID: %s\r\n", input.CallID)
	if input.InReplyTo != "" {
		fmt.Fprintf(&message, "In-Reply-To: %s\r\n", input.InReplyTo)
	}
	fmt.Fprintf(&message, "CSeq: %d MESSAGE\r\n", input.Sequence)
	message.WriteString("Allow: MESSAGE\r\n")
	message.WriteString("Request-Disposition: no-fork\r\n")
	fmt.Fprintf(&message, "Security-Verify: %s\r\n", input.SecurityVerify)
	fmt.Fprintf(&message, "P-Access-Network-Info: IEEE-802.11;i-wlan-node-id=%s\r\n", input.WLANNodeID)
	fmt.Fprintf(&message, "Content-Type: %s\r\n", smsOverIPContentType)
	message.WriteString("Content-Transfer-Encoding: binary\r\n")
	fmt.Fprintf(&message, "Content-Length: %d\r\n\r\n", len(input.Body))
	packet := append([]byte(message.String()), input.Body...)
	return packet, nil
}

func parseSIPPacket(packet []byte) (sipPacket, error) {
	if len(packet) < 16 || len(packet) > 64<<10 {
		return sipPacket{}, errors.New("invalid SIP packet")
	}
	separator := findHeaderBoundary(packet)
	if separator < 0 {
		return sipPacket{}, errors.New("invalid SIP packet")
	}
	head := string(packet[:separator])
	lines := strings.Split(head, "\r\n")
	if len(lines) < 2 {
		return sipPacket{}, errors.New("invalid SIP packet")
	}
	result := sipPacket{Headers: make(map[string][]string), Body: append([]byte(nil), packet[separator+4:]...)}
	if strings.HasPrefix(lines[0], "SIP/2.0 ") {
		fields := strings.Fields(lines[0])
		if len(fields) < 3 {
			return sipPacket{}, errors.New("invalid SIP status")
		}
		status, err := strconv.Atoi(fields[1])
		if err != nil || status < 100 || status > 699 {
			return sipPacket{}, errors.New("invalid SIP status")
		}
		result.Status = status
	} else {
		fields := strings.Fields(lines[0])
		if len(fields) != 3 || fields[2] != "SIP/2.0" || !validSIPMethod(fields[0]) || !validIMSURI(fields[1]) {
			return sipPacket{}, errors.New("invalid SIP request line")
		}
		result.Method, result.RequestURI = fields[0], fields[1]
	}
	unfolded := make([]string, 0, len(lines)-1)
	for _, line := range lines[1:] {
		if len(line) > 4096 {
			return sipPacket{}, errors.New("oversized SIP header")
		}
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			if len(unfolded) == 0 {
				return sipPacket{}, errors.New("invalid folded SIP header")
			}
			unfolded[len(unfolded)-1] += " " + strings.TrimSpace(line)
			continue
		}
		unfolded = append(unfolded, line)
	}
	if len(unfolded) > 128 {
		return sipPacket{}, errors.New("too many SIP headers")
	}
	for _, line := range unfolded {
		colon := strings.IndexByte(line, ':')
		if colon < 1 {
			return sipPacket{}, errors.New("invalid SIP header")
		}
		name := canonicalCompactSIPHeader(strings.ToLower(strings.TrimSpace(line[:colon])))
		value := strings.TrimSpace(line[colon+1:])
		if name == "" || len(name) > 64 || !validSIPHeaderValue(value, 4096) {
			return sipPacket{}, errors.New("invalid SIP header")
		}
		result.Headers[name] = append(result.Headers[name], value)
	}
	contentLengths := result.Headers["content-length"]
	if len(contentLengths) != 1 {
		return sipPacket{}, errors.New("invalid SIP content length")
	}
	length, err := strconv.Atoi(contentLengths[0])
	if err != nil || length < 0 || length != len(result.Body) {
		return sipPacket{}, errors.New("invalid SIP content length")
	}
	return result, nil
}

func parseSMSIPRequest(packet []byte) (sipPacket, error) {
	request, err := parseSIPPacket(packet)
	if err != nil || request.Method != "MESSAGE" || request.Status != 0 || len(request.Body) < 2 || len(request.Body) > 255 {
		return sipPacket{}, errors.New("invalid SMS over IMS request")
	}
	contentTypes := request.Headers["content-type"]
	if len(contentTypes) != 1 || !strings.EqualFold(strings.TrimSpace(strings.SplitN(contentTypes[0], ";", 2)[0]), smsOverIPContentType) ||
		len(request.Headers["via"]) == 0 || len(request.Headers["from"]) != 1 || len(request.Headers["to"]) != 1 ||
		len(request.Headers["call-id"]) != 1 || !validSIPCallID(strings.TrimSpace(request.Headers["call-id"][0])) ||
		len(request.Headers["cseq"]) != 1 || !matchingCSeq(request.Headers["cseq"][0], "MESSAGE", 0) {
		return sipPacket{}, errors.New("invalid SMS over IMS request")
	}
	return request, nil
}

func buildSIPResponse(request sipPacket, status int, toTag string) ([]byte, error) {
	reason := ""
	switch status {
	case 200:
		reason = "OK"
	case 400:
		reason = "Bad Request"
	case 415:
		reason = "Unsupported Media Type"
	case 488:
		reason = "Not Acceptable Here"
	default:
		return nil, errors.New("unsupported SIP response")
	}
	if request.Method == "" || request.Status != 0 || len(request.Headers["via"]) == 0 ||
		len(request.Headers["from"]) != 1 || len(request.Headers["to"]) != 1 ||
		len(request.Headers["call-id"]) != 1 || len(request.Headers["cseq"]) != 1 ||
		!validIMSToken(toTag, 16, 64) {
		return nil, errors.New("invalid SIP response transaction")
	}
	to := request.Headers["to"][0]
	if !strings.Contains(strings.ToLower(to), ";tag=") {
		to += ";tag=" + toTag
	}
	var response strings.Builder
	fmt.Fprintf(&response, "SIP/2.0 %d %s\r\n", status, reason)
	for _, via := range request.Headers["via"] {
		fmt.Fprintf(&response, "Via: %s\r\n", via)
	}
	fmt.Fprintf(&response, "From: %s\r\n", request.Headers["from"][0])
	fmt.Fprintf(&response, "To: %s\r\n", to)
	fmt.Fprintf(&response, "Call-ID: %s\r\n", request.Headers["call-id"][0])
	fmt.Fprintf(&response, "CSeq: %s\r\n", request.Headers["cseq"][0])
	response.WriteString("Content-Length: 0\r\n\r\n")
	return []byte(response.String()), nil
}

func matchingSIPResponse(packet sipPacket, callID string, sequence uint64, method string) bool {
	return packet.Status != 0 && len(packet.Headers["call-id"]) == 1 &&
		strings.TrimSpace(packet.Headers["call-id"][0]) == callID && len(packet.Headers["cseq"]) == 1 &&
		matchingCSeq(packet.Headers["cseq"][0], method, sequence)
}

func matchingCSeq(value, method string, expected uint64) bool {
	fields := strings.Fields(value)
	if len(fields) != 2 || !strings.EqualFold(fields[1], method) {
		return false
	}
	sequence, err := strconv.ParseUint(fields[0], 10, 31)
	return err == nil && sequence >= 1 && (expected == 0 || sequence == expected)
}

func firstSIPURI(values []string) string {
	return firstSIPURIWithBareParameters(values, false)
}

// firstSIPAssertedIdentityURI preserves URI parameters in the addr-spec form
// allowed by P-Asserted-Identity. Unlike From/To, this header has no tag-style
// header parameters whose semicolon must be removed.
func firstSIPAssertedIdentityURI(values []string) string {
	return firstSIPURIWithBareParameters(values, true)
}

func firstSIPURIWithBareParameters(values []string, preserveBareParameters bool) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if left := strings.IndexByte(value, '<'); left >= 0 {
			right := strings.IndexByte(value[left+1:], '>')
			if right < 0 {
				continue
			}
			value = value[left+1 : left+1+right]
		} else if !preserveBareParameters {
			if semicolon := strings.IndexByte(value, ';'); semicolon >= 0 {
				value = value[:semicolon]
			}
		}
		value = strings.TrimSpace(value)
		if validIMSURI(value) {
			return value
		}
	}
	return ""
}

func findHeaderBoundary(packet []byte) int {
	for index := 0; index+3 < len(packet); index++ {
		if packet[index] == '\r' && packet[index+1] == '\n' && packet[index+2] == '\r' && packet[index+3] == '\n' {
			return index
		}
	}
	return -1
}

func canonicalCompactSIPHeader(name string) string {
	switch name {
	case "v":
		return "via"
	case "f":
		return "from"
	case "t":
		return "to"
	case "i":
		return "call-id"
	case "l":
		return "content-length"
	case "c":
		return "content-type"
	default:
		return name
	}
}

func validSIPMethod(method string) bool {
	if method == "" || len(method) > 32 {
		return false
	}
	for _, current := range method {
		if current < 'A' || current > 'Z' {
			return false
		}
	}
	return true
}

func validIMSURI(value string) bool {
	if len(value) < 5 || len(value) > 512 ||
		!strings.HasPrefix(value, "sip:") && !strings.HasPrefix(value, "sips:") && !strings.HasPrefix(value, "tel:") {
		return false
	}
	for _, current := range value {
		if current < 0x21 || current > 0x7e || strings.ContainsRune("<>\"\\", current) {
			return false
		}
	}
	return true
}

func validSIPCallID(value string) bool {
	if len(value) == 0 || len(value) > 255 || strings.Count(value, "@") > 1 {
		return false
	}
	local, host, hasHost := strings.Cut(value, "@")
	return validSIPCallIDWord(local) && (!hasHost || validSIPCallIDWord(host))
}

func validSIPCallIDWord(value string) bool {
	if value == "" {
		return false
	}
	for _, current := range value {
		if current >= 'a' && current <= 'z' || current >= 'A' && current <= 'Z' || current >= '0' && current <= '9' ||
			strings.ContainsRune("-._!%*+`'~()<>:\\\"/[]?{}", current) {
			continue
		}
		return false
	}
	return true
}

func validSIPHeaderValue(value string, maximum int) bool {
	if value == "" || len(value) > maximum {
		return false
	}
	return strings.IndexFunc(value, func(current rune) bool {
		return current == '\r' || current == '\n' || current < 0x20 && current != '\t' || current > 0x7e
	}) < 0
}
