package hardware

import "time"

const (
	TransportUSB       = "usb"
	TransportUART      = "uart"
	TransportSimulated = "simulated"

	DeviceAvailable   = "available"
	DeviceUnavailable = "unavailable"

	BackendSimulated    = "simulated"
	BackendDirectAT     = "direct-at"
	BackendDirectQMI    = "direct-qmi"
	BackendDirectMBIM   = "direct-mbim"
	BackendModemManager = "modemmanager"
	BackendPCSC         = "pcsc"

	SlotPresent = "present"
	SlotAbsent  = "absent"
	SlotUnknown = "unknown"

	MediaUICC            = "uicc"
	MediaRemovableEUICC  = "removable-euicc"
	MediaIdentityKnown   = "known"
	MediaIdentityUnknown = "unknown"

	ProfileActive   = "active"
	ProfileInactive = "inactive"
	ProfileLocked   = "locked"

	ResourceRadioControl     = "radio-control"
	ResourceSIMAccess        = "sim-access"
	ResourceVoiceMedia       = "voice-media"
	ResourceSMSStorage       = "sms-storage"
	ResourceSIMAPDU          = "sim-apdu"
	ResourceHostVoWiFiAuth   = "host-vowifi-auth"
	ResourceNetworkSelection = "network-selection"
	ResourceSIMLock          = "sim-lock"
	ResourceEUICCProfiles    = "euicc-profiles"
)

type Capabilities struct {
	SIMAccess              bool
	SMS                    bool
	CellularVoice          bool
	DigitalVoiceMedia      bool
	USBUAC                 bool
	SIMAPDU                bool
	HostVoWiFiAuth         bool
	RFControl              bool
	NetworkScan            bool
	ManualNetworkSelection bool
	PrimarySIMLockState    bool
	PIN1Verify             bool
	PUK1Unblock            bool
	EUICCProfiles          bool
}

type PhysicalDevice struct {
	ID          string
	DisplayName string
	Transport   string
	State       string
	Generation  uint64
}

type ModemFunction struct {
	ID               string
	PhysicalDeviceID string
	DisplayName      string
	Backend          string
	Generation       uint64
	Capabilities     Capabilities
}

type SIMSlot struct {
	ID               string
	PhysicalDeviceID string
	Index            int
	Presence         string
	ActiveMediaID    string
	Generation       uint64
}

type SIMMedia struct {
	ID            string
	SIMSlotID     string
	Kind          string
	IdentityState string
	// IdentityFingerprint is an internal, per-install keyed pseudonym produced at the hardware boundary. Real adapters must never place a raw identity or an unkeyed ICCID/EID digest here.
	IdentityFingerprint string
	DisplayIdentityHint string
	Generation          uint64
}

type SubscriptionProfile struct {
	ID          string
	SIMMediaID  string
	DisplayName string
	State       string
	// IdentityFingerprint is an internal, per-install keyed pseudonym produced at the hardware boundary. Real adapters must never place a raw identity or an unkeyed ICCID digest here.
	IdentityFingerprint string
	DisplayIdentityHint string
	Generation          uint64
}

type ResourceGroup struct {
	ID               string
	PhysicalDeviceID string
	DisplayName      string
	Resources        []string
	ModemFunctionIDs []string
	SIMSlotIDs       []string
	MaxActiveCalls   int
	MaxConcurrentOps int
	Generation       uint64
}

type Line struct {
	ID                    string
	PhysicalDeviceID      string
	ModemFunctionID       string
	SubscriptionProfileID string
	ResourceGroupID       string
	DisplayName           string
	Generation            uint64
	Capabilities          Capabilities
}

type Snapshot struct {
	Generation           uint64
	ObservedAt           time.Time
	Devices              []PhysicalDevice
	ModemFunctions       []ModemFunction
	SIMSlots             []SIMSlot
	SIMMedia             []SIMMedia
	SubscriptionProfiles []SubscriptionProfile
	ResourceGroups       []ResourceGroup
	Lines                []Line
}
