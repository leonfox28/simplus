package pagination

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCursorRoundTrip(t *testing.T) {
	want := Cursor{CreatedAt: time.UnixMilli(1_800_000_000_123).UTC(), ID: "call_abcdefghijklmnop"}
	encoded, err := Encode(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || !got.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("cursor round trip = %#v, want %#v", got, want)
	}
}

func TestCursorRejectsMalformedVersionAndBounds(t *testing.T) {
	for _, encoded := range []string{"%%%", "AgAAAAAAAAABY2FsbF9hYmNkZWZnaGlqa2xtbm9w", strings.Repeat("a", MaximumCursorLen+1)} {
		if _, err := Decode(encoded); !errors.Is(err, ErrCursorInvalid) {
			t.Fatalf("Decode(%q) error = %v, want ErrCursorInvalid", encoded, err)
		}
	}
	if _, err := Encode(Cursor{CreatedAt: time.Now(), ID: "short"}); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("Encode invalid ID error = %v, want ErrCursorInvalid", err)
	}
}
