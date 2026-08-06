package main

import (
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
)

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

func TestHardwareRuntimeRequiresTypedRFControlAndRejectsUnapprovedMutationFeatures(t *testing.T) {
	if err := requireTypedHardwareAgent(agentapi.Hello{Features: []string{agentapi.FeatureRFControl}}); err != nil {
		t.Fatalf("typed RF Agent rejected: %v", err)
	}
	for _, features := range [][]string{
		{},
		{agentapi.FeatureSMS, agentapi.FeatureRFControl},
		{agentapi.CommandRadioEnsureOff, agentapi.FeatureRFControl},
		{"durable-command-outcomes", agentapi.FeatureRFControl},
	} {
		if err := requireTypedHardwareAgent(agentapi.Hello{Features: features}); err == nil {
			t.Fatalf("unsafe Agent features accepted: %#v", features)
		}
	}
}
