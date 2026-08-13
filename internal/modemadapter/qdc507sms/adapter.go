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
	modemadapter.QDC507SMS
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

func (adapter *Adapter) ListSMS(ctx context.Context, target modemadapter.SMSRuntimeTarget) ([]agentapi.SMSMessageReference, error) {
	return listSMS(ctx, adapter.driver, adapter.store, target)
}

func listSMS(ctx context.Context, driver PDUDriver, store StateStore, target modemadapter.SMSRuntimeTarget) ([]agentapi.SMSMessageReference, error) {
	pending, err := collectInbound(ctx, driver, store, target)
	if err != nil {
		return nil, err
	}
	return inboundReferences(target.Device.ID, pending), nil
}

func collectInbound(ctx context.Context, driver PDUDriver, store StateStore, target modemadapter.SMSRuntimeTarget) ([]InboundRecord, error) {
	_, pending, err := collectInboundWithStored(ctx, driver, store, target)
	return pending, err
}

func collectInboundWithStored(ctx context.Context, driver PDUDriver, store StateStore, target modemadapter.SMSRuntimeTarget) ([]StoredPDU, []InboundRecord, error) {
	if err := readyAdapter(driver, store, target); err != nil {
		return nil, nil, err
	}
	stored, err := driver.List(ctx, target.Device)
	if err != nil {
		return nil, nil, err
	}
	assembled, err := assembleInbound(target.SubscriptionKey, stored)
	if err != nil {
		return nil, nil, err
	}
	for _, message := range assembled {
		if _, _, err := store.PutInbound(ctx, message); err != nil {
			return nil, nil, fmt.Errorf("persist QDC507 inbound SMS: %w", err)
		}
	}
	pending, err := store.ListInbound(ctx, target.SubscriptionKey)
	if err != nil {
		return nil, nil, fmt.Errorf("list QDC507 inbound SMS state: %w", err)
	}
	return stored, pending, nil
}

func inboundReferences(deviceID string, pending []InboundRecord) []agentapi.SMSMessageReference {
	references := make([]agentapi.SMSMessageReference, 0, len(pending))
	for _, message := range pending {
		references = append(references, agentapi.SMSMessageReference{
			MessageID: message.MessageID, DeviceID: deviceID, Sender: message.Sender, ReceivedAt: message.ReceivedAt,
		})
	}
	return references
}

func (adapter *Adapter) ReadSMS(ctx context.Context, target modemadapter.SMSRuntimeTarget, messageID string) (agentapi.SMSStoredMessage, error) {
	return readSMS(ctx, adapter.driver, adapter.store, target, messageID)
}

func readSMS(ctx context.Context, driver PDUDriver, store StateStore, target modemadapter.SMSRuntimeTarget, messageID string) (agentapi.SMSStoredMessage, error) {
	if err := readyAdapter(driver, store, target); err != nil {
		return agentapi.SMSStoredMessage{}, err
	}
	record, found, err := store.FindInbound(ctx, target.SubscriptionKey, messageID)
	if err != nil {
		return agentapi.SMSStoredMessage{}, fmt.Errorf("read QDC507 inbound SMS state: %w", err)
	}
	if !found || record.Acknowledged {
		return agentapi.SMSStoredMessage{}, agentapi.ErrSMSMessageNotFound
	}
	return agentapi.SMSStoredMessage{
		MessageID: record.MessageID, DeviceID: target.Device.ID, Sender: record.Sender,
		Body: record.Body, ReceivedAt: record.ReceivedAt,
	}, nil
}

func (adapter *Adapter) SendSMS(ctx context.Context, target modemadapter.SMSRuntimeTarget, request agentapi.SMSSendRequest) (agentapi.SMSSubmission, error) {
	if adapter == nil {
		return agentapi.SMSSubmission{}, agentapi.ErrSMSUnsupported
	}
	return sendSMS(ctx, adapter.driver, adapter.store, adapter.currentTime, target, request)
}

func sendSMS(
	ctx context.Context,
	driver PDUDriver,
	store StateStore,
	now func() time.Time,
	target modemadapter.SMSRuntimeTarget,
	request agentapi.SMSSendRequest,
) (agentapi.SMSSubmission, error) {
	if err := readyAdapter(driver, store, target); err != nil {
		return agentapi.SMSSubmission{}, err
	}
	if request.DeviceID != target.Device.ID {
		return agentapi.SMSSubmission{}, agentapi.ErrSMSRequestInvalid
	}
	digest := digestFields(target.SubscriptionKey, request.Destination, request.Body)
	accepted := operationRecord{
		OperationID: request.OperationID, SubscriptionKey: target.SubscriptionKey, Kind: operationSend, RequestDigest: digest, State: operationAccepted,
	}
	existing, replayed, err := store.PutOperation(ctx, accepted)
	if err != nil {
		return agentapi.SMSSubmission{}, fmt.Errorf("persist QDC507 SMS send operation: %w", err)
	}
	if replayed {
		if existing.SubscriptionKey != target.SubscriptionKey || existing.Kind != operationSend || existing.RequestDigest != digest {
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

	result, sendErr := driver.Send(ctx, target.Device, request.Destination, request.Body)
	if sendErr != nil {
		var partial *SendFailure
		uncertain := errors.Is(sendErr, ErrSendOutcomeUnknown) || (errors.As(sendErr, &partial) && partial.CompletedParts > 0)
		if uncertain {
			accepted.State = operationUnknown
			if err := store.UpdateOperation(context.WithoutCancel(ctx), accepted); err != nil {
				return agentapi.SMSSubmission{}, errors.Join(agentapi.ErrSMSOutcomeUnknown, sendErr, err)
			}
			return agentapi.SMSSubmission{}, errors.Join(agentapi.ErrSMSOutcomeUnknown, sendErr)
		}
		if err := store.DeleteOperation(context.WithoutCancel(ctx), accepted); err != nil {
			return agentapi.SMSSubmission{}, errors.Join(agentapi.ErrSMSOutcomeUnknown, sendErr, err)
		}
		return agentapi.SMSSubmission{}, sendErr
	}
	if !validSendResult(result) {
		return agentapi.SMSSubmission{}, terminalSendFailure(ctx, store, accepted, errors.New("QDC507 send returned invalid submitted parts"))
	}
	submission := agentapi.SMSSubmission{
		OperationID: request.OperationID,
		MessageID:   outboundMessageID(request.OperationID, result),
		SubmittedAt: currentTime(now),
	}
	accepted.State = operationSucceeded
	accepted.Submission = submission
	if err := store.UpdateOperation(context.WithoutCancel(ctx), accepted); err != nil {
		return agentapi.SMSSubmission{}, errors.Join(agentapi.ErrSMSOutcomeUnknown, err)
	}
	return submission, nil
}

func (adapter *Adapter) AcknowledgeSMS(ctx context.Context, target modemadapter.SMSRuntimeTarget, request agentapi.SMSAcknowledgeRequest) (bool, error) {
	return acknowledgeSMS(ctx, adapter.driver, adapter.store, target, request)
}

func acknowledgeSMS(ctx context.Context, driver PDUDriver, store StateStore, target modemadapter.SMSRuntimeTarget, request agentapi.SMSAcknowledgeRequest) (bool, error) {
	if err := readyAdapter(driver, store, target); err != nil {
		return false, err
	}
	if request.DeviceID != target.Device.ID {
		return false, agentapi.ErrSMSRequestInvalid
	}
	digest := digestFields(target.SubscriptionKey, request.MessageID)
	accepted := operationRecord{
		OperationID: request.OperationID, SubscriptionKey: target.SubscriptionKey, Kind: operationAcknowledge, RequestDigest: digest, State: operationAccepted,
	}
	existing, replayed, err := store.PutOperation(ctx, accepted)
	if err != nil {
		return false, fmt.Errorf("persist QDC507 SMS acknowledge operation: %w", err)
	}
	if replayed {
		if existing.SubscriptionKey != target.SubscriptionKey || existing.Kind != operationAcknowledge || existing.RequestDigest != digest {
			return false, agentapi.ErrSMSOperationConflict
		}
		if existing.State == operationSucceeded {
			return true, nil
		}
		if existing.State != operationAccepted {
			return false, ErrStateConflict
		}
	}

	record, found, err := store.FindInbound(ctx, target.SubscriptionKey, request.MessageID)
	if err != nil {
		return false, fmt.Errorf("read QDC507 SMS acknowledge state: %w", err)
	}
	if !found {
		return false, abandonAcknowledge(ctx, store, accepted, agentapi.ErrSMSMessageNotFound)
	}
	if record.Acknowledged {
		accepted.State = operationSucceeded
		if err := store.UpdateOperation(context.WithoutCancel(ctx), accepted); err != nil {
			return false, fmt.Errorf("complete QDC507 SMS acknowledge replay: %w", err)
		}
		return true, nil
	}

	for index := range record.Segments {
		if record.Segments[index].Deleted {
			continue
		}
		deleted, err := deleteInboundSegment(ctx, driver, target.Device, record.Segments[index])
		if err != nil {
			return false, err
		}
		if !deleted {
			return false, errors.New("QDC507 SMS segment deletion was not confirmed")
		}
		record.Segments[index].Deleted = true
		if err := store.UpdateInbound(context.WithoutCancel(ctx), record); err != nil {
			return false, fmt.Errorf("persist QDC507 SMS segment acknowledgement: %w", err)
		}
	}
	record.Acknowledged = true
	if err := store.UpdateInbound(context.WithoutCancel(ctx), record); err != nil {
		return false, fmt.Errorf("persist QDC507 SMS acknowledgement: %w", err)
	}
	accepted.State = operationSucceeded
	if err := store.UpdateOperation(context.WithoutCancel(ctx), accepted); err != nil {
		return false, fmt.Errorf("complete QDC507 SMS acknowledge operation: %w", err)
	}
	return true, nil
}

func deleteInboundSegment(ctx context.Context, driver PDUDriver, device agentapi.DeviceReport, segment InboundSegment) (bool, error) {
	current, err := driver.Read(ctx, device, segment.Index)
	if errors.Is(err, agentapi.ErrSMSMessageNotFound) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("read QDC507 SMS before acknowledgement: %w", err)
	}
	if current.Index != segment.Index || sha256.Sum256(current.PDU) != segment.PDUDigest {
		return false, ErrStorageIndexReused
	}
	deleteErr := driver.Delete(ctx, device, segment.Index)
	if deleteErr == nil || errors.Is(deleteErr, agentapi.ErrSMSMessageNotFound) {
		return true, nil
	}
	// A lost delete response is reconciled once by reading the fixed index. A
	// still-matching PDU is left for an explicit retry; a different PDU is
	// never deleted.
	reconciled, readErr := driver.Read(ctx, device, segment.Index)
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

func terminalSendFailure(ctx context.Context, store StateStore, accepted operationRecord, cause error) error {
	accepted.State = operationUnknown
	if err := store.UpdateOperation(context.WithoutCancel(ctx), accepted); err != nil {
		return errors.Join(agentapi.ErrSMSOutcomeUnknown, cause, err)
	}
	return errors.Join(agentapi.ErrSMSOutcomeUnknown, cause)
}

func abandonAcknowledge(ctx context.Context, store StateStore, accepted operationRecord, cause error) error {
	if err := store.DeleteOperation(context.WithoutCancel(ctx), accepted); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func readyAdapter(driver PDUDriver, store StateStore, target modemadapter.SMSRuntimeTarget) error {
	if driver == nil || store == nil || target.Device.ID == "" || target.Device.Profile != agentapi.ProfileQDC507 ||
		!subscriptionKeyPattern.MatchString(target.SubscriptionKey) {
		return agentapi.ErrSMSUnsupported
	}
	return nil
}

func (adapter *Adapter) currentTime() time.Time {
	if adapter == nil {
		return time.Now().UTC()
	}
	return currentTime(adapter.now)
}

func currentTime(now func() time.Time) time.Time {
	if now == nil {
		return time.Now().UTC()
	}
	return now().UTC()
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
