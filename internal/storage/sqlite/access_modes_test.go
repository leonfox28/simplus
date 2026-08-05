package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/leonfox28/simplus/internal/domain/accessmode"
)

func TestSubscriptionProfileAccessModePersists(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "db")
	set, err := OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}

	if mode, configured, err := set.SubscriptionProfileAccessMode(ctx, "simulator-profile-1"); err != nil {
		t.Fatal(err)
	} else if configured || mode != "" {
		t.Fatalf("unconfigured mode = %q, configured = %v", mode, configured)
	}
	if err := set.PutSubscriptionProfileAccessMode(ctx, "simulator-profile-1", accessmode.HostVoWiFiOnly); err != nil {
		t.Fatal(err)
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}

	set, err = OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	mode, configured, err := set.SubscriptionProfileAccessMode(ctx, "simulator-profile-1")
	if err != nil {
		t.Fatal(err)
	}
	if !configured || mode != accessmode.HostVoWiFiOnly {
		t.Fatalf("persisted mode = %q, configured = %v", mode, configured)
	}
	modes, err := set.SubscriptionProfileAccessModes(ctx, []string{"simulator-profile-1", "missing-profile", "simulator-profile-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(modes) != 1 || modes["simulator-profile-1"] != accessmode.HostVoWiFiOnly {
		t.Fatalf("bulk access modes = %#v", modes)
	}
}

func TestPutSubscriptionProfileAccessModeRejectsInvalidMode(t *testing.T) {
	set, err := OpenSet(context.Background(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	if err := set.PutSubscriptionProfileAccessMode(context.Background(), "simulator-profile-1", accessmode.Mode("automatic")); err == nil {
		t.Fatal("PutSubscriptionProfileAccessMode accepted an invalid mode")
	}
}
