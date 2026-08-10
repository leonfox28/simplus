package pagination

import (
	"encoding/base64"
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

func TestSMSCursorRoundTripAndLegacyCompatibility(t *testing.T) {
	want := Cursor{RecordSequence: 42, ID: "message_abcdefghijklmnop"}
	encoded, err := EncodeSMS(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeSMS(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got.RecordSequence != want.RecordSequence || got.ID != want.ID || !got.CreatedAt.IsZero() {
		t.Fatalf("SMS cursor round trip = %#v, want %#v", got, want)
	}
	if _, err := Decode(encoded); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("Calls decoder accepted SMS cursor: %v", err)
	}

	legacy := Cursor{CreatedAt: time.UnixMilli(1_800_000_000_123).UTC(), ID: want.ID}
	legacyEncoded, err := Encode(legacy)
	if err != nil {
		t.Fatal(err)
	}
	legacyDecoded, err := DecodeSMS(legacyEncoded)
	if err != nil {
		t.Fatal(err)
	}
	if legacyDecoded.RecordSequence != 0 || legacyDecoded.ID != legacy.ID || !legacyDecoded.CreatedAt.Equal(legacy.CreatedAt) {
		t.Fatalf("legacy SMS cursor = %#v, want %#v", legacyDecoded, legacy)
	}
}

func TestSMSCursorRejectsInvalidKindSequenceAndBounds(t *testing.T) {
	valid, err := EncodeSMS(Cursor{RecordSequence: 7, ID: "message_abcdefghijklmnop"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := base64.RawURLEncoding.DecodeString(valid)
	if err != nil {
		t.Fatal(err)
	}
	wrongKind := append([]byte(nil), payload...)
	wrongKind[1] = 'c'
	zeroSequence := append([]byte(nil), payload...)
	for index := 2; index < smsCursorPrefixLen; index++ {
		zeroSequence[index] = 0
	}
	for _, encoded := range []string{
		base64.RawURLEncoding.EncodeToString(wrongKind),
		base64.RawURLEncoding.EncodeToString(zeroSequence),
		strings.Repeat("a", MaximumCursorLen+1),
	} {
		if _, err := DecodeSMS(encoded); !errors.Is(err, ErrCursorInvalid) {
			t.Fatalf("DecodeSMS(%q) error = %v, want ErrCursorInvalid", encoded, err)
		}
	}
	if _, err := EncodeSMS(Cursor{RecordSequence: 0, ID: "message_abcdefghijklmnop"}); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("EncodeSMS invalid sequence error = %v, want ErrCursorInvalid", err)
	}
}
