package agentapi

import (
	"context"
	"strings"
	"testing"
	"time"
)

type monitorScanner struct {
	devices   []DeviceReport
	probes    []DeviceProbe
	probeHook func()
}

func (scanner *monitorScanner) Scan(context.Context) ([]DeviceReport, error) {
	return cloneDevices(scanner.devices), nil
}

func (scanner *monitorScanner) Probe(context.Context, Snapshot, []string) ([]DeviceProbe, error) {
	if scanner.probeHook != nil {
		scanner.probeHook()
	}
	return append([]DeviceProbe(nil), scanner.probes...), nil
}

func TestNewMonitorIdentityUsesUUIDAndBoundedGeneration(t *testing.T) {
	instanceID, generation := newMonitorIdentity()
	if !IsValidAgentInstanceID(instanceID) || instanceID[14] != '4' || !strings.ContainsRune("89ab", rune(instanceID[19])) {
		t.Fatalf("instance id = %q", instanceID)
	}
	if generation == 0 || generation >= 1<<40 {
		t.Fatalf("generation = %d", generation)
	}
}

func TestMonitorTracksContentAndReenumerationGenerations(t *testing.T) {
	scanner := &monitorScanner{devices: []DeviceReport{{ID: "usb-1-1", DisplayName: "QDC507", PhysicalPath: "1-1"}}}
	monitor := newMonitor(scanner, "01234567-89ab-cdef-0123-456789abcdef", 1)
	now := time.Date(2026, 8, 2, 1, 2, 3, 0, time.UTC)
	monitor.now = func() time.Time { return now }
	first, err := monitor.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.Generation != 1 || first.Devices[0].Generation != 1 || len(first.Revision) != 64 {
		t.Fatalf("first = %#v", first)
	}
	second, err := monitor.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.Generation != first.Generation || second.Devices[0].Generation != first.Devices[0].Generation {
		t.Fatalf("unchanged scan advanced generation: %#v", second)
	}
	scanner.devices[0].DisplayName = "QDC507 updated"
	third, err := monitor.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if third.Generation != 2 || third.Devices[0].Generation != 2 {
		t.Fatalf("changed scan = %#v", third)
	}
	scanner.devices = nil
	removed, err := monitor.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if removed.Generation != 3 || len(removed.Devices) != 0 {
		t.Fatalf("removed = %#v", removed)
	}
	scanner.devices = []DeviceReport{{ID: "usb-1-1", DisplayName: "QDC507 updated", PhysicalPath: "1-1"}}
	reappeared, err := monitor.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reappeared.Generation != 4 || reappeared.Devices[0].Generation != 3 {
		t.Fatalf("reappeared = %#v", reappeared)
	}
}

func TestMonitorRejectsProbeWhenTopologyChangesMidFlight(t *testing.T) {
	scanner := &monitorScanner{
		devices: []DeviceReport{{ID: "usb-1-1", DisplayName: "QDC507", PhysicalPath: "1-1"}},
		probes: []DeviceProbe{validCompleteProbeFixture(
			"usb-1-1",
			SIMObservation{State: SIMStateUnknown, PrimaryLockState: PrimaryLockUnknown},
		)},
	}
	monitor := newMonitor(scanner, "01234567-89ab-cdef-0123-456789abcdef", 10)
	if _, err := monitor.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	scanner.probeHook = func() {
		scanner.devices[0].DisplayName = "changed"
		if _, err := monitor.Refresh(context.Background()); err != nil {
			t.Errorf("refresh during probe: %v", err)
		}
	}
	if _, err := monitor.Probe(context.Background(), nil); err == nil {
		t.Fatal("mid-probe topology change unexpectedly accepted")
	}
}

func TestMonitorContentRevisionIsStableAcrossAgentRestart(t *testing.T) {
	scanner := &monitorScanner{devices: []DeviceReport{{ID: "usb-1-1", DisplayName: "QDC507", PhysicalPath: "1-1"}}}
	first, err := newMonitor(scanner, "01234567-89ab-cdef-0123-456789abcdef", 100).Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := newMonitor(scanner, "fedcba98-7654-3210-fedc-ba9876543210", 200).Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.Generation == second.Generation || first.AgentInstanceID == second.AgentInstanceID || first.Revision != second.Revision {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
}

func TestMonitorWaitAndProbeUseVersionedSnapshot(t *testing.T) {
	scanner := &monitorScanner{
		devices: []DeviceReport{{ID: "usb-1-1", DisplayName: "QDC507", PhysicalPath: "1-1"}},
		probes: []DeviceProbe{validCompleteProbeFixture(
			"usb-1-1",
			SIMObservation{State: SIMStateAbsent, PrimaryLockState: PrimaryLockUnknown},
		)},
	}
	monitor := newMonitor(scanner, "01234567-89ab-cdef-0123-456789abcdef", 1)
	initial, err := monitor.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	waited, changed, err := monitor.WaitForChange(ctx, initial.AgentInstanceID, initial.Generation)
	if err != nil || changed || waited.Generation != initial.Generation {
		t.Fatalf("wait = %#v changed=%v err=%v", waited, changed, err)
	}
	response, err := monitor.Probe(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if response.ProtocolVersion != ProtocolVersion || response.AgentInstanceID != initial.AgentInstanceID || response.SnapshotGeneration != initial.Generation || len(response.Devices) != 1 {
		t.Fatalf("probe = %#v", response)
	}
	other, changed, err := monitor.WaitForChange(context.Background(), "fedcba98-7654-3210-fedc-ba9876543210", initial.Generation)
	if err != nil || !changed || other.AgentInstanceID != initial.AgentInstanceID {
		t.Fatalf("restart fence = %#v changed=%v err=%v", other, changed, err)
	}
}
