package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSetupHardwareReviewPersists(t *testing.T) {
	ctx := context.Background()
	set, err := OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	if _, _, _, reviewed, err := set.ReadSetupHardwareReview(ctx); err != nil || reviewed {
		t.Fatalf("initial hardware review = %t/%v", reviewed, err)
	}
	const digest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := set.ConfirmSetupHardware(ctx, digest, 1, 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	storedDigest, devices, lines, reviewed, err := set.ReadSetupHardwareReview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reviewed || storedDigest != digest || devices != 1 || lines != 1 {
		t.Fatalf("hardware review = %q %d %d %t", storedDigest, devices, lines, reviewed)
	}
}
