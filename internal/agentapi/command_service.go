package agentapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sync"
	"time"
)

var (
	ErrCommandRequestInvalid = errors.New("Agent command request is invalid")
	ErrCommandAgentStale     = errors.New("Agent instance changed")
	ErrCommandSnapshotStale  = errors.New("Agent snapshot changed")
	ErrCommandDeviceStale    = errors.New("Agent device generation changed")
	ErrCommandUnsupported    = errors.New("Agent command is unsupported for the device")
	ErrCommandPersistence    = errors.New("Agent command outcome could not be persisted")
)

var commandIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type RadioEnsureOffExecution struct {
	Dispatched  bool
	Uncertain   bool
	Observation RadioEnsureOffObservation
	Error       *ProbeError
}

type RadioEnsureOffExecutor interface {
	EnsureRadioOff(context.Context, Snapshot, string) (RadioEnsureOffExecution, error)
}

type CommandService struct {
	monitor  *Monitor
	executor RadioEnsureOffExecutor
	store    *OutcomeStore
	now      func() time.Time

	mu sync.Mutex
}

func NewCommandService(monitor *Monitor, executor RadioEnsureOffExecutor, store *OutcomeStore) *CommandService {
	return &CommandService{monitor: monitor, executor: executor, store: store, now: time.Now}
}

func (service *CommandService) EnsureRadioOff(ctx context.Context, request RadioEnsureOffRequest) (RadioEnsureOffResponse, error) {
	if service == nil || service.monitor == nil || service.executor == nil || service.store == nil {
		return RadioEnsureOffResponse{}, errors.New("Agent command service is unavailable")
	}
	if err := validateRadioEnsureOffShape(request); err != nil {
		return RadioEnsureOffResponse{}, err
	}
	digest, err := radioEnsureOffDigest(request)
	if err != nil {
		return RadioEnsureOffResponse{}, ErrCommandRequestInvalid
	}
	service.mu.Lock()
	defer service.mu.Unlock()

	if existing, found, err := service.store.Find(ctx, request.OperationID); err != nil {
		return RadioEnsureOffResponse{}, fmt.Errorf("%w: %w", ErrCommandPersistence, err)
	} else if found {
		if existing.RequestDigest != digest {
			return RadioEnsureOffResponse{}, ErrOutcomeReplayConflict
		}
		if existing.Outcome.State == CommandOutcomeAccepted {
			existing, err = service.reconcile(ctx, existing)
			if err != nil {
				return RadioEnsureOffResponse{}, err
			}
		}
		return service.response(existing.Outcome), nil
	}

	snapshot, err := service.currentSnapshot(ctx)
	if err != nil {
		return RadioEnsureOffResponse{}, err
	}
	if err := validateRadioEnsureOffRequest(request, snapshot); err != nil {
		return RadioEnsureOffResponse{}, err
	}
	record, _, err := service.store.Accept(ctx, request, digest, service.currentTime())
	if err != nil {
		return RadioEnsureOffResponse{}, err
	}

	current := service.monitor.Snapshot()
	if !sameSnapshotFence(snapshot, current) {
		outcome := terminalCommandOutcome(record, service.currentTime(), CommandOutcomeFailed, ErrorHardwareChanged, ErrorLayerDevice, true, false,
			RadioEnsureOffObservation{RF: RFObservation{State: RFStateUnknown}})
		if err := service.persist(outcome); err != nil {
			return RadioEnsureOffResponse{}, err
		}
		return service.response(outcome), nil
	}

	execution, executionErr := service.executor.EnsureRadioOff(ctx, snapshot, request.DeviceID)
	outcome := service.outcomeFromExecution(record, execution, executionErr)
	after := service.monitor.Snapshot()
	if !sameSnapshotFence(snapshot, after) {
		state := CommandOutcomeFailed
		retryable := true
		if execution.Dispatched {
			state = CommandOutcomeUncertain
			retryable = false
		}
		outcome = terminalCommandOutcome(record, service.currentTime(), state, ErrorHardwareChanged, ErrorLayerDevice, retryable, false, execution.Observation)
	}
	if err := service.persist(outcome); err != nil {
		return RadioEnsureOffResponse{}, err
	}
	return service.response(outcome), nil
}

func (service *CommandService) ReconcilePending(ctx context.Context) (int, error) {
	if service == nil || service.monitor == nil || service.store == nil {
		return 0, errors.New("Agent command service is unavailable")
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	records, err := service.store.Pending(ctx)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrCommandPersistence, err)
	}
	for _, record := range records {
		if _, err := service.reconcile(ctx, record); err != nil {
			return 0, err
		}
	}
	return len(records), nil
}

func (service *CommandService) reconcile(ctx context.Context, record outcomeRecord) (outcomeRecord, error) {
	observation := RadioEnsureOffObservation{RF: RFObservation{State: RFStateUnknown}}
	state := CommandOutcomeUncertain
	code := ErrorRadioOffOutcomeUncertain
	layer := ErrorLayerRadio

	snapshot, snapshotErr := service.currentSnapshot(ctx)
	if snapshotErr == nil && snapshot.Revision == record.Request.SnapshotRevision && snapshotHasQDC507(snapshot, record.Request.DeviceID) {
		probe, probeErr := service.monitor.Probe(ctx, []string{record.Request.DeviceID})
		if probeErr == nil && len(probe.Devices) == 1 {
			device := probe.Devices[0]
			observation = RadioEnsureOffObservation{RF: device.RF, ActiveCallCount: device.ActiveCallCount}
			if device.State == ProbeStateComplete && device.RF.State == RFStateOff && device.ActiveCallCount != nil && *device.ActiveCallCount == 0 {
				state = CommandOutcomeSucceeded
				code = OutcomeCodeRadioOffConfirmed
				layer = ""
			}
		}
	} else if snapshotErr == nil {
		code = ErrorHardwareChanged
		layer = ErrorLayerDevice
	}
	outcome := terminalCommandOutcome(record, service.currentTime(), state, code, layer, false, true, observation)
	if err := service.persist(outcome); err != nil {
		return outcomeRecord{}, err
	}
	record.Outcome = outcome
	return record, nil
}

func (service *CommandService) outcomeFromExecution(record outcomeRecord, execution RadioEnsureOffExecution, executionErr error) CommandOutcome {
	observation := normalizeRadioEnsureOffObservation(execution.Observation)
	switch {
	case executionErr != nil:
		return terminalCommandOutcome(record, service.currentTime(), CommandOutcomeFailed, ErrorHardwareChanged, ErrorLayerDevice, true, false, observation)
	case execution.Error == nil && observation.RF.State == RFStateOff && observation.ActiveCallCount != nil && *observation.ActiveCallCount == 0:
		return terminalCommandOutcome(record, service.currentTime(), CommandOutcomeSucceeded, OutcomeCodeRadioOffConfirmed, "", false, false, observation)
	case execution.Error == nil:
		return terminalCommandOutcome(record, service.currentTime(), CommandOutcomeFailed, ErrorRadioOffNotConfirmed, ErrorLayerRadio, true, false, observation)
	case execution.Uncertain:
		return terminalCommandOutcome(record, service.currentTime(), CommandOutcomeUncertain, execution.Error.Code, execution.Error.Layer, false, false, observation)
	default:
		return terminalCommandOutcome(record, service.currentTime(), CommandOutcomeFailed, execution.Error.Code, execution.Error.Layer, execution.Error.Retryable, false, observation)
	}
}

func (service *CommandService) persist(outcome CommandOutcome) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.store.Complete(ctx, outcome); err != nil {
		return fmt.Errorf("%w: %w", ErrCommandPersistence, err)
	}
	return nil
}

func (service *CommandService) currentSnapshot(ctx context.Context) (Snapshot, error) {
	snapshot := service.monitor.Snapshot()
	if snapshot.Generation != 0 {
		return snapshot, nil
	}
	refreshed, err := service.monitor.Refresh(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read current Agent snapshot: %w", err)
	}
	return refreshed, nil
}

func (service *CommandService) currentTime() time.Time {
	if service.now == nil {
		return time.Now().UTC()
	}
	return service.now().UTC()
}

func (service *CommandService) response(outcome CommandOutcome) RadioEnsureOffResponse {
	return RadioEnsureOffResponse{
		ProtocolVersion: ProtocolVersion,
		AgentInstanceID: service.monitor.InstanceID(),
		Outcome:         outcome,
	}
}

func validateRadioEnsureOffRequest(request RadioEnsureOffRequest, snapshot Snapshot) error {
	if err := validateRadioEnsureOffShape(request); err != nil {
		return err
	}
	if request.AgentInstanceID != snapshot.AgentInstanceID {
		return ErrCommandAgentStale
	}
	if request.SnapshotGeneration != snapshot.Generation || request.SnapshotRevision != snapshot.Revision {
		return ErrCommandSnapshotStale
	}
	for _, device := range snapshot.Devices {
		if device.ID != request.DeviceID {
			continue
		}
		if device.Generation != request.DeviceGeneration {
			return ErrCommandDeviceStale
		}
		if device.Profile != ProfileQDC507 {
			return ErrCommandUnsupported
		}
		return nil
	}
	return ErrCommandDeviceStale
}

func validateRadioEnsureOffShape(request RadioEnsureOffRequest) error {
	if !commandIDPattern.MatchString(request.OperationID) || !commandIDPattern.MatchString(request.DeviceID) ||
		!commandIDPattern.MatchString(request.ResourceGroupID) || !IsValidAgentInstanceID(request.AgentInstanceID) ||
		!isSHA256Hex(request.SnapshotRevision) || request.SnapshotGeneration == 0 || request.DeviceGeneration == 0 ||
		request.ResourceGroupGeneration == 0 || request.FencingToken == 0 ||
		request.SnapshotGeneration > math.MaxInt64 || request.DeviceGeneration > math.MaxInt64 ||
		request.ResourceGroupGeneration > math.MaxInt64 || request.FencingToken > math.MaxInt64 {
		return ErrCommandRequestInvalid
	}
	return nil
}

func radioEnsureOffDigest(request RadioEnsureOffRequest) (string, error) {
	encoded, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func terminalCommandOutcome(record outcomeRecord, completedAt time.Time, state, code, layer string, retryable, reconciled bool, observation RadioEnsureOffObservation) CommandOutcome {
	completedAt = completedAt.UTC()
	if completedAt.Before(record.Outcome.AcceptedAt) {
		completedAt = record.Outcome.AcceptedAt
	}
	observation = normalizeRadioEnsureOffObservation(observation)
	return CommandOutcome{
		OperationID: record.Request.OperationID,
		Command:     CommandRadioEnsureOff,
		State:       state,
		Code:        code,
		ErrorLayer:  layer,
		Retryable:   retryable,
		Reconciled:  reconciled,
		Observation: observation,
		AcceptedAt:  record.Outcome.AcceptedAt,
		CompletedAt: &completedAt,
	}
}

func normalizeRadioEnsureOffObservation(observation RadioEnsureOffObservation) RadioEnsureOffObservation {
	if observation.RF.State == "" {
		observation.RF.State = RFStateUnknown
	}
	return observation
}

func sameSnapshotFence(left, right Snapshot) bool {
	return left.AgentInstanceID == right.AgentInstanceID && left.Generation == right.Generation && left.Revision == right.Revision
}

func snapshotHasQDC507(snapshot Snapshot, deviceID string) bool {
	for _, device := range snapshot.Devices {
		if device.ID == deviceID {
			return device.Profile == ProfileQDC507
		}
	}
	return false
}
