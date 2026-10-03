package connectivityagent

import (
	"context"
	"errors"

	"github.com/leonfox28/simplus/internal/agentapi"
	app "github.com/leonfox28/simplus/internal/application/connectivity"
)

type Client interface {
	Snapshot(context.Context, bool) (agentapi.Snapshot, error)
	Probe(context.Context, agentapi.ProbeRequest) (agentapi.ProbeResponse, error)
}
type Source struct{ Client Client }

func (s Source) Targets(ctx context.Context) ([]app.ProbeTarget, error) {
	snapshot, err := s.Client.Snapshot(ctx, true)
	if err != nil {
		return nil, err
	}
	targets := make([]app.ProbeTarget, 0, len(snapshot.Devices))
	for _, d := range snapshot.Devices {
		targets = append(targets, app.ProbeTarget{Instance: snapshot.AgentInstanceID, ID: d.ID, Revision: snapshot.Revision, Generation: snapshot.Generation, DeviceGeneration: d.Generation})
	}
	return targets, nil
}
func (s Source) Probe(ctx context.Context, target app.ProbeTarget) (app.CellularSample, error) {
	probe, err := s.Client.Probe(ctx, agentapi.ProbeRequest{DeviceIDs: []string{target.ID}})
	if err != nil {
		return app.CellularSample{}, err
	}
	if probe.AgentInstanceID != target.Instance || probe.SnapshotGeneration != target.Generation || probe.SnapshotRevision != target.Revision || len(probe.Devices) != 1 || probe.Devices[0].DeviceID != target.ID {
		return app.CellularSample{}, errors.New("cellular observation target changed")
	}
	d := probe.Devices[0]
	sample := app.CellularSample{Fingerprint: d.Identity.EquipmentIdentityFingerprint, ObservedAt: probe.ObservedAt}
	if len(sample.Fingerprint) != 64 {
		return sample, nil
	}
	// Partial reads and unknown SIM/RF states never manufacture disconnection.
	if d.State != agentapi.ProbeStateComplete {
		return sample, nil
	}
	if d.SIM.State == agentapi.SIMStateAbsent || d.SIM.State == agentapi.SIMStateLocked {
		sample.Known = true
		sample.Reason = "sim_unavailable"
		return sample, nil
	}
	if d.RF.State == agentapi.RFStateOff || d.RF.State == agentapi.RFStateMinimum {
		sample.Known = true
		sample.Reason = "rf_off"
		return sample, nil
	}
	if d.SIM.State != agentapi.SIMStatePresent || d.SIM.PrimaryLockState != agentapi.PrimaryLockReady || d.RF.State != agentapi.RFStateOn {
		return sample, nil
	}
	classification := agentapi.ClassifyCellular(d)
	switch classification.State {
	case agentapi.CellularRegisteredHome, agentapi.CellularRegisteredRoaming:
		sample.Known, sample.Connected, sample.Reason = true, true, "registered"
	case agentapi.CellularSearching, agentapi.CellularDenied, agentapi.CellularNotRegistered:
		sample.Known, sample.Reason = true, "registration_lost"
	}
	return sample, nil
}
