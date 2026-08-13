package line

import (
	"errors"
	"time"

	"github.com/leonfox28/simplus/internal/domain/hardware"
)

var ErrNotFound = errors.New("managed line was not found")

const (
	StateReady          = "ready"
	StateModemOffline   = "modem-offline"
	StateSIMUnavailable = "sim-unavailable"

	CandidateReady           = "READY"
	CandidateModemOffline    = "MODEM_OFFLINE"
	CandidateSIMAbsent       = "SIM_ABSENT"
	CandidateSIMUnavailable  = "SIM_UNAVAILABLE"
	CandidateAlreadyAdded    = "ALREADY_ADDED"
	CandidateBindingConflict = "BINDING_CONFLICT"

	PhoneNumberSourceCellularSIM = "cellular-sim"
	PhoneNumberSourceIMS         = "ims"
)

type PhoneNumberObservation struct {
	Number  string
	Sources []string
}

// Record is the administrator-owned Line configuration. It binds a stable
// ManagedModem to one SIM/Profile identity without persisting any Agent,
// sysfs, device-node or model-specific identifier.
type Record struct {
	ID                              string
	ManagedModemID                  string
	SIMSlotIndex                    int
	SubscriptionIdentityFingerprint string
	SubscriptionDisplayHint         string
	DisplayName                     string
	CreatedAt                       time.Time
	UpdatedAt                       time.Time
}

type View struct {
	ID                       string
	DisplayName              string
	ManagedModemID           string
	ManagedModemDisplayName  string
	ManagedModemModel        string
	ManagedModemSerialNumber string
	SubscriptionDisplayHint  string
	State                    string
	Capabilities             hardware.Capabilities
	PhoneNumbers             []PhoneNumberObservation
	CreatedAt                time.Time
}

// Candidate is a transient selection derived from a fresh hardware
// observation. CandidateID may be submitted only to Create and is never a
// stable business identity.
type Candidate struct {
	CandidateID              string
	ManagedModemID           string
	ManagedModemDisplayName  string
	ManagedModemModel        string
	ManagedModemSerialNumber string
	SubscriptionDisplayHint  string
	HomeOperatorName         string
	HomeOperatorCode         string
	SIMPresence              string
	Capabilities             hardware.Capabilities
	Addable                  bool
	Readiness                string
}
