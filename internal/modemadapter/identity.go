package modemadapter

import (
	"regexp"
	"strings"

	"github.com/leonfox28/simplus/internal/attransport"
)

var (
	iccidPattern       = regexp.MustCompile(`^[0-9]{18,22}$`)
	imeiPattern        = regexp.MustCompile(`^[0-9]{15}$`)
	fingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func pseudonymizedICCID(lines []string, responsePrefix string, pseudonymizer IdentityPseudonymizer) (string, string) {
	if pseudonymizer == nil || !attransport.HasTerminalOK(lines) {
		return "", ""
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if responsePrefix == "" || !strings.HasPrefix(line, responsePrefix) {
			continue
		}
		value := strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, responsePrefix)), `"`)
		if !iccidPattern.MatchString(value) {
			return "", ""
		}
		fingerprint, err := pseudonymizer.Pseudonym("sim-iccid-v1", []byte(value))
		if err != nil || !fingerprintPattern.MatchString(fingerprint) {
			return "", ""
		}
		return fingerprint, "ICCID •••• " + value[len(value)-4:]
	}
	return "", ""
}

func equipmentIMEI(lines []string) string {
	if !attransport.HasTerminalOK(lines) {
		return ""
	}
	for _, line := range lines {
		value := strings.TrimSpace(line)
		if strings.HasPrefix(value, "+CGSN:") {
			value = strings.Trim(strings.TrimSpace(strings.TrimPrefix(value, "+CGSN:")), `"`)
		}
		if !imeiPattern.MatchString(value) || !validIMEICheckDigit(value) {
			continue
		}
		return value
	}
	return ""
}

func validIMEICheckDigit(value string) bool {
	if !imeiPattern.MatchString(value) {
		return false
	}
	sum := 0
	for index, character := range value {
		digit := int(character - '0')
		if index%2 == 1 {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
	}
	return sum%10 == 0
}
