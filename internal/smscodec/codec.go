package smscodec

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

type Encoding string

const (
	EncodingGSM7 Encoding = "gsm7"
	EncodingUCS2 Encoding = "ucs2"

	gsmSingleSeptets = 160
	gsmConcatSeptets = 153
	ucs2SingleUnits  = 70
	ucs2ConcatUnits  = 67
)

type Segment struct {
	Encoding  Encoding
	Reference byte
	Part      int
	Total     int
	UnitCount int
	UserData  []byte
}

var (
	gsmDefaultRunes   = []rune("@£$¥èéùìòÇ\nØø\rÅåΔ_ΦΓΛΩΠΨΣΘΞ\x1bÆæßÉ !\"#¤%&'()*+,-./0123456789:;<=>?¡ABCDEFGHIJKLMNOPQRSTUVWXYZÄÖÑÜ§¿abcdefghijklmnopqrstuvwxyzäöñüà")
	gsmExtensionRunes = map[byte]rune{
		0x0a: '\f', 0x14: '^', 0x28: '{', 0x29: '}', 0x2f: '\\',
		0x3c: '[', 0x3d: '~', 0x3e: ']', 0x40: '|', 0x65: '€',
	}
	gsmDefaultCodes   map[rune]byte
	gsmExtensionCodes map[rune]byte
)

func init() {
	if len(gsmDefaultRunes) != 128 {
		panic("GSM 03.38 default alphabet must contain 128 entries")
	}
	gsmDefaultCodes = make(map[rune]byte, len(gsmDefaultRunes))
	for code, character := range gsmDefaultRunes {
		if character != '\x1b' {
			gsmDefaultCodes[character] = byte(code)
		}
	}
	gsmExtensionCodes = make(map[rune]byte, len(gsmExtensionRunes))
	for code, character := range gsmExtensionRunes {
		gsmExtensionCodes[character] = code
	}
}

func Encode(text string) ([]Segment, error) {
	if text == "" || !utf8.ValidString(text) || utf8.RuneCountInString(text) > 1600 {
		return nil, errors.New("SMS text must contain from 1 through 1600 valid Unicode characters")
	}
	if tokens, ok := gsmTokens(text); ok {
		return encodeGSM7(text, tokens), nil
	}
	return encodeUCS2(text), nil
}

func Decode(segments []Segment) (string, error) {
	ordered, err := validateAndOrder(segments)
	if err != nil {
		return "", err
	}
	var decoded strings.Builder
	for _, segment := range ordered {
		var text string
		switch segment.Encoding {
		case EncodingGSM7:
			text, err = decodeGSM7Segment(segment)
		case EncodingUCS2:
			text, err = decodeUCS2Segment(segment)
		default:
			err = fmt.Errorf("unsupported SMS encoding %q", segment.Encoding)
		}
		if err != nil {
			return "", fmt.Errorf("decode SMS segment %d: %w", segment.Part, err)
		}
		decoded.WriteString(text)
	}
	return decoded.String(), nil
}

func encodeGSM7(text string, tokens [][]byte) []Segment {
	limit := gsmSingleSeptets
	if tokenUnits(tokens) > gsmSingleSeptets {
		limit = gsmConcatSeptets
	}
	parts := splitByteTokens(tokens, limit)
	reference := concatenationReference(text, len(parts))
	segments := make([]Segment, 0, len(parts))
	for index, part := range parts {
		septets := flattenByteTokens(part)
		segment := Segment{
			Encoding: EncodingGSM7, Reference: reference, Part: index + 1, Total: len(parts), UnitCount: len(septets),
		}
		if len(parts) == 1 {
			segment.UserData = packSeptets(septets, 0)
		} else {
			segment.UserData = append(concatenationHeader(reference, len(parts), index+1), packSeptets(septets, 1)...)
		}
		segments = append(segments, segment)
	}
	return segments
}

type ucs2Token []uint16

func encodeUCS2(text string) []Segment {
	tokens := make([]ucs2Token, 0, len([]rune(text)))
	unitCount := 0
	for _, character := range text {
		units := ucs2Token(utf16.Encode([]rune{character}))
		tokens = append(tokens, units)
		unitCount += len(units)
	}
	limit := ucs2SingleUnits
	if unitCount > ucs2SingleUnits {
		limit = ucs2ConcatUnits
	}
	parts := splitUCS2Tokens(tokens, limit)
	reference := concatenationReference(text, len(parts))
	segments := make([]Segment, 0, len(parts))
	for index, part := range parts {
		units := flattenUCS2Tokens(part)
		data := make([]byte, len(units)*2)
		for unitIndex, unit := range units {
			binary.BigEndian.PutUint16(data[unitIndex*2:], unit)
		}
		if len(parts) > 1 {
			data = append(concatenationHeader(reference, len(parts), index+1), data...)
		}
		segments = append(segments, Segment{
			Encoding: EncodingUCS2, Reference: reference, Part: index + 1, Total: len(parts), UnitCount: len(units), UserData: data,
		})
	}
	return segments
}

func gsmTokens(text string) ([][]byte, bool) {
	tokens := make([][]byte, 0, len([]rune(text)))
	for _, character := range text {
		if code, ok := gsmDefaultCodes[character]; ok {
			tokens = append(tokens, []byte{code})
			continue
		}
		if code, ok := gsmExtensionCodes[character]; ok {
			tokens = append(tokens, []byte{0x1b, code})
			continue
		}
		return nil, false
	}
	return tokens, true
}

func tokenUnits(tokens [][]byte) int {
	total := 0
	for _, token := range tokens {
		total += len(token)
	}
	return total
}

func splitByteTokens(tokens [][]byte, limit int) [][][]byte {
	parts := make([][][]byte, 0, 1)
	current := make([][]byte, 0)
	units := 0
	for _, token := range tokens {
		if units != 0 && units+len(token) > limit {
			parts = append(parts, current)
			current = make([][]byte, 0)
			units = 0
		}
		current = append(current, token)
		units += len(token)
	}
	return append(parts, current)
}

func flattenByteTokens(tokens [][]byte) []byte {
	units := make([]byte, 0, tokenUnits(tokens))
	for _, token := range tokens {
		units = append(units, token...)
	}
	return units
}

func splitUCS2Tokens(tokens []ucs2Token, limit int) [][]ucs2Token {
	parts := make([][]ucs2Token, 0, 1)
	current := make([]ucs2Token, 0)
	units := 0
	for _, token := range tokens {
		if units != 0 && units+len(token) > limit {
			parts = append(parts, current)
			current = make([]ucs2Token, 0)
			units = 0
		}
		current = append(current, token)
		units += len(token)
	}
	return append(parts, current)
}

func flattenUCS2Tokens(tokens []ucs2Token) []uint16 {
	count := 0
	for _, token := range tokens {
		count += len(token)
	}
	units := make([]uint16, 0, count)
	for _, token := range tokens {
		units = append(units, token...)
	}
	return units
}

func packSeptets(septets []byte, paddingBits int) []byte {
	packed := make([]byte, (paddingBits+len(septets)*7+7)/8)
	for index, septet := range septets {
		bit := paddingBits + index*7
		byteIndex := bit / 8
		shift := bit % 8
		value := septet & 0x7f
		packed[byteIndex] |= value << shift
		if shift > 1 {
			packed[byteIndex+1] |= value >> (8 - shift)
		}
	}
	return packed
}

func unpackSeptets(data []byte, count, paddingBits int) ([]byte, error) {
	expected := (paddingBits + count*7 + 7) / 8
	if count < 0 || len(data) != expected {
		return nil, errors.New("GSM7 user data length does not match its septet count")
	}
	septets := make([]byte, count)
	for index := range count {
		bit := paddingBits + index*7
		byteIndex := bit / 8
		shift := bit % 8
		value := uint16(data[byteIndex]) >> shift
		if shift > 1 {
			value |= uint16(data[byteIndex+1]) << (8 - shift)
		}
		septets[index] = byte(value & 0x7f)
	}
	return septets, nil
}

func decodeGSM7Segment(segment Segment) (string, error) {
	data, padding, err := segmentPayload(segment)
	if err != nil {
		return "", err
	}
	limit := gsmSingleSeptets
	if segment.Total > 1 {
		limit = gsmConcatSeptets
	}
	if segment.UnitCount < 1 || segment.UnitCount > limit {
		return "", errors.New("GSM7 septet count is outside the segment limit")
	}
	septets, err := unpackSeptets(data, segment.UnitCount, padding)
	if err != nil {
		return "", err
	}
	var decoded strings.Builder
	for index := 0; index < len(septets); index++ {
		code := septets[index]
		if code != 0x1b {
			decoded.WriteRune(gsmDefaultRunes[code])
			continue
		}
		index++
		if index == len(septets) {
			return "", errors.New("GSM7 extension escape is missing its extension code")
		}
		character, ok := gsmExtensionRunes[septets[index]]
		if !ok {
			return "", fmt.Errorf("unknown GSM7 extension code 0x%02x", septets[index])
		}
		decoded.WriteRune(character)
	}
	return decoded.String(), nil
}

func decodeUCS2Segment(segment Segment) (string, error) {
	data, _, err := segmentPayload(segment)
	if err != nil {
		return "", err
	}
	limit := ucs2SingleUnits
	if segment.Total > 1 {
		limit = ucs2ConcatUnits
	}
	if segment.UnitCount < 1 || segment.UnitCount > limit || len(data) != segment.UnitCount*2 {
		return "", errors.New("UCS-2 user data length does not match its UTF-16 unit count")
	}
	units := make([]uint16, segment.UnitCount)
	for index := range units {
		units[index] = binary.BigEndian.Uint16(data[index*2:])
	}
	if err := validateUTF16(units); err != nil {
		return "", err
	}
	return string(utf16.Decode(units)), nil
}

func validateAndOrder(segments []Segment) ([]Segment, error) {
	if len(segments) == 0 || len(segments) > 255 {
		return nil, errors.New("SMS must contain from 1 through 255 segments")
	}
	ordered := append([]Segment(nil), segments...)
	first := ordered[0]
	if first.Total != len(ordered) || first.Total < 1 || first.Total > 255 || first.Part < 1 || first.Part > first.Total {
		return nil, errors.New("invalid SMS concatenation envelope")
	}
	seen := make(map[int]struct{}, len(ordered))
	for _, segment := range ordered {
		if segment.Encoding != first.Encoding || segment.Reference != first.Reference || segment.Total != first.Total ||
			segment.Part < 1 || segment.Part > segment.Total {
			return nil, errors.New("SMS segments do not share one concatenation envelope")
		}
		if _, duplicate := seen[segment.Part]; duplicate {
			return nil, errors.New("SMS contains a duplicate segment")
		}
		seen[segment.Part] = struct{}{}
	}
	sort.Slice(ordered, func(left, right int) bool { return ordered[left].Part < ordered[right].Part })
	for index, segment := range ordered {
		if segment.Part != index+1 {
			return nil, errors.New("SMS is missing a segment")
		}
	}
	return ordered, nil
}

func segmentPayload(segment Segment) ([]byte, int, error) {
	if segment.Total == 1 {
		if segment.Part != 1 || segment.Reference != 0 {
			return nil, 0, errors.New("single-part SMS has an invalid concatenation envelope")
		}
		return segment.UserData, 0, nil
	}
	if len(segment.UserData) < 6 {
		return nil, 0, errors.New("concatenated SMS is missing its UDH")
	}
	header := segment.UserData[:6]
	if header[0] != 0x05 || header[1] != 0x00 || header[2] != 0x03 || header[3] != segment.Reference ||
		int(header[4]) != segment.Total || int(header[5]) != segment.Part {
		return nil, 0, errors.New("concatenated SMS UDH does not match its envelope")
	}
	padding := 0
	if segment.Encoding == EncodingGSM7 {
		padding = 1
	}
	return segment.UserData[6:], padding, nil
}

func concatenationReference(text string, total int) byte {
	if total == 1 {
		return 0
	}
	digest := sha256.Sum256([]byte(text))
	return digest[0]
}

func concatenationHeader(reference byte, total, part int) []byte {
	return []byte{0x05, 0x00, 0x03, reference, byte(total), byte(part)}
}

func validateUTF16(units []uint16) error {
	for index := 0; index < len(units); index++ {
		unit := units[index]
		switch {
		case 0xd800 <= unit && unit <= 0xdbff:
			index++
			if index == len(units) || units[index] < 0xdc00 || units[index] > 0xdfff {
				return errors.New("UCS-2 payload contains an unpaired high surrogate")
			}
		case 0xdc00 <= unit && unit <= 0xdfff:
			return errors.New("UCS-2 payload contains an unpaired low surrogate")
		}
	}
	return nil
}
