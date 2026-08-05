package agentapi

import (
	"encoding/hex"
	"regexp"
	"strings"
)

const (
	FeatureSIMAKAHIL = "sim-aka-hil-v1"
	FeatureSIMIMSHIL = "sim-ims-hil-v1"

	SIMAKAStateSuccess                = "success"
	SIMAKAStateSynchronizationFailure = "synchronization-failure"
	SIMIMSIdentityISIM                = "isim"
	SIMIMSIdentityDerived             = "derived"
	SIMIMSDiscoveryEFDIR              = "efdir"
	SIMIMSDiscoveryGenericAID         = "generic-aid"
)

const (
	simAKARANDLength = 16
	simAKAAUTNLength = 16
	simAKACKLength   = 16
	simAKAIKLength   = 16
	simAKAAUTSLength = 14
	simAKARESMax     = 16
)

var (
	simAKAExchangeIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)
	simAKAIMSI              = regexp.MustCompile(`^[0-9]{14,16}$`)
	simAKAFingerprint       = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// SIMAKATarget fences a transient AKA exchange to the exact Agent process,
// topology generation, physical device generation and pseudonymized SIM.
type SIMAKATarget struct {
	AgentInstanceID     string `json:"agentInstanceId"`
	SnapshotGeneration  uint64 `json:"snapshotGeneration"`
	SnapshotRevision    string `json:"snapshotRevision"`
	DeviceID            string `json:"deviceId"`
	DeviceGeneration    uint64 `json:"deviceGeneration"`
	IdentityFingerprint string `json:"identityFingerprint"`
}

type SIMAKAIdentityRequest struct {
	SIMAKATarget
}

type SIMAKAIdentityResponse struct {
	ProtocolVersion int    `json:"protocolVersion"`
	AgentInstanceID string `json:"agentInstanceId"`
	DeviceID        string `json:"deviceId"`
	// IMSI is available only on the separate root-only HIL socket. It must not
	// be logged, persisted or forwarded to the management HTTP API.
	IMSI string `json:"imsi"`
}

type SIMIMSProfileRequest struct {
	SIMAKATarget
}

type SIMIMSProfileResponse struct {
	ProtocolVersion int    `json:"protocolVersion"`
	AgentInstanceID string `json:"agentInstanceId"`
	DeviceID        string `json:"deviceId"`
	// ISIMAvailable means that a complete, usable provisioned ISIM identity
	// profile is available. An ISIM application with an unprovisioned EFIMPI
	// is reported as derived so callers do not mix partial ISIM material with
	// an IMSI-derived identity.
	ISIMAvailable  bool   `json:"isimAvailable"`
	IdentitySource string `json:"identitySource"`
}

type SIMIMSIdentityRequest struct {
	SIMAKATarget
}

type SIMIMSIdentityResponse struct {
	ProtocolVersion       int      `json:"protocolVersion"`
	AgentInstanceID       string   `json:"agentInstanceId"`
	DeviceID              string   `json:"deviceId"`
	IdentitySource        string   `json:"identitySource"`
	PrivateIdentity       string   `json:"privateIdentity"`
	HomeDomain            string   `json:"homeDomain"`
	PublicIdentities      []string `json:"publicIdentities"`
	ApplicationDiscovery  string   `json:"applicationDiscovery"`
	ApplicationCandidates int      `json:"applicationCandidates"`
}

// SIMIMSIdentityMaterial is transient root-only HIL material.  It must not
// leave the SIM AKA/IMS Unix socket boundary except for a root-owned /run
// artifact consumed by the one-shot IMS process.
type SIMIMSIdentityMaterial struct {
	Source                string
	PrivateIdentity       string
	HomeDomain            string
	PublicIdentities      []string
	ApplicationDiscovery  string
	ApplicationCandidates int
}

type SIMAKAAuthenticationRequest struct {
	SIMAKATarget
	ExchangeID string `json:"exchangeId"`
	RAND       string `json:"rand"`
	AUTN       string `json:"autn"`
}

type SIMAKAAuthenticationResult struct {
	State string `json:"state"`
	RES   string `json:"res,omitempty"`
	CK    string `json:"ck,omitempty"`
	IK    string `json:"ik,omitempty"`
	AUTS  string `json:"auts,omitempty"`
}

type SIMAKAAuthenticationResponse struct {
	ProtocolVersion int                        `json:"protocolVersion"`
	AgentInstanceID string                     `json:"agentInstanceId"`
	DeviceID        string                     `json:"deviceId"`
	ExchangeID      string                     `json:"exchangeId"`
	Result          SIMAKAAuthenticationResult `json:"result"`
}

type SIMAKAChallenge struct {
	RAND [simAKARANDLength]byte
	AUTN [simAKAAUTNLength]byte
}

type SIMAKAExecution struct {
	State string
	RES   []byte
	CK    [simAKACKLength]byte
	IK    [simAKAIKLength]byte
	AUTS  [simAKAAUTSLength]byte
}

func validSIMAKATarget(target SIMAKATarget) bool {
	return IsValidAgentInstanceID(target.AgentInstanceID) &&
		target.SnapshotGeneration != 0 && target.DeviceGeneration != 0 &&
		len(target.SnapshotRevision) == 64 && simAKAFingerprint.MatchString(target.SnapshotRevision) &&
		len(target.DeviceID) >= 1 && len(target.DeviceID) <= 128 &&
		simAKAFingerprint.MatchString(target.IdentityFingerprint)
}

func validSIMIMSIdentityMaterial(material SIMIMSIdentityMaterial) bool {
	if material.Source != SIMIMSIdentityISIM && material.Source != SIMIMSIdentityDerived {
		return false
	}
	if material.ApplicationDiscovery != SIMIMSDiscoveryEFDIR &&
		material.ApplicationDiscovery != SIMIMSDiscoveryGenericAID ||
		material.ApplicationCandidates < 0 || material.ApplicationCandidates > 8 ||
		material.ApplicationDiscovery == SIMIMSDiscoveryGenericAID && material.ApplicationCandidates != 1 {
		return false
	}
	const domain = "ims.mnc015.mcc234.3gppnetwork.org"
	if material.HomeDomain != domain || len(material.PrivateIdentity) < len(domain)+15 ||
		len(material.PrivateIdentity) > 255 || !strings.HasSuffix(material.PrivateIdentity, "@"+domain) ||
		len(material.PublicIdentities) == 0 || len(material.PublicIdentities) > 8 {
		return false
	}
	for _, value := range append([]string{material.PrivateIdentity, material.HomeDomain}, material.PublicIdentities...) {
		if value == "" || len(value) > 255 || strings.IndexFunc(value, func(r rune) bool { return r < 0x21 || r > 0x7e }) >= 0 {
			return false
		}
	}
	seen := make(map[string]struct{}, len(material.PublicIdentities))
	for _, identity := range material.PublicIdentities {
		if !strings.HasPrefix(identity, "sip:") && !strings.HasPrefix(identity, "tel:") {
			return false
		}
		if _, duplicate := seen[identity]; duplicate {
			return false
		}
		seen[identity] = struct{}{}
	}
	return true
}

func decodeSIMAKAHex(value string, expected int) ([]byte, bool) {
	if len(value) != expected*2 {
		return nil, false
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || hex.EncodeToString(decoded) != value {
		return nil, false
	}
	return decoded, true
}

func validSIMAKAAuthenticationResult(result SIMAKAAuthenticationResult) bool {
	switch result.State {
	case SIMAKAStateSuccess:
		res, resOK := decodeSIMAKAHex(result.RES, len(result.RES)/2)
		_, ckOK := decodeSIMAKAHex(result.CK, simAKACKLength)
		_, ikOK := decodeSIMAKAHex(result.IK, simAKAIKLength)
		return resOK && len(res) >= 4 && len(res) <= simAKARESMax && ckOK && ikOK && result.AUTS == ""
	case SIMAKAStateSynchronizationFailure:
		_, autsOK := decodeSIMAKAHex(result.AUTS, simAKAAUTSLength)
		return autsOK && result.RES == "" && result.CK == "" && result.IK == ""
	default:
		return false
	}
}
