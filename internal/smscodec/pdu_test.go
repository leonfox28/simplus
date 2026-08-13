package smscodec

import (
	"slices"
	"strings"
	"testing"
)

func TestEncodeSubmitPDUGSM7Fixture(t *testing.T) {
	segments, err := Encode("hello")
	if err != nil {
		t.Fatal(err)
	}
	pdu, err := EncodeSubmitPDU("+8613800138000", segments[0], 0)
	if err != nil {
		t.Fatal(err)
	}
	if pdu.Hex() != "0001000D91683108108300F0000005E8329BFD06" {
		t.Fatalf("PDU = %s", pdu.Hex())
	}
	if pdu.TPDULength != len(pdu.Bytes)-1 || pdu.TPDULength != 19 || pdu.UserDataSize != 5 {
		t.Fatalf("PDU envelope = %#v", pdu)
	}
}

func TestEncodeSubmitPDUUCS2Fixture(t *testing.T) {
	segments, err := Encode("短")
	if err != nil {
		t.Fatal(err)
	}
	pdu, err := EncodeSubmitPDU("10086", segments[0], 7)
	if err != nil {
		t.Fatal(err)
	}
	if pdu.Hex() != "00010705810180F600080277ED" {
		t.Fatalf("PDU = %s", pdu.Hex())
	}
	if pdu.TPDULength != 12 || pdu.UserDataSize != 2 {
		t.Fatalf("PDU envelope = %#v", pdu)
	}
}

func TestEncodeSubmitPDUMultipartKeepsUDHAndLimits(t *testing.T) {
	for name, text := range map[string]string{
		"gsm7": strings.Repeat("a", 161),
		"ucs2": strings.Repeat("短", 71),
	} {
		t.Run(name, func(t *testing.T) {
			segments, err := Encode(text)
			if err != nil {
				t.Fatal(err)
			}
			if len(segments) != 2 {
				t.Fatalf("segments = %d", len(segments))
			}
			for index, segment := range segments {
				pdu, err := EncodeSubmitPDU("+8613800138000", segment, byte(40+index))
				if err != nil {
					t.Fatal(err)
				}
				if pdu.Bytes[1]&0x40 == 0 || pdu.UserDataSize > 140 || !slices.Equal(segment.UserData[:6], []byte{
					0x05, 0x00, 0x03, byte(segment.Reference), byte(segment.Total), byte(segment.Part),
				}) {
					t.Fatalf("multipart PDU = %#v segment = %#v", pdu, segment)
				}
			}
		})
	}
}

func TestEncodeSubmitPDURejectsInvalidInput(t *testing.T) {
	segments, err := Encode("hello")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EncodeSubmitPDU("+12", segments[0], 0); err == nil {
		t.Fatal("short destination was accepted")
	}
	corrupt := segments[0]
	corrupt.UserData = slices.Clone(corrupt.UserData[:len(corrupt.UserData)-1])
	if _, err := EncodeSubmitPDU("10086", corrupt, 0); err == nil {
		t.Fatal("corrupt GSM7 payload was accepted")
	}
	multipart, err := Encode(strings.Repeat("a", 161))
	if err != nil || len(multipart) != 2 {
		t.Fatalf("multipart fixture = %#v, error = %v", multipart, err)
	}
	multipart[0].Reference = 256
	if _, err := EncodeSubmitPDU("10086", multipart[0], 0); err == nil {
		t.Fatal("16-bit outbound concatenation reference was accepted")
	}
}
