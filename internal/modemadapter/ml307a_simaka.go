package modemadapter

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/attransport"
	"github.com/leonfox28/simplus/internal/modemadapter/standardat"
)

const usimApplicationAID = "A0000000871002"

func (ML307A) ReadSIMAKAIdentity(ctx context.Context, query attransport.Query, identities IdentityPseudonymizer, identityFingerprint string) (string, error) {
	if identities == nil {
		return "", agentapi.ErrSIMAKAUnsupported
	}
	if query == nil {
		return "", agentapi.ErrSIMAKAUnavailable
	}
	if err := validateML307ASIMAKATarget(ctx, query, identities, identityFingerprint); err != nil {
		return "", err
	}
	lines, err := query(ctx, "AT+CIMI", 2*time.Second)
	if err != nil {
		return "", agentapi.ErrSIMAKAUnavailable
	}
	imsi := parseML307AIMSI(lines)
	if imsi == "" {
		return "", agentapi.ErrSIMAKAUnavailable
	}
	return imsi, nil
}

func (ML307A) AuthenticateSIMAKA(ctx context.Context, query attransport.Query, identities IdentityPseudonymizer, identityFingerprint string, challenge agentapi.SIMAKAChallenge) (agentapi.SIMAKAExecution, error) {
	if identities == nil {
		return agentapi.SIMAKAExecution{}, agentapi.ErrSIMAKAUnsupported
	}
	if query == nil {
		return agentapi.SIMAKAExecution{}, agentapi.ErrSIMAKAUnavailable
	}
	if err := validateML307ASIMAKATarget(ctx, query, identities, identityFingerprint); err != nil {
		return agentapi.SIMAKAExecution{}, err
	}
	return exchangeML307AUSIMAKA(ctx, query, challenge)
}

func (ML307A) ProbeSIMIMSProfile(ctx context.Context, query attransport.Query, identities IdentityPseudonymizer, identityFingerprint string) (bool, error) {
	if identities == nil {
		return false, agentapi.ErrSIMAKAUnsupported
	}
	if query == nil {
		return false, agentapi.ErrSIMAKAUnavailable
	}
	if err := validateML307ASIMAKATarget(ctx, query, identities, identityFingerprint); err != nil {
		return false, err
	}
	_, available, err := readML307AISIMIdentity(ctx, query)
	return available, err
}

func (ML307A) ReadSIMIMSIdentity(ctx context.Context, query attransport.Query, identities IdentityPseudonymizer, identityFingerprint string) (agentapi.SIMIMSIdentityMaterial, error) {
	if identities == nil {
		return agentapi.SIMIMSIdentityMaterial{}, agentapi.ErrSIMAKAUnsupported
	}
	if query == nil {
		return agentapi.SIMIMSIdentityMaterial{}, agentapi.ErrSIMAKAUnavailable
	}
	if err := validateML307ASIMAKATarget(ctx, query, identities, identityFingerprint); err != nil {
		return agentapi.SIMIMSIdentityMaterial{}, err
	}
	material, available, err := readML307AISIMIdentity(ctx, query)
	if err != nil {
		return agentapi.SIMIMSIdentityMaterial{}, err
	}
	if available {
		material.SMSOverIP = readML307ASMSOverIPConfiguration(ctx, query)
		return material, nil
	}
	lines, err := query(ctx, "AT+CIMI", 2*time.Second)
	if err != nil {
		return agentapi.SIMIMSIdentityMaterial{}, agentapi.ErrSIMAKAUnavailable
	}
	imsi := parseML307AIMSI(lines)
	if imsi == "" {
		return agentapi.SIMIMSIdentityMaterial{}, agentapi.ErrSIMAKAUnavailable
	}
	material, err = deriveML307AIMSIdentity(ctx, query, imsi, material)
	if err != nil {
		return agentapi.SIMIMSIdentityMaterial{}, err
	}
	material.SMSOverIP = readML307ASMSOverIPConfiguration(ctx, query)
	return material, nil
}

func readML307ASMSOverIPConfiguration(ctx context.Context, query attransport.Query) *agentapi.SIMIMSSMSConfiguration {
	lines, err := query(ctx, "AT+CSCA?", 2*time.Second)
	if err != nil {
		return nil
	}
	address := parseML307AServiceCentreAddress(lines)
	if address == "" {
		return nil
	}
	return &agentapi.SIMIMSSMSConfiguration{
		ServiceCentreURI: "tel:" + address, ServiceCentreAddress: address,
	}
}

func parseML307AServiceCentreAddress(lines []string) string {
	if !attransport.HasTerminalOK(lines) {
		return ""
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "+CSCA:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "+CSCA:"))
		comma := strings.LastIndexByte(value, ',')
		if comma < 3 {
			return ""
		}
		number := strings.TrimSpace(value[:comma])
		if len(number) < 2 || number[0] != '"' || number[len(number)-1] != '"' {
			return ""
		}
		number = number[1 : len(number)-1]
		typeOfAddress, err := strconv.Atoi(strings.TrimSpace(value[comma+1:]))
		if err != nil || typeOfAddress != 145 || len(number) < 3 || len(number) > 21 {
			return ""
		}
		if number[0] != '+' {
			number = "+" + number
		}
		if len(number) < 4 || len(number) > 21 {
			return ""
		}
		for _, current := range number[1:] {
			if current < '0' || current > '9' {
				return ""
			}
		}
		return number
	}
	return ""
}

func validateML307ASIMAKATarget(ctx context.Context, query attransport.Query, identities IdentityPseudonymizer, expectedFingerprint string) error {
	if identities == nil || len(expectedFingerprint) != 64 {
		return agentapi.ErrSIMAKAUnavailable
	}
	if lines, err := query(ctx, "AT", time.Second); err != nil || !attransport.HasTerminalOK(lines) {
		return agentapi.ErrSIMAKAUnavailable
	}
	cpin, err := query(ctx, "AT+CPIN?", 2*time.Second)
	if err != nil || !attransport.HasTerminalOK(cpin) {
		return agentapi.ErrSIMAKAUnavailable
	}
	sim := standardat.SIMObservation(cpin, nil)
	if sim.State != agentapi.SIMStatePresent || sim.PrimaryLockState != agentapi.PrimaryLockReady {
		return agentapi.ErrSIMAKASIMNotReady
	}
	mccid, err := query(ctx, "AT+MCCID", 2*time.Second)
	if err != nil {
		return agentapi.ErrSIMAKAUnavailable
	}
	fingerprint, _ := pseudonymizedICCID(mccid, "+MCCID:", identities)
	if fingerprint == "" {
		return agentapi.ErrSIMAKAUnavailable
	}
	if fingerprint != expectedFingerprint {
		return agentapi.ErrSIMAKAIdentityChanged
	}
	return nil
}

func parseML307AIMSI(lines []string) string {
	if !attransport.HasTerminalOK(lines) {
		return ""
	}
	for _, line := range lines {
		if len(line) < 14 || len(line) > 16 {
			continue
		}
		valid := true
		for _, value := range line {
			if value < '0' || value > '9' {
				valid = false
				break
			}
		}
		if valid {
			return line
		}
	}
	return ""
}

func exchangeML307AUSIMAKA(ctx context.Context, query attransport.Query, challenge agentapi.SIMAKAChallenge) (agentapi.SIMAKAExecution, error) {
	opened, err := query(ctx, fmt.Sprintf("AT+CCHO=\"%s\"", usimApplicationAID), 3*time.Second)
	if err != nil {
		return agentapi.SIMAKAExecution{}, agentapi.ErrSIMAKAUnavailable
	}
	sessionID := parseCCHOSessionID(opened)
	if sessionID == 0 {
		return agentapi.SIMAKAExecution{}, agentapi.ErrSIMAKAUnsupported
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = query(closeCtx, fmt.Sprintf("AT+CCHC=%d", sessionID), 2*time.Second)
	}()

	apdu := buildUSIMAKAAPDU(challenge)
	response, err := sendCGLAAPDU(ctx, query, sessionID, apdu)
	zeroSIMAKABytesLocal(apdu)
	if err != nil {
		return agentapi.SIMAKAExecution{}, err
	}
	defer zeroSIMAKABytesLocal(response)
	return parseUSIMAKAResponse(response)
}

func buildUSIMAKAAPDU(challenge agentapi.SIMAKAChallenge) []byte {
	command := make([]byte, 0, 39)
	command = append(command, 0x00, 0x88, 0x00, 0x81, 0x22, 0x10)
	command = append(command, challenge.RAND[:]...)
	command = append(command, 0x10)
	command = append(command, challenge.AUTN[:]...)
	return command
}

func sendCGLAAPDU(ctx context.Context, query attransport.Query, sessionID int, apdu []byte) ([]byte, error) {
	commandHex := strings.ToUpper(hex.EncodeToString(apdu))
	command := fmt.Sprintf("AT+CGLA=%d,%d,\"%s\"", sessionID, len(commandHex), commandHex)
	lines, err := query(ctx, command, 5*time.Second)
	if err != nil {
		return nil, agentapi.ErrSIMAKAUnavailable
	}
	response := parseCGLAResponse(lines)
	if len(response) < 2 {
		return nil, agentapi.ErrSIMAKAUnavailable
	}
	statusOffset := len(response) - 2
	status1, status2 := response[statusOffset], response[statusOffset+1]
	data := append([]byte(nil), response[:statusOffset]...)
	zeroSIMAKABytesLocal(response)
	if status1 == 0x61 || status1 == 0x9f {
		getResponse := []byte{0x00, 0xc0, 0x00, 0x00, status2}
		follow, followErr := sendCGLAAPDUOnce(ctx, query, sessionID, getResponse)
		zeroSIMAKABytesLocal(getResponse)
		if followErr != nil || len(follow) < 2 {
			zeroSIMAKABytesLocal(data)
			return nil, agentapi.ErrSIMAKAUnavailable
		}
		followStatus := len(follow) - 2
		if follow[followStatus] != 0x90 || follow[followStatus+1] != 0x00 {
			zeroSIMAKABytesLocal(data)
			zeroSIMAKABytesLocal(follow)
			return nil, classifyUSIMAKAStatus(follow[followStatus], follow[followStatus+1])
		}
		data = append(data, follow[:followStatus]...)
		zeroSIMAKABytesLocal(follow)
		return data, nil
	}
	if status1 != 0x90 || status2 != 0x00 {
		zeroSIMAKABytesLocal(data)
		return nil, classifyUSIMAKAStatus(status1, status2)
	}
	return data, nil
}

func sendCGLAAPDUOnce(ctx context.Context, query attransport.Query, sessionID int, apdu []byte) ([]byte, error) {
	commandHex := strings.ToUpper(hex.EncodeToString(apdu))
	lines, err := query(ctx, fmt.Sprintf("AT+CGLA=%d,%d,\"%s\"", sessionID, len(commandHex), commandHex), 5*time.Second)
	if err != nil {
		return nil, err
	}
	return parseCGLAResponse(lines), nil
}

func classifyUSIMAKAStatus(sw1, sw2 byte) error {
	if sw1 == 0x98 && sw2 == 0x62 {
		return agentapi.ErrSIMAKARejected
	}
	return agentapi.ErrSIMAKAUnavailable
}

func parseCCHOSessionID(lines []string) int {
	if !attransport.HasTerminalOK(lines) {
		return 0
	}
	for _, line := range lines {
		value := strings.TrimSpace(strings.TrimPrefix(line, "+CCHO:"))
		parsed, err := strconv.Atoi(value)
		if err == nil && parsed >= 1 && parsed <= 19 {
			return parsed
		}
	}
	return 0
}

func parseCGLAResponse(lines []string) []byte {
	if !attransport.HasTerminalOK(lines) {
		return nil
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "+CGLA:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "+CGLA:"))
		comma := strings.IndexByte(payload, ',')
		if comma < 1 {
			return nil
		}
		declared, err := strconv.Atoi(strings.TrimSpace(payload[:comma]))
		if err != nil || declared < 4 || declared > 512 {
			return nil
		}
		encoded := strings.TrimSpace(payload[comma+1:])
		if len(encoded) < 2 || encoded[0] != '"' || encoded[len(encoded)-1] != '"' {
			return nil
		}
		encoded = encoded[1 : len(encoded)-1]
		if len(encoded) != declared || len(encoded)%2 != 0 {
			return nil
		}
		decoded, err := hex.DecodeString(encoded)
		if err != nil {
			return nil
		}
		return decoded
	}
	return nil
}

func parseUSIMAKAResponse(data []byte) (agentapi.SIMAKAExecution, error) {
	if len(data) < 2 {
		return agentapi.SIMAKAExecution{}, agentapi.ErrSIMAKAUnavailable
	}
	switch data[0] {
	case 0xdb:
		position := 1
		res, ok := takeUSIMAKAField(data, &position, 4, 16)
		if !ok {
			return agentapi.SIMAKAExecution{}, agentapi.ErrSIMAKAUnavailable
		}
		ck, ok := takeUSIMAKAField(data, &position, 16, 16)
		if !ok {
			return agentapi.SIMAKAExecution{}, agentapi.ErrSIMAKAUnavailable
		}
		ik, ok := takeUSIMAKAField(data, &position, 16, 16)
		if !ok {
			return agentapi.SIMAKAExecution{}, agentapi.ErrSIMAKAUnavailable
		}
		if position < len(data) {
			kc, present := takeUSIMAKAField(data, &position, 8, 8)
			if !present {
				return agentapi.SIMAKAExecution{}, agentapi.ErrSIMAKAUnavailable
			}
			zeroSIMAKABytesLocal(kc)
		}
		if position != len(data) {
			return agentapi.SIMAKAExecution{}, agentapi.ErrSIMAKAUnavailable
		}
		result := agentapi.SIMAKAExecution{State: agentapi.SIMAKAStateSuccess, RES: append([]byte(nil), res...)}
		copy(result.CK[:], ck)
		copy(result.IK[:], ik)
		return result, nil
	case 0xdc:
		position := 1
		auts, ok := takeUSIMAKAField(data, &position, 14, 14)
		if !ok || position != len(data) {
			return agentapi.SIMAKAExecution{}, agentapi.ErrSIMAKAUnavailable
		}
		result := agentapi.SIMAKAExecution{State: agentapi.SIMAKAStateSynchronizationFailure}
		copy(result.AUTS[:], auts)
		return result, nil
	default:
		return agentapi.SIMAKAExecution{}, agentapi.ErrSIMAKAUnavailable
	}
}

func takeUSIMAKAField(data []byte, position *int, minimum, maximum int) ([]byte, bool) {
	if position == nil || *position >= len(data) {
		return nil, false
	}
	length := int(data[*position])
	*position = *position + 1
	if length < minimum || length > maximum || *position+length > len(data) {
		return nil, false
	}
	value := data[*position : *position+length]
	*position += length
	return value, true
}

func zeroSIMAKABytesLocal(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
