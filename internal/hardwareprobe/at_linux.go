//go:build linux

package hardwareprobe

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
	"golang.org/x/sys/unix"
)

type atQuerier struct{ identities IdentityPseudonymizer }

type atConnection struct {
	fd       int
	original *unix.Termios
}

type atConnectionFailure struct {
	state     string
	layer     string
	code      string
	retryable bool
	detail    string
}

var (
	errISIMTextTLVMissing = errors.New("ISIM text TLV is missing")
	errISIMTextEncoding   = errors.New("ISIM text encoding is invalid")
)

const (
	ml307AUSIMAID = "A0000000871002"
	ml307AISIMAID = "A0000000871004"
)

func NewATQuerier() ModemQuerier { return atQuerier{} }

func NewATQuerierWithIdentity(pseudonymizer IdentityPseudonymizer) ModemQuerier {
	return atQuerier{identities: pseudonymizer}
}

func (querier atQuerier) Probe(ctx context.Context, endpoint, profile string) agentapi.DeviceProbe {
	result := agentapi.DeviceProbe{
		State: endpointState(profile), Endpoint: endpoint,
		RF:             agentapi.RFObservation{State: agentapi.RFStateUnknown},
		SIM:            agentapi.SIMObservation{State: agentapi.SIMStateUnknown, PrimaryLockState: agentapi.PrimaryLockUnknown},
		SignalMetrics:  agentapi.SignalObservation{State: agentapi.SignalStateUnknown},
		Registrations:  []agentapi.RegistrationObservation{},
		CurrentNetwork: agentapi.NetworkObservation{SelectionMode: agentapi.NetworkSelectionUnknown},
	}
	if profile != agentapi.ProfileQDC507 && profile != agentapi.ProfileML307A {
		return result
	}
	connection, failure := openATConnection(endpoint)
	if failure != nil {
		return probeFailure(result, failure.state, failure.layer, failure.code, failure.retryable, failure.detail)
	}
	defer connection.Close()
	fd := connection.fd

	query := func(command string) []string {
		lines, queryErr := queryAT(ctx, fd, command, 1500*time.Millisecond)
		if queryErr != nil {
			return nil
		}
		return lines
	}
	if lines, err := queryAT(ctx, fd, "AT", time.Second); err != nil || !hasTerminalOK(lines) {
		if ctx.Err() != nil {
			return probeFailure(result, agentapi.ProbeStateFailed, agentapi.ErrorLayerTransport, agentapi.ErrorProbeCancelled, true, "read-only probe was cancelled")
		}
		return probeFailure(result, agentapi.ProbeStateFailed, agentapi.ErrorLayerTransport, agentapi.ErrorModemNoResponse, true, "AT endpoint did not complete the bounded handshake")
	}

	manufacturer := query("AT+CGMI")
	model := query("AT+CGMM")
	revision := query("AT+CGMR")
	cfun := query("AT+CFUN?")
	cpin := query("AT+CPIN?")
	var simStatus []string
	if profile == agentapi.ProfileQDC507 {
		simStatus = query("AT+QSIMSTAT?")
	}
	creg := query("AT+CREG?")
	cgreg := query("AT+CGREG?")
	cereg := query("AT+CEREG?")
	cops := query("AT+COPS?")
	var network []string
	if profile == agentapi.ProfileQDC507 {
		network = query("AT+QNWINFO")
	}
	signal := query("AT+CSQ")
	calls := query("AT+CLCC")
	var usbConfiguration []string
	if profile == agentapi.ProfileQDC507 {
		usbConfiguration = query("AT+QCFG=\"USBCFG\"")
	}
	if ctx.Err() != nil {
		return probeFailure(result, agentapi.ProbeStateFailed, agentapi.ErrorLayerTransport, agentapi.ErrorProbeCancelled, true, "read-only probe was cancelled")
	}

	result.RF = rfObservation(cfun)
	result.RF.Network = firstPayload(network, "+QNWINFO:")
	result.RF.Signal = firstPayload(signal, "+CSQ:")
	if !hasTerminalOK(cfun) || result.RF.State == agentapi.RFStateUnknown {
		return probeFailure(result, agentapi.ProbeStateFailed, agentapi.ErrorLayerRadio, agentapi.ErrorRFStateUnavailable, true, "functional mode could not be read")
	}
	if !hasTerminalResponse(cpin) {
		return probeFailure(result, agentapi.ProbeStateFailed, agentapi.ErrorLayerSIM, agentapi.ErrorSIMStateUnavailable, true, "SIM lock state could not be read")
	}
	if !hasTerminalOK(calls) {
		return probeFailure(result, agentapi.ProbeStateFailed, agentapi.ErrorLayerCall, agentapi.ErrorCallStateUnknown, true, "active call state could not be read")
	}

	count := activeCallCount(calls)
	result.State = agentapi.ProbeStateComplete
	result.Identity = agentapi.ModemIdentity{
		Manufacturer: identityPayload(manufacturer), Model: identityPayload(model), Revision: identityPayload(revision),
	}
	result.SIM = simObservation(cpin, simStatus)
	if profile == agentapi.ProfileML307A && result.SIM.State == agentapi.SIMStatePresent &&
		result.SIM.PrimaryLockState == agentapi.PrimaryLockReady && querier.identities != nil {
		result.SIM.IdentityFingerprint, result.SIM.DisplayIdentityHint = pseudonymizedML307AICCID(query("AT+MCCID"), querier.identities)
	}
	result.SignalMetrics = signalObservation(signal)
	result.Registrations = registrationObservations(creg, cgreg, cereg)
	result.CurrentNetwork = networkObservation(cops, network)
	result.ActiveCallCount = &count
	result.USBConfiguration = firstPayload(usbConfiguration, "+QCFG:")
	return result
}

func (atQuerier) EnsureRadioOff(ctx context.Context, endpoint, profile string) agentapi.RadioEnsureOffExecution {
	result := agentapi.RadioEnsureOffExecution{
		Observation: agentapi.RadioEnsureOffObservation{RF: agentapi.RFObservation{State: agentapi.RFStateUnknown}},
	}
	if profile != agentapi.ProfileQDC507 {
		result.Error = &agentapi.ProbeError{Layer: agentapi.ErrorLayerPlatform, Code: agentapi.ErrorPlatformUnsupported}
		return result
	}
	connection, failure := openATConnection(endpoint)
	if failure != nil {
		result.Error = &agentapi.ProbeError{Layer: failure.layer, Code: failure.code, Retryable: failure.retryable}
		return result
	}
	defer connection.Close()
	fd := connection.fd
	return ensureRadioOffWithQuery(ctx, func(ctx context.Context, command string, timeout time.Duration) ([]string, error) {
		return queryAT(ctx, fd, command, timeout)
	})
}

func (querier atQuerier) ReadSIMAKAIdentity(ctx context.Context, endpoint, profile, identityFingerprint string) (string, error) {
	if profile != agentapi.ProfileML307A || querier.identities == nil {
		return "", agentapi.ErrSIMAKAUnsupported
	}
	connection, failure := openATConnection(endpoint)
	if failure != nil {
		return "", agentapi.ErrSIMAKAUnavailable
	}
	defer connection.Close()
	query := func(ctx context.Context, command string, timeout time.Duration) ([]string, error) {
		return queryAT(ctx, connection.fd, command, timeout)
	}
	if err := validateML307ASIMAKATarget(ctx, query, querier.identities, identityFingerprint); err != nil {
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

func (querier atQuerier) AuthenticateSIMAKA(ctx context.Context, endpoint, profile, identityFingerprint string, challenge agentapi.SIMAKAChallenge) (agentapi.SIMAKAExecution, error) {
	if profile != agentapi.ProfileML307A || querier.identities == nil {
		return agentapi.SIMAKAExecution{}, agentapi.ErrSIMAKAUnsupported
	}
	connection, failure := openATConnection(endpoint)
	if failure != nil {
		return agentapi.SIMAKAExecution{}, agentapi.ErrSIMAKAUnavailable
	}
	defer connection.Close()
	query := func(ctx context.Context, command string, timeout time.Duration) ([]string, error) {
		return queryATBounded(ctx, connection.fd, command, timeout, 256)
	}
	if err := validateML307ASIMAKATarget(ctx, query, querier.identities, identityFingerprint); err != nil {
		return agentapi.SIMAKAExecution{}, err
	}
	return exchangeML307AUSIMAKA(ctx, query, challenge)
}

func (querier atQuerier) ProbeSIMIMSProfile(ctx context.Context, endpoint, profile, identityFingerprint string) (bool, error) {
	if profile != agentapi.ProfileML307A || querier.identities == nil {
		return false, agentapi.ErrSIMAKAUnsupported
	}
	connection, failure := openATConnection(endpoint)
	if failure != nil {
		return false, agentapi.ErrSIMAKAUnavailable
	}
	defer connection.Close()
	query := func(ctx context.Context, command string, timeout time.Duration) ([]string, error) {
		return queryATBounded(ctx, connection.fd, command, timeout, 128)
	}
	if err := validateML307ASIMAKATarget(ctx, query, querier.identities, identityFingerprint); err != nil {
		return false, err
	}
	_, available, err := readML307AISIMIdentity(ctx, query)
	return available, err
}

func (querier atQuerier) ReadSIMIMSIdentity(ctx context.Context, endpoint, profile, identityFingerprint string) (agentapi.SIMIMSIdentityMaterial, error) {
	if profile != agentapi.ProfileML307A || querier.identities == nil {
		return agentapi.SIMIMSIdentityMaterial{}, agentapi.ErrSIMAKAUnsupported
	}
	connection, failure := openATConnection(endpoint)
	if failure != nil {
		return agentapi.SIMIMSIdentityMaterial{}, agentapi.ErrSIMAKAUnavailable
	}
	defer connection.Close()
	query := func(ctx context.Context, command string, timeout time.Duration) ([]string, error) {
		return queryATBounded(ctx, connection.fd, command, timeout, 512)
	}
	if err := validateML307ASIMAKATarget(ctx, query, querier.identities, identityFingerprint); err != nil {
		return agentapi.SIMIMSIdentityMaterial{}, err
	}
	material, available, err := readML307AISIMIdentity(ctx, query)
	if err != nil {
		return agentapi.SIMIMSIdentityMaterial{}, err
	}
	if available {
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
	domain := "ims.mnc015.mcc234.3gppnetwork.org"
	privateIdentity := imsi + "@" + domain
	return agentapi.SIMIMSIdentityMaterial{
		Source: agentapi.SIMIMSIdentityDerived, PrivateIdentity: privateIdentity,
		HomeDomain: domain, PublicIdentities: []string{"sip:" + privateIdentity},
		ApplicationDiscovery:  material.ApplicationDiscovery,
		ApplicationCandidates: material.ApplicationCandidates,
	}, nil
}

func validateML307ASIMAKATarget(ctx context.Context, query boundedATQuery, identities IdentityPseudonymizer, expectedFingerprint string) error {
	if identities == nil || len(expectedFingerprint) != 64 {
		return agentapi.ErrSIMAKAUnavailable
	}
	if lines, err := query(ctx, "AT", time.Second); err != nil || !hasTerminalOK(lines) {
		return agentapi.ErrSIMAKAUnavailable
	}
	cfun, err := query(ctx, "AT+CFUN?", 2*time.Second)
	if err != nil || !hasTerminalOK(cfun) {
		return agentapi.ErrSIMAKAUnavailable
	}
	if rfObservation(cfun).State != agentapi.RFStateOff {
		return agentapi.ErrSIMAKARFNotOff
	}
	cpin, err := query(ctx, "AT+CPIN?", 2*time.Second)
	if err != nil || !hasTerminalOK(cpin) {
		return agentapi.ErrSIMAKAUnavailable
	}
	sim := simObservation(cpin, nil)
	if sim.State != agentapi.SIMStatePresent || sim.PrimaryLockState != agentapi.PrimaryLockReady {
		return agentapi.ErrSIMAKASIMNotReady
	}
	mccid, err := query(ctx, "AT+MCCID", 2*time.Second)
	if err != nil {
		return agentapi.ErrSIMAKAUnavailable
	}
	fingerprint, _ := pseudonymizedML307AICCID(mccid, identities)
	if fingerprint == "" {
		return agentapi.ErrSIMAKAUnavailable
	}
	if fingerprint != expectedFingerprint {
		return agentapi.ErrSIMAKAIdentityChanged
	}
	return nil
}

func parseML307AIMSI(lines []string) string {
	if !hasTerminalOK(lines) {
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

func exchangeML307AUSIMAKA(ctx context.Context, query boundedATQuery, challenge agentapi.SIMAKAChallenge) (agentapi.SIMAKAExecution, error) {
	opened, err := query(ctx, fmt.Sprintf("AT+CCHO=\"%s\"", ml307AUSIMAID), 3*time.Second)
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

func readML307AISIMIdentity(ctx context.Context, query boundedATQuery) (agentapi.SIMIMSIdentityMaterial, bool, error) {
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

func readML307AISIMIdentityAID(ctx context.Context, query boundedATQuery, aid string) (agentapi.SIMIMSIdentityMaterial, bool, error) {
	if !validML307AISIMAID(aid) {
		return agentapi.SIMIMSIdentityMaterial{}, false, agentapi.ErrSIMAKAUnavailable
	}
	opened, err := query(ctx, fmt.Sprintf("AT+CCHO=\"%s\"", aid), 3*time.Second)
	if err != nil {
		return agentapi.SIMIMSIdentityMaterial{}, false, agentapi.NewSIMIMSHILStageError(agentapi.SIMIMSStageApplicationOpen)
	}
	sessionID := parseCCHOSessionID(opened)
	if sessionID == 0 {
		if hasTerminalResponse(opened) {
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
	if closeErr != nil || !hasTerminalOK(closeLines) {
		return agentapi.SIMIMSIdentityMaterial{}, true, agentapi.NewSIMIMSHILStageError(agentapi.SIMIMSStageChannelClose)
	}
	closed = true
	return agentapi.SIMIMSIdentityMaterial{
		Source: agentapi.SIMIMSIdentityISIM, PrivateIdentity: privateIdentity,
		HomeDomain: homeDomain, PublicIdentities: publicIdentities,
	}, true, nil
}

func discoverML307AISIMAIDs(ctx context.Context, query boundedATQuery) ([]string, bool, error) {
	lines, err := query(ctx, "AT+CUAD=0", 5*time.Second)
	if err != nil {
		return nil, false, err
	}
	if !hasTerminalOK(lines) {
		if hasTerminalResponse(lines) {
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

func discoverML307AISIMAIDsCRSM(ctx context.Context, query boundedATQuery) ([]string, bool, error) {
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
		if !hasTerminalOK(lines) {
			if record == 1 && hasTerminalResponse(lines) {
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

func readISIMTransparentText(ctx context.Context, query boundedATQuery, sessionID int, fileID uint16,
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

func readISIMPublicIdentities(ctx context.Context, query boundedATQuery, sessionID int) ([]string, error) {
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

func sendISIMAPDU(ctx context.Context, query boundedATQuery, sessionID int, command []byte) ([]byte, error) {
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

func buildUSIMAKAAPDU(challenge agentapi.SIMAKAChallenge) []byte {
	command := make([]byte, 0, 39)
	command = append(command, 0x00, 0x88, 0x00, 0x81, 0x22, 0x10)
	command = append(command, challenge.RAND[:]...)
	command = append(command, 0x10)
	command = append(command, challenge.AUTN[:]...)
	return command
}

func sendCGLAAPDU(ctx context.Context, query boundedATQuery, sessionID int, apdu []byte) ([]byte, error) {
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

func sendCGLAAPDUOnce(ctx context.Context, query boundedATQuery, sessionID int, apdu []byte) ([]byte, error) {
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
	if !hasTerminalOK(lines) {
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
	if !hasTerminalOK(lines) {
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

func openATConnection(endpoint string) (*atConnection, *atConnectionFailure) {
	if deviceOpenByAnotherProcess(endpoint) {
		return nil, &atConnectionFailure{
			state: agentapi.ProbeStateBusy, layer: agentapi.ErrorLayerTransport, code: agentapi.ErrorControlEndpointBusy,
			retryable: true, detail: "AT endpoint is already open by another process",
		}
	}
	fd, err := unix.Open(endpoint, unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM) {
			return nil, &atConnectionFailure{
				state: agentapi.ProbeStateUnavailable, layer: agentapi.ErrorLayerDevice, code: agentapi.ErrorControlPermissionDenied,
				detail: "AT endpoint permission was denied",
			}
		}
		return nil, &atConnectionFailure{
			state: agentapi.ProbeStateUnavailable, layer: agentapi.ErrorLayerDevice, code: agentapi.ErrorControlEndpointOpen,
			retryable: true, detail: "AT endpoint could not be opened",
		}
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = unix.Close(fd)
		return nil, &atConnectionFailure{
			state: agentapi.ProbeStateBusy, layer: agentapi.ErrorLayerTransport, code: agentapi.ErrorControlEndpointBusy,
			retryable: true, detail: "AT endpoint lock is unavailable",
		}
	}
	original, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		_ = unix.Flock(fd, unix.LOCK_UN)
		_ = unix.Close(fd)
		return nil, &atConnectionFailure{
			state: agentapi.ProbeStateFailed, layer: agentapi.ErrorLayerTransport, code: agentapi.ErrorControlEndpointConfigure,
			retryable: true, detail: "AT endpoint termios cannot be read",
		}
	}
	configured := *original
	configured.Iflag = unix.IGNPAR
	configured.Oflag = 0
	configured.Cflag = unix.B115200 | unix.CS8 | unix.CREAD | unix.CLOCAL
	configured.Lflag = 0
	configured.Cc[unix.VMIN] = 0
	configured.Cc[unix.VTIME] = 0
	configured.Ispeed = unix.B115200
	configured.Ospeed = unix.B115200
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &configured); err != nil {
		_ = unix.IoctlSetTermios(fd, unix.TCSETS, original)
		_ = unix.Flock(fd, unix.LOCK_UN)
		_ = unix.Close(fd)
		return nil, &atConnectionFailure{
			state: agentapi.ProbeStateFailed, layer: agentapi.ErrorLayerTransport, code: agentapi.ErrorControlEndpointConfigure,
			retryable: true, detail: "AT endpoint termios cannot be configured",
		}
	}
	_ = unix.IoctlSetInt(fd, unix.TCFLSH, unix.TCIOFLUSH)
	return &atConnection{fd: fd, original: original}, nil
}

func (connection *atConnection) Close() {
	if connection == nil || connection.fd < 0 {
		return
	}
	if connection.original != nil {
		_ = unix.IoctlSetTermios(connection.fd, unix.TCSETS, connection.original)
	}
	_ = unix.Flock(connection.fd, unix.LOCK_UN)
	_ = unix.Close(connection.fd)
	connection.fd = -1
}

func endpointState(profile string) string {
	if profile == agentapi.ProfileQDC507 || profile == agentapi.ProfileML307A {
		return agentapi.ProbeStateFailed
	}
	return agentapi.ProbeStateDescriptorOnly
}

func probeFailure(result agentapi.DeviceProbe, state, layer, code string, retryable bool, detail string) agentapi.DeviceProbe {
	result.State = state
	result.Error = &agentapi.ProbeError{Layer: layer, Code: code, Retryable: retryable}
	result.ErrorCode = code
	result.ErrorDetail = detail
	return result
}

func queryAT(ctx context.Context, fd int, command string, timeout time.Duration) ([]string, error) {
	return queryATBounded(ctx, fd, command, timeout, 64)
}

func queryATBounded(ctx context.Context, fd int, command string, timeout time.Duration, maximumCommandLength int) ([]string, error) {
	if command == "" || maximumCommandLength < 1 || len(command) > maximumCommandLength || strings.ContainsAny(command, "\r\n") {
		return nil, errors.New("invalid bounded AT query")
	}
	_ = unix.IoctlSetInt(fd, unix.TCFLSH, unix.TCIFLUSH)
	wirePayload := []byte(command + "\r")
	defer zeroSIMAKABytesLocal(wirePayload)
	payload := wirePayload
	for len(payload) != 0 {
		if err := pollContext(ctx, fd, unix.POLLOUT, timeout); err != nil {
			return nil, err
		}
		written, err := unix.Write(fd, payload)
		if err != nil {
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR) {
				continue
			}
			return nil, err
		}
		payload = payload[written:]
	}
	deadline := time.Now().Add(timeout)
	buffer := make([]byte, 0, 2048)
	temporary := make([]byte, 512)
	defer func() {
		zeroSIMAKABytesLocal(buffer)
		zeroSIMAKABytesLocal(temporary)
	}()
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		if err := pollContext(ctx, fd, unix.POLLIN, remaining); err != nil {
			return nil, err
		}
		count, err := unix.Read(fd, temporary)
		if err != nil {
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR) {
				continue
			}
			return nil, err
		}
		if count == 0 {
			continue
		}
		if len(buffer)+count > 8192 {
			return nil, errors.New("AT response exceeds bounded size")
		}
		buffer = append(buffer, temporary[:count]...)
		lines := splitATLines(string(buffer), command)
		if hasTerminalResponse(lines) {
			return lines, nil
		}
	}
	return nil, errors.New("AT query timed out")
}

func pollContext(ctx context.Context, fd int, events int16, timeout time.Duration) error {
	if timeout <= 0 {
		return errors.New("poll timed out")
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		remaining := time.Until(deadline)
		milliseconds := int(remaining.Milliseconds())
		if milliseconds < 1 {
			milliseconds = 1
		}
		if milliseconds > 200 {
			milliseconds = 200
		}
		pollFD := []unix.PollFd{{Fd: int32(fd), Events: events}}
		count, err := unix.Poll(pollFD, milliseconds)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return err
		}
		if count == 0 {
			continue
		}
		if pollFD[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			return errors.New("AT endpoint became unavailable")
		}
		if pollFD[0].Revents&events != 0 {
			return nil
		}
	}
	return errors.New("poll timed out")
}

func splitATLines(response, command string) []string {
	response = strings.ReplaceAll(response, "\r", "\n")
	parts := strings.Split(response, "\n")
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == command {
			continue
		}
		lines = append(lines, safeProbeText(part, 256))
	}
	return lines
}

func hasTerminalResponse(lines []string) bool {
	for _, line := range lines {
		if line == "OK" || line == "ERROR" || strings.HasPrefix(line, "+CME ERROR:") || strings.HasPrefix(line, "+CMS ERROR:") {
			return true
		}
	}
	return false
}

func deviceOpenByAnotherProcess(path string) bool {
	target, err := os.Stat(path)
	if err != nil {
		return false
	}
	processes, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	self := os.Getpid()
	for _, process := range processes {
		pid, err := strconv.Atoi(process.Name())
		if err != nil || pid == self {
			continue
		}
		fds, err := os.ReadDir(filepath.Join("/proc", process.Name(), "fd"))
		if err != nil {
			continue
		}
		for _, fd := range fds {
			candidate, err := os.Stat(filepath.Join("/proc", process.Name(), "fd", fd.Name()))
			if err == nil && os.SameFile(target, candidate) {
				return true
			}
		}
	}
	return false
}
