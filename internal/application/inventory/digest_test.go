package inventory

import (
	"context"
	"errors"
	"testing"

	"github.com/leonfox28/simplus/internal/domain/accessmode"
)

func TestSetupDigestCoversTopologyAndAccessModeWithoutObservationTime(t *testing.T) {
	store := &memoryAccessModes{values: map[string]accessmode.Mode{"simulator-profile-1": accessmode.HoldRFOff}}
	service := NewSimulator(store)
	first, err := service.Topology(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Topology(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Revision) != 64 || first.Revision != second.Revision {
		t.Fatalf("topology revisions = %q %q", first.Revision, second.Revision)
	}
	firstDigest, err := SetupDigest(first)
	if err != nil {
		t.Fatal(err)
	}
	secondDigest, err := SetupDigest(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstDigest != first.Revision {
		t.Fatalf("safe setup digest %s != topology revision %s", firstDigest, first.Revision)
	}
	if firstDigest != secondDigest {
		t.Fatalf("observation time changed digest: %s != %s", firstDigest, secondDigest)
	}
	second.ResourceGroups[0].MaxActiveCalls++
	changedDigest, err := SetupDigest(second)
	if err != nil {
		t.Fatal(err)
	}
	if changedDigest == firstDigest {
		t.Fatal("resource topology change did not change setup digest")
	}
	store.values["simulator-profile-1"] = accessmode.HostVoWiFiOnly
	modeChanged, err := service.Topology(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if modeChanged.Generation != first.Generation || modeChanged.Revision == first.Revision {
		t.Fatalf("mode change generation/revision = %d/%s", modeChanged.Generation, modeChanged.Revision)
	}
}

func TestSetupDigestRequiresEveryProfileAccessModeAndRFOff(t *testing.T) {
	service := NewSimulator(&memoryAccessModes{values: map[string]accessmode.Mode{}})
	topology, err := service.Topology(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(topology.Revision) != 64 {
		t.Fatalf("unconfigured topology revision = %q", topology.Revision)
	}
	if _, err := SetupDigest(topology); !errors.Is(err, ErrSetupTopologyUnsafe) {
		t.Fatalf("unconfigured profile error = %v", err)
	}
	topology.SubscriptionProfiles[0].AccessModeConfigured = true
	topology.SubscriptionProfiles[0].AccessMode = accessmode.HoldRFOff
	topology.Lines[0].AccessModeConfigured = true
	topology.Lines[0].AccessMode = accessmode.HoldRFOff
	topology.Lines[0].RFSafety = "on"
	if _, err := SetupDigest(topology); !errors.Is(err, ErrSetupTopologyUnsafe) {
		t.Fatalf("RF-on topology error = %v", err)
	}
}
