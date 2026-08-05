package call

import (
	"errors"
	"time"
)

var (
	ErrNotFound      = errors.New("call not found")
	ErrStateConflict = errors.New("call state conflict")
)

const (
	DirectionInbound  = "inbound"
	DirectionOutbound = "outbound"

	StateIncoming = "incoming"
	StateDialing  = "dialing"
	StateActive   = "active"
	StateEnded    = "ended"
	StateFailed   = "failed"
)

type Record struct {
	ID            string
	OperationID   string
	LineID        string
	RemoteAddress string
	Direction     string
	State         string
	EndReason     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	AnsweredAt    *time.Time
	EndedAt       *time.Time
}
