package vowifisupervisor

import (
	"context"
	"errors"
	"time"
)

const (
	EgressDirect        = "direct"
	EgressMihomoCountry = "mihomo-country"

	StateStopped      = "stopped"
	StateStarting     = "starting"
	StateConnecting   = "connecting"
	StateRegistering  = "registering"
	StateOnline       = "online"
	StateReconnecting = "reconnecting"
	StateStopping     = "stopping"
	StateFailed       = "failed"
)

var (
	ErrAlreadyRunning = errors.New("Host VoWiFi Line is already running")
	ErrNotRunning     = errors.New("Host VoWiFi Line is not running")
	ErrRequestInvalid = errors.New("Host VoWiFi supervisor request is invalid")
	ErrStartupFailed  = errors.New("Host VoWiFi worker startup failed")
)

// StartRequest deliberately contains only stable business selections. The
// privileged supervisor resolves all executable paths, ports, network object
// names and current SIM/Agent fences itself.
type StartRequest struct {
	LineID      string `json:"lineId"`
	EgressMode  string `json:"egressMode"`
	CountryCode string `json:"countryCode"`
}

type StopRequest struct {
	LineID string `json:"lineId"`
}

// Status is safe for the management plane. It intentionally excludes PID,
// namespace/interface names, addresses, P-CSCF, SPIs and authentication data.
type Status struct {
	LineID       string    `json:"lineId"`
	State        string    `json:"state"`
	Stage        string    `json:"stage,omitempty"`
	Online       bool      `json:"online"`
	EgressMode   string    `json:"egressMode"`
	CountryCode  string    `json:"countryCode"`
	StartedAt    time.Time `json:"startedAt,omitempty"`
	RegisteredAt time.Time `json:"registeredAt,omitempty"`
	NextRefresh  time.Time `json:"nextRefreshAt,omitempty"`
	Attempt      int       `json:"attempt"`
	ErrorCode    string    `json:"errorCode,omitempty"`
}

type StatusList struct {
	Lines []Status `json:"lines"`
}

type API interface {
	List(context.Context) ([]Status, error)
	Start(context.Context, StartRequest) (Status, error)
	Stop(context.Context, string) (Status, error)
}
