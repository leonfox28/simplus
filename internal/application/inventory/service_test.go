package inventory

import (
	"context"
	"errors"
	"testing"

	"github.com/leonfox28/simplus/internal/domain/accessmode"
	"github.com/leonfox28/simplus/internal/domain/hardware"
)

type hardwareSourceFunc func(context.Context) (hardware.Snapshot, error)

func (source hardwareSourceFunc) Snapshot(ctx context.Context) (hardware.Snapshot, error) {
	return source(ctx)
}

type memoryAccessModes struct {
	values map[string]accessmode.Mode
	err    error
}

func (store *memoryAccessModes) SubscriptionProfileAccessModes(_ context.Context, profileIDs []string) (map[string]accessmode.Mode, error) {
	if store.err != nil {
		return nil, store.err
	}
	modes := make(map[string]accessmode.Mode, len(profileIDs))
	for _, profileID := range profileIDs {
		if mode, configured := store.values[profileID]; configured {
			modes[profileID] = mode
		}
	}
	return modes, nil
}

func (store *memoryAccessModes) PutSubscriptionProfileAccessMode(_ context.Context, profileID string, mode accessmode.Mode) error {
	if store.err != nil {
		return store.err
	}
	store.values[profileID] = mode
	return nil
}

func TestSimulatorSnapshotModelsDeviceProfileAndLineSeparately(t *testing.T) {
	service := NewSimulator(&memoryAccessModes{values: make(map[string]accessmode.Mode)})
	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Devices) != 1 || len(snapshot.Lines) != 1 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	device := snapshot.Devices[0]
	line := snapshot.Lines[0]
	if line.PhysicalDeviceID != device.ID || line.SubscriptionProfileID != "simulator-profile-1" {
		t.Fatalf("line identity = %#v, device = %#v", line, device)
	}
	if line.AccessModeConfigured || line.AccessMode != accessmode.HoldRFOff || line.RFSafety != RFSafetyOff || line.State != LineAwaitingAccessMode {
		t.Fatalf("unsafe unconfigured simulator line = %#v", line)
	}

	snapshot.Devices[0].DisplayName = "mutated"
	fresh, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Devices[0].DisplayName == "mutated" {
		t.Fatal("Snapshot returned mutable service state")
	}
}

func TestMultiSimulatorModelsTwoIndependentModems(t *testing.T) {
	service := NewMultiSimulator(&memoryAccessModes{values: map[string]accessmode.Mode{
		"simulator-profile-1": accessmode.CellularNative,
		"simulator-profile-2": accessmode.CellularNative,
	}})
	topology, err := service.Topology(context.Background())
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

func TestPutAccessModeConfiguresKnownProfile(t *testing.T) {
	store := &memoryAccessModes{values: make(map[string]accessmode.Mode)}
	service := NewSimulator(store)
	snapshot, err := service.PutAccessMode(context.Background(), "simulator-profile-1", accessmode.CellularNative)
	if err != nil {
		t.Fatal(err)
	}
	line := snapshot.Lines[0]
	if !line.AccessModeConfigured || line.AccessMode != accessmode.CellularNative || line.State != LineReady {
		t.Fatalf("configured line = %#v", line)
	}
	if line.RFSafety != RFSafetyOff {
		t.Fatalf("simulator access mode changed RF state: %#v", line)
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
			}), &memoryAccessModes{values: map[string]accessmode.Mode{"simulator-profile-1": accessmode.CellularNative}})
			topology, err := service.Topology(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if topology.Lines[0].State != LineUnavailable || topology.Lines[0].RFSafety != RFSafetyOff {
				t.Fatalf("line = %#v", topology.Lines[0])
			}
		})
	}
}

func TestPutAccessModeRejectsUnknownProfileAndMode(t *testing.T) {
	service := NewSimulator(&memoryAccessModes{values: make(map[string]accessmode.Mode)})
	if _, err := service.PutAccessMode(context.Background(), "missing-profile", accessmode.HoldRFOff); !errors.Is(err, ErrSubscriptionProfileNotFound) {
		t.Fatalf("unknown profile error = %v", err)
	}
	if _, err := service.PutAccessMode(context.Background(), "simulator-profile-1", accessmode.Mode("automatic")); !errors.Is(err, ErrInvalidAccessMode) {
		t.Fatalf("invalid mode error = %v", err)
	}
}

func TestSnapshotPropagatesRepositoryAndContextErrors(t *testing.T) {
	storeError := errors.New("store unavailable")
	if _, err := NewSimulator(&memoryAccessModes{values: map[string]accessmode.Mode{}, err: storeError}).Snapshot(context.Background()); !errors.Is(err, storeError) {
		t.Fatalf("repository error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewSimulator(&memoryAccessModes{values: make(map[string]accessmode.Mode)}).Snapshot(ctx); err == nil {
		t.Fatal("Snapshot accepted a cancelled context")
	}
}
