package setup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/domain/managementtls"
	"github.com/leonfox28/simplus/internal/security/managementcert"
	storagefs "github.com/leonfox28/simplus/internal/storage/filesystem"
)

type testStore struct {
	mu sync.Mutex

	state    string
	stateErr error

	bootstrapHash     [32]byte
	bootstrapExpires  int64
	bootstrapConsumed bool
	sessions          map[[32]byte]testSession

	administratorUsername string
	administratorHash     string
	administratorLocale   string

	dataRoot          string
	recordingsRoot    string
	storageConfigured bool
	dataDevice        uint64
	dataInode         uint64
	recordingsDevice  uint64
	recordingsInode   uint64

	managementTLS      managementtls.Configuration
	managementTLSFound bool

	hardwareDigest      string
	hardwareDeviceCount int
	hardwareLineCount   int
	hardwareReviewed    bool
}

type testSession struct {
	expiresAt int64
	flow      string
}

func (store *testStore) InstallationState(context.Context) (string, error) {
	return store.state, store.stateErr
}

func (store *testStore) ReplaceSetupBootstrap(_ context.Context, hash [32]byte, _ int64, expires int64) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.bootstrapHash = hash
	store.bootstrapExpires = expires
	store.bootstrapConsumed = false
	store.sessions = make(map[[32]byte]testSession)
	return nil
}

func (store *testStore) ConsumeSetupBootstrap(_ context.Context, bootstrapHash, sessionHash [32]byte, now, expires int64) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.bootstrapHash != bootstrapHash || store.bootstrapConsumed || store.bootstrapExpires <= now {
		return false, nil
	}
	store.bootstrapConsumed = true
	if store.sessions == nil {
		store.sessions = make(map[[32]byte]testSession)
	}
	store.sessions[sessionHash] = testSession{expiresAt: expires, flow: FlowCreateNew}
	return true, nil
}

func (store *testStore) RevokeSetupAuthorization(context.Context) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.sessions = make(map[[32]byte]testSession)
	store.bootstrapHash = [32]byte{}
	return nil
}

func (store *testStore) ReadSetupSession(_ context.Context, hash [32]byte, now int64) (int64, string, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	session, ok := store.sessions[hash]
	if !ok || session.expiresAt <= now {
		return 0, "", false, nil
	}
	return session.expiresAt, session.flow, true, nil
}

func (store *testStore) ConfigureInitialAdministrator(_ context.Context, username, passwordHash, locale string, _ time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.state != InstallationUninitialized {
		return ErrSetupUnavailable
	}
	store.administratorUsername = username
	store.administratorHash = passwordHash
	store.administratorLocale = locale
	return nil
}

func (store *testStore) ReadInitialAdministrator(context.Context) (string, string, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	locale := store.administratorLocale
	if locale == "" {
		locale = "en-US"
	}
	return store.administratorUsername, locale, store.administratorUsername != "", nil
}

func (store *testStore) SetupDataRoot() string {
	if store.dataRoot == "" {
		return "/srv/simplus/data"
	}
	return store.dataRoot
}

func (store *testStore) ConfigureSetupStorage(
	_ context.Context,
	dataRoot string,
	recordingsRoot string,
	dataDevice, dataInode, recordingsDevice, recordingsInode uint64,
	_ time.Time,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.dataRoot = dataRoot
	store.recordingsRoot = recordingsRoot
	store.storageConfigured = true
	store.dataDevice = dataDevice
	store.dataInode = dataInode
	store.recordingsDevice = recordingsDevice
	store.recordingsInode = recordingsInode
	return nil
}

func (store *testStore) ReadSetupStorage(context.Context) (string, string, uint64, uint64, uint64, uint64, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	dataRoot := store.dataRoot
	if dataRoot == "" {
		dataRoot = "/srv/simplus/data"
	}
	return dataRoot, store.recordingsRoot, store.dataDevice, store.dataInode, store.recordingsDevice, store.recordingsInode, store.storageConfigured, nil
}

func (store *testStore) ConfigureManagementTLS(_ context.Context, configuration managementtls.Configuration) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.managementTLS = configuration
	store.managementTLSFound = true
	return nil
}

func (store *testStore) ReadManagementTLS(context.Context) (managementtls.Configuration, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.managementTLS, store.managementTLSFound, nil
}

func (store *testStore) ConfirmManagementTLS(_ context.Context, fingerprint string, now time.Time) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.managementTLSFound || store.managementTLS.RootFingerprintSHA256 != fingerprint {
		return false, nil
	}
	store.managementTLS.Confirmed = true
	store.managementTLS.ConfiguredAt = now
	return true, nil
}

func (store *testStore) ConfirmSetupHardware(_ context.Context, digest string, deviceCount, lineCount int, _ time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.hardwareDigest = digest
	store.hardwareDeviceCount = deviceCount
	store.hardwareLineCount = lineCount
	store.hardwareReviewed = true
	return nil
}

func (store *testStore) ReadSetupHardwareReview(context.Context) (string, int, int, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.hardwareDigest, store.hardwareDeviceCount, store.hardwareLineCount, store.hardwareReviewed, nil
}

func (store *testStore) CompleteInitialSetup(_ context.Context, _ string, _ time.Time) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.state != InstallationUninitialized || store.administratorUsername == "" || !store.storageConfigured {
		return false, nil
	}
	store.state = InstallationReady
	return true, nil
}

type testProtector struct{}

func (testProtector) Encrypt(label string, plaintext []byte) ([]byte, error) {
	return append([]byte(label+":"), plaintext...), nil
}

type testPasswordHasher struct {
	hash string
	err  error
}

func (hasher testPasswordHasher) Hash(string) (string, error) {
	return hasher.hash, hasher.err
}

func newTestService(store *testStore) *Service {
	service := New(store, store)
	service.now = func() time.Time { return time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC) }
	randomBytes := append(bytes.Repeat([]byte{0x11}, 32), bytes.Repeat([]byte{0x22}, 32)...)
	randomBytes = append(randomBytes, bytes.Repeat([]byte{0x33}, 32)...)
	service.random = bytes.NewReader(randomBytes)
	service.passwordHasher = testPasswordHasher{hash: "$argon2id$test-password-hash"}
	service.prepareDirectory = func(path string) (storagefs.DirectoryIdentity, error) {
		return storagefs.DirectoryIdentity{Path: path, Device: 1, Inode: 2}, nil
	}
	service.openSecretProtector = func() (SecretProtector, error) { return testProtector{}, nil }
	service.generateLocalCA = managementcert.GenerateLocalCA
	return service
}

func TestStatusMapsInstallationStateToSetupBoundary(t *testing.T) {
	for _, test := range []struct {
		name     string
		state    string
		expected Status
	}{
		{
			name:  "uninitialized",
			state: InstallationUninitialized,
			expected: Status{
				InstallationState:            InstallationUninitialized,
				Phase:                        PhaseBootstrapRequired,
				SetupRequired:                true,
				BusinessAPIAvailable:         false,
				BootstrapGenerationAvailable: false,
				SupportedFlows:               []string{FlowCreateNew},
			},
		},
		{
			name:  "ready",
			state: InstallationReady,
			expected: Status{
				InstallationState:            InstallationReady,
				Phase:                        PhaseComplete,
				SetupRequired:                false,
				BusinessAPIAvailable:         true,
				BootstrapGenerationAvailable: false,
				SupportedFlows:               []string{},
			},
		},
		{
			name:  "maintenance",
			state: InstallationMaintenance,
			expected: Status{
				InstallationState:            InstallationMaintenance,
				Phase:                        PhaseMaintenance,
				SetupRequired:                false,
				BusinessAPIAvailable:         false,
				BootstrapGenerationAvailable: false,
				SupportedFlows:               []string{},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &testStore{state: test.state}
			status, err := New(store, store).Status(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(status, test.expected) {
				t.Fatalf("status = %#v, want %#v", status, test.expected)
			}
		})
	}
}

func TestProvisionAdministratorIsNoOpForExistingReadyInstance(t *testing.T) {
	store := &testStore{state: InstallationReady, administratorUsername: "simplus_admin", administratorLocale: "zh-CN"}
	created, err := newTestService(store).ProvisionAdministrator(context.Background(), AdministratorInput{
		Username: "simplus_admin", Password: "replacement-password", PasswordConfirmation: "replacement-password", InstanceDefaultLocale: "zh-CN",
	})
	if err != nil || created {
		t.Fatalf("ready provisioning = %t, %v", created, err)
	}
}

func TestBootstrapIsSingleUseAndCreatesRestrictedSession(t *testing.T) {
	store := &testStore{state: InstallationUninitialized}
	service := newTestService(store)

	grant, err := service.GenerateBootstrap(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(grant.Code) != 43 {
		t.Fatalf("bootstrap code length = %d, want 43", len(grant.Code))
	}
	if grant.ExpiresAt.Sub(service.now()) != 10*time.Minute {
		t.Fatalf("bootstrap lifetime = %s", grant.ExpiresAt.Sub(service.now()))
	}
	if store.bootstrapHash != sha256.Sum256([]byte(grant.Code)) {
		t.Fatal("authorization store did not retain exactly the bootstrap-code hash")
	}

	sessionGrant, err := service.ConsumeBootstrap(context.Background(), grant.Code)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessionGrant.Token) != 43 {
		t.Fatalf("session token length = %d, want 43", len(sessionGrant.Token))
	}
	if sessionGrant.ExpiresAt.Sub(service.now()) != 30*time.Minute {
		t.Fatalf("session lifetime = %s", sessionGrant.ExpiresAt.Sub(service.now()))
	}
	if _, err := service.ConsumeBootstrap(context.Background(), grant.Code); !errors.Is(err, ErrBootstrapInvalidOrExpired) {
		t.Fatalf("second bootstrap consumption error = %v", err)
	}

	if sessionGrant.SelectedFlow != FlowCreateNew {
		t.Fatalf("selected flow = %q", sessionGrant.SelectedFlow)
	}
	resumed, err := service.ReadSession(context.Background(), sessionGrant.Token)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resumed, sessionGrant.Session) {
		t.Fatalf("resumed session = %#v, want %#v", resumed, sessionGrant.Session)
	}
}

func TestConfigureAdministratorPersistsOnlyAfterAuthorizedValidation(t *testing.T) {
	store := &testStore{state: InstallationUninitialized}
	service := newTestService(store)
	grant, err := service.GenerateBootstrap(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sessionGrant, err := service.ConsumeBootstrap(context.Background(), grant.Code)
	if err != nil {
		t.Fatal(err)
	}

	session, err := service.ConfigureAdministrator(context.Background(), sessionGrant.Token, AdministratorInput{
		Username:              " Leon ",
		Password:              "correct horse battery staple",
		PasswordConfirmation:  "correct horse battery staple",
		InstanceDefaultLocale: "zh-CN",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !session.AdministratorConfigured || session.AdministratorUsername != "leon" || session.InstanceDefaultLocale != "zh-CN" {
		t.Fatalf("configured session = %#v", session)
	}
	if store.administratorHash != "$argon2id$test-password-hash" {
		t.Fatalf("stored hash = %q", store.administratorHash)
	}

	for _, input := range []AdministratorInput{
		{Username: "bad user", Password: "correct horse battery staple", PasswordConfirmation: "correct horse battery staple", InstanceDefaultLocale: "en-US"},
		{Username: "admin", Password: "too-short", PasswordConfirmation: "too-short", InstanceDefaultLocale: "en-US"},
		{Username: "admin", Password: "correct horse battery staple", PasswordConfirmation: "different password value", InstanceDefaultLocale: "en-US"},
		{Username: "admin", Password: "correct horse battery staple", PasswordConfirmation: "correct horse battery staple", InstanceDefaultLocale: "fr-FR"},
	} {
		if _, err := service.ConfigureAdministrator(context.Background(), sessionGrant.Token, input); !errors.Is(err, ErrAdministratorRequestInvalid) {
			t.Fatalf("ConfigureAdministrator(%#v) error = %v", input, err)
		}
	}
	if _, err := service.ConfigureAdministrator(context.Background(), "not-a-token", AdministratorInput{
		Username: "admin", Password: "correct horse battery staple", PasswordConfirmation: "correct horse battery staple", InstanceDefaultLocale: "en-US",
	}); !errors.Is(err, ErrSetupSessionUnauthorized) {
		t.Fatalf("unauthorized administrator setup error = %v", err)
	}
}

func TestConfigureStorageRequiresAdministratorAndPersistsValidatedRoots(t *testing.T) {
	store := &testStore{state: InstallationUninitialized}
	service := newTestService(store)
	grant, err := service.GenerateBootstrap(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sessionGrant, err := service.ConsumeBootstrap(context.Background(), grant.Code)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfigureStorage(context.Background(), sessionGrant.Token, StorageInput{}); !errors.Is(err, ErrSetupPrerequisiteMissing) {
		t.Fatalf("storage without administrator error = %v", err)
	}
	if _, err := service.ConfigureAdministrator(context.Background(), sessionGrant.Token, AdministratorInput{
		Username: "admin", Password: "correct horse battery staple", PasswordConfirmation: "correct horse battery staple", InstanceDefaultLocale: "en-US",
	}); err != nil {
		t.Fatal(err)
	}
	session, err := service.ConfigureStorage(context.Background(), sessionGrant.Token, StorageInput{})
	if err != nil {
		t.Fatal(err)
	}
	if !session.StorageConfigured || session.DataRoot != "/srv/simplus/data" || session.RecordingsRoot != "/srv/simplus/data/recordings" {
		t.Fatalf("storage session = %#v", session)
	}
	if _, err := service.ConfigureStorage(context.Background(), sessionGrant.Token, StorageInput{RecordingsRoot: "/srv"}); !errors.Is(err, ErrStorageRequestInvalid) {
		t.Fatalf("ancestor recordings root error = %v", err)
	}
	if _, err := service.ConfigureStorage(context.Background(), sessionGrant.Token, StorageInput{RecordingsRoot: "relative"}); !errors.Is(err, ErrStorageRequestInvalid) {
		t.Fatalf("relative recordings root error = %v", err)
	}
}

func TestConfigureHTTPSSupportsLoopbackAndConfirmedLocalCA(t *testing.T) {
	store := &testStore{state: InstallationUninitialized}
	service := newTestService(store)
	grant, err := service.GenerateBootstrap(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sessionGrant, err := service.ConsumeBootstrap(context.Background(), grant.Code)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfigureHTTPS(context.Background(), sessionGrant.Token, HTTPSInput{Mode: "loopback-only", ListenHost: "127.0.0.1", ListenPort: 8080}); !errors.Is(err, ErrSetupPrerequisiteMissing) {
		t.Fatalf("HTTPS without storage error = %v", err)
	}
	if _, err := service.ConfigureAdministrator(context.Background(), sessionGrant.Token, AdministratorInput{
		Username: "admin", Password: "correct horse battery staple", PasswordConfirmation: "correct horse battery staple", InstanceDefaultLocale: "en-US",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfigureStorage(context.Background(), sessionGrant.Token, StorageInput{}); err != nil {
		t.Fatal(err)
	}
	loopback, err := service.ConfigureHTTPS(context.Background(), sessionGrant.Token, HTTPSInput{
		Mode: "loopback-only", ListenHost: "127.0.0.1", ListenPort: 8080,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !loopback.HTTPSConfigured || !loopback.HTTPSConfirmed || loopback.HTTPSListenURL != "http://127.0.0.1:8080" {
		t.Fatalf("loopback HTTPS session = %#v", loopback)
	}

	candidate, err := service.ConfigureHTTPS(context.Background(), sessionGrant.Token, HTTPSInput{
		Mode: "local-ca", ListenHost: "192.168.50.10", ListenPort: 8443, SubjectAlternativeNames: []string{"simplus.local"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !candidate.HTTPSConfigured || candidate.HTTPSConfirmed || candidate.HTTPSListenURL != "https://192.168.50.10:8443" || candidate.HTTPSRootFingerprint == "" {
		t.Fatalf("local CA candidate = %#v", candidate)
	}
	certificate, fingerprint, err := service.ReadRootCertificate(context.Background(), sessionGrant.Token)
	if err != nil {
		t.Fatal(err)
	}
	if len(certificate) == 0 || fingerprint != candidate.HTTPSRootFingerprint {
		t.Fatalf("root certificate/fingerprint = %d/%q", len(certificate), fingerprint)
	}
	if _, err := service.ConfirmHTTPS(context.Background(), sessionGrant.Token, "wrong"); !errors.Is(err, ErrHTTPSConfirmationInvalid) {
		t.Fatalf("wrong fingerprint confirmation error = %v", err)
	}
	confirmed, err := service.ConfirmHTTPS(context.Background(), sessionGrant.Token, fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if !confirmed.HTTPSConfirmed {
		t.Fatalf("confirmed HTTPS session = %#v", confirmed)
	}
	devices := []HardwareDevice{{ID: "device-1", Transport: "usb", State: "available", ModemFunctionCount: 1, SIMSlotCount: 1, ResourceGroupCount: 1}}
	topologyDigest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := service.ConfirmHardwareReview(context.Background(), sessionGrant.Token, HardwareReviewInput{
		TopologyDigest: topologyDigest,
		Devices:        devices,
		Lines:          []HardwareLine{{ID: "line-1", PhysicalDeviceID: "missing-device", SubscriptionProfileID: "profile-1"}},
	}); !errors.Is(err, ErrHardwareReviewInvalid) {
		t.Fatalf("unknown device review error = %v", err)
	}
	hardware, err := service.ConfirmHardwareReview(context.Background(), sessionGrant.Token, HardwareReviewInput{
		TopologyDigest: topologyDigest,
		Devices:        devices,
		Lines:          []HardwareLine{{ID: "line-1", PhysicalDeviceID: "device-1", SubscriptionProfileID: "profile-1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hardware.HardwareReviewed || hardware.HardwareDeviceCount != 1 || hardware.HardwareLineCount != 1 || len(hardware.HardwareInventoryDigest) != 64 {
		t.Fatalf("hardware review session = %#v", hardware)
	}
	if _, err := service.ConfigureHTTPS(context.Background(), sessionGrant.Token, HTTPSInput{Mode: "local-ca", ListenHost: "0.0.0.0", ListenPort: 8443}); !errors.Is(err, ErrHTTPSRequestInvalid) {
		t.Fatalf("unspecified listen host error = %v", err)
	}
	completion, err := service.Complete(context.Background(), sessionGrant.Token, HardwareReviewInput{
		TopologyDigest: topologyDigest,
		Devices:        devices,
		Lines:          []HardwareLine{{ID: "line-1", PhysicalDeviceID: "device-1", SubscriptionProfileID: "profile-1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if completion.InstallationState != InstallationReady || completion.ManagementURL != "/" || !completion.LoginRequired {
		t.Fatalf("completion = %#v", completion)
	}
	if _, err := service.ReadSession(context.Background(), sessionGrant.Token); !errors.Is(err, ErrSetupUnavailable) {
		t.Fatalf("completed setup session error = %v", err)
	}
}

func TestBootstrapAndSessionFailClosed(t *testing.T) {
	store := &testStore{state: InstallationUninitialized}
	service := newTestService(store)
	if _, err := service.ConsumeBootstrap(context.Background(), "not-a-token"); !errors.Is(err, ErrBootstrapInvalidOrExpired) {
		t.Fatalf("invalid bootstrap error = %v", err)
	}
	if _, err := service.ReadSession(context.Background(), "not-a-token"); !errors.Is(err, ErrSetupSessionUnauthorized) {
		t.Fatalf("invalid session error = %v", err)
	}
	grant, err := service.GenerateBootstrap(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return grant.ExpiresAt }
	if _, err := service.ConsumeBootstrap(context.Background(), grant.Code); !errors.Is(err, ErrBootstrapInvalidOrExpired) {
		t.Fatalf("expired bootstrap error = %v", err)
	}

	store.state = InstallationReady
	if _, err := service.GenerateBootstrap(context.Background()); !errors.Is(err, ErrSetupUnavailable) {
		t.Fatalf("ready bootstrap error = %v", err)
	}
}

func TestStatusFailsClosed(t *testing.T) {
	storeError := errors.New("storage unavailable")
	store := &testStore{stateErr: storeError}
	if _, err := New(store, store).Status(context.Background()); !errors.Is(err, storeError) {
		t.Fatalf("store error = %v", err)
	}
	store.stateErr = nil
	store.state = "broken"
	if _, err := New(store, store).Status(context.Background()); err == nil {
		t.Fatal("Status accepted an unsupported installation state")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store.state = InstallationReady
	if _, err := New(store, store).Status(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled status error = %v", err)
	}
}
