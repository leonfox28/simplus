package modemadapter

import (
	"errors"
	"fmt"
	"strings"

	"github.com/leonfox28/simplus/internal/agentapi"
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
	for _, adapter := range registry.ordered {
		if adapter.Matches(descriptor) {
			return adapter, true
		}
	}
	return nil, false
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
