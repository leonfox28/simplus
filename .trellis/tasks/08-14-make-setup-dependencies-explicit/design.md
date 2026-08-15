# Design: Make Setup dependencies explicit

## 1. Boundary and compatibility decision

`internal/application/setup` remains the owner of the Setup state machine, validation, side-effect ordering and domain-facing configuration. It stops selecting concrete password, filesystem, keyring and certificate implementations. `cmd/simplusd` becomes the production composition owner for those adapters.

This is an internal constructor refactor, not a Setup redesign. The existing OpenAPI, HTTP handlers, Web flow, SQLite stores, encryption labels, paths, password parameters, certificate generation and setup transitions remain unchanged.

The constructor must support two evidenced shapes without hidden discovery:

1. full production/integration assembly, with every persistence and security capability supplied explicitly;
2. explicit reduced test assembly, commonly StateStore-only for unrelated ready-instance HTTP routing tests.

Nil optional fields mean the named capability is unavailable. The constructor never inspects another dependency's dynamic type to discover it.

## 2. Application dependency contract

Replace the positional constructor with an application-owned structure shaped as follows (final field names may be tightened without changing the contract):

```go
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

    Random io.Reader
    Now    func() time.Time
}

func New(Dependencies) (*Service, error)
```

`StateStore` is always required. Optional implementation pairs are all-or-none:

- `AdministratorStore` + `PasswordHasher`;
- `StorageStore` + `DirectoryPreparer`;
- `SecretProtectorOpener` + `LocalCAGenerator`.

The Local CA pair requires `ManagementTLSStore`. Invalid construction wraps one stable `ErrDependenciesInvalid` sentinel with a bounded missing/inconsistent role description. Other optional fields remain independently explicit; their methods retain existing unavailable errors. Add a nil-Administrator guard to `BeginAdministratorSetup` so a deliberately reduced dependency set fails closed rather than dereferencing an absent capability.

`Random` and `Now` are deterministic seams, not lower-layer business capabilities. Nil values select the existing `crypto/rand.Reader` and `time.Now` defaults. Bootstrap/session durations remain internal constants at ten/thirty minutes.

## 3. Application-owned boundary values

Replace lower concrete values with the exact data Setup consumes:

```go
type DirectoryIdentity struct {
    Path   string
    Device uint64
    Inode  uint64
}
type DirectoryPreparer func(string) (DirectoryIdentity, error)

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
```

These shapes retain every currently consumed field and no concrete adapter metadata. Setup continues encrypting the two private keys with the same labels, clearing plaintext key buffers, persisting the same `managementtls.Configuration`, and using the same directory identity range/preflight checks.

## 4. Production composition

Add a focused helper in the `cmd/simplusd` package so `main.go` remains readable while composition remains executable-owned. Its inputs are the concrete SQLite Set and the fixed instance secret-key path. It builds `setup.Dependencies` by:

- assigning the same SQLite Set separately to every persistence role;
- selecting `password.NewDefaultHasher()`;
- wrapping `storagefs.PreparePrivateDirectory` and copying Path/Device/Inode into `setup.DirectoryIdentity`;
- supplying a fixed closure that calls `secretbox.Open(instanceSecretKeyPath)`;
- wrapping `managementcert.GenerateLocalCA` and copying every consumed bundle field into `setup.LocalCABundle`.

`main.go` derives `instanceSecretKeyPath` once from the configured database root, calls the Setup assembly helper and handles its construction error before continuing. The existing general `secretbox.Open` for notification/instance secrets uses exactly the same variable. No environment/request/caller supplies this path.

A command-package test constructs a temporary SQLite Set, invokes the production helper with a synthetic absolute temp path and proves the full dependency set is accepted. Concrete adapter packages retain their own behavior tests; Setup application tests use fakes.

## 5. Caller and test migration

- Setup same-package tests build `Dependencies` with deterministic clock/random, fake password hasher, fake directory identity, fake secret protector and synthetic Local CA bundle. A mutable clock variable replaces later writes to `service.now`.
- `internal/control` bootstrap tests explicitly supply the persistence roles they exercise instead of relying on SQLite dynamic type assertions.
- `internal/api/httpapi` test helpers provide either StateStore-only dependencies for ready/unready gate fixtures or the full dependency set for Setup integration tests. Test-only composition may use temp filesystem/security adapters, but application production code remains concrete-free.
- All constructor errors are handled at the call site; no `MustNew`, panic, service locator or default concrete application constructor is added.

## 6. Validation and rollback

Targeted validation covers Setup, `cmd/simplusd`, control bootstrap and HTTP API under race detection, followed by the supported `./cmd/... ./internal/...` test/vet scope, formatting/lint, dependency scans and task validation. Generated/OpenAPI verification is a no-diff assertion because their sources are not changed.

No real service, host, private data, network, hardware or HIL action is needed. The change has no data migration. Rollback is the constructor/type/caller diff as one unit; do not retain a compatibility constructor that reintroduces hidden concrete defaults.
