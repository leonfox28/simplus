package control

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	setupapp "github.com/leonfox28/simplus/internal/application/setup"
)

const bootstrapPath = "/v1/bootstrap"
const provisionAdministratorPath = "/v1/administrator/provision"

type BootstrapResponse struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type ProvisionAdministratorRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Locale   string `json:"locale"`
}

type ProvisionAdministratorResponse struct {
	Created bool `json:"created"`
}

type BootstrapHandler struct {
	setup  *setupapp.Service
	logger *slog.Logger
}

func NewBootstrapHandler(service *setupapp.Service, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	handler := &BootstrapHandler{setup: service, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc(bootstrapPath, handler.generate)
	mux.HandleFunc(provisionAdministratorPath, handler.provisionAdministrator)
	return mux
}

func (handler *BootstrapHandler) provisionAdministrator(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request ProvisionAdministratorRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, 4097))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeControlJSON(w, http.StatusBadRequest, map[string]string{"code": "PROVISION_REQUEST_INVALID"})
		return
	}
	created, err := handler.setup.ProvisionAdministrator(r.Context(), setupapp.AdministratorInput{
		Username: request.Username, Password: request.Password, PasswordConfirmation: request.Password, InstanceDefaultLocale: request.Locale,
	})
	if err != nil {
		if errors.Is(err, setupapp.ErrSetupUnavailable) {
			writeControlJSON(w, http.StatusConflict, map[string]string{"code": "SETUP_UNAVAILABLE"})
			return
		}
		handler.logger.ErrorContext(r.Context(), "root administrator provisioning failed", "error", err)
		writeControlJSON(w, http.StatusInternalServerError, map[string]string{"code": "ADMINISTRATOR_PROVISION_FAILED"})
		return
	}
	writeControlJSON(w, http.StatusOK, ProvisionAdministratorResponse{Created: created})
}

func ProvisionAdministrator(ctx context.Context, socketPath string, requestBody ProvisionAdministratorRequest) (ProvisionAdministratorResponse, error) {
	return postControlJSON[ProvisionAdministratorRequest, ProvisionAdministratorResponse](ctx, socketPath, provisionAdministratorPath, requestBody)
}

func postControlJSON[Request any, Response any](ctx context.Context, socketPath, path string, requestBody Request) (Response, error) {
	var result Response
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", socketPath)
	}, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	body, err := json.Marshal(requestBody)
	if err != nil {
		return result, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://simplusd"+path, bytes.NewReader(body))
	if err != nil {
		return result, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Transport: transport}).Do(request)
	if err != nil {
		return result, fmt.Errorf("contact root control socket: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil {
		return result, err
	}
	if response.StatusCode != http.StatusOK {
		return result, fmt.Errorf("root control request rejected with HTTP %d", response.StatusCode)
	}
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return result, err
	}
	return result, nil
}

func (handler *BootstrapHandler) generate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	grant, err := handler.setup.GenerateBootstrap(r.Context())
	if err != nil {
		if errors.Is(err, setupapp.ErrSetupUnavailable) {
			writeControlJSON(w, http.StatusConflict, map[string]string{"code": "SETUP_UNAVAILABLE"})
			return
		}
		handler.logger.ErrorContext(r.Context(), "root bootstrap generation failed", "error", err)
		writeControlJSON(w, http.StatusInternalServerError, map[string]string{"code": "BOOTSTRAP_GENERATION_FAILED"})
		return
	}
	writeControlJSON(w, http.StatusCreated, BootstrapResponse{Code: grant.Code, ExpiresAt: grant.ExpiresAt})
}

func GenerateBootstrap(ctx context.Context, socketPath string) (BootstrapResponse, error) {
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
		DisableKeepAlives: true,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://simplusd"+bootstrapPath, nil)
	if err != nil {
		return BootstrapResponse{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return BootstrapResponse{}, fmt.Errorf("contact root control socket: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil {
		return BootstrapResponse{}, fmt.Errorf("read root control response: %w", err)
	}
	if len(body) > 4096 {
		return BootstrapResponse{}, fmt.Errorf("root control response is too large")
	}
	if response.StatusCode != http.StatusCreated {
		var failure struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(body, &failure) == nil && failure.Code != "" {
			return BootstrapResponse{}, fmt.Errorf("root control rejected bootstrap generation: %s", failure.Code)
		}
		return BootstrapResponse{}, fmt.Errorf("root control rejected bootstrap generation with HTTP %d", response.StatusCode)
	}
	var result BootstrapResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return BootstrapResponse{}, fmt.Errorf("decode root control response: %w", err)
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(result.Code)
	if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != result.Code {
		return BootstrapResponse{}, fmt.Errorf("root control returned an invalid bootstrap code")
	}
	if result.ExpiresAt.IsZero() {
		return BootstrapResponse{}, fmt.Errorf("root control returned no expiry")
	}
	return result, nil
}

func writeControlJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
