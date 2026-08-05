package smscodec

import (
	"errors"
	"fmt"
	"time"
)

type DeliverPDU struct {
	Sender     string
	ReceivedAt time.Time
	Segment    Segment
	DCS        byte
}

func DecodeDeliverPDU(pdu []byte) (DeliverPDU, error) {
	if len(pdu) < 2 {
		return DeliverPDU{}, errors.New("SMS-DELIVER PDU is too short")
	}
	smscLength := int(pdu[0])
	position := 1 + smscLength
	if smscLength < 0 || position >= len(pdu) {
		return DeliverPDU{}, errors.New("SMS-DELIVER PDU has an invalid SMSC field")
	}
	firstOctet := pdu[position]
	position++
	if firstOctet&0x03 != 0 {
		return DeliverPDU{}, errors.New("PDU is not an SMS-DELIVER TPDU")
	}
	if position+2 > len(pdu) {
		return DeliverPDU{}, errors.New("SMS-DELIVER PDU is missing its originating address")
	}
	addressLength := int(pdu[position])
	addressType := pdu[position+1]
	position += 2
	if addressLength < 1 || addressLength > 20 || addressType&0x80 == 0 {
		return DeliverPDU{}, errors.New("SMS-DELIVER PDU uses an unsupported originating address")
	}
	addressBytes := (addressLength + 1) / 2
	if position+addressBytes+10 > len(pdu) {
		return DeliverPDU{}, errors.New("SMS-DELIVER PDU is truncated before user data")
	}
	sender, err := decodeTPAddress(pdu[position:position+addressBytes], addressLength, addressType)
	if err != nil {
		return DeliverPDU{}, err
	}
	position += addressBytes
	position++ // TP-PID
	dcs := pdu[position]
	position++
	encoding, err := encodingForDCS(dcs)
	if err != nil {
		return DeliverPDU{}, err
	}
	receivedAt, err := decodeServiceCentreTimestamp(pdu[position : position+7])
	if err != nil {
		return DeliverPDU{}, err
	}
	position += 7
	userDataLength := int(pdu[position])
	position++
	userData := pdu[position:]
	segment, err := decodeDeliverUserData(encoding, firstOctet&0x40 != 0, userDataLength, userData)
	if err != nil {
		return DeliverPDU{}, err
	}
	return DeliverPDU{Sender: sender, ReceivedAt: receivedAt, Segment: segment, DCS: dcs}, nil
}

func decodeTPAddress(encoded []byte, addressLength int, addressType byte) (string, error) {
	if addressType&0x70 == 0x50 {
		// TS 23.040 9.1.2.5 expresses Address-Length in useful
		// semi-octets even when TP-OA contains a packed GSM 7-bit name.
		// This is the same conversion used by AOSP's GsmSmsAddress.
		septetCount := addressLength * 4 / 7
		packedBytes := (septetCount*7 + 7) / 8
		if septetCount < 1 || packedBytes > len(encoded) {
			return "", errors.New("SMS-DELIVER alphanumeric originating address has an invalid length")
		}
		septets, err := unpackSeptets(encoded[:packedBytes], septetCount, 0)
		if err != nil {
			return "", fmt.Errorf("decode SMS-DELIVER alphanumeric originating address: %w", err)
		}
		decoded, err := decodeGSM7Septets(septets)
		if err != nil || decoded == "" {
			return "", errors.New("SMS-DELIVER alphanumeric originating address is invalid")
		}
		return decoded, nil
	}

	decoded := make([]byte, 0, addressLength+1)
	if addressType&0x70 == 0x10 {
		decoded = append(decoded, '+')
	}
	for index := 0; index < addressLength; index++ {
		value := encoded[index/2]
		nibble := value & 0x0f
		if index%2 == 1 {
			nibble = value >> 4
		}
		if nibble > 9 {
			return "", errors.New("SMS-DELIVER originating address contains a non-decimal digit")
		}
		decoded = append(decoded, '0'+nibble)
	}
	if addressLength%2 == 1 && encoded[len(encoded)-1]>>4 != 0x0f {
		return "", errors.New("SMS-DELIVER originating address has an invalid filler nibble")
	}
	return string(decoded), nil
}

func encodingForDCS(dcs byte) (Encoding, error) {
	switch {
	case dcs&0xc0 == 0x00:
		if dcs&0x20 != 0 {
			break // Compressed TP-UD is intentionally unsupported.
		}
		switch (dcs >> 2) & 0x03 {
		case 0:
			return EncodingGSM7, nil
		case 2:
			return EncodingUCS2, nil
		}
	case dcs&0xf0 == 0xc0 || dcs&0xf0 == 0xd0:
		return EncodingGSM7, nil
	case dcs&0xf0 == 0xe0:
		return EncodingUCS2, nil
	case dcs&0xf0 == 0xf0 && dcs&0x04 == 0:
		return EncodingGSM7, nil
	}
	return "", fmt.Errorf("unsupported SMS data coding scheme 0x%02x", dcs)
}

func decodeServiceCentreTimestamp(encoded []byte) (time.Time, error) {
	if len(encoded) != 7 {
		return time.Time{}, errors.New("SMS service-centre timestamp is truncated")
	}
	values := make([]int, 6)
	for index := range values {
		value, err := swappedBCD(encoded[index])
		if err != nil {
			return time.Time{}, errors.New("SMS service-centre timestamp contains invalid BCD")
		}
		values[index] = value
	}
	timezone := encoded[6]
	negative := timezone&0x08 != 0
	timezone &^= 0x08
	quarters, err := swappedBCD(timezone)
	if err != nil || quarters > 79 {
		return time.Time{}, errors.New("SMS service-centre timestamp has an invalid timezone")
	}
	offset := quarters * 15 * 60
	if negative {
		offset = -offset
	}
	location := time.FixedZone("SMS-SCTS", offset)
	value := time.Date(2000+values[0], time.Month(values[1]), values[2], values[3], values[4], values[5], 0, location)
	if value.Year() != 2000+values[0] || int(value.Month()) != values[1] || value.Day() != values[2] ||
		value.Hour() != values[3] || value.Minute() != values[4] || value.Second() != values[5] {
		return time.Time{}, errors.New("SMS service-centre timestamp is outside the calendar range")
	}
	return value.UTC(), nil
}

func swappedBCD(value byte) (int, error) {
	low := int(value & 0x0f)
	high := int(value >> 4)
	if low > 9 || high > 9 {
		return 0, errors.New("invalid swapped BCD")
	}
	return low*10 + high, nil
}

func decodeDeliverUserData(encoding Encoding, hasHeader bool, userDataLength int, data []byte) (Segment, error) {
	if userDataLength < 1 || len(data) < 1 {
		return Segment{}, errors.New("SMS-DELIVER PDU has empty user data")
	}
	segment := Segment{Encoding: encoding, Part: 1, Total: 1}
	headerBytes := 0
	if hasHeader {
		if len(data) < 6 || data[0] != 0x05 || data[1] != 0x00 || data[2] != 0x03 || data[4] < 2 || data[5] < 1 || data[5] > data[4] {
			return Segment{}, errors.New("SMS-DELIVER PDU uses an unsupported user-data header")
		}
		headerBytes = 6
		segment.Reference = data[3]
		segment.Total = int(data[4])
		segment.Part = int(data[5])
	}

	switch encoding {
	case EncodingGSM7:
		headerSeptets := 0
		paddingBits := 0
		if hasHeader {
			headerSeptets = (headerBytes*8 + 6) / 7
			paddingBits = headerSeptets*7 - headerBytes*8
		}
		segment.UnitCount = userDataLength - headerSeptets
		if segment.UnitCount < 1 {
			return Segment{}, errors.New("SMS-DELIVER GSM7 user data has no message septets")
		}
		expectedPayload := (paddingBits + segment.UnitCount*7 + 7) / 8
		if len(data) != headerBytes+expectedPayload {
			return Segment{}, errors.New("SMS-DELIVER GSM7 user data length is inconsistent")
		}
	case EncodingUCS2:
		if len(data) != userDataLength || (userDataLength-headerBytes)%2 != 0 {
			return Segment{}, errors.New("SMS-DELIVER UCS-2 user data length is inconsistent")
		}
		segment.UnitCount = (userDataLength - headerBytes) / 2
		if segment.UnitCount < 1 {
			return Segment{}, errors.New("SMS-DELIVER UCS-2 user data is empty")
		}
	default:
		return Segment{}, errors.New("SMS-DELIVER PDU has an unsupported encoding")
	}
	segment.UserData = append([]byte(nil), data...)
	if encoding == EncodingGSM7 {
		if _, err := decodeGSM7Segment(segment); err != nil {
			return Segment{}, err
		}
	} else if _, err := decodeUCS2Segment(segment); err != nil {
		return Segment{}, err
	}
	return segment, nil
}
