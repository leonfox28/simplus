package qdc507sms

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/modemadapter"
)

var ErrStorageIndexReused = errors.New("QDC507 SMS storage index now contains a different message")

// PDUDriver is the model-private boundary below the complete SMSAdapter. It
// contains no caller-provided AT commands or device paths.
type PDUDriver interface {
	List(context.Context, agentapi.DeviceReport) ([]StoredPDU, error)
	Read(context.Context, agentapi.DeviceReport, int) (StoredPDU, error)
	Delete(context.Context, agentapi.DeviceReport, int) error
	Send(context.Context, agentapi.DeviceReport, string, string) (SendResult, error)
}

// Adapter completes the common SMSAdapter contract for fixture validation.
// It is intentionally not part of modemadapter.DefaultRegistry until the
// durable store is wired, a real tty transport exists, and authorized HIL has
// been accepted.
type Adapter struct {
	modemadapter.QDC507
	driver PDUDriver
	store  StateStore
	now    func() time.Time
}

var (
	_ PDUDriver               = (*Driver)(nil)
	_ modemadapter.SMSAdapter = (*Adapter)(nil)
)

func NewAdapter(driver PDUDriver, store StateStore) (*Adapter, error) {
	if driver == nil || store == nil {
		return nil, errors.New("QDC507 SMS adapter dependencies are incomplete")
	}
	return &Adapter{driver: driver, store: store, now: time.Now}, nil
}

func (adapter *Adapter) ListSMS(ctx context.Context, device agentapi.DeviceReport) ([]agentapi.SMSMessageReference, error) {
	if err := adapter.ready(device); err != nil {
		return nil, err
	}
	stored, err := adapter.driver.List(ctx, device)
	if err != nil {
		return nil, err
	}
	assembled, err := assembleInbound(device.ID, stored)
	if err != nil {
		return nil, err
	}
	for _, message := range assembled {
		if _, _, err := adapter.store.PutInbound(ctx, message); err != nil {
			return nil, fmt.Errorf("persist QDC507 inbound SMS: %w", err)
		}
	}
	pending, err := adapter.store.ListInbound(ctx, device.ID)
	if err != nil {
		return nil, fmt.Errorf("list QDC507 inbound SMS state: %w", err)
	}
	references := make([]agentapi.SMSMessageReference, 0, len(pending))
	for _, message := range pending {
		references = append(references, agentapi.SMSMessageReference{
			MessageID: message.MessageID, DeviceID: message.DeviceID, Sender: message.Sender, ReceivedAt: message.ReceivedAt,
		})
	}
	return references, nil
}

func (adapter *Adapter) ReadSMS(ctx context.Context, device agentapi.DeviceReport, messageID string) (agentapi.SMSStoredMessage, error) {
	if err := adapter.ready(device); err != nil {
		return agentapi.SMSStoredMessage{}, err
	}
	record, found, err := adapter.store.FindInbound(ctx, device.ID, messageID)
	if err != nil {
		return agentapi.SMSStoredMessage{}, fmt.Errorf("read QDC507 inbound SMS state: %w", err)
	}
	if !found || record.Acknowledged {
		return agentapi.SMSStoredMessage{}, agentapi.ErrSMSMessageNotFound
	}
	return agentapi.SMSStoredMessage{
		MessageID: record.MessageID, DeviceID: record.DeviceID, Sender: record.Sender,
		Body: record.Body, ReceivedAt: record.ReceivedAt,
	}, nil
}

func (adapter *Adapter) SendSMS(ctx context.Context, device agentapi.DeviceReport, request agentapi.SMSSendRequest) (agentapi.SMSSubmission, error) {
	if err := adapter.ready(device); err != nil {
		return agentapi.SMSSubmission{}, err
	}
	if request.DeviceID != device.ID {
		return agentapi.SMSSubmission{}, agentapi.ErrSMSRequestInvalid
	}
	digest := digestFields(device.ID, request.Destination, request.Body)
	accepted := operationRecord{
		OperationID: request.OperationID, Kind: operationSend, RequestDigest: digest, State: operationAccepted,
	}
	existing, replayed, err := adapter.store.PutOperation(ctx, accepted)
	if err != nil {
		return agentapi.SMSSubmission{}, fmt.Errorf("persist QDC507 SMS send operation: %w", err)
	}
	if replayed {
		if existing.Kind != operationSend || existing.RequestDigest != digest {
			return agentapi.SMSSubmission{}, agentapi.ErrSMSOperationConflict
		}
		switch existing.State {
		case operationSucceeded:
			return existing.Submission, nil
		case operationAccepted, operationUnknown:
			return agentapi.SMSSubmission{}, agentapi.ErrSMSOutcomeUnknown
		default:
			return agentapi.SMSSubmission{}, ErrStateConflict
		}
	}

	result, sendErr := adapter.driver.Send(ctx, device, request.Destination, request.Body)
	if sendErr != nil {
		var partial *SendFailure
		uncertain := errors.Is(sendErr, ErrSendOutcomeUnknown) || (errors.As(sendErr, &partial) && partial.CompletedParts > 0)
		if uncertain {
			accepted.State = operationUnknown
			if err := adapter.store.UpdateOperation(context.WithoutCancel(ctx), accepted); err != nil {
				return agentapi.SMSSubmission{}, errors.Join(agentapi.ErrSMSOutcomeUnknown, sendErr, err)
			}
			return agentapi.SMSSubmission{}, errors.Join(agentapi.ErrSMSOutcomeUnknown, sendErr)
		}
		if err := adapter.store.DeleteOperation(context.WithoutCancel(ctx), accepted); err != nil {
			return agentapi.SMSSubmission{}, errors.Join(agentapi.ErrSMSOutcomeUnknown, sendErr, err)
		}
		return agentapi.SMSSubmission{}, sendErr
	}
	if !validSendResult(result) {
		return agentapi.SMSSubmission{}, adapter.terminalSendFailure(ctx, accepted, errors.New("QDC507 send returned invalid submitted parts"))
	}
	submission := agentapi.SMSSubmission{
		OperationID: request.OperationID,
		MessageID:   outboundMessageID(request.OperationID, result),
		SubmittedAt: adapter.currentTime(),
	}
	accepted.State = operationSucceeded
	accepted.Submission = submission
	if err := adapter.store.UpdateOperation(context.WithoutCancel(ctx), accepted); err != nil {
		return agentapi.SMSSubmission{}, errors.Join(agentapi.ErrSMSOutcomeUnknown, err)
	}
	return submission, nil
}

func (adapter *Adapter) AcknowledgeSMS(ctx context.Context, device agentapi.DeviceReport, request agentapi.SMSAcknowledgeRequest) (bool, error) {
	if err := adapter.ready(device); err != nil {
		return false, err
	}
	if request.DeviceID != device.ID {
		return false, agentapi.ErrSMSRequestInvalid
	}
	digest := digestFields(device.ID, request.MessageID)
	accepted := operationRecord{
		OperationID: request.OperationID, Kind: operationAcknowledge, RequestDigest: digest, State: operationAccepted,
	}
	existing, replayed, err := adapter.store.PutOperation(ctx, accepted)
	if err != nil {
		return false, fmt.Errorf("persist QDC507 SMS acknowledge operation: %w", err)
	}
	if replayed {
		if existing.Kind != operationAcknowledge || existing.RequestDigest != digest {
			return false, agentapi.ErrSMSOperationConflict
		}
		if existing.State == operationSucceeded {
			return true, nil
		}
		if existing.State != operationAccepted {
			return false, ErrStateConflict
		}
	}

	record, found, err := adapter.store.FindInbound(ctx, device.ID, request.MessageID)
	if err != nil {
		return false, fmt.Errorf("read QDC507 SMS acknowledge state: %w", err)
	}
	if !found {
		return false, adapter.abandonAcknowledge(ctx, accepted, agentapi.ErrSMSMessageNotFound)
	}
	if record.Acknowledged {
		accepted.State = operationSucceeded
		if err := adapter.store.UpdateOperation(context.WithoutCancel(ctx), accepted); err != nil {
			return false, fmt.Errorf("complete QDC507 SMS acknowledge replay: %w", err)
		}
		return true, nil
	}

	for index := range record.Segments {
		if record.Segments[index].Deleted {
			continue
		}
		deleted, err := adapter.deleteInboundSegment(ctx, device, record.Segments[index])
		if err != nil {
			return false, err
		}
		if !deleted {
			return false, errors.New("QDC507 SMS segment deletion was not confirmed")
		}
		record.Segments[index].Deleted = true
		if err := adapter.store.UpdateInbound(context.WithoutCancel(ctx), record); err != nil {
			return false, fmt.Errorf("persist QDC507 SMS segment acknowledgement: %w", err)
		}
	}
	record.Acknowledged = true
	if err := adapter.store.UpdateInbound(context.WithoutCancel(ctx), record); err != nil {
		return false, fmt.Errorf("persist QDC507 SMS acknowledgement: %w", err)
	}
	accepted.State = operationSucceeded
	if err := adapter.store.UpdateOperation(context.WithoutCancel(ctx), accepted); err != nil {
		return false, fmt.Errorf("complete QDC507 SMS acknowledge operation: %w", err)
	}
	return true, nil
}

func (adapter *Adapter) deleteInboundSegment(ctx context.Context, device agentapi.DeviceReport, segment InboundSegment) (bool, error) {
	current, err := adapter.driver.Read(ctx, device, segment.Index)
	if errors.Is(err, agentapi.ErrSMSMessageNotFound) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("read QDC507 SMS before acknowledgement: %w", err)
	}
	if current.Index != segment.Index || sha256.Sum256(current.PDU) != segment.PDUDigest {
		return false, ErrStorageIndexReused
	}
	deleteErr := adapter.driver.Delete(ctx, device, segment.Index)
	if deleteErr == nil || errors.Is(deleteErr, agentapi.ErrSMSMessageNotFound) {
		return true, nil
	}
	// A lost delete response is reconciled once by reading the fixed index. A
	// still-matching PDU is left for an explicit retry; a different PDU is
	// never deleted.
	reconciled, readErr := adapter.driver.Read(ctx, device, segment.Index)
	if errors.Is(readErr, agentapi.ErrSMSMessageNotFound) {
		return true, nil
	}
	if readErr != nil {
		return false, errors.Join(deleteErr, fmt.Errorf("reconcile QDC507 SMS deletion: %w", readErr))
	}
	if reconciled.Index != segment.Index || sha256.Sum256(reconciled.PDU) != segment.PDUDigest {
		return false, errors.Join(ErrStorageIndexReused, deleteErr)
	}
	return false, deleteErr
}

func (adapter *Adapter) terminalSendFailure(ctx context.Context, accepted operationRecord, cause error) error {
	accepted.State = operationUnknown
	if err := adapter.store.UpdateOperation(context.WithoutCancel(ctx), accepted); err != nil {
		return errors.Join(agentapi.ErrSMSOutcomeUnknown, cause, err)
	}
	return errors.Join(agentapi.ErrSMSOutcomeUnknown, cause)
}

func (adapter *Adapter) abandonAcknowledge(ctx context.Context, accepted operationRecord, cause error) error {
	if err := adapter.store.DeleteOperation(context.WithoutCancel(ctx), accepted); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func (adapter *Adapter) ready(device agentapi.DeviceReport) error {
	if adapter == nil || adapter.driver == nil || adapter.store == nil || device.ID == "" || device.Profile != agentapi.ProfileQDC507 {
		return agentapi.ErrSMSUnsupported
	}
	return nil
}

func (adapter *Adapter) currentTime() time.Time {
	if adapter.now == nil {
		return time.Now().UTC()
	}
	return adapter.now().UTC()
}

func digestFields(fields ...string) [sha256.Size]byte {
	digest := sha256.New()
	for _, field := range fields {
		writeHashField(digest, []byte(field))
	}
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result
}

func outboundMessageID(operationID string, result SendResult) string {
	parts := append([]PartSubmission(nil), result.Parts...)
	sort.Slice(parts, func(left, right int) bool { return parts[left].Part < parts[right].Part })
	digest := sha256.New()
	writeHashField(digest, []byte(operationID))
	for _, part := range parts {
		writeHashField(digest, []byte(fmt.Sprintf("%d/%d/%d", part.Part, part.Total, part.MessageReference)))
	}
	return "qdc507-out-" + base64.RawURLEncoding.EncodeToString(digest.Sum(nil))
}

func validSendResult(result SendResult) bool {
	if len(result.Parts) == 0 || len(result.Parts) > 255 {
		return false
	}
	total := result.Parts[0].Total
	if total != len(result.Parts) {
		return false
	}
	seen := make(map[int]struct{}, len(result.Parts))
	for _, part := range result.Parts {
		if part.Total != total || part.Part < 1 || part.Part > total || part.MessageReference < 0 || part.MessageReference > 255 {
			return false
		}
		if _, duplicate := seen[part.Part]; duplicate {
			return false
		}
		seen[part.Part] = struct{}{}
	}
	return true
}
