package connectivity

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	modem "github.com/leonfox28/simplus/internal/domain/modem"
	domain "github.com/leonfox28/simplus/internal/domain/notification"
)

// ProbeTarget is an opaque current locator, never a public API or persisted key.
type ProbeTarget struct {
	Instance, ID, Revision       string
	Generation, DeviceGeneration uint64
}

func (t ProbeTarget) Key() string {
	return fmt.Sprintf("%s:%s:%d", t.Instance, t.ID, t.DeviceGeneration)
}

type CellularSample struct {
	Fingerprint      string
	Known, Connected bool
	Reason           string
	ObservedAt       time.Time
}
type CellularSource interface {
	Targets(context.Context) ([]ProbeTarget, error)
	Probe(context.Context, ProbeTarget) (CellularSample, error)
}
type Modems interface {
	ListManagedModems(context.Context) ([]modem.Record, error)
}
type CellularMonitor struct {
	store  Repository
	modems Modems
	source CellularSource
}

func NewCellularMonitor(store Repository, modems Modems, source CellularSource) (*CellularMonitor, error) {
	if store == nil || modems == nil || source == nil {
		return nil, errors.New("cellular monitor dependencies are incomplete")
	}
	return &CellularMonitor{store, modems, source}, nil
}
func (m *CellularMonitor) Run(ctx context.Context, report func(error)) {
	type result struct {
		target ProbeTarget
		sample CellularSample
		err    error
	}
	results := make(chan result, 256)
	var work sync.WaitGroup
	defer work.Wait()
	active := map[string]bool{}
	next := map[string]time.Time{}
	samples := map[string]CellularSample{}
	present := map[string]ProbeTarget{}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	publish := func(sample CellularSample) {
		if !sample.Known {
			return
		}
		records, err := m.modems.ListManagedModems(ctx)
		if err != nil {
			if report != nil {
				report(err)
			}
			return
		}
		matches := []modem.Record{}
		for _, r := range records {
			if r.EquipmentIdentityFingerprint == sample.Fingerprint {
				matches = append(matches, r)
			}
		}
		if len(matches) != 1 {
			return
		}
		r := matches[0]
		err = m.store.CommitConnectionObservations(ctx, domain.Cursor{}, domain.Cursor{}, []domain.Observation{{ObjectID: r.ID, DisplayName: r.DisplayName, Kind: "cellular", Connected: sample.Connected, Reason: sample.Reason, ObservedAt: sample.ObservedAt}})
		if err != nil && report != nil {
			report(err)
		}
	}
	// Removal is confirmed only after a successful topology read and identity
	// reconciliation. A source restart, changed port, or failed probe is not loss.
	reconcileMissing := func() {
		fingerprints := map[string]bool{}
		for key := range present {
			sample, ok := samples[key]
			if !ok || sample.Fingerprint == "" {
				return
			}
			fingerprints[sample.Fingerprint] = true
		}
		records, err := m.modems.ListManagedModems(ctx)
		if err != nil {
			if report != nil {
				report(err)
			}
			return
		}
		for _, record := range records {
			if record.EquipmentIdentityFingerprint != "" && !fingerprints[record.EquipmentIdentityFingerprint] {
				publish(CellularSample{Fingerprint: record.EquipmentIdentityFingerprint, Known: true, Reason: "device_removed", ObservedAt: time.Now().UTC()})
			}
		}

	}
	schedule := func() {
		snapshotCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		targets, err := m.source.Targets(snapshotCtx)
		cancel()
		if err != nil {
			if report != nil {
				report(err)
			}
			return
		}
		current := map[string]ProbeTarget{}
		for _, target := range targets {
			current[target.Key()] = target
		}
		for key := range samples {
			if _, ok := current[key]; !ok {
				delete(samples, key)
				delete(next, key)
			}
		}
		present = current
		reconcileMissing()
		for _, target := range targets {
			key := target.Key()
			if active[target.ID] || time.Now().Before(next[key]) {
				continue
			}
			active[target.ID] = true
			next[key] = time.Now().Add(5 * time.Second)
			work.Go(func() {
				probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
				defer cancel()
				sample, err := m.source.Probe(probeCtx, target)
				select {
				case results <- result{target, sample, err}:
				case <-ctx.Done():
				}
			})
		}
	}
	schedule()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			schedule()
		case r := <-results:
			key := r.target.Key()
			delete(active, r.target.ID)
			if r.err != nil {
				if report != nil {
					report(r.err)
				}
				continue
			}
			if _, ok := present[key]; !ok {
				continue
			}
			samples[key] = r.sample
			duplicate := false
			for other, sample := range samples {
				if other != key && sample.Fingerprint != "" && sample.Fingerprint == r.sample.Fingerprint {
					duplicate = true
				}
			}
			if !duplicate {
				publish(r.sample)
			}
			reconcileMissing()
		}
	}
}
