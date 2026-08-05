package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	domain "github.com/leonfox28/simplus/internal/domain/vowifi"
)

func TestVoWiFiDesirePersistsAcrossReopen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "db")
	ctx := context.Background()
	set, err := OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Desire{LineID: "agent-line-0123456789abcdef0123456789abcdef", DesiredActive: true, UpdatedAt: time.Date(2026, 8, 5, 4, 30, 0, 123, time.UTC)}
	if err := set.PutVoWiFiDesire(ctx, want); err != nil {
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
	values, err := set.ListVoWiFiDesires(ctx)
	if err != nil || len(values) != 1 || values[0] != want {
		t.Fatalf("values=%#v err=%v", values, err)
	}
	want.DesiredActive = false
	want.UpdatedAt = want.UpdatedAt.Add(time.Minute)
	if err := set.PutVoWiFiDesire(ctx, want); err != nil {
		t.Fatal(err)
	}
	values, _ = set.ListVoWiFiDesires(ctx)
	if len(values) != 1 || values[0] != want {
		t.Fatalf("updated values=%#v", values)
	}
}
