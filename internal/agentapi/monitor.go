package agentapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

type Scanner interface {
	Scan(context.Context) ([]DeviceReport, error)
	Probe(context.Context, Snapshot, []string) ([]DeviceProbe, error)
}

type Monitor struct {
	scanner           Scanner
	now               func() time.Time
	instanceID        string
	initialGeneration uint64

	refreshMu         sync.Mutex
	mu                sync.RWMutex
	snapshot          Snapshot
	deviceRevisions   map[string]string
	deviceGenerations map[string]uint64
	present           map[string]bool
	changed           chan struct{}
}

func NewMonitor(scanner Scanner) *Monitor {
	instanceID, initialGeneration := newMonitorIdentity()
	return newMonitor(scanner, instanceID, initialGeneration)
}

func newMonitor(scanner Scanner, instanceID string, initialGeneration uint64) *Monitor {
	if initialGeneration == 0 {
		initialGeneration = 1
	}
	return &Monitor{
		scanner: scanner, now: time.Now, instanceID: instanceID, initialGeneration: initialGeneration,
		deviceRevisions: make(map[string]string), deviceGenerations: make(map[string]uint64),
		present: make(map[string]bool), changed: make(chan struct{}),
	}
}

func newMonitorIdentity() (string, uint64) {
	material := make([]byte, 24)
	if _, err := rand.Read(material); err != nil {
		sum := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
		copy(material, sum[:24])
	}
	material[6] = (material[6] & 0x0f) | 0x40
	material[8] = (material[8] & 0x3f) | 0x80
	// Keep the fencing generation below 10^13 so generic identity scanners cannot mistake it for ICCID/IMEI material.
	generation := binary.BigEndian.Uint64(material[16:]) & ((1 << 40) - 1)
	if generation == 0 {
		generation = 1
	}
	rawID := hex.EncodeToString(material[:16])
	instanceID := rawID[:8] + "-" + rawID[8:12] + "-" + rawID[12:16] + "-" + rawID[16:20] + "-" + rawID[20:]
	return instanceID, generation
}

func (monitor *Monitor) InstanceID() string {
	if monitor == nil {
		return ""
	}
	return monitor.instanceID
}

func (monitor *Monitor) Refresh(ctx context.Context) (Snapshot, error) {
	if monitor == nil || monitor.scanner == nil {
		return Snapshot{}, errors.New("agent scanner is unavailable")
	}
	monitor.refreshMu.Lock()
	defer monitor.refreshMu.Unlock()
	devices, err := monitor.scanner.Scan(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	sort.Slice(devices, func(left, right int) bool { return devices[left].ID < devices[right].ID })
	seen := make(map[string]bool, len(devices))
	for index := range devices {
		device := &devices[index]
		if device.ID == "" {
			return Snapshot{}, errors.New("agent scanner returned a device without an id")
		}
		if seen[device.ID] {
			return Snapshot{}, fmt.Errorf("agent scanner returned duplicate device %q", device.ID)
		}
		seen[device.ID] = true
		revision, err := digestDevice(*device)
		if err != nil {
			return Snapshot{}, err
		}
		previouslyPresent := monitor.present[device.ID]
		if !previouslyPresent || monitor.deviceRevisions[device.ID] != revision {
			monitor.deviceGenerations[device.ID] = monitor.nextGeneration(monitor.deviceGenerations[device.ID])
		}
		monitor.deviceRevisions[device.ID] = revision
		device.Generation = monitor.deviceGenerations[device.ID]
	}
	for id := range monitor.present {
		monitor.present[id] = seen[id]
	}
	for id := range seen {
		monitor.present[id] = true
	}

	revision, err := digestDevices(devices)
	if err != nil {
		return Snapshot{}, err
	}
	now := monitor.now().UTC()

	monitor.mu.Lock()
	defer monitor.mu.Unlock()
	changed := monitor.snapshot.Generation == 0 || monitor.snapshot.Revision != revision
	if changed {
		monitor.snapshot.Generation = monitor.nextGeneration(monitor.snapshot.Generation)
		close(monitor.changed)
		monitor.changed = make(chan struct{})
	}
	monitor.snapshot.ProtocolVersion = ProtocolVersion
	monitor.snapshot.AgentInstanceID = monitor.instanceID
	monitor.snapshot.Revision = revision
	monitor.snapshot.ObservedAt = now
	monitor.snapshot.Devices = cloneDevices(devices)
	return cloneSnapshot(monitor.snapshot), nil
}

func (monitor *Monitor) nextGeneration(current uint64) uint64 {
	if current == 0 {
		return monitor.initialGeneration
	}
	if current >= (1<<40)-1 {
		return 1
	}
	return current + 1
}

func (monitor *Monitor) Snapshot() Snapshot {
	if monitor == nil {
		return Snapshot{}
	}
	monitor.mu.RLock()
	defer monitor.mu.RUnlock()
	return cloneSnapshot(monitor.snapshot)
}

func (monitor *Monitor) WaitForChange(ctx context.Context, instanceID string, after uint64) (Snapshot, bool, error) {
	if monitor == nil {
		return Snapshot{}, false, errors.New("agent monitor is unavailable")
	}
	monitor.mu.RLock()
	current := cloneSnapshot(monitor.snapshot)
	changed := monitor.changed
	monitor.mu.RUnlock()
	if instanceID != "" && instanceID != current.AgentInstanceID {
		return current, true, nil
	}
	if current.Generation > after {
		return current, true, nil
	}
	select {
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return monitor.Snapshot(), false, nil
		}
		return Snapshot{}, false, ctx.Err()
	case <-changed:
		return monitor.Snapshot(), true, nil
	}
}

func (monitor *Monitor) Probe(ctx context.Context, deviceIDs []string) (ProbeResponse, error) {
	if monitor == nil || monitor.scanner == nil {
		return ProbeResponse{}, errors.New("agent scanner is unavailable")
	}
	snapshot := monitor.Snapshot()
	if snapshot.Generation == 0 {
		var err error
		snapshot, err = monitor.Refresh(ctx)
		if err != nil {
			return ProbeResponse{}, err
		}
	}
	devices, err := monitor.scanner.Probe(ctx, snapshot, deviceIDs)
	if err != nil {
		return ProbeResponse{}, err
	}
	current := monitor.Snapshot()
	if current.AgentInstanceID != snapshot.AgentInstanceID || current.Generation != snapshot.Generation || current.Revision != snapshot.Revision {
		return ProbeResponse{}, errors.New("hardware changed during read-only probe")
	}
	sort.Slice(devices, func(left, right int) bool { return devices[left].DeviceID < devices[right].DeviceID })
	return ProbeResponse{
		ProtocolVersion: ProtocolVersion, AgentInstanceID: snapshot.AgentInstanceID,
		SnapshotGeneration: snapshot.Generation, SnapshotRevision: snapshot.Revision,
		ObservedAt: monitor.now().UTC(), Devices: devices,
	}, nil
}

func (monitor *Monitor) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return errors.New("agent scan interval must be positive")
	}
	if _, err := monitor.Refresh(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if _, err := monitor.Refresh(ctx); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("refresh hardware snapshot: %w", err)
			}
		}
	}
}

func digestDevice(device DeviceReport) (string, error) {
	device.Generation = 0
	encoded, err := json.Marshal(device)
	if err != nil {
		return "", fmt.Errorf("encode device revision: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func digestDevices(devices []DeviceReport) (string, error) {
	content := cloneDevices(devices)
	for index := range content {
		content[index].Generation = 0
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return "", fmt.Errorf("encode topology revision: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func cloneDevices(devices []DeviceReport) []DeviceReport {
	if devices == nil {
		return nil
	}
	encoded, _ := json.Marshal(devices)
	var clone []DeviceReport
	_ = json.Unmarshal(encoded, &clone)
	return clone
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	snapshot.Devices = cloneDevices(snapshot.Devices)
	return snapshot
}
