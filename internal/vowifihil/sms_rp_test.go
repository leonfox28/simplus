package vowifihil

import (
	"bytes"
	"testing"
)

func TestBuildRPDataSubmitUsesSIMServiceCentreAndRawTPDU(t *testing.T) {
	tpdu := []byte{0x01, 0x2a, 0x05, 0x91, 0x21, 0x43, 0xf5, 0x00, 0x00, 0x01, 0x41}
	packet, err := BuildRPDataSubmit(0x17, "+447700900123", tpdu)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x00, 0x17, 0x00, 0x07, 0x91, 0x44, 0x77, 0x00, 0x09, 0x10, 0x32, byte(len(tpdu))}
	want = append(want, tpdu...)
	if !bytes.Equal(packet, want) {
		t.Fatalf("RP-DATA = %x, want %x", packet, want)
	}
}

func TestParseNetworkRPDataAndDeliveryACK(t *testing.T) {
	tpdu := []byte{0x00, 0x04, 0x91, 0x21, 0x43, 0x00, 0x00, 0x62, 0x80, 0x50, 0x21, 0x43, 0x65, 0x00, 0x01, 0x41}
	packet := []byte{0x01, 0x29, 0x07, 0x91, 0x44, 0x77, 0x00, 0x09, 0x10, 0x32, 0x00, byte(len(tpdu))}
	packet = append(packet, tpdu...)
	message, err := ParseNetworkRPMessage(packet)
	if err != nil {
		t.Fatal(err)
	}
	if message.Type != rpDataNetworkToMS || message.Reference != 0x29 || message.OriginatorAddress != "+447700900123" || !bytes.Equal(message.UserData, tpdu) {
		t.Fatalf("message = %#v", message)
	}
	if ack := BuildRPDeliveryACK(message.Reference); !bytes.Equal(ack, []byte{0x02, 0x29, 0x41, 0x02, 0x00, 0x00}) {
		t.Fatalf("RP-ACK = %x", ack)
	}
}

func TestParseNetworkRPAckAndError(t *testing.T) {
	ack, err := ParseNetworkRPMessage([]byte{0x03, 0x07, 0x41, 0x02, 0x01, 0x00})
	if err != nil || ack.Type != rpACKNetworkToMS || ack.Reference != 7 || !bytes.Equal(ack.UserData, []byte{0x01, 0x00}) {
		t.Fatalf("ack=%#v error=%v", ack, err)
	}
	rpError, err := ParseNetworkRPMessage([]byte{0x05, 0x08, 0x01, 0x15})
	if err != nil || rpError.Type != rpErrorNetworkToMS || rpError.Reference != 8 || rpError.Cause != 0x15 {
		t.Fatalf("error=%#v parse=%v", rpError, err)
	}
	rpError, err = ParseNetworkRPMessage([]byte{0x05, 0x09, 0x02, 0x29, 0x80, 0x41, 0x02, 0x00, 0x00})
	if err != nil || rpError.Reference != 9 || rpError.Cause != 0x29 || !bytes.Equal(rpError.UserData, []byte{0x00, 0x00}) {
		t.Fatalf("diagnostic error=%#v parse=%v", rpError, err)
	}
}

func TestRPCodecRejectsWrongDirectionAndMalformedLengths(t *testing.T) {
	for _, packet := range [][]byte{
		{0x00, 0x01},
		{0x01, 0x01, 0x01, 0x91, 0x00, 0x01, 0x00},
		{0x01, 0x01, 0x02, 0x91, 0x21, 0x01, 0x01, 0x00},
		{0x03, 0x01, 0x41, 0x02, 0x00},
		{0x05, 0x01, 0x00},
		{0x05, 0x01, 0x03, 0x15, 0x00, 0x00},
		{0x05, 0x01, 0x01, 0x15, 0x41, 0x02, 0x00},
	} {
		if _, err := ParseNetworkRPMessage(packet); err == nil {
			t.Fatalf("accepted malformed RP message %x", packet)
		}
	}
	if _, err := BuildRPDataSubmit(1, "not-a-number", []byte{1}); err == nil {
		t.Fatal("accepted invalid service centre")
	}
	if _, err := BuildRPDataSubmit(1, "+447700900123", make([]byte, maxRPUserDataSize+1)); err == nil {
		t.Fatal("accepted oversized TPDU")
	}
}
