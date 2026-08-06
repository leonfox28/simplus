package calls

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/leonfox28/simplus/internal/application/inventory"
	"github.com/leonfox28/simplus/internal/domain/accessmode"
	"github.com/leonfox28/simplus/internal/domain/call"
)

const ErrorInterruptedByRestart = "CALL_INTERRUPTED_BY_RESTART"

var (
	ErrInvalid         = errors.New("call request is invalid")
	ErrUnsafeNumber    = errors.New("emergency or uncertain short number is forbidden")
	ErrLineUnavailable = errors.New("call line is unavailable")
	ErrLineBusy        = errors.New("call line is busy")
)

var (
	operationPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)
	linePattern      = regexp.MustCompile(`^line_[A-Za-z0-9_-]{22}$`)
	numberPattern    = regexp.MustCompile(`^\+?[0-9]{3,20}$`)
	dtmfPattern      = regexp.MustCompile(`^[0-9*#A-D]{1,32}$`)
)

type Repository interface {
	CreateCall(context.Context, call.Record) (call.Record, bool, error)
	SetCallState(context.Context, string, string, string, time.Time) (call.Record, error)
	ListCalls(context.Context, int) ([]call.Record, error)
	ReconcileCalls(context.Context, string, time.Time) (int64, error)
}
type LineSource interface {
	Topology(context.Context) (inventory.Topology, error)
}
type AccessPathGuard interface {
	Available(context.Context, string) bool
}

type Service struct {
	repository  Repository
	lines       LineSource
	random      io.Reader
	now         func() time.Time
	mu          sync.Mutex
	accessPaths AccessPathGuard
}

func (service *Service) UseAccessPathGuard(guard AccessPathGuard) {
	if service != nil {
		service.accessPaths = guard
	}
}

func New(ctx context.Context, repository Repository, lines LineSource) (*Service, error) {
	if repository == nil || lines == nil {
		return nil, errors.New("call service dependencies are incomplete")
	}
	service := &Service{repository: repository, lines: lines, random: rand.Reader, now: time.Now}
	if _, err := repository.ReconcileCalls(ctx, ErrorInterruptedByRestart, service.now().UTC()); err != nil {
		return nil, err
	}
	return service, nil
}

func (service *Service) List(ctx context.Context) ([]call.Record, error) {
	return service.repository.ListCalls(ctx, 100)
}

func (service *Service) Dial(ctx context.Context, operationID, lineID, number string) (call.Record, bool, error) {
	operationID, lineID, number = strings.TrimSpace(operationID), strings.TrimSpace(lineID), strings.TrimSpace(number)
	if !operationPattern.MatchString(operationID) || !linePattern.MatchString(lineID) || !numberPattern.MatchString(number) {
		return call.Record{}, false, ErrInvalid
	}
	if unsafeNumber(number) {
		return call.Record{}, false, ErrUnsafeNumber
	}
	if err := service.requireLine(ctx, lineID); err != nil {
		return call.Record{}, false, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if existing, replayed, err := service.replayOrBusy(ctx, operationID, lineID, number, call.DirectionOutbound); err != nil || replayed {
		return existing, replayed, err
	}
	id, err := service.newID()
	if err != nil {
		return call.Record{}, false, err
	}
	now := service.now().UTC()
	value, replayed, err := service.repository.CreateCall(ctx, call.Record{ID: id, OperationID: operationID, LineID: lineID, RemoteAddress: number, Direction: call.DirectionOutbound, State: call.StateDialing, CreatedAt: now, UpdatedAt: now})
	if err != nil || replayed {
		return value, replayed, err
	}
	value, err = service.repository.SetCallState(ctx, value.ID, call.StateActive, "", service.now().UTC())
	return value, false, err
}

func (service *Service) Incoming(ctx context.Context, operationID, lineID, number string) (call.Record, bool, error) {
	if !operationPattern.MatchString(operationID) || !linePattern.MatchString(lineID) || !numberPattern.MatchString(number) {
		return call.Record{}, false, ErrInvalid
	}
	if err := service.requireLine(ctx, lineID); err != nil {
		return call.Record{}, false, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if existing, replayed, err := service.replayOrBusy(ctx, operationID, lineID, number, call.DirectionInbound); err != nil || replayed {
		return existing, replayed, err
	}
	id, err := service.newID()
	if err != nil {
		return call.Record{}, false, err
	}
	now := service.now().UTC()
	return service.repository.CreateCall(ctx, call.Record{ID: id, OperationID: operationID, LineID: lineID, RemoteAddress: number, Direction: call.DirectionInbound, State: call.StateIncoming, CreatedAt: now, UpdatedAt: now})
}

func (service *Service) Answer(ctx context.Context, id string) (call.Record, error) {
	return service.transition(ctx, id, []string{call.StateIncoming}, call.StateActive, "")
}
func (service *Service) Reject(ctx context.Context, id string) (call.Record, error) {
	return service.transition(ctx, id, []string{call.StateIncoming}, call.StateEnded, "rejected")
}
func (service *Service) Hangup(ctx context.Context, id string) (call.Record, error) {
	return service.transition(ctx, id, []string{call.StateDialing, call.StateActive}, call.StateEnded, "hangup")
}

func (service *Service) DTMF(ctx context.Context, id, digits string) (call.Record, error) {
	if !dtmfPattern.MatchString(digits) {
		return call.Record{}, ErrInvalid
	}
	values, err := service.repository.ListCalls(ctx, 100)
	if err != nil {
		return call.Record{}, err
	}
	for _, value := range values {
		if value.ID == id {
			if value.State != call.StateActive {
				return call.Record{}, call.ErrStateConflict
			}
			return value, nil
		}
	}
	return call.Record{}, call.ErrNotFound
}

func (service *Service) transition(ctx context.Context, id string, from []string, to, reason string) (call.Record, error) {
	if !operationPattern.MatchString(id) {
		return call.Record{}, ErrInvalid
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	values, err := service.repository.ListCalls(ctx, 100)
	if err != nil {
		return call.Record{}, err
	}
	for _, value := range values {
		if value.ID == id {
			ok := false
			for _, state := range from {
				ok = ok || value.State == state
			}
			if !ok {
				return call.Record{}, call.ErrStateConflict
			}
			return service.repository.SetCallState(ctx, id, to, reason, service.now().UTC())
		}
	}
	return call.Record{}, call.ErrNotFound
}

func (service *Service) requireLine(ctx context.Context, id string) error {
	topology, err := service.lines.Topology(ctx)
	if err != nil {
		return ErrLineUnavailable
	}
	for _, line := range topology.Lines {
		if line.ID == id && line.State == inventory.LineReady && line.Capabilities.CellularVoice {
			if line.AccessMode == accessmode.HostVoWiFiOnly && (service.accessPaths == nil || !service.accessPaths.Available(ctx, id)) {
				return ErrLineUnavailable
			}
			return nil
		}
	}
	return ErrLineUnavailable
}

func (service *Service) replayOrBusy(ctx context.Context, operationID, lineID, number, direction string) (call.Record, bool, error) {
	values, err := service.repository.ListCalls(ctx, 100)
	if err != nil {
		return call.Record{}, false, err
	}
	for _, value := range values {
		if value.OperationID == operationID {
			if value.LineID != lineID || value.RemoteAddress != number || value.Direction != direction {
				return call.Record{}, false, call.ErrStateConflict
			}
			return value, true, nil
		}
	}
	for _, value := range values {
		if value.LineID == lineID && (value.State == call.StateIncoming || value.State == call.StateDialing || value.State == call.StateActive) {
			return call.Record{}, false, ErrLineBusy
		}
	}
	return call.Record{}, false, nil
}
func (service *Service) newID() (string, error) {
	data := make([]byte, 16)
	if _, err := io.ReadFull(service.random, data); err != nil {
		return "", err
	}
	return "call_" + base64.RawURLEncoding.EncodeToString(data), nil
}
func unsafeNumber(number string) bool {
	digits := strings.TrimPrefix(number, "+")
	if len(digits) <= 6 {
		return true
	}
	switch digits {
	case "112", "911", "110", "119", "120", "122", "999":
		return true
	}
	return false
}
