package main

import (
	"testing"

	"github.com/leonfox28/simplus/internal/agentapi"
)

func TestHardwareRuntimeRequiresReadOnlyAgentAndRejectsMutationFeatures(t *testing.T) {
	if err := requireReadOnlyAgent(agentapi.Hello{Features: []string{agentapi.FeatureHardwareReadOnly}}); err != nil {
		t.Fatalf("read-only Agent rejected: %v", err)
	}
	for _, features := range [][]string{
		{},
		{agentapi.FeatureSMS, agentapi.FeatureHardwareReadOnly},
		{agentapi.CommandRadioEnsureOff, agentapi.FeatureHardwareReadOnly},
		{"durable-command-outcomes", agentapi.FeatureHardwareReadOnly},
	} {
		if err := requireReadOnlyAgent(agentapi.Hello{Features: features}); err == nil {
			t.Fatalf("unsafe Agent features accepted: %#v", features)
		}
	}
}
