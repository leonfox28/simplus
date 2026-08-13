package modemadapter

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/leonfox28/simplus/internal/attransport"
)

const usimServiceProviderNameFileID = 0x6f46

var (
	simGSMDefaultRunes   = []rune("@£$¥èéùìòÇ\nØø\rÅåΔ_ΦΓΛΩΠΨΣΘΞ\x1bÆæßÉ !\"#¤%&'()*+,-./0123456789:;<=>?¡ABCDEFGHIJKLMNOPQRSTUVWXYZÄÖÑÜ§¿abcdefghijklmnopqrstuvwxyzäöñüà")
	simGSMExtensionRunes = map[byte]rune{
		0x0a: '\f', 0x14: '^', 0x28: '{', 0x29: '}', 0x2f: '\\',
		0x3c: '[', 0x3d: '~', 0x3e: ']', 0x40: '|', 0x65: '€',
	}
)

// readML307AHomeOperator returns best-effort metadata for the active profile.
// EF_SPN is the human-facing provider label. MCC/MNC is derived locally from
// IMSI plus EF_AD and only the bounded PLMN code leaves this function.
func readSIMHomeOperator(ctx context.Context, query attransport.Query) (string, string) {
	if query == nil {
		return "", ""
	}
	name := readSIMServiceProviderName(ctx, query)
	code := readSIMHomeOperatorCode(ctx, query)
	return name, code
}

func readSIMServiceProviderName(ctx context.Context, query attransport.Query) string {
	command := fmt.Sprintf("AT+CRSM=176,%d,0,0,17", usimServiceProviderNameFileID)
	lines, err := query(ctx, command, 3*time.Second)
	if err != nil || !attransport.HasTerminalOK(lines) {
		return ""
	}
	sw1, _, encoded, ok := parseCRSMResponse(lines)
	if !ok || sw1 != 0x90 && sw1 != 0x91 || len(encoded) != 34 {
		return ""
	}
	data, err := hex.DecodeString(encoded)
	if err != nil || len(data) != 17 {
		zeroSIMAKABytesLocal(data)
		return ""
	}
	defer zeroSIMAKABytesLocal(data)
	return normalizeSIMOperatorName(decodeSIMAlphaIdentifier(data[1:]))
}

func readSIMHomeOperatorCode(ctx context.Context, query attransport.Query) string {
	lines, err := query(ctx, "AT+CIMI", 2*time.Second)
	if err != nil {
		return ""
	}
	imsi := parseML307AIMSI(lines)
	if imsi == "" {
		return ""
	}
	mncLength, err := readSIMMNCLength(ctx, query)
	if err != nil || len(imsi) < 3+mncLength {
		return ""
	}
	return imsi[:3] + "-" + imsi[3:3+mncLength]
}

// Kept as a compatibility seam for the SIM AKA implementation and its focused
// tests. Operator metadata itself is model-neutral and is also used by QDC507.
func readML307AHomeOperator(ctx context.Context, query attransport.Query) (string, string) {
	return readSIMHomeOperator(ctx, query)
}

func decodeSIMAlphaIdentifier(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	switch data[0] {
	case 0x80:
		return decodeSIMUCS2(data[1:])
	case 0x81:
		if len(data) < 3 {
			return ""
		}
		return decodeSIMCompactUCS2(data[3:], int(data[1]), rune(data[2]&0x7f)<<7)
	case 0x82:
		if len(data) < 4 {
			return ""
		}
		base := rune(data[2])<<8 | rune(data[3])
		return decodeSIMCompactUCS2(data[4:], int(data[1]), base)
	default:
		return decodeSIMGSM(data)
	}
}

func decodeSIMUCS2(data []byte) string {
	if len(data)%2 != 0 {
		if data[len(data)-1] != 0xff {
			return ""
		}
		data = data[:len(data)-1]
	}
	units := make([]uint16, 0, len(data)/2)
	for index := 0; index < len(data); index += 2 {
		unit := uint16(data[index])<<8 | uint16(data[index+1])
		if unit == 0xffff {
			break
		}
		units = append(units, unit)
	}
	return string(utf16.Decode(units))
}

func decodeSIMCompactUCS2(data []byte, count int, base rune) string {
	if count < 1 || count > len(data) {
		return ""
	}
	var decoded strings.Builder
	for _, value := range data[:count] {
		if value == 0xff {
			break
		}
		if value&0x80 != 0 {
			decoded.WriteRune(base + rune(value&0x7f))
			continue
		}
		if value == 0x1b || int(value) >= len(simGSMDefaultRunes) {
			return ""
		}
		decoded.WriteRune(simGSMDefaultRunes[value])
	}
	return decoded.String()
}

func decodeSIMGSM(data []byte) string {
	var decoded strings.Builder
	for index := 0; index < len(data); index++ {
		value := data[index]
		if value == 0xff {
			break
		}
		if value&0x80 != 0 || int(value) >= len(simGSMDefaultRunes) {
			return ""
		}
		if value != 0x1b {
			decoded.WriteRune(simGSMDefaultRunes[value])
			continue
		}
		index++
		if index == len(data) {
			return ""
		}
		extension, ok := simGSMExtensionRunes[data[index]]
		if !ok {
			return ""
		}
		decoded.WriteRune(extension)
	}
	return decoded.String()
}

func normalizeSIMOperatorName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 32 {
		return ""
	}
	for _, character := range value {
		if unicode.IsControl(character) || character == unicode.ReplacementChar {
			return ""
		}
	}
	return value
}
