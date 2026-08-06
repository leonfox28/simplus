package inventory

import (
	"testing"
)

func TestSetupDigestCoversTopologyWithoutObservationTime(t *testing.T) {
	service := NewSimulator()
	first, err := service.Topology(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Topology(t.Context())
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
	if firstDigest != first.Revision || firstDigest != secondDigest {
		t.Fatalf("setup digests = %q %q, revision = %q", firstDigest, secondDigest, first.Revision)
	}
	second.ResourceGroups[0].MaxActiveCalls++
	changedDigest, err := SetupDigest(second)
	if err != nil {
		t.Fatal(err)
	}
	if changedDigest == firstDigest {
		t.Fatal("resource topology change did not change setup digest")
	}
}
