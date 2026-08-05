package agentapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSMSErrorReportsUnsupportedDeviceWithoutRetryHint(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/sms/list", nil)
	response := httptest.NewRecorder()
	writeSMSError(response, request, nil, ErrSMSUnsupported, "SMS list failed")
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", response.Code)
	}
	var body ErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "SMS_UNSUPPORTED" || body.Retryable {
		t.Fatalf("error response = %#v", body)
	}
}

func TestSMSErrorReportsUnknownSendOutcomeWithoutRetryHint(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/sms/send", nil)
	response := httptest.NewRecorder()
	writeSMSError(response, request, nil, errors.Join(ErrSMSOutcomeUnknown, errors.New("response lost")), "SMS send failed")
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d", response.Code)
	}
	var body ErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "SMS_SEND_OUTCOME_UNKNOWN" || body.Retryable {
		t.Fatalf("error response = %#v", body)
	}
}
