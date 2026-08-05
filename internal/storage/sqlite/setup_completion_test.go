package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestCompleteInitialSetupAtomicallyRequiresEveryPrerequisite(t *testing.T) {
	ctx := context.Background()
	set, err := OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	const digest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if completed, err := set.CompleteInitialSetup(ctx, digest, now); err != nil || completed {
		t.Fatalf("completion without prerequisites = %t/%v", completed, err)
	}
	if err := set.ConfigureInitialAdministrator(ctx, "admin", "$argon2id$v1$m=8192,t=1,p=1$WlpaWlpaWlpaWlpaWlpaWg$WlpaWlpaWlpaWlpaWlpaWg", "en-US", now); err != nil {
		t.Fatal(err)
	}
	if completed, err := set.CompleteInitialSetup(ctx, digest, now); err != nil || completed {
		t.Fatalf("completion without storage = %t/%v", completed, err)
	}
	if err := set.ConfigureSetupStorage(ctx, set.Root, filepath.Join(set.Root, "recordings"), 1, 2, 1, 3, now); err != nil {
		t.Fatal(err)
	}
	completed, err := set.CompleteInitialSetup(ctx, digest, now)
	if err != nil || !completed {
		t.Fatalf("completion = %t/%v", completed, err)
	}
	var state string
	var initializedAt string
	var generation int
	if err := set.Core.QueryRowContext(ctx, `SELECT state, initialized_at_utc, instance_generation FROM installation_state WHERE singleton = 1`).Scan(&state, &initializedAt, &generation); err != nil {
		t.Fatal(err)
	}
	if state != InstallationReady || initializedAt == "" || generation != 2 {
		t.Fatalf("installation state = %q %q %d", state, initializedAt, generation)
	}
	if completed, err := set.CompleteInitialSetup(ctx, digest, now); err != nil || completed {
		t.Fatalf("second completion = %t/%v", completed, err)
	}
}
