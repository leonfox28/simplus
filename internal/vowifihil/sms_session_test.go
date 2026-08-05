package vowifihil

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestQueueInboundSMSDecodesAndDeduplicatesSinglePartDelivery(t *testing.T) {
	tpdu, err := hex.DecodeString("040D91685120012194F600F10180817144302304F4F29C0E")
	if err != nil {
		t.Fatal(err)
	}
	session := &IMSSession{pendingSMS: make(map[string]pendingIMSSMS)}
	request := sipPacket{Headers: map[string][]string{
		"call-id":             {"2123456789abcdef@pcscf"},
		"p-asserted-identity": {"<sip:ipsmgw.example.invalid>"},
		"to":                  {"<tel:+447700900123>;tag=network"},
	}}
	rpMessage := RPMessage{Type: rpDataNetworkToMS, Reference: 9, UserData: tpdu}
	session.queueInboundSMSLocked(request, rpMessage)
	retransmission := request
	retransmission.Headers = map[string][]string{
		"call-id":             {"retransmitted0123456789@pcscf"},
		"p-asserted-identity": {"<sip:ipsmgw.example.invalid>"},
		"to":                  {"<tel:+447700900123>;tag=network"},
	}
	retransmittedRPMessage := rpMessage
	retransmittedRPMessage.Reference = 10
	session.queueInboundSMSLocked(retransmission, retransmittedRPMessage)
	if len(session.pendingSMS) != 1 {
		t.Fatalf("pending messages = %#v", session.pendingSMS)
	}
	var messageID string
	for currentMessageID, pending := range session.pendingSMS {
		messageID = currentMessageID
		if pending.message.Sender != "+8615021012496" || pending.message.Body != "test" ||
			pending.reference != 10 || pending.gatewayURI != "sip:ipsmgw.example.invalid" ||
			pending.inReplyTo != "retransmitted0123456789@pcscf" ||
			!validOpaqueSMSID(pending.message.MessageID) {
			t.Fatalf("pending = %#v", pending)
		}
	}
	delete(session.pendingSMS, messageID)
	session.acknowledgedSMS = map[string]string{"acknowledged_012345": messageID}
	session.queueInboundSMSLocked(retransmission, retransmittedRPMessage)
	if len(session.pendingSMS) != 1 || len(session.acknowledgedSMS) != 0 {
		t.Fatalf("network redelivery did not reopen acknowledgement: pending=%#v acknowledged=%#v", session.pendingSMS, session.acknowledgedSMS)
	}
}

func TestQueueInboundSMSRejectsMissingGatewayIdentity(t *testing.T) {
	tpdu, err := hex.DecodeString("040D91685120012194F600F10180817144302304F4F29C0E")
	if err != nil {
		t.Fatal(err)
	}
	requests := []sipPacket{
		{Headers: map[string][]string{
			"call-id": {"2123456789abcdef@pcscf"}, "to": {"<tel:+447700900123>"},
		}},
	}
	for _, request := range requests {
		session := &IMSSession{pendingSMS: make(map[string]pendingIMSSMS)}
		session.queueInboundSMSLocked(request, RPMessage{Type: rpDataNetworkToMS, Reference: 1, UserData: tpdu})
		if len(session.pendingSMS) != 0 {
			t.Fatalf("unsafe inbound message was queued: %#v", session.pendingSMS)
		}
	}
}

func TestSubmitReportRequiresMatchingInReplyTo(t *testing.T) {
	request := sipPacket{Headers: map[string][]string{"in-reply-to": {"sent0123456789abcdef@host"}}}
	if !matchingSubmitReport(request, "sent0123456789abcdef@host") || matchingSubmitReport(request, "other0123456789abcdef@host") {
		t.Fatal("submit report correlation did not enforce the original Call-ID")
	}
	if !matchingSubmitReport(sipPacket{Headers: map[string][]string{}}, "sent0123456789abcdef@host") {
		t.Fatal("submit report without In-Reply-To was not accepted by RP reference")
	}
	for _, invalid := range [][]string{nil, {"one", "two"}, {"bad value"}} {
		if singleSIPCallID(invalid) != "" || matchingSubmitReport(sipPacket{Headers: map[string][]string{"in-reply-to": invalid}}, "sent0123456789abcdef@host") {
			t.Fatalf("accepted invalid In-Reply-To %#v", invalid)
		}
	}
}

func TestSubmitSMSReturnsAcceptedWithoutWaitingForSubmitReports(t *testing.T) {
	gateway, session := newSMSSubmitTestSession(t, 2*time.Second)
	defer gateway.Close()
	defer session.Close()

	received := make(chan RPMessage, 2)
	responderError := make(chan error, 1)
	go func() {
		for range 2 {
			buffer := make([]byte, 4096)
			count, sender, err := gateway.ReadFromUDP(buffer)
			if err != nil {
				responderError <- err
				return
			}
			request, err := parseSMSIPRequest(buffer[:count])
			if err != nil {
				responderError <- err
				return
			}
			rpMessage, err := parseMSRPDataForTest(request.Body)
			if err != nil {
				responderError <- err
				return
			}
			received <- rpMessage
			response := []byte(fmt.Sprintf("SIP/2.0 202 Accepted\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
				request.Headers["call-id"][0], request.Headers["cseq"][0]))
			if _, err := gateway.WriteToUDP(response, sender); err != nil {
				responderError <- err
				return
			}
		}
		responderError <- nil
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	started := time.Now()
	submission, err := session.SubmitSMS(ctx, "msg_0123456789abcdef", [][]byte{{0x01, 0x00, 0x01}, {0x01, 0x01, 0x01}})
	if err != nil || submission.State != IMSSMSSubmitAccepted || !validOpaqueSMSID(submission.ProviderMessageID) {
		t.Fatalf("SubmitSMS() = %#v, %v", submission, err)
	}
	if elapsed := time.Since(started); elapsed >= 500*time.Millisecond {
		t.Fatalf("SubmitSMS waited for RP reports: %v", elapsed)
	}
	if err := <-responderError; err != nil {
		t.Fatal(err)
	}
	first, second := <-received, <-received
	if first.Type != rpDataMSToNetwork || second.Type != rpDataMSToNetwork || first.Reference == second.Reference ||
		!bytes.Equal(first.UserData, []byte{0x01, 0x00, 0x01}) || !bytes.Equal(second.UserData, []byte{0x01, 0x01, 0x01}) {
		t.Fatalf("submitted RP messages = %#v, %#v", first, second)
	}
	session.mu.Lock()
	for _, operation := range session.submitOperations {
		operation.deadline = time.Now().Add(-time.Second)
	}
	session.mu.Unlock()
	if reports := session.ListSMSSubmitReports(); len(reports) != 1 || reports[0].State != IMSSMSSubmitUnconfirmed ||
		reports[0].ErrorCode != IMSSMSSubmitErrorUnknown || len(session.submitOperations) != 0 || len(session.submitSegments) != 0 {
		t.Fatalf("expired report state: reports=%#v operations=%d segments=%d", reports, len(session.submitOperations), len(session.submitSegments))
	}
	if snapshot := session.SMSProtocolSnapshot(); snapshot.ReportTimeouts != 1 {
		t.Fatalf("timeout snapshot=%#v", snapshot)
	}
}

func TestSubmitSMSMarksSentOnlyAfterCorrelatedRPACK(t *testing.T) {
	gateway, session := newSMSSubmitTestSession(t, time.Second)
	defer gateway.Close()
	defer session.Close()

	responderError := make(chan error, 1)
	go func() {
		buffer := make([]byte, 4096)
		count, sender, err := gateway.ReadFromUDP(buffer)
		if err != nil {
			responderError <- err
			return
		}
		request, err := parseSMSIPRequest(buffer[:count])
		if err != nil {
			responderError <- err
			return
		}
		rpMessage, err := parseMSRPDataForTest(request.Body)
		if err != nil {
			responderError <- err
			return
		}
		response := []byte(fmt.Sprintf("SIP/2.0 202 Accepted\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
			request.Headers["call-id"][0], request.Headers["cseq"][0]))
		if _, err := gateway.WriteToUDP(response, sender); err != nil {
			responderError <- err
			return
		}
		reportBody := []byte{rpACKNetworkToMS, rpMessage.Reference}
		report := []byte(fmt.Sprintf("MESSAGE sip:10.255.0.42:42002 SIP/2.0\r\n"+
			"Via: SIP/2.0/UDP pcscf.example.invalid;branch=z9hG4bKreport\r\n"+
			"From: <sip:ipsmgw.example.invalid>;tag=remote\r\n"+
			"To: <sip:user@example.invalid>\r\n"+
			"Call-ID: submitreport0123456789@pcscf\r\n"+
			"CSeq: 8 MESSAGE\r\n"+
			"P-Asserted-Identity: <sip:ipsmgw.example.invalid>\r\n"+
			"In-Reply-To: %s\r\n"+
			"Content-Type: application/vnd.3gpp.sms\r\n"+
			"Content-Length: %d\r\n\r\n", request.Headers["call-id"][0], len(reportBody)))
		report = append(report, reportBody...)
		if _, err := gateway.WriteToUDP(report, sender); err != nil {
			responderError <- err
			return
		}
		responderError <- nil
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	submission, err := session.SubmitSMS(ctx, "msg_0123456789abcdef", [][]byte{{0x01, 0x00, 0x01}})
	if err != nil || !validOpaqueSMSID(submission.ProviderMessageID) ||
		(submission.State != IMSSMSSubmitAccepted && submission.State != IMSSMSSubmitSent) {
		t.Fatalf("submission = %#v, error = %v", submission, err)
	}
	if err := <-responderError; err != nil {
		t.Fatal(err)
	}
	pollCtx, pollCancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer pollCancel()
	if err := session.PollSMS(pollCtx); err != nil {
		t.Fatal(err)
	}
	reports := session.ListSMSSubmitReports()
	if len(reports) != 1 || reports[0].ProviderMessageID != submission.ProviderMessageID ||
		reports[0].MessageID != "msg_0123456789abcdef" || reports[0].State != IMSSMSSubmitSent ||
		reports[0].ErrorCode != "" || reports[0].Cause != 0 {
		t.Fatalf("submit reports = %#v", reports)
	}
	if snapshot := session.SMSProtocolSnapshot(); snapshot.RPACKs != 1 || snapshot.RPErrors != 0 || snapshot.CorrelationFailures != 0 {
		t.Fatalf("protocol snapshot = %#v", snapshot)
	}
	if err := session.AcknowledgeSMSSubmitReport(submission.ProviderMessageID); err != nil {
		t.Fatal(err)
	}
	if err := session.AcknowledgeSMSSubmitReport(submission.ProviderMessageID); err != nil {
		t.Fatalf("replayed submit-report acknowledgement: %v", err)
	}
	if reports := session.ListSMSSubmitReports(); len(reports) != 0 {
		t.Fatalf("acknowledged reports = %#v", reports)
	}
}

func TestSubmitReportTransactionKeepsReferencesReservedAndClassifiesRPErrors(t *testing.T) {
	const providerMessageID = "ims_0123456789abcdef01234567"
	session := &IMSSession{
		submitSegments: map[byte]pendingIMSSubmitSegment{
			7: {providerMessageID: providerMessageID, callID: "first0123456789@host"},
			8: {providerMessageID: providerMessageID, callID: "second01234567@host"},
		},
		submitOperations: map[string]*pendingIMSSubmitOperation{
			providerMessageID: {
				messageID: "msg_0123456789abcdef", providerMessageID: providerMessageID,
				totalSegments: 2, acceptedSegments: 2, submissionComplete: true, deadline: time.Now().Add(time.Minute),
			},
		},
		completedSMSReports: make(map[string]IMSSMSSubmitReport), acknowledgedReports: make(map[string]struct{}),
	}
	session.recordSubmitReportLocked(RPMessage{Type: rpACKNetworkToMS, Reference: 7})
	session.recordSubmitReportLocked(RPMessage{Type: rpACKNetworkToMS, Reference: 7}) // network retransmission
	if _, reserved := session.submitSegments[7]; !reserved || session.submitOperations[providerMessageID].reportedSegments != 1 {
		t.Fatalf("acknowledged reference was not reserved: segments=%#v operation=%#v",
			session.submitSegments, session.submitOperations[providerMessageID])
	}
	session.recordSubmitReportLocked(RPMessage{Type: rpErrorNetworkToMS, Reference: 8, Cause: 41})
	report := session.completedSMSReports[providerMessageID]
	if report.State != IMSSMSSubmitUnconfirmed || report.ErrorCode != IMSSMSSubmitErrorPartial || report.Cause != 41 ||
		len(session.submitSegments) != 0 || len(session.submitOperations) != 0 {
		t.Fatalf("multipart error report=%#v segments=%#v operations=%#v", report, session.submitSegments, session.submitOperations)
	}
	session.rpReference = 7
	if reference, available := session.nextRPReferenceLocked(); !available || reference != 9 {
		t.Fatalf("retired RP references were reused: reference=%d available=%t retired=%#v",
			reference, available, session.retiredRPReferences)
	}
	if snapshot := session.SMSProtocolSnapshot(); snapshot.RPACKs != 2 || snapshot.RPErrors != 1 {
		t.Fatalf("protocol snapshot=%#v", snapshot)
	}
}

// ParseMSRPDataForTest checks the fixed MS-to-network RP-DATA shape emitted
// by BuildRPDataSubmit without adding production support for that direction.
func parseMSRPDataForTest(packet []byte) (RPMessage, error) {
	if len(packet) < 6 || packet[0] != rpDataMSToNetwork || packet[2] != 0 {
		return RPMessage{}, errors.New("invalid test MS RP-DATA")
	}
	destinationLength := int(packet[3])
	position := 4 + destinationLength
	if destinationLength < 2 || position >= len(packet) {
		return RPMessage{}, errors.New("invalid test MS RP-DATA destination")
	}
	userDataLength := int(packet[position])
	position++
	if userDataLength < 1 || position+userDataLength != len(packet) {
		return RPMessage{}, errors.New("invalid test MS RP-DATA user data")
	}
	return RPMessage{Type: rpDataMSToNetwork, Reference: packet[1], UserData: append([]byte(nil), packet[position:]...)}, nil
}

func newSMSSubmitTestSession(t *testing.T, reportWait time.Duration) (*net.UDPConn, *IMSSession) {
	t.Helper()
	gateway, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	client, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		gateway.Close()
		t.Fatal(err)
	}
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		client.Close()
		gateway.Close()
		t.Fatal(err)
	}
	gatewayPort := uint16(gateway.LocalAddr().(*net.UDPAddr).Port)
	session := &IMSSession{
		input: IMSInitialRegisterInput{
			Source: netip.MustParseAddr("10.255.0.42"), ProtectedServerPort: uint16(server.LocalAddr().(*net.UDPAddr).Port),
			PublicIdentity: "sip:user@example.invalid", WLANNodeID: "020000000001", SMSCapable: true,
		},
		pcscf: netip.MustParseAddr("127.0.0.1"), client: client, server: server,
		challenge: IMSRegistrationChallenge{SecurityServer: IMSIPSecParameters{
			Raw: "ipsec-3gpp;alg=hmac-sha-1-96", ProtectedServerPort: gatewayPort, ProtectedClientPort: gatewayPort,
		}},
		serviceCentreURI: "tel:+447700900123", serviceCentreAddress: "+447700900123",
		smsSequence: 1, smsSubmitReportWait: reportWait,
		pendingSMS: make(map[string]pendingIMSSMS), acknowledgedSMS: make(map[string]string),
		submitSegments: make(map[byte]pendingIMSSubmitSegment), submitOperations: make(map[string]*pendingIMSSubmitOperation),
		completedSMSReports: make(map[string]IMSSMSSubmitReport), acknowledgedReports: make(map[string]struct{}),
	}
	return gateway, session
}

func TestRegistrationServiceRoutesAreBounded(t *testing.T) {
	packet := []byte("SIP/2.0 200 OK\r\nCall-ID: 2123456789abcdef@host\r\nCSeq: 2 REGISTER\r\n" +
		"Service-Route: <sip:first.example.invalid;lr>\r\nService-Route: <sip:second.example.invalid;lr>\r\nContent-Length: 0\r\n\r\n")
	routes := registrationServiceRoutes(packet)
	if len(routes) != 2 || routes[0] != "<sip:first.example.invalid;lr>" || routes[1] != "<sip:second.example.invalid;lr>" {
		t.Fatalf("routes = %#v", routes)
	}
}

func TestRegistrationAuthorizedIdentityUsesFirstAssociatedURI(t *testing.T) {
	const fallback = "sip:imsi-user@ims.example.invalid"
	packet := []byte("SIP/2.0 200 OK\r\n" +
		"P-Associated-URI: \"User, One\" <sip:+447700900123@ims.example.invalid>, <tel:+447700900123>\r\n" +
		"Content-Length: 0\r\n\r\n")
	if got := registrationAuthorizedIdentity(packet, fallback); got != "sip:+447700900123@ims.example.invalid" {
		t.Fatalf("authorized identity = %q", got)
	}
	packet = []byte("SIP/2.0 200 OK\r\nP-Associated-URI: <tel:+447700900123>\r\nContent-Length: 0\r\n\r\n")
	if got := registrationAuthorizedIdentity(packet, fallback); got != "tel:+447700900123" {
		t.Fatalf("TEL authorized identity = %q", got)
	}
	packet = []byte("SIP/2.0 200 OK\r\nContent-Length: 0\r\n\r\n")
	if got := registrationAuthorizedIdentity(packet, fallback); got != fallback {
		t.Fatalf("fallback identity = %q", got)
	}
}

func TestSMSRequestUsesNetworkAuthorizedIdentity(t *testing.T) {
	session := &IMSSession{
		input: IMSInitialRegisterInput{
			Source: netip.MustParseAddr("10.255.0.42"), ProtectedServerPort: 42002,
			PublicIdentity: "sip:imsi-user@ims.example.invalid", WLANNodeID: "020000000001",
		},
		authorizedIdentity: "sip:+447700900123@ims.example.invalid", smsSequence: 1,
		challenge: IMSRegistrationChallenge{SecurityServer: IMSIPSecParameters{
			Raw: "ipsec-3gpp;alg=hmac-sha-1-96;ealg=aes-cbc;prot=esp;mod=trans;spi-c=1;spi-s=2;port-c=3;port-s=4",
		}},
	}
	_, _, packet, err := session.buildSMSRequestLocked("tel:+447700900123", "", []byte{0x03, 0x01})
	if err != nil {
		t.Fatal(err)
	}
	text := string(packet)
	for _, expected := range []string{
		"P-Preferred-Identity: <sip:+447700900123@ims.example.invalid>\r\n",
		"From: <sip:+447700900123@ims.example.invalid>;tag=",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("SMS request does not use authorized identity: %q", packet)
		}
	}
}

func TestAcknowledgeSMSFallsBackWithoutInReplyToAfterCorrelationRejection(t *testing.T) {
	gateway, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer gateway.Close()
	client, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	gatewayPort := uint16(gateway.LocalAddr().(*net.UDPAddr).Port)
	session := &IMSSession{
		input: IMSInitialRegisterInput{
			Source: netip.MustParseAddr("10.255.0.42"), ProtectedServerPort: uint16(server.LocalAddr().(*net.UDPAddr).Port),
			PublicIdentity: "sip:user@example.invalid", WLANNodeID: "020000000001",
		},
		pcscf: netip.MustParseAddr("127.0.0.1"), client: client, server: server,
		challenge: IMSRegistrationChallenge{SecurityServer: IMSIPSecParameters{
			Raw: "ipsec-3gpp;alg=hmac-sha-1-96", ProtectedServerPort: gatewayPort, ProtectedClientPort: gatewayPort,
		}},
		smsSequence:      1,
		smsInReplyToMode: imsSMSInReplyToUnknown,
		pendingSMS:       make(map[string]pendingIMSSMS),
		acknowledgedSMS:  make(map[string]string),
	}
	const messageID = "imsin_0123456789abcdef"
	session.pendingSMS[messageID] = pendingIMSSMS{
		reference: 0x29, gatewayURI: "sip:ipsmgw.example.invalid",
		inReplyTo: "incoming0123456789@pcscf",
	}
	for index := range maxPendingIMSSubmissions {
		session.acknowledgedSMS[fmt.Sprintf("old_ack_%016d", index)] = "old_message_0123456789"
	}

	type deliveryAttempt struct {
		body            []byte
		includesReplyTo bool
		fromURI         string
		preferredURI    string
	}
	attempts := make(chan deliveryAttempt, 2)
	responderError := make(chan error, 1)
	go func() {
		for _, responseLine := range []string{"488 Not Acceptable Here", "202 Accepted"} {
			buffer := make([]byte, 4096)
			count, sender, readErr := gateway.ReadFromUDP(buffer)
			if readErr != nil {
				responderError <- readErr
				return
			}
			request, parseErr := parseSIPPacket(buffer[:count])
			if parseErr != nil {
				responderError <- parseErr
				return
			}
			_, includesReplyTo := request.Headers["in-reply-to"]
			attempts <- deliveryAttempt{
				body: append([]byte(nil), request.Body...), includesReplyTo: includesReplyTo,
				fromURI: firstSIPURI(request.Headers["from"]), preferredURI: firstSIPURI(request.Headers["p-preferred-identity"]),
			}
			response := []byte(fmt.Sprintf("SIP/2.0 %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
				responseLine, request.Headers["call-id"][0], request.Headers["cseq"][0]))
			if _, writeErr := gateway.WriteToUDP(response, sender); writeErr != nil {
				responderError <- writeErr
				return
			}
		}
		responderError <- nil
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := session.AcknowledgeSMS(ctx, messageID, "acknowledge_0123456789"); err != nil {
		t.Fatal(err)
	}
	if err := <-responderError; err != nil {
		t.Fatal(err)
	}
	first, second := <-attempts, <-attempts
	for index, attempt := range []deliveryAttempt{first, second} {
		if !bytes.Equal(attempt.body, BuildRPDeliveryACK(0x29)) ||
			attempt.fromURI != session.input.PublicIdentity || attempt.preferredURI != session.input.PublicIdentity {
			t.Fatalf("delivery report attempt %d = %#v", index, attempt)
		}
	}
	if !first.includesReplyTo || second.includesReplyTo || session.smsInReplyToMode != imsSMSInReplyToUnsupported {
		t.Fatalf("fallback correlation first=%#v second=%#v mode=%d", first, second, session.smsInReplyToMode)
	}
	if len(session.pendingSMS) != 0 || len(session.acknowledgedSMS) != 1 ||
		session.acknowledgedSMS["acknowledge_0123456789"] != messageID {
		t.Fatalf("acknowledgement state was not committed: pending=%d acknowledged=%d", len(session.pendingSMS), len(session.acknowledgedSMS))
	}
}
