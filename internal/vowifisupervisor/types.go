package vowifisupervisor

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	EgressDirect        = "direct"
	EgressMihomoCountry = "mihomo-country"

	StateStopped      = "stopped"
	StateStarting     = "starting"
	StateConnecting   = "connecting"
	StateRegistering  = "registering"
	StateOnline       = "online"
	StateReconnecting = "reconnecting"
	StateStopping     = "stopping"
	StateFailed       = "failed"

	SMSSubmitAccepted    = "accepted"
	SMSSubmitSent        = "sent"
	SMSSubmitFailed      = "failed"
	SMSSubmitUnconfirmed = "unconfirmed"
)

var (
	ErrAlreadyRunning     = errors.New("Host VoWiFi Line is already running")
	ErrNotRunning         = errors.New("Host VoWiFi Line is not running")
	ErrRequestInvalid     = errors.New("Host VoWiFi supervisor request is invalid")
	ErrStartupFailed      = errors.New("Host VoWiFi worker startup failed")
	ErrSMSUnavailable     = errors.New("Host VoWiFi SMS is unavailable")
	ErrSMSMessageNotFound = errors.New("Host VoWiFi SMS message was not found")
	ErrSMSOutcomeUnknown  = errors.New("Host VoWiFi SMS outcome is unknown")
	ErrSMSRejected        = errors.New("Host VoWiFi SMS was rejected")
)

var (
	smsOperationPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)
	smsAddressPattern   = regexp.MustCompile(`^\+?[0-9]{3,20}$`)
	phoneNumberPattern  = regexp.MustCompile(`^\+[1-9][0-9]{2,14}$`)
	errorCodePattern    = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
)

// StartRequest contains a stable business Line and the current opaque hardware
// target resolved by the unprivileged application. The privileged supervisor
// still resolves every executable path, port and kernel object itself.
type StartRequest struct {
	LineID         string `json:"lineId"`
	HardwareLineID string `json:"hardwareLineId"`
	EgressMode     string `json:"egressMode"`
	CountryCode    string `json:"countryCode"`
}

type StopRequest struct {
	LineID string `json:"lineId"`
}

// Status is safe for the management plane. It intentionally excludes PID,
// namespace/interface names, addresses, P-CSCF, SPIs and authentication data.
// PhoneNumber is present only when IMS returned an unambiguous E.164 identity.
type Status struct {
	LineID       string    `json:"lineId"`
	State        string    `json:"state"`
	Stage        string    `json:"stage,omitempty"`
	Online       bool      `json:"online"`
	EgressMode   string    `json:"egressMode"`
	CountryCode  string    `json:"countryCode"`
	StartedAt    time.Time `json:"startedAt,omitempty"`
	RegisteredAt time.Time `json:"registeredAt,omitempty"`
	NextRefresh  time.Time `json:"nextRefreshAt,omitempty"`
	PhoneNumber  string    `json:"phoneNumber"`
	Attempt      int       `json:"attempt"`
	ErrorCode    string    `json:"errorCode,omitempty"`
}

type StatusList struct {
	Lines []Status `json:"lines"`
}

type SMSSendRequest struct {
	OperationID string `json:"operationId"`
	MessageID   string `json:"messageId"`
	LineID      string `json:"lineId"`
	Destination string `json:"destination"`
	Body        string `json:"body"`
}

type SMSSendResponse struct {
	ProviderMessageID string `json:"providerMessageId"`
	State             string `json:"state"`
	ErrorCode         string `json:"errorCode,omitempty"`
}

type SMSListRequest struct {
	LineID string `json:"lineId"`
}

type SMSMessageReference struct {
	MessageID  string    `json:"messageId"`
	ReceivedAt time.Time `json:"receivedAt"`
}

type SMSListResponse struct {
	Messages []SMSMessageReference `json:"messages"`
}

type SMSReadRequest struct {
	LineID    string `json:"lineId"`
	MessageID string `json:"messageId"`
}

type SMSMessage struct {
	MessageID              string    `json:"messageId"`
	Sender                 string    `json:"sender"`
	Body                   string    `json:"body,omitempty"`
	Encoding               string    `json:"encoding"`
	ConcatenationReference int       `json:"concatenationReference"`
	Part                   int       `json:"part"`
	Total                  int       `json:"total"`
	UnitCount              int       `json:"unitCount"`
	UserData               []byte    `json:"userData"`
	ReceivedAt             time.Time `json:"receivedAt"`
}

type SMSAcknowledgeRequest struct {
	OperationID string `json:"operationId"`
	LineID      string `json:"lineId"`
	MessageID   string `json:"messageId"`
}

type SMSAcknowledgeResponse struct {
	Acknowledged bool `json:"acknowledged"`
}

type SMSSubmitReport struct {
	MessageID         string    `json:"messageId"`
	ProviderMessageID string    `json:"providerMessageId"`
	State             string    `json:"state"`
	ErrorCode         string    `json:"errorCode,omitempty"`
	Cause             int       `json:"cause,omitempty"`
	CompletedAt       time.Time `json:"completedAt"`
}

// SMSProtocolDiagnostics contains counters only. It intentionally excludes
// identities, addresses, message bodies, PDU bytes and SIP headers.
type SMSProtocolDiagnostics struct {
	SIPRequests         uint64 `json:"sipRequests"`
	SIPParseFailures    uint64 `json:"sipParseFailures"`
	RPParseFailures     uint64 `json:"rpParseFailures"`
	RPDataDeliveries    uint64 `json:"rpDataDeliveries"`
	RPACKs              uint64 `json:"rpAcks"`
	RPErrors            uint64 `json:"rpErrors"`
	CorrelationFailures uint64 `json:"correlationFailures"`
	ReportTimeouts      uint64 `json:"reportTimeouts"`
}

type SMSSubmitReportListResponse struct {
	Reports     []SMSSubmitReport      `json:"reports"`
	Diagnostics SMSProtocolDiagnostics `json:"diagnostics"`
}

type SMSSubmitReportAcknowledgeRequest struct {
	OperationID       string `json:"operationId"`
	LineID            string `json:"lineId"`
	ProviderMessageID string `json:"providerMessageId"`
}

type API interface {
	List(context.Context) ([]Status, error)
	Start(context.Context, StartRequest) (Status, error)
	Stop(context.Context, string) (Status, error)
}

type SMSAPI interface {
	SendSMS(context.Context, SMSSendRequest) (SMSSendResponse, error)
	ListSMS(context.Context, string) ([]SMSMessageReference, error)
	ReadSMS(context.Context, string, string) (SMSMessage, error)
	AcknowledgeSMS(context.Context, SMSAcknowledgeRequest) error
	ListSMSSubmitReports(context.Context, string) (SMSSubmitReportListResponse, error)
	AcknowledgeSMSSubmitReport(context.Context, SMSSubmitReportAcknowledgeRequest) error
}

func validSMSSendRequest(request SMSSendRequest) bool {
	return managedLinePattern.MatchString(request.LineID) && smsOperationPattern.MatchString(request.OperationID) &&
		smsOperationPattern.MatchString(request.MessageID) &&
		smsAddressPattern.MatchString(request.Destination) && strings.TrimSpace(request.Body) != "" &&
		utf8.ValidString(request.Body) && utf8.RuneCountInString(request.Body) <= 1600 && len(request.Body) <= 6400
}

func validSMSSendResponse(response SMSSendResponse) bool {
	if !smsOperationPattern.MatchString(response.ProviderMessageID) {
		return false
	}
	switch response.State {
	case SMSSubmitAccepted, SMSSubmitSent:
		return response.ErrorCode == ""
	case SMSSubmitFailed, SMSSubmitUnconfirmed:
		return errorCodePattern.MatchString(response.ErrorCode)
	default:
		return false
	}
}

func validSMSMessageRequest(lineID, messageID string) bool {
	return managedLinePattern.MatchString(lineID) && smsOperationPattern.MatchString(messageID)
}

func validSMSAcknowledgeRequest(request SMSAcknowledgeRequest) bool {
	return validSMSMessageRequest(request.LineID, request.MessageID) && smsOperationPattern.MatchString(request.OperationID)
}

func validSMSSubmitReportAcknowledgeRequest(request SMSSubmitReportAcknowledgeRequest) bool {
	return managedLinePattern.MatchString(request.LineID) && smsOperationPattern.MatchString(request.OperationID) &&
		smsOperationPattern.MatchString(request.ProviderMessageID)
}

func validSMSSubmitReport(report SMSSubmitReport) bool {
	if !smsOperationPattern.MatchString(report.MessageID) || !smsOperationPattern.MatchString(report.ProviderMessageID) ||
		report.CompletedAt.IsZero() || report.Cause < 0 || report.Cause > 255 {
		return false
	}
	switch report.State {
	case SMSSubmitSent:
		return report.ErrorCode == ""
	case SMSSubmitFailed, SMSSubmitUnconfirmed:
		return errorCodePattern.MatchString(report.ErrorCode)
	default:
		return false
	}
}
