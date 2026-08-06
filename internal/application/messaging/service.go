package messaging

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/leonfox28/simplus/internal/application/inventory"
	"github.com/leonfox28/simplus/internal/domain/sms"
	"github.com/leonfox28/simplus/internal/smscodec"
)

const (
	ErrorOutcomeUnknownAfterRestart = "SEND_OUTCOME_UNKNOWN_AFTER_RESTART"
	ErrorSendOutcomeUnknown         = "SMS_SEND_OUTCOME_UNKNOWN"
	ErrorAcceptedAwaitingReport     = "IMS_SMS_ACCEPTED_AWAITING_REPORT"
	ErrorCancelledBeforeDispatch    = "SEND_CANCELLED_BEFORE_DISPATCH"
	ErrorTransportFailed            = "SMS_TRANSPORT_FAILED"
	HistoryCapacity                 = 10000
	InboundFragmentRetention        = 7 * 24 * time.Hour
)

var (
	ErrRequestInvalid       = errors.New("SMS send request is invalid")
	ErrLineNotFound         = errors.New("SMS line was not found")
	ErrLineUnavailable      = errors.New("SMS line is unavailable")
	ErrLineUnsupported      = errors.New("SMS is unsupported on this line")
	ErrTransportUnavailable = errors.New("SMS transport is unavailable")
	ErrInventoryUnavailable = errors.New("SMS line inventory is unavailable")
	ErrPersistence          = errors.New("SMS persistence failed")
	ErrInboundSync          = errors.New("inbound SMS synchronization failed")
)

var (
	operationIDPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)
	lineIDPattern        = regexp.MustCompile(`^line_[A-Za-z0-9_-]{22}$`)
	destinationPattern   = regexp.MustCompile(`^\+?[0-9]{3,20}$`)
	remoteAddressPattern = regexp.MustCompile(`^(?:\+?[0-9]{3,20}|[A-Za-z][A-Za-z0-9 ._-]{0,19})$`)
	errorCodePattern     = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
)

type Repository interface {
	CreateOutboundSMS(context.Context, sms.Message) (sms.Message, bool, error)
	CreateInboundSMS(context.Context, sms.Message) (sms.Message, bool, error)
	StoreInboundSMSFragment(context.Context, sms.InboundFragment) ([]sms.InboundFragment, bool, error)
	PruneInboundSMSFragments(context.Context, time.Time) (int64, error)
	MarkOutboundSMSSent(context.Context, string, string, time.Time) (sms.Message, error)
	MarkOutboundSMSUnconfirmed(context.Context, string, string, string, time.Time) (sms.Message, error)
	MarkOutboundSMSFailed(context.Context, string, string, string, time.Time) (sms.Message, error)
	MarkQueuedOutboundSMSUnconfirmed(context.Context, string, time.Time) (int64, error)
	ListSMS(context.Context, int) ([]sms.Message, error)
	CountSMS(context.Context) (int64, error)
	DeleteSMS(context.Context, string) error
}

type LineSource interface {
	Topology(context.Context) (inventory.Topology, error)
}
type TransportAvailability interface {
	Available(context.Context, string) bool
}

// SendSMSCommand is the narrow typed boundary between the application and a
// modem-specific transport. It deliberately contains no AT/QMI text or device
// paths.
type SendSMSCommand struct {
	OperationID      string
	MessageID        string
	LineID           string
	PhysicalDeviceID string
	ModemFunctionID  string
	Destination      string
	Body             string
	Segments         []smscodec.Segment
}

type SendSMSResult struct {
	ProviderMessageID string
	State             string
	ErrorCode         string
}

const (
	SendStateAccepted    = "accepted"
	SendStateSent        = "sent"
	SendStateFailed      = "failed"
	SendStateUnconfirmed = "unconfirmed"
)

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
	LineID           string
	PhysicalDeviceID string
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

type SendRequest struct {
	OperationID string
	LineID      string
	Destination string
	Body        string
}

type SendResult struct {
	Message  sms.Message
	Replayed bool
}

type HistoryStats struct {
	TotalCount   int64
	Capacity     int64
	NearCapacity bool
}

type serialGate struct {
	token chan struct{}
}

type Service struct {
	repository Repository
	lines      LineSource
	sender     Sender
	inbox      Inbox
	reports    SubmitReportInbox
	random     io.Reader
	now        func() time.Time

	gatesMu       sync.Mutex
	gates         map[string]*serialGate
	availability  TransportAvailability
	hostVoWiFiSMS bool
}

func (service *Service) UseHostVoWiFiTransport(availability TransportAvailability) {
	if service != nil {
		service.hostVoWiFiSMS = true
		service.availability = availability
	}
}

func (service *Service) supportsSMS(line inventory.Line) bool {
	if service != nil && service.hostVoWiFiSMS {
		return line.Capabilities.HostVoWiFiAuth
	}
	return line.Capabilities.SMS
}

func NewService(ctx context.Context, repository Repository, lines LineSource, sender Sender, inboxes ...Inbox) (*Service, error) {
	if repository == nil || lines == nil {
		return nil, errors.New("messaging service dependencies are incomplete")
	}
	service := &Service{
		repository: repository,
		lines:      lines,
		sender:     sender,
		random:     rand.Reader,
		now:        time.Now,
		gates:      make(map[string]*serialGate),
	}
	if len(inboxes) != 0 {
		service.inbox = inboxes[0]
		service.reports, _ = inboxes[0].(SubmitReportInbox)
	}
	if _, err := repository.MarkQueuedOutboundSMSUnconfirmed(ctx, ErrorOutcomeUnknownAfterRestart, service.currentTime()); err != nil {
		return nil, fmt.Errorf("%w: reconcile interrupted outbound SMS: %v", ErrPersistence, err)
	}
	if _, err := repository.PruneInboundSMSFragments(ctx, service.currentTime().Add(-InboundFragmentRetention)); err != nil {
		return nil, fmt.Errorf("%w: prune expired inbound SMS fragments: %v", ErrPersistence, err)
	}
	return service, nil
}

func (service *Service) Send(ctx context.Context, request SendRequest) (SendResult, error) {
	request.OperationID = strings.TrimSpace(request.OperationID)
	request.LineID = strings.TrimSpace(request.LineID)
	request.Destination = strings.TrimSpace(request.Destination)
	if err := validateSendRequest(request); err != nil {
		return SendResult{}, err
	}
	if service == nil || service.repository == nil || service.lines == nil {
		return SendResult{}, ErrTransportUnavailable
	}
	if service.sender == nil {
		return SendResult{}, ErrTransportUnavailable
	}
	line, err := service.sendLine(ctx, request.LineID)
	if err != nil {
		return SendResult{}, err
	}
	if service.hostVoWiFiSMS && (service.availability == nil || !service.availability.Available(ctx, line.ID)) {
		return SendResult{}, ErrLineUnavailable
	}
	segments, err := smscodec.Encode(request.Body)
	if err != nil {
		return SendResult{}, ErrRequestInvalid
	}
	messageID, err := service.newMessageID()
	if err != nil {
		return SendResult{}, fmt.Errorf("%w: generate message id", ErrPersistence)
	}
	now := service.currentTime()
	message, replayed, err := service.repository.CreateOutboundSMS(ctx, sms.Message{
		ID: messageID, OperationID: request.OperationID, Direction: sms.DirectionOutbound,
		LineID: request.LineID, RemoteAddress: request.Destination, Body: request.Body,
		Status: sms.StatusQueued, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		if errors.Is(err, sms.ErrOperationConflict) {
			return SendResult{}, err
		}
		return SendResult{}, fmt.Errorf("%w: create outbound SMS: %v", ErrPersistence, err)
	}
	if replayed {
		return SendResult{Message: message, Replayed: true}, nil
	}

	gate := service.gate(line.ModemFunctionID)
	select {
	case gate.token <- struct{}{}:
		defer func() { <-gate.token }()
	case <-ctx.Done():
		failed, persistenceErr := service.markFailed(message.ID, "", ErrorCancelledBeforeDispatch)
		if persistenceErr != nil {
			return SendResult{}, persistenceErr
		}
		return SendResult{Message: failed}, nil
	}

	result, sendErr := service.sender.SendSMS(ctx, SendSMSCommand{
		OperationID: request.OperationID, MessageID: message.ID, LineID: line.ID,
		PhysicalDeviceID: line.PhysicalDeviceID, ModemFunctionID: line.ModemFunctionID,
		Destination: request.Destination, Body: request.Body, Segments: segments,
	})
	if sendErr != nil {
		errorCode := transportErrorCode(sendErr)
		if errorCode == ErrorSendOutcomeUnknown {
			unconfirmed, persistenceErr := service.markUnconfirmed(message.ID, "", errorCode)
			if persistenceErr != nil {
				return SendResult{}, persistenceErr
			}
			return SendResult{Message: unconfirmed}, nil
		}
		failed, persistenceErr := service.markFailed(message.ID, "", errorCode)
		if persistenceErr != nil {
			return SendResult{}, persistenceErr
		}
		return SendResult{Message: failed}, nil
	}
	if strings.TrimSpace(result.ProviderMessageID) == "" || len(result.ProviderMessageID) > 128 {
		failed, persistenceErr := service.markFailed(message.ID, "", ErrorTransportFailed)
		if persistenceErr != nil {
			return SendResult{}, persistenceErr
		}
		return SendResult{Message: failed}, nil
	}
	switch result.State {
	case "", SendStateSent:
		if result.ErrorCode != "" {
			unconfirmed, persistenceErr := service.markUnconfirmed(message.ID, result.ProviderMessageID, ErrorSendOutcomeUnknown)
			if persistenceErr != nil {
				return SendResult{}, persistenceErr
			}
			return SendResult{Message: unconfirmed}, nil
		}
		finalizeCtx, cancel := service.finalizeContext(ctx)
		defer cancel()
		completed, err := service.repository.MarkOutboundSMSSent(finalizeCtx, message.ID, result.ProviderMessageID, service.currentTime())
		if err != nil {
			return SendResult{}, fmt.Errorf("%w: persist outbound SMS success: %v", ErrPersistence, err)
		}
		return SendResult{Message: completed}, nil
	case SendStateAccepted:
		if result.ErrorCode != "" {
			result.ErrorCode = ErrorSendOutcomeUnknown
		} else {
			result.ErrorCode = ErrorAcceptedAwaitingReport
		}
	case SendStateUnconfirmed:
		if !errorCodePattern.MatchString(result.ErrorCode) {
			result.ErrorCode = ErrorSendOutcomeUnknown
		}
	case SendStateFailed:
		if !errorCodePattern.MatchString(result.ErrorCode) {
			result.ErrorCode = ErrorTransportFailed
		}
		failed, persistenceErr := service.markFailed(message.ID, result.ProviderMessageID, result.ErrorCode)
		if persistenceErr != nil {
			return SendResult{}, persistenceErr
		}
		return SendResult{Message: failed}, nil
	default:
		result.ErrorCode = ErrorSendOutcomeUnknown
	}
	unconfirmed, persistenceErr := service.markUnconfirmed(message.ID, result.ProviderMessageID, result.ErrorCode)
	if persistenceErr != nil {
		return SendResult{}, persistenceErr
	}
	return SendResult{Message: unconfirmed}, nil
}

func (service *Service) List(ctx context.Context, limit int) ([]sms.Message, error) {
	if service == nil || service.repository == nil {
		return nil, ErrPersistence
	}
	if limit < 1 || limit > 100 {
		return nil, ErrRequestInvalid
	}
	if service.inbox != nil || service.reports != nil {
		if _, err := service.SyncInbound(ctx); err != nil {
			return nil, err
		}
	}
	messages, err := service.repository.ListSMS(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("%w: list SMS: %v", ErrPersistence, err)
	}
	return messages, nil
}

func (service *Service) Stats(ctx context.Context) (HistoryStats, error) {
	if service == nil || service.repository == nil {
		return HistoryStats{}, ErrPersistence
	}
	count, err := service.repository.CountSMS(ctx)
	if err != nil {
		return HistoryStats{}, fmt.Errorf("%w: count SMS: %v", ErrPersistence, err)
	}
	return HistoryStats{TotalCount: count, Capacity: HistoryCapacity, NearCapacity: count >= HistoryCapacity*8/10}, nil
}

func (service *Service) Delete(ctx context.Context, messageID string) error {
	if service == nil || service.repository == nil || !operationIDPattern.MatchString(strings.TrimSpace(messageID)) {
		return ErrRequestInvalid
	}
	if err := service.repository.DeleteSMS(ctx, messageID); err != nil {
		if errors.Is(err, sms.ErrMessageNotFound) {
			return sms.ErrMessageNotFound
		}
		return fmt.Errorf("%w: delete SMS: %v", ErrPersistence, err)
	}
	return nil
}

func (service *Service) sendLine(ctx context.Context, lineID string) (inventory.Line, error) {
	topology, err := service.lines.Topology(ctx)
	if err != nil {
		return inventory.Line{}, fmt.Errorf("%w: %v", ErrInventoryUnavailable, err)
	}
	for _, line := range topology.Lines {
		if line.ID != lineID {
			continue
		}
		if line.State != inventory.LineReady {
			return inventory.Line{}, ErrLineUnavailable
		}
		if !service.supportsSMS(line) {
			return inventory.Line{}, ErrLineUnsupported
		}
		if line.ModemFunctionID == "" || line.PhysicalDeviceID == "" {
			return inventory.Line{}, ErrLineUnavailable
		}
		return line, nil
	}
	return inventory.Line{}, ErrLineNotFound
}

func (service *Service) gate(modemFunctionID string) *serialGate {
	service.gatesMu.Lock()
	defer service.gatesMu.Unlock()
	gate := service.gates[modemFunctionID]
	if gate == nil {
		gate = &serialGate{token: make(chan struct{}, 1)}
		service.gates[modemFunctionID] = gate
	}
	return gate
}

func (service *Service) markFailed(messageID, providerMessageID, errorCode string) (sms.Message, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	message, err := service.repository.MarkOutboundSMSFailed(ctx, messageID, providerMessageID, errorCode, service.currentTime())
	if err != nil {
		return sms.Message{}, fmt.Errorf("%w: persist outbound SMS failure: %v", ErrPersistence, err)
	}
	return message, nil
}

func (service *Service) markUnconfirmed(messageID, providerMessageID, errorCode string) (sms.Message, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	message, err := service.repository.MarkOutboundSMSUnconfirmed(ctx, messageID, providerMessageID, errorCode, service.currentTime())
	if err != nil {
		return sms.Message{}, fmt.Errorf("%w: persist unconfirmed outbound SMS: %v", ErrPersistence, err)
	}
	return message, nil
}

func (service *Service) finalizeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
}

func (service *Service) newMessageID() (string, error) {
	return service.newOpaqueID("msg_")
}

func (service *Service) newOpaqueID(prefix string) (string, error) {
	random := make([]byte, 16)
	if _, err := io.ReadFull(service.random, random); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(random), nil
}

func (service *Service) currentTime() time.Time {
	if service.now == nil {
		return time.Now().UTC()
	}
	return service.now().UTC()
}

func validateSendRequest(request SendRequest) error {
	if !operationIDPattern.MatchString(request.OperationID) || !lineIDPattern.MatchString(request.LineID) ||
		!destinationPattern.MatchString(request.Destination) || strings.TrimSpace(request.Body) == "" ||
		!utf8.ValidString(request.Body) || utf8.RuneCountInString(request.Body) > 1600 || len(request.Body) > 6400 {
		return ErrRequestInvalid
	}
	return nil
}

func transportErrorCode(err error) string {
	var transportErr *TransportError
	if errors.As(err, &transportErr) && errorCodePattern.MatchString(transportErr.Code) {
		return transportErr.Code
	}
	return ErrorTransportFailed
}
