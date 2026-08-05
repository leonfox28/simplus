package agentapi

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
)

var (
	ErrSIMAKARequestInvalid  = errors.New("SIM AKA request is invalid")
	ErrSIMAKAAgentStale      = errors.New("SIM AKA Agent instance changed")
	ErrSIMAKASnapshotStale   = errors.New("SIM AKA hardware snapshot changed")
	ErrSIMAKADeviceStale     = errors.New("SIM AKA device generation changed")
	ErrSIMAKAUnsupported     = errors.New("SIM AKA is unsupported for this device")
	ErrSIMAKARFNotOff        = errors.New("SIM AKA requires RF off")
	ErrSIMAKASIMNotReady     = errors.New("SIM AKA requires a ready SIM")
	ErrSIMAKAIdentityChanged = errors.New("SIM AKA identity changed")
	ErrSIMAKAUnavailable     = errors.New("SIM AKA backend is unavailable")
	ErrSIMAKARejected        = errors.New("SIM AKA authentication was rejected")
)

const (
	SIMIMSStageApplicationDiscover = "application-discovery"
	SIMIMSStageApplicationOpen     = "application-open"
	SIMIMSStagePrivateSelect       = "private-select"
	SIMIMSStagePrivateLayout       = "private-layout"
	SIMIMSStagePrivateRead         = "private-read"
	SIMIMSStagePrivateTLV          = "private-tlv"
	SIMIMSStagePrivateEncoding     = "private-encoding"
	SIMIMSStageDomainSelect        = "domain-select"
	SIMIMSStageDomainLayout        = "domain-layout"
	SIMIMSStageDomainRead          = "domain-read"
	SIMIMSStageDomainTLV           = "domain-tlv"
	SIMIMSStageDomainEncoding      = "domain-encoding"
	SIMIMSStagePublicSelect        = "public-select"
	SIMIMSStagePublicLayout        = "public-layout"
	SIMIMSStagePublicRead          = "public-read"
	SIMIMSStagePublicTLV           = "public-tlv"
	SIMIMSStagePublicEncoding      = "public-encoding"
	SIMIMSStageChannelClose        = "channel-close"

	SIMIMSShapeEmpty               = "empty"
	SIMIMSShapePaddingOnly         = "padding-only"
	SIMIMSShapeTag80Malformed      = "tag80-malformed"
	SIMIMSShapeLengthPrefixedASCII = "length-prefixed-ascii"
	SIMIMSShapeDirectASCII         = "direct-ascii"
	SIMIMSShapeOtherTLV            = "other-tlv"
	SIMIMSShapeOpaque              = "opaque"
)

type simIMSHILStageError struct {
	stage string
	shape string
}

func (err *simIMSHILStageError) Error() string { return "SIM IMS HIL stage failed" }

func (err *simIMSHILStageError) Is(target error) bool { return target == ErrSIMAKAUnavailable }

func NewSIMIMSHILStageError(stage string) error {
	if !validSIMIMSHILStage(stage) {
		return ErrSIMAKAUnavailable
	}
	return &simIMSHILStageError{stage: stage}
}

func NewSIMIMSHILStageShapeError(stage, shape string) error {
	if !validSIMIMSHILStage(stage) || !validSIMIMSHILShape(shape) {
		return ErrSIMAKAUnavailable
	}
	return &simIMSHILStageError{stage: stage, shape: shape}
}

func SIMIMSHILStage(err error) (string, bool) {
	var stageErr *simIMSHILStageError
	if !errors.As(err, &stageErr) || !validSIMIMSHILStage(stageErr.stage) {
		return "", false
	}
	return stageErr.stage, true
}

func SIMIMSHILShape(err error) (string, bool) {
	var stageErr *simIMSHILStageError
	if !errors.As(err, &stageErr) || !validSIMIMSHILShape(stageErr.shape) {
		return "", false
	}
	return stageErr.shape, true
}

func validSIMIMSHILStage(stage string) bool {
	switch stage {
	case SIMIMSStageApplicationDiscover, SIMIMSStageApplicationOpen, SIMIMSStagePrivateSelect, SIMIMSStagePrivateLayout,
		SIMIMSStagePrivateRead, SIMIMSStagePrivateTLV, SIMIMSStagePrivateEncoding, SIMIMSStageDomainSelect,
		SIMIMSStageDomainLayout, SIMIMSStageDomainRead, SIMIMSStageDomainTLV, SIMIMSStageDomainEncoding,
		SIMIMSStagePublicSelect, SIMIMSStagePublicLayout, SIMIMSStagePublicRead,
		SIMIMSStagePublicTLV, SIMIMSStagePublicEncoding, SIMIMSStageChannelClose:
		return true
	default:
		return false
	}
}

func validSIMIMSHILShape(shape string) bool {
	switch shape {
	case SIMIMSShapeEmpty, SIMIMSShapePaddingOnly, SIMIMSShapeTag80Malformed,
		SIMIMSShapeLengthPrefixedASCII, SIMIMSShapeDirectASCII, SIMIMSShapeOtherTLV, SIMIMSShapeOpaque:
		return true
	default:
		return false
	}
}

type SIMAKABackend interface {
	ReadSIMAKAIdentity(context.Context, Snapshot, string, string) (string, error)
	AuthenticateSIMAKA(context.Context, Snapshot, string, string, SIMAKAChallenge) (SIMAKAExecution, error)
}

type SIMIMSBackend interface {
	ProbeSIMIMSProfile(context.Context, Snapshot, string, string) (bool, error)
	ReadSIMIMSIdentity(context.Context, Snapshot, string, string) (SIMIMSIdentityMaterial, error)
}

type SIMAKAService struct {
	monitor *Monitor
	backend SIMAKABackend
}

func NewSIMAKAService(monitor *Monitor, backend SIMAKABackend) *SIMAKAService {
	return &SIMAKAService{monitor: monitor, backend: backend}
}

func (service *SIMAKAService) Identity(ctx context.Context, request SIMAKAIdentityRequest) (SIMAKAIdentityResponse, error) {
	if !validSIMAKATarget(request.SIMAKATarget) {
		return SIMAKAIdentityResponse{}, ErrSIMAKARequestInvalid
	}
	snapshot, err := service.validateTarget(ctx, request.SIMAKATarget)
	if err != nil {
		return SIMAKAIdentityResponse{}, err
	}
	imsi, err := service.backend.ReadSIMAKAIdentity(ctx, snapshot, request.DeviceID, request.IdentityFingerprint)
	if err != nil {
		return SIMAKAIdentityResponse{}, err
	}
	if !simAKAIMSI.MatchString(imsi) {
		return SIMAKAIdentityResponse{}, ErrSIMAKAUnavailable
	}
	return SIMAKAIdentityResponse{
		ProtocolVersion: ProtocolVersion, AgentInstanceID: snapshot.AgentInstanceID,
		DeviceID: request.DeviceID, IMSI: imsi,
	}, nil
}

func (service *SIMAKAService) IMSProfile(ctx context.Context, request SIMIMSProfileRequest) (SIMIMSProfileResponse, error) {
	if !validSIMAKATarget(request.SIMAKATarget) {
		return SIMIMSProfileResponse{}, ErrSIMAKARequestInvalid
	}
	snapshot, err := service.validateTarget(ctx, request.SIMAKATarget)
	if err != nil {
		return SIMIMSProfileResponse{}, err
	}
	backend, ok := service.backend.(SIMIMSBackend)
	if !ok {
		return SIMIMSProfileResponse{}, ErrSIMAKAUnsupported
	}
	available, err := backend.ProbeSIMIMSProfile(ctx, snapshot, request.DeviceID, request.IdentityFingerprint)
	if err != nil {
		return SIMIMSProfileResponse{}, err
	}
	source := SIMIMSIdentityDerived
	if available {
		source = SIMIMSIdentityISIM
	}
	return SIMIMSProfileResponse{
		ProtocolVersion: ProtocolVersion, AgentInstanceID: snapshot.AgentInstanceID,
		DeviceID: request.DeviceID, ISIMAvailable: available, IdentitySource: source,
	}, nil
}

func (service *SIMAKAService) IMSIdentity(ctx context.Context, request SIMIMSIdentityRequest) (SIMIMSIdentityResponse, error) {
	if !validSIMAKATarget(request.SIMAKATarget) {
		return SIMIMSIdentityResponse{}, ErrSIMAKARequestInvalid
	}
	snapshot, err := service.validateTarget(ctx, request.SIMAKATarget)
	if err != nil {
		return SIMIMSIdentityResponse{}, err
	}
	backend, ok := service.backend.(SIMIMSBackend)
	if !ok {
		return SIMIMSIdentityResponse{}, ErrSIMAKAUnsupported
	}
	material, err := backend.ReadSIMIMSIdentity(ctx, snapshot, request.DeviceID, request.IdentityFingerprint)
	if err != nil {
		return SIMIMSIdentityResponse{}, err
	}
	if !validSIMIMSIdentityMaterial(material) {
		return SIMIMSIdentityResponse{}, ErrSIMAKAUnavailable
	}
	return SIMIMSIdentityResponse{
		ProtocolVersion: ProtocolVersion, AgentInstanceID: snapshot.AgentInstanceID,
		DeviceID: request.DeviceID, IdentitySource: material.Source,
		PrivateIdentity: material.PrivateIdentity, HomeDomain: material.HomeDomain,
		PublicIdentities:      append([]string(nil), material.PublicIdentities...),
		ApplicationDiscovery:  material.ApplicationDiscovery,
		ApplicationCandidates: material.ApplicationCandidates,
		SMSOverIP:             cloneSIMIMSSMSConfiguration(material.SMSOverIP),
	}, nil
}

func cloneSIMIMSSMSConfiguration(value *SIMIMSSMSConfiguration) *SIMIMSSMSConfiguration {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func (service *SIMAKAService) Authenticate(ctx context.Context, request SIMAKAAuthenticationRequest) (SIMAKAAuthenticationResponse, error) {
	if !validSIMAKATarget(request.SIMAKATarget) || !simAKAExchangeIDPattern.MatchString(request.ExchangeID) {
		return SIMAKAAuthenticationResponse{}, ErrSIMAKARequestInvalid
	}
	rand, randOK := decodeSIMAKAHex(request.RAND, simAKARANDLength)
	autn, autnOK := decodeSIMAKAHex(request.AUTN, simAKAAUTNLength)
	if !randOK || !autnOK {
		return SIMAKAAuthenticationResponse{}, ErrSIMAKARequestInvalid
	}
	defer zeroSIMAKABytes(rand)
	defer zeroSIMAKABytes(autn)
	challenge := SIMAKAChallenge{}
	copy(challenge.RAND[:], rand)
	copy(challenge.AUTN[:], autn)
	defer zeroSIMAKABytes(challenge.RAND[:])
	defer zeroSIMAKABytes(challenge.AUTN[:])

	snapshot, err := service.validateTarget(ctx, request.SIMAKATarget)
	if err != nil {
		return SIMAKAAuthenticationResponse{}, err
	}
	execution, err := service.backend.AuthenticateSIMAKA(ctx, snapshot, request.DeviceID, request.IdentityFingerprint, challenge)
	if err != nil {
		return SIMAKAAuthenticationResponse{}, err
	}
	defer zeroSIMAKAExecution(&execution)
	result := SIMAKAAuthenticationResult{State: execution.State}
	switch execution.State {
	case SIMAKAStateSuccess:
		if len(execution.RES) < 4 || len(execution.RES) > simAKARESMax {
			return SIMAKAAuthenticationResponse{}, ErrSIMAKAUnavailable
		}
		result.RES = hex.EncodeToString(execution.RES)
		result.CK = hex.EncodeToString(execution.CK[:])
		result.IK = hex.EncodeToString(execution.IK[:])
	case SIMAKAStateSynchronizationFailure:
		result.AUTS = hex.EncodeToString(execution.AUTS[:])
	default:
		return SIMAKAAuthenticationResponse{}, ErrSIMAKAUnavailable
	}
	if !validSIMAKAAuthenticationResult(result) {
		return SIMAKAAuthenticationResponse{}, ErrSIMAKAUnavailable
	}
	return SIMAKAAuthenticationResponse{
		ProtocolVersion: ProtocolVersion, AgentInstanceID: snapshot.AgentInstanceID,
		DeviceID: request.DeviceID, ExchangeID: request.ExchangeID, Result: result,
	}, nil
}

func (service *SIMAKAService) validateTarget(ctx context.Context, target SIMAKATarget) (Snapshot, error) {
	if service == nil || service.monitor == nil || service.backend == nil {
		return Snapshot{}, ErrSIMAKAUnavailable
	}
	snapshot := service.monitor.Snapshot()
	if target.AgentInstanceID != snapshot.AgentInstanceID {
		return Snapshot{}, ErrSIMAKAAgentStale
	}
	if target.SnapshotGeneration != snapshot.Generation || target.SnapshotRevision != snapshot.Revision {
		return Snapshot{}, ErrSIMAKASnapshotStale
	}
	deviceFound := false
	for _, device := range snapshot.Devices {
		if device.ID != target.DeviceID {
			continue
		}
		deviceFound = true
		if device.Generation != target.DeviceGeneration {
			return Snapshot{}, ErrSIMAKADeviceStale
		}
		if device.Profile != ProfileML307A || !observedAgentCapability(device, "sim-apdu") {
			return Snapshot{}, ErrSIMAKAUnsupported
		}
		break
	}
	if !deviceFound {
		return Snapshot{}, ErrSIMAKADeviceStale
	}
	probe, err := service.monitor.Probe(ctx, []string{target.DeviceID})
	if err != nil || len(probe.Devices) != 1 {
		return Snapshot{}, fmt.Errorf("%w: target probe failed", ErrSIMAKAUnavailable)
	}
	observed := probe.Devices[0]
	if observed.DeviceID != target.DeviceID || observed.State != ProbeStateComplete {
		return Snapshot{}, ErrSIMAKAUnavailable
	}
	if observed.RF.State != RFStateOff {
		return Snapshot{}, ErrSIMAKARFNotOff
	}
	if observed.SIM.State != SIMStatePresent || observed.SIM.PrimaryLockState != PrimaryLockReady {
		return Snapshot{}, ErrSIMAKASIMNotReady
	}
	if observed.SIM.IdentityFingerprint != target.IdentityFingerprint {
		return Snapshot{}, ErrSIMAKAIdentityChanged
	}
	return snapshot, nil
}

func observedAgentCapability(device DeviceReport, name string) bool {
	for _, capability := range device.Capabilities {
		if capability.Capability == name && capability.Status == EvidenceObserved {
			return true
		}
	}
	return false
}

func zeroSIMAKABytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func zeroSIMAKAExecution(execution *SIMAKAExecution) {
	if execution == nil {
		return
	}
	zeroSIMAKABytes(execution.RES)
	zeroSIMAKABytes(execution.CK[:])
	zeroSIMAKABytes(execution.IK[:])
	zeroSIMAKABytes(execution.AUTS[:])
}
