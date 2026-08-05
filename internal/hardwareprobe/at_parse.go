package hardwareprobe

import (
	"encoding/csv"
	"regexp"
	"strconv"
	"strings"

	"github.com/leonfox28/simplus/internal/agentapi"
)

var (
	longDigitRun       = regexp.MustCompile(`[0-9]{14,22}`)
	plmnPattern        = regexp.MustCompile(`^[0-9]{5,6}$`)
	iccidPattern       = regexp.MustCompile(`^[0-9]{18,22}$`)
	fingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func pseudonymizedML307AICCID(lines []string, pseudonymizer IdentityPseudonymizer) (string, string) {
	if pseudonymizer == nil {
		return "", ""
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "+MCCID:") {
			continue
		}
		value := strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "+MCCID:")), `"`)
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

func firstPayload(lines []string, prefixes ...string) string {
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "OK" || line == "ERROR" || strings.HasPrefix(line, "+CME ERROR:") || strings.HasPrefix(line, "+CMS ERROR:") {
			continue
		}
		matched := len(prefixes) == 0
		for _, prefix := range prefixes {
			if strings.HasPrefix(line, prefix) {
				matched = true
				break
			}
		}
		if matched {
			return safeProbeText(line, 192)
		}
	}
	return ""
}

func identityPayload(lines []string) string {
	value := firstPayload(lines)
	if longDigitRun.MatchString(value) {
		return ""
	}
	return safeProbeText(value, 128)
}

func rfObservation(lines []string) agentapi.RFObservation {
	raw := firstPayload(lines, "+CFUN:")
	observation := agentapi.RFObservation{State: agentapi.RFStateUnknown, FunctionalMode: raw}
	fields := csvPayload(raw)
	if len(fields) == 0 {
		return observation
	}
	mode, err := strconv.Atoi(strings.TrimSpace(fields[0]))
	if err != nil {
		return observation
	}
	observation.Mode = intPointer(mode)
	switch mode {
	case 0:
		observation.State = agentapi.RFStateMinimum
	case 1:
		observation.State = agentapi.RFStateOn
	case 4:
		observation.State = agentapi.RFStateOff
	}
	return observation
}

func simObservation(cpin, simStatus []string) agentapi.SIMObservation {
	combined := strings.ToUpper(strings.Join(append(append([]string{}, cpin...), simStatus...), "\n"))
	observation := agentapi.SIMObservation{State: agentapi.SIMStateUnknown, PrimaryLockState: agentapi.PrimaryLockUnknown}
	switch {
	case strings.Contains(combined, "+CME ERROR: 10"), strings.Contains(combined, "SIM NOT INSERTED"), strings.Contains(combined, "+QSIMSTAT: 0,0"), strings.Contains(combined, "+QSIMSTAT: 1,0"):
		observation.State = agentapi.SIMStateAbsent
	case strings.Contains(combined, "SIM PUK BLOCKED"), strings.Contains(combined, "SIM PERM BLOCK"), strings.Contains(combined, "SIM BLOCKED"):
		observation.State = agentapi.SIMStateLocked
		observation.PrimaryLockState = agentapi.PrimaryLockPermanentlyBlocked
		observation.LockType = "puk1-blocked"
	case strings.Contains(combined, "+CPIN: READY"):
		observation.State = agentapi.SIMStatePresent
		observation.PrimaryLockState = agentapi.PrimaryLockReady
	case strings.Contains(combined, "+CPIN: SIM PIN") && !strings.Contains(combined, "+CPIN: SIM PIN2"):
		observation.State = agentapi.SIMStateLocked
		observation.PrimaryLockState = agentapi.PrimaryLockPIN1Required
		observation.LockType = "pin1"
	case strings.Contains(combined, "+CPIN: SIM PUK") && !strings.Contains(combined, "+CPIN: SIM PUK2"):
		observation.State = agentapi.SIMStateLocked
		observation.PrimaryLockState = agentapi.PrimaryLockPUK1Required
		observation.LockType = "puk1"
	case strings.Contains(combined, "+CPIN: SIM PIN2"), strings.Contains(combined, "+CPIN: SIM PUK2"), strings.Contains(combined, "+CPIN: PH-"):
		observation.State = agentapi.SIMStateLocked
		observation.PrimaryLockState = agentapi.PrimaryLockUnsupported
		observation.LockType = "unsupported"
	case strings.Contains(combined, "+QSIMSTAT: 0,1"), strings.Contains(combined, "+QSIMSTAT: 1,1"):
		observation.State = agentapi.SIMStatePresent
	}
	return observation
}

func registrationObservations(creg, cgreg, cereg []string) []agentapi.RegistrationObservation {
	return []agentapi.RegistrationObservation{
		registrationObservation(creg, "+CREG:", agentapi.RegistrationDomainCS, "at-creg"),
		registrationObservation(cgreg, "+CGREG:", agentapi.RegistrationDomainPacket, "at-cgreg"),
		registrationObservation(cereg, "+CEREG:", agentapi.RegistrationDomainEPS, "at-cereg"),
	}
}

func registrationObservation(lines []string, prefix, domain, source string) agentapi.RegistrationObservation {
	observation := agentapi.RegistrationObservation{Domain: domain, State: agentapi.RegistrationUnknown, Source: source}
	fields := csvPayload(firstPayload(lines, prefix))
	if len(fields) == 0 {
		return observation
	}
	statIndex := 0
	if len(fields) >= 2 {
		statIndex = 1
	}
	stat, err := strconv.Atoi(strings.TrimSpace(fields[statIndex]))
	if err != nil {
		return observation
	}
	switch stat {
	case 0:
		observation.State = agentapi.RegistrationNotRegistered
	case 1:
		observation.State = agentapi.RegistrationRegisteredHome
	case 2:
		observation.State = agentapi.RegistrationSearching
	case 3:
		observation.State = agentapi.RegistrationDenied
	case 5:
		observation.State = agentapi.RegistrationRegisteredRoaming
	case 6:
		observation.State = agentapi.RegistrationRegisteredSMSHome
	case 7:
		observation.State = agentapi.RegistrationRegisteredSMSRoaming
	case 8:
		observation.State = agentapi.RegistrationEmergencyOnly
	case 9:
		observation.State = agentapi.RegistrationHomeCSFBNotPreferred
	case 10:
		observation.State = agentapi.RegistrationRoamingCSFBNotPreferred
	}
	return observation
}

func signalObservation(lines []string) agentapi.SignalObservation {
	observation := agentapi.SignalObservation{State: agentapi.SignalStateUnknown}
	fields := csvPayload(firstPayload(lines, "+CSQ:"))
	if len(fields) < 2 {
		return observation
	}
	observation.Source = "at-csq"
	rssi, rssiErr := strconv.Atoi(strings.TrimSpace(fields[0]))
	ber, berErr := strconv.Atoi(strings.TrimSpace(fields[1]))
	if rssiErr == nil {
		switch {
		case rssi >= 0 && rssi <= 31:
			observation.State = agentapi.SignalStateMeasured
			observation.RSSI = intPointer(rssi)
			observation.RSSIDBm = intPointer(-113 + 2*rssi)
		case rssi == 99:
			observation.State = agentapi.SignalStateUnavailable
		}
	}
	if berErr == nil && ber >= 0 && ber <= 7 {
		observation.BER = intPointer(ber)
	}
	return observation
}

func networkObservation(cops, qnwinfo []string) agentapi.NetworkObservation {
	observation := agentapi.NetworkObservation{SelectionMode: agentapi.NetworkSelectionUnknown}
	fields := csvPayload(firstPayload(cops, "+COPS:"))
	if len(fields) != 0 {
		if mode, err := strconv.Atoi(strings.TrimSpace(fields[0])); err == nil {
			switch mode {
			case 0:
				observation.SelectionMode = agentapi.NetworkSelectionAutomatic
			case 1:
				observation.SelectionMode = agentapi.NetworkSelectionManual
			case 2:
				observation.SelectionMode = agentapi.NetworkSelectionDeregistered
			case 4:
				observation.SelectionMode = agentapi.NetworkSelectionManualAutomatic
			}
		}
		if len(fields) >= 3 {
			applyCOPSOperator(&observation, fields[1], fields[2])
		}
		if len(fields) >= 4 {
			if accessTechnology, err := strconv.Atoi(strings.TrimSpace(fields[3])); err == nil {
				observation.RAT = accessTechnologyName(accessTechnology)
			}
		}
	}

	qnwFields := csvPayload(firstPayload(qnwinfo, "+QNWINFO:"))
	if len(qnwFields) != 0 && !strings.Contains(strings.ToUpper(qnwFields[0]), "NO SERVICE") {
		if observation.RAT == "" {
			observation.RAT = textualRAT(qnwFields[0])
		}
		if len(qnwFields) >= 2 && observation.PLMN == "" && observation.OperatorName == "" {
			applyOperator(&observation, qnwFields[1])
		}
	}
	return observation
}

func applyOperator(observation *agentapi.NetworkObservation, value string) {
	value = safeProbeText(strings.Trim(value, `"`), 64)
	if plmnPattern.MatchString(value) {
		observation.PLMN = value
	} else if value != "" {
		observation.OperatorName = value
	}
}

func applyCOPSOperator(observation *agentapi.NetworkObservation, formatValue, value string) {
	format, err := strconv.Atoi(strings.TrimSpace(formatValue))
	if err != nil {
		return
	}
	value = safeProbeText(strings.Trim(value, `"`), 64)
	switch format {
	case 0, 1:
		observation.OperatorName = value
	case 2:
		if plmnPattern.MatchString(value) {
			observation.PLMN = value
		}
	}
}

func accessTechnologyName(value int) string {
	switch value {
	case 0:
		return agentapi.RATGSM
	case 1:
		return agentapi.RATGSMCompact
	case 2:
		return agentapi.RATUTRAN
	case 3:
		return agentapi.RATGSMEdge
	case 4:
		return agentapi.RATUTRANHSDPA
	case 5:
		return agentapi.RATUTRANHSUPA
	case 6:
		return agentapi.RATUTRANHSPA
	case 7:
		return agentapi.RATLTE
	case 8:
		return agentapi.RATECGSMIoT
	case 9:
		return agentapi.RATNBIoT
	case 10:
		return agentapi.RATLTE5GC
	case 11:
		return agentapi.RATNR5GC
	case 12:
		return agentapi.RATNGRAN
	default:
		return ""
	}
}

func textualRAT(value string) string {
	upper := strings.ToUpper(value)
	switch {
	case strings.Contains(upper, "NR5G"), strings.Contains(upper, "NR"):
		return agentapi.RATNR
	case strings.Contains(upper, "LTE"):
		return agentapi.RATLTE
	case strings.Contains(upper, "WCDMA"), strings.Contains(upper, "UMTS"):
		return agentapi.RATUTRAN
	case strings.Contains(upper, "GSM"), strings.Contains(upper, "GPRS"), strings.Contains(upper, "EDGE"):
		return agentapi.RATGSM
	case strings.Contains(upper, "CDMA"):
		return agentapi.RATCDMA
	default:
		return ""
	}
}

func csvPayload(value string) []string {
	separator := strings.IndexByte(value, ':')
	if separator >= 0 {
		value = value[separator+1:]
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	reader := csv.NewReader(strings.NewReader(value))
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true
	fields, err := reader.Read()
	if err != nil || len(fields) > 16 {
		return nil
	}
	for index := range fields {
		fields[index] = safeProbeText(fields[index], 96)
	}
	return fields
}

func activeCallCount(lines []string) int {
	count := 0
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "+CLCC:") {
			count++
		}
	}
	return count
}

func hasTerminalOK(lines []string) bool {
	for _, line := range lines {
		if line == "OK" {
			return true
		}
	}
	return false
}

func intPointer(value int) *int {
	return &value
}

func safeProbeText(value string, limit int) string {
	value = safeText(strings.ReplaceAll(strings.ReplaceAll(value, "\r", " "), "\n", " "), limit)
	return strings.Join(strings.Fields(value), " ")
}
