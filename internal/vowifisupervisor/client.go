package vowifisupervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"time"
)

type Client struct {
	http *http.Client
}

func NewClient(socketPath string) (*Client, error) {
	if !filepath.IsAbs(socketPath) {
		return nil, errors.New("Host VoWiFi supervisor socket must be absolute")
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", filepath.Clean(socketPath))
	}}
	return &Client{http: &http.Client{Transport: transport}}, nil
}

func (client *Client) List(ctx context.Context) ([]Status, error) {
	var value StatusList
	if err := client.request(ctx, http.MethodGet, "/v1/vowifi/status", nil, &value); err != nil {
		return nil, err
	}
	return value.Lines, nil
}

func (client *Client) Start(ctx context.Context, request StartRequest) (Status, error) {
	var status Status
	err := client.request(ctx, http.MethodPost, "/v1/vowifi/start", request, &status)
	return status, err
}

func (client *Client) Stop(ctx context.Context, lineID string) (Status, error) {
	var status Status
	err := client.request(ctx, http.MethodPost, "/v1/vowifi/stop", StopRequest{LineID: lineID}, &status)
	return status, err
}

func (client *Client) SendSMS(ctx context.Context, request SMSSendRequest) (SMSSendResponse, error) {
	if !validSMSSendRequest(request) {
		return SMSSendResponse{}, ErrRequestInvalid
	}
	var response SMSSendResponse
	if err := client.request(ctx, http.MethodPost, "/v1/vowifi/sms/send", request, &response); err != nil {
		return SMSSendResponse{}, err
	}
	if !validSMSSendResponse(response) {
		return SMSSendResponse{}, ErrSMSUnavailable
	}
	return response, nil
}

func (client *Client) ListSMS(ctx context.Context, lineID string) ([]SMSMessageReference, error) {
	if !managedLinePattern.MatchString(lineID) {
		return nil, ErrRequestInvalid
	}
	var response SMSListResponse
	if err := client.request(ctx, http.MethodPost, "/v1/vowifi/sms/list", SMSListRequest{LineID: lineID}, &response); err != nil {
		return nil, err
	}
	if !validSMSMessageReferences(response.Messages) {
		return nil, ErrSMSUnavailable
	}
	return response.Messages, nil
}

func (client *Client) ReadSMS(ctx context.Context, lineID, messageID string) (SMSMessage, error) {
	if !validSMSMessageRequest(lineID, messageID) {
		return SMSMessage{}, ErrRequestInvalid
	}
	var response SMSMessage
	if err := client.request(ctx, http.MethodPost, "/v1/vowifi/sms/read", SMSReadRequest{LineID: lineID, MessageID: messageID}, &response); err != nil {
		return SMSMessage{}, err
	}
	if !validSMSMessagePayload(response, messageID) {
		return SMSMessage{}, ErrSMSUnavailable
	}
	return response, nil
}

func (client *Client) AcknowledgeSMS(ctx context.Context, request SMSAcknowledgeRequest) error {
	if !validSMSAcknowledgeRequest(request) {
		return ErrRequestInvalid
	}
	var response SMSAcknowledgeResponse
	if err := client.request(ctx, http.MethodPost, "/v1/vowifi/sms/acknowledge", request, &response); err != nil {
		return err
	}
	if !response.Acknowledged {
		return ErrSMSUnavailable
	}
	return nil
}

func (client *Client) ListSMSSubmitReports(ctx context.Context, lineID string) (SMSSubmitReportListResponse, error) {
	if !managedLinePattern.MatchString(lineID) {
		return SMSSubmitReportListResponse{}, ErrRequestInvalid
	}
	var response SMSSubmitReportListResponse
	if err := client.request(ctx, http.MethodPost, "/v1/vowifi/sms/reports/list", SMSListRequest{LineID: lineID}, &response); err != nil {
		return SMSSubmitReportListResponse{}, err
	}
	if !validSMSSubmitReports(response.Reports) {
		return SMSSubmitReportListResponse{}, ErrSMSUnavailable
	}
	return response, nil
}

func (client *Client) AcknowledgeSMSSubmitReport(ctx context.Context, request SMSSubmitReportAcknowledgeRequest) error {
	if !validSMSSubmitReportAcknowledgeRequest(request) {
		return ErrRequestInvalid
	}
	var response SMSAcknowledgeResponse
	if err := client.request(ctx, http.MethodPost, "/v1/vowifi/sms/reports/acknowledge", request, &response); err != nil {
		return err
	}
	if !response.Acknowledged {
		return ErrSMSUnavailable
	}
	return nil
}

func (client *Client) request(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://unix"+path, body)
	if err != nil {
		return err
	}
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("contact Host VoWiFi supervisor: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var value errorResponse
		_ = json.NewDecoder(io.LimitReader(response.Body, 4<<10)).Decode(&value)
		switch value.Code {
		case "REQUEST_INVALID":
			return ErrRequestInvalid
		case "ALREADY_RUNNING":
			return ErrAlreadyRunning
		case "NOT_RUNNING":
			return ErrNotRunning
		case "STARTUP_FAILED":
			return ErrStartupFailed
		case "SMS_UNAVAILABLE":
			return ErrSMSUnavailable
		case "SMS_MESSAGE_NOT_FOUND":
			return ErrSMSMessageNotFound
		case "SMS_SEND_OUTCOME_UNKNOWN":
			return ErrSMSOutcomeUnknown
		case "SMS_REJECTED":
			return ErrSMSRejected
		default:
			return fmt.Errorf("Host VoWiFi supervisor returned HTTP %d", response.StatusCode)
		}
	}
	if output != nil {
		return json.NewDecoder(io.LimitReader(response.Body, maxSMSResponseBytes)).Decode(output)
	}
	return nil
}

var _ SMSAPI = (*Client)(nil)
