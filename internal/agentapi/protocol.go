package agentapi

import (
	"encoding/hex"
	"strings"
	"time"

	"github.com/leonfox28/simplus/internal/buildinfo"
)

const (
	ProtocolName    = "simplus-agent"
	ProtocolVersion = 1
)

func IsValidAgentInstanceID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	decoded, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	return err == nil && len(decoded) == 16
}

const (
	ProfileQDC507 = "qdc507"
	ProfileML307A = "ml307a"
)

const (
	EndpointTTY  = "tty"
	EndpointQMI  = "qmi"
	EndpointNet  = "net"
	EndpointALSA = "alsa"
)

const (
	EvidenceObserved    = "observed"
	EvidenceDocumented  = "documented"
	EvidenceUnverified  = "unverified"
	EvidenceUnsupported = "unsupported"
	EvidenceUnavailable = "unavailable"
)

const (
	ProbeStateComplete       = "complete"
	ProbeStateDescriptorOnly = "descriptor-only"
	ProbeStateBusy           = "busy"
	ProbeStateUnavailable    = "unavailable"
	ProbeStateFailed         = "failed"
)

const (
	SIMStatePresent = "present"
	SIMStateAbsent  = "absent"
	SIMStateLocked  = "locked"
	SIMStateUnknown = "unknown"
)

const (
	PrimaryLockReady              = "ready"
	PrimaryLockPIN1Required       = "pin1-required"
	PrimaryLockPUK1Required       = "puk1-required"
	PrimaryLockPermanentlyBlocked = "permanently-blocked"
	PrimaryLockUnsupported        = "unsupported-lock"
	PrimaryLockUnknown            = "unknown"
)

const (
	RFStateOff     = "off"
	RFStateMinimum = "minimum"
	RFStateOn      = "on"
	RFStateUnknown = "unknown"
)

const (
	SignalStateMeasured    = "measured"
	SignalStateUnavailable = "unavailable"
	SignalStateUnknown     = "unknown"
)

const (
	RegistrationNotRegistered           = "not-registered"
	RegistrationSearching               = "searching"
	RegistrationDenied                  = "denied"
	RegistrationRegisteredHome          = "registered-home"
	RegistrationRegisteredRoaming       = "registered-roaming"
	RegistrationRegisteredSMSHome       = "registered-sms-home"
	RegistrationRegisteredSMSRoaming    = "registered-sms-roaming"
	RegistrationEmergencyOnly           = "emergency-only"
	RegistrationHomeCSFBNotPreferred    = "registered-home-csfb-not-preferred"
	RegistrationRoamingCSFBNotPreferred = "registered-roaming-csfb-not-preferred"
	RegistrationUnknown                 = "unknown"
)

const (
	RegistrationDomainCS     = "cs"
	RegistrationDomainPacket = "packet"
	RegistrationDomainEPS    = "eps"
)

const (
	NetworkSelectionAutomatic       = "automatic"
	NetworkSelectionManual          = "manual"
	NetworkSelectionDeregistered    = "deregistered"
	NetworkSelectionManualAutomatic = "manual-automatic"
	NetworkSelectionUnknown         = "unknown"
)

const (
	RATGSM        = "gsm"
	RATGSMCompact = "gsm-compact"
	RATUTRAN      = "utran"
	RATGSMEdge    = "gsm-edge"
	RATUTRANHSDPA = "utran-hsdpa"
	RATUTRANHSUPA = "utran-hsupa"
	RATUTRANHSPA  = "utran-hspa"
	RATLTE        = "lte"
	RATECGSMIoT   = "ec-gsm-iot"
	RATNBIoT      = "nb-iot"
	RATLTE5GC     = "e-utra-5gc"
	RATNR5GC      = "nr-5gc"
	RATNGRAN      = "ng-ran"
	RATNR         = "nr"
	RATCDMA       = "cdma"
)

const (
	ErrorLayerPlatform  = "platform"
	ErrorLayerDevice    = "device"
	ErrorLayerTransport = "transport"
	ErrorLayerRadio     = "radio"
	ErrorLayerSIM       = "sim"
	ErrorLayerCall      = "call"
)

const (
	ErrorPlatformUnsupported      = "PLATFORM_UNSUPPORTED"
	ErrorControlEndpointBusy      = "CONTROL_ENDPOINT_BUSY"
	ErrorControlEndpointMissing   = "CONTROL_ENDPOINT_UNAVAILABLE"
	ErrorControlPermissionDenied  = "CONTROL_PERMISSION_DENIED"
	ErrorControlEndpointOpen      = "CONTROL_ENDPOINT_OPEN_FAILED"
	ErrorControlEndpointConfigure = "CONTROL_ENDPOINT_CONFIGURE_FAILED"
	ErrorModemNoResponse          = "MODEM_NO_RESPONSE"
	ErrorProbeCancelled           = "PROBE_CANCELLED"
	ErrorRFStateUnavailable       = "RF_STATE_UNAVAILABLE"
	ErrorSIMStateUnavailable      = "SIM_STATE_UNAVAILABLE"
	ErrorCallStateUnknown         = "CALL_STATE_UNKNOWN"
	ErrorActiveCallPresent        = "ACTIVE_CALL_PRESENT"
	ErrorRadioOffCommandRejected  = "RADIO_OFF_COMMAND_REJECTED"
	ErrorRadioOffNotConfirmed     = "RADIO_OFF_NOT_CONFIRMED"
	ErrorRadioOffOutcomeUncertain = "RADIO_OFF_OUTCOME_UNCERTAIN"
	ErrorHardwareChanged          = "HARDWARE_CHANGED"
)

const (
	CommandRadioEnsureOff = "radio.ensure-off"
)

const (
	CommandOutcomeAccepted  = "accepted"
	CommandOutcomeSucceeded = "succeeded"
	CommandOutcomeFailed    = "failed"
	CommandOutcomeUncertain = "uncertain"
)

const (
	OutcomeCodeAccepted          = "COMMAND_ACCEPTED"
	OutcomeCodeRadioOffConfirmed = "RADIO_OFF_CONFIRMED"
)

type Hello struct {
	Protocol        string         `json:"protocol"`
	ProtocolVersion int            `json:"protocolVersion"`
	AgentInstanceID string         `json:"agentInstanceId"`
	Agent           buildinfo.Info `json:"agent"`
	Features        []string       `json:"features"`
}

type USBIdentity struct {
	VendorID          string `json:"vendorId"`
	ProductID         string `json:"productId"`
	BCDDevice         string `json:"bcdDevice,omitempty"`
	Manufacturer      string `json:"manufacturer,omitempty"`
	Product           string `json:"product,omitempty"`
	SerialPresent     bool   `json:"serialPresent"`
	SerialNumber      string `json:"serialNumber,omitempty"`
	SerialFingerprint string `json:"serialFingerprint,omitempty"`
	Configuration     int    `json:"configuration,omitempty"`
	InterfaceCount    int    `json:"interfaceCount"`
}

type Endpoint struct {
	Kind            string `json:"kind"`
	InterfaceNumber int    `json:"interfaceNumber"`
	Driver          string `json:"driver,omitempty"`
	Node            string `json:"node,omitempty"`
}

type USBInterface struct {
	Number    int        `json:"number"`
	Class     string     `json:"class"`
	Subclass  string     `json:"subclass"`
	Protocol  string     `json:"protocol"`
	Driver    string     `json:"driver,omitempty"`
	Endpoints []Endpoint `json:"endpoints"`
}

type CapabilityEvidence struct {
	Capability string   `json:"capability"`
	Status     string   `json:"status"`
	Evidence   []string `json:"evidence"`
}

type DeviceReport struct {
	ID           string               `json:"id"`
	Generation   uint64               `json:"generation"`
	PhysicalPath string               `json:"physicalPath"`
	Profile      string               `json:"profile,omitempty"`
	DisplayName  string               `json:"displayName"`
	USB          USBIdentity          `json:"usb"`
	Interfaces   []USBInterface       `json:"interfaces"`
	Capabilities []CapabilityEvidence `json:"capabilities"`
}

type Snapshot struct {
	ProtocolVersion int            `json:"protocolVersion"`
	AgentInstanceID string         `json:"agentInstanceId"`
	Generation      uint64         `json:"generation"`
	Revision        string         `json:"revision"`
	ObservedAt      time.Time      `json:"observedAt"`
	Devices         []DeviceReport `json:"devices"`
}

type ChangeResponse struct {
	Changed  bool     `json:"changed"`
	Snapshot Snapshot `json:"snapshot"`
}

type ProbeRequest struct {
	DeviceIDs []string `json:"deviceIds,omitempty"`
}

type ModemIdentity struct {
	Manufacturer                 string `json:"manufacturer,omitempty"`
	Model                        string `json:"model,omitempty"`
	Revision                     string `json:"revision,omitempty"`
	EquipmentIdentityFingerprint string `json:"equipmentIdentityFingerprint,omitempty"`
}

type RFObservation struct {
	State          string `json:"state"`
	Mode           *int   `json:"mode,omitempty"`
	FunctionalMode string `json:"functionalMode,omitempty"`
	Network        string `json:"network,omitempty"`
	Signal         string `json:"signal,omitempty"`
}

type SIMObservation struct {
	State               string `json:"state"`
	PrimaryLockState    string `json:"primaryLockState"`
	LockType            string `json:"lockType,omitempty"`
	AttemptsRemaining   *int   `json:"attemptsRemaining,omitempty"`
	AttemptsSource      string `json:"attemptsSource,omitempty"`
	IdentityFingerprint string `json:"identityFingerprint,omitempty"`
	DisplayIdentityHint string `json:"displayIdentityHint,omitempty"`
}

type SignalObservation struct {
	State   string `json:"state"`
	RSSI    *int   `json:"rssi,omitempty"`
	RSSIDBm *int   `json:"rssiDbm,omitempty"`
	BER     *int   `json:"ber,omitempty"`
	Source  string `json:"source,omitempty"`
}

type RegistrationObservation struct {
	Domain string `json:"domain"`
	State  string `json:"state"`
	Source string `json:"source"`
}

type NetworkObservation struct {
	SelectionMode string `json:"selectionMode"`
	PLMN          string `json:"plmn,omitempty"`
	OperatorName  string `json:"operatorName,omitempty"`
	RAT           string `json:"rat,omitempty"`
}

type ProbeError struct {
	Layer     string `json:"layer"`
	Code      string `json:"code"`
	Retryable bool   `json:"retryable"`
}

type DeviceProbe struct {
	DeviceID         string                    `json:"deviceId"`
	State            string                    `json:"state"`
	Endpoint         string                    `json:"endpoint,omitempty"`
	Identity         ModemIdentity             `json:"identity"`
	RF               RFObservation             `json:"rf"`
	SIM              SIMObservation            `json:"sim"`
	SignalMetrics    SignalObservation         `json:"signalMetrics"`
	Registrations    []RegistrationObservation `json:"registrations"`
	CurrentNetwork   NetworkObservation        `json:"currentNetwork"`
	ActiveCallCount  *int                      `json:"activeCallCount,omitempty"`
	USBConfiguration string                    `json:"usbConfiguration,omitempty"`
	Error            *ProbeError               `json:"error,omitempty"`
	ErrorCode        string                    `json:"errorCode,omitempty"`
	ErrorDetail      string                    `json:"errorDetail,omitempty"`
}

type ProbeResponse struct {
	ProtocolVersion    int           `json:"protocolVersion"`
	AgentInstanceID    string        `json:"agentInstanceId"`
	SnapshotGeneration uint64        `json:"snapshotGeneration"`
	SnapshotRevision   string        `json:"snapshotRevision"`
	ObservedAt         time.Time     `json:"observedAt"`
	Devices            []DeviceProbe `json:"devices"`
}

type RadioEnsureOffRequest struct {
	OperationID             string `json:"operationId"`
	AgentInstanceID         string `json:"agentInstanceId"`
	SnapshotGeneration      uint64 `json:"snapshotGeneration"`
	SnapshotRevision        string `json:"snapshotRevision"`
	DeviceID                string `json:"deviceId"`
	DeviceGeneration        uint64 `json:"deviceGeneration"`
	ResourceGroupID         string `json:"resourceGroupId"`
	ResourceGroupGeneration uint64 `json:"resourceGroupGeneration"`
	FencingToken            uint64 `json:"fencingToken"`
}

type RadioEnsureOffObservation struct {
	RF              RFObservation `json:"rf"`
	ActiveCallCount *int          `json:"activeCallCount,omitempty"`
}

type CommandOutcome struct {
	OperationID string                    `json:"operationId"`
	Command     string                    `json:"command"`
	State       string                    `json:"state"`
	Code        string                    `json:"code"`
	ErrorLayer  string                    `json:"errorLayer,omitempty"`
	Retryable   bool                      `json:"retryable"`
	Reconciled  bool                      `json:"reconciled"`
	Observation RadioEnsureOffObservation `json:"observation"`
	AcceptedAt  time.Time                 `json:"acceptedAt"`
	CompletedAt *time.Time                `json:"completedAt,omitempty"`
}

type RadioEnsureOffResponse struct {
	ProtocolVersion int            `json:"protocolVersion"`
	AgentInstanceID string         `json:"agentInstanceId"`
	Outcome         CommandOutcome `json:"outcome"`
}
