package messaging

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/application/inventory"
	"github.com/leonfox28/simplus/internal/domain/hardware"
	"github.com/leonfox28/simplus/internal/domain/sms"
	"github.com/leonfox28/simplus/internal/smscodec"
	sqlitestore "github.com/leonfox28/simplus/internal/storage/sqlite"
	"github.com/leonfox28/simplus/internal/vowifisupervisor"
)

type fakeVoWiFiSMSAPI struct {
	sendRequest       vowifisupervisor.SMSSendRequest
	messages          []vowifisupervisor.SMSMessage
	ackRequests       []vowifisupervisor.SMSAcknowledgeRequest
	submitReports     []vowifisupervisor.SMSSubmitReport
	reportAckRequests []vowifisupervisor.SMSSubmitReportAcknowledgeRequest
	sendResponse      vowifisupervisor.SMSSendResponse
}

func (fake *fakeVoWiFiSMSAPI) SendSMS(_ context.Context, request vowifisupervisor.SMSSendRequest) (vowifisupervisor.SMSSendResponse, error) {
	fake.sendRequest = request
	if fake.sendResponse.ProviderMessageID != "" {
		return fake.sendResponse, nil
	}
	return vowifisupervisor.SMSSendResponse{
		ProviderMessageID: "ims_provider_0123456789", State: vowifisupervisor.SMSSubmitSent,
	}, nil
}

func TestVoWiFiAcceptedSubmissionIsFinalizedByLaterRPACK(t *testing.T) {
	fake := &fakeVoWiFiSMSAPI{sendResponse: vowifisupervisor.SMSSendResponse{
		ProviderMessageID: "ims_provider_0123456789", State: vowifisupervisor.SMSSubmitAccepted,
	}}
	gateway, err := NewVoWiFiSMSGateway(fake)
	if err != nil {
		t.Fatal(err)
	}
	service, stores := newTestService(t, gateway)
	line := inventory.Line{
		ID: "line_AQEBAQEBAQEBAQEBAQEBAQ", PhysicalDeviceID: "usb-device",
		ModemFunctionID: "modem-function", State: inventory.LineReady,
		Capabilities: hardware.Capabilities{HostVoWiFiAuth: true},
	}
	service.lines = fixedLineSource{topology: inventory.Topology{Lines: []inventory.Line{line}}}
	if err := service.UseTransports(HostVoWiFiSMSTransport(messagingTransportAvailability(true), gateway, gateway)); err != nil {
		t.Fatal(err)
	}
	sent, err := service.Send(context.Background(), SendRequest{
		OperationID: "operation_vowifi_report_01", LineID: line.ID,
		Destination: "+447700900456", Body: "accepted over IMS",
	})
	if err != nil || sent.Message.Status != sms.StatusUnconfirmed ||
		sent.Message.ProviderMessageID != fake.sendResponse.ProviderMessageID ||
		sent.Message.ErrorCode != ErrorAcceptedAwaitingReport || fake.sendRequest.MessageID != sent.Message.ID {
		t.Fatalf("accepted result=%#v request=%#v error=%v", sent, fake.sendRequest, err)
	}
	fake.submitReports = []vowifisupervisor.SMSSubmitReport{{
		MessageID: sent.Message.ID, ProviderMessageID: fake.sendResponse.ProviderMessageID,
		State: vowifisupervisor.SMSSubmitSent, CompletedAt: time.Now().UTC(),
	}}
	synced, err := service.SyncInbound(context.Background())
	if err != nil || synced.OutboundSent != 1 || synced.OutboundReportsAcknowledged != 1 || len(fake.reportAckRequests) != 1 {
		t.Fatalf("sync=%#v acknowledgements=%#v error=%v", synced, fake.reportAckRequests, err)
	}
	messages, err := stores.ListSMS(context.Background(), 10)
	if err != nil || len(messages) != 1 || messages[0].Status != sms.StatusSent ||
		messages[0].ProviderMessageID != fake.sendResponse.ProviderMessageID || messages[0].ErrorCode != "" {
		t.Fatalf("messages=%#v error=%v", messages, err)
	}

	fake.sendResponse = vowifisupervisor.SMSSendResponse{
		ProviderMessageID: "ims_provider_abcdef012345", State: vowifisupervisor.SMSSubmitAccepted,
	}
	rejected, err := service.Send(context.Background(), SendRequest{
		OperationID: "operation_vowifi_report_02", LineID: line.ID,
		Destination: "+447700900456", Body: "rejected over IMS",
	})
	if err != nil || rejected.Message.Status != sms.StatusUnconfirmed {
		t.Fatalf("rejected pending result=%#v error=%v", rejected, err)
	}
	fake.submitReports = []vowifisupervisor.SMSSubmitReport{{
		MessageID: rejected.Message.ID, ProviderMessageID: fake.sendResponse.ProviderMessageID,
		State: vowifisupervisor.SMSSubmitFailed, ErrorCode: "IMS_SMS_REJECTED", CompletedAt: time.Now().UTC(),
	}}
	synced, err = service.SyncInbound(context.Background())
	if err != nil || synced.OutboundFailed != 1 || synced.OutboundReportsAcknowledged != 1 {
		t.Fatalf("failed sync=%#v error=%v", synced, err)
	}
	messages, err = stores.ListSMS(context.Background(), 10)
	var failedMessage sms.Message
	for _, message := range messages {
		if message.ID == rejected.Message.ID {
			failedMessage = message
		}
	}
	if err != nil || len(messages) != 2 || failedMessage.Status != sms.StatusFailed ||
		failedMessage.ProviderMessageID != fake.sendResponse.ProviderMessageID || failedMessage.ErrorCode != "IMS_SMS_REJECTED" {
		t.Fatalf("failed messages=%#v error=%v", messages, err)
	}
}
func (fake *fakeVoWiFiSMSAPI) ListSMS(context.Context, string) ([]vowifisupervisor.SMSMessageReference, error) {
	result := make([]vowifisupervisor.SMSMessageReference, 0, len(fake.messages))
	for _, message := range fake.messages {
		result = append(result, vowifisupervisor.SMSMessageReference{MessageID: message.MessageID, ReceivedAt: message.ReceivedAt})
	}
	return result, nil
}
func (fake *fakeVoWiFiSMSAPI) ReadSMS(_ context.Context, _ string, messageID string) (vowifisupervisor.SMSMessage, error) {
	for _, message := range fake.messages {
		if message.MessageID == messageID {
			return message, nil
		}
	}
	return vowifisupervisor.SMSMessage{}, vowifisupervisor.ErrSMSMessageNotFound
}
func (fake *fakeVoWiFiSMSAPI) AcknowledgeSMS(_ context.Context, request vowifisupervisor.SMSAcknowledgeRequest) error {
	fake.ackRequests = append(fake.ackRequests, request)
	for index, message := range fake.messages {
		if message.MessageID == request.MessageID {
			fake.messages = append(fake.messages[:index], fake.messages[index+1:]...)
			break
		}
	}
	return nil
}
func (fake *fakeVoWiFiSMSAPI) ListSMSSubmitReports(context.Context, string) (vowifisupervisor.SMSSubmitReportListResponse, error) {
	return vowifisupervisor.SMSSubmitReportListResponse{Reports: append([]vowifisupervisor.SMSSubmitReport(nil), fake.submitReports...)}, nil
}
func (fake *fakeVoWiFiSMSAPI) AcknowledgeSMSSubmitReport(_ context.Context,
	request vowifisupervisor.SMSSubmitReportAcknowledgeRequest) error {
	fake.reportAckRequests = append(fake.reportAckRequests, request)
	for index, report := range fake.submitReports {
		if report.ProviderMessageID == request.ProviderMessageID {
			fake.submitReports = append(fake.submitReports[:index], fake.submitReports[index+1:]...)
			break
		}
	}
	return nil
}

type fixedLineSource struct{ topology inventory.Topology }

func (source fixedLineSource) Topology(context.Context) (inventory.Topology, error) {
	return source.topology, nil
}

func TestVoWiFiGatewayAllowsHostLineWithoutCellularSMSCapability(t *testing.T) {
	segment := mustSMSPart(t, "received over IMS", 0)
	fake := &fakeVoWiFiSMSAPI{messages: []vowifisupervisor.SMSMessage{{
		MessageID: "imsin_0123456789abcdef", Sender: "+447700900123", Body: "received over IMS",
		Encoding: string(segment.Encoding), ConcatenationReference: int(segment.Reference),
		Part: segment.Part, Total: segment.Total, UnitCount: segment.UnitCount, UserData: segment.UserData,
		ReceivedAt: time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC),
	}}}
	gateway, err := NewVoWiFiSMSGateway(fake)
	if err != nil {
		t.Fatal(err)
	}
	service, stores := newTestService(t, gateway)
	line := inventory.Line{
		ID: "line_AQEBAQEBAQEBAQEBAQEBAQ", PhysicalDeviceID: "usb-device",
		ModemFunctionID: "modem-function", State: inventory.LineReady,
		Capabilities: hardware.Capabilities{SMS: false, HostVoWiFiAuth: true},
	}
	service.lines = fixedLineSource{topology: inventory.Topology{Lines: []inventory.Line{line}}}
	if err := service.UseTransports(HostVoWiFiSMSTransport(messagingTransportAvailability(true), gateway, gateway)); err != nil {
		t.Fatal(err)
	}
	result, err := service.Send(context.Background(), SendRequest{
		OperationID: "operation_vowifi_012345", LineID: line.ID, Destination: "+447700900456", Body: "sent over IMS",
	})
	if err != nil || result.Message.Status != sms.StatusSent || fake.sendRequest.LineID != line.ID {
		t.Fatalf("result=%#v request=%#v error=%v", result, fake.sendRequest, err)
	}
	synced, err := service.SyncInbound(context.Background())
	if err != nil || synced.Persisted != 1 || synced.Acknowledged != 1 || len(fake.ackRequests) != 1 || fake.ackRequests[0].LineID != line.ID {
		t.Fatalf("sync=%#v acks=%#v error=%v", synced, fake.ackRequests, err)
	}
	messages, err := stores.ListSMS(context.Background(), 10)
	if err != nil || len(messages) != 2 {
		t.Fatalf("messages=%#v error=%v", messages, err)
	}
}

func TestVoWiFiGatewayRequiresHostVoWiFiCapability(t *testing.T) {
	fake := &fakeVoWiFiSMSAPI{}
	gateway, err := NewVoWiFiSMSGateway(fake)
	if err != nil {
		t.Fatal(err)
	}
	service, _ := newTestService(t, gateway)
	line := inventory.Line{
		ID: "line_AQEBAQEBAQEBAQEBAQEBAQ", PhysicalDeviceID: "usb-device",
		ModemFunctionID: "modem-function", State: inventory.LineReady,
		Capabilities: hardware.Capabilities{SMS: true},
	}
	service.lines = fixedLineSource{topology: inventory.Topology{Lines: []inventory.Line{line}}}
	if err := service.UseTransports(HostVoWiFiSMSTransport(messagingTransportAvailability(true), gateway, gateway)); err != nil {
		t.Fatal(err)
	}
	_, err = service.Send(context.Background(), SendRequest{
		OperationID: "operation_vowifi_012345", LineID: line.ID, Destination: "+447700900456", Body: "must not route",
	})
	if !errors.Is(err, ErrLineUnsupported) || fake.sendRequest.OperationID != "" {
		t.Fatalf("error=%v request=%#v", err, fake.sendRequest)
	}
}

func TestVoWiFiMultipartInboundSurvivesControlPlaneRestart(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "db")
	stores, err := sqlitestore.OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	line := inventory.Line{
		ID: "line_AQEBAQEBAQEBAQEBAQEBAQ", PhysicalDeviceID: "usb-device",
		ModemFunctionID: "modem-function", State: inventory.LineReady,
		Capabilities: hardware.Capabilities{HostVoWiFiAuth: true},
	}
	body := strings.Repeat("a", 161)
	segments, err := smscodec.Encode(body)
	if err != nil || len(segments) != 2 {
		t.Fatalf("segments=%d error=%v", len(segments), err)
	}
	// Deliberately cross an hour boundary: fragment grouping must use the
	// reassembly window, not a lossy timestamp bucket.
	receivedAt := time.Now().UTC().Truncate(time.Hour).Add(59 * time.Minute)
	first := &fakeVoWiFiSMSAPI{messages: []vowifisupervisor.SMSMessage{
		voWiFiSMSMessage("imsin_first_part_012345", "+447700900123", receivedAt, segments[0]),
	}}
	firstGateway, err := NewVoWiFiSMSGateway(first)
	if err != nil {
		t.Fatal(err)
	}
	firstService, err := NewService(ctx, stores, fixedLineSource{topology: inventory.Topology{Lines: []inventory.Line{line}}},
		HostVoWiFiSMSTransport(messagingTransportAvailability(true), firstGateway, firstGateway))
	if err != nil {
		t.Fatal(err)
	}
	result, err := firstService.SyncInbound(ctx)
	if err != nil || result.Persisted != 0 || result.Acknowledged != 1 || len(first.ackRequests) != 1 {
		t.Fatalf("first sync=%#v acks=%#v error=%v", result, first.ackRequests, err)
	}
	if messages, listErr := stores.ListSMS(ctx, 10); listErr != nil || len(messages) != 0 {
		t.Fatalf("incomplete visible messages=%#v error=%v", messages, listErr)
	}
	if err := stores.Close(); err != nil {
		t.Fatal(err)
	}

	stores, err = sqlitestore.OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()
	const secondMessageID = "imsin_second_part_01234"
	second := &fakeVoWiFiSMSAPI{messages: []vowifisupervisor.SMSMessage{
		voWiFiSMSMessage(secondMessageID, "+447700900123", receivedAt.Add(2*time.Minute), segments[1]),
	}}
	secondGateway, err := NewVoWiFiSMSGateway(second)
	if err != nil {
		t.Fatal(err)
	}
	secondService, err := NewService(ctx, stores, fixedLineSource{topology: inventory.Topology{Lines: []inventory.Line{line}}},
		HostVoWiFiSMSTransport(messagingTransportAvailability(true), secondGateway, secondGateway))
	if err != nil {
		t.Fatal(err)
	}
	result, err = secondService.SyncInbound(ctx)
	if err != nil || result.Persisted != 1 || result.Acknowledged != 1 || len(second.ackRequests) != 1 {
		t.Fatalf("second sync=%#v acks=%#v error=%v", result, second.ackRequests, err)
	}
	if len(result.receivedSMS) != 1 || result.receivedSMS[0].Sender != "+447700900123" || result.receivedSMS[0].Body != body {
		t.Fatalf("assembled received SMS notification = %#v", result.receivedSMS)
	}
	messages, err := stores.ListSMS(ctx, 10)
	if err != nil || len(messages) != 1 || messages[0].Body != body || messages[0].ProviderMessageID == secondMessageID {
		t.Fatalf("assembled messages=%#v error=%v", messages, err)
	}
}

func TestVoWiFiMultipartInboundRejectsAssembledBodyBeyondSMSLimit(t *testing.T) {
	ctx := context.Background()
	stores, err := sqlitestore.OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()
	line := inventory.Line{
		ID: "line_AQEBAQEBAQEBAQEBAQEBAQ", PhysicalDeviceID: "usb-device",
		ModemFunctionID: "modem-function", State: inventory.LineReady,
		Capabilities: hardware.Capabilities{HostVoWiFiAuth: true},
	}
	segments, err := smscodec.Encode(strings.Repeat("界", maximumSMSBodyRunes))
	if err != nil || len(segments) < 2 {
		t.Fatalf("segments=%d error=%v", len(segments), err)
	}
	last := &segments[len(segments)-1]
	last.UserData = append(last.UserData, 0x75, 0x4c)
	last.UnitCount++
	assembled, err := smscodec.Decode(segments)
	if err != nil || len([]rune(assembled)) != maximumSMSBodyRunes+1 {
		t.Fatalf("assembled runes=%d error=%v", len([]rune(assembled)), err)
	}
	receivedAt := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	messages := make([]vowifisupervisor.SMSMessage, 0, len(segments))
	for index, segment := range segments {
		messages = append(messages, voWiFiSMSMessage(
			fmt.Sprintf("imsin_oversize_%02d_012345", index), "+447700900123", receivedAt, segment,
		))
	}
	fake := &fakeVoWiFiSMSAPI{messages: messages}
	gateway, err := NewVoWiFiSMSGateway(fake)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(ctx, stores, fixedLineSource{topology: inventory.Topology{Lines: []inventory.Line{line}}},
		HostVoWiFiSMSTransport(messagingTransportAvailability(true), gateway, gateway))
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.SyncInbound(ctx)
	if !errors.Is(err, ErrInboundSync) || result.Persisted != 0 || len(result.receivedSMS) != 0 ||
		result.Acknowledged != len(segments)-1 || len(fake.ackRequests) != len(segments)-1 {
		t.Fatalf("sync=%#v acks=%d error=%v", result, len(fake.ackRequests), err)
	}
	persisted, err := stores.ListSMS(ctx, 10)
	if err != nil || len(persisted) != 0 {
		t.Fatalf("persisted messages=%#v error=%v", persisted, err)
	}
}

func mustSMSPart(t *testing.T, body string, part int) smscodec.Segment {
	t.Helper()
	segments, err := smscodec.Encode(body)
	if err != nil || part < 0 || part >= len(segments) {
		t.Fatalf("encode SMS part: count=%d part=%d error=%v", len(segments), part, err)
	}
	return segments[part]
}

func voWiFiSMSMessage(messageID, sender string, receivedAt time.Time, segment smscodec.Segment) vowifisupervisor.SMSMessage {
	body := ""
	if segment.Total == 1 {
		body, _ = smscodec.DecodeSegment(segment)
	}
	return vowifisupervisor.SMSMessage{
		MessageID: messageID, Sender: sender, Body: body, Encoding: string(segment.Encoding),
		ConcatenationReference: int(segment.Reference), Part: segment.Part, Total: segment.Total,
		UnitCount: segment.UnitCount, UserData: append([]byte(nil), segment.UserData...), ReceivedAt: receivedAt,
	}
}
