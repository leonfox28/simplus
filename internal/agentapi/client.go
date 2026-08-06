package agentapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"time"
)

type Client struct {
	http       *http.Client
	socketPath string
}

var _ SMSClientAPI = (*Client)(nil)

func NewClient(socketPath string) (*Client, error) {
	if !filepath.IsAbs(socketPath) {
		return nil, errors.New("agent socket path must be absolute")
	}
	socketPath = filepath.Clean(socketPath)
	transport := &http.Transport{
		DisableCompression: true,
		DisableKeepAlives:  false,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			dialer := net.Dialer{Timeout: 3 * time.Second}
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{
		http: &http.Client{
			Transport:     transport,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
		},
		socketPath: socketPath,
	}, nil
}

func (client *Client) Hello(ctx context.Context) (Hello, error) {
	var response Hello
	if err := client.request(ctx, http.MethodGet, "/v1/hello", nil, &response); err != nil {
		return Hello{}, err
	}
	if response.Protocol != ProtocolName || response.ProtocolVersion != ProtocolVersion || !IsValidAgentInstanceID(response.AgentInstanceID) {
		return Hello{}, fmt.Errorf("unsupported agent protocol %q version %d", response.Protocol, response.ProtocolVersion)
	}
	return response, nil
}

func (client *Client) Snapshot(ctx context.Context, refresh bool) (Snapshot, error) {
	var response Snapshot
	path := "/v1/snapshot?refresh=" + strconv.FormatBool(refresh)
	if err := client.request(ctx, http.MethodGet, path, nil, &response); err != nil {
		return Snapshot{}, err
	}
	if response.ProtocolVersion != ProtocolVersion || !IsValidAgentInstanceID(response.AgentInstanceID) {
		return Snapshot{}, fmt.Errorf("unsupported snapshot protocol version %d", response.ProtocolVersion)
	}
	return response, nil
}

func (client *Client) Changes(ctx context.Context, instanceID string, after uint64, timeoutSeconds int) (ChangeResponse, error) {
	if !IsValidAgentInstanceID(instanceID) {
		return ChangeResponse{}, errors.New("change watch requires the current Agent instance id")
	}
	if timeoutSeconds < 1 || timeoutSeconds > 30 {
		return ChangeResponse{}, errors.New("change timeout must be from 1 through 30 seconds")
	}
	query := url.Values{"after": {strconv.FormatUint(after, 10)}, "instanceId": {instanceID}, "timeoutSeconds": {strconv.Itoa(timeoutSeconds)}}
	var response ChangeResponse
	if err := client.request(ctx, http.MethodGet, "/v1/changes?"+query.Encode(), nil, &response); err != nil {
		return ChangeResponse{}, err
	}
	if response.Snapshot.ProtocolVersion != ProtocolVersion || !IsValidAgentInstanceID(response.Snapshot.AgentInstanceID) {
		return ChangeResponse{}, fmt.Errorf("unsupported snapshot protocol version %d", response.Snapshot.ProtocolVersion)
	}
	return response, nil
}

func (client *Client) Probe(ctx context.Context, request ProbeRequest) (ProbeResponse, error) {
	var response ProbeResponse
	if err := client.request(ctx, http.MethodPost, "/v1/probes/read-only", request, &response); err != nil {
		return ProbeResponse{}, err
	}
	if err := validateProbeResponse(response); err != nil {
		return ProbeResponse{}, fmt.Errorf("invalid probe response: %w", err)
	}
	return response, nil
}

func (client *Client) EnsureRadioOff(ctx context.Context, request RadioEnsureOffRequest) (RadioEnsureOffResponse, error) {
	var response RadioEnsureOffResponse
	if err := client.request(ctx, http.MethodPost, "/v1/commands/radio/ensure-off", request, &response); err != nil {
		return RadioEnsureOffResponse{}, err
	}
	if err := validateRadioEnsureOffResponse(response, request.OperationID); err != nil {
		return RadioEnsureOffResponse{}, fmt.Errorf("invalid radio.ensure-off response: %w", err)
	}
	return response, nil
}

func (client *Client) SetRFState(ctx context.Context, request RFSetRequest) (RFSetResponse, error) {
	var response RFSetResponse
	if err := client.request(ctx, http.MethodPost, "/v1/radio/state", request, &response); err != nil {
		return RFSetResponse{}, err
	}
	if response.ProtocolVersion != ProtocolVersion || response.AgentInstanceID != request.AgentInstanceID ||
		response.DeviceID != request.DeviceID || (response.State != RFStateOn && response.State != RFStateOff) {
		return RFSetResponse{}, errors.New("invalid RF state response")
	}
	if request.Enabled != (response.State == RFStateOn) {
		return RFSetResponse{}, errors.New("RF state response does not match request")
	}
	return response, nil
}

func (client *Client) ReadEquipmentIdentity(ctx context.Context, request EquipmentIdentityReadRequest) (EquipmentIdentityReadResponse, error) {
	var response EquipmentIdentityReadResponse
	if err := client.request(ctx, http.MethodPost, "/v1/equipment-identity/read", request, &response); err != nil {
		return EquipmentIdentityReadResponse{}, err
	}
	if response.ProtocolVersion != ProtocolVersion || response.AgentInstanceID != request.AgentInstanceID ||
		response.DeviceID != request.DeviceID || !validIMEI(response.IMEI) || !isSHA256Hex(response.Fingerprint) {
		return EquipmentIdentityReadResponse{}, errors.New("invalid equipment identity response")
	}
	return response, nil
}

func (client *Client) ListSMS(ctx context.Context, request SMSListRequest) (SMSListResponse, error) {
	if err := validateSMSListRequest(request); err != nil {
		return SMSListResponse{}, err
	}
	var response SMSListResponse
	if err := client.request(ctx, http.MethodPost, "/v1/sms/list", request, &response); err != nil {
		return SMSListResponse{}, err
	}
	if err := validateSMSListResponse(response, request); err != nil {
		return SMSListResponse{}, fmt.Errorf("invalid SMS list response: %w", err)
	}
	return response, nil
}

func (client *Client) ReadSMS(ctx context.Context, request SMSReadRequest) (SMSReadResponse, error) {
	if err := validateSMSReadRequest(request); err != nil {
		return SMSReadResponse{}, err
	}
	var response SMSReadResponse
	if err := client.request(ctx, http.MethodPost, "/v1/sms/read", request, &response); err != nil {
		return SMSReadResponse{}, err
	}
	if err := validateSMSReadResponse(response, request); err != nil {
		return SMSReadResponse{}, fmt.Errorf("invalid SMS read response: %w", err)
	}
	return response, nil
}

func (client *Client) SendSMS(ctx context.Context, request SMSSendRequest) (SMSSendResponse, error) {
	if err := validateSMSSendRequest(request); err != nil {
		return SMSSendResponse{}, err
	}
	var response SMSSendResponse
	if err := client.request(ctx, http.MethodPost, "/v1/sms/send", request, &response); err != nil {
		return SMSSendResponse{}, err
	}
	if err := validateSMSSendResponse(response, request); err != nil {
		return SMSSendResponse{}, fmt.Errorf("invalid SMS send response: %w", err)
	}
	return response, nil
}

func (client *Client) AcknowledgeSMS(ctx context.Context, request SMSAcknowledgeRequest) (SMSAcknowledgeResponse, error) {
	if err := validateSMSAcknowledgeRequest(request); err != nil {
		return SMSAcknowledgeResponse{}, err
	}
	var response SMSAcknowledgeResponse
	if err := client.request(ctx, http.MethodPost, "/v1/sms/acknowledge", request, &response); err != nil {
		return SMSAcknowledgeResponse{}, err
	}
	if err := validateSMSAcknowledgeResponse(response, request); err != nil {
		return SMSAcknowledgeResponse{}, fmt.Errorf("invalid SMS acknowledge response: %w", err)
	}
	return response, nil
}

func (client *Client) request(ctx context.Context, method, path string, body, output any) error {
	if client == nil || client.http == nil {
		return errors.New("agent client is unavailable")
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode agent request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://unix"+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("contact agent at %s: %w", client.socketPath, err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, 4<<20)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var apiError ErrorResponse
		if err := json.NewDecoder(limited).Decode(&apiError); err != nil {
			return fmt.Errorf("agent returned HTTP %d", response.StatusCode)
		}
		return &apiError
	}
	decoder := json.NewDecoder(limited)
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("decode agent response: %w", err)
	}
	return nil
}
