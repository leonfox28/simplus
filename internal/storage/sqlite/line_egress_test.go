package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/domain/lineegress"
)

func TestLineEgressBindingPersistsWithoutSubscriptionIdentity(t *testing.T) {
	root := filepath.Join(t.TempDir(), "db")
	stores, err := OpenSet(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	binding := lineegress.Binding{
		LineID: "agent-line-123", Mode: lineegress.ModeMihomoCountry, CountryCode: "GB", UpdatedAt: time.Unix(100, 0).UTC(),
	}
	if err := stores.UpsertLineEgressBinding(context.Background(), binding); err != nil {
		t.Fatal(err)
	}
	if err := stores.Close(); err != nil {
		t.Fatal(err)
	}

	stores, err = OpenSet(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()
	items, err := stores.ListLineEgressBindings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].LineID != binding.LineID || items[0].Mode != lineegress.ModeMihomoCountry || items[0].CountryCode != "GB" || !items[0].UpdatedAt.Equal(binding.UpdatedAt) {
		t.Fatalf("bindings = %#v", items)
	}
}
