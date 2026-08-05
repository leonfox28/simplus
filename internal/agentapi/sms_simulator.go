package agentapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/leonfox28/simplus/internal/smscodec"
)

const SimulatorInboundMessageID = "simulator-inbound-welcome-1"

type simulatorSendRecord struct {
	request    SMSSendRequest
	submission SMSSubmission
}

type simulatorAcknowledgeRecord struct {
	deviceID  string
	messageID string
}

type SimulatorSMSBackend struct {
	mu               sync.Mutex
	now              func() time.Time
	messages         map[string]map[string]SMSStoredMessage
	sends            map[string]simulatorSendRecord
	acknowledgements map[string]simulatorAcknowledgeRecord
}

func NewSimulatorSMSBackend(initial ...SMSStoredMessage) *SimulatorSMSBackend {
	backend := &SimulatorSMSBackend{
		now: time.Now, messages: make(map[string]map[string]SMSStoredMessage),
		sends: make(map[string]simulatorSendRecord), acknowledgements: make(map[string]simulatorAcknowledgeRecord),
	}
	for _, message := range initial {
		if backend.messages[message.DeviceID] == nil {
			backend.messages[message.DeviceID] = make(map[string]SMSStoredMessage)
		}
		backend.messages[message.DeviceID][message.MessageID] = message
	}
	return backend
}

func NewDefaultSimulatorSMSBackend() *SimulatorSMSBackend {
	return NewSimulatorSMSBackend(SMSStoredMessage{
		MessageID: SimulatorInboundMessageID, DeviceID: "simulator-device-1", Sender: "10086",
		Body: "欢迎使用 Simplus Simulator。", ReceivedAt: time.Now().UTC(),
	})
}

func (backend *SimulatorSMSBackend) ListSMS(ctx context.Context, deviceID string) ([]SMSMessageReference, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	messages := backend.messages[deviceID]
	references := make([]SMSMessageReference, 0, len(messages))
	for _, message := range messages {
		references = append(references, SMSMessageReference{
			MessageID: message.MessageID, DeviceID: message.DeviceID, Sender: message.Sender, ReceivedAt: message.ReceivedAt,
		})
	}
	sort.Slice(references, func(left, right int) bool {
		if references[left].ReceivedAt.Equal(references[right].ReceivedAt) {
			return references[left].MessageID < references[right].MessageID
		}
		return references[left].ReceivedAt.Before(references[right].ReceivedAt)
	})
	return references, nil
}

func (backend *SimulatorSMSBackend) ReadSMS(ctx context.Context, deviceID, messageID string) (SMSStoredMessage, error) {
	if err := ctx.Err(); err != nil {
		return SMSStoredMessage{}, err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	message, found := backend.messages[deviceID][messageID]
	if !found {
		return SMSStoredMessage{}, ErrSMSMessageNotFound
	}
	return message, nil
}

func (backend *SimulatorSMSBackend) SendSMS(ctx context.Context, request SMSSendRequest) (SMSSubmission, error) {
	if err := ctx.Err(); err != nil {
		return SMSSubmission{}, err
	}
	segments, err := smscodec.Encode(request.Body)
	if err != nil {
		return SMSSubmission{}, ErrSMSRequestInvalid
	}
	decoded, err := smscodec.Decode(segments)
	if err != nil || decoded != request.Body {
		return SMSSubmission{}, errors.New("simulator SMS codec round trip failed")
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if existing, found := backend.sends[request.OperationID]; found {
		if existing.request.DeviceID != request.DeviceID || existing.request.Destination != request.Destination || existing.request.Body != request.Body ||
			existing.request.AgentInstanceID != request.AgentInstanceID {
			return SMSSubmission{}, ErrSMSOperationConflict
		}
		return existing.submission, nil
	}
	digest := sha256.Sum256([]byte(request.OperationID))
	submission := SMSSubmission{
		OperationID: request.OperationID, MessageID: "simulator-outbound-" + hex.EncodeToString(digest[:8]), SubmittedAt: backend.now().UTC(),
	}
	backend.sends[request.OperationID] = simulatorSendRecord{request: request, submission: submission}
	return submission, nil
}

func (backend *SimulatorSMSBackend) AcknowledgeSMS(ctx context.Context, request SMSAcknowledgeRequest) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if existing, found := backend.acknowledgements[request.OperationID]; found {
		if existing.deviceID != request.DeviceID || existing.messageID != request.MessageID {
			return false, ErrSMSOperationConflict
		}
		return true, nil
	}
	if _, found := backend.messages[request.DeviceID][request.MessageID]; !found {
		return false, ErrSMSMessageNotFound
	}
	delete(backend.messages[request.DeviceID], request.MessageID)
	backend.acknowledgements[request.OperationID] = simulatorAcknowledgeRecord{deviceID: request.DeviceID, messageID: request.MessageID}
	return true, nil
}

type LocalSMSClient struct {
	instanceID string
	backend    SMSBackend
}

var _ SMSClientAPI = (*LocalSMSClient)(nil)

func NewLocalSMSClient(instanceID string, backend SMSBackend) (*LocalSMSClient, error) {
	if !IsValidAgentInstanceID(instanceID) || backend == nil {
		return nil, errors.New("local Agent SMS client configuration is invalid")
	}
	return &LocalSMSClient{instanceID: instanceID, backend: backend}, nil
}

func (client *LocalSMSClient) ListSMS(ctx context.Context, request SMSListRequest) (SMSListResponse, error) {
	if err := validateSMSListRequest(request); err != nil {
		return SMSListResponse{}, err
	}
	if request.AgentInstanceID != client.instanceID {
		return SMSListResponse{}, ErrSMSAgentStale
	}
	messages, err := client.backend.ListSMS(ctx, request.DeviceID)
	if err != nil {
		return SMSListResponse{}, err
	}
	response := SMSListResponse{ProtocolVersion: ProtocolVersion, AgentInstanceID: client.instanceID, Messages: messages}
	if err := validateSMSListResponse(response, request); err != nil {
		return SMSListResponse{}, err
	}
	return response, nil
}

func (client *LocalSMSClient) ReadSMS(ctx context.Context, request SMSReadRequest) (SMSReadResponse, error) {
	if err := validateSMSReadRequest(request); err != nil {
		return SMSReadResponse{}, err
	}
	if request.AgentInstanceID != client.instanceID {
		return SMSReadResponse{}, ErrSMSAgentStale
	}
	message, err := client.backend.ReadSMS(ctx, request.DeviceID, request.MessageID)
	if err != nil {
		return SMSReadResponse{}, err
	}
	response := SMSReadResponse{ProtocolVersion: ProtocolVersion, AgentInstanceID: client.instanceID, Message: message}
	if err := validateSMSReadResponse(response, request); err != nil {
		return SMSReadResponse{}, err
	}
	return response, nil
}

func (client *LocalSMSClient) SendSMS(ctx context.Context, request SMSSendRequest) (SMSSendResponse, error) {
	if err := validateSMSSendRequest(request); err != nil {
		return SMSSendResponse{}, err
	}
	if request.AgentInstanceID != client.instanceID {
		return SMSSendResponse{}, ErrSMSAgentStale
	}
	submission, err := client.backend.SendSMS(ctx, request)
	if err != nil {
		return SMSSendResponse{}, err
	}
	response := SMSSendResponse{ProtocolVersion: ProtocolVersion, AgentInstanceID: client.instanceID, Submission: submission}
	if err := validateSMSSendResponse(response, request); err != nil {
		return SMSSendResponse{}, err
	}
	return response, nil
}

func (client *LocalSMSClient) AcknowledgeSMS(ctx context.Context, request SMSAcknowledgeRequest) (SMSAcknowledgeResponse, error) {
	if err := validateSMSAcknowledgeRequest(request); err != nil {
		return SMSAcknowledgeResponse{}, err
	}
	if request.AgentInstanceID != client.instanceID {
		return SMSAcknowledgeResponse{}, ErrSMSAgentStale
	}
	acknowledged, err := client.backend.AcknowledgeSMS(ctx, request)
	if err != nil {
		return SMSAcknowledgeResponse{}, err
	}
	response := SMSAcknowledgeResponse{
		ProtocolVersion: ProtocolVersion, AgentInstanceID: client.instanceID, OperationID: request.OperationID,
		MessageID: request.MessageID, Acknowledged: acknowledged,
	}
	if err := validateSMSAcknowledgeResponse(response, request); err != nil {
		return SMSAcknowledgeResponse{}, err
	}
	return response, nil
}
