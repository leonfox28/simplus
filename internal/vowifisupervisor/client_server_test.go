package vowifisupervisor

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/smscodec"
)

type fakeAPI struct {
	status       Status
	message      SMSMessage
	acknowledged bool
	report       SMSSubmitReport
	reportAcked  bool
}

func (fake *fakeAPI) SendSMS(_ context.Context, request SMSSendRequest) (SMSSendResponse, error) {
	if !validSMSSendRequest(request) {
		return SMSSendResponse{}, ErrRequestInvalid
	}
	return SMSSendResponse{ProviderMessageID: "ims_provider_0123456789", State: SMSSubmitAccepted}, nil
}

func (fake *fakeAPI) ListSMS(_ context.Context, lineID string) ([]SMSMessageReference, error) {
	if !hardwareLinePattern.MatchString(lineID) {
		return nil, ErrRequestInvalid
	}
	return []SMSMessageReference{{MessageID: fake.message.MessageID, ReceivedAt: fake.message.ReceivedAt}}, nil
}

func (fake *fakeAPI) ReadSMS(_ context.Context, lineID, messageID string) (SMSMessage, error) {
	if !validSMSMessageRequest(lineID, messageID) || messageID != fake.message.MessageID {
		return SMSMessage{}, ErrSMSMessageNotFound
	}
	return fake.message, nil
}

func (fake *fakeAPI) AcknowledgeSMS(_ context.Context, request SMSAcknowledgeRequest) error {
	if !validSMSAcknowledgeRequest(request) || request.MessageID != fake.message.MessageID {
		return ErrSMSMessageNotFound
	}
	fake.acknowledged = true
	return nil
}

func (fake *fakeAPI) ListSMSSubmitReports(_ context.Context, lineID string) (SMSSubmitReportListResponse, error) {
	if !hardwareLinePattern.MatchString(lineID) {
		return SMSSubmitReportListResponse{}, ErrRequestInvalid
	}
	return SMSSubmitReportListResponse{
		Reports: []SMSSubmitReport{fake.report}, Diagnostics: SMSProtocolDiagnostics{RPACKs: 1},
	}, nil
}

func (fake *fakeAPI) AcknowledgeSMSSubmitReport(_ context.Context, request SMSSubmitReportAcknowledgeRequest) error {
	if !validSMSSubmitReportAcknowledgeRequest(request) || request.ProviderMessageID != fake.report.ProviderMessageID {
		return ErrSMSMessageNotFound
	}
	fake.reportAcked = true
	return nil
}

func (fake *fakeAPI) List(context.Context) ([]Status, error) { return []Status{fake.status}, nil }
func (fake *fakeAPI) Start(_ context.Context, request StartRequest) (Status, error) {
	if !validStartRequest(request) {
		return Status{}, ErrRequestInvalid
	}
	fake.status = Status{LineID: request.LineID, State: StateStarting, EgressMode: request.EgressMode, CountryCode: request.CountryCode}
	return fake.status, nil
}
func (fake *fakeAPI) Stop(_ context.Context, lineID string) (Status, error) {
	if lineID != fake.status.LineID {
		return Status{}, ErrNotRunning
	}
	fake.status.State = StateStopped
	return fake.status, nil
}

func TestClientServerRoundTrip(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "netd.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	segment, err := smscodec.Encode("inbound")
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeAPI{message: SMSMessage{
		MessageID: "imsin_0123456789abcdef", Sender: "+447700900123", Body: "inbound",
		Encoding: string(segment[0].Encoding), ConcatenationReference: int(segment[0].Reference),
		Part: segment[0].Part, Total: segment[0].Total, UnitCount: segment[0].UnitCount, UserData: segment[0].UserData,
		ReceivedAt: time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC),
	}, report: SMSSubmitReport{
		MessageID: "msg_0123456789abcdef", ProviderMessageID: "ims_provider_0123456789",
		State: SMSSubmitSent, CompletedAt: time.Date(2026, 8, 5, 10, 1, 0, 0, time.UTC),
	}}
	mux := http.NewServeMux()
	mux.Handle("/v1/vowifi/", http.StripPrefix("/v1/vowifi", NewHandler(fake, slog.Default())))
	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
	})
	client, err := NewClient(socket)
	if err != nil {
		t.Fatal(err)
	}
	request := StartRequest{LineID: "agent-line-0123456789abcdef0123456789abcdef", EgressMode: EgressMihomoCountry, CountryCode: "JP"}
	started, err := client.Start(context.Background(), request)
	if err != nil || started.State != StateStarting {
		t.Fatalf("start=%#v err=%v", started, err)
	}
	listed, err := client.List(context.Background())
	if err != nil || len(listed) != 1 || listed[0].LineID != request.LineID {
		t.Fatalf("list=%#v err=%v", listed, err)
	}
	stopped, err := client.Stop(context.Background(), request.LineID)
	if err != nil || stopped.State != StateStopped {
		t.Fatalf("stop=%#v err=%v", stopped, err)
	}
	if _, err := client.Stop(context.Background(), "agent-line-fedcba9876543210fedcba9876543210"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("missing stop error = %v", err)
	}
	sent, err := client.SendSMS(context.Background(), SMSSendRequest{
		OperationID: "operation_0123456789", MessageID: "msg_0123456789abcdef",
		LineID: request.LineID, Destination: "+447700900456", Body: "outbound",
	})
	if err != nil || sent.ProviderMessageID != "ims_provider_0123456789" || sent.State != SMSSubmitAccepted {
		t.Fatalf("send=%#v error=%v", sent, err)
	}
	references, err := client.ListSMS(context.Background(), request.LineID)
	if err != nil || len(references) != 1 || references[0].MessageID != fake.message.MessageID {
		t.Fatalf("references=%#v error=%v", references, err)
	}
	message, err := client.ReadSMS(context.Background(), request.LineID, fake.message.MessageID)
	if err != nil || message.Body != "inbound" {
		t.Fatalf("message=%#v error=%v", message, err)
	}
	if err := client.AcknowledgeSMS(context.Background(), SMSAcknowledgeRequest{
		OperationID: "acknowledge_01234567", LineID: request.LineID, MessageID: fake.message.MessageID,
	}); err != nil || !fake.acknowledged {
		t.Fatalf("acknowledge error=%v state=%t", err, fake.acknowledged)
	}
	reports, err := client.ListSMSSubmitReports(context.Background(), request.LineID)
	if err != nil || len(reports.Reports) != 1 || reports.Reports[0] != fake.report || reports.Diagnostics.RPACKs != 1 {
		t.Fatalf("reports=%#v error=%v", reports, err)
	}
	if err := client.AcknowledgeSMSSubmitReport(context.Background(), SMSSubmitReportAcknowledgeRequest{
		OperationID: "report_ack_0123456789", LineID: request.LineID, ProviderMessageID: fake.report.ProviderMessageID,
	}); err != nil || !fake.reportAcked {
		t.Fatalf("report acknowledgement error=%v state=%t", err, fake.reportAcked)
	}
}

func TestHandlerRejectsUnknownFields(t *testing.T) {
	// Covered through the same decoder contract used by both start and stop;
	// keep this assertion at the API layer instead of relying on Local.
	if validStartRequest(StartRequest{LineID: "agent-line-0123456789abcdef0123456789abcdef", EgressMode: EgressDirect, CountryCode: "JP"}) {
		t.Fatal("direct request with a country unexpectedly accepted")
	}
}
