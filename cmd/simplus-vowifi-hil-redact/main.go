package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var (
	naiPattern    = regexp.MustCompile(`[^[:space:]\[\]"']+@nai\.epc\.mnc[0-9]{3}\.mcc[0-9]{3}\.3gppnetwork\.org`)
	digitsPattern = regexp.MustCompile(`(^|[^0-9])[0-9]{14,16}([^0-9]|$)`)
	hexPattern    = regexp.MustCompile(`(?i)(^|[^0-9a-f])[0-9a-f]{16,}([^0-9a-f]|$)`)
	keyPattern    = regexp.MustCompile(`(?i)(rand|autn|auts|(^|[^a-z])res([^a-z]|$)|(^|[^a-z])ck([^a-z]|$)|(^|[^a-z])ik([^a-z]|$))`)
)

func main() {
	expectedIdentity := readExpectedIdentity()
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	writer := bufio.NewWriter(os.Stdout)
	defer writer.Flush()
	count := 0
	for scanner.Scan() {
		rawLine := scanner.Text()
		if marker, ok := identityMarker(rawLine, expectedIdentity); ok {
			fmt.Fprintln(writer, marker)
			_ = writer.Flush()
		}
		line := strings.Map(func(value rune) rune {
			if value == '\t' || value >= 0x20 && value <= 0x7e {
				return value
			}
			return '?'
		}, rawLine)
		line = naiPattern.ReplaceAllString(line, "[identity-redacted]")
		line = digitsPattern.ReplaceAllString(line, "${1}[digits-redacted]${2}")
		line = hexPattern.ReplaceAllString(line, "${1}[hex-redacted]${2}")
		if keyPattern.MatchString(line) {
			line = redactAssignments(line)
		}
		if len(line) > 600 {
			line = line[:600] + "..."
		}
		if count < 300 {
			fmt.Fprintln(writer, line)
			_ = writer.Flush()
			count++
		}
	}
}

func readExpectedIdentity() string {
	data, err := os.ReadFile("/run/simplus-vowifi-hil/vici.json")
	if err != nil || len(data) > 4096 {
		return ""
	}
	defer func() {
		for index := range data {
			data[index] = 0
		}
	}()
	var input struct {
		Identity string `json:"identity"`
	}
	if json.Unmarshal(data, &input) != nil {
		return ""
	}
	return input.Identity
}

func identityMarker(line, expected string) (string, bool) {
	const prefix = "quintuplets for '"
	position := strings.Index(line, prefix)
	if position < 0 || expected == "" {
		return "", false
	}
	identity := line[position+len(prefix):]
	end := strings.IndexByte(identity, '\'')
	if end < 0 {
		return "", false
	}
	identity = identity[:end]
	return fmt.Sprintf("SAFE sim_card_identity_matches=%t identity_had_type_prefix=%t", identity == expected, identity == "userfqdn:"+expected), true
}

func redactAssignments(line string) string {
	fields := strings.Fields(line)
	for index, field := range fields {
		lower := strings.ToLower(field)
		if strings.Contains(lower, "rand") || strings.Contains(lower, "autn") ||
			strings.Contains(lower, "auts") || strings.HasPrefix(lower, "res=") ||
			strings.HasPrefix(lower, "ck=") || strings.HasPrefix(lower, "ik=") {
			fields[index] = "[aka-material-redacted]"
		}
	}
	return strings.Join(fields, " ")
}
