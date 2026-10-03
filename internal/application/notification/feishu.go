package notification

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
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

func ValidateFeishuResult(result FeishuRegistrationResult) error {
	if strings.EqualFold(result.TenantBrand, "lark") {
		return ErrFeishuLarkUnsupported
	}
	if result.TenantBrand != "feishu" || !providerCredentialPattern.MatchString(result.AppID) || !providerCredentialPattern.MatchString(result.AppSecret) || !providerCredentialPattern.MatchString(result.OpenID) {
		return ErrFeishuProviderResultInvalid
	}
	return nil
}

var (
	ErrFeishuAuthorizationDenied   = errors.New("feishu authorization denied")
	ErrFeishuAuthorizationExpired  = errors.New("feishu authorization expired")
	ErrFeishuProviderUnavailable   = errors.New("feishu provider unavailable")
	ErrFeishuProviderResultInvalid = errors.New("feishu provider result invalid")
	ErrFeishuLarkUnsupported       = errors.New("lark tenant is unsupported")
)
var providerCredentialPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,512}$`)

type DisabledFeishu struct{}

func (DisabledFeishu) Begin(context.Context) (FeishuRegistration, error) {
	return FeishuRegistration{}, ErrBindingUnavailable
}
func (DisabledFeishu) Poll(context.Context, FeishuRegistration) (FeishuRegistrationResult, error) {
	return FeishuRegistrationResult{}, ErrBindingUnavailable
}
func (DisabledFeishu) SendText(context.Context, FeishuRegistrationResult, string) error {
	return ErrBindingUnavailable
}
