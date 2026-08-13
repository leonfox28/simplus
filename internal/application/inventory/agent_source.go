package inventory

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/domain/hardware"
)

type AgentClient interface {
	Snapshot(context.Context, bool) (agentapi.Snapshot, error)
	Probe(context.Context, agentapi.ProbeRequest) (agentapi.ProbeResponse, error)
}

type AgentSource struct {
	client AgentClient
	now    func() time.Time

	mu              sync.Mutex
	probeGeneration uint64
	probeRevision   string
	probeAt         time.Time
	probe           agentapi.ProbeResponse
}

func NewAgentSource(client AgentClient) *AgentSource {
	return &AgentSource{client: client, now: time.Now}
}

func (source *AgentSource) Snapshot(ctx context.Context) (hardware.Snapshot, error) {
	if source == nil || source.client == nil {
		return hardware.Snapshot{}, errors.New("hardware agent client is unavailable")
	}
	report, err := source.client.Snapshot(ctx, true)
	if err != nil {
		return hardware.Snapshot{}, fmt.Errorf("read hardware agent snapshot: %w", err)
	}
	if report.ProtocolVersion != agentapi.ProtocolVersion || !agentapi.IsValidAgentInstanceID(report.AgentInstanceID) || report.Generation == 0 || len(report.Revision) != 64 {
		return hardware.Snapshot{}, errors.New("hardware agent returned an invalid snapshot envelope")
	}
	probe := source.cachedProbe(ctx, report)
	probeByDevice := make(map[string]agentapi.DeviceProbe, len(probe.Devices))
	for _, device := range probe.Devices {
		probeByDevice[device.DeviceID] = device
	}

	snapshot := hardware.Snapshot{Generation: report.Generation, ObservedAt: report.ObservedAt.UTC()}
	for _, device := range report.Devices {
		if device.ID == "" || device.Generation == 0 || device.Generation > report.Generation {
			return hardware.Snapshot{}, errors.New("hardware agent returned an invalid device generation")
		}
		deviceID := "agent-" + device.ID
		functionID := deviceID + "-modem"
		slotID := deviceID + "-slot-0"
		groupID := deviceID + "-resources"
		controlObserved := observedCapability(device, "at-control") || observedCapability(device, "qmi-control")
		capabilities := mapAgentCapabilities(device)
		observation, observed := probeByDevice[device.ID]
		equipmentIdentity := ""
		modemModel := ""
		modemSerialNumber := ""
		if observed {
			equipmentIdentity = observation.Identity.EquipmentIdentityFingerprint
			modemModel = observation.Identity.Model
			modemSerialNumber = observation.Identity.SerialNumber
		}
		backend := hardware.BackendDirectAT
		if observedCapability(device, "qmi-control") {
			backend = hardware.BackendDirectQMI
		}
		snapshot.Devices = append(snapshot.Devices, hardware.PhysicalDevice{
			ID: deviceID, DisplayName: device.DisplayName, ModemModel: modemModel, Transport: hardware.TransportUSB,
			State: hardware.DeviceAvailable, EquipmentIdentityFingerprint: equipmentIdentity,
			USBAddress: device.PhysicalPath, USBVendorID: device.USB.VendorID, USBProductID: device.USB.ProductID,
			ModemSerialNumber: modemSerialNumber, USBSerialNumber: device.USB.SerialNumber,
			USBSerialFingerprint: device.USB.SerialFingerprint, Generation: device.Generation,
		})
		if !controlObserved {
			// Keep descriptor-only candidates visible without inventing operable functions, slots or resources.
			continue
		}
		snapshot.ModemFunctions = append(snapshot.ModemFunctions, hardware.ModemFunction{
			ID: functionID, PhysicalDeviceID: deviceID, DisplayName: device.DisplayName + " modem",
			Backend: backend, Generation: device.Generation, Capabilities: capabilities,
		})
		if !capabilities.SIMAccess {
			// A discovered control function remains visible as a modem, but the
			// SIM/Line graph exists only when the adapter declares SIM access.
			continue
		}
		presence := hardware.SlotUnknown
		if observed {
			switch observation.SIM.State {
			case agentapi.SIMStatePresent, agentapi.SIMStateLocked:
				presence = hardware.SlotPresent
			case agentapi.SIMStateAbsent:
				presence = hardware.SlotAbsent
			}
		}
		identityReady := observed && observation.SIM.State == agentapi.SIMStatePresent && observation.SIM.PrimaryLockState == agentapi.PrimaryLockReady &&
			len(observation.SIM.IdentityFingerprint) == 64 && observation.SIM.DisplayIdentityHint != ""
		mediaID, profileID, lineID := "", "", ""
		if identityReady {
			token := observation.SIM.IdentityFingerprint[:32]
			mediaID = "agent-media-" + token
			profileID = "agent-profile-" + token
			lineID = "agent-line-" + token
		}
		snapshot.SIMSlots = append(snapshot.SIMSlots, hardware.SIMSlot{
			ID: slotID, PhysicalDeviceID: deviceID, Index: 0, Presence: presence, ActiveMediaID: mediaID, Generation: device.Generation,
		})
		resources := []string{hardware.ResourceSIMAccess}
		if capabilities.RFControl {
			resources = append(resources, hardware.ResourceRadioControl)
		}
		if capabilities.SMS {
			resources = append(resources, hardware.ResourceSMSStorage)
		}
		if capabilities.SIMAPDU {
			resources = append(resources, hardware.ResourceSIMAPDU)
		}
		if capabilities.HostVoWiFiAuth {
			resources = append(resources, hardware.ResourceHostVoWiFiAuth)
		}
		if capabilities.NetworkScan || capabilities.ManualNetworkSelection {
			resources = append(resources, hardware.ResourceNetworkSelection)
		}
		if capabilities.DigitalVoiceMedia {
			resources = append(resources, hardware.ResourceVoiceMedia)
		}
		if capabilities.PrimarySIMLockState || capabilities.PIN1Verify || capabilities.PUK1Unblock {
			resources = append(resources, hardware.ResourceSIMLock)
		}
		sort.Strings(resources)
		maxCalls := 0
		if capabilities.CellularVoice && capabilities.DigitalVoiceMedia {
			maxCalls = 1
		}
		snapshot.ResourceGroups = append(snapshot.ResourceGroups, hardware.ResourceGroup{
			ID: groupID, PhysicalDeviceID: deviceID, DisplayName: device.DisplayName + " shared resources",
			Resources: resources, ModemFunctionIDs: []string{functionID}, SIMSlotIDs: []string{slotID},
			MaxActiveCalls: maxCalls, MaxConcurrentOps: 1, Generation: device.Generation,
		})
		if identityReady {
			snapshot.SIMMedia = append(snapshot.SIMMedia, hardware.SIMMedia{
				ID: mediaID, SIMSlotID: slotID, Kind: hardware.MediaUICC, IdentityState: hardware.MediaIdentityKnown,
				IdentityFingerprint: observation.SIM.IdentityFingerprint, DisplayIdentityHint: observation.SIM.DisplayIdentityHint,
				Generation: device.Generation,
			})
			snapshot.SubscriptionProfiles = append(snapshot.SubscriptionProfiles, hardware.SubscriptionProfile{
				ID: profileID, SIMMediaID: mediaID, DisplayName: device.DisplayName + " active SIM",
				State: hardware.ProfileActive, IdentityFingerprint: observation.SIM.IdentityFingerprint,
				DisplayIdentityHint: observation.SIM.DisplayIdentityHint,
				HomeOperatorName:    observation.SIM.HomeOperatorName, HomeOperatorCode: observation.SIM.HomeOperatorCode,
				CellularPhoneNumber: observation.SIM.SubscriberNumber,
				Generation:          device.Generation,
			})
			snapshot.Lines = append(snapshot.Lines, hardware.Line{
				ID: lineID, PhysicalDeviceID: deviceID, ModemFunctionID: functionID, SubscriptionProfileID: profileID,
				ResourceGroupID: groupID, DisplayName: device.DisplayName + " line", Generation: device.Generation,
				Capabilities: capabilities,
			})
		}
	}
	return hardware.NormalizeAndValidate(snapshot)
}

func (source *AgentSource) cachedProbe(ctx context.Context, snapshot agentapi.Snapshot) agentapi.ProbeResponse {
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.probeGeneration == snapshot.Generation && source.probeRevision == snapshot.Revision && source.now().Sub(source.probeAt) < 5*time.Second {
		return source.probe
	}
	probe, err := source.client.Probe(ctx, agentapi.ProbeRequest{})
	if err != nil || probe.ProtocolVersion != agentapi.ProtocolVersion || probe.AgentInstanceID != snapshot.AgentInstanceID || probe.SnapshotGeneration != snapshot.Generation || probe.SnapshotRevision != snapshot.Revision {
		return agentapi.ProbeResponse{}
	}
	source.probeGeneration = snapshot.Generation
	source.probeRevision = snapshot.Revision
	source.probeAt = source.now()
	source.probe = probe
	return probe
}

func mapAgentCapabilities(device agentapi.DeviceReport) hardware.Capabilities {
	control := observedCapability(device, "at-control") || observedCapability(device, "qmi-control")
	return hardware.Capabilities{
		SIMAccess:         control && observedCapability(device, "sim-access"),
		SMS:               control && observedCapability(device, "sms-control"),
		CellularVoice:     false,
		DigitalVoiceMedia: false,
		// Physical UAC evidence remains in the Agent report; operational use waits for in-call media HIL.
		USBUAC:                 false,
		SIMAPDU:                control && observedCapability(device, "sim-apdu"),
		HostVoWiFiAuth:         control && observedCapability(device, "host-vowifi-auth"),
		RFControl:              control && observedCapability(device, "rf-control"),
		NetworkScan:            control && observedCapability(device, "operator-selection"),
		ManualNetworkSelection: control && observedCapability(device, "operator-selection"),
		PrimarySIMLockState:    control && observedCapability(device, "sim-presence"),
		PIN1Verify:             false,
		PUK1Unblock:            false,
		EUICCProfiles:          false,
	}
}

func observedCapability(device agentapi.DeviceReport, name string) bool {
	for _, capability := range device.Capabilities {
		if capability.Capability == name {
			return capability.Status == agentapi.EvidenceObserved
		}
	}
	return false
}
