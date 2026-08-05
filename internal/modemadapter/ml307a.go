package modemadapter

import (
	"strings"

	"github.com/leonfox28/simplus/internal/agentapi"
)

type ML307A struct{}

func (ML307A) Profile() string { return agentapi.ProfileML307A }

func (ML307A) DisplayName() string { return "China Mobile IoT ML307A" }

func (ML307A) Matches(descriptor USBDescriptor) bool {
	if normalizedUSBIdentity(descriptor) != "2ecc:3012" {
		return false
	}
	productText := strings.ToLower(descriptor.Manufacturer + " " + descriptor.Product)
	return strings.Contains(productText, "ml307") || strings.Contains(productText, "cmiot")
}

func (ML307A) Endpoint(device agentapi.DeviceReport, role EndpointRole) (agentapi.Endpoint, bool) {
	if role == EndpointPrimaryAT {
		return endpoint(device, agentapi.EndpointTTY, 2)
	}
	return agentapi.Endpoint{}, false
}

func (ML307A) Capabilities(device agentapi.DeviceReport) []agentapi.CapabilityEvidence {
	hasAT := hasEndpoint(device, agentapi.EndpointTTY, 2)
	hasUAC := hasEndpoint(device, agentapi.EndpointALSA, -1)
	result := make([]agentapi.CapabilityEvidence, 0, 11)
	add := func(capability, status string, evidence ...string) {
		result = append(result, agentapi.CapabilityEvidence{Capability: capability, Status: status, Evidence: evidence})
	}
	if hasAT {
		add("at-control", agentapi.EvidenceObserved, "official ML307A interface 2 mapping confirmed by bounded HIL")
	} else {
		add("at-control", agentapi.EvidenceUnavailable, "expected interface 2 AT endpoint not present")
	}
	add("qmi-control", agentapi.EvidenceUnsupported, "purchased ML307A exposes RNDIS and AT, not a verified QMI control path")
	if hasUAC {
		add("usb-uac", agentapi.EvidenceObserved, "snd-usb-audio endpoint")
	} else {
		add("usb-uac", agentapi.EvidenceUnsupported, "purchased ML307A exposes no supported digital media path")
	}
	if hasAT {
		add("rf-control", agentapi.EvidenceObserved, "CFUN read-back confirmed with RF off; write route remains disabled")
		add("sim-access", agentapi.EvidenceObserved, "CPIN read-back confirmed SIM ready")
		add("sim-apdu", agentapi.EvidenceObserved, "CRSM/CSIM/CCHO/CGLA forms and SIM AKA exchange accepted by bounded HIL")
		add("host-vowifi-auth", agentapi.EvidenceObserved, "VOXI SIM AKA, ePDG and protected IMS REGISTER accepted by bounded HIL")
		add("sms-control", agentapi.EvidenceUnverified, "requires designated-SIM HIL")
		add("operator-selection", agentapi.EvidenceUnverified, "requires RF-armed HIL")
	} else {
		addUnavailableControlCapabilities(add)
		add("sim-apdu", agentapi.EvidenceUnavailable, "no supported control endpoint")
		add("host-vowifi-auth", agentapi.EvidenceUnavailable, "no supported control endpoint")
	}
	add("digital-voice-media", agentapi.EvidenceUnsupported, "analog media paths are excluded by Simplus")
	sortCapabilities(result)
	return result
}
