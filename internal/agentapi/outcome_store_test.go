package agentapi

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func radioEnsureOffRequestFixture() RadioEnsureOffRequest {
	return RadioEnsureOffRequest{
		OperationID:             "radio-off-1",
		AgentInstanceID:         "01234567-89ab-cdef-0123-456789abcdef",
		SnapshotGeneration:      10,
		SnapshotRevision:        strings.Repeat("a", 64),
		DeviceID:                "usb-1-1",
		DeviceGeneration:        10,
		ResourceGroupID:         "agent-usb-1-1-resources",
		ResourceGroupGeneration: 10,
		FencingToken:            1,
	}
}

func openTestOutcomeStore(t *testing.T, directory string, maxOutcomes, maxGroups int) *OutcomeStore {
	t.Helper()
	store, err := OpenOutcomeStore(t.Context(), OutcomeStoreOptions{
		Directory: directory, MaxOutcomes: maxOutcomes, MaxResourceGroups: maxGroups,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestOutcomeStorePersistsReplayAndTerminalOutcome(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "agent-state")
	store := openTestOutcomeStore(t, directory, 8, 4)
	request := radioEnsureOffRequestFixture()
	digest := mustRadioEnsureOffDigest(t, request)
	now := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	record, replayed, err := store.Accept(t.Context(), request, digest, now)
	if err != nil || replayed || record.Outcome.State != CommandOutcomeAccepted {
		t.Fatalf("accepted=%#v replayed=%v err=%v", record, replayed, err)
	}
	count := 0
	outcome := terminalCommandOutcome(record, now.Add(time.Second), CommandOutcomeSucceeded, OutcomeCodeRadioOffConfirmed, "", false, false,
		RadioEnsureOffObservation{RF: RFObservation{State: RFStateOff, Mode: intPointerForAgentTest(4)}, ActiveCallCount: &count})
	if err := store.Complete(t.Context(), outcome); err != nil {
		t.Fatal(err)
	}
	terminal, found, err := store.Find(t.Context(), request.OperationID)
	if err != nil || !found || terminal.Outcome.State != CommandOutcomeSucceeded || terminal.Outcome.Observation.RF.Mode == nil || *terminal.Outcome.Observation.RF.Mode != 4 {
		t.Fatalf("terminal=%#v found=%v err=%v", terminal, found, err)
	}
	replayedRecord, replayed, err := store.Accept(t.Context(), request, digest, now.Add(2*time.Second))
	if err != nil || !replayed || replayedRecord.Outcome.State != CommandOutcomeSucceeded {
		t.Fatalf("replay=%#v replayed=%v err=%v", replayedRecord, replayed, err)
	}
	conflict := request
	conflict.FencingToken++
	conflictDigest := mustRadioEnsureOffDigest(t, conflict)
	if _, _, err := store.Accept(t.Context(), conflict, conflictDigest, now.Add(3*time.Second)); !errors.Is(err, ErrOutcomeReplayConflict) {
		t.Fatalf("replay conflict error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openTestOutcomeStore(t, directory, 8, 4)
	persisted, found, err := reopened.Find(t.Context(), request.OperationID)
	if err != nil || !found || persisted.Outcome.State != CommandOutcomeSucceeded || persisted.RequestDigest != digest {
		t.Fatalf("persisted=%#v found=%v err=%v", persisted, found, err)
	}
}

func TestOutcomeStoreBlocksPendingAndStaleFences(t *testing.T) {
	store := openTestOutcomeStore(t, filepath.Join(t.TempDir(), "agent-state"), 8, 2)
	now := time.Date(2026, 8, 3, 11, 0, 0, 0, time.UTC)
	first := radioEnsureOffRequestFixture()
	firstDigest := mustRadioEnsureOffDigest(t, first)
	record, _, err := store.Accept(t.Context(), first, firstDigest, now)
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.OperationID = "radio-off-2"
	second.FencingToken = 2
	secondDigest := mustRadioEnsureOffDigest(t, second)
	if _, _, err := store.Accept(t.Context(), second, secondDigest, now.Add(time.Second)); !errors.Is(err, ErrOutcomePending) {
		t.Fatalf("pending barrier error = %v", err)
	}
	failed := terminalCommandOutcome(record, now.Add(2*time.Second), CommandOutcomeFailed, ErrorActiveCallPresent, ErrorLayerCall, true, false,
		RadioEnsureOffObservation{RF: RFObservation{State: RFStateOn}, ActiveCallCount: intPointerForAgentTest(1)})
	if err := store.Complete(t.Context(), failed); err != nil {
		t.Fatal(err)
	}
	secondRecord, _, err := store.Accept(t.Context(), second, secondDigest, now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	secondOutcome := terminalCommandOutcome(secondRecord, now.Add(4*time.Second), CommandOutcomeSucceeded, OutcomeCodeRadioOffConfirmed, "", false, false,
		RadioEnsureOffObservation{RF: RFObservation{State: RFStateOff}, ActiveCallCount: intPointerForAgentTest(0)})
	if err := store.Complete(t.Context(), secondOutcome); err != nil {
		t.Fatal(err)
	}
	stale := first
	stale.OperationID = "radio-off-stale"
	staleDigest := mustRadioEnsureOffDigest(t, stale)
	if _, _, err := store.Accept(t.Context(), stale, staleDigest, now.Add(5*time.Second)); !errors.Is(err, ErrOutcomeFenceStale) {
		t.Fatalf("stale fence error = %v", err)
	}
}

func TestOutcomeStorePrunesTerminalRowsButRetainsFence(t *testing.T) {
	store := openTestOutcomeStore(t, filepath.Join(t.TempDir(), "agent-state"), 2, 1)
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	requests := make([]RadioEnsureOffRequest, 3)
	for index := range requests {
		request := radioEnsureOffRequestFixture()
		request.OperationID = "radio-off-" + string(rune('1'+index))
		request.FencingToken = uint64(index + 1)
		requests[index] = request
		digest := mustRadioEnsureOffDigest(t, request)
		record, _, err := store.Accept(t.Context(), request, digest, now.Add(time.Duration(index*2)*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		outcome := terminalCommandOutcome(record, now.Add(time.Duration(index*2+1)*time.Second), CommandOutcomeSucceeded,
			OutcomeCodeRadioOffConfirmed, "", false, false,
			RadioEnsureOffObservation{RF: RFObservation{State: RFStateOff}, ActiveCallCount: intPointerForAgentTest(0)})
		if err := store.Complete(t.Context(), outcome); err != nil {
			t.Fatal(err)
		}
	}
	if _, found, err := store.Find(t.Context(), requests[0].OperationID); err != nil || found {
		t.Fatalf("oldest outcome found=%v err=%v", found, err)
	}
	oldDigest := mustRadioEnsureOffDigest(t, requests[0])
	if _, _, err := store.Accept(t.Context(), requests[0], oldDigest, now.Add(10*time.Second)); !errors.Is(err, ErrOutcomeFenceStale) {
		t.Fatalf("pruned replay fence error = %v", err)
	}
	newGroup := radioEnsureOffRequestFixture()
	newGroup.OperationID = "radio-off-new-group"
	newGroup.ResourceGroupID = "agent-usb-2-1-resources"
	newGroup.FencingToken = 1
	newGroupDigest := mustRadioEnsureOffDigest(t, newGroup)
	if _, _, err := store.Accept(t.Context(), newGroup, newGroupDigest, now.Add(11*time.Second)); !errors.Is(err, ErrOutcomeLedgerFull) {
		t.Fatalf("resource-group capacity error = %v", err)
	}
}

func intPointerForAgentTest(value int) *int { return &value }

func mustRadioEnsureOffDigest(t *testing.T, request RadioEnsureOffRequest) string {
	t.Helper()
	digest, err := radioEnsureOffDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}
