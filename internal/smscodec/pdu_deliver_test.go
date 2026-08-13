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

func TestDecodeDeliverPDUAccepts16BitConcatenationReference(t *testing.T) {
	parts, err := Encode(repeatRune('长', 71))
	if err != nil || len(parts) != 2 {
		t.Fatalf("multipart fixture segments = %d, error = %v", len(parts), err)
	}
	const reference uint16 = 0x1234
	decodedParts := make([]Segment, 0, len(parts))
	for _, part := range parts {
		if len(part.UserData) < 6 {
			t.Fatal("multipart fixture has no UDH")
		}
		part.Reference = reference
		part.UserData = append([]byte{
			0x06, 0x08, 0x04, byte(reference >> 8), byte(reference & 0xff), byte(part.Total), byte(part.Part),
		}, part.UserData[6:]...)
		delivered, decodeErr := DecodeDeliverPDU(buildDeliverFixture(t, "+8613800138000", part))
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if delivered.Segment.Reference != reference || delivered.Segment.Total != 2 || delivered.Segment.Part != part.Part {
			t.Fatalf("16-bit concatenation envelope = %#v", delivered.Segment)
		}
		decodedParts = append(decodedParts, delivered.Segment)
	}
	body, err := Decode(decodedParts)
	if err != nil || body != repeatRune('长', 71) {
		t.Fatalf("16-bit multipart body length = %d, error = %v", len([]rune(body)), err)
	}
}

func TestDecodeDeliverPDUAccepts16BitGSM7ConcatenationReference(t *testing.T) {
	const text = "This is a GSM-7 multipart fixture. " +
		"It is deliberately long enough to cross the single-message boundary while preserving extension characters ^{}[]. " +
		"The second segment proves that a seven-byte 16-bit UDH uses zero padding bits."
	parts, err := Encode(text)
	if err != nil || len(parts) != 2 {
		t.Fatalf("multipart fixture segments = %d, error = %v", len(parts), err)
	}
	const reference uint16 = 0x1234
	decodedParts := make([]Segment, 0, len(parts))
	for _, part := range parts {
		if part.Encoding != EncodingGSM7 || len(part.UserData) < 6 {
			t.Fatalf("unexpected multipart fixture = %#v", part)
		}
		septets, unpackErr := unpackSeptets(part.UserData[6:], part.UnitCount, 1)
		if unpackErr != nil {
			t.Fatal(unpackErr)
		}
		part.Reference = reference
		part.UserData = append([]byte{
			0x06, 0x08, 0x04, byte(reference >> 8), byte(reference & 0xff), byte(part.Total), byte(part.Part),
		}, packSeptets(septets, 0)...)
		delivered, decodeErr := DecodeDeliverPDU(buildDeliverFixture(t, "+8613800138000", part))
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if delivered.Segment.Reference != reference || delivered.Segment.Total != 2 || delivered.Segment.Part != part.Part {
			t.Fatalf("16-bit concatenation envelope = %#v", delivered.Segment)
		}
		decodedParts = append(decodedParts, delivered.Segment)
	}
	body, err := Decode(decodedParts)
	if err != nil || body != text {
		t.Fatalf("16-bit GSM-7 multipart body mismatch, error = %v", err)
	}
}

func TestDecodeDeliverPDUAlphanumericOriginatingAddress(t *testing.T) {
	parts, err := Encode("Your one-time code is 123456")
	if err != nil {
		t.Fatal(err)
	}
	addressSeptets, ok := gsmTokens("VOXI")
	if !ok {
		t.Fatal("alphanumeric address fixture is not GSM7")
	}
	encodedSeptets := flattenByteTokens(addressSeptets)
	addressLength := (len(encodedSeptets)*7 + 3) / 4
	pdu := buildDeliverFixtureWithAddress(t, packSeptets(encodedSeptets, 0), 0xd0, addressLength, parts[0])

	delivered, err := DecodeDeliverPDU(pdu)
	if err != nil {
		t.Fatal(err)
	}
	if delivered.Sender != "VOXI" {
		t.Fatalf("sender = %q", delivered.Sender)
	}
	body, err := Decode([]Segment{delivered.Segment})
	if err != nil {
		t.Fatal(err)
	}
	if body != "Your one-time code is 123456" {
		t.Fatalf("body = %q", body)
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
	return buildDeliverFixtureWithAddress(t, address, addressType, digits, segment)
}

func buildDeliverFixtureWithAddress(t *testing.T, address []byte, addressType byte, addressLength int, segment Segment) []byte {
	t.Helper()
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
		if len(segment.UserData) < 1 {
			t.Fatal("multipart fixture has no user-data header")
		}
		headerBytes := int(segment.UserData[0]) + 1
		if headerBytes > len(segment.UserData) {
			t.Fatal("multipart fixture has a truncated user-data header")
		}
		userDataLength += (headerBytes*8 + 6) / 7
	}
	tpdu := []byte{firstOctet, byte(addressLength), addressType}
	tpdu = append(tpdu, address...)
	tpdu = append(tpdu, 0x00, dcs)
	// 2026-08-03 12:34:56 +08:00 in swapped BCD.
	tpdu = append(tpdu, 0x62, 0x80, 0x30, 0x21, 0x43, 0x65, 0x23, byte(userDataLength))
	tpdu = append(tpdu, segment.UserData...)
	return append([]byte{0x00}, tpdu...)
}
