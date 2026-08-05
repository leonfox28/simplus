package inventory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

var ErrSetupTopologyUnsafe = errors.New("hardware topology is not safe for setup completion")

type setupDigestTopology struct {
	Generation           uint64
	Devices              any
	ModemFunctions       any
	SIMSlots             any
	SIMMedia             any
	SubscriptionProfiles any
	ResourceGroups       any
	Lines                any
}

func Revision(topology Topology) (string, error) {
	canonical, err := json.Marshal(setupDigestTopology{
		Generation:           topology.Generation,
		Devices:              topology.Devices,
		ModemFunctions:       topology.ModemFunctions,
		SIMSlots:             topology.SIMSlots,
		SIMMedia:             topology.SIMMedia,
		SubscriptionProfiles: topology.SubscriptionProfiles,
		ResourceGroups:       topology.ResourceGroups,
		Lines:                topology.Lines,
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

func SetupDigest(topology Topology) (string, error) {
	for _, profile := range topology.SubscriptionProfiles {
		if !profile.AccessModeConfigured || !profile.AccessMode.Valid() {
			return "", ErrSetupTopologyUnsafe
		}
	}
	for _, line := range topology.Lines {
		if !line.AccessModeConfigured || !line.AccessMode.Valid() || line.RFSafety != RFSafetyOff {
			return "", ErrSetupTopologyUnsafe
		}
	}
	return Revision(topology)
}
