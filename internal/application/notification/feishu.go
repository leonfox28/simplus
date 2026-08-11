package notification

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	feishuAccountsOrigin   = "https://accounts.feishu.cn"
	feishuOpenAPIOrigin    = "https://open.feishu.cn"
	feishuRegistrationPath = "/oauth/v1/app/registration"
	feishuTenantTokenPath  = "/open-apis/auth/v3/tenant_access_token/internal"
	feishuMessagePath      = "/open-apis/im/v1/messages"
	providerResponseLimit  = 64 << 10
)

var providerCredentialPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,512}$`)

var (
	ErrFeishuAuthorizationDenied   = errors.New("feishu authorization denied")
	ErrFeishuAuthorizationExpired  = errors.New("feishu authorization expired")
	ErrFeishuProviderUnavailable   = errors.New("feishu provider unavailable")
	ErrFeishuProviderResultInvalid = errors.New("feishu provider result invalid")
	ErrFeishuLarkUnsupported       = errors.New("lark tenant is unsupported")
)

type FeishuRegistration struct {
	DeviceCode      string
	VerificationURL string
	ExpiresAt       time.Time
	PollInterval    time.Duration
}

type FeishuRegistrationResult struct {
	AppID, AppSecret, OpenID, TenantBrand string
}

type FeishuRegistrar interface {
	Begin(context.Context) (FeishuRegistration, error)
	Poll(context.Context, FeishuRegistration) (FeishuRegistrationResult, error)
}

type FeishuMessenger interface {
	SendText(context.Context, FeishuRegistrationResult, string) error
}

type FeishuClient struct {
	Client *http.Client
	Now    func() time.Time
	Wait   func(context.Context, time.Duration) error
}

func NewFeishuClient() *FeishuClient {
	client := &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("feishu redirects are not allowed") },
	}
	return &FeishuClient{Client: client, Now: time.Now, Wait: waitForFeishuPoll}
}

func waitForFeishuPoll(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (client *FeishuClient) Begin(ctx context.Context) (FeishuRegistration, error) {
	form := url.Values{
		"action": {"begin"}, "archetype": {"PersonalAgent"},
		"auth_method": {"client_secret"}, "request_user_info": {"open_id"},
	}
	var response struct {
		DeviceCode              string `json:"device_code"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		Interval                int    `json:"interval"`
		ExpireIn                int    `json:"expire_in"`
	}
	if err := client.registrationRequest(ctx, form, &response); err != nil {
		return FeishuRegistration{}, err
	}
	if !providerCredentialPattern.MatchString(response.DeviceCode) {
		return FeishuRegistration{}, ErrFeishuProviderResultInvalid
	}
	interval := response.Interval
	if interval <= 0 {
		interval = 5
	}
	expireIn := response.ExpireIn
	if expireIn <= 0 {
		expireIn = 600
	}
	if interval > 60 || expireIn > 900 || expireIn < interval {
		return FeishuRegistration{}, ErrFeishuProviderResultInvalid
	}
	verificationURL, err := buildFeishuVerificationURL(response.VerificationURIComplete)
	if err != nil {
		return FeishuRegistration{}, err
	}
	return FeishuRegistration{
		DeviceCode: response.DeviceCode, VerificationURL: verificationURL,
		ExpiresAt:    client.Now().UTC().Add(time.Duration(expireIn) * time.Second),
		PollInterval: time.Duration(interval) * time.Second,
	}, nil
}

func buildFeishuVerificationURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != "accounts.feishu.cn" || parsed.Port() != "" || parsed.User != nil || parsed.Fragment != "" || len(raw) > 2048 {
		return "", ErrFeishuProviderResultInvalid
	}
	addons, err := encodeFeishuAddons()
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Set("from", "sdk")
	query.Set("tp", "sdk")
	query.Set("source", "go-sdk/simplus")
	query.Set("name", "Simplus 飞书通知")
	query.Set("desc", "Simplus 单向通知应用")
	query.Set("createOnly", "true")
	query.Set("addons", addons)
	parsed.RawQuery = query.Encode()
	if len(parsed.String()) > 4096 {
		return "", ErrFeishuProviderResultInvalid
	}
	return parsed.String(), nil
}

func encodeFeishuAddons() (string, error) {
	body, err := json.Marshal(map[string]any{
		"preset": false,
		"scopes": map[string][]string{"tenant": {"im:message:send_as_bot"}},
	})
	if err != nil {
		return "", err
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(body); err != nil {
		_ = writer.Close()
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(compressed.Bytes()), nil
}

func (client *FeishuClient) Poll(ctx context.Context, registration FeishuRegistration) (FeishuRegistrationResult, error) {
	interval := registration.PollInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	for {
		remaining := registration.ExpiresAt.Sub(client.Now())
		if remaining <= 0 {
			return FeishuRegistrationResult{}, ErrFeishuAuthorizationExpired
		}
		wait := interval
		if remaining < wait {
			wait = remaining
		}
		if err := client.Wait(ctx, wait); err != nil {
			return FeishuRegistrationResult{}, err
		}
		if !client.Now().Before(registration.ExpiresAt) {
			return FeishuRegistrationResult{}, ErrFeishuAuthorizationExpired
		}
		var response struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
			UserInfo     *struct {
				OpenID      string `json:"open_id"`
				TenantBrand string `json:"tenant_brand"`
			} `json:"user_info"`
			Error string `json:"error"`
		}
		if err := client.registrationRequest(ctx, url.Values{"action": {"poll"}, "device_code": {registration.DeviceCode}}, &response); err != nil {
			return FeishuRegistrationResult{}, err
		}
		if response.UserInfo != nil && strings.EqualFold(response.UserInfo.TenantBrand, "lark") {
			return FeishuRegistrationResult{}, ErrFeishuLarkUnsupported
		}
		if response.ClientID != "" || response.ClientSecret != "" {
			if response.UserInfo == nil {
				return FeishuRegistrationResult{}, ErrFeishuProviderResultInvalid
			}
			result := FeishuRegistrationResult{AppID: response.ClientID, AppSecret: response.ClientSecret, OpenID: response.UserInfo.OpenID, TenantBrand: response.UserInfo.TenantBrand}
			if err := validateFeishuResult(result); err != nil {
				return FeishuRegistrationResult{}, err
			}
			return result, nil
		}
		switch response.Error {
		case "authorization_pending", "":
		case "slow_down":
			interval += 5 * time.Second
			if interval > 60*time.Second {
				return FeishuRegistrationResult{}, ErrFeishuProviderResultInvalid
			}
		case "access_denied":
			return FeishuRegistrationResult{}, ErrFeishuAuthorizationDenied
		case "expired_token":
			return FeishuRegistrationResult{}, ErrFeishuAuthorizationExpired
		default:
			return FeishuRegistrationResult{}, ErrFeishuProviderUnavailable
		}
	}
}

func validateFeishuResult(result FeishuRegistrationResult) error {
	if strings.EqualFold(result.TenantBrand, "lark") {
		return ErrFeishuLarkUnsupported
	}
	if result.TenantBrand != "feishu" || !providerCredentialPattern.MatchString(result.AppID) || !providerCredentialPattern.MatchString(result.AppSecret) || !providerCredentialPattern.MatchString(result.OpenID) {
		return ErrFeishuProviderResultInvalid
	}
	return nil
}

func (client *FeishuClient) registrationRequest(ctx context.Context, form url.Values, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, feishuAccountsOrigin+feishuRegistrationPath, strings.NewReader(form.Encode()))
	if err != nil {
		return ErrFeishuProviderUnavailable
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return client.doJSON(request, target)
}

func (client *FeishuClient) SendText(ctx context.Context, credentials FeishuRegistrationResult, message string) error {
	if err := validateFeishuResult(credentials); err != nil || message == "" || len([]rune(message)) > 4000 {
		if err != nil {
			return err
		}
		return ErrChannelInvalid
	}
	tokenBody, err := json.Marshal(map[string]string{"app_id": credentials.AppID, "app_secret": credentials.AppSecret})
	if err != nil {
		return ErrFeishuProviderUnavailable
	}
	tokenRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, feishuOpenAPIOrigin+feishuTenantTokenPath, bytes.NewReader(tokenBody))
	if err != nil {
		return ErrFeishuProviderUnavailable
	}
	tokenRequest.Header.Set("Content-Type", "application/json")
	var tokenResponse struct {
		Code              *int   `json:"code"`
		TenantAccessToken string `json:"tenant_access_token"`
	}
	if err := client.doJSON(tokenRequest, &tokenResponse); err != nil {
		return err
	}
	if tokenResponse.Code == nil || *tokenResponse.Code != 0 || !providerCredentialPattern.MatchString(tokenResponse.TenantAccessToken) {
		return ErrFeishuProviderUnavailable
	}
	content, _ := json.Marshal(map[string]string{"text": message})
	messageBody, err := json.Marshal(map[string]string{"receive_id": credentials.OpenID, "msg_type": "text", "content": string(content)})
	if err != nil {
		return ErrFeishuProviderUnavailable
	}
	messageRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, feishuOpenAPIOrigin+feishuMessagePath+"?receive_id_type=open_id", bytes.NewReader(messageBody))
	if err != nil {
		return ErrFeishuProviderUnavailable
	}
	messageRequest.Header.Set("Authorization", "Bearer "+tokenResponse.TenantAccessToken)
	messageRequest.Header.Set("Content-Type", "application/json")
	var messageResponse struct {
		Code *int `json:"code"`
	}
	if err := client.doJSON(messageRequest, &messageResponse); err != nil {
		return err
	}
	if messageResponse.Code == nil || *messageResponse.Code != 0 {
		return ErrFeishuProviderUnavailable
	}
	return nil
}

func (client *FeishuClient) doJSON(request *http.Request, target any) error {
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Simplus")
	response, err := client.Client.Do(request)
	if err != nil {
		return fmt.Errorf("%w", ErrFeishuProviderUnavailable)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, providerResponseLimit+1))
	if err != nil || len(body) > providerResponseLimit || response.StatusCode < 200 || response.StatusCode >= 300 || len(bytes.TrimSpace(body)) == 0 {
		return ErrFeishuProviderUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(target); err != nil {
		return ErrFeishuProviderResultInvalid
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ErrFeishuProviderResultInvalid
	}
	return nil
}
