package modem

import (
	"time"

	"github.com/leonfox28/simplus/internal/domain/hardware"
)

const (
	StateOnline  = "online"
	StateOffline = "offline"

	RFStateOn      = "on"
	RFStateOff     = "off"
	RFStateUnknown = "unknown"

	SIMPresencePresent = "present"
	SIMPresenceAbsent  = "absent"
	SIMPresenceUnknown = "unknown"

	CellularRegisteredHome    = "registered-home"
	CellularRegisteredRoaming = "registered-roaming"
	CellularSearching         = "searching"
	CellularDenied            = "denied"
	CellularNotRegistered     = "not-registered"
	CellularRFOff             = "rf-off"
	CellularSIMNotReady       = "sim-not-ready"
	CellularUnavailable       = "unavailable"
	CellularUnknown           = "unknown"

	SupportSupported = "supported"
	SupportNotReady  = "not-ready"

	ReadinessReady                        = "READY"
	ReadinessControlUnavailable           = "CONTROL_UNAVAILABLE"
	ReadinessSIMAccessUnavailable         = "SIM_ACCESS_UNAVAILABLE"
	ReadinessEquipmentIdentityUnavailable = "EQUIPMENT_IDENTITY_UNAVAILABLE"
	ReadinessIdentityConflict             = "IDENTITY_CONFLICT"
)

// Record is administrator-owned configuration. EquipmentIdentityFingerprint
// is the stable, instance-scoped binding derived from the modem IMEI.
// LegacyHardwareDeviceID exists only to promote records created by migration
// 18; new records never persist a USB topology location.
type Record struct {
	ID                           string
	LegacyHardwareDeviceID       string
	EquipmentIdentityFingerprint string
	USBSerialFingerprint         string
	DisplayName                  string
	Model                        string
	Transport                    string
	Capabilities                 hardware.Capabilities
	CreatedAt                    time.Time
	UpdatedAt                    time.Time
}

type View struct {
	ID           string
	DisplayName  string
	Model        string
	SerialNumber string
	Transport    string
	State        string
	Capabilities hardware.Capabilities
	RFState      string
	SIMPresence  string
	Cellular     CellularStatus
	AddedAt      time.Time
}

type CellularRegistration struct {
	Domain string
	State  string
}

type CellularStatus struct {
	State         string
	ErrorCode     string
	Registrations []CellularRegistration
	OperatorName  string
	OperatorCode  string
	RAT           string
	SignalState   string
	SignalRSSIDBm int
	ObservedAt    time.Time
}

type RuntimeStatus struct {
	RFState     string
	SIMPresence string
	Cellular    CellularStatus
}

func UnavailableCellularStatus() CellularStatus {
	return CellularStatus{
		State: CellularUnavailable, ErrorCode: "CELLULAR_STATUS_UNAVAILABLE",
		Registrations: []CellularRegistration{{Domain: "cs", State: "unknown"}, {Domain: "packet", State: "unknown"}, {Domain: "eps", State: "unknown"}},
		SignalState:   "unknown",
	}
}

// Candidate is a transient, read-only hardware observation. CandidateID is
// valid only for selecting the current observation in Add; it is not a stable
// business identity.
type Candidate struct {
	CandidateID   string
	USBAddress    string
	USBVendorID   string
	USBProductID  string
	USBSerialHint string
	Model         string
	Transport     string
	Support       string
	Addable       bool
	Readiness     string
	Capabilities  hardware.Capabilities
	SIMPresence   string
}
