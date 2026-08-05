package agentapi

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

type fakeRadioExecutor struct {
	calls     int
	execution RadioEnsureOffExecution
	err       error
}

func (executor *fakeRadioExecutor) EnsureRadioOff(context.Context, Snapshot, string) (RadioEnsureOffExecution, error) {
	executor.calls++
	return executor.execution, executor.err
}

func commandRequestForSnapshot(snapshot Snapshot) RadioEnsureOffRequest {
	return RadioEnsureOffRequest{
		OperationID:             "radio-off-command-1",
		AgentInstanceID:         snapshot.AgentInstanceID,
		SnapshotGeneration:      snapshot.Generation,
		SnapshotRevision:        snapshot.Revision,
		DeviceID:                snapshot.Devices[0].ID,
		DeviceGeneration:        snapshot.Devices[0].Generation,
		ResourceGroupID:         "agent-usb-1-1-resources",
		ResourceGroupGeneration: snapshot.Devices[0].Generation,
		FencingToken:            1,
	}
}

func commandTestMonitor(t *testing.T, scanner *monitorScanner, instanceID string, generation uint64) (*Monitor, Snapshot) {
	t.Helper()
	monitor := newMonitor(scanner, instanceID, generation)
	snapshot, err := monitor.Refresh(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return monitor, snapshot
}

func TestCommandServicePersistsSuccessReplayConflictAndFence(t *testing.T) {
	scanner := &monitorScanner{devices: []DeviceReport{{
		ID: "usb-1-1", DisplayName: "QDC507", PhysicalPath: "1-1", Profile: ProfileQDC507,
	}}}
	monitor, snapshot := commandTestMonitor(t, scanner, "01234567-89ab-cdef-0123-456789abcdef", 10)
	store := openTestOutcomeStore(t, filepath.Join(t.TempDir(), "agent-state"), 16, 4)
	executor := &fakeRadioExecutor{execution: RadioEnsureOffExecution{
		Observation: RadioEnsureOffObservation{RF: RFObservation{State: RFStateOff, Mode: intPointerForAgentTest(4)}, ActiveCallCount: intPointerForAgentTest(0)},
	}}
	service := NewCommandService(monitor, executor, store)
	service.now = func() time.Time { return time.Date(2026, 8, 3, 13, 0, 0, 0, time.UTC) }
	request := commandRequestForSnapshot(snapshot)
	response, err := service.EnsureRadioOff(t.Context(), request)
	if err != nil || response.Outcome.State != CommandOutcomeSucceeded || response.Outcome.Code != OutcomeCodeRadioOffConfirmed || executor.calls != 1 {
		t.Fatalf("response=%#v calls=%d err=%v", response, executor.calls, err)
	}
	replayed, err := service.EnsureRadioOff(t.Context(), request)
	if err != nil || replayed.Outcome.State != CommandOutcomeSucceeded || executor.calls != 1 {
		t.Fatalf("replayed=%#v calls=%d err=%v", replayed, executor.calls, err)
	}
	conflict := request
	conflict.FencingToken++
	if _, err := service.EnsureRadioOff(t.Context(), conflict); !errors.Is(err, ErrOutcomeReplayConflict) {
		t.Fatalf("replay conflict error = %v", err)
	}
	second := request
	second.OperationID = "radio-off-command-2"
	second.FencingToken = 2
	if _, err := service.EnsureRadioOff(t.Context(), second); err != nil || executor.calls != 2 {
		t.Fatalf("second command calls=%d err=%v", executor.calls, err)
	}
	stale := request
	stale.OperationID = "radio-off-command-stale"
	stale.FencingToken = 2
	if _, err := service.EnsureRadioOff(t.Context(), stale); !errors.Is(err, ErrOutcomeFenceStale) {
		t.Fatalf("stale fence error = %v", err)
	}
	staleAgent := request
	staleAgent.OperationID = "radio-off-command-stale-agent"
	staleAgent.FencingToken = 3
	staleAgent.AgentInstanceID = "fedcba98-7654-3210-fedc-ba9876543210"
	if _, err := service.EnsureRadioOff(t.Context(), staleAgent); !errors.Is(err, ErrCommandAgentStale) {
		t.Fatalf("stale Agent error = %v", err)
	}
	staleSnapshot := request
	staleSnapshot.OperationID = "radio-off-command-stale-snapshot"
	staleSnapshot.FencingToken = 3
	staleSnapshot.SnapshotGeneration++
	if _, err := service.EnsureRadioOff(t.Context(), staleSnapshot); !errors.Is(err, ErrCommandSnapshotStale) {
		t.Fatalf("stale snapshot error = %v", err)
	}
	staleDevice := request
	staleDevice.OperationID = "radio-off-command-stale-device"
	staleDevice.FencingToken = 3
	staleDevice.DeviceGeneration++
	if _, err := service.EnsureRadioOff(t.Context(), staleDevice); !errors.Is(err, ErrCommandDeviceStale) {
		t.Fatalf("stale device error = %v", err)
	}
	invalid := request
	invalid.OperationID = ""
	if _, err := service.EnsureRadioOff(t.Context(), invalid); !errors.Is(err, ErrCommandRequestInvalid) {
		t.Fatalf("invalid request error = %v", err)
	}
}

func TestCommandServicePersistsFailedAndUncertainWithoutRedispatch(t *testing.T) {
	tests := []struct {
		name      string
		execution RadioEnsureOffExecution
		state     string
		code      string
	}{
		{
			name: "unconfirmed adapter result",
			execution: RadioEnsureOffExecution{
				Observation: RadioEnsureOffObservation{RF: RFObservation{State: RFStateOn}, ActiveCallCount: intPointerForAgentTest(0)},
			},
			state: CommandOutcomeFailed, code: ErrorRadioOffNotConfirmed,
		},
		{
			name: "active call",
			execution: RadioEnsureOffExecution{
				Observation: RadioEnsureOffObservation{RF: RFObservation{State: RFStateOn}, ActiveCallCount: intPointerForAgentTest(1)},
				Error:       &ProbeError{Layer: ErrorLayerCall, Code: ErrorActiveCallPresent, Retryable: true},
			},
			state: CommandOutcomeFailed, code: ErrorActiveCallPresent,
		},
		{
			name: "dispatch uncertain",
			execution: RadioEnsureOffExecution{
				Dispatched: true, Uncertain: true,
				Observation: RadioEnsureOffObservation{RF: RFObservation{State: RFStateUnknown}, ActiveCallCount: intPointerForAgentTest(0)},
				Error:       &ProbeError{Layer: ErrorLayerRadio, Code: ErrorRadioOffOutcomeUncertain},
			},
			state: CommandOutcomeUncertain, code: ErrorRadioOffOutcomeUncertain,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scanner := &monitorScanner{devices: []DeviceReport{{
				ID: "usb-1-1", DisplayName: "QDC507", PhysicalPath: "1-1", Profile: ProfileQDC507,
			}}}
			monitor, snapshot := commandTestMonitor(t, scanner, "01234567-89ab-cdef-0123-456789abcdef", 20)
			store := openTestOutcomeStore(t, filepath.Join(t.TempDir(), "agent-state"), 8, 2)
			executor := &fakeRadioExecutor{execution: test.execution}
			service := NewCommandService(monitor, executor, store)
			request := commandRequestForSnapshot(snapshot)
			first, err := service.EnsureRadioOff(t.Context(), request)
			if err != nil || first.Outcome.State != test.state || first.Outcome.Code != test.code || executor.calls != 1 {
				t.Fatalf("first=%#v calls=%d err=%v", first, executor.calls, err)
			}
			second, err := service.EnsureRadioOff(t.Context(), request)
			if err != nil || second.Outcome.State != test.state || executor.calls != 1 {
				t.Fatalf("replay=%#v calls=%d err=%v", second, executor.calls, err)
			}
		})
	}
}

func TestCommandServiceReconcilesPendingAfterAgentRestartWithoutRedispatch(t *testing.T) {
	firstScanner := &monitorScanner{devices: []DeviceReport{{
		ID: "usb-1-1", DisplayName: "QDC507", PhysicalPath: "1-1", Profile: ProfileQDC507,
	}}}
	_, firstSnapshot := commandTestMonitor(t, firstScanner, "01234567-89ab-cdef-0123-456789abcdef", 30)
	store := openTestOutcomeStore(t, filepath.Join(t.TempDir(), "agent-state"), 8, 2)
	request := commandRequestForSnapshot(firstSnapshot)
	digest := mustRadioEnsureOffDigest(t, request)
	if _, _, err := store.Accept(t.Context(), request, digest, time.Date(2026, 8, 3, 14, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	secondScanner := &monitorScanner{
		devices: []DeviceReport{{ID: "usb-1-1", DisplayName: "QDC507", PhysicalPath: "1-1", Profile: ProfileQDC507}},
		probes: []DeviceProbe{validCompleteProbeFixture(
			"usb-1-1", SIMObservation{State: SIMStatePresent, PrimaryLockState: PrimaryLockReady},
		)},
	}
	secondMonitor, secondSnapshot := commandTestMonitor(t, secondScanner, "fedcba98-7654-3210-fedc-ba9876543210", 300)
	if secondSnapshot.Revision != firstSnapshot.Revision || secondSnapshot.Generation == firstSnapshot.Generation {
		t.Fatalf("first=%#v second=%#v", firstSnapshot, secondSnapshot)
	}
	executor := &fakeRadioExecutor{}
	service := NewCommandService(secondMonitor, executor, store)
	service.now = func() time.Time { return time.Date(2026, 8, 3, 14, 0, 1, 0, time.UTC) }
	reconciled, err := service.ReconcilePending(t.Context())
	if err != nil || reconciled != 1 || executor.calls != 0 {
		t.Fatalf("reconciled=%d calls=%d err=%v", reconciled, executor.calls, err)
	}
	record, found, err := store.Find(t.Context(), request.OperationID)
	if err != nil || !found || record.Outcome.State != CommandOutcomeSucceeded || !record.Outcome.Reconciled {
		t.Fatalf("record=%#v found=%v err=%v", record, found, err)
	}
	replayed, err := service.EnsureRadioOff(t.Context(), request)
	if err != nil || replayed.Outcome.State != CommandOutcomeSucceeded || replayed.AgentInstanceID != secondSnapshot.AgentInstanceID || executor.calls != 0 {
		t.Fatalf("replayed=%#v calls=%d err=%v", replayed, executor.calls, err)
	}
}

func TestCommandServiceMarksPendingOutcomeUncertainWhenHardwareChanged(t *testing.T) {
	firstScanner := &monitorScanner{devices: []DeviceReport{{
		ID: "usb-1-1", DisplayName: "QDC507", PhysicalPath: "1-1", Profile: ProfileQDC507,
	}}}
	_, firstSnapshot := commandTestMonitor(t, firstScanner, "01234567-89ab-cdef-0123-456789abcdef", 40)
	store := openTestOutcomeStore(t, filepath.Join(t.TempDir(), "agent-state"), 8, 2)
	request := commandRequestForSnapshot(firstSnapshot)
	digest := mustRadioEnsureOffDigest(t, request)
	if _, _, err := store.Accept(t.Context(), request, digest, time.Date(2026, 8, 3, 15, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	changedScanner := &monitorScanner{devices: []DeviceReport{{
		ID: "usb-1-1", DisplayName: "QDC507 changed", PhysicalPath: "1-1", Profile: ProfileQDC507,
	}}}
	changedMonitor, _ := commandTestMonitor(t, changedScanner, "fedcba98-7654-3210-fedc-ba9876543210", 400)
	executor := &fakeRadioExecutor{}
	service := NewCommandService(changedMonitor, executor, store)
	service.now = func() time.Time { return time.Date(2026, 8, 3, 15, 0, 1, 0, time.UTC) }
	if reconciled, err := service.ReconcilePending(t.Context()); err != nil || reconciled != 1 {
		t.Fatalf("reconciled=%d err=%v", reconciled, err)
	}
	record, found, err := store.Find(t.Context(), request.OperationID)
	if err != nil || !found || record.Outcome.State != CommandOutcomeUncertain || record.Outcome.Code != ErrorHardwareChanged ||
		!record.Outcome.Reconciled || record.Outcome.Retryable || executor.calls != 0 {
		t.Fatalf("record=%#v found=%v calls=%d err=%v", record, found, executor.calls, err)
	}
}
