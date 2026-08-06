package inventory

import (
	"context"
	"errors"
	"testing"

	"github.com/leonfox28/simplus/internal/domain/hardware"
)

type hardwareSourceFunc func(context.Context) (hardware.Snapshot, error)

func (source hardwareSourceFunc) Snapshot(ctx context.Context) (hardware.Snapshot, error) {
	return source(ctx)
}

func TestSimulatorSnapshotModelsDeviceProfileAndReadyLineSeparately(t *testing.T) {
	service := NewSimulator()
	snapshot, err := service.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Devices) != 1 || len(snapshot.Lines) != 1 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	device, line := snapshot.Devices[0], snapshot.Lines[0]
	if line.PhysicalDeviceID != device.ID || line.SubscriptionProfileID != "simulator-profile-1" || line.State != LineReady {
		t.Fatalf("line identity = %#v, device = %#v", line, device)
	}

	snapshot.Devices[0].DisplayName = "mutated"
	fresh, err := service.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Devices[0].DisplayName == "mutated" {
		t.Fatal("Snapshot returned mutable service state")
	}
}

func TestMultiSimulatorModelsTwoIndependentModems(t *testing.T) {
	topology, err := NewMultiSimulator().Topology(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(topology.Devices) != 2 || len(topology.ModemFunctions) != 2 || len(topology.Lines) != 2 {
		t.Fatalf("multi-Simulator topology counts = devices:%d functions:%d lines:%d", len(topology.Devices), len(topology.ModemFunctions), len(topology.Lines))
	}
	if topology.Lines[0].PhysicalDeviceID == topology.Lines[1].PhysicalDeviceID ||
		topology.Lines[0].ModemFunctionID == topology.Lines[1].ModemFunctionID ||
		topology.Lines[0].ResourceGroupID == topology.Lines[1].ResourceGroupID {
		t.Fatalf("Simulator lines share a modem boundary: %#v", topology.Lines)
	}
}

func TestTopologyKeepsUnavailableOrLockedLinesFailClosed(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*hardware.Snapshot)
	}{
		{name: "device unavailable", mutate: func(snapshot *hardware.Snapshot) { snapshot.Devices[0].State = hardware.DeviceUnavailable }},
		{name: "profile locked", mutate: func(snapshot *hardware.Snapshot) { snapshot.SubscriptionProfiles[0].State = hardware.ProfileLocked }},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := New(hardwareSourceFunc(func(ctx context.Context) (hardware.Snapshot, error) {
				snapshot, err := (simulatorSource{}).Snapshot(ctx)
				if err == nil {
					test.mutate(&snapshot)
				}
				return snapshot, err
			}))
			topology, err := service.Topology(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if topology.Lines[0].State != LineUnavailable {
				t.Fatalf("line = %#v", topology.Lines[0])
			}
		})
	}
}

func TestSnapshotPropagatesSourceAndContextErrors(t *testing.T) {
	sourceError := errors.New("source unavailable")
	service := New(hardwareSourceFunc(func(context.Context) (hardware.Snapshot, error) {
		return hardware.Snapshot{}, sourceError
	}))
	if _, err := service.Snapshot(t.Context()); !errors.Is(err, sourceError) {
		t.Fatalf("source error = %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := NewSimulator().Snapshot(ctx); err == nil {
		t.Fatal("Snapshot accepted a cancelled context")
	}
}
