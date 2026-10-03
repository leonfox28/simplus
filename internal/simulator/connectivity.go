// Package simulator provides deterministic, in-memory capability adapters.
// It never opens a modem or starts a network process.
package simulator

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	app "github.com/leonfox28/simplus/internal/application/connectivity"
	"github.com/leonfox28/simplus/internal/application/inventory"
	stream "github.com/leonfox28/simplus/internal/domain/connectivity"
	"github.com/leonfox28/simplus/internal/domain/modem"
	"github.com/leonfox28/simplus/internal/domain/vowifi"
)

type Inventory interface {
	Topology(context.Context) (inventory.Topology, error)
}
type Cellular struct {
	inventory Inventory
	mu        sync.Mutex
	off       map[string]bool
}

func NewCellular(inventory Inventory) *Cellular {
	return &Cellular{inventory: inventory, off: map[string]bool{}}
}
func (s *Cellular) Targets(ctx context.Context) ([]app.ProbeTarget, error) {
	topology, err := s.inventory.Topology(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]app.ProbeTarget, 0, len(topology.Devices))
	for _, d := range topology.Devices {
		result = append(result, app.ProbeTarget{Instance: topology.AgentInstanceID, ID: d.ID, Generation: topology.Generation, DeviceGeneration: d.Generation, Revision: topology.Revision})
	}
	return result, nil
}
func (s *Cellular) Read(ctx context.Context, id string) (modem.RuntimeStatus, error) {
	topology, err := s.inventory.Topology(ctx)
	if err != nil {
		return modem.RuntimeStatus{}, err
	}
	found := false
	for _, d := range topology.Devices {
		if d.ID == id {
			found = true
		}
	}
	if !found {
		return modem.RuntimeStatus{}, errors.New("simulator device absent")
	}
	s.mu.Lock()
	off := s.off[id]
	s.mu.Unlock()
	status := modem.RuntimeStatus{RFState: modem.RFStateOn, SIMPresence: modem.SIMPresencePresent, Cellular: modem.UnavailableCellularStatus()}
	status.Cellular.State = modem.CellularRegisteredHome
	status.Cellular.ErrorCode = ""
	status.Cellular.ObservedAt = time.Now().UTC()
	status.Cellular.Registrations = []modem.CellularRegistration{{Domain: "cs", State: "registered-home"}, {Domain: "packet", State: "registered-home"}, {Domain: "eps", State: "registered-home"}}
	if off {
		status.RFState = modem.RFStateOff
		status.Cellular.State = modem.CellularRFOff
		for i := range status.Cellular.Registrations {
			status.Cellular.Registrations[i].State = "not-registered"
		}
	}
	return status, nil
}
func (s *Cellular) Set(ctx context.Context, id string, enabled bool) (string, error) {
	if _, err := s.Read(ctx, id); err != nil {
		return "", err
	}
	s.mu.Lock()
	s.off[id] = !enabled
	s.mu.Unlock()
	if enabled {
		return modem.RFStateOn, nil
	}
	return modem.RFStateOff, nil
}
func (s *Cellular) Probe(ctx context.Context, target app.ProbeTarget) (app.CellularSample, error) {
	topology, err := s.inventory.Topology(ctx)
	if err != nil {
		return app.CellularSample{}, err
	}
	if topology.AgentInstanceID != target.Instance {
		return app.CellularSample{}, errors.New("simulator target stale")
	}
	for _, d := range topology.Devices {
		if d.ID == target.ID && d.Generation == target.DeviceGeneration {
			status, err := s.Read(ctx, d.ID)
			if err != nil {
				return app.CellularSample{}, err
			}
			online := status.RFState == modem.RFStateOn
			reason := "registered"
			if !online {
				reason = "rf_off"
			}
			return app.CellularSample{Fingerprint: d.EquipmentIdentityFingerprint, Known: true, Connected: online, Reason: reason, ObservedAt: time.Now().UTC()}, nil
		}
	}
	return app.CellularSample{}, errors.New("simulator target stale")
}

type VoWiFi struct {
	mu     sync.Mutex
	cursor stream.Cursor
	states map[string]vowifi.Status
	events []stream.Change
}

func NewVoWiFi() *VoWiFi {
	return &VoWiFi{cursor: stream.Cursor{Instance: uuid.NewString()}, states: map[string]vowifi.Status{}}
}
func (s *VoWiFi) List(ctx context.Context) ([]vowifi.Status, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]vowifi.Status, 0, len(s.states))
	for _, v := range s.states {
		result = append(result, v)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].LineID < result[j].LineID })
	return result, nil
}
func (s *VoWiFi) Start(ctx context.Context, r vowifi.StartRequest) (vowifi.Status, error) {
	if err := ctx.Err(); err != nil {
		return vowifi.Status{}, err
	}
	if r.LineID == "" || r.HardwareLineID == "" {
		return vowifi.Status{}, vowifi.ErrRequestInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if old := s.states[r.LineID]; old.Online {
		return old, vowifi.ErrAlreadyRunning
	}
	now := time.Now().UTC()
	status := vowifi.Status{LineID: r.LineID, State: vowifi.StateOnline, Online: true, EgressMode: r.EgressMode, CountryCode: r.CountryCode, StartedAt: now, RegisteredAt: now, NextRefresh: now.Add(time.Hour), Attempt: 1}
	s.states[r.LineID] = status
	s.change(r.LineID, true, "registered", now)
	return status, nil
}
func (s *VoWiFi) Stop(ctx context.Context, id string) (vowifi.Status, error) {
	if err := ctx.Err(); err != nil {
		return vowifi.Status{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	status, found := s.states[id]
	if !found || !status.Online {
		return status, vowifi.ErrNotRunning
	}
	status.Online = false
	status.State = vowifi.StateStopped
	s.states[id] = status
	s.change(id, false, "stopped", time.Now().UTC())
	return status, nil
}
func (s *VoWiFi) change(id string, online bool, reason string, at time.Time) {
	s.cursor.Sequence++
	s.events = append(s.events, stream.Change{Sequence: s.cursor.Sequence, State: stream.State{ObjectID: id, Connected: online, ObservedAt: at, Reason: reason}})
	if len(s.events) > stream.Capacity {
		s.events = append([]stream.Change(nil), s.events[len(s.events)-stream.Capacity:]...)
	}
}
func (s *VoWiFi) ConnectionSnapshot(ctx context.Context) (stream.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return stream.Snapshot{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result := stream.Snapshot{Version: stream.Version, Cursor: s.cursor, States: []stream.State{}}
	for id, status := range s.states {
		reason := "stopped"
		if status.Online {
			reason = "registered"
		}
		result.States = append(result.States, stream.State{ObjectID: id, Connected: status.Online, Reason: reason, ObservedAt: time.Now().UTC()})
	}
	return result, nil
}
func (s *VoWiFi) ConnectionChanges(ctx context.Context, c stream.Cursor) (stream.Changes, error) {
	if err := ctx.Err(); err != nil {
		return stream.Changes{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result := stream.Changes{Version: stream.Version, Cursor: s.cursor, Events: []stream.Change{}}
	result.Reset = c.Instance != s.cursor.Instance || c.Sequence > s.cursor.Sequence || (len(s.events) > 0 && c.Sequence < s.events[0].Sequence-1)
	if !result.Reset {
		for _, e := range s.events {
			if e.Sequence > c.Sequence {
				result.Events = append(result.Events, e)
			}
		}
	}
	return result, nil
}
