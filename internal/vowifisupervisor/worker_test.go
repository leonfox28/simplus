package vowifisupervisor

import (
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leonfox28/simplus/internal/vowifihil"
)

func TestStrongSwanLogReaderContinuesPastTwoMiB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "charon.log")
	// 10.255.0.42 is a synthetic private IMS fixture, not an observed address.
	body := strings.Repeat("ordinary bounded log line\n", 90_000) +
		"received P-CSCF server IP 10.255.0.42\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	pcscf := make(chan netip.Addr, 1)
	done := make(chan struct{})
	readStrongSwanLog(file, pcscf, make(chan string, 1), &strongSwanTrace{}, done)
	select {
	case address := <-pcscf:
		if address.String() != "10.255.0.42" {
			t.Fatalf("P-CSCF = %s", address)
		}
	default:
		t.Fatal("log reader stopped before the P-CSCF line")
	}
}

func TestStrongSwanDiagnosticsExposeOnlyFixedSafeCodes(t *testing.T) {
	cases := map[string]string{
		"giving up after 5 retransmits":                                  "EPDG_NO_RESPONSE",
		"establishing IKE_SA failed, peer not responding":                "EPDG_NO_RESPONSE",
		"received AUTHENTICATION_FAILED notify error":                    "EPDG_AUTH_FAILED",
		"simplus SIM AKA Agent exchange failed at stage response_decode": "SIM_AKA_FAILED",
		"sending packet failed: Operation not permitted":                 "EPDG_SEND_FAILED",
		"received packet: from 88.82.11.221":                             "",
	}
	for input, expected := range cases {
		if actual := classifyStrongSwanDiagnostic(input); actual != expected {
			t.Fatalf("diagnostic %q = %q, want %q", input, actual, expected)
		}
	}
}

func TestInitiateErrorsMapToSafeStages(t *testing.T) {
	if code := initiateErrorCode(vowifihil.ErrRequiredPluginsUnavailable); code != "STRONGSWAN_PLUGINS_MISSING" {
		t.Fatalf("code=%q", code)
	}
	if code := initiateErrorCode(vowifihil.ErrConnectionInitiateFailed); code != "EPDG_CONNECT_FAILED" {
		t.Fatalf("code=%q", code)
	}
}

func TestIMSRefreshErrorsMapToSafeStages(t *testing.T) {
	cases := map[error]string{
		vowifihil.ErrIMSReauthenticationRequired: "IMS_REAUTH_REQUIRED",
		vowifihil.ErrIMSRefreshIntervalRejected:  "IMS_REFRESH_INTERVAL_REJECTED",
		vowifihil.ErrIMSRefreshRejected:          "IMS_REFRESH_REJECTED",
		vowifihil.ErrIMSRefreshNoResponse:        "IMS_REFRESH_NO_RESPONSE",
		vowifihil.ErrIMSRefreshResponseUnmatched: "IMS_REFRESH_RESPONSE_UNMATCHED",
		errors.New("transport failed"):           "IMS_REFRESH_FAILED",
	}
	for input, expected := range cases {
		if actual := imsRefreshErrorCode(input); actual != expected {
			t.Fatalf("refresh error %v = %q, want %q", input, actual, expected)
		}
	}
}

func TestStrongSwanTraceReportsFurthestSafeStage(t *testing.T) {
	trace := &strongSwanTrace{}
	trace.observe("generating IKE_SA_INIT request 0")
	trace.observe("parsed IKE_SA_INIT response 0")
	trace.observe("simplus IMS APN IDr added")
	trace.observe("generating IKE_AUTH request 1")
	if code := trace.failureCode("EPDG_CONNECT_FAILED"); code != "EPDG_IKE_AUTH_NO_RESPONSE" {
		t.Fatalf("code=%q", code)
	}
	trace.observe("parsed IKE_AUTH response 1 [ EAP/REQ/AKA ]")
	if code := trace.failureCode("EPDG_CONNECT_FAILED"); code != "EPDG_EAP_FAILED" {
		t.Fatalf("code=%q", code)
	}
}
