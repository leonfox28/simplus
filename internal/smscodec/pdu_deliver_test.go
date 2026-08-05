package smscodec

import (
	"encoding/hex"
	"testing"
	"time"
)

func TestDecodeDeliverPDUFromQuectelPDUTranscript(t *testing.T) {
	pdu, err := hex.DecodeString("0891683108200105F0040D91685120012194F600F10180817144302304F4F29C0E")
	if err != nil {
		t.Fatal(err)
	}
	delivered, err := DecodeDeliverPDU(pdu)
	if err != nil {
		t.Fatal(err)
	}
	if delivered.Sender != "+8615021012496" || delivered.DCS != 0xf1 || !delivered.ReceivedAt.Equal(time.Date(2010, 8, 18, 9, 44, 3, 0, time.UTC)) {
		t.Fatalf("delivery envelope = %#v", delivered)
	}
	body, err := Decode([]Segment{delivered.Segment})
	if err != nil {
		t.Fatal(err)
	}
	if body != "test" {
		t.Fatalf("body = %q", body)
	}
}

func TestDecodeDeliverPDUMultipartRoundTrip(t *testing.T) {
	// Use two independently valid UCS-2 segments so the decoder exercises UDH
	// parsing without relying on a modem or network transcript.
	parts, err := Encode("这是第一段内容。" + repeatRune('短', 65))
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 {
		t.Fatalf("multipart fixture segments = %d", len(parts))
	}
	decodedParts := make([]Segment, 0, len(parts))
	for _, part := range parts {
		pdu := buildDeliverFixture(t, "+8613800138000", part)
		delivered, err := DecodeDeliverPDU(pdu)
		if err != nil {
			t.Fatal(err)
		}
		decodedParts = append(decodedParts, delivered.Segment)
	}
	body, err := Decode(decodedParts)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := Decode(parts)
	if err != nil {
		t.Fatal(err)
	}
	if body != expected {
		t.Fatalf("multipart body mismatch")
	}
}

func TestDecodeDeliverPDURejectsUnsupportedOrMalformedPayload(t *testing.T) {
	fixture, err := hex.DecodeString("0891683108200105F0040D91685120012194F600F10180817144302304F4F29C0E")
	if err != nil {
		t.Fatal(err)
	}
	wrongType := append([]byte(nil), fixture...)
	wrongType[9] = 0x01
	if _, err := DecodeDeliverPDU(wrongType); err == nil {
		t.Fatal("SMS-SUBMIT was accepted as SMS-DELIVER")
	}
	brokenLength := append([]byte(nil), fixture...)
	brokenLength[len(brokenLength)-5]++
	if _, err := DecodeDeliverPDU(brokenLength); err == nil {
		t.Fatal("broken user-data length was accepted")
	}
	invalidAddressType := append([]byte(nil), fixture...)
	invalidAddressType[11] &^= 0x80
	if _, err := DecodeDeliverPDU(invalidAddressType); err == nil {
		t.Fatal("originating address without extension bit was accepted")
	}
	compressedDCS := append([]byte(nil), fixture...)
	compressedDCS[20] = 0x20
	if _, err := DecodeDeliverPDU(compressedDCS); err == nil {
		t.Fatal("compressed SMS user data was accepted")
	}
}

func repeatRune(character rune, count int) string {
	result := make([]rune, count)
	for index := range result {
		result[index] = character
	}
	return string(result)
}

func buildDeliverFixture(t *testing.T, sender string, segment Segment) []byte {
	t.Helper()
	address, addressType, digits, err := encodeTPAddress(sender)
	if err != nil {
		t.Fatal(err)
	}
	firstOctet := byte(0x00)
	if segment.Total > 1 {
		firstOctet |= 0x40
	}
	dcs := byte(0x00)
	userDataLength := segment.UnitCount
	if segment.Encoding == EncodingUCS2 {
		dcs = 0x08
		userDataLength = len(segment.UserData)
	} else if segment.Total > 1 {
		userDataLength += 7
	}
	tpdu := []byte{firstOctet, byte(digits), addressType}
	tpdu = append(tpdu, address...)
	tpdu = append(tpdu, 0x00, dcs)
	// 2026-08-03 12:34:56 +08:00 in swapped BCD.
	tpdu = append(tpdu, 0x62, 0x80, 0x30, 0x21, 0x43, 0x65, 0x23, byte(userDataLength))
	tpdu = append(tpdu, segment.UserData...)
	return append([]byte{0x00}, tpdu...)
}
