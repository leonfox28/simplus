package connectivity

import (
	"context"
	"errors"
	"time"

	stream "github.com/leonfox28/simplus/internal/domain/connectivity"
	line "github.com/leonfox28/simplus/internal/domain/line"
	domain "github.com/leonfox28/simplus/internal/domain/notification"
)

type Repository interface {
	HasConnectionCheckpoint(context.Context, string, string) (bool, error)
	ReadConnectionCursor(context.Context, string) (domain.Cursor, error)
	CommitConnectionObservations(context.Context, domain.Cursor, domain.Cursor, []domain.Observation) error
}
type Lines interface {
	ListManagedLines(context.Context) ([]line.Record, error)
}
type VoWiFiSource interface {
	ConnectionSnapshot(context.Context) (stream.Snapshot, error)
	ConnectionChanges(context.Context, stream.Cursor) (stream.Changes, error)
}
type VoWiFiMonitor struct {
	store  Repository
	lines  Lines
	source VoWiFiSource
}

func NewVoWiFiMonitor(store Repository, lines Lines, source VoWiFiSource) (*VoWiFiMonitor, error) {
	if store == nil || lines == nil || source == nil {
		return nil, errors.New("connection monitor dependencies are incomplete")
	}
	return &VoWiFiMonitor{store, lines, source}, nil
}
func (m *VoWiFiMonitor) Poll(ctx context.Context) error {
	previous, err := m.store.ReadConnectionCursor(ctx, "vowifi")
	if err != nil {
		return err
	}
	lines, err := m.lines.ListManagedLines(ctx)
	if err != nil {
		return err
	}
	names := map[string]string{}
	for _, line := range lines {
		names[line.ID] = line.DisplayName
	}
	missing := map[string]bool{}
	for id := range names {
		known, err := m.store.HasConnectionCheckpoint(ctx, id, "vowifi")
		if err != nil {
			return err
		}
		if !known {
			missing[id] = true
		}
	}
	next := previous
	states := []stream.State{}
	reset := previous.Instance == ""
	if !reset {
		var baseline *stream.Snapshot
		if len(missing) > 0 {
			snapshot, err := m.source.ConnectionSnapshot(ctx)
			if err != nil {
				return err
			}
			baseline = &snapshot
		}
		changes, err := m.source.ConnectionChanges(ctx, stream.Cursor{Instance: previous.Instance, Sequence: previous.Sequence})
		if err != nil {
			return err
		}
		reset = changes.Reset
		if !reset {
			next.Instance, next.Sequence = changes.Cursor.Instance, changes.Cursor.Sequence
			if baseline != nil && baseline.Cursor.Instance == changes.Cursor.Instance {
				seen := map[string]bool{}
				for _, state := range baseline.States {
					seen[state.ObjectID] = true
					if missing[state.ObjectID] {
						states = append(states, state)
					}
				}
				for id := range missing {
					if !seen[id] {
						states = append(states, stream.State{ObjectID: id, ObservedAt: time.Now().UTC(), Reason: "stopped"})
					}
				}
			}
			for _, event := range changes.Events {
				if baseline != nil && baseline.Cursor.Instance == changes.Cursor.Instance && missing[event.State.ObjectID] && event.Sequence <= baseline.Cursor.Sequence {
					continue
				}
				states = append(states, event.State)
			}
		}
	}
	if reset {
		snapshot, err := m.source.ConnectionSnapshot(ctx)
		if err != nil {
			return err
		}
		next.Instance, next.Sequence = snapshot.Cursor.Instance, snapshot.Cursor.Sequence
		if previous.Instance != "" {
			next.Gaps++
		}
		states = snapshot.States
		seen := map[string]bool{}
		for _, state := range states {
			seen[state.ObjectID] = true
		}
		for id := range names {
			if !seen[id] {
				states = append(states, stream.State{ObjectID: id, ObservedAt: time.Now().UTC(), Reason: "stopped"})
			}
		}
	}
	observations := make([]domain.Observation, 0, len(states))
	for _, state := range states {
		if name, ok := names[state.ObjectID]; ok {
			observations = append(observations, domain.Observation{ObjectID: state.ObjectID, DisplayName: name, Kind: "vowifi", Connected: state.Connected, ObservedAt: state.ObservedAt, Reason: state.Reason})
		}
	}
	return m.store.CommitConnectionObservations(ctx, previous, next, observations)
}
func (m *VoWiFiMonitor) Run(ctx context.Context, report func(error)) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		cycle, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := m.Poll(cycle)
		cancel()
		if report != nil {
			report(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
