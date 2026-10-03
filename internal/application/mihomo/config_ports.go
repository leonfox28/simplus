package mihomo

import (
	"errors"
	"strings"
	"time"

	"github.com/leonfox28/simplus/internal/domain/lineegress"
)

type CountrySummary struct {
	Code, Name string
	NodeCount  int
}

type ArtifactMetadata struct {
	SubscriptionID string           `json:"subscriptionId"`
	Version        string           `json:"version"`
	RawSHA256      string           `json:"rawSha256"`
	ConfigSHA256   string           `json:"configSha256"`
	CoreVersion    string           `json:"coreVersion"`
	GeneratedAt    time.Time        `json:"generatedAt"`
	Countries      []CountrySummary `json:"countries"`
}

type ConfigStatus struct {
	Published              bool      `json:"published"`
	Launchable             bool      `json:"launchable"`
	SHA256                 string    `json:"sha256"`
	GeneratedAt            time.Time `json:"generatedAt"`
	ErrorCode              string    `json:"errorCode"`
	SelectedSubscriptionID string    `json:"selectedSubscriptionId"`
	RunningSubscriptionID  string    `json:"runningSubscriptionId"`
}

func countryPort(code string) int {
	return lineegress.CountryListenerPort(strings.ToUpper(code))
}

func CountryListenerPort(code string) int {
	return lineegress.CountryListenerPort(code)
}

var (
	ErrConfigNotReady         = errors.New("Mihomo configuration is not ready")
	ErrConfigValidationFailed = errors.New("Mihomo configuration validation failed")
)
