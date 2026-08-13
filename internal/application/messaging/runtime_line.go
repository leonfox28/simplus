package messaging

import (
	"errors"
	"regexp"
	"strings"

	"github.com/leonfox28/simplus/internal/application/inventory"
)

var runtimeFingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type runtimeLine struct {
	line                    inventory.Line
	transportDeviceID       string
	equipmentFingerprint    string
	subscriptionFingerprint string
}

func resolveRuntimeLine(topology inventory.Topology, line inventory.Line) (runtimeLine, error) {
	var equipment string
	for _, device := range topology.Devices {
		if device.ID == line.PhysicalDeviceID {
			equipment = device.EquipmentIdentityFingerprint
			break
		}
	}
	profiles := 0
	var subscription string
	for _, profile := range topology.SubscriptionProfiles {
		if profile.ID == line.SubscriptionProfileID {
			profiles++
			if profile.State == "active" {
				subscription = profile.IdentityFingerprint
			}
		}
	}
	if !runtimeFingerprintPattern.MatchString(equipment) || profiles != 1 || !runtimeFingerprintPattern.MatchString(subscription) || line.Generation == 0 {
		return runtimeLine{}, errors.New("SMS runtime Line identity is unavailable")
	}
	transportDeviceID := line.PhysicalDeviceID
	if strings.HasPrefix(transportDeviceID, "agent-") {
		transportDeviceID = strings.TrimPrefix(transportDeviceID, "agent-")
	}
	if transportDeviceID == "" || len(transportDeviceID) > 128 {
		return runtimeLine{}, errors.New("SMS runtime device identity is unavailable")
	}
	return runtimeLine{
		line: line, transportDeviceID: transportDeviceID,
		equipmentFingerprint: equipment, subscriptionFingerprint: subscription,
	}, nil
}

func (target runtimeLine) sendCommand(command SendSMSCommand) SendSMSCommand {
	command.PhysicalDeviceID = target.transportDeviceID
	command.DeviceGeneration = target.line.Generation
	command.ExpectedEquipmentFingerprint = target.equipmentFingerprint
	command.ExpectedSubscriptionFingerprint = target.subscriptionFingerprint
	return command
}

func (target runtimeLine) inboxTarget() InboxTarget {
	deviceID := target.transportDeviceID
	if deviceID == "" {
		deviceID = target.line.PhysicalDeviceID
	}
	return InboxTarget{LineID: target.line.ID, PhysicalDeviceID: deviceID, DeviceGeneration: target.line.Generation,
		ExpectedEquipmentFingerprint: target.equipmentFingerprint, ExpectedSubscriptionFingerprint: target.subscriptionFingerprint}
}
