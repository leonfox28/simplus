package agentapi

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	FeatureSMS              = "sms-v1"
	FeatureHardwareReadOnly = "hardware-read-only-policy-v1"

	// SMSDispatchTimeout is the whole modem-side send budget, including every
	// multipart segment. The outer HTTP budget leaves time to persist and
	// return an outcome after modem dispatch stops.
	SMSDispatchTimeout = 120 * time.Second
	SMSRequestTimeout  = SMSDispatchTimeout + 10*time.Second
)

var (
	ErrSMSRequestInvalid     = errors.New("Agent SMS request is invalid")
	ErrSMSAgentStale         = errors.New("Agent SMS request targets a stale Agent instance")
	ErrSMSDeviceNotFound     = errors.New("Agent SMS device was not found")
	ErrSMSUnsupported        = errors.New("Agent SMS is unsupported for this device")
	ErrSMSMessageNotFound    = errors.New("Agent SMS message was not found")
	ErrSMSOperationConflict  = errors.New("Agent SMS operation id belongs to different parameters")
	ErrSMSOutcomeUnknown     = errors.New("Agent SMS send may have completed but its outcome is unknown")
	ErrSMSDeviceStale        = errors.New("Agent SMS device generation changed")
	ErrSMSEquipmentIdentity  = errors.New("Agent SMS equipment identity changed")
	ErrSMSSIMNotReady        = errors.New("Agent SMS SIM is not ready")
	ErrSMSSIMIdentity        = errors.New("Agent SMS SIM identity changed")
	ErrSMSRFOff              = errors.New("Agent SMS RF is off")
	ErrSMSRegistrationDenied = errors.New("Agent SMS registration was denied")
	ErrSMSNotRegistered      = errors.New("Agent SMS is not registered")
	ErrSMSStatusUnavailable  = errors.New("Agent SMS status is unavailable")
)

var (
	smsOperationIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)
	smsAddressPattern     = regexp.MustCompile(`^\+?[0-9]{3,20}$`)
	smsSenderPattern      = regexp.MustCompile(`^(?:\+?[0-9]{3,20}|[A-Za-z][A-Za-z0-9 ._-]{0,19})$`)
)

type SMSListRequest struct {
	AgentInstanceID                 string `json:"agentInstanceId"`
	DeviceID                        string `json:"deviceId"`
	DeviceGeneration                uint64 `json:"deviceGeneration"`
	ExpectedEquipmentFingerprint    string `json:"expectedEquipmentFingerprint"`
	ExpectedSubscriptionFingerprint string `json:"expectedSubscriptionFingerprint"`
}

type SMSMessageReference struct {
	// MessageID is stable for repeated list/read calls until acknowledgement
	// and must not be reused for different message content.
	MessageID  string    `json:"messageId"`
	DeviceID   string    `json:"deviceId"`
	Sender     string    `json:"sender"`
	ReceivedAt time.Time `json:"receivedAt"`
}

type SMSListResponse struct {
	ProtocolVersion int                   `json:"protocolVersion"`
	AgentInstanceID string                `json:"agentInstanceId"`
	Messages        []SMSMessageReference `json:"messages"`
}

type SMSReadRequest struct {
	AgentInstanceID                 string `json:"agentInstanceId"`
	DeviceID                        string `json:"deviceId"`
	MessageID                       string `json:"messageId"`
	DeviceGeneration                uint64 `json:"deviceGeneration"`
	ExpectedEquipmentFingerprint    string `json:"expectedEquipmentFingerprint"`
	ExpectedSubscriptionFingerprint string `json:"expectedSubscriptionFingerprint"`
}

type SMSStoredMessage struct {
	MessageID  string    `json:"messageId"`
	DeviceID   string    `json:"deviceId"`
	Sender     string    `json:"sender"`
	Body       string    `json:"body"`
	ReceivedAt time.Time `json:"receivedAt"`
}

type SMSReadResponse struct {
	ProtocolVersion int              `json:"protocolVersion"`
	AgentInstanceID string           `json:"agentInstanceId"`
	Message         SMSStoredMessage `json:"message"`
}

type SMSSendRequest struct {
	OperationID                     string `json:"operationId"`
	AgentInstanceID                 string `json:"agentInstanceId"`
	DeviceID                        string `json:"deviceId"`
	Destination                     string `json:"destination"`
	Body                            string `json:"body"`
	DeviceGeneration                uint64 `json:"deviceGeneration"`
	ExpectedEquipmentFingerprint    string `json:"expectedEquipmentFingerprint"`
	ExpectedSubscriptionFingerprint string `json:"expectedSubscriptionFingerprint"`
}

type SMSSubmission struct {
	OperationID string    `json:"operationId"`
	MessageID   string    `json:"messageId"`
	SubmittedAt time.Time `json:"submittedAt"`
}

type SMSSendResponse struct {
	ProtocolVersion int           `json:"protocolVersion"`
	AgentInstanceID string        `json:"agentInstanceId"`
	Submission      SMSSubmission `json:"submission"`
}

type SMSAcknowledgeRequest struct {
	OperationID                     string `json:"operationId"`
	AgentInstanceID                 string `json:"agentInstanceId"`
	DeviceID                        string `json:"deviceId"`
	MessageID                       string `json:"messageId"`
	DeviceGeneration                uint64 `json:"deviceGeneration"`
	ExpectedEquipmentFingerprint    string `json:"expectedEquipmentFingerprint"`
	ExpectedSubscriptionFingerprint string `json:"expectedSubscriptionFingerprint"`
}

type SMSAcknowledgeResponse struct {
	ProtocolVersion int    `json:"protocolVersion"`
	AgentInstanceID string `json:"agentInstanceId"`
	OperationID     string `json:"operationId"`
	MessageID       string `json:"messageId"`
	Acknowledged    bool   `json:"acknowledged"`
}

type SMSClientAPI interface {
	ListSMS(context.Context, SMSListRequest) (SMSListResponse, error)
	ReadSMS(context.Context, SMSReadRequest) (SMSReadResponse, error)
	SendSMS(context.Context, SMSSendRequest) (SMSSendResponse, error)
	AcknowledgeSMS(context.Context, SMSAcknowledgeRequest) (SMSAcknowledgeResponse, error)
}

type SMSBackend interface {
	ListSMS(context.Context, SMSListRequest) ([]SMSMessageReference, error)
	ReadSMS(context.Context, SMSReadRequest) (SMSStoredMessage, error)
	SendSMS(context.Context, SMSSendRequest) (SMSSubmission, error)
	AcknowledgeSMS(context.Context, SMSAcknowledgeRequest) (bool, error)
}

func validateSMSListRequest(request SMSListRequest) error {
	return validateSMSTarget(request.AgentInstanceID, request.DeviceID, request.DeviceGeneration, request.ExpectedEquipmentFingerprint, request.ExpectedSubscriptionFingerprint)
}

func validateSMSReadRequest(request SMSReadRequest) error {
	if err := validateSMSTarget(request.AgentInstanceID, request.DeviceID, request.DeviceGeneration, request.ExpectedEquipmentFingerprint, request.ExpectedSubscriptionFingerprint); err != nil || !validSMSOpaqueID(request.MessageID) {
		return ErrSMSRequestInvalid
	}
	return nil
}

func validateSMSSendRequest(request SMSSendRequest) error {
	if err := validateSMSTarget(request.AgentInstanceID, request.DeviceID, request.DeviceGeneration, request.ExpectedEquipmentFingerprint, request.ExpectedSubscriptionFingerprint); err != nil ||
		!smsOperationIDPattern.MatchString(request.OperationID) || !smsAddressPattern.MatchString(request.Destination) ||
		strings.TrimSpace(request.Body) == "" || !utf8.ValidString(request.Body) || utf8.RuneCountInString(request.Body) > 1600 || len(request.Body) > 6400 {
		return ErrSMSRequestInvalid
	}
	return nil
}

func validateSMSAcknowledgeRequest(request SMSAcknowledgeRequest) error {
	if err := validateSMSTarget(request.AgentInstanceID, request.DeviceID, request.DeviceGeneration, request.ExpectedEquipmentFingerprint, request.ExpectedSubscriptionFingerprint); err != nil ||
		!smsOperationIDPattern.MatchString(request.OperationID) || !validSMSOpaqueID(request.MessageID) {
		return ErrSMSRequestInvalid
	}
	return nil
}

func validateSMSTarget(instanceID, deviceID string, generation uint64, equipmentFingerprint, subscriptionFingerprint string) error {
	if !IsValidAgentInstanceID(instanceID) || strings.TrimSpace(deviceID) == "" || len(deviceID) > 128 || generation == 0 ||
		!isSHA256Hex(equipmentFingerprint) || !isSHA256Hex(subscriptionFingerprint) {
		return ErrSMSRequestInvalid
	}
	return nil
}

func validSMSOpaqueID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '-' && character != '_' {
			return false
		}
	}
	return true
}

func validateSMSListResponse(response SMSListResponse, request SMSListRequest) error {
	if err := validateSMSEnvelope(response.ProtocolVersion, response.AgentInstanceID, request.AgentInstanceID); err != nil {
		return err
	}
	if response.Messages == nil || len(response.Messages) > 4096 {
		return errors.New("invalid Agent SMS message list")
	}
	seen := make(map[string]struct{}, len(response.Messages))
	for _, message := range response.Messages {
		if message.DeviceID != request.DeviceID || !validSMSOpaqueID(message.MessageID) || !smsSenderPattern.MatchString(message.Sender) || message.ReceivedAt.IsZero() {
			return errors.New("invalid Agent SMS message reference")
		}
		if _, duplicate := seen[message.MessageID]; duplicate {
			return errors.New("duplicate Agent SMS message reference")
		}
		seen[message.MessageID] = struct{}{}
	}
	return nil
}

func validateSMSReadResponse(response SMSReadResponse, request SMSReadRequest) error {
	if err := validateSMSEnvelope(response.ProtocolVersion, response.AgentInstanceID, request.AgentInstanceID); err != nil {
		return err
	}
	message := response.Message
	if message.DeviceID != request.DeviceID || message.MessageID != request.MessageID || !smsSenderPattern.MatchString(message.Sender) ||
		strings.TrimSpace(message.Body) == "" || !utf8.ValidString(message.Body) || utf8.RuneCountInString(message.Body) > 1600 || len(message.Body) > 6400 || message.ReceivedAt.IsZero() {
		return errors.New("invalid Agent SMS read response")
	}
	return nil
}

func validateSMSSendResponse(response SMSSendResponse, request SMSSendRequest) error {
	if err := validateSMSEnvelope(response.ProtocolVersion, response.AgentInstanceID, request.AgentInstanceID); err != nil {
		return err
	}
	if response.Submission.OperationID != request.OperationID || !validSMSOpaqueID(response.Submission.MessageID) || response.Submission.SubmittedAt.IsZero() {
		return errors.New("invalid Agent SMS send response")
	}
	return nil
}

func validateSMSAcknowledgeResponse(response SMSAcknowledgeResponse, request SMSAcknowledgeRequest) error {
	if err := validateSMSEnvelope(response.ProtocolVersion, response.AgentInstanceID, request.AgentInstanceID); err != nil {
		return err
	}
	if response.OperationID != request.OperationID || response.MessageID != request.MessageID || !response.Acknowledged {
		return errors.New("invalid Agent SMS acknowledge response")
	}
	return nil
}

func validateSMSEnvelope(protocolVersion int, responseInstanceID, requestInstanceID string) error {
	if protocolVersion != ProtocolVersion || !IsValidAgentInstanceID(responseInstanceID) || responseInstanceID != requestInstanceID {
		return fmt.Errorf("invalid Agent SMS response envelope")
	}
	return nil
}
