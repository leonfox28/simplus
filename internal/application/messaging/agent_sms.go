package messaging

import (
	"context"
	"errors"
	"fmt"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/smscodec"
)

type AgentSMSGateway struct {
	client     agentapi.SMSClientAPI
	instanceID string
}

func NewAgentSMSGateway(client agentapi.SMSClientAPI, instanceID string) (*AgentSMSGateway, error) {
	if client == nil || !agentapi.IsValidAgentInstanceID(instanceID) {
		return nil, errors.New("Agent SMS gateway configuration is invalid")
	}
	return &AgentSMSGateway{client: client, instanceID: instanceID}, nil
}

func (gateway *AgentSMSGateway) SendSMS(ctx context.Context, command SendSMSCommand) (SendSMSResult, error) {
	decoded, err := smscodec.Decode(command.Segments)
	if err != nil || decoded != command.Body {
		return SendSMSResult{}, &TransportError{Code: "SMS_ENCODING_INVALID"}
	}
	response, err := gateway.client.SendSMS(ctx, agentapi.SMSSendRequest{
		OperationID: command.OperationID, AgentInstanceID: gateway.instanceID, DeviceID: command.PhysicalDeviceID,
		Destination: command.Destination, Body: command.Body,
	})
	if err != nil {
		if errors.Is(err, agentapi.ErrSMSOutcomeUnknown) {
			return SendSMSResult{}, &TransportError{Code: ErrorSendOutcomeUnknown}
		}
		return SendSMSResult{}, &TransportError{Code: "AGENT_SMS_SEND_FAILED"}
	}
	return SendSMSResult{ProviderMessageID: response.Submission.MessageID}, nil
}

func (gateway *AgentSMSGateway) ListSMS(ctx context.Context, deviceID string) ([]InboxMessageReference, error) {
	response, err := gateway.client.ListSMS(ctx, agentapi.SMSListRequest{AgentInstanceID: gateway.instanceID, DeviceID: deviceID})
	if err != nil {
		return nil, fmt.Errorf("list Agent SMS: %w", err)
	}
	references := make([]InboxMessageReference, 0, len(response.Messages))
	for _, message := range response.Messages {
		references = append(references, InboxMessageReference{SourceMessageID: message.MessageID, ReceivedAt: message.ReceivedAt})
	}
	return references, nil
}

func (gateway *AgentSMSGateway) ReadSMS(ctx context.Context, deviceID, messageID string) (InboxMessage, error) {
	response, err := gateway.client.ReadSMS(ctx, agentapi.SMSReadRequest{
		AgentInstanceID: gateway.instanceID, DeviceID: deviceID, MessageID: messageID,
	})
	if err != nil {
		return InboxMessage{}, fmt.Errorf("read Agent SMS: %w", err)
	}
	return InboxMessage{
		SourceMessageID: response.Message.MessageID, Sender: response.Message.Sender,
		Body: response.Message.Body, ReceivedAt: response.Message.ReceivedAt,
	}, nil
}

func (gateway *AgentSMSGateway) AcknowledgeSMS(ctx context.Context, deviceID, messageID, operationID string) error {
	_, err := gateway.client.AcknowledgeSMS(ctx, agentapi.SMSAcknowledgeRequest{
		OperationID: operationID, AgentInstanceID: gateway.instanceID, DeviceID: deviceID, MessageID: messageID,
	})
	if err != nil {
		return fmt.Errorf("acknowledge Agent SMS: %w", err)
	}
	return nil
}
