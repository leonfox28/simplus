# Application and Hardware Boundaries

## Consumer-Owned Typed Ports

Application services define only the dependencies required by their use case.
For example, `internal/application/messaging/service.go` defines `Repository`,
`LineSource`, `Sender`, `Inbox`, and `SubmitReportInbox`; its
`SendSMSCommand` carries business IDs, destination, body, and encoded segments
but deliberately carries no AT/QMI text or device path. Follow this shape when
adding a transport or repository capability:

- accept `context.Context` on I/O and long-running work;
- use typed commands/results and stable domain IDs;
- validate mandatory dependencies in constructors (`messaging.NewService`,
  `line.New`, and `modem.New` are current examples);
- add the smallest method needed by the current vertical slice.

The HTTP layer follows the same rule. Interfaces such as `Messenger`,
`ManagedLineManager`, and `VoWiFiManager` in
`internal/api/httpapi/server.go` express handler needs and keep concrete
assembly in `cmd/simplusd/main.go`.

## Scenario: Background coordination and realtime invalidation

### 1. Scope / Trigger

Apply this contract whenever an executable starts a polling or long-polling
loop whose result determines notification intent, realtime invalidation,
attention metadata, retry policy, or other business meaning. The executable
owns construction, configured intervals, goroutine lifetime, shutdown and
operational log rendering. The owning application package owns result
interpretation, event/topic selection, side-effect ordering and retry state.

### 2. Signatures

The current SMS coordinator in `internal/application/messaging` consumes:

```go
type InboundSyncer interface {
    SyncInbound(context.Context) (InboundSyncResult, error)
}
type NotificationSender interface {
    Notify(context.Context, string, string) error
}
type RealtimePublisher interface {
    Publish([]realtime.Topic, realtime.Attention)
}

func NewSyncCoordinator(InboundSyncer, NotificationSender, RealtimePublisher) (*SyncCoordinator, error)
func (*SyncCoordinator) Run(context.Context, time.Duration, func(SyncReport))
```

The Agent change coordinator in `internal/application/inventory` consumes a
separate smallest source instead of widening the Snapshot/Probe `AgentClient`:

```go
type AgentChangeSource interface {
    Snapshot(context.Context, bool) (agentapi.Snapshot, error)
    Changes(context.Context, string, uint64, int) (agentapi.ChangeResponse, error)
}
type AgentChangePublisher interface {
    Publish([]realtime.Topic, realtime.Attention)
}

func NewAgentChangeCoordinator(AgentChangeSource, AgentChangePublisher) (*AgentChangeCoordinator, error)
func (*AgentChangeCoordinator) Run(context.Context, func(AgentChangeReport))
```

`SyncReport` carries the typed result, synchronization error, notification
error and durable-change decision. `AgentChangeReport` carries operation
`snapshot | changes` plus the source error. These reports let
`cmd/simplusd` preserve operational logs without importing `slog` into the
application coordinators.

### 3. Contracts

- SMS runs once immediately. Each `SyncInbound` call is bounded to 20 seconds.
  `Persisted`, `AlreadyKnown`, `OutboundSent`, `OutboundFailed` or
  `OutboundUnconfirmed` publishes `messages`; acknowledgement-only counters do
  not. Only `Persisted > 0` attaches `sms.received` attention.
- Newly persisted inbound SMS invokes notification event `sms.received` with
  the bounded summary text, using a 15-second context detached from parent
  cancellation. The coordinator publishes `notifications` after the attempt,
  including when delivery fails. Only the synchronization error selects retry;
  notification failure is report-only for scheduling.
- SMS intervals below one second normalize to two seconds. Synchronization
  failure begins at `max(15s, interval*4)`, doubles to five minutes and resets
  after a successful synchronization. Partial durable results are still
  published/notified before the error selects retry.
- Agent watching starts with `Snapshot(ctx, false)` and calls `Changes` with
  the current instance ID, generation and 25-second wait. Explicit changes and
  instance/generation differences after reconnect publish exactly
  `inventory`, `modems`, and `lines`, without attention.
- Agent retry starts at one second, doubles to 30 seconds and resets after a
  successful change response. Cancellation stops pending retry waits. The
  coordinator publishes no snapshot/device payload through realtime.
- Each coordinator owns its consumer interfaces even though the concrete
  `realtime.Hub` implements both structurally. Do not replace them with a
  generic background framework, catch-all event bus or concrete Hub field.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Any required SMS coordinator dependency is nil | `ErrSyncCoordinatorConfiguration`; do not start the loop |
| Any required Agent coordinator dependency is nil | `ErrAgentChangeCoordinatorConfiguration`; do not start the loop |
| SMS result is partial and synchronization also errors | publish/notify all durable progress, report the error, then use retry delay |
| SMS result contains only acknowledgement counters | no realtime publication or notification |
| Notification delivery fails after persisted SMS | report notification error, still publish `notifications`; a successful sync keeps normal interval |
| Agent initial snapshot fails | report operation `snapshot`, wait with bounded retry, retain prior successful snapshot |
| Agent long-poll fails | report operation `changes`, reconnect with bounded retry |
| Reconnected instance or generation differs | publish Inventory/Modems/Lines once before resuming long-poll |
| Parent context is cancelled | stop pending interval/retry waits promptly and cancel in-flight sync/Agent source contexts; an already-started notification remains detached but bounded by its 15-second deadline |

### 5. Good / Base / Bad Cases

- Good: SMS persists a new inbound record and returns a partial transport
  error; the coordinator publishes Messages with attention, attempts the
  notification, publishes Notifications, reports the error and then backs off.
- Base: Agent long-poll returns unchanged state; update the current snapshot,
  reset retry and continue without publishing.
- Bad: `cmd/simplusd` branches on `InboundSyncResult`, constructs notification
  text, maps Agent changes to topics, or owns exponential-backoff helpers.

### 6. Tests Required

- `messaging/sync_coordinator_test.go`: required dependencies, exact
  sync→Messages→notification→Notifications order, detached/bounded notification
  context, partial progress plus error, acknowledgement-only behavior,
  notification-error retry isolation, retry reset/cap and cancellation.
- `inventory/change_coordinator_test.go`: non-probing snapshot and exact
  long-poll arguments, explicit change, instance restart, generation change,
  unchanged reconnect, typed failure reporting, retry reset/cap and
  cancellation.
- `cmd/simplusd` tests retain executable-only helpers; focused ownership scans
  must find no command-local SMS/Agent topic mapping or backoff functions.
- Run the coordinator packages and `cmd/simplusd` under `go test -race`, then
  run the supported `./cmd/... ./internal/...` test/vet scope.

### 7. Wrong vs Correct

```go
// Wrong: composition root owns business/event policy.
result, err := messages.SyncInbound(ctx)
if result.Persisted > 0 {
    hub.Publish([]realtime.Topic{realtime.TopicMessages}, realtime.AttentionSMSReceived)
}

// Correct: application coordinator owns policy through narrow ports.
coordinator, err := messaging.NewSyncCoordinator(messages, notifications, hub)
if err != nil {
    logger.Error("SMS synchronization coordinator configuration failed", "error", err)
    _ = stores.Close()
    return 1
}
go coordinator.Run(ctx, smsInterval, logSyncReport)
```

## Scenario: Explicit Setup dependencies and concrete adapter assembly

### 1. Scope / Trigger

Apply this contract when changing `internal/application/setup`, any Setup
constructor caller, the instance secret-key path, private-directory validation,
password hashing, or Local CA generation. Setup owns its state machine,
validation and side-effect ordering. The `cmd/simplusd` composition root owns
which concrete SQLite, password, filesystem, secretbox and certificate
implementations satisfy those needs.

Setup is an active OpenAPI/HTTP/Web flow. Do not delete or simplify it as part
of a dependency repair, and do not put concrete defaults back into the
application package for caller convenience.

### 2. Signatures

The application constructor accepts named dependencies and returns a stable
configuration error:

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
    Random                io.Reader
    Now                   func() time.Time
}

func New(Dependencies) (*Service, error)
```

Filesystem and certificate adapters translate into application-owned values:

```go
type DirectoryIdentity struct {
    Path string
    Device, Inode uint64
}
type DirectoryPreparer func(string) (DirectoryIdentity, error)

type LocalCABundle struct {
    CACertificatePEM, CAPrivateKeyPEM       []byte
    LeafCertificatePEM, LeafPrivateKeyPEM   []byte
    RootFingerprint                         string
    LeafNotAfter                            time.Time
    SANs                                    []string
}
type LocalCAGenerator func(time.Time, []string) (LocalCABundle, error)
```

`cmd/simplusd/setup.go:newSetupService(*sqlite.Set, instanceSecretKeyPath)` is
the production assembly seam. `main.go` derives the path argument from its
configured database root; no separate API, environment variable or request
field selects the Setup key path.

### 3. Contracts

- `StateStore` is mandatory. Other nil fields explicitly mean that capability
  is unavailable and existing method-level configuration errors remain
  authoritative. A StateStore-only service is a valid narrow fixture for
  Setup/ready-state gates.
- AdministratorStore/PasswordHasher, StorageStore/DirectoryPreparer, and
  SecretProtectorOpener/LocalCAGenerator are all-or-none pairs. The Local CA
  pair additionally requires ManagementTLSStore. `New` wraps
  `ErrDependenciesInvalid` for every invalid shape; it never discovers roles
  by asserting another dependency's dynamic type.
- Nil `Random` and `Now` select `crypto/rand.Reader` and `time.Now`. Bootstrap
  and session lifetimes remain ten and thirty minutes. Tests inject these
  seams through `Dependencies`, not by assigning private Service fields.
- Production assigns the same SQLite Set separately to every persistence role,
  selects `password.NewDefaultHasher`, translates all three directory identity
  fields, and translates all seven Local CA bundle fields.
- `databaseRoot/.simplus-secrets-key-v1` is derived once in `cmd/simplusd` and
  used by both the lazy Setup SecretProtector opener and the existing instance
  keyring. `setup.Dependencies` carries no key-path field, and production
  exposes no independent key-path selector.
- Local CA mode retains the exact encryption labels
  `management-tls-ca-private-key-v1` and
  `management-tls-leaf-private-key-v1`. After both encryptions succeed, Setup
  clears both plaintext key buffers before calling `ConfigureManagementTLS`
  with the same `managementtls.Configuration` fields. Directory device/inode
  range and completion preflight checks are unchanged.
- This constructor refactor changes no OpenAPI, HTTP error, cookie, generated
  source, SQLite schema/data, cryptographic format, certificate lifetime,
  setup transition or Web behavior.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| StateStore missing | constructor returns `ErrDependenciesInvalid` |
| Exactly one of AdministratorStore / PasswordHasher present | constructor returns `ErrDependenciesInvalid` |
| Exactly one of StorageStore / DirectoryPreparer present | constructor returns `ErrDependenciesInvalid` |
| Exactly one of SecretProtectorOpener / LocalCAGenerator present | constructor returns `ErrDependenciesInvalid` |
| Local CA pair present without ManagementTLSStore | constructor returns `ErrDependenciesInvalid` |
| Optional capability absent and its operation is called | existing bounded “not configured” error; never panic or infer a fallback |
| StateStore-only dependency set | valid Service; Status/gates work, unrelated mutations remain unavailable |
| Production dependency set incomplete | `cmd/simplusd` logs Setup dependency configuration failure, closes stores and exits non-zero |
| LocalCAGenerator returns an error | existing `ErrHTTPSRequestInvalid` mapping; no key/certificate persistence |

### 5. Good / Base / Bad Cases

- Good: `cmd/simplusd` passes explicit roles and adapters, Setup receives only
  application values, and Local CA private keys are encrypted/cleared before
  the unchanged configuration is persisted.
- Base: an HTTP test needs only ready-state gating and constructs
  `setup.New(setup.Dependencies{StateStore: fixedState})`; no SQLite concrete
  capabilities appear implicitly.
- Bad: `setup.New(store, store)` type-asserts the first store into hidden roles,
  imports secretbox/filesystem/managementcert, or assembles a key path inside
  the application package.

### 6. Tests Required

- `internal/application/setup/service_test.go`: missing StateStore, each incomplete
  pair, Local CA without TLS, valid State-only and full dependency shapes;
  deterministic clock/random injection and adapter fakes with no private-field
  assignment.
- Setup behavior tests: exact directory identity persistence/range checks,
  Local CA bundle-to-configuration mapping, exact SecretProtector labels and
  ciphertext mapping, plaintext private-key clearing after both encryptions
  succeed, session lifetimes, hardware review and completion preflight.
- `cmd/simplusd/setup_test.go`: a temporary SQLite Set is accepted by the full
  production assembly and constructing the Service does not eagerly open the
  lazy Setup secret protector.
- Control and HTTP tests: explicit exercised/full dependencies preserve
  bootstrap, session, Setup endpoint and ready-state-gate behavior.
- Focused scans find no concrete security/storage import, StateStore role
  assertion or private Service-field injection in application Setup; run the
  affected packages under `go test -race`, then the supported
  `./cmd/... ./internal/...` test/vet scope.

### 7. Wrong vs Correct

```go
// Wrong: application construction selects hidden implementations by dynamic type.
setupService := setup.New(stores, stores)

// Correct: the executable composition root supplies every production role.
instanceSecretKeyPath := filepath.Join(databaseRoot, ".simplus-secrets-key-v1")
setupService, err := newSetupService(stores, instanceSecretKeyPath)
if err != nil {
    logger.Error("Setup dependency configuration failed", "error", err)
    _ = stores.Close()
    return 1
}
```

## Scenario: Explicit Mihomo supervisor composition

### 1. Scope / Trigger

Apply this contract when changing `RuntimeManager` in
`internal/application/mihomo`, its constructor,
`SIMPLUS_MIHOMO_SUPERVISOR_SOCKET`, or local/socket Mihomo supervisor
selection. The application owns selected-subscription intent, artifact
readiness, persisted running state and restart/rollback semantics. The
`cmd/simplusd` composition root owns which concrete supervisor implementation
satisfies the typed runtime capability.

### 2. Signatures

The application constructor has one explicit, error-returning form:

```go
var ErrRuntimeManagerConfiguration = errors.New(
    "Mihomo runtime manager dependencies are invalid",
)

func NewRuntimeManager(
    root string,
    store RuntimeStore,
    artifacts ArtifactResolver,
    core CoreStatusReader,
    supervisor mihomosupervisor.API,
) (*RuntimeManager, error)
```

The executable owns the concrete selection seam:

```go
func newMihomoSupervisor(
    root string,
    socketPath string,
) (mihomosupervisor.API, error)
```

### 3. Contracts

- `root` is the absolute `filepath.Join(cfg.Storage.DataRoot, "mihomo")`.
  `NewRuntimeManager` requires an absolute root and cleans it before storing it.
- `RuntimeStore`, `ArtifactResolver`, `CoreStatusReader` and
  `mihomosupervisor.API` are all mandatory. Nil and typed-nil dependencies wrap
  `ErrRuntimeManagerConfiguration`; construction never returns a manager that
  can fail later because one of these dependencies is absent.
- Empty `SIMPLUS_MIHOMO_SUPERVISOR_SOCKET` explicitly selects
  `mihomosupervisor.NewLocal(root)` for the existing development/Simulator
  mode. A non-empty value must be an absolute Unix-socket path and selects
  `mihomosupervisor.NewClient(socketPath)`.
- Both concrete constructors only validate/normalize paths and allocate
  in-memory state: selecting local mode does not start Mihomo, and selecting
  socket mode does not dial netd. Supervisor filesystem/process/socket I/O
  starts only when the application invokes `Status`, `Start` or `Stop` on the
  injected typed API.
- `newMihomoSupervisor` translates either concrete-constructor failure into a
  true nil API. `main.go` logs the bounded configuration failure, closes the
  stores and exits with code 2. Application dependency configuration failure
  also closes the stores and exits non-zero (currently code 1).
- Compose production supplies the absolute netd socket and remains
  socket-backed. Empty-socket local mode is not a production fallback.
- Fixed artifact readiness, generated-config checks and restart rollback stay
  in `internal/application/mihomo`; supervisor request-path/process validation
  and concrete process lifecycle stay in `internal/mihomosupervisor`. Do not
  move either behavior into `cmd/simplusd` while fixing composition.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Empty or relative application root | `NewRuntimeManager` returns nil and wraps `ErrRuntimeManagerConfiguration` |
| Any required dependency is nil or typed nil | constructor returns nil and wraps `ErrRuntimeManagerConfiguration` |
| Empty socket plus absolute root | select `*mihomosupervisor.Local`; do not execute a process |
| Empty socket plus relative root | return a true nil API and the local-constructor error; startup stops |
| Absolute socket path | select `*mihomosupervisor.Client`; do not contact the socket during construction |
| Relative socket path | return a true nil API and the client-constructor error; startup stops |
| Valid supervisor but invalid application dependencies | close stores and exit non-zero before HTTP/background assembly |
| Runtime `Start`/`Stop`/`Status`/`Restart` fails | preserve existing typed application/supervisor error mapping and rollback semantics; do not select another implementation |

### 5. Good / Base / Bad Cases

- Good: Compose passes an absolute netd socket; `cmd/simplusd` constructs the
  Unix client, injects it once, and the application knows only the typed API.
- Base: local development leaves the socket empty; the command constructs a
  local supervisor but no child process starts until `RuntimeManager.Start` or
  `RuntimeManager.Restart` issues a typed `Start` request.
- Bad: an application convenience constructor calls `NewLocal`, discards its
  error, infers local mode from a missing dependency, or silently falls back
  from a failed socket client to a local process.

### 6. Tests Required

- `internal/application/mihomo/runtime_test.go` injects a recording fake,
  asserts the selected subscription, binary path, absolute generated-config
  path, pending-restart state and one stop call, and covers empty/relative
  root plus nil and typed-nil mandatory dependencies with `errors.Is`.
- `cmd/simplusd/mihomo_test.go` asserts concrete local/socket selection and
  true-nil error returns for invalid paths. A deliberately missing absolute
  socket proves construction does not dial it.
- `internal/mihomosupervisor` retains the concrete process/client behavior
  tests. Application runtime tests may create synthetic artifact files needed
  by readiness checks, but must not launch a process.
- Focused scans must find one `NewRuntimeManager` constructor, no
  `NewRuntimeManagerWithSupervisor`, no application call to `NewLocal`, and no
  discarded concrete-constructor error. Run focused packages under
  `go test -race`, then the supported `./cmd/... ./internal/...` test/vet scope.

### 7. Wrong vs Correct

```go
// Wrong: the application hides a concrete default and its failure.
func NewRuntimeManager(root string, store RuntimeStore, artifacts ArtifactResolver, core CoreStatusReader) *RuntimeManager {
    local, _ := mihomosupervisor.NewLocal(root)
    return &RuntimeManager{Supervisor: local}
}

// Correct: cmd owns implementation choice and the application requires it.
supervisor, err := newMihomoSupervisor(mihomoRoot, mihomoSupervisorSocket)
if err != nil {
    logger.Error("Mihomo supervisor configuration failed", "error", err)
    _ = stores.Close()
    return 2
}
runtime, err := mihomoapp.NewRuntimeManager(
    mihomoRoot, stores, configManager, coreManager, supervisor,
)
if err != nil {
    logger.Error("Mihomo runtime manager dependency configuration failed", "error", err)
    _ = stores.Close()
    return 1
}
```

## Stable Business Identity and Runtime Resolution

Persisted configuration is not the same as live hardware observation.
`internal/application/line/service.go` stores a random Line ID plus immutable
`ManagedModem + SIM/Profile fingerprint + slot` binding, then its `Topology`
method resolves that business Line against the current inventory. Offline,
missing, changed, or ambiguous identities remain unavailable instead of being
rebound by model or port.

Consequences for new work:

- SMS, calls, egress, and Host VoWiFi consume stable Line IDs; they do not save
  `agent-line-*`, USB topology, sysfs, or `/dev` values.
- A `ManagedModem` survives hot-unplug. Resolution by equipment identity and
  conflict handling live in `internal/application/modem/service.go`, with
  persistence in `internal/storage/sqlite/managed_modems.go`.
- Creating a Line is configuration only. `line.Service.Add` re-reads the
  candidate and persists the binding; it does not change RF, start Mihomo or
  Host VoWiFi, send a message, or place a call. These separations are normative
  in `docs/decisions/0018-persistent-lines-and-runtime-resolution.md` and
  `docs/decisions/0019-line-identity-and-communication-paths.md`.

Do not add a fallback that converts a transient scan result into a business
object or silently switches transports when the selected one is unavailable.

## Line-Owned Phone Number Observations

### 1. Scope / Trigger

Apply this contract whenever a modem or IMS implementation contributes a
current subscriber number, or whenever the authenticated Line response changes
its number observations. Phone numbers are optional current Line observations,
not persisted Line configuration, stable SIM identity, or VoWiFi-owned public
state.

### 2. Signatures

- Model seam: `SubscriberNumberAdapter.ReadSubscriberNumber(context.Context,
  attransport.Query) (string, error)`.
- Agent wire observation: `SIMObservation.subscriberNumber?: string`.
- Normalized hardware observation:
  `SubscriptionProfile.CellularPhoneNumber string`.
- Line-owned optional source: `PhoneNumberSource.CurrentPhoneNumbers(
  context.Context) (map[lineID]e164, error)`.
- Line domain/API: `View.PhoneNumbers []PhoneNumberObservation` and
  `ManagedLine.phoneNumbers: Array<{number, sources}>`, with at most two items
  and source enum `cellular-sim | ims`.

### 3. Contracts

A supported model adapter may add one strictly validated cellular subscriber
number only to the same present, ready, identity-known SIM observation.
`hardware.SubscriptionProfile` and inventory carry that value without changing
the stable SIM fingerprint. The observation is cleared for absent, locked,
inactive, changed, ambiguous, or identity-unknown SIMs.

`internal/application/line` is the sole merger. It combines the current
cellular value with the optional consumer-owned IMS source keyed by stable Line
ID, deduplicates exact E.164 values, keeps source order `cellular-sim`, `ims`,
and sorts observations by number. IMS lookup is best effort and is used only
for `List` and mutation display views; `Topology` for SMS, calls, egress, and
VoWiFi control never consults it. The authenticated
`ManagedLine.phoneNumbers` response is the only public owner; persistence,
ordinary logs, errors, SSE, hardware/setup responses, and public VoWiFi state
exclude phone numbers.

### 4. Validation & Error Matrix

- Empty adapter or IMS result -> omit that source; the Line remains usable.
- Unique explicit `+E.164` value (`^\\+[1-9][0-9]{2,14}$`) -> carry it.
- QDC507 empty `AT+CNUM` result -> unavailable without failing the probe.
- QDC507 duplicate/multiple, non-145, missing `+`, echo, URC, overflow,
  malformed CSV, or non-`OK` transcript -> unavailable without guessing.
- Agent number without ready state and SIM identity fingerprint, or malformed
  number -> reject the probe payload.
- Locked/inactive/missing/mismatched profile -> clear the cellular value.
- IMS source failure, malformed value, duplicate Line ID, or offline worker ->
  omit IMS for that view; do not change `Topology` availability.

### 5. Good / Base / Bad Cases

- Good: cellular and IMS return different valid values; return both, sorted by
  number, each with its own source.
- Base: only one source returns a value; return one observation. If both return
  the same value, return one observation with both ordered sources.
- Bad: persist a prior number, infer one from IMEI/IMSI/ICCID/operator data, or
  expose a model command/source selector through Web/API.

### 6. Tests Required

- Adapter fixtures assert the exact fixed command and all accepted/rejected
  transcript shapes without real identifiers.
- Agent/hardware/inventory tests assert validation, round trip, revision
  participation, and clearing on absent/locked/swapped/unknown identity.
- Line tests assert empty/single/same/different merges, deterministic order,
  IMS best effort, and zero IMS-source calls from `Topology`.
- OpenAPI/HTTP/Web tests assert the bounded schema, removal from public VoWiFi
  state, and rendering of all values on desktop and mobile.
- Privacy/source scans assert no persistence field, real number, raw transcript,
  model branch above the adapter, log/error/SSE exposure, or arbitrary AT seam.

### 7. Wrong vs Correct

Wrong: the Lines page reads `voWiFi.phoneNumber`, or a Line service branches on
`QDC507` and executes `AT+CNUM` itself.

Correct: the model adapter emits an optional typed cellular observation; Line
merges it with optional IMS evidence; Web renders only
`ManagedLine.phoneNumbers` and knows neither modem model nor AT/IMS protocol.

## Model Isolation and Capability Evidence

The dependency direction in `docs/architecture.md` is enforced by current
code:

```text
application intent -> Agent typed capability -> model adapter -> protocol I/O
```

- `internal/modemadapter/registry.go` owns USB match rules, endpoint roles,
  model-specific capability interfaces, and the fail-closed registry. Its
  `Match` returns no adapter when rules overlap;
  `internal/modemadapter/registry_test.go` protects
  that behavior and rejects shared dynamic USB IDs.
- `internal/hardwareprobe/scanner.go` selects an adapter from the registry and
  orchestrates probing. It does not manufacture model commands.
- `internal/attransport/transport.go` owns tty session mechanics. Its `Query`
  is a compiled-in adapter seam, never a Web/API input.
- `internal/application/inventory/agent_source.go` maps only `observed`
  evidence to business capabilities; descriptors or documented/unverified
  features do not become operational support.

Add a new model by implementing and registering the smallest existing
capability interfaces plus tests/evidence. If upper layers need
`if model == ...`, a VID/PID, an interface number, a vendor response, or a
device path, the adapter contract is leaking and must be corrected or recorded
as an explicit design exception.

## Side-Effect Ordering and Uncertain Outcomes

Operations with external effects must make ordering and uncertainty visible.
The messaging service is the representative contract:

- `Service.Send` validates the request and Line, encodes the body, persists a
  queued record, and only then calls `Sender.SendSMS`.
- A per-modem `serialGate` prevents concurrent dispatch through the same modem
  function.
- Operation IDs make an identical retry replayable; conflicting parameters are
  rejected by `internal/storage/sqlite/messages.go`.
- Lost or partial outcomes become `unconfirmed` with stable codes such as
  `SMS_SEND_OUTCOME_UNKNOWN`; they are not blindly resent.
- `internal/application/messaging/service_test.go` proves
  persistence-before-dispatch, replay, conflict, and
  restart behavior against a temporary real SQLite store.

Likewise, `internal/application/calls/service.go` rejects known emergency and
uncertain short numbers before the simulated call transition, and serializes
active-call state. A future real call transport must preserve that ordering;
Simulator success is not hardware evidence.

## Error Contract

Use package sentinel/domain errors for expected decisions and wrap them with
`%w` when adding context. `messaging.ErrLineUnavailable`,
`line.ErrCandidateInvalid`, and `sms.ErrOperationConflict` are examples.
Transport-specific detail is reduced to bounded stable codes before it crosses
the service boundary. Public mapping belongs in `httpapi.Server`, not in the
service or storage package.

Avoid branching on free-form `err.Error()` for new contracts. Existing string
classification in `httpapi.writeMihomoSubscriptionError` is a localized
legacy behavior, not a pattern to copy; prefer typed errors that can be tested
with `errors.Is` or `errors.As`.

## Current Capability Limits

Describe current assembly, not a planned universal platform:

- `cmd/simplusd/main.go` wires calls and eUICC only for the Simulator backend.
- The hardware backend wires the HIL-accepted QDC507 native SMS transport and
  can additionally wire Host VoWiFi SMS through a typed supervisor. Real calls
  and other ordinary cellular SMS transports are not production capabilities.
- `modemadapter.DefaultRegistry` remains SMS-closed, while the production Agent
  explicitly composes the QDC507 transport, durable store, adapter, registry,
  shared operation gate, and resolver. Registry and Agent tests protect both
  sides of that boundary.

Tests or UI fixtures must not be used to advertise a real hardware capability.
Update `docs/compatibility.md` only when the stated evidence level is actually
met.
