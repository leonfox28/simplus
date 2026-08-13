package smscodec

import (
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
)

var pduDestinationPattern = regexp.MustCompile(`^\+?[0-9]{3,20}$`)

type SubmitPDU struct {
	Bytes        []byte
	TPDULength   int
	UserDataSize int
}

func (pdu SubmitPDU) Hex() string {
	return strings.ToUpper(hex.EncodeToString(pdu.Bytes))
}

// EncodeSubmitPDU wraps one already encoded Segment in a 3GPP SMS-SUBMIT PDU.
// The leading zero-length SMSC field tells the modem to use its configured
// service centre; TPDULength therefore excludes that first octet.
func EncodeSubmitPDU(destination string, segment Segment, messageReference byte) (SubmitPDU, error) {
	address, addressType, digitCount, err := encodeTPAddress(destination)
	if err != nil {
		return SubmitPDU{}, err
	}
	if segment.Total < 1 || segment.Total > 255 || segment.Part < 1 || segment.Part > segment.Total {
		return SubmitPDU{}, errors.New("SMS segment has an invalid concatenation envelope")
	}
	if segment.Total == 1 && (segment.Part != 1 || segment.Reference != 0) {
		return SubmitPDU{}, errors.New("single-part SMS has an invalid concatenation envelope")
	}
	if segment.Total > 1 && segment.Reference > 255 {
		return SubmitPDU{}, errors.New("outbound SMS requires an 8-bit concatenation reference")
	}
	firstOctet := byte(0x01) // SMS-SUBMIT with no validity period.
	dataCodingScheme := byte(0)
	userDataLength := 0
	switch segment.Encoding {
	case EncodingGSM7:
		if _, err := decodeGSM7Segment(segment); err != nil {
			return SubmitPDU{}, err
		}
		userDataLength = segment.UnitCount
		if segment.Total > 1 {
			firstOctet |= 0x40  // TP-UDHI
			userDataLength += 7 // Six UDH octets occupy seven GSM septets.
		}
	case EncodingUCS2:
		if _, err := decodeUCS2Segment(segment); err != nil {
			return SubmitPDU{}, err
		}
		dataCodingScheme = 0x08
		userDataLength = len(segment.UserData)
		if segment.Total > 1 {
			firstOctet |= 0x40 // TP-UDHI
		}
	default:
		return SubmitPDU{}, errors.New("SMS segment uses an unsupported encoding")
	}
	if len(segment.UserData) < 1 || len(segment.UserData) > 140 || userDataLength < 1 || userDataLength > 255 {
		return SubmitPDU{}, errors.New("SMS segment exceeds the TP-UD limit")
	}

	tpdu := make([]byte, 0, 10+len(address)+len(segment.UserData))
	tpdu = append(tpdu, firstOctet, messageReference, byte(digitCount), addressType)
	tpdu = append(tpdu, address...)
	tpdu = append(tpdu, 0x00, dataCodingScheme, byte(userDataLength)) // TP-PID, TP-DCS, TP-UDL
	tpdu = append(tpdu, segment.UserData...)
	pduWithSMSC := append([]byte{0x00}, tpdu...)
	return SubmitPDU{Bytes: pduWithSMSC, TPDULength: len(tpdu), UserDataSize: len(segment.UserData)}, nil
}

func encodeTPAddress(destination string) ([]byte, byte, int, error) {
	if !pduDestinationPattern.MatchString(destination) {
		return nil, 0, 0, errors.New("SMS destination is invalid")
	}
	addressType := byte(0x81)
	digits := destination
	if strings.HasPrefix(destination, "+") {
		addressType = 0x91
		digits = destination[1:]
	}
	encoded := make([]byte, (len(digits)+1)/2)
	for index := range encoded {
		low := digits[index*2] - '0'
		high := byte(0x0f)
		if index*2+1 < len(digits) {
			high = digits[index*2+1] - '0'
		}
		encoded[index] = low | high<<4
	}
	return encoded, addressType, len(digits), nil
}
