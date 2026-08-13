package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/application/messaging"
	"github.com/leonfox28/simplus/internal/application/realtime"
)

type oneAgentChange struct{ cancel context.CancelFunc }

func (source oneAgentChange) Snapshot(context.Context, bool) (agentapi.Snapshot, error) {
	return agentapi.Snapshot{ProtocolVersion: agentapi.ProtocolVersion, AgentInstanceID: "01234567-89ab-cdef-0123-456789abcdef", Generation: 1}, nil
}
func (source oneAgentChange) Changes(context.Context, string, uint64, int) (agentapi.ChangeResponse, error) {
	source.cancel()
	return agentapi.ChangeResponse{Changed: true, Snapshot: agentapi.Snapshot{
		ProtocolVersion: agentapi.ProtocolVersion, AgentInstanceID: "01234567-89ab-cdef-0123-456789abcdef", Generation: 2,
	}}, nil
}

func TestManagementListenDerivesMihomoControllerAndSocketFamily(t *testing.T) {
	tests := []struct {
		management string
		controller string
		network    string
	}{
		{management: "0.0.0.0:8080", controller: "0.0.0.0:19090", network: "tcp4"},
		{management: "192.168.50.10:8080", controller: "192.168.50.10:19090", network: "tcp4"},
		{management: "[fd00::10]:8080", controller: "[fd00::10]:19090", network: "tcp6"},
	}
	for _, test := range tests {
		t.Run(test.management, func(t *testing.T) {
			controller, err := mihomoControllerAddress(test.management)
			if err != nil || controller != test.controller {
				t.Fatalf("controller=%q err=%v", controller, err)
			}
			if network := managementListenerNetwork(test.management); network != test.network {
				t.Fatalf("network=%q", network)
			}
		})
	}
	if _, err := mihomoControllerAddress("localhost:8080"); err == nil {
		t.Fatal("accepted hostname management address")
	}
}

func TestInboundSMSRetryDelayBacksOffAndCaps(t *testing.T) {
	delay := time.Duration(0)
	want := []time.Duration{15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 5 * time.Minute, 5 * time.Minute}
	for index, expected := range want {
		delay = nextSMSSyncRetryDelay(delay, 2*time.Second)
		if delay != expected {
			t.Fatalf("delay %d = %s, want %s", index, delay, expected)
		}
	}
	if got := nextSMSSyncRetryDelay(0, 10*time.Second); got != 40*time.Second {
		t.Fatalf("long interval retry = %s", got)
	}
}

func TestAgentChangeWatchPublishesBoundedInventoryTopics(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	hub := realtime.NewHub()
	subscription := hub.Subscribe()
	defer subscription.Close()
	<-subscription.C
	runAgentChanges(ctx, oneAgentChange{cancel: cancel}, hub, slog.New(slog.NewTextHandler(io.Discard, nil)))
	event := <-subscription.C
	if event.Kind != realtime.KindUpdate || len(event.Topics) != 3 || event.Attention != "" {
		t.Fatalf("agent event = %#v", event)
	}
	if nextAgentRetryDelay(time.Second) != 2*time.Second || nextAgentRetryDelay(20*time.Second) != 30*time.Second {
		t.Fatal("agent retry delay did not back off and cap")
	}
}

func TestPartialSMSSyncProgressStillPublishesDurableState(t *testing.T) {
	hub := realtime.NewHub()
	subscription := hub.Subscribe()
	defer subscription.Close()
	<-subscription.C
	if !publishSMSSyncResult(hub, messaging.InboundSyncResult{Persisted: 1}) {
		t.Fatal("persisted partial sync was not treated as a durable change")
	}
	event := <-subscription.C
	if event.Kind != realtime.KindUpdate || len(event.Topics) != 1 || event.Topics[0] != realtime.TopicMessages ||
		event.Attention != realtime.AttentionSMSReceived {
		t.Fatalf("partial sync event = %#v", event)
	}
	if publishSMSSyncResult(hub, messaging.InboundSyncResult{Acknowledged: 1}) {
		t.Fatal("acknowledgement-only sync published a business-state change")
	}
}

func TestHardwareRuntimeRequiresTypedRFControlAndRejectsUnapprovedMutationFeatures(t *testing.T) {
	required := []string{agentapi.FeatureRFControl, agentapi.FeatureEquipmentIdentityRead, agentapi.FeatureSMS}
	if err := requireTypedHardwareAgent(agentapi.Hello{Features: required}); err != nil {
		t.Fatalf("typed hardware Agent rejected: %v", err)
	}
	for _, features := range [][]string{
		{},
		{agentapi.FeatureRFControl, agentapi.FeatureEquipmentIdentityRead},
		{agentapi.FeatureRFControl, agentapi.FeatureSMS},
		{agentapi.FeatureEquipmentIdentityRead, agentapi.FeatureSMS},
		append(append([]string(nil), required...), agentapi.CommandRadioEnsureOff),
		append(append([]string(nil), required...), "durable-command-outcomes"),
	} {
		if err := requireTypedHardwareAgent(agentapi.Hello{Features: features}); err == nil {
			t.Fatalf("unsafe Agent features accepted: %#v", features)
		}
	}
}
