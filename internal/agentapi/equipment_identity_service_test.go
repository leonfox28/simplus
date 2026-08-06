package agentapi

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeEquipmentIdentityBackend struct {
	observation EquipmentIdentityObservation
	calls       int
}

func (backend *fakeEquipmentIdentityBackend) ReadEquipmentIdentity(context.Context, Snapshot, string) (EquipmentIdentityObservation, error) {
	backend.calls++
	return backend.observation, nil
}

func TestEquipmentIdentityServiceUsesCurrentSnapshotFenceAndValidatesIMEI(t *testing.T) {
	scanner := &monitorScanner{devices: []DeviceReport{{
		ID: "usb-1-3", DisplayName: "ML307A", PhysicalPath: "1-3", Profile: ProfileML307A,
	}}}
	monitor := newMonitor(scanner, "01234567-89ab-cdef-0123-456789abcdef", 1)
	snapshot, err := monitor.Refresh(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeEquipmentIdentityBackend{observation: EquipmentIdentityObservation{
		IMEI: "490154203237518", Fingerprint: strings.Repeat("a", 64),
	}}
	service := NewEquipmentIdentityService(monitor, backend)
	request := EquipmentIdentityReadRequest{
		AgentInstanceID: snapshot.AgentInstanceID, SnapshotGeneration: snapshot.Generation,
		SnapshotRevision: snapshot.Revision, DeviceID: snapshot.Devices[0].ID,
		DeviceGeneration: snapshot.Devices[0].Generation,
	}
	response, err := service.Read(t.Context(), request)
	if err != nil || response.IMEI != backend.observation.IMEI || response.Fingerprint != backend.observation.Fingerprint || backend.calls != 1 {
		t.Fatalf("response=%#v calls=%d error=%v", response, backend.calls, err)
	}
	request.SnapshotGeneration++
	if _, err := service.Read(t.Context(), request); !errors.Is(err, ErrEquipmentIdentitySnapshotStale) || backend.calls != 1 {
		t.Fatalf("stale error=%v calls=%d", err, backend.calls)
	}
	request.SnapshotGeneration--
	backend.observation.IMEI = "490154203237519"
	if _, err := service.Read(t.Context(), request); !errors.Is(err, ErrEquipmentIdentityUnavailable) {
		t.Fatalf("invalid IMEI error=%v", err)
	}
}
