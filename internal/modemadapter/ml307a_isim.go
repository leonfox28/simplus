package modemadapter

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/attransport"
)

var (
	errISIMTextTLVMissing = errors.New("ISIM text TLV is missing")
	errISIMTextEncoding   = errors.New("ISIM text encoding is invalid")
)

const ml307AISIMAID = "A0000000871004"

func readML307AISIMIdentity(ctx context.Context, query attransport.Query) (agentapi.SIMIMSIdentityMaterial, bool, error) {
	aids, discovered, err := discoverML307AISIMAIDs(ctx, query)
	if err != nil {
		return agentapi.SIMIMSIdentityMaterial{}, false,
			agentapi.NewSIMIMSHILStageError(agentapi.SIMIMSStageApplicationDiscover)
	}
	discovery := agentapi.SIMIMSDiscoveryEFDIR
	if !discovered {
		aids = []string{ml307AISIMAID}
		discovery = agentapi.SIMIMSDiscoveryGenericAID
	}
	metadata := agentapi.SIMIMSIdentityMaterial{
		ApplicationDiscovery: discovery, ApplicationCandidates: len(aids),
	}
	if len(aids) == 0 {
		return metadata, false, nil
	}
	var firstFailure error
	for _, aid := range aids {
		material, available, readErr := readML307AISIMIdentityAID(ctx, query, aid)
		if readErr != nil {
			if firstFailure == nil {
				firstFailure = readErr
			}
			continue
		}
		if available {
			material.ApplicationDiscovery = discovery
			material.ApplicationCandidates = len(aids)
			return material, true, nil
		}
	}
	if firstFailure != nil {
		return agentapi.SIMIMSIdentityMaterial{}, true, firstFailure
	}
	return metadata, false, nil
}

func readML307AISIMIdentityAID(ctx context.Context, query attransport.Query, aid string) (agentapi.SIMIMSIdentityMaterial, bool, error) {
	if !validML307AISIMAID(aid) {
		return agentapi.SIMIMSIdentityMaterial{}, false, agentapi.ErrSIMAKAUnavailable
	}
	opened, err := query(ctx, fmt.Sprintf("AT+CCHO=\"%s\"", aid), 3*time.Second)
	if err != nil {
		return agentapi.SIMIMSIdentityMaterial{}, false, agentapi.NewSIMIMSHILStageError(agentapi.SIMIMSStageApplicationOpen)
	}
	sessionID := parseCCHOSessionID(opened)
	if sessionID == 0 {
		if attransport.HasTerminalResponse(opened) {
			return agentapi.SIMIMSIdentityMaterial{}, false, nil
		}
		return agentapi.SIMIMSIdentityMaterial{}, false, agentapi.ErrSIMAKAUnavailable
	}
	closed := false
	defer func() {
		if !closed {
			closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, _ = query(closeCtx, fmt.Sprintf("AT+CCHC=%d", sessionID), 2*time.Second)
		}
	}()

	privateIdentity, err := readISIMTransparentText(ctx, query, sessionID, 0x6f02,
		agentapi.SIMIMSStagePrivateSelect, agentapi.SIMIMSStagePrivateLayout,
		agentapi.SIMIMSStagePrivateRead, agentapi.SIMIMSStagePrivateTLV, agentapi.SIMIMSStagePrivateEncoding)
	if err != nil {
		// TS 23.003 requires an IMSI-derived private identity when the private
		// identity is unknown. Treat only an explicitly unprovisioned EFIMPI as
		// unknown. Malformed or partially provisioned ISIM data remains a hard
		// failure so identities from different sources are never mixed.
		if shape, ok := agentapi.SIMIMSHILShape(err); ok && shape == agentapi.SIMIMSShapePaddingOnly {
			return agentapi.SIMIMSIdentityMaterial{}, false, nil
		}
		return agentapi.SIMIMSIdentityMaterial{}, true, err
	}
	homeDomain, err := readISIMTransparentText(ctx, query, sessionID, 0x6f03,
		agentapi.SIMIMSStageDomainSelect, agentapi.SIMIMSStageDomainLayout,
		agentapi.SIMIMSStageDomainRead, agentapi.SIMIMSStageDomainTLV, agentapi.SIMIMSStageDomainEncoding)
	if err != nil {
		return agentapi.SIMIMSIdentityMaterial{}, true, err
	}
	publicIdentities, err := readISIMPublicIdentities(ctx, query, sessionID)
	if err != nil {
		return agentapi.SIMIMSIdentityMaterial{}, true, err
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	closeLines, closeErr := query(closeCtx, fmt.Sprintf("AT+CCHC=%d", sessionID), 2*time.Second)
	cancel()
	if closeErr != nil || !attransport.HasTerminalOK(closeLines) {
		return agentapi.SIMIMSIdentityMaterial{}, true, agentapi.NewSIMIMSHILStageError(agentapi.SIMIMSStageChannelClose)
	}
	closed = true
	return agentapi.SIMIMSIdentityMaterial{
		Source: agentapi.SIMIMSIdentityISIM, PrivateIdentity: privateIdentity,
		HomeDomain: homeDomain, PublicIdentities: publicIdentities,
	}, true, nil
}

func discoverML307AISIMAIDs(ctx context.Context, query attransport.Query) ([]string, bool, error) {
	lines, err := query(ctx, "AT+CUAD=0", 5*time.Second)
	if err != nil {
		return nil, false, err
	}
	if !attransport.HasTerminalOK(lines) {
		if attransport.HasTerminalResponse(lines) {
			return discoverML307AISIMAIDsCRSM(ctx, query)
		}
		return nil, false, agentapi.ErrSIMAKAUnavailable
	}
	encoded, ok := parseCUADPayload(lines)
	if !ok {
		return nil, true, agentapi.ErrSIMAKAUnavailable
	}
	if encoded == "" {
		return nil, true, nil
	}
	data, err := hex.DecodeString(encoded)
	if err != nil {
		return nil, true, agentapi.ErrSIMAKAUnavailable
	}
	defer zeroSIMAKABytesLocal(data)
	aids, valid := parseISIMAIDsFromEFDIR(data)
	if !valid {
		return nil, true, agentapi.ErrSIMAKAUnavailable
	}
	return aids, true, nil
}

func discoverML307AISIMAIDsCRSM(ctx context.Context, query attransport.Query) ([]string, bool, error) {
	const efDIRFileID = 12032
	data := make([]byte, 0, 512)
	defer zeroSIMAKABytesLocal(data)
	recordLength := 0
	for record := 1; record <= 32; record++ {
		p3 := recordLength
		lines, err := query(ctx, fmt.Sprintf("AT+CRSM=178,%d,%d,4,%d", efDIRFileID, record, p3), 5*time.Second)
		if err != nil {
			return nil, false, err
		}
		if !attransport.HasTerminalOK(lines) {
			if record == 1 && attransport.HasTerminalResponse(lines) {
				return nil, false, nil
			}
			return nil, true, agentapi.ErrSIMAKAUnavailable
		}
		sw1, sw2, encoded, ok := parseCRSMResponse(lines)
		if !ok {
			return nil, true, agentapi.ErrSIMAKAUnavailable
		}
		if sw1 == 0x6c && recordLength == 0 && sw2 != 0 {
			recordLength = sw2
			record--
			continue
		}
		if (sw1 == 0x6a && (sw2 == 0x82 || sw2 == 0x83)) || sw1 == 0x94 {
			if record == 1 {
				return nil, true, nil
			}
			break
		}
		if sw1 != 0x90 && sw1 != 0x91 {
			return nil, true, agentapi.ErrSIMAKAUnavailable
		}
		recordData, decodeErr := hex.DecodeString(encoded)
		if decodeErr != nil || len(recordData) == 0 || len(recordData) > 256 {
			zeroSIMAKABytesLocal(recordData)
			return nil, true, agentapi.ErrSIMAKAUnavailable
		}
		if recordLength == 0 {
			recordLength = len(recordData)
		}
		if len(recordData) != recordLength || len(data)+len(recordData) > 8192 {
			zeroSIMAKABytesLocal(recordData)
			return nil, true, agentapi.ErrSIMAKAUnavailable
		}
		data = append(data, recordData...)
		zeroSIMAKABytesLocal(recordData)
	}
	aids, valid := parseISIMAIDsFromEFDIR(data)
	if !valid {
		return nil, true, agentapi.ErrSIMAKAUnavailable
	}
	return aids, true, nil
}

func parseCRSMResponse(lines []string) (int, int, string, bool) {
	for _, line := range lines {
		if !strings.HasPrefix(line, "+CRSM:") {
			continue
		}
		parts := strings.SplitN(strings.TrimSpace(strings.TrimPrefix(line, "+CRSM:")), ",", 3)
		if len(parts) < 2 {
			return 0, 0, "", false
		}
		sw1, sw1Err := strconv.Atoi(strings.TrimSpace(parts[0]))
		sw2, sw2Err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if sw1Err != nil || sw2Err != nil || sw1 < 0 || sw1 > 255 || sw2 < 0 || sw2 > 255 {
			return 0, 0, "", false
		}
		encoded := ""
		if len(parts) == 3 {
			encoded = strings.TrimSpace(parts[2])
			if len(encoded) >= 2 && encoded[0] == '"' && encoded[len(encoded)-1] == '"' {
				encoded = encoded[1 : len(encoded)-1]
			}
			if len(encoded) > 512 || len(encoded)%2 != 0 {
				return 0, 0, "", false
			}
			for _, current := range encoded {
				if current >= '0' && current <= '9' || current >= 'A' && current <= 'F' || current >= 'a' && current <= 'f' {
					continue
				}
				return 0, 0, "", false
			}
		}
		return sw1, sw2, strings.ToUpper(encoded), true
	}
	return 0, 0, "", false
}

func parseCUADPayload(lines []string) (string, bool) {
	for _, line := range lines {
		if !strings.HasPrefix(line, "+CUAD:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "+CUAD:"))
		if comma := strings.IndexByte(value, ','); comma >= 0 {
			value = strings.TrimSpace(value[:comma])
		}
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			value = value[1 : len(value)-1]
		}
		if len(value) > 8192 || len(value)%2 != 0 {
			return "", false
		}
		for _, current := range value {
			if current >= '0' && current <= '9' || current >= 'A' && current <= 'F' || current >= 'a' && current <= 'f' {
				continue
			}
			return "", false
		}
		return strings.ToUpper(value), true
	}
	return "", false
}

func parseISIMAIDsFromEFDIR(data []byte) ([]string, bool) {
	const isimPrefix = "A0000000871004"
	result := make([]string, 0, 2)
	seen := make(map[string]struct{}, 2)
	for position := 0; position < len(data); {
		if data[position] == 0x00 || data[position] == 0xff {
			position++
			continue
		}
		if data[position] != 0x61 {
			return nil, false
		}
		length, consumed, ok := parseISIMTLVLength(data[position+1:])
		start := position + 1 + consumed
		if !ok || length < 1 || start+length > len(data) {
			return nil, false
		}
		application, found := findISIMTLV(data[start:start+length], 0x4f)
		if found && len(application) >= 7 && len(application) <= 16 {
			aid := strings.ToUpper(hex.EncodeToString(application))
			if strings.HasPrefix(aid, isimPrefix) {
				if _, duplicate := seen[aid]; !duplicate {
					seen[aid] = struct{}{}
					result = append(result, aid)
					if len(result) == 8 {
						return result, true
					}
				}
			}
		}
		position = start + length
	}
	return result, true
}

func validML307AISIMAID(aid string) bool {
	if len(aid) < len(ml307AISIMAID) || len(aid) > 32 || len(aid)%2 != 0 || !strings.HasPrefix(aid, ml307AISIMAID) {
		return false
	}
	for _, current := range aid {
		if current < '0' || current > '9' && current < 'A' || current > 'F' {
			return false
		}
	}
	return true
}

func readISIMTransparentText(ctx context.Context, query attransport.Query, sessionID int, fileID uint16,
	selectStage, layoutStage, readStage, tlvStage, encodingStage string) (string, error) {
	selectAPDU := []byte{0x00, 0xa4, 0x00, 0x04, 0x02, byte(fileID >> 8), byte(fileID)}
	fcp, err := sendISIMAPDU(ctx, query, sessionID, selectAPDU)
	zeroSIMAKABytesLocal(selectAPDU)
	if err != nil {
		return "", agentapi.NewSIMIMSHILStageError(selectStage)
	}
	defer zeroSIMAKABytesLocal(fcp)
	size := parseISIMFileSize(fcp)
	if size < 1 || size > 256 {
		return "", agentapi.NewSIMIMSHILStageError(layoutStage)
	}
	readAPDU := []byte{0x00, 0xb0, 0x00, 0x00, byte(size)}
	data, err := sendISIMAPDU(ctx, query, sessionID, readAPDU)
	zeroSIMAKABytesLocal(readAPDU)
	if err != nil {
		return "", agentapi.NewSIMIMSHILStageError(readStage)
	}
	defer zeroSIMAKABytesLocal(data)
	value, err := parseISIMTextTLV(data)
	if err != nil {
		if errors.Is(err, errISIMTextTLVMissing) {
			return "", agentapi.NewSIMIMSHILStageShapeError(tlvStage, classifyISIMTextShape(data))
		}
		return "", agentapi.NewSIMIMSHILStageError(encodingStage)
	}
	return value, nil
}

func readISIMPublicIdentities(ctx context.Context, query attransport.Query, sessionID int) ([]string, error) {
	selectAPDU := []byte{0x00, 0xa4, 0x00, 0x04, 0x02, 0x6f, 0x04}
	fcp, err := sendISIMAPDU(ctx, query, sessionID, selectAPDU)
	zeroSIMAKABytesLocal(selectAPDU)
	if err != nil {
		return nil, agentapi.NewSIMIMSHILStageError(agentapi.SIMIMSStagePublicSelect)
	}
	defer zeroSIMAKABytesLocal(fcp)
	recordLength, recordCount := parseISIMRecordLayout(fcp)
	if recordLength < 1 || recordLength > 256 || recordCount < 1 || recordCount > 32 {
		return nil, agentapi.NewSIMIMSHILStageError(agentapi.SIMIMSStagePublicLayout)
	}
	identities := make([]string, 0, recordCount)
	seen := make(map[string]struct{}, recordCount)
	for record := 1; record <= recordCount; record++ {
		readAPDU := []byte{0x00, 0xb2, byte(record), 0x04, byte(recordLength)}
		data, err := sendISIMAPDU(ctx, query, sessionID, readAPDU)
		zeroSIMAKABytesLocal(readAPDU)
		if err != nil {
			return nil, agentapi.NewSIMIMSHILStageError(agentapi.SIMIMSStagePublicRead)
		}
		identity, parseErr := parseISIMTextTLV(data)
		zeroSIMAKABytesLocal(data)
		if parseErr != nil {
			if errors.Is(parseErr, errISIMTextEncoding) {
				return nil, agentapi.NewSIMIMSHILStageError(agentapi.SIMIMSStagePublicEncoding)
			}
			continue
		}
		if _, duplicate := seen[identity]; duplicate {
			continue
		}
		seen[identity] = struct{}{}
		identities = append(identities, identity)
		if len(identities) == 8 {
			break
		}
	}
	if len(identities) == 0 {
		return nil, agentapi.NewSIMIMSHILStageError(agentapi.SIMIMSStagePublicTLV)
	}
	return identities, nil
}

func sendISIMAPDU(ctx context.Context, query attransport.Query, sessionID int, command []byte) ([]byte, error) {
	request := append([]byte(nil), command...)
	defer zeroSIMAKABytesLocal(request)
	for attempt := 0; attempt < 3; attempt++ {
		response, err := sendCGLAAPDUOnce(ctx, query, sessionID, request)
		if err != nil || len(response) < 2 {
			zeroSIMAKABytesLocal(response)
			return nil, agentapi.ErrSIMAKAUnavailable
		}
		statusOffset := len(response) - 2
		status1, status2 := response[statusOffset], response[statusOffset+1]
		if status1 == 0x90 && status2 == 0x00 {
			data := append([]byte(nil), response[:statusOffset]...)
			zeroSIMAKABytesLocal(response)
			return data, nil
		}
		zeroSIMAKABytesLocal(response)
		switch {
		case (status1 == 0x61 || status1 == 0x9f) && attempt == 0:
			zeroSIMAKABytesLocal(request)
			request = []byte{0x00, 0xc0, 0x00, 0x00, status2}
		case status1 == 0x6c && len(request) >= 5 && attempt == 0:
			request[len(request)-1] = status2
		default:
			return nil, agentapi.ErrSIMAKAUnavailable
		}
	}
	return nil, agentapi.ErrSIMAKAUnavailable
}

func parseISIMFileSize(fcp []byte) int {
	value, ok := findISIMTLV(fcp, 0x80)
	if !ok || len(value) == 0 || len(value) > 2 {
		return 0
	}
	size := 0
	for _, current := range value {
		size = size<<8 | int(current)
	}
	return size
}

func parseISIMRecordLayout(fcp []byte) (int, int) {
	value, ok := findISIMTLV(fcp, 0x82)
	if !ok || len(value) < 5 {
		return 0, 0
	}
	return int(binary.BigEndian.Uint16(value[2:4])), int(value[4])
}

func findISIMTLV(data []byte, target byte) ([]byte, bool) {
	for position := 0; position < len(data); {
		tag := data[position]
		position++
		if tag&0x1f == 0x1f {
			for position < len(data) {
				current := data[position]
				position++
				if current&0x80 == 0 {
					break
				}
			}
		}
		length, consumed, ok := parseISIMTLVLength(data[position:])
		if !ok {
			return nil, false
		}
		position += consumed
		if length < 0 || position+length > len(data) {
			return nil, false
		}
		value := data[position : position+length]
		if tag == target {
			return value, true
		}
		if tag&0x20 != 0 {
			if nested, found := findISIMTLV(value, target); found {
				return nested, true
			}
		}
		position += length
	}
	return nil, false
}

func parseISIMTLVLength(data []byte) (int, int, bool) {
	if len(data) == 0 {
		return 0, 0, false
	}
	if data[0]&0x80 == 0 {
		return int(data[0]), 1, true
	}
	bytes := int(data[0] & 0x7f)
	if bytes < 1 || bytes > 2 || len(data) < bytes+1 {
		return 0, 0, false
	}
	length := 0
	for _, current := range data[1 : bytes+1] {
		length = length<<8 | int(current)
	}
	return length, bytes + 1, true
}

func parseISIMTextTLV(data []byte) (string, error) {
	value, ok := findISIMTLV(data, 0x80)
	if !ok {
		value, ok = findPaddedISIMTextTLV(data)
	}
	if !ok || len(value) == 0 || len(value) > 255 {
		return "", errISIMTextTLVMissing
	}
	for _, current := range value {
		if current < 0x21 || current > 0x7e {
			return "", errISIMTextEncoding
		}
	}
	return string(value), nil
}

func findPaddedISIMTextTLV(data []byte) ([]byte, bool) {
	for offset, current := range data {
		if current != 0x80 {
			if current != 0x00 && current != 0xff {
				return nil, false
			}
			continue
		}
		length, consumed, ok := parseISIMTLVLength(data[offset+1:])
		start := offset + 1 + consumed
		if !ok || length < 1 || start+length > len(data) {
			return nil, false
		}
		for _, trailing := range data[start+length:] {
			if trailing != 0x00 && trailing != 0xff {
				return nil, false
			}
		}
		return data[start : start+length], true
	}
	return nil, false
}

func classifyISIMTextShape(data []byte) string {
	if len(data) == 0 {
		return agentapi.SIMIMSShapeEmpty
	}
	start, end := 0, len(data)
	for start < end && (data[start] == 0x00 || data[start] == 0xff) {
		start++
	}
	for end > start && (data[end-1] == 0x00 || data[end-1] == 0xff) {
		end--
	}
	if start == end {
		return agentapi.SIMIMSShapePaddingOnly
	}
	trimmed := data[start:end]
	for _, current := range trimmed {
		if current == 0x80 {
			return agentapi.SIMIMSShapeTag80Malformed
		}
	}
	if len(trimmed) > 1 && int(trimmed[0]) == len(trimmed)-1 && isISIMPrintableASCII(trimmed[1:]) {
		return agentapi.SIMIMSShapeLengthPrefixedASCII
	}
	if isISIMPrintableASCII(trimmed) {
		return agentapi.SIMIMSShapeDirectASCII
	}
	if len(trimmed) > 1 {
		if length, consumed, ok := parseISIMTLVLength(trimmed[1:]); ok && 1+consumed+length <= len(trimmed) {
			return agentapi.SIMIMSShapeOtherTLV
		}
	}
	return agentapi.SIMIMSShapeOpaque
}

func isISIMPrintableASCII(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	for _, current := range data {
		if current < 0x21 || current > 0x7e {
			return false
		}
	}
	return true
}
