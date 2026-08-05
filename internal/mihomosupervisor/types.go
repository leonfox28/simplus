package mihomosupervisor

import (
	"context"
	"errors"
	"time"
)

var (
	ErrAlreadyRunning = errors.New("Mihomo is already running")
	ErrNotRunning     = errors.New("Mihomo is not running")
	ErrRequestInvalid = errors.New("Mihomo supervisor request is invalid")
	ErrStartupFailed  = errors.New("Mihomo startup failed")
)

type StartRequest struct {
	SubscriptionID string `json:"subscriptionId"`
	BinaryPath     string `json:"binaryPath"`
	ConfigPath     string `json:"configPath"`
}

type Status struct {
	Running        bool      `json:"running"`
	PID            int       `json:"pid"`
	SubscriptionID string    `json:"subscriptionId"`
	BinaryPath     string    `json:"binaryPath"`
	ConfigPath     string    `json:"configPath"`
	StartedAt      time.Time `json:"startedAt"`
}

type API interface {
	Status(context.Context) (Status, error)
	Start(context.Context, StartRequest) (Status, error)
	Stop(context.Context) error
}
