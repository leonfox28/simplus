package smscodec

import (
	"slices"
	"strings"
	"testing"
)

func TestGSM7RoundTripAndSegmentBoundaries(t *testing.T) {
	for name, test := range map[string]struct {
		text  string
		parts int
	}{
		"basic single":     {text: strings.Repeat("a", 160), parts: 1},
		"basic multipart":  {text: strings.Repeat("a", 161), parts: 2},
		"extension single": {text: strings.Repeat("^", 80), parts: 1},
		"extension multi":  {text: strings.Repeat("^", 81), parts: 2},
		"alphabet":         {text: "Hello @ £ Δ {}[]~|€\n", parts: 1},
	} {
		t.Run(name, func(t *testing.T) {
			segments, err := Encode(test.text)
			if err != nil {
				t.Fatal(err)
			}
			if len(segments) != test.parts {
				t.Fatalf("segments = %d, want %d", len(segments), test.parts)
			}
			for _, segment := range segments {
				if segment.Encoding != EncodingGSM7 {
					t.Fatalf("encoding = %q", segment.Encoding)
				}
				limit := gsmSingleSeptets
				if len(segments) > 1 {
					limit = gsmConcatSeptets
				}
				if segment.UnitCount > limit {
					t.Fatalf("unit count = %d, limit = %d", segment.UnitCount, limit)
				}
			}
			decoded, err := Decode(slices.Clone(segments))
			if err != nil {
				t.Fatal(err)
			}
			if decoded != test.text {
				t.Fatalf("decoded = %q", decoded)
			}
		})
	}
}

func TestUCS2RoundTripPreservesSurrogatePairs(t *testing.T) {
	for name, test := range map[string]struct {
		text  string
		parts int
	}{
		"chinese single": {text: strings.Repeat("短", 70), parts: 1},
		"chinese multi":  {text: strings.Repeat("短", 71), parts: 2},
		"emoji single":   {text: strings.Repeat("📱", 35), parts: 1},
		"emoji multi":    {text: strings.Repeat("📱", 36), parts: 2},
	} {
		t.Run(name, func(t *testing.T) {
			segments, err := Encode(test.text)
			if err != nil {
				t.Fatal(err)
			}
			if len(segments) != test.parts {
				t.Fatalf("segments = %d, want %d", len(segments), test.parts)
			}
			for _, segment := range segments {
				if segment.Encoding != EncodingUCS2 {
					t.Fatalf("encoding = %q", segment.Encoding)
				}
				limit := ucs2SingleUnits
				if len(segments) > 1 {
					limit = ucs2ConcatUnits
				}
				if segment.UnitCount > limit {
					t.Fatalf("unit count = %d, limit = %d", segment.UnitCount, limit)
				}
			}
			reversed := slices.Clone(segments)
			slices.Reverse(reversed)
			decoded, err := Decode(reversed)
			if err != nil {
				t.Fatal(err)
			}
			if decoded != test.text {
				t.Fatalf("decoded differs after round trip")
			}
		})
	}
}

func TestDecodeRejectsBrokenConcatenationEnvelope(t *testing.T) {
	segments, err := Encode(strings.Repeat("a", 161))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(segments[:1]); err == nil {
		t.Fatal("Decode accepted a missing segment")
	}
	corrupt := slices.Clone(segments)
	corrupt[1].UserData = slices.Clone(corrupt[1].UserData)
	corrupt[1].UserData[3]++
	if _, err := Decode(corrupt); err == nil {
		t.Fatal("Decode accepted a conflicting UDH reference")
	}
}

func TestDecodeSegmentValidatesOneMultipartPart(t *testing.T) {
	segments, err := Encode(strings.Repeat("a", 161))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeSegment(segments[0])
	if err != nil || decoded != strings.Repeat("a", 153) {
		t.Fatalf("decoded=%q error=%v", decoded, err)
	}
	segments[0].UserData[3]++
	if _, err := DecodeSegment(segments[0]); err == nil {
		t.Fatal("DecodeSegment accepted a conflicting UDH")
	}
}

func TestEncodeRejectsUnboundedOrInvalidText(t *testing.T) {
	for _, text := range []string{"", strings.Repeat("短", 1601), string([]byte{0xff})} {
		if _, err := Encode(text); err == nil {
			t.Fatalf("Encode accepted invalid text with %d bytes", len(text))
		}
	}
}
