package agentapi

import (
	"context"
	"errors"
	"testing"
)

type fakeRFBackend struct {
	observation RFObservation
	applied     bool
	err         error
	calls       int
}

func (backend *fakeRFBackend) SetRFState(context.Context, Snapshot, string, bool) (RFObservation, bool, error) {
	backend.calls++
	return backend.observation, backend.applied, backend.err
}

func TestRFServiceUsesTypedDesiredStateAndCurrentSnapshotFence(t *testing.T) {
	scanner := &monitorScanner{devices: []DeviceReport{{
		ID: "usb-1-3", DisplayName: "ML307A", PhysicalPath: "1-3", Profile: ProfileML307A,
		Capabilities: []CapabilityEvidence{{Capability: "rf-control", Status: EvidenceObserved}},
	}}}
	monitor := newMonitor(scanner, "01234567-89ab-cdef-0123-456789abcdef", 1)
	snapshot, err := monitor.Refresh(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeRFBackend{observation: RFObservation{State: RFStateOn}, applied: true}
	service := NewRFService(monitor, backend)
	request := RFSetRequest{
		AgentInstanceID: snapshot.AgentInstanceID, SnapshotGeneration: snapshot.Generation,
		SnapshotRevision: snapshot.Revision, DeviceID: snapshot.Devices[0].ID,
		DeviceGeneration: snapshot.Devices[0].Generation, Enabled: true,
	}
	response, err := service.Set(t.Context(), request)
	if err != nil || response.State != RFStateOn || !response.Applied || backend.calls != 1 {
		t.Fatalf("response=%#v calls=%d error=%v", response, backend.calls, err)
	}
	request.SnapshotGeneration++
	if _, err := service.Set(t.Context(), request); !errors.Is(err, ErrRFSnapshotStale) || backend.calls != 1 {
		t.Fatalf("stale error=%v calls=%d", err, backend.calls)
	}
}

func TestRFServiceRejectsUnverifiedCapabilityAndUnconfirmedState(t *testing.T) {
	scanner := &monitorScanner{devices: []DeviceReport{{
		ID: "usb-1-3", DisplayName: "ML307A", PhysicalPath: "1-3", Profile: ProfileML307A,
		Capabilities: []CapabilityEvidence{{Capability: "rf-control", Status: EvidenceDocumented}},
	}}}
	monitor := newMonitor(scanner, "01234567-89ab-cdef-0123-456789abcdef", 1)
	snapshot, err := monitor.Refresh(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeRFBackend{observation: RFObservation{State: RFStateOff}, applied: true}
	service := NewRFService(monitor, backend)
	request := RFSetRequest{
		AgentInstanceID: snapshot.AgentInstanceID, SnapshotGeneration: snapshot.Generation,
		SnapshotRevision: snapshot.Revision, DeviceID: snapshot.Devices[0].ID,
		DeviceGeneration: snapshot.Devices[0].Generation, Enabled: true,
	}
	if _, err := service.Set(t.Context(), request); !errors.Is(err, ErrRFUnsupported) {
		t.Fatalf("unverified capability error=%v", err)
	}
	snapshot.Devices[0].Capabilities[0].Status = EvidenceObserved
	monitor.mu.Lock()
	monitor.snapshot = snapshot
	monitor.mu.Unlock()
	if _, err := service.Set(t.Context(), request); !errors.Is(err, ErrRFNotConfirmed) {
		t.Fatalf("unconfirmed state error=%v", err)
	}
}
