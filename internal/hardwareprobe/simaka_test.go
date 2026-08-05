package hardwareprobe

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
)

func TestBuildUSIMAKAAPDUUses3GSecurityContext(t *testing.T) {
	challenge := agentapi.SIMAKAChallenge{}
	for index := range challenge.RAND {
		challenge.RAND[index] = byte(index)
		challenge.AUTN[index] = byte(index + 16)
	}
	actual := strings.ToUpper(hex.EncodeToString(buildUSIMAKAAPDU(challenge)))
	expected := "008800812210000102030405060708090A0B0C0D0E0F10101112131415161718191A1B1C1D1E1F"
	if actual != expected {
		t.Fatalf("APDU = %s", actual)
	}
}

func TestParseUSIMAKAResponseAcceptsSuccessAndSynchronizationFailure(t *testing.T) {
	res := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	ck := bytesFromRange(0x10, 16)
	ik := bytesFromRange(0x20, 16)
	kc := bytesFromRange(0x30, 8)
	payload := []byte{0xdb, byte(len(res))}
	payload = append(payload, res...)
	payload = append(payload, byte(len(ck)))
	payload = append(payload, ck...)
	payload = append(payload, byte(len(ik)))
	payload = append(payload, ik...)
	payload = append(payload, byte(len(kc)))
	payload = append(payload, kc...)
	result, err := parseUSIMAKAResponse(payload)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != agentapi.SIMAKAStateSuccess || !equalBytes(result.RES, res) ||
		!equalBytes(result.CK[:], ck) || !equalBytes(result.IK[:], ik) {
		t.Fatalf("success = %#v", result)
	}

	auts := bytesFromRange(0x40, 14)
	syncPayload := append([]byte{0xdc, byte(len(auts))}, auts...)
	syncResult, err := parseUSIMAKAResponse(syncPayload)
	if err != nil {
		t.Fatal(err)
	}
	if syncResult.State != agentapi.SIMAKAStateSynchronizationFailure || !equalBytes(syncResult.AUTS[:], auts) {
		t.Fatalf("sync failure = %#v", syncResult)
	}

	for _, invalid := range [][]byte{
		{0xdb, 3, 1, 2, 3},
		{0xdb, 4, 1, 2, 3, 4, 15},
		{0xdc, 13, 1, 2, 3},
		{0xaa, 0},
	} {
		if _, err := parseUSIMAKAResponse(invalid); !errors.Is(err, agentapi.ErrSIMAKAUnavailable) {
			t.Fatalf("invalid response accepted: %x, error=%v", invalid, err)
		}
	}
}

func TestExchangeML307AUSIMAKAOpensAndAlwaysClosesLogicalChannel(t *testing.T) {
	challenge := agentapi.SIMAKAChallenge{}
	for index := range challenge.RAND {
		challenge.RAND[index] = byte(index)
		challenge.AUTN[index] = byte(index + 16)
	}
	payload := successfulUSIMAKAPayload()
	commands := make([]string, 0, 3)
	query := func(_ context.Context, command string, _ time.Duration) ([]string, error) {
		commands = append(commands, command)
		switch {
		case command == `AT+CCHO="A0000000871002"`:
			return []string{"1", "OK"}, nil
		case strings.HasPrefix(command, `AT+CGLA=1,78,"00880081`):
			encoded := strings.ToUpper(hex.EncodeToString(append(append([]byte(nil), payload...), 0x90, 0x00)))
			return []string{fmt.Sprintf(`+CGLA: %d,"%s"`, len(encoded), encoded), "OK"}, nil
		case command == "AT+CCHC=1":
			return []string{"OK"}, nil
		default:
			return nil, fmt.Errorf("unexpected command")
		}
	}
	result, err := exchangeML307AUSIMAKA(t.Context(), query, challenge)
	if err != nil || result.State != agentapi.SIMAKAStateSuccess {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	if len(commands) != 3 || commands[len(commands)-1] != "AT+CCHC=1" {
		t.Fatalf("commands = %#v", commands)
	}

	commands = commands[:0]
	rejecting := func(_ context.Context, command string, _ time.Duration) ([]string, error) {
		commands = append(commands, command)
		switch {
		case strings.HasPrefix(command, "AT+CCHO="):
			return []string{"+CCHO: 2", "OK"}, nil
		case strings.HasPrefix(command, "AT+CGLA="):
			return []string{`+CGLA: 4,"9862"`, "OK"}, nil
		case command == "AT+CCHC=2":
			return []string{"OK"}, nil
		default:
			return nil, errors.New("unexpected command")
		}
	}
	if _, err := exchangeML307AUSIMAKA(t.Context(), rejecting, challenge); !errors.Is(err, agentapi.ErrSIMAKARejected) {
		t.Fatalf("rejection error = %v", err)
	}
	if commands[len(commands)-1] != "AT+CCHC=2" {
		t.Fatalf("logical channel not closed: %#v", commands)
	}
}

func TestReadML307AISIMIdentityUsesOnlyProvisionedIdentityFiles(t *testing.T) {
	const fullISIMAID = "A0000000871004AABB"
	privateIdentity := "234150123456789@ims.mnc015.mcc234.3gppnetwork.org"
	domain := "ims.mnc015.mcc234.3gppnetwork.org"
	publicIdentity := "sip:" + privateIdentity
	commands := make([]string, 0, 10)
	query := func(_ context.Context, command string, _ time.Duration) ([]string, error) {
		commands = append(commands, command)
		switch {
		case command == "AT+CUAD=0":
			return cuadFixture(fullISIMAID), nil
		case command == `AT+CCHO="`+fullISIMAID+`"`:
			return []string{"+CCHO: 5", "OK"}, nil
		case strings.Contains(command, "00A40004026F02"):
			return cglaFixture([]byte{0x62, 0x04, 0x80, 0x02, 0x00, byte(len(privateIdentity) + 2)}), nil
		case strings.Contains(command, fmt.Sprintf("00B00000%02X", len(privateIdentity)+2)):
			return cglaFixture(append([]byte{0x80, byte(len(privateIdentity))}, []byte(privateIdentity)...)), nil
		case strings.Contains(command, "00A40004026F03"):
			return cglaFixture([]byte{0x62, 0x04, 0x80, 0x02, 0x00, byte(len(domain) + 2)}), nil
		case strings.Contains(command, fmt.Sprintf("00B00000%02X", len(domain)+2)):
			return cglaFixture(append([]byte{0x80, byte(len(domain))}, []byte(domain)...)), nil
		case strings.Contains(command, "00A40004026F04"):
			return cglaFixture([]byte{0x62, 0x07, 0x82, 0x05, 0x42, 0x21, 0x00, 0x80, 0x01}), nil
		case strings.Contains(command, "00B2010480"):
			record := append([]byte{0x80, byte(len(publicIdentity))}, []byte(publicIdentity)...)
			for len(record) < 128 {
				record = append(record, 0xff)
			}
			return cglaFixture(record), nil
		case command == "AT+CCHC=5":
			return []string{"OK"}, nil
		default:
			return nil, fmt.Errorf("unexpected command %q", command)
		}
	}
	material, available, err := readML307AISIMIdentity(t.Context(), query)
	if err != nil || !available || material.Source != agentapi.SIMIMSIdentityISIM ||
		material.PrivateIdentity != privateIdentity || material.HomeDomain != domain ||
		len(material.PublicIdentities) != 1 || material.PublicIdentities[0] != publicIdentity ||
		commands[len(commands)-1] != "AT+CCHC=5" {
		t.Fatalf("material=%#v available=%t commands=%#v error=%v", material, available, commands, err)
	}
}

func TestReadML307AISIMIdentityTreatsPaddingOnlyEFIMPIAsUnprovisioned(t *testing.T) {
	const fullISIMAID = "A0000000871004CCDD"
	commands := make([]string, 0, 4)
	query := func(_ context.Context, command string, _ time.Duration) ([]string, error) {
		commands = append(commands, command)
		switch {
		case command == "AT+CUAD=0":
			return cuadFixture(fullISIMAID), nil
		case command == `AT+CCHO="`+fullISIMAID+`"`:
			return []string{"+CCHO: 4", "OK"}, nil
		case strings.Contains(command, "00A40004026F02"):
			return cglaFixture([]byte{0x62, 0x04, 0x80, 0x02, 0x00, 0x04}), nil
		case strings.Contains(command, "00B0000004"):
			return cglaFixture([]byte{0xff, 0xff, 0xff, 0xff}), nil
		case command == "AT+CCHC=4":
			return []string{"OK"}, nil
		default:
			return nil, fmt.Errorf("unexpected command %q", command)
		}
	}
	material, available, err := readML307AISIMIdentity(t.Context(), query)
	if err != nil || available || material.Source != "" || commands[len(commands)-1] != "AT+CCHC=4" {
		t.Fatalf("material=%#v available=%t commands=%#v error=%v", material, available, commands, err)
	}
	for _, command := range commands {
		if strings.Contains(command, "6F03") || strings.Contains(command, "6F04") {
			t.Fatalf("read partial ISIM identity after empty EFIMPI: %q", command)
		}
	}
}

func TestParseISIMAIDsFromEFDIRSelectsCompleteApplications(t *testing.T) {
	first := "A0000000871004AABB"
	second := "A0000000871004CCDD"
	usim := "A0000000871002FFFF"
	data := append(efDIRApplication(t, usim), 0xff, 0xff)
	data = append(data, efDIRApplication(t, first)...)
	data = append(data, 0xff)
	data = append(data, efDIRApplication(t, second)...)
	data = append(data, efDIRApplication(t, first)...)
	actual, valid := parseISIMAIDsFromEFDIR(data)
	if !valid || len(actual) != 2 || actual[0] != first || actual[1] != second {
		t.Fatalf("AIDs=%#v valid=%t", actual, valid)
	}
	if parsed, ok := parseCUADPayload([]string{`+CUAD: "` + strings.ToLower(hex.EncodeToString(data)) + `",2,"` + usim + `"`, "OK"}); !ok || parsed != strings.ToUpper(hex.EncodeToString(data)) {
		t.Fatalf("CUAD parsed=%q ok=%t", parsed, ok)
	}
	if validML307AISIMAID("A0000000871004secret") || !validML307AISIMAID(first) {
		t.Fatal("invalid AID validation")
	}
}

func TestDiscoverML307AISIMAIDsFallsBackToCRSM(t *testing.T) {
	const fullISIMAID = "A0000000871004EEFF"
	record := efDIRApplication(t, fullISIMAID)
	query := func(_ context.Context, command string, _ time.Duration) ([]string, error) {
		switch command {
		case "AT+CUAD=0":
			return []string{"ERROR"}, nil
		case "AT+CRSM=178,12032,1,4,0":
			return []string{fmt.Sprintf(`+CRSM: 144,0,"%s"`, strings.ToUpper(hex.EncodeToString(record))), "OK"}, nil
		case fmt.Sprintf("AT+CRSM=178,12032,2,4,%d", len(record)):
			return []string{"+CRSM: 106,131", "OK"}, nil
		default:
			return nil, fmt.Errorf("unexpected command %q", command)
		}
	}
	aids, discovered, err := discoverML307AISIMAIDs(t.Context(), query)
	if err != nil || !discovered || len(aids) != 1 || aids[0] != fullISIMAID {
		t.Fatalf("AIDs=%#v discovered=%t error=%v", aids, discovered, err)
	}
	if sw1, sw2, payload, ok := parseCRSMResponse([]string{`+CRSM: 144,0,"6100"`, "OK"}); !ok || sw1 != 144 || sw2 != 0 || payload != "6100" {
		t.Fatalf("CRSM sw1=%d sw2=%d payload=%q ok=%t", sw1, sw2, payload, ok)
	}
}

func TestSIMIMSHILStageErrorsRemainBounded(t *testing.T) {
	err := agentapi.NewSIMIMSHILStageError(agentapi.SIMIMSStagePublicRead)
	if stage, ok := agentapi.SIMIMSHILStage(err); !ok || stage != agentapi.SIMIMSStagePublicRead ||
		!errors.Is(err, agentapi.ErrSIMAKAUnavailable) {
		t.Fatalf("stage=%q ok=%t error=%v", stage, ok, err)
	}
	if stage, ok := agentapi.SIMIMSHILStage(agentapi.NewSIMIMSHILStageError("secret")); ok || stage != "" {
		t.Fatalf("unexpected stage=%q ok=%t", stage, ok)
	}
	shaped := agentapi.NewSIMIMSHILStageShapeError(agentapi.SIMIMSStagePrivateTLV, agentapi.SIMIMSShapePaddingOnly)
	if shape, ok := agentapi.SIMIMSHILShape(shaped); !ok || shape != agentapi.SIMIMSShapePaddingOnly {
		t.Fatalf("shape=%q ok=%t", shape, ok)
	}
	if shape, ok := agentapi.SIMIMSHILShape(agentapi.NewSIMIMSHILStageShapeError(
		agentapi.SIMIMSStagePrivateTLV, "secret")); ok || shape != "" {
		t.Fatalf("unexpected shape=%q ok=%t", shape, ok)
	}
}

func TestISIMTLVParsersRejectMalformedData(t *testing.T) {
	if size := parseISIMFileSize([]byte{0x62, 0x04, 0x80, 0x02, 0x00, 0x40}); size != 64 {
		t.Fatalf("size=%d", size)
	}
	if length, count := parseISIMRecordLayout([]byte{0x62, 0x07, 0x82, 0x05, 0x42, 0x21, 0x00, 0x80, 0x02}); length != 128 || count != 2 {
		t.Fatalf("length=%d count=%d", length, count)
	}
	for _, invalid := range [][]byte{{}, {0x80, 0x02, 'a'}, {0x80, 0x01, 0x00}, {0x81, 0x01, 'a'}} {
		if value, err := parseISIMTextTLV(invalid); err == nil {
			t.Fatalf("invalid TLV accepted: %x => %q", invalid, value)
		}
	}
	if value, err := parseISIMTextTLV([]byte{0x00, 0xff, 0x80, 0x03, 'a', 'b', 'c', 0xff}); err != nil || value != "abc" {
		t.Fatalf("padded TLV value=%q error=%v", value, err)
	}
	shapes := []struct {
		data []byte
		want string
	}{
		{nil, agentapi.SIMIMSShapeEmpty},
		{[]byte{0xff, 0x00}, agentapi.SIMIMSShapePaddingOnly},
		{[]byte{0x80, 0x05, 'a'}, agentapi.SIMIMSShapeTag80Malformed},
		{[]byte{0x03, 'a', 'b', 'c'}, agentapi.SIMIMSShapeLengthPrefixedASCII},
		{[]byte("abc"), agentapi.SIMIMSShapeDirectASCII},
		{[]byte{0x81, 0x01, 0x01}, agentapi.SIMIMSShapeOtherTLV},
		{[]byte{0x01}, agentapi.SIMIMSShapeOpaque},
	}
	for _, fixture := range shapes {
		if got := classifyISIMTextShape(fixture.data); got != fixture.want {
			t.Fatalf("shape(%x)=%q want=%q", fixture.data, got, fixture.want)
		}
	}
}

func cglaFixture(data []byte) []string {
	response := append(append([]byte(nil), data...), 0x90, 0x00)
	encoded := strings.ToUpper(hex.EncodeToString(response))
	return []string{fmt.Sprintf(`+CGLA: %d,"%s"`, len(encoded), encoded), "OK"}
}

func cuadFixture(aid string) []string {
	decoded, err := hex.DecodeString(aid)
	if err != nil {
		panic(err)
	}
	template := append([]byte{0x61, byte(len(decoded) + 2), 0x4f, byte(len(decoded))}, decoded...)
	return []string{`+CUAD: "` + strings.ToUpper(hex.EncodeToString(template)) + `"`, "OK"}
}

func efDIRApplication(t *testing.T, aid string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(aid)
	if err != nil {
		t.Fatal(err)
	}
	return append([]byte{0x61, byte(len(decoded) + 2), 0x4f, byte(len(decoded))}, decoded...)
}

func TestSendCGLAAPDUHandlesGetResponse(t *testing.T) {
	payload := successfulUSIMAKAPayload()
	calls := 0
	query := func(_ context.Context, command string, _ time.Duration) ([]string, error) {
		calls++
		if calls == 1 {
			if !strings.Contains(command, "00880081") {
				t.Fatalf("first command = %s", command)
			}
			return []string{`+CGLA: 4,"612C"`, "OK"}, nil
		}
		if command != `AT+CGLA=3,10,"00C000002C"` {
			t.Fatalf("GET RESPONSE = %s", command)
		}
		encoded := strings.ToUpper(hex.EncodeToString(append(append([]byte(nil), payload...), 0x90, 0x00)))
		return []string{fmt.Sprintf(`+CGLA: %d,"%s"`, len(encoded), encoded), "OK"}, nil
	}
	challenge := agentapi.SIMAKAChallenge{}
	data, err := sendCGLAAPDU(t.Context(), query, 3, buildUSIMAKAAPDU(challenge))
	if err != nil || !equalBytes(data, payload) || calls != 2 {
		t.Fatalf("data=%x calls=%d error=%v", data, calls, err)
	}
}

func TestML307AIdentityParserAcceptsOnlyCompleteIMSI(t *testing.T) {
	if actual := parseML307AIMSI([]string{"234150123456789", "OK"}); actual != "234150123456789" {
		t.Fatalf("IMSI = %q", actual)
	}
	for _, lines := range [][]string{
		{"234150123456789"}, {"23415x123456789", "OK"}, {"123", "OK"}, {"234150123456789", "ERROR"},
	} {
		if actual := parseML307AIMSI(lines); actual != "" {
			t.Fatalf("invalid IMSI accepted: %q", actual)
		}
	}
}

func successfulUSIMAKAPayload() []byte {
	res := bytesFromRange(1, 8)
	ck := bytesFromRange(0x10, 16)
	ik := bytesFromRange(0x20, 16)
	payload := []byte{0xdb, byte(len(res))}
	payload = append(payload, res...)
	payload = append(payload, byte(len(ck)))
	payload = append(payload, ck...)
	payload = append(payload, byte(len(ik)))
	payload = append(payload, ik...)
	return payload
}

func bytesFromRange(start, count int) []byte {
	result := make([]byte, count)
	for index := range result {
		result[index] = byte(start + index)
	}
	return result
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
