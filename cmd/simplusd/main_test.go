package main

import (
	"testing"

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
