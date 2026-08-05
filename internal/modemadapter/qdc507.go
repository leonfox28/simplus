package modemadapter

import (
	"sort"
	"strings"

	"github.com/leonfox28/simplus/internal/agentapi"
)

type QDC507 struct{}

func (QDC507) Profile() string { return agentapi.ProfileQDC507 }

func (QDC507) DisplayName() string { return "DJI/Baiwang QDC507" }

func (QDC507) Matches(descriptor USBDescriptor) bool {
	identity := normalizedUSBIdentity(descriptor)
	if identity == "2ca3:4006" {
		return true
	}
	if identity != "2c7c:0125" {
		return false
	}
	productText := strings.ToLower(descriptor.Manufacturer + " " + descriptor.Product)
	return strings.Contains(productText, "baiwang") || strings.Contains(productText, "qdc507")
}

func (QDC507) Endpoint(device agentapi.DeviceReport, role EndpointRole) (agentapi.Endpoint, bool) {
	switch role {
	case EndpointPrimaryAT:
		return endpoint(device, agentapi.EndpointTTY, 2)
	case EndpointQMI:
		return endpoint(device, agentapi.EndpointQMI, 4)
	default:
		return agentapi.Endpoint{}, false
	}
}

func (QDC507) Capabilities(device agentapi.DeviceReport) []agentapi.CapabilityEvidence {
	hasAT := hasEndpoint(device, agentapi.EndpointTTY, 2)
	hasQMI := hasEndpoint(device, agentapi.EndpointQMI, 4)
	hasUAC := hasEndpoint(device, agentapi.EndpointALSA, -1)
	result := make([]agentapi.CapabilityEvidence, 0, 10)
	add := func(capability, status string, evidence ...string) {
		result = append(result, agentapi.CapabilityEvidence{Capability: capability, Status: status, Evidence: evidence})
	}
	if hasAT {
		add("at-control", agentapi.EvidenceObserved, "known QDC507 interface 2 tty endpoint")
	} else {
		add("at-control", agentapi.EvidenceUnavailable, "expected AT endpoint not present")
	}
	if hasQMI {
		add("qmi-control", agentapi.EvidenceObserved, "known QDC507 cdc-wdm endpoint on interface 4")
	} else {
		add("qmi-control", agentapi.EvidenceUnavailable, "QMI endpoint not present")
	}
	if hasUAC {
		add("usb-uac", agentapi.EvidenceObserved, "snd-usb-audio endpoint")
	} else {
		add("usb-uac", agentapi.EvidenceUnavailable, "USB Audio endpoint not present")
	}
	if hasAT || hasQMI {
		add("rf-control", agentapi.EvidenceObserved, "QDC507 control path accepted in prior RF-off HIL")
		add("sim-access", agentapi.EvidenceObserved, "fixed CPIN status query available")
		add("sms-control", agentapi.EvidenceDocumented, "requires designated-SIM HIL")
		add("operator-selection", agentapi.EvidenceDocumented, "requires RF-armed HIL")
	} else {
		addUnavailableControlCapabilities(add)
	}
	if hasUAC {
		add("digital-voice-media", agentapi.EvidenceUnverified, "UAC gadget observed; in-call media not yet accepted")
	} else {
		add("digital-voice-media", agentapi.EvidenceUnavailable, "UAC gadget not present")
	}
	add("host-vowifi-auth", agentapi.EvidenceUnverified, "APDU command surface documented; SIM AKA HIL pending")
	sortCapabilities(result)
	return result
}

func normalizedUSBIdentity(descriptor USBDescriptor) string {
	return strings.ToLower(strings.TrimSpace(descriptor.VendorID) + ":" + strings.TrimSpace(descriptor.ProductID))
}

func addUnavailableControlCapabilities(add func(string, string, ...string)) {
	add("rf-control", agentapi.EvidenceUnavailable, "no supported control endpoint")
	add("sim-access", agentapi.EvidenceUnavailable, "no supported control endpoint")
	add("sms-control", agentapi.EvidenceUnavailable, "no supported control endpoint")
	add("operator-selection", agentapi.EvidenceUnavailable, "no supported control endpoint")
}

func sortCapabilities(capabilities []agentapi.CapabilityEvidence) {
	sort.Slice(capabilities, func(left, right int) bool {
		return capabilities[left].Capability < capabilities[right].Capability
	})
}
