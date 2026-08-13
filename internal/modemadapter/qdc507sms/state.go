package qdc507sms

import (
	"context"
	"crypto/sha256"
	"errors"
	"regexp"
	"sort"
	"sync"

	"github.com/leonfox28/simplus/internal/agentapi"
)

var ErrStateConflict = errors.New("QDC507 SMS state conflicts with an existing record")
var subscriptionKeyPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type operationKind string
type operationState string

const (
	operationSend        operationKind  = "send"
	operationAcknowledge operationKind  = "acknowledge"
	operationAccepted    operationState = "accepted"
	operationSucceeded   operationState = "succeeded"
	operationUnknown     operationState = "outcome-unknown"
)

type operationRecord struct {
	OperationID     string
	SubscriptionKey string
	Kind            operationKind
	RequestDigest   [sha256.Size]byte
	State           operationState
	Submission      agentapi.SMSSubmission
}

// StateStore is deliberately scoped to QDC507 SMS recovery. A production
// implementation must make each method atomic and retain records across Agent
// process restarts; it is not a second generic command ledger.
type StateStore interface {
	PutInbound(context.Context, InboundRecord) (InboundRecord, bool, error)
	FindInbound(context.Context, string, string) (InboundRecord, bool, error)
	ListInbound(context.Context, string) ([]InboundRecord, error)
	UpdateInbound(context.Context, InboundRecord) error

	PutOperation(context.Context, operationRecord) (operationRecord, bool, error)
	UpdateOperation(context.Context, operationRecord) error
	DeleteOperation(context.Context, operationRecord) error
}

// MemoryStateStore is useful for fixture and adapter-lifecycle tests. Keeping
// the same store while constructing a new Adapter models restart recovery, but
// this implementation alone is not process-durable.
type MemoryStateStore struct {
	mu         sync.Mutex
	inbound    map[string]InboundRecord
	operations map[string]operationRecord
}

var _ StateStore = (*MemoryStateStore)(nil)

func NewMemoryStateStore() *MemoryStateStore {
	return &MemoryStateStore{inbound: make(map[string]InboundRecord), operations: make(map[string]operationRecord)}
}

func (store *MemoryStateStore) PutInbound(ctx context.Context, record InboundRecord) (InboundRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return InboundRecord{}, false, err
	}
	if err := validInboundRecord(record); err != nil {
		return InboundRecord{}, false, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if existing, found := store.inbound[inboundKey(record.SubscriptionKey, record.MessageID)]; found {
		if !sameInboundIdentity(existing, record) {
			return InboundRecord{}, false, ErrStateConflict
		}
		return cloneInbound(existing), true, nil
	}
	store.inbound[inboundKey(record.SubscriptionKey, record.MessageID)] = cloneInbound(record)
	return cloneInbound(record), false, nil
}

func (store *MemoryStateStore) FindInbound(ctx context.Context, subscriptionKey, messageID string) (InboundRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return InboundRecord{}, false, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	record, found := store.inbound[inboundKey(subscriptionKey, messageID)]
	return cloneInbound(record), found, nil
}

func (store *MemoryStateStore) ListInbound(ctx context.Context, subscriptionKey string) ([]InboundRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	records := make([]InboundRecord, 0)
	for _, record := range store.inbound {
		if record.SubscriptionKey == subscriptionKey && !record.Acknowledged {
			records = append(records, cloneInbound(record))
		}
	}
	sort.Slice(records, func(left, right int) bool {
		if records[left].ReceivedAt.Equal(records[right].ReceivedAt) {
			return records[left].MessageID < records[right].MessageID
		}
		return records[left].ReceivedAt.Before(records[right].ReceivedAt)
	})
	return records, nil
}

func (store *MemoryStateStore) UpdateInbound(ctx context.Context, record InboundRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validInboundRecord(record); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	key := inboundKey(record.SubscriptionKey, record.MessageID)
	existing, found := store.inbound[key]
	if !found || !sameInboundIdentity(existing, record) || !validInboundProgress(existing, record) {
		return ErrStateConflict
	}
	store.inbound[key] = cloneInbound(record)
	return nil
}

func (store *MemoryStateStore) PutOperation(ctx context.Context, record operationRecord) (operationRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return operationRecord{}, false, err
	}
	if !validOperation(record) || record.State != operationAccepted {
		return operationRecord{}, false, errors.New("invalid QDC507 SMS operation record")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if existing, found := store.operations[record.OperationID]; found {
		return existing, true, nil
	}
	store.operations[record.OperationID] = record
	return record, false, nil
}

func (store *MemoryStateStore) UpdateOperation(ctx context.Context, record operationRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validOperation(record) || (record.State != operationSucceeded && record.State != operationUnknown) {
		return errors.New("invalid terminal QDC507 SMS operation record")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	existing, found := store.operations[record.OperationID]
	if !found || existing.SubscriptionKey != record.SubscriptionKey || existing.Kind != record.Kind || existing.RequestDigest != record.RequestDigest ||
		(existing.State != operationAccepted && existing.State != record.State) {
		return ErrStateConflict
	}
	store.operations[record.OperationID] = record
	return nil
}

func (store *MemoryStateStore) DeleteOperation(ctx context.Context, record operationRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	existing, found := store.operations[record.OperationID]
	if !found || existing.SubscriptionKey != record.SubscriptionKey || existing.Kind != record.Kind || existing.RequestDigest != record.RequestDigest || existing.State != operationAccepted {
		return ErrStateConflict
	}
	delete(store.operations, record.OperationID)
	return nil
}

func validInboundRecord(record InboundRecord) error {
	if record.MessageID == "" || !subscriptionKeyPattern.MatchString(record.SubscriptionKey) || record.Sender == "" || record.Body == "" || record.ReceivedAt.IsZero() || len(record.Segments) == 0 {
		return errors.New("invalid QDC507 inbound SMS state")
	}
	seen := make(map[int]struct{}, len(record.Segments))
	for _, segment := range record.Segments {
		if segment.Index < 0 || segment.Index > maxStorageIndex || segment.DeleteStarted {
			return errors.New("invalid QDC507 inbound SMS storage index")
		}
		if _, duplicate := seen[segment.Index]; duplicate {
			return errors.New("duplicate QDC507 inbound SMS storage index")
		}
		seen[segment.Index] = struct{}{}
	}
	if record.Acknowledged {
		for _, segment := range record.Segments {
			if !segment.Deleted {
				return errors.New("acknowledged QDC507 SMS retains an undeleted segment")
			}
		}
	}
	return nil
}

func sameInboundIdentity(left, right InboundRecord) bool {
	if left.MessageID != right.MessageID || left.SubscriptionKey != right.SubscriptionKey || left.Sender != right.Sender || left.Body != right.Body ||
		!left.ReceivedAt.Equal(right.ReceivedAt) || len(left.Segments) != len(right.Segments) {
		return false
	}
	for index := range left.Segments {
		if left.Segments[index].Index != right.Segments[index].Index || left.Segments[index].PDUDigest != right.Segments[index].PDUDigest {
			return false
		}
	}
	return true
}

func validInboundProgress(before, after InboundRecord) bool {
	if before.Acknowledged && !after.Acknowledged {
		return false
	}
	for index := range before.Segments {
		if before.Segments[index].Deleted && !after.Segments[index].Deleted {
			return false
		}
	}
	return true
}

func validOperation(record operationRecord) bool {
	return record.OperationID != "" && subscriptionKeyPattern.MatchString(record.SubscriptionKey) && (record.Kind == operationSend || record.Kind == operationAcknowledge)
}

func inboundKey(subscriptionKey, messageID string) string {
	return subscriptionKey + "\x00" + messageID
}

func cloneInbound(record InboundRecord) InboundRecord {
	record.Segments = append([]InboundSegment(nil), record.Segments...)
	return record
}
