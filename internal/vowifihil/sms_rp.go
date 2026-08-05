package vowifihil

import (
	"errors"
	"regexp"
	"strings"
)

const (
	rpDataMSToNetwork  = byte(0x00)
	rpDataNetworkToMS  = byte(0x01)
	rpACKMSToNetwork   = byte(0x02)
	rpACKNetworkToMS   = byte(0x03)
	rpErrorNetworkToMS = byte(0x05)

	rpUserDataIEI     = byte(0x41)
	maxRPUserDataSize = 232
)

var rpAddressPattern = regexp.MustCompile(`^\+?[0-9]{3,20}$`)

// RPMessage is the bounded subset of 3GPP TS 24.011 RP messages needed by
// SMS over IMS. UserData contains a TPDU, never a modem PDU with an SMSC
// prefix.
type RPMessage struct {
	Type              byte
	Reference         byte
	OriginatorAddress string
	UserData          []byte
	Cause             byte
}

// BuildRPDataSubmit wraps one SMS-SUBMIT TPDU in an MS-to-network RP-DATA.
// The originating address is empty in this direction; serviceCentre is the
// TS-Service-Centre-Address obtained from the SIM.
func BuildRPDataSubmit(reference byte, serviceCentre string, tpdu []byte) ([]byte, error) {
	address, err := encodeRPAddress(serviceCentre)
	if err != nil || len(tpdu) == 0 || len(tpdu) > maxRPUserDataSize {
		return nil, errors.New("invalid RP-DATA submit input")
	}
	packet := make([]byte, 0, 5+len(address)+len(tpdu))
	packet = append(packet, rpDataMSToNetwork, reference, 0x00, byte(len(address)))
	packet = append(packet, address...)
	packet = append(packet, byte(len(tpdu)))
	packet = append(packet, tpdu...)
	return packet, nil
}

// BuildRPDeliveryACK creates the positive RP delivery report required after
// an ordinary inbound SMS-DELIVER has been durably persisted. A successful
// SMS-DELIVER-REPORT contains TP-MTI followed by an empty TP-PI. PID, DCS and
// UDL are only present when they are needed, such as for USIM data download.
func BuildRPDeliveryACK(reference byte) []byte {
	return []byte{rpACKMSToNetwork, reference, rpUserDataIEI, 0x02, 0x00, 0x00}
}

// ParseNetworkRPMessage accepts only network-to-MS RP-DATA, RP-ACK and
// RP-ERROR. Directionally invalid or trailing data is rejected fail closed.
func ParseNetworkRPMessage(packet []byte) (RPMessage, error) {
	if len(packet) < 2 || len(packet) > 255 {
		return RPMessage{}, errors.New("invalid network RP message")
	}
	message := RPMessage{Type: packet[0] & 0x07, Reference: packet[1]}
	if packet[0]&0xf8 != 0 {
		return RPMessage{}, errors.New("invalid network RP message")
	}
	switch message.Type {
	case rpDataNetworkToMS:
		if err := parseRPDataNetworkToMS(packet[2:], &message); err != nil {
			return RPMessage{}, err
		}
	case rpACKNetworkToMS:
		userData, err := parseOptionalRPUserData(packet[2:])
		if err != nil {
			return RPMessage{}, err
		}
		message.UserData = userData
	case rpErrorNetworkToMS:
		if err := parseRPErrorNetworkToMS(packet[2:], &message); err != nil {
			return RPMessage{}, err
		}
	default:
		return RPMessage{}, errors.New("unsupported network RP message")
	}
	return message, nil
}

func parseRPDataNetworkToMS(data []byte, message *RPMessage) error {
	if len(data) < 4 {
		return errors.New("invalid network RP-DATA")
	}
	originatorLength := int(data[0])
	if originatorLength < 2 || originatorLength > 11 || len(data) < 1+originatorLength+2 {
		return errors.New("invalid network RP-DATA originator")
	}
	originator, err := decodeRPAddress(data[1 : 1+originatorLength])
	if err != nil {
		return errors.New("invalid network RP-DATA originator")
	}
	position := 1 + originatorLength
	if data[position] != 0x00 {
		return errors.New("invalid network RP-DATA destination")
	}
	position++
	userDataLength := int(data[position])
	position++
	if userDataLength < 1 || userDataLength > maxRPUserDataSize || position+userDataLength != len(data) {
		return errors.New("invalid network RP-DATA user data")
	}
	message.OriginatorAddress = originator
	message.UserData = append([]byte(nil), data[position:]...)
	return nil
}

func parseRPErrorNetworkToMS(data []byte, message *RPMessage) error {
	if len(data) < 2 {
		return errors.New("invalid network RP-ERROR")
	}
	causeLength := int(data[0])
	if causeLength < 1 || causeLength > 2 || len(data) < 1+causeLength {
		return errors.New("invalid network RP-ERROR")
	}
	message.Cause = data[1] & 0x7f
	userData, err := parseOptionalRPUserData(data[1+causeLength:])
	if err != nil {
		return err
	}
	message.UserData = userData
	return nil
}

func parseOptionalRPUserData(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if len(data) < 2 || data[0] != rpUserDataIEI {
		return nil, errors.New("invalid RP user data")
	}
	length := int(data[1])
	if length < 1 || length > maxRPUserDataSize || len(data) != length+2 {
		return nil, errors.New("invalid RP user data")
	}
	return append([]byte(nil), data[2:]...), nil
}

func encodeRPAddress(value string) ([]byte, error) {
	if !rpAddressPattern.MatchString(value) {
		return nil, errors.New("invalid RP address")
	}
	typeOfAddress := byte(0x81)
	digits := value
	if strings.HasPrefix(value, "+") {
		typeOfAddress = 0x91
		digits = value[1:]
	}
	encoded := make([]byte, 1+(len(digits)+1)/2)
	encoded[0] = typeOfAddress
	for index := 0; index < len(digits); index += 2 {
		low := digits[index] - '0'
		high := byte(0x0f)
		if index+1 < len(digits) {
			high = digits[index+1] - '0'
		}
		encoded[1+index/2] = low | high<<4
	}
	if len(encoded) < 2 || len(encoded) > 11 {
		return nil, errors.New("invalid RP address")
	}
	return encoded, nil
}

func decodeRPAddress(encoded []byte) (string, error) {
	if len(encoded) < 2 || len(encoded) > 11 || encoded[0]&0x80 == 0 {
		return "", errors.New("invalid RP address")
	}
	prefix := ""
	switch encoded[0] {
	case 0x91:
		prefix = "+"
	case 0x81:
	default:
		return "", errors.New("unsupported RP address")
	}
	var digits strings.Builder
	for index, current := range encoded[1:] {
		low, high := current&0x0f, current>>4
		if low > 9 {
			return "", errors.New("invalid RP address")
		}
		digits.WriteByte('0' + low)
		if high == 0x0f {
			if index != len(encoded)-2 {
				return "", errors.New("invalid RP address filler")
			}
			continue
		}
		if high > 9 {
			return "", errors.New("invalid RP address")
		}
		digits.WriteByte('0' + high)
	}
	result := prefix + digits.String()
	if !rpAddressPattern.MatchString(result) {
		return "", errors.New("invalid RP address")
	}
	return result, nil
}
