package vowifihil

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/leonfox28/simplus/internal/smscodec"
)

var (
	ErrIMSSMSUnavailable     = errors.New("SMS over IMS is unavailable")
	ErrIMSSMSMessageNotFound = errors.New("SMS over IMS message was not found")
	ErrIMSSMSRejected        = errors.New("SMS over IMS was rejected")
	ErrIMSSMSOutcomeUnknown  = errors.New("SMS over IMS outcome is unknown")
)

type IMSSMSReference struct {
	MessageID  string
	ReceivedAt time.Time
}

type IMSSMSMessage struct {
	MessageID  string
	Sender     string
	Body       string
	ReceivedAt time.Time
	Segment    smscodec.Segment
}

type pendingIMSSMS struct {
	message    IMSSMSMessage
	reference  byte
	gatewayURI string
	inReplyTo  string
}

const (
	IMSSMSSubmitAccepted    = "accepted"
	IMSSMSSubmitSent        = "sent"
	IMSSMSSubmitFailed      = "failed"
	IMSSMSSubmitUnconfirmed = "unconfirmed"

	IMSSMSSubmitErrorRejected = "IMS_SMS_REJECTED"
	IMSSMSSubmitErrorPartial  = "IMS_SMS_PARTIAL_OR_REJECTED"
	IMSSMSSubmitErrorUnknown  = "SMS_SEND_OUTCOME_UNKNOWN"

	defaultIMSSubmitReportWait = 130 * time.Second
	maxPendingIMSSubmissions   = 256
)

type IMSSMSSubmission struct {
	ProviderMessageID string
	State             string
	ErrorCode         string
}

// IMSSMSSubmitReport is a sanitized terminal network report for an outbound
// user operation. It deliberately contains no addresses, text, PDU or SIP
// identity material.
type IMSSMSSubmitReport struct {
	MessageID         string
	ProviderMessageID string
	State             string
	ErrorCode         string
	Cause             byte
	CompletedAt       time.Time
}

type IMSSMSProtocolSnapshot struct {
	SIPRequests         uint64
	SIPParseFailures    uint64
	RPParseFailures     uint64
	RPDataDeliveries    uint64
	RPACKs              uint64
	RPErrors            uint64
	CorrelationFailures uint64
	ReportTimeouts      uint64
}

type imsSMSProtocolCounters IMSSMSProtocolSnapshot

type pendingIMSSubmitSegment struct {
	providerMessageID string
	callID            string
	reported          bool
}

type pendingIMSSubmitOperation struct {
	messageID          string
	providerMessageID  string
	totalSegments      int
	acceptedSegments   int
	reportedSegments   int
	rejectedSegments   int
	lastCause          byte
	submissionComplete bool
	deadline           time.Time
}

type imsSMSInReplyToMode uint8

const (
	imsSMSInReplyToUnknown imsSMSInReplyToMode = iota
	imsSMSInReplyToSupported
	imsSMSInReplyToUnsupported
)

// SMSAvailable reports whether the SIM provided the service-centre routing
// material needed to advertise and use SMS over IMS.
func (session *IMSSession) SMSAvailable() bool {
	if session == nil {
		return false
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	return !session.closed && session.input.SMSCapable && session.serviceCentreURI != "" && session.serviceCentreAddress != ""
}

// SubmitSMS submits already encoded SMS-SUBMIT TPDUs one at a time, as
// required by TS 24.341. It returns after every segment has received a final
// SIP response; it never waits for an RP-ACK/RP-ERROR. A SIP 2xx is therefore
// "accepted", not proof that the SMSC accepted or delivered the message.
func (session *IMSSession) SubmitSMS(ctx context.Context, messageID string, tpdus [][]byte) (IMSSMSSubmission, error) {
	if session == nil || !validOpaqueSMSID(messageID) || len(tpdus) == 0 || len(tpdus) > 255 {
		return IMSSMSSubmission{}, ErrIMSSMSUnavailable
	}
	for _, tpdu := range tpdus {
		if len(tpdu) == 0 || len(tpdu) > maxRPUserDataSize {
			return IMSSMSSubmission{}, ErrIMSSMSUnavailable
		}
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed || !session.input.SMSCapable || session.client == nil || session.server == nil ||
		session.serviceCentreURI == "" || session.serviceCentreAddress == "" {
		return IMSSMSSubmission{}, ErrIMSSMSUnavailable
	}
	session.ensureSMSSubmitStateLocked()
	session.expireSubmitOperationsLocked(time.Now().UTC())
	if len(session.submitOperations)+len(session.completedSMSReports) >= maxPendingIMSSubmissions {
		return IMSSMSSubmission{}, ErrIMSSMSUnavailable
	}
	providerToken, err := randomHexToken(12)
	if err != nil {
		return IMSSMSSubmission{}, ErrIMSSMSUnavailable
	}
	providerMessageID := "ims_" + providerToken
	operation := &pendingIMSSubmitOperation{
		messageID: messageID, providerMessageID: providerMessageID, totalSegments: len(tpdus),
	}
	session.submitOperations[providerMessageID] = operation
	for _, tpdu := range tpdus {
		reference, available := session.nextRPReferenceLocked()
		if !available {
			session.cleanupSubmitOperationLocked(providerMessageID)
			if operation.acceptedSegments > 0 || operation.reportedSegments > 0 {
				return IMSSMSSubmission{ProviderMessageID: providerMessageID, State: IMSSMSSubmitUnconfirmed, ErrorCode: IMSSMSSubmitErrorUnknown}, nil
			}
			return IMSSMSSubmission{}, ErrIMSSMSUnavailable
		}
		rpdu, err := BuildRPDataSubmit(reference, session.serviceCentreAddress, tpdu)
		if err != nil {
			session.cleanupSubmitOperationLocked(providerMessageID)
			if operation.acceptedSegments > 0 || operation.reportedSegments > 0 {
				return IMSSMSSubmission{ProviderMessageID: providerMessageID, State: IMSSMSSubmitUnconfirmed, ErrorCode: IMSSMSSubmitErrorUnknown}, nil
			}
			return IMSSMSSubmission{}, ErrIMSSMSUnavailable
		}
		callID, sequence, packet, err := session.buildSMSRequestLocked(session.serviceCentreURI, "", rpdu)
		zeroBytes(rpdu)
		if err != nil {
			session.cleanupSubmitOperationLocked(providerMessageID)
			if operation.acceptedSegments > 0 || operation.reportedSegments > 0 {
				return IMSSMSSubmission{ProviderMessageID: providerMessageID, State: IMSSMSSubmitUnconfirmed, ErrorCode: IMSSMSSubmitErrorUnknown}, nil
			}
			return IMSSMSSubmission{}, ErrIMSSMSUnavailable
		}
		session.submitSegments[reference] = pendingIMSSubmitSegment{providerMessageID: providerMessageID, callID: callID}
		response, err := session.exchangeProtectedRequestLocked(ctx, packet, callID, sequence, "MESSAGE", 15*time.Second)
		zeroBytes(packet)
		if err != nil {
			session.cleanupSubmitOperationLocked(providerMessageID)
			return IMSSMSSubmission{ProviderMessageID: providerMessageID, State: IMSSMSSubmitUnconfirmed, ErrorCode: IMSSMSSubmitErrorUnknown}, nil
		}
		parsed, err := parseSIPPacket(response)
		zeroBytes(response)
		if err != nil || parsed.Status < 200 || parsed.Status > 299 {
			session.cleanupSubmitOperationLocked(providerMessageID)
			if operation.acceptedSegments > 0 || operation.reportedSegments > 0 {
				return IMSSMSSubmission{ProviderMessageID: providerMessageID, State: IMSSMSSubmitUnconfirmed, ErrorCode: IMSSMSSubmitErrorUnknown}, nil
			}
			return IMSSMSSubmission{}, ErrIMSSMSRejected
		}
		operation.acceptedSegments++
	}
	completedAt := time.Now().UTC()
	operation.deadline = completedAt.Add(session.submitReportWaitLocked())
	operation.submissionComplete = true
	session.completeSubmitOperationIfReadyLocked(operation, completedAt)
	if report, found := session.completedSMSReports[providerMessageID]; found {
		return IMSSMSSubmission{ProviderMessageID: providerMessageID, State: report.State, ErrorCode: report.ErrorCode}, nil
	}
	return IMSSMSSubmission{ProviderMessageID: providerMessageID, State: IMSSMSSubmitAccepted}, nil
}

// PollSMS drains protected SIP traffic for a bounded interval. A context
// deadline is a normal empty poll, not a session failure.
func (session *IMSSession) PollSMS(ctx context.Context) error {
	if session == nil {
		return ErrIMSSMSUnavailable
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed || session.client == nil || session.server == nil {
		return ErrIMSSMSUnavailable
	}
	session.ensureSMSSubmitStateLocked()
	session.expireSubmitOperationsLocked(time.Now().UTC())
	for {
		_, received, err := session.readProtectedPacketLocked(ctx, time.Now().Add(100*time.Millisecond))
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
		if !received {
			session.expireSubmitOperationsLocked(time.Now().UTC())
			return nil
		}
	}
}

func (session *IMSSession) ListSMS() []IMSSMSReference {
	if session == nil {
		return nil
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	result := make([]IMSSMSReference, 0, len(session.pendingSMS))
	for _, pending := range session.pendingSMS {
		result = append(result, IMSSMSReference{MessageID: pending.message.MessageID, ReceivedAt: pending.message.ReceivedAt})
	}
	sortIMSSMSReferences(result)
	return result
}

func (session *IMSSession) ReadSMS(messageID string) (IMSSMSMessage, error) {
	if session == nil {
		return IMSSMSMessage{}, ErrIMSSMSMessageNotFound
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	pending, found := session.pendingSMS[messageID]
	if !found {
		return IMSSMSMessage{}, ErrIMSSMSMessageNotFound
	}
	message := pending.message
	message.Segment.UserData = append([]byte(nil), message.Segment.UserData...)
	return message, nil
}

func (session *IMSSession) AcknowledgeSMS(ctx context.Context, messageID, operationID string) error {
	if session == nil || !validOpaqueSMSID(messageID) || !validOpaqueSMSID(operationID) {
		return ErrIMSSMSMessageNotFound
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if acknowledged, found := session.acknowledgedSMS[operationID]; found {
		if acknowledged == messageID {
			return nil
		}
		return ErrIMSSMSMessageNotFound
	}
	pending, found := session.pendingSMS[messageID]
	if !found || pending.gatewayURI == "" {
		return ErrIMSSMSMessageNotFound
	}
	inReplyTo := pending.inReplyTo
	if session.smsInReplyToMode == imsSMSInReplyToUnsupported {
		inReplyTo = ""
	}
	status, err := session.sendRPDeliveryACKLocked(ctx, pending, inReplyTo, BuildRPDeliveryACK(pending.reference))
	if err != nil {
		return err
	}
	if status >= 200 && status <= 299 {
		if inReplyTo == "" {
			session.smsInReplyToMode = imsSMSInReplyToUnsupported
		} else {
			session.smsInReplyToMode = imsSMSInReplyToSupported
		}
	} else if status == 488 && inReplyTo != "" && session.smsInReplyToMode == imsSMSInReplyToUnknown {
		// TS 24.341 permits an IP-SM-GW without In-Reply-To support. A
		// correlated attempt is made first; only an explicit correlation
		// rejection allows one bounded retry of the same RP-ACK without it.
		status, err = session.sendRPDeliveryACKLocked(ctx, pending, "", BuildRPDeliveryACK(pending.reference))
		if err != nil {
			return err
		}
		if status >= 200 && status <= 299 {
			session.smsInReplyToMode = imsSMSInReplyToUnsupported
		}
	}
	if status < 200 || status > 299 {
		return ErrIMSSMSRejected
	}
	delete(session.pendingSMS, messageID)
	if len(session.acknowledgedSMS) >= maxPendingIMSSubmissions {
		clear(session.acknowledgedSMS)
	}
	session.acknowledgedSMS[operationID] = messageID
	return nil
}

func (session *IMSSession) sendRPDeliveryACKLocked(ctx context.Context, pending pendingIMSSMS, inReplyTo string,
	rpdu []byte) (int, error) {
	callID, sequence, packet, err := session.buildSMSRequestForIdentityLocked(pending.gatewayURI, session.messagePublicIdentityLocked(),
		inReplyTo, rpdu)
	zeroBytes(rpdu)
	if err != nil {
		return 0, ErrIMSSMSUnavailable
	}
	response, err := session.exchangeProtectedRequestLocked(ctx, packet, callID, sequence, "MESSAGE", 15*time.Second)
	zeroBytes(packet)
	if err != nil {
		return 0, ErrIMSSMSOutcomeUnknown
	}
	parsed, err := parseSIPPacket(response)
	zeroBytes(response)
	if err != nil {
		return 0, ErrIMSSMSRejected
	}
	return parsed.Status, nil
}

func (session *IMSSession) buildSMSRequestLocked(requestURI, inReplyTo string, rpdu []byte) (string, uint64, []byte, error) {
	return session.buildSMSRequestForIdentityLocked(requestURI, session.messagePublicIdentityLocked(), inReplyTo, rpdu)
}

func (session *IMSSession) messagePublicIdentityLocked() string {
	if session.authorizedIdentity != "" {
		return session.authorizedIdentity
	}
	return session.input.PublicIdentity
}

func (session *IMSSession) buildSMSRequestForIdentityLocked(requestURI, publicIdentity, inReplyTo string,
	rpdu []byte) (string, uint64, []byte, error) {
	branch, err := randomHexToken(8)
	if err != nil {
		return "", 0, nil, err
	}
	fromTag, err := randomHexToken(8)
	if err != nil {
		return "", 0, nil, err
	}
	callToken, err := randomHexToken(8)
	if err != nil {
		return "", 0, nil, err
	}
	sequence := session.smsSequence
	if sequence == 0 || sequence >= 1<<31-1 {
		sequence = 1
	}
	session.smsSequence = sequence + 1
	callID := callToken + "@" + session.input.Source.String()
	packet, err := buildSMSIPMessage(smsSIPRequestInput{
		Source: session.input.Source, ViaPort: session.input.ProtectedServerPort,
		RequestURI: requestURI, PublicIdentity: publicIdentity,
		Branch: branch, FromTag: fromTag, CallID: callID, InReplyTo: inReplyTo, Sequence: sequence,
		Routes: append([]string(nil), session.serviceRoutes...), SecurityVerify: session.challenge.SecurityServer.Raw,
		WLANNodeID: session.input.WLANNodeID, Body: rpdu,
	})
	return callID, sequence, packet, err
}

func (session *IMSSession) nextRPReferenceLocked() (byte, bool) {
	now := time.Now().UTC()
	for range 256 {
		reference := session.rpReference
		session.rpReference++
		if _, inUse := session.submitSegments[reference]; inUse {
			continue
		}
		if retiredUntil, retired := session.retiredRPReferences[reference]; retired {
			if retiredUntil.After(now) {
				continue
			}
			delete(session.retiredRPReferences, reference)
		}
		return reference, true
	}
	return 0, false
}

func (session *IMSSession) submitReportWaitLocked() time.Duration {
	if session.smsSubmitReportWait > 0 {
		return session.smsSubmitReportWait
	}
	return defaultIMSSubmitReportWait
}

func (session *IMSSession) ensureSMSSubmitStateLocked() {
	if session.submitSegments == nil {
		session.submitSegments = make(map[byte]pendingIMSSubmitSegment)
	}
	if session.retiredRPReferences == nil {
		session.retiredRPReferences = make(map[byte]time.Time)
	}
	if session.submitOperations == nil {
		session.submitOperations = make(map[string]*pendingIMSSubmitOperation)
	}
	if session.completedSMSReports == nil {
		session.completedSMSReports = make(map[string]IMSSMSSubmitReport)
	}
	if session.acknowledgedReports == nil {
		session.acknowledgedReports = make(map[string]struct{})
	}
}

func (session *IMSSession) cleanupSubmitOperationLocked(providerMessageID string) {
	session.ensureSMSSubmitStateLocked()
	delete(session.submitOperations, providerMessageID)
	retiredUntil := time.Now().UTC().Add(session.submitReportWaitLocked())
	for reference, segment := range session.submitSegments {
		if segment.providerMessageID == providerMessageID {
			delete(session.submitSegments, reference)
			session.retiredRPReferences[reference] = retiredUntil
		}
	}
}

func (session *IMSSession) recordSubmitReportLocked(message RPMessage) {
	segment, found := session.submitSegments[message.Reference]
	if !found {
		return
	}
	operation := session.submitOperations[segment.providerMessageID]
	if operation == nil {
		return
	}
	switch message.Type {
	case rpACKNetworkToMS:
		session.smsProtocolCounters.RPACKs++
	case rpErrorNetworkToMS:
		session.smsProtocolCounters.RPErrors++
	default:
		return
	}
	if segment.reported {
		return
	}
	segment.reported = true
	session.submitSegments[message.Reference] = segment
	operation.reportedSegments++
	if message.Type == rpErrorNetworkToMS {
		operation.rejectedSegments++
		operation.lastCause = message.Cause
	}
	session.completeSubmitOperationIfReadyLocked(operation, time.Now().UTC())
}

func (session *IMSSession) completeSubmitOperationIfReadyLocked(operation *pendingIMSSubmitOperation, completedAt time.Time) {
	if operation == nil || !operation.submissionComplete || operation.reportedSegments < operation.totalSegments {
		return
	}
	state, errorCode := IMSSMSSubmitSent, ""
	if operation.rejectedSegments > 0 {
		if operation.totalSegments == 1 {
			state, errorCode = IMSSMSSubmitFailed, IMSSMSSubmitErrorRejected
		} else {
			state, errorCode = IMSSMSSubmitUnconfirmed, IMSSMSSubmitErrorPartial
		}
	}
	session.completedSMSReports[operation.providerMessageID] = IMSSMSSubmitReport{
		MessageID: operation.messageID, ProviderMessageID: operation.providerMessageID,
		State: state, ErrorCode: errorCode, Cause: operation.lastCause, CompletedAt: completedAt,
	}
	session.cleanupSubmitOperationLocked(operation.providerMessageID)
}

func (session *IMSSession) expireSubmitOperationsLocked(now time.Time) {
	for providerMessageID, operation := range session.submitOperations {
		if operation == nil || operation.deadline.IsZero() || operation.deadline.After(now) {
			continue
		}
		// A timeout is deliberately not promoted to "failed": the SIP layer
		// may already have caused a carrier-side effect. Emit a terminal
		// unconfirmed event so the application stops displaying "awaiting
		// report", while preserving the no-automatic-retry invariant.
		errorCode := IMSSMSSubmitErrorUnknown
		if operation.rejectedSegments > 0 {
			errorCode = IMSSMSSubmitErrorPartial
		}
		session.completedSMSReports[providerMessageID] = IMSSMSSubmitReport{
			MessageID: operation.messageID, ProviderMessageID: operation.providerMessageID,
			State: IMSSMSSubmitUnconfirmed, ErrorCode: errorCode, Cause: operation.lastCause, CompletedAt: now,
		}
		session.smsProtocolCounters.ReportTimeouts++
		session.cleanupSubmitOperationLocked(providerMessageID)
	}
}

func (session *IMSSession) ListSMSSubmitReports() []IMSSMSSubmitReport {
	if session == nil {
		return nil
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	session.ensureSMSSubmitStateLocked()
	session.expireSubmitOperationsLocked(time.Now().UTC())
	reports := make([]IMSSMSSubmitReport, 0, len(session.completedSMSReports))
	for _, report := range session.completedSMSReports {
		reports = append(reports, report)
	}
	sortIMSSMSSubmitReports(reports)
	return reports
}

func (session *IMSSession) AcknowledgeSMSSubmitReport(providerMessageID string) error {
	if session == nil || !validOpaqueSMSID(providerMessageID) {
		return ErrIMSSMSMessageNotFound
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	session.ensureSMSSubmitStateLocked()
	if _, acknowledged := session.acknowledgedReports[providerMessageID]; acknowledged {
		return nil
	}
	if _, found := session.completedSMSReports[providerMessageID]; !found {
		return ErrIMSSMSMessageNotFound
	}
	delete(session.completedSMSReports, providerMessageID)
	if len(session.acknowledgedReports) >= maxPendingIMSSubmissions {
		clear(session.acknowledgedReports)
	}
	session.acknowledgedReports[providerMessageID] = struct{}{}
	return nil
}

func (session *IMSSession) SMSProtocolSnapshot() IMSSMSProtocolSnapshot {
	if session == nil {
		return IMSSMSProtocolSnapshot{}
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	return IMSSMSProtocolSnapshot(session.smsProtocolCounters)
}

func (session *IMSSession) exchangeProtectedRequestLocked(ctx context.Context, request []byte, callID string,
	sequence uint64, method string, budget time.Duration) ([]byte, error) {
	target := &net.UDPAddr{IP: net.IP(session.pcscf.AsSlice()), Port: int(session.challenge.SecurityServer.ProtectedServerPort)}
	deadline := time.Now().Add(budget)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	backoff, nextSend := 500*time.Millisecond, time.Now()
	for time.Now().Before(deadline) {
		if !time.Now().Before(nextSend) {
			if _, err := session.client.WriteToUDP(request, target); err != nil {
				return nil, errors.New("send protected IMS request")
			}
			nextSend = time.Now().Add(backoff)
			if backoff < 4*time.Second {
				backoff *= 2
			}
		}
		packet, received, err := session.readProtectedPacketLocked(ctx, minTime(deadline, nextSend))
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				break
			}
			return nil, err
		}
		if !received || packet.Status == 0 || !matchingSIPResponse(packet, callID, sequence, method) || packet.Status < 200 {
			continue
		}
		return marshalParsedSIPPacket(packet), nil
	}
	return nil, errProtectedIMSNoResponse
}

// readProtectedPacketLocked reads at most one packet from either protected
// flow. Incoming MESSAGE requests are answered and consumed; responses are
// returned to the transaction caller.
func (session *IMSSession) readProtectedPacketLocked(ctx context.Context, deadline time.Time) (sipPacket, bool, error) {
	buffer := make([]byte, 64<<10)
	defer zeroBytes(buffer)
	for _, connection := range []*net.UDPConn{session.client, session.server} {
		if err := ctx.Err(); err != nil {
			return sipPacket{}, false, err
		}
		readUntil := minTime(deadline, time.Now().Add(60*time.Millisecond))
		if contextDeadline, ok := ctx.Deadline(); ok {
			readUntil = minTime(readUntil, contextDeadline)
		}
		_ = connection.SetReadDeadline(readUntil)
		count, sender, err := connection.ReadFromUDP(buffer)
		if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
			continue
		}
		if err != nil {
			return sipPacket{}, false, errors.New("read protected IMS packet")
		}
		validPort := sender.Port == int(session.challenge.SecurityServer.ProtectedServerPort) ||
			sender.Port == int(session.challenge.SecurityServer.ProtectedClientPort)
		if !sender.IP.Equal(net.IP(session.pcscf.AsSlice())) || !validPort || count == 0 {
			continue
		}
		parsed, parseErr := parseSIPPacket(buffer[:count])
		if parseErr != nil {
			session.smsProtocolCounters.SIPParseFailures++
			continue
		}
		if parsed.Method != "" {
			if err := session.handleIncomingRequestLocked(connection, sender, buffer[:count], parsed); err != nil {
				return sipPacket{}, true, err
			}
			return sipPacket{}, true, nil
		}
		return parsed, true, nil
	}
	if time.Now().After(deadline) {
		if err := ctx.Err(); err != nil {
			return sipPacket{}, false, err
		}
		return sipPacket{}, false, nil
	}
	return sipPacket{}, false, nil
}

func (session *IMSSession) handleIncomingRequestLocked(connection *net.UDPConn, sender *net.UDPAddr, raw []byte, parsed sipPacket) error {
	session.smsProtocolCounters.SIPRequests++
	request, err := parseSMSIPRequest(raw)
	var rpMessage RPMessage
	var submitReportInReplyTo string
	if err != nil {
		session.smsProtocolCounters.SIPParseFailures++
	} else {
		rpMessage, err = ParseNetworkRPMessage(request.Body)
		if err != nil {
			session.smsProtocolCounters.RPParseFailures++
		}
	}
	status := 200
	if err != nil {
		status = 400
	} else if rpMessage.Type == rpACKNetworkToMS || rpMessage.Type == rpErrorNetworkToMS {
		segment, found := session.submitSegments[rpMessage.Reference]
		var matches bool
		submitReportInReplyTo, matches = submitReportCorrelation(request, segment.callID)
		if !found || !matches {
			status = 488
			session.smsProtocolCounters.CorrelationFailures++
		}
	}
	toTag, randomErr := randomHexToken(8)
	if randomErr != nil {
		return randomErr
	}
	response, responseErr := buildSIPResponse(parsed, status, toTag)
	if responseErr != nil {
		return nil
	}
	if _, writeErr := connection.WriteToUDP(response, sender); writeErr != nil {
		zeroBytes(response)
		return errors.New("send protected IMS response")
	}
	zeroBytes(response)
	if err != nil || status != 200 {
		return nil
	}
	switch rpMessage.Type {
	case rpACKNetworkToMS, rpErrorNetworkToMS:
		if submitReportInReplyTo == "" {
			session.smsInReplyToMode = imsSMSInReplyToUnsupported
		} else {
			session.smsInReplyToMode = imsSMSInReplyToSupported
		}
		session.recordSubmitReportLocked(rpMessage)
	case rpDataNetworkToMS:
		session.smsProtocolCounters.RPDataDeliveries++
		session.queueInboundSMSLocked(request, rpMessage)
	}
	return nil
}

func matchingSubmitReport(request sipPacket, expectedCallID string) bool {
	_, matches := submitReportCorrelation(request, expectedCallID)
	return matches
}

func submitReportCorrelation(request sipPacket, expectedCallID string) (string, bool) {
	if expectedCallID == "" {
		return "", false
	}
	values, present := request.Headers["in-reply-to"]
	if !present {
		// TS 24.341 permits an IP-SM-GW that does not support In-Reply-To.
		// RP references stay reserved for the lifetime of each pending user
		// operation, so the reference remains an unambiguous correlation key.
		return "", true
	}
	inReplyTo := singleSIPCallID(values)
	return inReplyTo, inReplyTo == expectedCallID
}

func (session *IMSSession) queueInboundSMSLocked(request sipPacket, rpMessage RPMessage) {
	gatewayURI := firstSIPAssertedIdentityURI(request.Headers["p-asserted-identity"])
	if gatewayURI == "" {
		return
	}
	pdu := append([]byte{0x00}, rpMessage.UserData...)
	delivered, err := smscodec.DecodeDeliverPDU(pdu)
	zeroBytes(pdu)
	if err != nil {
		return
	}
	body := ""
	if delivered.Segment.Total == 1 {
		body, err = smscodec.Decode([]smscodec.Segment{delivered.Segment})
		if err != nil || body == "" {
			return
		}
	}
	callID := strings.TrimSpace(request.Headers["call-id"][0])
	digest := sha256.New()
	digest.Write([]byte(rpMessage.OriginatorAddress))
	// The RP reference identifies one network transaction and can change when
	// the IP-SM-GW retransmits the same SMS-DELIVER. The TPDU includes the
	// originating address and service-centre timestamp, so it is the stable
	// delivery identity and keeps retransmissions on one durable message.
	digest.Write([]byte{0})
	digest.Write(rpMessage.UserData)
	messageID := "imsin_" + base64.RawURLEncoding.EncodeToString(digest.Sum(nil))
	if pending, exists := session.pendingSMS[messageID]; exists {
		pending.gatewayURI = gatewayURI
		pending.inReplyTo = callID
		pending.reference = rpMessage.Reference
		session.pendingSMS[messageID] = pending
		return
	}
	if len(session.pendingSMS) >= 256 {
		return
	}
	for operationID, acknowledgedMessageID := range session.acknowledgedSMS {
		if acknowledgedMessageID == messageID {
			delete(session.acknowledgedSMS, operationID)
		}
	}
	receivedAt := delivered.ReceivedAt.UTC()
	if receivedAt.IsZero() {
		receivedAt = time.Now().UTC()
	}
	session.pendingSMS[messageID] = pendingIMSSMS{
		message: IMSSMSMessage{MessageID: messageID, Sender: delivered.Sender, Body: body, ReceivedAt: receivedAt,
			Segment: smscodec.Segment{
				Encoding: delivered.Segment.Encoding, Reference: delivered.Segment.Reference,
				Part: delivered.Segment.Part, Total: delivered.Segment.Total, UnitCount: delivered.Segment.UnitCount,
				UserData: append([]byte(nil), delivered.Segment.UserData...),
			}},
		reference: rpMessage.Reference, gatewayURI: gatewayURI, inReplyTo: callID,
	}
}

func singleSIPCallID(values []string) string {
	if len(values) != 1 {
		return ""
	}
	value := strings.TrimSpace(values[0])
	if !validSIPCallID(value) {
		return ""
	}
	return value
}

func marshalParsedSIPPacket(packet sipPacket) []byte {
	// Transaction responses are parsed only to correlate them. Reconstruct a
	// minimal valid response so existing response validators can consume it
	// without retaining a second raw packet buffer.
	var builder strings.Builder
	reason := "Response"
	if packet.Status == 200 {
		reason = "OK"
	} else if packet.Status == 202 {
		reason = "Accepted"
	}
	builder.WriteString("SIP/2.0 " + strconv.Itoa(packet.Status) + " " + reason + "\r\n")
	for name, values := range packet.Headers {
		for _, value := range values {
			builder.WriteString(name + ": " + value + "\r\n")
		}
	}
	builder.WriteString("\r\n")
	return append([]byte(builder.String()), packet.Body...)
}

func sortIMSSMSReferences(values []IMSSMSReference) {
	for index := 1; index < len(values); index++ {
		for current := index; current > 0; current-- {
			left, right := values[current-1], values[current]
			if left.ReceivedAt.Before(right.ReceivedAt) || left.ReceivedAt.Equal(right.ReceivedAt) && left.MessageID <= right.MessageID {
				break
			}
			values[current-1], values[current] = right, left
		}
	}
}

func sortIMSSMSSubmitReports(values []IMSSMSSubmitReport) {
	for index := 1; index < len(values); index++ {
		for current := index; current > 0; current-- {
			left, right := values[current-1], values[current]
			if left.CompletedAt.Before(right.CompletedAt) ||
				left.CompletedAt.Equal(right.CompletedAt) && left.ProviderMessageID <= right.ProviderMessageID {
				break
			}
			values[current-1], values[current] = right, left
		}
	}
}

func validOpaqueSMSID(value string) bool {
	if len(value) < 16 || len(value) > 128 {
		return false
	}
	for _, current := range value {
		if current >= 'a' && current <= 'z' || current >= 'A' && current <= 'Z' || current >= '0' && current <= '9' || current == '_' || current == '-' {
			continue
		}
		return false
	}
	return true
}

func minTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}
