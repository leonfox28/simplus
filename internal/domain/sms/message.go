package sms

import (
	"errors"
	"time"
)

const (
	DirectionOutbound = "outbound"
	DirectionInbound  = "inbound"

	StatusQueued      = "queued"
	StatusUnconfirmed = "unconfirmed"
	StatusSent        = "sent"
	StatusFailed      = "failed"
	StatusReceived    = "received"
)

var (
	ErrOperationConflict = errors.New("SMS operation id belongs to different parameters")
	ErrMessageNotFound   = errors.New("SMS message was not found")
	ErrSourceConflict    = errors.New("inbound SMS source id belongs to different content")
	ErrStateConflict     = errors.New("SMS message state conflicts with the requested transition")
)

// Message is the durable, transport-neutral SMS representation used by the
// application service and messages.sqlite3. Transport-specific handles never
// become Web/API inputs.
type Message struct {
	ID                string
	OperationID       string
	Direction         string
	LineID            string
	RemoteAddress     string
	Body              string
	Status            string
	ProviderMessageID string
	ErrorCode         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	SentAt            *time.Time
}
