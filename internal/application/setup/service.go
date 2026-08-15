package setup

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/leonfox28/simplus/internal/domain/managementtls"
)

const (
	InstallationUninitialized = "uninitialized"
	InstallationReady         = "ready"
	InstallationMaintenance   = "maintenance"

	PhaseBootstrapRequired = "bootstrap-required"
	PhaseComplete          = "complete"
	PhaseMaintenance       = "maintenance"

	FlowCreateNew = "create-new"

	bootstrapTokenBytes = 32
	sessionTokenBytes   = 32
)

var (
	ErrSetupUnavailable            = errors.New("setup is not available in the current installation state")
	ErrBootstrapInvalidOrExpired   = errors.New("bootstrap code is invalid, expired, or already consumed")
	ErrSetupSessionUnauthorized    = errors.New("setup session is missing, invalid, or expired")
	ErrAdministratorRequestInvalid = errors.New("initial administrator request is invalid")
	ErrStorageRequestInvalid       = errors.New("setup storage request is invalid")
	ErrSetupPrerequisiteMissing    = errors.New("a required earlier setup step is incomplete")
	ErrHTTPSRequestInvalid         = errors.New("setup HTTPS request is invalid")
	ErrHTTPSConfirmationInvalid    = errors.New("setup HTTPS confirmation is invalid")
	ErrHardwareReviewInvalid       = errors.New("setup hardware review is invalid or incomplete")
	ErrSetupPreflightFailed        = errors.New("final setup preflight failed")
	ErrDependenciesInvalid         = errors.New("setup dependencies are invalid")
)

type StateStore interface {
	InstallationState(context.Context) (string, error)
}

type AuthorizationStore interface {
	ReplaceSetupBootstrap(context.Context, [32]byte, int64, int64) error
	ConsumeSetupBootstrap(context.Context, [32]byte, [32]byte, int64, int64) (bool, error)
	ReadSetupSession(context.Context, [32]byte, int64) (int64, string, bool, error)
	RevokeSetupAuthorization(context.Context) error
}

type AdministratorStore interface {
	ConfigureInitialAdministrator(context.Context, string, string, string, time.Time) error
	ReadInitialAdministrator(context.Context) (string, string, bool, error)
}

type PasswordHasher interface {
	Hash(string) (string, error)
}

type StorageStore interface {
	SetupDataRoot() string
	ConfigureSetupStorage(context.Context, string, string, uint64, uint64, uint64, uint64, time.Time) error
	ReadSetupStorage(context.Context) (string, string, uint64, uint64, uint64, uint64, bool, error)
}

type DirectoryIdentity struct {
	Path   string
	Device uint64
	Inode  uint64
}

type DirectoryPreparer func(string) (DirectoryIdentity, error)

type ManagementTLSStore interface {
	ConfigureManagementTLS(context.Context, managementtls.Configuration) error
	ReadManagementTLS(context.Context) (managementtls.Configuration, bool, error)
	ConfirmManagementTLS(context.Context, string, time.Time) (bool, error)
}

type SecretProtector interface {
	Encrypt(string, []byte) ([]byte, error)
}

type SecretProtectorOpener func() (SecretProtector, error)

type LocalCABundle struct {
	CACertificatePEM   []byte
	CAPrivateKeyPEM    []byte
	LeafCertificatePEM []byte
	LeafPrivateKeyPEM  []byte
	RootFingerprint    string
	LeafNotAfter       time.Time
	SANs               []string
}

type LocalCAGenerator func(time.Time, []string) (LocalCABundle, error)

type HardwareReviewStore interface {
	ConfirmSetupHardware(context.Context, string, int, int, time.Time) error
	ReadSetupHardwareReview(context.Context) (string, int, int, bool, error)
}

type CompletionStore interface {
	CompleteInitialSetup(context.Context, string, time.Time) (bool, error)
}

type Dependencies struct {
	StateStore            StateStore
	AuthorizationStore    AuthorizationStore
	AdministratorStore    AdministratorStore
	PasswordHasher        PasswordHasher
	StorageStore          StorageStore
	DirectoryPreparer     DirectoryPreparer
	ManagementTLSStore    ManagementTLSStore
	SecretProtectorOpener SecretProtectorOpener
	LocalCAGenerator      LocalCAGenerator
	HardwareReviewStore   HardwareReviewStore
	CompletionStore       CompletionStore
	Random                io.Reader
	Now                   func() time.Time
}

var (
	administratorUsernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,31}$`)
	hardwareIdentifierPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	hardwareDigestPattern        = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Status struct {
	InstallationState            string
	Phase                        string
	SetupRequired                bool
	BusinessAPIAvailable         bool
	BootstrapGenerationAvailable bool
	SupportedFlows               []string
}

type BootstrapGrant struct {
	Code      string
	ExpiresAt time.Time
}

type Session struct {
	ExpiresAt               time.Time
	SelectedFlow            string
	AdministratorConfigured bool
	AdministratorUsername   string
	InstanceDefaultLocale   string
	StorageConfigured       bool
	DataRoot                string
	RecordingsRoot          string
	HTTPSConfigured         bool
	HTTPSConfirmed          bool
	HTTPSMode               string
	HTTPSListenURL          string
	HTTPSRootFingerprint    string
	HTTPSLeafNotAfter       time.Time
	HardwareReviewed        bool
	HardwareDeviceCount     int
	HardwareLineCount       int
	HardwareInventoryDigest string
}

type AdministratorInput struct {
	Username              string
	Password              string
	PasswordConfirmation  string
	InstanceDefaultLocale string
}

type StorageInput struct {
	RecordingsRoot string
}

type HTTPSInput struct {
	Mode                    string
	ListenHost              string
	ListenPort              int
	SubjectAlternativeNames []string
}

type HardwareLine struct {
	ID                    string
	PhysicalDeviceID      string
	SubscriptionProfileID string
}

type HardwareDevice struct {
	ID                 string
	Transport          string
	State              string
	ModemFunctionCount int
	SIMSlotCount       int
	ResourceGroupCount int
}

type HardwareReviewInput struct {
	TopologyDigest string
	Devices        []HardwareDevice
	Lines          []HardwareLine
}

type Completion struct {
	InstallationState string
	ManagementURL     string
	LoginRequired     bool
}

type SessionGrant struct {
	Token string
	Session
}

type Service struct {
	stateStore          StateStore
	authorization       AuthorizationStore
	administrator       AdministratorStore
	passwordHasher      PasswordHasher
	storage             StorageStore
	prepareDirectory    DirectoryPreparer
	managementTLS       ManagementTLSStore
	openSecretProtector SecretProtectorOpener
	generateLocalCA     LocalCAGenerator
	hardwareReview      HardwareReviewStore
	completionStore     CompletionStore
	random              io.Reader
	now                 func() time.Time
	bootstrapTimeout    time.Duration
	sessionTimeout      time.Duration
	mutationMu          sync.Mutex
}

func New(dependencies Dependencies) (*Service, error) {
	if dependencies.StateStore == nil {
		return nil, fmt.Errorf("%w: StateStore is required", ErrDependenciesInvalid)
	}
	if (dependencies.AdministratorStore == nil) != (dependencies.PasswordHasher == nil) {
		return nil, fmt.Errorf("%w: AdministratorStore and PasswordHasher must be configured together", ErrDependenciesInvalid)
	}
	if (dependencies.StorageStore == nil) != (dependencies.DirectoryPreparer == nil) {
		return nil, fmt.Errorf("%w: StorageStore and DirectoryPreparer must be configured together", ErrDependenciesInvalid)
	}
	if (dependencies.SecretProtectorOpener == nil) != (dependencies.LocalCAGenerator == nil) {
		return nil, fmt.Errorf("%w: SecretProtectorOpener and LocalCAGenerator must be configured together", ErrDependenciesInvalid)
	}
	if dependencies.ManagementTLSStore == nil && dependencies.SecretProtectorOpener != nil {
		return nil, fmt.Errorf("%w: Local CA dependencies require ManagementTLSStore", ErrDependenciesInvalid)
	}
	random := dependencies.Random
	if random == nil {
		random = rand.Reader
	}
	now := dependencies.Now
	if now == nil {
		now = time.Now
	}
	service := &Service{
		stateStore:          dependencies.StateStore,
		authorization:       dependencies.AuthorizationStore,
		administrator:       dependencies.AdministratorStore,
		passwordHasher:      dependencies.PasswordHasher,
		storage:             dependencies.StorageStore,
		prepareDirectory:    dependencies.DirectoryPreparer,
		managementTLS:       dependencies.ManagementTLSStore,
		openSecretProtector: dependencies.SecretProtectorOpener,
		generateLocalCA:     dependencies.LocalCAGenerator,
		hardwareReview:      dependencies.HardwareReviewStore,
		completionStore:     dependencies.CompletionStore,
		random:              random,
		now:                 now,
		bootstrapTimeout:    10 * time.Minute,
		sessionTimeout:      30 * time.Minute,
	}
	return service, nil
}

func (service *Service) Status(ctx context.Context) (Status, error) {
	state, err := service.installationState(ctx)
	if err != nil {
		return Status{}, err
	}

	switch state {
	case InstallationUninitialized:
		return Status{
			InstallationState:            state,
			Phase:                        PhaseBootstrapRequired,
			SetupRequired:                true,
			BusinessAPIAvailable:         false,
			BootstrapGenerationAvailable: false,
			SupportedFlows:               []string{FlowCreateNew},
		}, nil
	case InstallationReady:
		return Status{
			InstallationState:            state,
			Phase:                        PhaseComplete,
			SetupRequired:                false,
			BusinessAPIAvailable:         true,
			BootstrapGenerationAvailable: false,
			SupportedFlows:               []string{},
		}, nil
	case InstallationMaintenance:
		return Status{
			InstallationState:            state,
			Phase:                        PhaseMaintenance,
			SetupRequired:                false,
			BusinessAPIAvailable:         false,
			BootstrapGenerationAvailable: false,
			SupportedFlows:               []string{},
		}, nil
	default:
		return Status{}, fmt.Errorf("unsupported installation state %q", state)
	}
}

func (service *Service) GenerateBootstrap(ctx context.Context) (BootstrapGrant, error) {
	if service == nil {
		return BootstrapGrant{}, fmt.Errorf("setup service is not configured")
	}
	service.mutationMu.Lock()
	defer service.mutationMu.Unlock()
	if service.authorization == nil {
		return BootstrapGrant{}, fmt.Errorf("setup authorization store is not configured")
	}
	if err := service.requireUninitialized(ctx); err != nil {
		return BootstrapGrant{}, err
	}
	code, hash, err := service.newToken(bootstrapTokenBytes)
	if err != nil {
		return BootstrapGrant{}, fmt.Errorf("generate bootstrap code: %w", err)
	}
	now := service.currentTime()
	expiresAt := now.Add(service.bootstrapTimeout)
	if err := service.authorization.ReplaceSetupBootstrap(ctx, hash, now.Unix(), expiresAt.Unix()); err != nil {
		return BootstrapGrant{}, fmt.Errorf("persist bootstrap authorization: %w", err)
	}
	return BootstrapGrant{Code: code, ExpiresAt: expiresAt}, nil
}

func (service *Service) ConsumeBootstrap(ctx context.Context, code string) (SessionGrant, error) {
	if service == nil {
		return SessionGrant{}, fmt.Errorf("setup service is not configured")
	}
	service.mutationMu.Lock()
	defer service.mutationMu.Unlock()
	if service.authorization == nil {
		return SessionGrant{}, fmt.Errorf("setup authorization store is not configured")
	}
	if err := service.requireUninitialized(ctx); err != nil {
		return SessionGrant{}, err
	}
	bootstrapHash, err := tokenHash(code, bootstrapTokenBytes)
	if err != nil {
		return SessionGrant{}, ErrBootstrapInvalidOrExpired
	}
	sessionToken, sessionHash, err := service.newToken(sessionTokenBytes)
	if err != nil {
		return SessionGrant{}, fmt.Errorf("generate setup session: %w", err)
	}
	now := service.currentTime()
	expiresAt := now.Add(service.sessionTimeout)
	consumed, err := service.authorization.ConsumeSetupBootstrap(
		ctx,
		bootstrapHash,
		sessionHash,
		now.Unix(),
		expiresAt.Unix(),
	)
	if err != nil {
		return SessionGrant{}, fmt.Errorf("consume bootstrap authorization: %w", err)
	}
	if !consumed {
		return SessionGrant{}, ErrBootstrapInvalidOrExpired
	}
	session, err := service.sessionDetails(ctx, expiresAt, FlowCreateNew)
	if err != nil {
		return SessionGrant{}, err
	}
	return SessionGrant{Token: sessionToken, Session: session}, nil
}

func (service *Service) ReadSession(ctx context.Context, token string) (Session, error) {
	if service == nil || service.authorization == nil {
		return Session{}, fmt.Errorf("setup authorization store is not configured")
	}
	if err := service.requireUninitialized(ctx); err != nil {
		return Session{}, err
	}
	hash, err := tokenHash(token, sessionTokenBytes)
	if err != nil {
		return Session{}, ErrSetupSessionUnauthorized
	}
	now := service.currentTime()
	expiresAtUnix, selectedFlow, found, err := service.authorization.ReadSetupSession(ctx, hash, now.Unix())
	if err != nil {
		return Session{}, fmt.Errorf("read setup session: %w", err)
	}
	if !found || selectedFlow != FlowCreateNew {
		return Session{}, ErrSetupSessionUnauthorized
	}
	return service.sessionDetails(ctx, time.Unix(expiresAtUnix, 0).UTC(), selectedFlow)
}

func (service *Service) ConfigureAdministrator(ctx context.Context, token string, input AdministratorInput) (Session, error) {
	if service == nil {
		return Session{}, fmt.Errorf("setup service is not configured")
	}
	service.mutationMu.Lock()
	defer service.mutationMu.Unlock()
	if service.administrator == nil || service.passwordHasher == nil {
		return Session{}, fmt.Errorf("initial administrator setup is not configured")
	}
	if _, err := service.ReadSession(ctx, token); err != nil {
		return Session{}, err
	}
	username, err := validateAdministratorInput(input)
	if err != nil {
		return Session{}, err
	}
	passwordHash, err := service.passwordHasher.Hash(input.Password)
	if err != nil {
		return Session{}, fmt.Errorf("hash initial administrator password: %w", err)
	}
	// Revalidate after the deliberately expensive password hash. GenerateBootstrap
	// shares mutationMu, so a root-issued replacement grant cannot race this write.
	if _, err := service.ReadSession(ctx, token); err != nil {
		return Session{}, err
	}
	if err := service.administrator.ConfigureInitialAdministrator(
		ctx,
		username,
		passwordHash,
		input.InstanceDefaultLocale,
		service.currentTime(),
	); err != nil {
		if errors.Is(err, ErrSetupUnavailable) {
			return Session{}, err
		}
		return Session{}, fmt.Errorf("persist initial administrator: %w", err)
	}
	return service.ReadSession(ctx, token)
}

// ProvisionAdministrator creates the sole administrator from the root-only
// control plane. It is idempotent for upgrades and never replaces an existing
// credential.
func (service *Service) ProvisionAdministrator(ctx context.Context, input AdministratorInput) (bool, error) {
	if service == nil {
		return false, fmt.Errorf("setup service is not configured")
	}
	service.mutationMu.Lock()
	defer service.mutationMu.Unlock()
	if service.administrator == nil || service.passwordHasher == nil {
		return false, fmt.Errorf("initial administrator setup is not configured")
	}
	_, _, configured, err := service.administrator.ReadInitialAdministrator(ctx)
	if err != nil {
		return false, fmt.Errorf("read initial administrator: %w", err)
	}
	if configured {
		return false, nil
	}
	if err := service.requireUninitialized(ctx); err != nil {
		return false, err
	}
	username, err := validateAdministratorInput(input)
	if err != nil {
		return false, err
	}
	passwordHash, err := service.passwordHasher.Hash(input.Password)
	if err != nil {
		return false, fmt.Errorf("hash provisioned administrator password: %w", err)
	}
	if err := service.administrator.ConfigureInitialAdministrator(ctx, username, passwordHash, input.InstanceDefaultLocale, service.currentTime()); err != nil {
		return false, fmt.Errorf("persist provisioned administrator: %w", err)
	}
	return true, nil
}

// BeginAdministratorSetup issues a restricted setup session after the
// administrator has authenticated. It replaces the root URL exchange without
// granting access to business APIs before the instance is ready.
func (service *Service) BeginAdministratorSetup(ctx context.Context) (SessionGrant, error) {
	if service == nil || service.authorization == nil {
		return SessionGrant{}, fmt.Errorf("setup authorization store is not configured")
	}
	if service.administrator == nil {
		return SessionGrant{}, fmt.Errorf("initial administrator setup is not configured")
	}
	service.mutationMu.Lock()
	defer service.mutationMu.Unlock()
	if err := service.requireUninitialized(ctx); err != nil {
		return SessionGrant{}, err
	}
	_, _, configured, err := service.administrator.ReadInitialAdministrator(ctx)
	if err != nil {
		return SessionGrant{}, fmt.Errorf("read provisioned administrator: %w", err)
	}
	if !configured {
		return SessionGrant{}, ErrSetupPrerequisiteMissing
	}
	_, bootstrapHash, err := service.newToken(bootstrapTokenBytes)
	if err != nil {
		return SessionGrant{}, fmt.Errorf("generate internal setup authorization: %w", err)
	}
	sessionToken, sessionHash, err := service.newToken(sessionTokenBytes)
	if err != nil {
		return SessionGrant{}, fmt.Errorf("generate administrator setup session: %w", err)
	}
	now := service.currentTime()
	expiresAt := now.Add(service.sessionTimeout)
	if err := service.authorization.ReplaceSetupBootstrap(ctx, bootstrapHash, now.Unix(), expiresAt.Unix()); err != nil {
		return SessionGrant{}, fmt.Errorf("persist internal setup authorization: %w", err)
	}
	consumed, err := service.authorization.ConsumeSetupBootstrap(ctx, bootstrapHash, sessionHash, now.Unix(), expiresAt.Unix())
	if err != nil || !consumed {
		return SessionGrant{}, fmt.Errorf("create administrator setup session: %w", err)
	}
	session, err := service.sessionDetails(ctx, expiresAt, FlowCreateNew)
	if err != nil {
		return SessionGrant{}, err
	}
	return SessionGrant{Token: sessionToken, Session: session}, nil
}

func (service *Service) ConfigureStorage(ctx context.Context, token string, input StorageInput) (Session, error) {
	if service == nil {
		return Session{}, fmt.Errorf("setup service is not configured")
	}
	service.mutationMu.Lock()
	defer service.mutationMu.Unlock()
	if service.storage == nil || service.prepareDirectory == nil {
		return Session{}, fmt.Errorf("setup storage is not configured")
	}
	session, err := service.ReadSession(ctx, token)
	if err != nil {
		return Session{}, err
	}
	if !session.AdministratorConfigured {
		return Session{}, ErrSetupPrerequisiteMissing
	}
	dataRoot := service.storage.SetupDataRoot()
	recordingsRoot := input.RecordingsRoot
	if recordingsRoot == "" {
		recordingsRoot = filepath.Join(dataRoot, "recordings")
	}
	if !filepath.IsAbs(recordingsRoot) || filepath.Clean(recordingsRoot) != recordingsRoot || recordingsRoot == dataRoot {
		return Session{}, ErrStorageRequestInvalid
	}
	dataRelativeToRecordings, err := filepath.Rel(recordingsRoot, dataRoot)
	if err != nil || (dataRelativeToRecordings != ".." && !strings.HasPrefix(dataRelativeToRecordings, ".."+string(filepath.Separator))) {
		return Session{}, ErrStorageRequestInvalid
	}
	dataIdentity, err := service.prepareDirectory(dataRoot)
	if err != nil {
		return Session{}, fmt.Errorf("validate setup data root: %w", err)
	}
	recordingsIdentity, err := service.prepareDirectory(recordingsRoot)
	if err != nil {
		return Session{}, fmt.Errorf("validate setup recordings root: %w", err)
	}
	if dataIdentity.Device > math.MaxInt64 || dataIdentity.Inode > math.MaxInt64 ||
		recordingsIdentity.Device > math.MaxInt64 || recordingsIdentity.Inode > math.MaxInt64 {
		return Session{}, errors.New("setup storage identity exceeds the durable integer range")
	}
	if err := service.storage.ConfigureSetupStorage(
		ctx,
		dataIdentity.Path,
		recordingsIdentity.Path,
		dataIdentity.Device,
		dataIdentity.Inode,
		recordingsIdentity.Device,
		recordingsIdentity.Inode,
		service.currentTime(),
	); err != nil {
		return Session{}, fmt.Errorf("persist setup storage: %w", err)
	}
	return service.ReadSession(ctx, token)
}

func (service *Service) ConfigureHTTPS(ctx context.Context, token string, input HTTPSInput) (Session, error) {
	if service == nil {
		return Session{}, fmt.Errorf("setup service is not configured")
	}
	service.mutationMu.Lock()
	defer service.mutationMu.Unlock()
	if service.managementTLS == nil {
		return Session{}, fmt.Errorf("management TLS setup is not configured")
	}
	session, err := service.ReadSession(ctx, token)
	if err != nil {
		return Session{}, err
	}
	if !session.StorageConfigured {
		return Session{}, ErrSetupPrerequisiteMissing
	}
	mode := managementtls.Mode(input.Mode)
	host := strings.TrimSpace(input.ListenHost)
	if input.ListenPort < 1 || input.ListenPort > 65535 || host == "" {
		return Session{}, ErrHTTPSRequestInvalid
	}
	configuration := managementtls.Configuration{
		Mode:         mode,
		ListenHost:   host,
		ListenPort:   input.ListenPort,
		ConfiguredAt: service.currentTime(),
	}
	switch mode {
	case managementtls.ModeLoopbackOnly:
		if !isLoopbackHost(host) || len(input.SubjectAlternativeNames) != 0 {
			return Session{}, ErrHTTPSRequestInvalid
		}
		configuration.Confirmed = true
	case managementtls.ModeLocalCA:
		if service.openSecretProtector == nil || service.generateLocalCA == nil || isUnspecifiedOrUnsafeHost(host) {
			return Session{}, ErrHTTPSRequestInvalid
		}
		sans := append([]string(nil), input.SubjectAlternativeNames...)
		sans = append(sans, host)
		bundle, err := service.generateLocalCA(service.currentTime(), sans)
		if err != nil {
			return Session{}, ErrHTTPSRequestInvalid
		}
		protector, err := service.openSecretProtector()
		if err != nil {
			return Session{}, fmt.Errorf("open management TLS key protector: %w", err)
		}
		encryptedCAKey, err := protector.Encrypt("management-tls-ca-private-key-v1", bundle.CAPrivateKeyPEM)
		if err != nil {
			return Session{}, fmt.Errorf("protect local CA private key: %w", err)
		}
		encryptedLeafKey, err := protector.Encrypt("management-tls-leaf-private-key-v1", bundle.LeafPrivateKeyPEM)
		if err != nil {
			return Session{}, fmt.Errorf("protect management leaf private key: %w", err)
		}
		clear(bundle.CAPrivateKeyPEM)
		clear(bundle.LeafPrivateKeyPEM)
		configuration.SubjectAlternativeNames = bundle.SANs
		configuration.CACertificatePEM = bundle.CACertificatePEM
		configuration.LeafCertificatePEM = bundle.LeafCertificatePEM
		configuration.EncryptedCAPrivateKey = encryptedCAKey
		configuration.EncryptedLeafPrivateKey = encryptedLeafKey
		configuration.RootFingerprintSHA256 = bundle.RootFingerprint
		configuration.LeafNotAfter = bundle.LeafNotAfter
		configuration.Confirmed = false
	default:
		return Session{}, ErrHTTPSRequestInvalid
	}
	if err := service.managementTLS.ConfigureManagementTLS(ctx, configuration); err != nil {
		return Session{}, fmt.Errorf("persist management TLS setup: %w", err)
	}
	return service.ReadSession(ctx, token)
}

func (service *Service) ConfirmHTTPS(ctx context.Context, token, rootFingerprint string) (Session, error) {
	if service == nil {
		return Session{}, fmt.Errorf("setup service is not configured")
	}
	service.mutationMu.Lock()
	defer service.mutationMu.Unlock()
	if service.managementTLS == nil {
		return Session{}, fmt.Errorf("management TLS setup is not configured")
	}
	if _, err := service.ReadSession(ctx, token); err != nil {
		return Session{}, err
	}
	confirmed, err := service.managementTLS.ConfirmManagementTLS(ctx, strings.TrimSpace(rootFingerprint), service.currentTime())
	if err != nil {
		return Session{}, fmt.Errorf("confirm management TLS setup: %w", err)
	}
	if !confirmed {
		return Session{}, ErrHTTPSConfirmationInvalid
	}
	return service.ReadSession(ctx, token)
}

func (service *Service) ReadRootCertificate(ctx context.Context, token string) ([]byte, string, error) {
	if _, err := service.ReadSession(ctx, token); err != nil {
		return nil, "", err
	}
	if service.managementTLS == nil {
		return nil, "", fmt.Errorf("management TLS setup is not configured")
	}
	configuration, found, err := service.managementTLS.ReadManagementTLS(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("read management TLS root certificate: %w", err)
	}
	if !found || configuration.Mode != managementtls.ModeLocalCA || len(configuration.CACertificatePEM) == 0 {
		return nil, "", ErrHTTPSRequestInvalid
	}
	return append([]byte(nil), configuration.CACertificatePEM...), configuration.RootFingerprintSHA256, nil
}

func (service *Service) ConfirmHardwareReview(ctx context.Context, token string, input HardwareReviewInput) (Session, error) {
	if service == nil {
		return Session{}, fmt.Errorf("setup service is not configured")
	}
	service.mutationMu.Lock()
	defer service.mutationMu.Unlock()
	if service.hardwareReview == nil {
		return Session{}, fmt.Errorf("setup hardware review is not configured")
	}
	session, err := service.ReadSession(ctx, token)
	if err != nil {
		return Session{}, err
	}
	if !session.HTTPSConfigured || !session.HTTPSConfirmed {
		return Session{}, ErrSetupPrerequisiteMissing
	}
	digest, err := hardwareDigest(input)
	if err != nil {
		return Session{}, err
	}
	if err := service.hardwareReview.ConfirmSetupHardware(ctx, digest, len(input.Devices), len(input.Lines), service.currentTime()); err != nil {
		return Session{}, fmt.Errorf("persist setup hardware review: %w", err)
	}
	return service.ReadSession(ctx, token)
}

func (service *Service) Complete(ctx context.Context, token string, _ HardwareReviewInput) (Completion, error) {
	if service == nil {
		return Completion{}, fmt.Errorf("setup service is not configured")
	}
	service.mutationMu.Lock()
	defer service.mutationMu.Unlock()
	if service.completionStore == nil || service.storage == nil {
		return Completion{}, fmt.Errorf("setup completion is not configured")
	}
	session, err := service.ReadSession(ctx, token)
	if err != nil {
		return Completion{}, err
	}
	if !session.AdministratorConfigured || !session.StorageConfigured {
		return Completion{}, ErrSetupPrerequisiteMissing
	}
	dataRoot, recordingsRoot, dataDevice, dataInode, recordingsDevice, recordingsInode, configured, err := service.storage.ReadSetupStorage(ctx)
	if err != nil || !configured {
		return Completion{}, ErrSetupPreflightFailed
	}
	dataIdentity, err := service.prepareDirectory(dataRoot)
	if err != nil || dataIdentity.Device != dataDevice || dataIdentity.Inode != dataInode {
		return Completion{}, ErrSetupPreflightFailed
	}
	recordingsIdentity, err := service.prepareDirectory(recordingsRoot)
	if err != nil || recordingsIdentity.Device != recordingsDevice || recordingsIdentity.Inode != recordingsInode {
		return Completion{}, ErrSetupPreflightFailed
	}
	completed, err := service.completionStore.CompleteInitialSetup(ctx, "", service.currentTime())
	if err != nil {
		return Completion{}, fmt.Errorf("atomically complete initial setup: %w", err)
	}
	if !completed {
		return Completion{}, ErrSetupPreflightFailed
	}
	// The core transition is authoritative. A leftover runtime row cannot be used
	// once installation_state is ready and will be removed on later maintenance.
	_ = service.authorization.RevokeSetupAuthorization(ctx)
	return Completion{
		InstallationState: InstallationReady,
		ManagementURL:     "/",
		LoginRequired:     true,
	}, nil
}

func hardwareDigest(input HardwareReviewInput) (string, error) {
	if !hardwareDigestPattern.MatchString(input.TopologyDigest) || len(input.Devices) > 1024 || len(input.Lines) > 1024 {
		return "", ErrHardwareReviewInvalid
	}
	devices := append([]HardwareDevice(nil), input.Devices...)
	sort.Slice(devices, func(left, right int) bool { return devices[left].ID < devices[right].ID })
	lines := append([]HardwareLine(nil), input.Lines...)
	sort.Slice(lines, func(left, right int) bool { return lines[left].ID < lines[right].ID })
	seenDevices := make(map[string]struct{}, len(devices))
	seenLines := make(map[string]struct{}, len(lines))
	seenProfiles := make(map[string]struct{}, len(lines))
	var canonical strings.Builder
	fmt.Fprintf(&canonical, "topology=%s\ndevices=%d\n", input.TopologyDigest, len(devices))
	for _, device := range devices {
		if !hardwareIdentifierPattern.MatchString(device.ID) || device.Transport == "" || device.State == "" ||
			device.ModemFunctionCount < 0 || device.SIMSlotCount < 0 || device.ResourceGroupCount < 0 {
			return "", ErrHardwareReviewInvalid
		}
		if _, duplicate := seenDevices[device.ID]; duplicate {
			return "", ErrHardwareReviewInvalid
		}
		seenDevices[device.ID] = struct{}{}
		fmt.Fprintf(&canonical, "device=%q|transport=%q|state=%q|functions=%d|slots=%d|groups=%d\n",
			device.ID, device.Transport, device.State, device.ModemFunctionCount, device.SIMSlotCount, device.ResourceGroupCount)
	}
	for _, line := range lines {
		if !hardwareIdentifierPattern.MatchString(line.ID) || !hardwareIdentifierPattern.MatchString(line.PhysicalDeviceID) ||
			!hardwareIdentifierPattern.MatchString(line.SubscriptionProfileID) {
			return "", ErrHardwareReviewInvalid
		}
		if _, present := seenDevices[line.PhysicalDeviceID]; !present {
			return "", ErrHardwareReviewInvalid
		}
		if _, duplicate := seenLines[line.ID]; duplicate {
			return "", ErrHardwareReviewInvalid
		}
		if _, duplicate := seenProfiles[line.SubscriptionProfileID]; duplicate {
			return "", ErrHardwareReviewInvalid
		}
		seenLines[line.ID] = struct{}{}
		seenProfiles[line.SubscriptionProfileID] = struct{}{}
		fmt.Fprintf(&canonical, "line=%q device=%q profile=%q\n", line.ID, line.PhysicalDeviceID, line.SubscriptionProfileID)
	}
	digest := sha256.Sum256([]byte(canonical.String()))
	return fmt.Sprintf("%x", digest), nil
}

func (service *Service) sessionDetails(ctx context.Context, expiresAt time.Time, selectedFlow string) (Session, error) {
	session := Session{ExpiresAt: expiresAt, SelectedFlow: selectedFlow, InstanceDefaultLocale: "en-US"}
	if service.administrator == nil {
		return session, nil
	}
	username, locale, configured, err := service.administrator.ReadInitialAdministrator(ctx)
	if err != nil {
		return Session{}, fmt.Errorf("read initial administrator setup: %w", err)
	}
	session.AdministratorConfigured = configured
	session.AdministratorUsername = username
	session.InstanceDefaultLocale = locale
	if service.storage == nil {
		return session, nil
	}
	dataRoot, recordingsRoot, _, _, _, _, storageConfigured, err := service.storage.ReadSetupStorage(ctx)
	if err != nil {
		return Session{}, fmt.Errorf("read setup storage: %w", err)
	}
	if recordingsRoot == "" && dataRoot != "" {
		recordingsRoot = filepath.Join(dataRoot, "recordings")
	}
	session.StorageConfigured = storageConfigured
	session.DataRoot = dataRoot
	session.RecordingsRoot = recordingsRoot
	if service.managementTLS == nil {
		return session, nil
	}
	managementTLS, tlsConfigured, err := service.managementTLS.ReadManagementTLS(ctx)
	if err != nil {
		return Session{}, fmt.Errorf("read management TLS setup: %w", err)
	}
	if tlsConfigured {
		session.HTTPSConfigured = true
		session.HTTPSConfirmed = managementTLS.Confirmed
		session.HTTPSMode = string(managementTLS.Mode)
		session.HTTPSListenURL = managementListenURL(managementTLS)
		session.HTTPSRootFingerprint = managementTLS.RootFingerprintSHA256
		session.HTTPSLeafNotAfter = managementTLS.LeafNotAfter
	}
	if service.hardwareReview == nil {
		return session, nil
	}
	digest, deviceCount, lineCount, reviewed, err := service.hardwareReview.ReadSetupHardwareReview(ctx)
	if err != nil {
		return Session{}, fmt.Errorf("read setup hardware review: %w", err)
	}
	session.HardwareReviewed = reviewed
	session.HardwareInventoryDigest = digest
	session.HardwareDeviceCount = deviceCount
	session.HardwareLineCount = lineCount
	return session, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isUnspecifiedOrUnsafeHost(host string) bool {
	if strings.ContainsAny(host, "/\\") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsUnspecified() || ip.IsMulticast()
	}
	return host == "" || strings.Contains(host, "*")
}

func managementListenURL(configuration managementtls.Configuration) string {
	scheme := "https"
	if configuration.Mode == managementtls.ModeLoopbackOnly {
		scheme = "http"
	}
	return (&url.URL{Scheme: scheme, Host: net.JoinHostPort(configuration.ListenHost, fmt.Sprintf("%d", configuration.ListenPort))}).String()
}

func validateAdministratorInput(input AdministratorInput) (string, error) {
	username := strings.ToLower(strings.TrimSpace(input.Username))
	if !administratorUsernamePattern.MatchString(username) {
		return "", ErrAdministratorRequestInvalid
	}
	if input.InstanceDefaultLocale != "zh-CN" && input.InstanceDefaultLocale != "en-US" {
		return "", ErrAdministratorRequestInvalid
	}
	if input.Password != input.PasswordConfirmation ||
		len(input.Password) > 256 ||
		utf8.RuneCountInString(input.Password) < 12 ||
		utf8.RuneCountInString(input.Password) > 128 {
		return "", ErrAdministratorRequestInvalid
	}
	for _, character := range input.Password {
		if unicode.IsControl(character) {
			return "", ErrAdministratorRequestInvalid
		}
	}
	return username, nil
}

func (service *Service) installationState(ctx context.Context) (string, error) {
	if service == nil || service.stateStore == nil {
		return "", fmt.Errorf("setup state store is not configured")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	state, err := service.stateStore.InstallationState(ctx)
	if err != nil {
		return "", fmt.Errorf("read setup state: %w", err)
	}
	return state, nil
}

func (service *Service) requireUninitialized(ctx context.Context) error {
	state, err := service.installationState(ctx)
	if err != nil {
		return err
	}
	if state != InstallationUninitialized {
		return ErrSetupUnavailable
	}
	return nil
}

func (service *Service) currentTime() time.Time {
	return service.now().UTC().Truncate(time.Second)
}

func (service *Service) newToken(size int) (string, [32]byte, error) {
	var zero [32]byte
	bytes := make([]byte, size)
	if _, err := io.ReadFull(service.random, bytes); err != nil {
		return "", zero, err
	}
	token := base64.RawURLEncoding.EncodeToString(bytes)
	return token, sha256.Sum256([]byte(token)), nil
}

func tokenHash(token string, expectedBytes int) ([32]byte, error) {
	var zero [32]byte
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(decoded) != expectedBytes || base64.RawURLEncoding.EncodeToString(decoded) != token {
		return zero, errors.New("invalid token encoding")
	}
	return sha256.Sum256([]byte(token)), nil
}
