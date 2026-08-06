package modemadapter

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/attransport"
	"github.com/leonfox28/simplus/internal/modemadapter/standardat"
)

type EndpointRole string

const (
	EndpointPrimaryAT EndpointRole = "primary-at"
	EndpointQMI       EndpointRole = "qmi"
)

type USBDescriptor struct {
	VendorID     string
	ProductID    string
	Manufacturer string
	Product      string
}

type IdentityPseudonymizer interface {
	Pseudonym(string, []byte) (string, error)
}

// SIMProfileIdentity contains only bounded, non-secret metadata needed to
// identify and describe the currently active SIM/eSIM profile. Raw ICCID and
// IMSI values never cross the adapter boundary.
type SIMProfileIdentity struct {
	Fingerprint      string
	DisplayHint      string
	HomeOperatorName string
	HomeOperatorCode string
}

// Adapter contains only model-specific discovery facts that are safe to use
// before a business driver is enabled. SMS, call, and eUICC drivers are added
// as separate capabilities after their model-specific behavior is verified.
type Adapter interface {
	Profile() string
	DisplayName() string
	Matches(USBDescriptor) bool
	Endpoint(agentapi.DeviceReport, EndpointRole) (agentapi.Endpoint, bool)
	Capabilities(agentapi.DeviceReport) []agentapi.CapabilityEvidence
}

// ATProbeAdapter explicitly opts a model into one compiled-in read-only AT
// probe plan. The Linux transport never selects commands or model behavior.
type ATProbeAdapter interface {
	Adapter
	ATProbePlan() (standardat.ProbePlan, bool)
}

// SIMAuthAdapter is the smallest model seam for SIM-backed network
// authentication. The common Agent service owns request fencing and secret
// handling; the model adapter owns the fixed protocol flow and resolves the
// control endpoint used by its verified implementation. Host VoWiFi is one
// consumer of this capability, not part of the adapter contract. No Web/API
// caller may provide a command or device path.
type SIMAuthAdapter interface {
	Adapter
	SIMAuthEndpoint(agentapi.DeviceReport) (agentapi.Endpoint, bool)
	ReadSIMAKAIdentity(context.Context, attransport.Query, IdentityPseudonymizer, string) (string, error)
	AuthenticateSIMAKA(context.Context, attransport.Query, IdentityPseudonymizer, string, agentapi.SIMAKAChallenge) (agentapi.SIMAKAExecution, error)
	ProbeSIMIMSProfile(context.Context, attransport.Query, IdentityPseudonymizer, string) (bool, error)
	ReadSIMIMSIdentity(context.Context, attransport.Query, IdentityPseudonymizer, string) (agentapi.SIMIMSIdentityMaterial, error)
}

// SIMPresenceAdapter detects whether the primary SIM slot is populated. It
// does not read identity, unlock the card, change RF, or imply that a Line
// should be created. Each model owns the fixed queries needed for this result.
type SIMPresenceAdapter interface {
	Adapter
	ReadSIMPresence(context.Context, attransport.Query) (agentapi.SIMObservation, error)
}

// SIMIdentityAdapter is separate from presence and authentication: Line
// binding needs a stable SIM identity, while a card may be present without
// being ready for identity access or network authentication. It may also
// return bounded home-operator metadata derived entirely inside the Agent.
type SIMIdentityAdapter interface {
	Adapter
	ReadSIMIdentity(context.Context, attransport.Query, IdentityPseudonymizer) (SIMProfileIdentity, error)
}

// RFControlAdapter describes model-specific runtime RF transitions. Commands
// never cross the Agent API; they are fixed adapter facts consumed by the
// bounded Agent driver and confirmed through a fresh read-only probe.
type RFControlAdapter interface {
	Adapter
	SetRFState(context.Context, attransport.Query, bool) (agentapi.RFObservation, error)
}

// EquipmentIdentityAdapter declares the fixed, read-only modem command used
// to obtain the equipment identity. The model adapter validates the raw value;
// the Agent normally converts it to an instance-scoped fingerprint and only
// discloses it through the dedicated, fenced identity-read operation.
type EquipmentIdentityAdapter interface {
	Adapter
	ReadEquipmentIdentity(context.Context, attransport.Query) (string, error)
}

type Registry struct {
	ordered   []Adapter
	byProfile map[string]Adapter
}

func NewRegistry(adapters ...Adapter) (*Registry, error) {
	if len(adapters) == 0 {
		return nil, errors.New("modem adapter registry must not be empty")
	}
	registry := &Registry{
		ordered:   make([]Adapter, 0, len(adapters)),
		byProfile: make(map[string]Adapter, len(adapters)),
	}
	for _, adapter := range adapters {
		if adapter == nil {
			return nil, errors.New("modem adapter must not be nil")
		}
		profile := strings.TrimSpace(adapter.Profile())
		if profile == "" {
			return nil, errors.New("modem adapter profile must not be empty")
		}
		if _, exists := registry.byProfile[profile]; exists {
			return nil, fmt.Errorf("duplicate modem adapter profile %q", profile)
		}
		registry.ordered = append(registry.ordered, adapter)
		registry.byProfile[profile] = adapter
	}
	return registry, nil
}

func DefaultRegistry() *Registry {
	registry, err := NewRegistry(QDC507{}, ML307A{})
	if err != nil {
		panic(fmt.Sprintf("build default modem adapter registry: %v", err))
	}
	return registry
}

func (registry *Registry) Match(descriptor USBDescriptor) (Adapter, bool) {
	if registry == nil {
		return nil, false
	}
	var matched Adapter
	for _, adapter := range registry.ordered {
		if !adapter.Matches(descriptor) {
			continue
		}
		if matched != nil {
			// Overlapping model rules are ambiguous. Fail closed instead of
			// making adapter registration order part of hardware identity.
			return nil, false
		}
		matched = adapter
	}
	return matched, matched != nil
}

func (registry *Registry) ForProfile(profile string) (Adapter, bool) {
	if registry == nil {
		return nil, false
	}
	adapter, ok := registry.byProfile[profile]
	return adapter, ok
}

func endpoint(device agentapi.DeviceReport, kind string, interfaceNumber int) (agentapi.Endpoint, bool) {
	for _, usbInterface := range device.Interfaces {
		if interfaceNumber >= 0 && usbInterface.Number != interfaceNumber {
			continue
		}
		for _, candidate := range usbInterface.Endpoints {
			if candidate.Kind == kind && candidate.Node != "" {
				return candidate, true
			}
		}
	}
	return agentapi.Endpoint{}, false
}

func hasEndpoint(device agentapi.DeviceReport, kind string, interfaceNumber int) bool {
	for _, usbInterface := range device.Interfaces {
		if interfaceNumber >= 0 && usbInterface.Number != interfaceNumber {
			continue
		}
		for _, candidate := range usbInterface.Endpoints {
			if candidate.Kind == kind {
				return true
			}
		}
	}
	return false
}
