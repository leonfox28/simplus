package messaging

import (
	"context"
	"errors"
	"fmt"

	"github.com/leonfox28/simplus/internal/smscodec"
	"github.com/leonfox28/simplus/internal/vowifisupervisor"
)

type VoWiFiSMSGateway struct {
	client vowifisupervisor.SMSAPI
}

func NewVoWiFiSMSGateway(client vowifisupervisor.SMSAPI) (*VoWiFiSMSGateway, error) {
	if client == nil {
		return nil, errors.New("Host VoWiFi SMS gateway configuration is invalid")
	}
	return &VoWiFiSMSGateway{client: client}, nil
}

func (gateway *VoWiFiSMSGateway) SendSMS(ctx context.Context, command SendSMSCommand) (SendSMSResult, error) {
	response, err := gateway.client.SendSMS(ctx, vowifisupervisor.SMSSendRequest{
		OperationID: command.OperationID, MessageID: command.MessageID, LineID: command.LineID,
		Destination: command.Destination, Body: command.Body,
	})
	if err != nil {
		switch {
		case errors.Is(err, vowifisupervisor.ErrSMSOutcomeUnknown):
			return SendSMSResult{}, &TransportError{Code: ErrorSendOutcomeUnknown}
		case errors.Is(err, vowifisupervisor.ErrSMSRejected):
			return SendSMSResult{}, &TransportError{Code: "IMS_SMS_REJECTED"}
		default:
			return SendSMSResult{}, &TransportError{Code: "IMS_SMS_UNAVAILABLE"}
		}
	}
	return SendSMSResult{
		ProviderMessageID: response.ProviderMessageID, State: response.State, ErrorCode: response.ErrorCode,
	}, nil
}

func (gateway *VoWiFiSMSGateway) ListSMS(ctx context.Context, target InboxTarget) ([]InboxMessageReference, error) {
	references, err := gateway.client.ListSMS(ctx, target.LineID)
	if err != nil {
		return nil, fmt.Errorf("list Host VoWiFi SMS: %w", err)
	}
	result := make([]InboxMessageReference, 0, len(references))
	for _, reference := range references {
		result = append(result, InboxMessageReference{SourceMessageID: reference.MessageID, ReceivedAt: reference.ReceivedAt})
	}
	return result, nil
}

func (gateway *VoWiFiSMSGateway) ReadSMS(ctx context.Context, target InboxTarget, messageID string) (InboxMessage, error) {
	message, err := gateway.client.ReadSMS(ctx, target.LineID, messageID)
	if err != nil {
		return InboxMessage{}, fmt.Errorf("read Host VoWiFi SMS: %w", err)
	}
	segment := &smscodec.Segment{
		Encoding: smscodec.Encoding(message.Encoding), Reference: uint16(message.ConcatenationReference),
		Part: message.Part, Total: message.Total, UnitCount: message.UnitCount,
		UserData: append([]byte(nil), message.UserData...),
	}
	return InboxMessage{SourceMessageID: message.MessageID, Sender: message.Sender, Body: message.Body, ReceivedAt: message.ReceivedAt, Segment: segment}, nil
}

func (gateway *VoWiFiSMSGateway) AcknowledgeSMS(ctx context.Context, target InboxTarget, messageID, operationID string) error {
	err := gateway.client.AcknowledgeSMS(ctx, vowifisupervisor.SMSAcknowledgeRequest{
		OperationID: operationID, LineID: target.LineID, MessageID: messageID,
	})
	if err != nil {
		return fmt.Errorf("acknowledge Host VoWiFi SMS: %w", err)
	}
	return nil
}

func (gateway *VoWiFiSMSGateway) ListSMSSubmitReports(ctx context.Context, target InboxTarget) ([]SubmitReport, error) {
	response, err := gateway.client.ListSMSSubmitReports(ctx, target.LineID)
	if err != nil {
		return nil, fmt.Errorf("list Host VoWiFi SMS submit reports: %w", err)
	}
	reports := make([]SubmitReport, 0, len(response.Reports))
	for _, report := range response.Reports {
		reports = append(reports, SubmitReport{
			MessageID: report.MessageID, ProviderMessageID: report.ProviderMessageID,
			State: report.State, ErrorCode: report.ErrorCode, CompletedAt: report.CompletedAt,
		})
	}
	return reports, nil
}

func (gateway *VoWiFiSMSGateway) AcknowledgeSMSSubmitReport(ctx context.Context, target InboxTarget,
	providerMessageID, operationID string) error {
	err := gateway.client.AcknowledgeSMSSubmitReport(ctx, vowifisupervisor.SMSSubmitReportAcknowledgeRequest{
		OperationID: operationID, LineID: target.LineID, ProviderMessageID: providerMessageID,
	})
	if err != nil {
		return fmt.Errorf("acknowledge Host VoWiFi SMS submit report: %w", err)
	}
	return nil
}

var _ Sender = (*VoWiFiSMSGateway)(nil)
var _ Inbox = (*VoWiFiSMSGateway)(nil)
var _ SubmitReportInbox = (*VoWiFiSMSGateway)(nil)
