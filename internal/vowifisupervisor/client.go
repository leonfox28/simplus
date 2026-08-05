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
		default:
			return fmt.Errorf("Host VoWiFi supervisor returned HTTP %d", response.StatusCode)
		}
	}
	if output != nil {
		return json.NewDecoder(io.LimitReader(response.Body, 16<<10)).Decode(output)
	}
	return nil
}
