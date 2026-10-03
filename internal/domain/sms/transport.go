package sms

import (
	"context"
	"time"

	"github.com/leonfox28/simplus/internal/smscodec"
)

type SendSMSCommand struct {
	AgentInstanceID                 string
	OperationID                     string
	MessageID                       string
	LineID                          string
	PhysicalDeviceID                string
	ModemFunctionID                 string
	Destination                     string
	Body                            string
	Segments                        []smscodec.Segment
	DeviceGeneration                uint64
	ExpectedEquipmentFingerprint    string
	ExpectedSubscriptionFingerprint string
}

type SendSMSResult struct {
	ProviderMessageID string
	State             string
	ErrorCode         string
}

type Sender interface {
	SendSMS(context.Context, SendSMSCommand) (SendSMSResult, error)
}

type InboxMessageReference struct {
	SourceMessageID string
	ReceivedAt      time.Time
}

type InboxMessage struct {
	SourceMessageID string
	Sender          string
	Body            string
	ReceivedAt      time.Time
	Segment         *smscodec.Segment
}

type InboxTarget struct {
	AgentInstanceID                 string
	LineID                          string
	PhysicalDeviceID                string
	DeviceGeneration                uint64
	ExpectedEquipmentFingerprint    string
	ExpectedSubscriptionFingerprint string
}

type Inbox interface {
	ListSMS(context.Context, InboxTarget) ([]InboxMessageReference, error)
	ReadSMS(context.Context, InboxTarget, string) (InboxMessage, error)
	AcknowledgeSMS(context.Context, InboxTarget, string, string) error
}

type SubmitReport struct {
	MessageID         string
	ProviderMessageID string
	State             string
	ErrorCode         string
	CompletedAt       time.Time
}

type SubmitReportInbox interface {
	ListSMSSubmitReports(context.Context, InboxTarget) ([]SubmitReport, error)
	AcknowledgeSMSSubmitReport(context.Context, InboxTarget, string, string) error
}

type TransportError struct {
	Code string
}

func (err *TransportError) Error() string {
	if err == nil || err.Code == "" {
		return "SMS transport failed"
	}
	return "SMS transport failed: " + err.Code
}

const (
	ErrorOutcomeUnknownAfterRestart = "SEND_OUTCOME_UNKNOWN_AFTER_RESTART"
	ErrorSendOutcomeUnknown         = "SMS_SEND_OUTCOME_UNKNOWN"
	ErrorAcceptedAwaitingReport     = "IMS_SMS_ACCEPTED_AWAITING_REPORT"
	ErrorCancelledBeforeDispatch    = "SEND_CANCELLED_BEFORE_DISPATCH"
	ErrorTransportFailed            = "SMS_TRANSPORT_FAILED"
	ErrorSIMNotReady                = "SMS_SIM_NOT_READY"
	ErrorSIMIdentityChanged         = "SMS_SIM_IDENTITY_CHANGED"
	ErrorEquipmentIdentityChanged   = "SMS_EQUIPMENT_IDENTITY_CHANGED"
	ErrorRFOff                      = "SMS_RF_OFF"
	ErrorRegistrationDenied         = "SMS_REGISTRATION_DENIED"
	ErrorNotRegistered              = "SMS_NOT_REGISTERED"
	ErrorStatusUnavailable          = "SMS_STATUS_UNAVAILABLE"
	ErrorDeviceStale                = "SMS_DEVICE_STALE"
	SendStateAccepted               = "accepted"
	SendStateSent                   = "sent"
	SendStateFailed                 = "failed"
	SendStateUnconfirmed            = "unconfirmed"
)
