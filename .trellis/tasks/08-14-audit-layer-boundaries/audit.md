# Static Layer-Boundary Audit

## Executive conclusion

At commit `7f6e8923420872a7be09280a4cb99dd6d9c4211d`, the principal Web/API → application → typed Agent/supervisor → model adapter → protocol path is intact. No Critical external command/path escape hatch was found, no production code above the hardware boundary constructs AT/QMI/APDU text, and the browser's raw network ownership is confined to its documented runtime owners.

The audit did find five active production responsibility/boundary violations:

| Class | Critical | High | Medium | Low | Total |
| --- | ---: | ---: | ---: | ---: | ---: |
| Confirmed violation | 0 | 1 | 4 | 0 | 5 |
| Architecture concern | 0 | 0 | 0 | 2 | 2 |

- **V-01 / High:** the still-installed native Debian fallback hardcodes one model's VID/PID and writes `option1/new_id` outside the adapter registry, unlike the registry-backed container path.
- **V-02 / Medium:** `cmd/simplusd` implements SMS retry, notification, attention and realtime-topic policy plus Agent-change publication policy instead of only wiring/lifecycle.
- **V-03 / Medium:** `application/setup.New` constructs concrete filesystem, password, certificate and secret-storage implementations inside the application package.
- **V-04 / Medium:** `application/mihomo.NewRuntimeManager` constructs the concrete local process supervisor inside the application package.
- **V-05 / Medium:** the notification application service constructs and drives raw Webhook HTTP directly instead of consuming a typed delivery port.

The two Low concerns are concrete adjacent-layer coupling in `httpapi.Server` and SQLite-owned lease types in the dormant `resourcelease` application package. No repair was made by this audit.

### Remediation closure — 2026-08-18

The findings above remain the historical static-audit result at commit
`7f6e8923420872a7be09280a4cb99dd6d9c4211d`; their source anchors must not be
read as current-line references. All seven findings were subsequently repaired
in separately planned, independently checked child tasks:

| Finding | Closed outcome | Child task | Functional commit |
| --- | --- | --- | --- |
| V-01 / High | Retired the native production deployment and its duplicate VID/PID writer; Docker Compose is the sole production path and driver registration remains registry-backed. | `08-14-retire-native-deployment` | `7013707` |
| V-02 / Medium | Moved SMS synchronization and Agent-change business/event policy into application-owned coordinators; `cmd/simplusd` retains assembly and lifecycle only. | `08-14-move-background-policy-to-application` | `024cbfb` |
| V-03 / Medium | Replaced Setup's hidden filesystem/password/secret/certificate defaults with explicit application-owned dependencies assembled in `cmd/simplusd`. | `08-14-make-setup-dependencies-explicit` | `d66860f` |
| V-04 / Medium | Made the Mihomo supervisor mandatory and injected; `cmd/simplusd` now owns local/client concrete selection and constructor failure handling. | `08-15-inject-mihomo-supervisor-explicitly` | `ffbc69b` |
| V-05 / Medium | Extracted legacy Webhook target/wire/HTTP behavior behind a bounded Notification-owned delivery port and a concrete adapter. | `08-15-extract-notification-delivery-ports` | `04e1770` |
| C-01 / Low | Replaced four concrete HTTP application pointers with exact HTTP-owned consumer ports while preserving typed-nil behavior. | `08-16-narrow-http-application-service-ports` | `aa8b41f` |
| C-02 / Low | Removed the never-assembled `application/resourcelease` package; retained the released runtime migration and SQLite repository/test only as historical storage compatibility fixtures. | `08-17-resolve-resourcelease-storage-coupling` | `1fe33a7` |

Each child task used offline/synthetic validation and an independent Trellis
check. The cumulative closure does not add a new production capability, alter
the released ResourceGroup lease migration, or authorize any deployment,
private-runtime, modem, RF, SMS/call, SIM/eUICC, network-mutation, or HIL
action. The original static-analysis limitations still apply, but no finding
from this report remains open in the reviewed source tree.

Except for this remediation-closure section, the remainder of this document is
the preserved report for baseline `7f6e8923420872a7be09280a4cb99dd6d9c4211d`.
Its present-tense verdicts, source anchors and prioritized-remediation list
describe that historical baseline and are superseded by the closure table
above; they are not assertions that the findings remain current.

Final closure verification rechecked the archived child records and functional
commit ancestry, traced every repaired boundary in the current source, matched
the retained ResourceGroup lease repository, test and runtime-v5 migration to
their baseline SHA-256 values, and passed the supported offline Go tests, vet,
format, generated-drift, documentation and container-contract gates. `make
lint` passed in the repository's local tool/module environment without
external access. An independent stricter `GOPROXY=off` repetition passed its
Go-vet stage but could not make `go run ...@v1.7.12` resolve cached module
metadata; the exact cached Actionlint v1.7.12 binary passed all workflow files.
No service, Compose, external endpoint, private runtime, hardware or HIL action
was used.

## Baseline, scope and safety

### Baseline

- Commit: `7f6e8923420872a7be09280a4cb99dd6d9c4211d`.
- Initial `git status --short`: only the untracked active task directory, `.trellis/tasks/08-14-audit-layer-boundaries/`; no tracked product diff.
- Inventoried source: 163 handwritten production-runtime Go files, 6 handwritten HIL-only command files, 4 generated Go files, 30 handwritten Web runtime files, 19 Web unit/component test files plus 1 E2E spec, 18 generated Web files, 114 Go test files, 4 product-owned runtime C files plus 4 headers and 1 C protocol test, and 18 tracked production container/release/packaging/build-helper files. The production Go count includes the 10 `internal/vowifihil` files compiled into the netd worker; the HIL-only count is limited to the six `cmd/simplus-vowifi-hil-*` mains. The infrastructure count is the prior 16 Docker/Compose/container/release/packaging files plus the two production-invoked `scripts/dev/build-*plugin.sh` helpers.
- `Makefile:22-25` independently distinguishes production commands, HIL commands and generated paths.

### Included

- Handwritten runtime Go under `cmd/**` and `internal/**`, including all current `internal/application/**` packages.
- Handwritten Web runtime under `web/src/**`.
- Public OpenAPI source and the way generated Go/Web contracts are consumed.
- Agent, Mihomo and Host VoWiFi typed Unix protocol definitions, clients, handlers and concrete runtime owners.
- The product-owned `components/strongswan-simplus-simaka/**` C runtime and its production package/build path; upstream strongSwan `p-cscf` source remains a locked third-party input rather than repository-owned runtime source.
- Production `Dockerfile`, `compose.yaml`, `containers/**`, `scripts/release/**`, and packaging ownership.
- Tests and development/HIL tools as separately classified secondary evidence.

### Excluded or treated as output

- Third-party implementation internals, `.tools/**`, ignored caches, `web/dist/**`, generated OpenAPI/sqlc/Web client internals, and ignored `.umi*` output.
- Private runtime data, databases, logs, captures, screenshots, device nodes and live service state.
- Dynamic instrumentation and real deployment/hardware behavior.

### Safety statement

The audit used source inspection, `git` metadata, `rg`, and `go list` with `GOPROXY=off`. It did not start Compose, deploy or prepare a host, discover hardware, access `/dev` or live sysfs, run a HIL helper, send SMS/place calls, mutate RF/SIM/eUICC, create network objects, or inspect private evidence.

## Judgment model

Directory distance was only a candidate signal. A confirmed violation requires an active production path that owns behavior in the wrong layer, bypasses the next typed boundary, or constructs a lower concrete implementation outside a composition root. A concrete type/import with no such runtime bypass is an architecture concern. Explicit composition, bounded platform runtime ownership, generated output and deliberate test assembly are allowed exceptions.

The normative dependency is evidenced by `docs/architecture.md:90-101` and the mechanical invariants at `docs/architecture.md:330-346`. Package ownership and the composition-root rule are at `.trellis/spec/core/backend/directory-structure.md:8-33`; typed application/device boundaries are at `.trellis/spec/core/backend/application-boundaries.md:1-25,135-153`; bounded Unix protocol rules are at `.trellis/spec/core/backend/api-contracts.md:324-371`; hardware safety is at `.trellis/spec/core/infra/hardware-and-hil-safety.md:25-48`. The separately licensed SIM-AKA bridge and package boundary are explicit in `docs/decisions/0020-strongswan-plugin-package.md:16-18,22-46` and `.trellis/spec/core/infra/build-and-generated-files.md`.

## Allowed behavior-edge matrix

| Caller/owner | Allowed next behavior boundary | Proven implementation evidence | Forbidden behavior checked |
| --- | --- | --- | --- |
| Web pages/components | Generated Query/SDK helpers and narrow handwritten API helpers | Generated Query imports throughout pages; sensitive direct SDK read at `web/src/pages/Modems.tsx:140-148` | Page-owned Fetch/EventSource, duplicate response contracts, device paths or commands |
| Browser API runtime | Same-origin authenticated public HTTP | `web/src/api/runtime.ts:27-78` | Unbounded/cross-origin transport and raw error/payload ownership |
| Browser realtime owner | Authenticated SSE followed by query invalidation | `web/src/app/RealtimeBridge.tsx:26-68` | Authoritative records or mutations over SSE |
| `internal/api/httpapi` | Consumer-facing application services and passive domain/OpenAPI mapping | Interfaces at `internal/api/httpapi/server.go:58-161`; handlers call those services | Direct SQLite, Agent/supervisor implementation, shell, process or hardware behavior |
| `internal/application/**` | Consumer-owned repository/service ports, domain vocabulary and typed Agent/supervisor capabilities | Representative messaging ports at `internal/application/messaging/service.go:64-96,125-180` | Concrete persistence/runtime construction, model/path/command selection, silent fallback |
| Persistence/filesystem adapters | SQLite/sqlc or bounded filesystem primitives | `internal/storage/sqlite/store.go:39-98`; `internal/storage/filesystem/private_directory.go:18-59` | Upward HTTP/application calls or device/protocol semantics |
| Agent/supervisor clients and handlers | Fixed bounded routes and typed requests/results | Agent constructors at `internal/agentapi/server.go:46-63`; VoWiFi request surface at `internal/vowifisupervisor/types.go:49-57,173-186` | Arbitrary shell, command, path, SIP/RP/APDU or network payload inputs |
| Hardware discovery/runtime | Registry-selected capability interfaces | `internal/hardwareprobe/scanner.go:61-111`; `internal/hardwareprobe/at_runtime.go:25-56` | Manufacturing model commands or exposing targets to business control |
| Model adapters/business drivers | Bounded protocol transport | Capability seams at `internal/modemadapter/registry.go:30-110`; QDC507/ML307A implementations | Upper-layer model branching or generic external command input |
| Generic AT/TTY transport | OS/device I/O only | `internal/attransport/transport.go:18-29`; Linux implementation in `session_linux.go` | Choosing commands, models or business behavior |
| netd workers | Fixed process/network/IMS primitives derived from typed stable inputs | `cmd/simplus-netd/main.go:90-147`; `internal/vowifisupervisor/network.go`, `worker.go`; `internal/vowifihil/config.go`, `preflight.go`, `vici.go`, `xfrm.go` | Public arbitrary routes, interfaces, commands, paths or SIP payloads |
| strongSwan SIM-AKA C bridge | One fixed Agent SIM-AKA Unix request plus fixed IMS APN IDr hook | `components/strongswan-simplus-simaka/simplus_simaka_card.c:63-120`; `simplus_simaka_agent.c:348-517`; `simplus_simaka_apn.c:18-54` | AT/APDU/model selection, arbitrary command/route/payload, or caller-selected production socket |
| `cmd/**` | Flag parsing, dependency construction, process lifecycle and executable-only fixed helpers | Main object graphs in the three production binaries | Business/event policy or hidden lower implementation construction |
| Container/release runtime | Fixed image/service privilege boundary and reviewed entrypoints | `compose.yaml:20-178`; container entrypoints | Registry bypass, broad capabilities, host network, arbitrary hardware/network control |

## Coverage matrix

“Candidates” counts traced candidate clusters, not raw lexical matches. Every row has an explicit verdict even when clean.

| Runtime owner | Inspected source/package set | Actual outbound behavior | Candidates | Verdict |
| --- | --- | --- | ---: | --- |
| Web pages/components/features | `web/src/pages`, `components`, `calls`, `messages`, `mihomo` | Generated Query/SDK and local presentation only | 2 | Clean; AE-01/FP-03 |
| Web API runtime | `web/src/api` excluding generated | Sole Fetch, validation, error normalization and query config | 1 | Clean; AE-02 |
| Web bootstrap/app/realtime | `web/src/main.tsx`, `web/src/app` | React bootstrap, generated auth SDK + one EventSource owner + cache invalidation | 1 | Clean; AE-02 |
| Public HTTP/OpenAPI mapper | `internal/api/httpapi`, `api/openapi.yaml` | Application calls, auth/middleware and type mapping | 2 | No lower-layer bypass; C-01 |
| `application/auth` | package | Store/verifier ports | 0 | Clean |
| `application/calls` | package | Repository + Line topology port; Simulator state machine | 1 | Clean; peer service port only |
| `application/contacts` | package | Repository port | 0 | Clean |
| `application/euicc` | package | Repository port; Simulator-only composition | 0 | Clean |
| `application/health` | package | Store probe + passive build info | 0 | Clean |
| `application/inventory` | package | Typed Agent snapshot/probe to normalized hardware topology | 2 | Clean; AE-03/FP-04 |
| `application/line` | package | Repository, inventory/modem records, optional typed VoWiFi number source | 2 | Clean; stable-ID resolution remains here |
| `application/lineegress` | package | Store, inventory and Mihomo status ports | 2 | Clean; peer application DTO coupling is interface-bound |
| `application/messaging` | package | Repository, Line source, typed native/VoWiFi SMS transports | 3 | Clean; unique eligible transport, no fallback (`service.go:583-603`) |
| `application/mihomo` | package | Artifact download/config validation and typed/local supervisor | 3 | V-04; artifact work otherwise AE-04 |
| `application/modem` | package | Repository, inventory and narrow typed Agent RF/identity adapters | 2 | Clean; no model/path branch |
| `application/notification` | package | Repository/secrets and bounded Webhook/Feishu provider HTTP | 2 | V-05; Feishu typed-port path is clean |
| `application/realtime` | package | In-memory bounded topic hub | 0 | Clean |
| `application/resourcelease` | package | Dormant topology/repository service | 1 | C-02; no production caller |
| `application/setup` | package | Setup stores plus directory/crypto/certificate behavior | 1 | V-03 |
| `application/vowifi` | package | Store, inventory, egress, Mihomo and typed supervisor ports | 3 | Clean; no RF/model/path decision and no direct-egress fallback |
| Passive domain | `internal/domain/**` | Validation and records only | 2 | Clean; FP-02/FP-03 |
| SQLite persistence | `internal/storage/sqlite` excluding generated | SQLite/migrations/domain conversion | 1 | Clean; no upward import |
| Filesystem adapter | `internal/storage/filesystem` | Private directory validation/preparation | 1 | Clean as adapter; its hidden construction is V-03 |
| Agent typed protocol | `internal/agentapi` | Fixed HTTP-over-Unix operations, peer checks, bounded validation | 3 | Clean active surface; AE-05/FP-04 |
| Mihomo supervisor | `internal/mihomosupervisor` | Fixed status/start/stop; constrained artifact paths; process owner | 2 | Clean; AE-04/AE-06 |
| Host VoWiFi supervisor and IMS runtime | `internal/vowifisupervisor`, `internal/vowifihil` | Typed Line/SMS operations; fixed Agent SIM-auth, process/network/XFRM/VICI/SIP worker | 5 | Clean production path; AE-06. Historical model-specific HIL wrapper is tool-only |
| strongSwan SIM-AKA bridge | `components/strongswan-simplus-simaka` production `.c`/headers | Fixed typed Agent challenge over AF_UNIX and fixed `vowifi-ims` APN hook | 2 | Clean; AE-09 |
| Hardware discovery | `internal/hardwareprobe` | sysfs/device discovery, adapter selection, typed orchestration | 2 | Clean; AE-05 |
| Model adapters/drivers | `internal/modemadapter/**` | Model match, fixed commands/parsers, QDC507 SMS recovery | 2 | Clean; AT/APDU/vendor strings confined here |
| Generic AT transport | `internal/attransport` | Bounded tty lifecycle/framing | 1 | Clean; no command constants |
| Supporting runtime | `internal/config`, `control`, `security`, `smscodec`, `command`, `buildinfo` | Typed config/control, crypto, codec and metadata | 2 | Clean; no hardware/model bypass |
| `cmd/simplusd` | `main.go`, `web.go` | Composition, HTTP/control lifecycle, static Web; background policies | 3 | V-02; other composition allowed |
| `cmd/simplus-agent` | `main.go` | Agent composition and fixed registry-backed driver registration | 2 | Clean; AE-05 |
| `cmd/simplus-netd` | `main.go` | Supervisor composition and private worker entry | 2 | Clean; AE-06 |
| `cmd/simplusctl` | package | Typed public/control/Agent/supervisor clients | 2 | Clean; fixed inspection/control CLI, no generic device command |
| Compose/container runtime | `Dockerfile`, `compose.yaml`, `containers/**` | Three-image privilege split, fixed entrypoints, netd preflight | 3 | Clean; AE-07 |
| Native release fallback | `scripts/release/**` | Host installation and systemd lifecycle | 2 | V-01; other host mutation remains installer-owned |
| strongSwan plugin packaging | `packaging/strongswan-plugins/**`, `scripts/dev/build-{simplus-simaka,strongswan-p-cscf}-plugin.sh` | Locked Debian inputs; builds/packages product SIM-AKA bridge and upstream `p-cscf` separately | 2 | Clean build boundary; AE-09 |
| Generated boundaries | OpenAPI Go, sqlc Go, Web generated SDK | Generated contract/persistence output | 1 | Excluded internals; source/consumption checked, AE-08 |

## Confirmed violations

### V-01 — Native production fallback bypasses the model registry for a hardware driver write

- **Class/severity:** Confirmed violation / High.
- **Caller layer → target layer:** Native release/systemd packaging → kernel USB serial driver hardware boundary.
- **Anchors:**
  - `scripts/release/bind-ml307a.sh:4-15`
  - `scripts/release/build-debian-bundle.sh:21`
  - `scripts/release/install-debian.sh:6,61,81-118`
  - Correct comparison path: `containers/agent-entrypoint.sh:33`, `cmd/simplus-agent/main.go:73-75,290-345`, `internal/modemadapter/registry.go:30-43,181-207,245-251`
- **Call chain:** native bundle copies `bind-ml307a.sh` as `helpers/bind-ml307a` → installer installs it under `/usr/local/libexec/simplus` and makes `simplus-agent.service` require a oneshot bind service → helper loads `option`, selects the literal model ID pair and writes it directly to the fixed `new_id` sysfs attribute. In contrast, container Agent entrypoint calls `simplus-agent register-option-driver` → `DefaultRegistry().USBSerialIDs()` → validated, unique adapter-owned IDs → fixed writer.
- **Violated rule:** dynamic USB IDs and model selection belong to `modemadapter` registry/runtime hardware ownership; a production release path must not maintain a second model registry. See `docs/architecture.md:80-82,90-101` and `.trellis/spec/core/infra/containers-and-privileges.md:27-38`.
- **Impact:** native fallback behavior can drift from the reviewed registry when models or IDs change, can bind a no-longer-supported ID, and cannot bind a newly supported ID. The helper also treats every failed write at `bind-ml307a.sh:13-15` as success, not only the documented already-registered case, so systemd can continue after an unclassified hardware-boundary failure.
- **Minimum remediation:** remove the duplicated helper or have the native oneshot invoke a fixed `simplus-agent` subcommand that obtains IDs exclusively from the registry. If native and container paths need different fixed sysfs mount points, select only between compiled-in fixed targets inside the hardware binary; do not accept a caller-provided path or VID/PID.
- **Safe validation:** add/extend non-HIL unit/contract tests proving both native bundle and container entrypoints reach the registry-backed command, all registry IDs are registered once, invalid/duplicate IDs fail closed, and a write error other than already-exists returns non-zero. Shell syntax and package tests are sufficient; no device is needed.

### V-02 — `cmd/simplusd` owns background business, notification and realtime publication policy

- **Class/severity:** Confirmed violation / Medium.
- **Caller layer → target layer:** Composition root → application messaging/notification/realtime and Agent event semantics.
- **Anchors:** `cmd/simplusd/main.go:281-283,394-463,470-505,533-553`; tests at `cmd/simplusd/main_test.go:53-99` demonstrate that the policy is intentionally owned by `main`.
- **Call chains:**
  1. `run:281` → `runSMSSync` → `messaging.Service.SyncInbound` → inspect persisted/already-known/report counters → decide Message topic and `sms.received` attention → synthesize notification content → call notification service → publish Notification topic → choose retry/backoff policy.
  2. `run:283` → `runAgentChanges` → Agent snapshot/change loop → compare Agent instance/generation → select Inventory/Modems/Lines topic set → choose retry/backoff policy.
- **Violated rule:** `cmd/**` owns dependency construction and lifecycle; business/event decisions belong to application/domain packages (`.trellis/spec/core/backend/directory-structure.md:22-33`). Polling goroutine lifetime is legitimate executable lifecycle, but deciding durable-result meaning, notification event, attention and topic mapping is not mere wiring.
- **Impact:** changes to SMS durability semantics, notification behavior or resource invalidation require editing the executable root and coupling concrete application services there. The policy cannot be reused or tested at its owning application boundary, and future binaries can silently diverge.
- **Minimum remediation:** move SMS synchronization policy into a narrow application coordinator with `Syncer`, `Notifier` and `Publisher` ports; move Agent-change-to-topic mapping into an inventory/realtime coordinator. Leave only construction, `go coordinator.Run(ctx)`, intervals/config and shutdown in `cmd/simplusd`.
- **Safe validation:** migrate current deterministic backoff/publication tests to the new application package; add fake-port tests for partial progress, attention, notification failure, generation/instance changes and cancellation. No network or hardware is required.

### V-03 — Setup application constructor silently assembles concrete lower-layer adapters

- **Class/severity:** Confirmed violation / Medium.
- **Caller layer → target layer:** Application setup → concrete filesystem and security implementations.
- **Anchors:**
  - Imports and port type: `internal/application/setup/service.go:23-27,58-97`
  - Hidden construction: `internal/application/setup/service.go:218-248`
  - Runtime use: `internal/application/setup/service.go:503-547,586-605,711-717`
  - Active assembly caller: `cmd/simplusd/main.go:69-77`
- **Call chain:** `cmd/simplusd` constructs SQLite set and calls `setup.New(stores, stores)` → `setup.New` type-asserts the same object into optional roles → creates the default password hasher, binds `storagefs.PreparePrivateDirectory`, creates a secretbox opener using a concrete filesystem path, and binds the concrete local CA generator → later setup mutations perform filesystem probing, key opening/encryption and certificate generation through those hidden defaults.
- **Violated rule:** lower implementations are selected in the composition root; application packages consume explicit narrow ports. `DirectoryPreparer` also returns the storage-owned `storagefs.DirectoryIdentity`, preserving concrete adapter vocabulary at the application seam.
- **Impact:** filesystem/crypto behavior and key-path policy are implicit consequences of the store's dynamic type. The application constructor both discovers capabilities and chooses implementations, making alternate deployment/tests and failure review depend on hidden wiring.
- **Minimum remediation:** define application-owned input/result types and require an explicit dependency/options object. Construct `storagefs`, password, secretbox and certificate implementations in `cmd/simplusd`, then inject them. Keep optional feature availability explicit rather than inferred from type assertions on one catch-all store.
- **Safe validation:** preserve current setup tests using injected fakes and add a composition test that all production dependencies are present. Run setup/httpapi tests against temporary storage only; no HIL.

### V-04 — Mihomo application constructor creates the concrete local process supervisor

- **Class/severity:** Confirmed violation / Medium.
- **Caller layer → target layer:** Application Mihomo runtime → concrete supervisor/process implementation.
- **Anchors:**
  - `internal/application/mihomo/runtime.go:37-53,92-111,162-188`
  - `cmd/simplusd/main.go:236-247`
  - Concrete process implementation: `internal/mihomosupervisor/local.go:29-105`
- **Call chain:** when no supervisor socket is configured, `cmd/simplusd` calls `mihomo.NewRuntimeManager` → the application constructor calls `mihomosupervisor.NewLocal(root)` (and discards its error) → later `RuntimeManager.Start` invokes its `mihomosupervisor.API` field → local supervisor validates paths and starts the process. The socket-backed branch correctly constructs a client in `cmd/simplusd` and injects it through `NewRuntimeManagerWithSupervisor`.
- **Violated rule:** application may call a typed supervisor port, but concrete local/client implementation choice belongs to the composition root. This is the same boundary regardless of whether the selected runtime is an unprivileged Simulator/development fallback.
- **Impact:** an environment/runtime implementation decision is hidden inside business construction and the no-socket path has different assembly semantics from the socket path. Ignoring the local-constructor error also weakens explicit startup failure handling.
- **Minimum remediation:** remove or narrow `NewRuntimeManager`; construct `mihomosupervisor.NewLocal` in `cmd/simplusd` for the explicitly supported local mode and inject it through the single constructor that requires a non-nil API. Keep actual process policy in `internal/mihomosupervisor`.
- **Safe validation:** composition tests for local and socket-backed modes plus existing supervisor fake tests; assert constructor failures stop startup. No process needs to be launched in the test.

### V-05 — Notification application service bypasses a typed provider port for Webhook delivery

- **Class/severity:** Confirmed violation / Medium.
- **Caller layer → target layer:** Application notification use case → raw external HTTP transport/provider protocol.
- **Anchors:**
  - Concrete field/default: `internal/application/notification/service.go:48-62`
  - Active delivery: `internal/application/notification/service.go:203-263`
  - Typed contrast: `internal/application/notification/feishu.go:60-80`, `binding.go:58-66`
  - Active assembly: `cmd/simplusd/main.go:213-218`
- **Call chain:** `cmd/simplusd` calls `notification.New(stores, secretKeyring)` → `New` constructs a concrete redirect-blocking `*http.Client` → `Service.Notify/Test` selects a stored Webhook channel → `deliverWebhook` decrypts provider material, constructs provider-specific JSON/signature and an HTTP request, calls `Client.Do`, interprets the provider response, and persists delivery state. The Feishu flow instead demonstrates the intended boundary: application state machine consumes injected `FeishuRegistrar` and `FeishuMessenger` ports, while `FeishuClient` is the concrete adapter assembled in `cmd/simplusd`.
- **Violated rule:** an application use case should express delivery intent through its smallest consumer-owned typed port and must not construct its lower transport implementation. This active path combines channel business state, provider wire format, HTTP transport and persistence outcome in one service.
- **Impact:** Webhook provider/HTTP behavior cannot be replaced independently from notification business logic; retries/failure mapping/provider parsing are coupled to the store-owning service, and adding another delivery implementation encourages more transport branches in the use case.
- **Minimum remediation:** introduce an application-owned typed Webhook delivery port carrying bounded provider/destination/secret/message inputs and a typed result. Move request/signature/response handling into a concrete adapter selected in `cmd/simplusd`; keep delivery-state transitions in the service. Do not generalize it into arbitrary HTTP.
- **Safe validation:** retain current synthetic `httptest` cases at the adapter boundary and add service tests with a fake typed deliverer for success, rejection and network failure. No external endpoint is required.

## Architecture concerns

### C-01 — HTTP boundary still stores four adjacent application services as concrete pointers

- **Class/severity:** Architecture concern / Low.
- **Anchors:** `internal/api/httpapi/server.go:163-185,254-288`; concrete method calls include `:436,669-926,1226,2844,3031`.
- **Call chain:** `cmd/simplusd:285-304` injects health/setup/inventory/realtime implementations → `httpapi.Server` stores `*health.Service`, `*setup.Service`, `*inventory.Service` and `*realtime.Hub` directly, while its other dependencies use consumer-owned interfaces.
- **Rule/impact:** this does not bypass application behavior or touch storage/hardware directly, but it is inconsistent with the consumer-owned port rule and makes the HTTP package compile against complete concrete services.
- **Minimum remediation:** add narrow HTTP-owned interfaces for the methods actually used, including a realtime subscription/publish-facing view as appropriate. Do not create a shared catch-all interface.
- **Safe validation:** compile-time fake implementations plus existing `httptest` router tests.

### C-02 — Dormant resource-lease application contract exports SQLite-owned types

- **Class/severity:** Architecture concern / Low.
- **Anchors:** `internal/application/resourcelease/service.go:12-15,42-51,69-152`.
- **Call chain:** application service defines a `Repository`, but its signatures, request validation, return values and constants use `storage/sqlite.ResourceLease*` directly. Repository-wide search found no non-test caller of `resourcelease`; current production binaries do not activate it.
- **Rule/impact:** this is concrete persistence vocabulary in an application contract, but absence of an active call chain prevents a confirmed runtime-bypass verdict. Future activation would make storage representation the application source of truth.
- **Minimum remediation:** before reactivation, move lease command/record/kind types to `application/resourcelease` or a protocol-neutral domain package and map them in SQLite. If the documented simplification makes the package obsolete, remove it in a separately approved task.
- **Safe validation:** package tests with an interface fake and SQLite adapter mapping tests; no HIL.

## Allowed exceptions and traced clean candidates

### AE-01 — Web sensitive read uses the generated boundary without caching

`web/src/pages/Modems.tsx:140-148` directly calls the generated equipment-identity SDK rather than a generated mutation helper. It does not call Fetch, build a path string or define a payload; generated validation/transport remains authoritative and the sensitive value stays in controlled component state. This is the documented no-cache behavior, not a layer bypass.

### AE-02 — Raw browser network primitives have exactly two owners

A production-only scan excluding generated/tests found only `globalThis.fetch` at `web/src/api/runtime.ts:55` and `new EventSource` at `web/src/app/RealtimeBridge.tsx:48`. Pages/components contain neither. Realtime events are decoded by `web/src/api/events.ts:20-34` and only invalidate active HTTP queries.

### AE-03 — Application typed Agent/supervisor adapters are narrow and fail closed

`inventory.AgentSource`, modem RF/identity adapters, SMS gateways, Line's VoWiFi phone source and the VoWiFi service import typed protocol packages, but callers pass stable IDs/typed intents. Messaging chooses exactly one eligible transport and returns ambiguity/unavailability (`internal/application/messaging/service.go:583-603`); it does not fall back between native and Host VoWiFi. VoWiFi constructs only `LineID`, opaque current hardware Line ID, typed egress mode and country (`internal/application/vowifi/service.go:319-337`).

Peer-application imports (`inventory.Topology`, Mihomo status and Line-egress views) are used behind consumer-defined interfaces. They are compile-time coupling but do not select a lower implementation or bypass behavior, so they are not hard findings in this audit.

### AE-04 — Mihomo artifact HTTP/filesystem/exec operations and bounded supervisor paths are explicitly owned

`internal/application/mihomo/core.go:39-61,79-248` downloads, verifies, probes with fixed `-v`, and atomically installs an official candidate. `config.go:58-70,82-147,313-319` owns immutable artifact creation and validation with fixed `-t/-f/-d`. `subscriptions.go:64-77,287-339,520-547` owns the bounded, SSRF-filtered subscription fetch. ADR 0005 decision 3 explicitly defines official-release download, staged verification, fixed version probing and atomic activation as the Mihomo-management use case; ADR 0007 decisions 1-5 likewise make immutable artifact construction, current-core validation and publication part of subscription state transitions. On that accepted, narrow contract, these managers co-locate the concrete artifact adapter with its application owner. Inputs are constrained official release metadata or validated subscription URLs, not caller-selected executables, arguments or artifact paths, and long-running process ownership remains the supervisor. This is an explicit current architecture exception, not a general precedent for unrelated application services. It differs from V-04 because `mihomosupervisor.Local` is a separately owned runtime/process implementation whose selection is not part of those artifact decisions and should occur in the composition root.

The internal Mihomo `StartRequest` contains binary/config paths (`internal/mihomosupervisor/types.go:16-20`), but `local.go:156-177` accepts only exact installed-version and immutable generated-config shapes under the fixed root, with no arguments/shell input. This is the explicit ADR 0008 exception, not an arbitrary path surface. V-04 concerns where the concrete supervisor is constructed, not these fixed operations.

### AE-05 — Hardware discovery, registry and transport divide responsibilities correctly

- `hardwareprobe.Scanner` reads sysfs and builds endpoint observations (`scanner.go:68-111,294-365`).
- The registry owns model matches, endpoint roles and unique dynamic USB IDs (`modemadapter/registry.go:160-251`).
- `hardwareprobe.atRuntime` opens a registry-selected endpoint and delegates a `Query`; it obtains the command plan/capabilities from the adapter (`at_runtime.go:25-84`).
- `attransport.Query` is a compiled-in adapter seam (`attransport/transport.go:18-29`); production command constants were found in `modemadapter/**`/model-owned drivers, not generic transport or upper control layers.
- `cmd/simplus-agent`'s fixed `register-option-driver` executable helper derives every ID from the registry and rejects any target other than the compiled-in container mount (`main.go:290-345`). This executable-only hardware-runtime helper is allowed; V-01 is the separate shell path that bypasses it.

### AE-06 — netd owns process/network/IMS behavior behind bounded protocols

Mihomo and VoWiFi handlers expose fixed routes with bounded JSON (`internal/mihomosupervisor/http.go`, `internal/vowifisupervisor/http.go`). VoWiFi public supervisor start accepts only Line ID, opaque hardware Line ID, egress enum and country (`types.go:49-57`); executable paths, runtime directories, link addresses, namespace/interface names, routing and SIP material are derived inside netd/private worker composition.

Despite its historical name, `internal/vowifihil` is partly a production runtime owner: `internal/vowifisupervisor/worker.go:21,85-175` imports it for the active netd worker. The production path calls model-independent `InspectHostVoWiFiLine` (`internal/vowifihil/preflight.go:59-143`), builds a fixed private strongSwan configuration whose Agent sockets and plugin set are constants (`config.go:16-25,83-172`), loads a fixed VICI connection (`vici.go:13-23,65-95,145-184`), and owns bounded SIP/XFRM behavior (`session.go`, `sms_session.go`, `xfrm.go:35-47,156-265`). The model-specific `InspectML307AVOXI` wrapper at `preflight.go:21-57` is called by historical HIL tools, not the production worker. Process/network hits in these packages, `cmd/simplus-netd`, and `containers/netd-preflight.sh` therefore match the documented netd owner.

### AE-07 — Compose/container privilege and shell boundaries are fixed

`compose.yaml:20-178` keeps app, Agent and netd separated; Agent has no network and only the reviewed device/sysfs mounts, app has no capabilities, and netd alone receives the network capability set on a bridge. `containers/agent-entrypoint.sh:9-59` validates mounts/GID, calls the registry-backed driver command and drops to UID 10002. `containers/netd-entrypoint.sh:9-24` uses fixed arguments; `netd-preflight.sh:24-64` creates PID-named disposable fixed test objects and cleans them. No shell `eval`, caller-supplied command or host-network path was found.

### AE-08 — Storage, generated output and integration-test assembly

SQLite/filesystem packages import domain/OS primitives but no HTTP/application behavior. Generated ownership is registered at `Makefile:25,67-92`. Tests in messaging/calls/notification/mihomo/eUICC/httpapi intentionally assemble temporary real SQLite; these are integration-test dependencies, not production imports.

### AE-09 — Product-owned strongSwan C bridge is a fixed typed Agent client

`components/strongswan-simplus-simaka` is production source, not third-party or HIL-only output. Its `simaka_card_t` implementation accepts only a 3G AKA quintuplet call and delegates to `simplus_simaka_agent_authenticate` (`simplus_simaka_card.c:63-120`). The client validates the generated target fence, sends one fixed `POST /v1/sim/aka/authenticate` with bounded RAND/AUTN and correlation fields, uses bounded AF_UNIX I/O, validates the typed result, and clears sensitive buffers (`simplus_simaka_agent.c:115-128,256-345,348-517`). The additional listener only inserts fixed `fqdn:ims` for the first `vowifi-ims` IKE_AUTH request (`simplus_simaka_apn.c:18-54`). It contains no AT/APDU text, model selector, generic Agent route or shell execution.

The plugin reads its socket/fence settings at `simplus_simaka_plugin.c:89-151`, but the production generator fixes the Agent socket to `/run/simplus-agent/sim-aka.sock` and derives the other private values from the typed Agent inspection (`internal/vowifihil/config.go:16-25,83-172`); no Web, supervisor request or operator config supplies that path. ADR 0020 decisions 1 and 7 explicitly retain this thin external-Go-Agent architecture. `packaging/strongswan-plugins/build-deb.sh:218-277,317-333` builds the product plugin and separately builds upstream `p-cscf` from locked Debian source; the upstream implementation is not copied into or claimed as product-owned source.

## Test, tool and generated-only observations

### TO-01 — HIL-only process/network/protocol code

The six `cmd/simplus-vowifi-hil-*` mains and `scripts/dev/run-ml307a-vowifi-hil.sh` are documented HIL tools and were not executed. Two historical entry paths, `cmd/simplus-vowifi-hil-ims/main.go:71` and `cmd/simplus-vowifi-hil-prepare/main.go:26`, call `internal/vowifihil.InspectML307AVOXI`; the other HIL mains have their own narrower helper roles. The rest of that shared package is also used by the production netd worker and is covered under AE-06; it is not excluded wholesale. The HIL entry points remain non-product evidence and do not authorize live execution.

### TO-02 — Synthetic AT/device fixtures

AT strings and `/dev/fixture-*` paths occur extensively in adapter/transport/hardwareprobe tests. They call injected functions or synthetic sessions and prove fixed commands/parsers. No test helper was found wired into a production Web/API or Unix route as a generic command runner.

### TO-03 — Dormant fixed radio ledger

`internal/agentapi/command_service.go` and `outcome_store.go` retain the fixed `radio.ensure-off` ledger. `cmd/simplus-agent` constructs `NewManagedHardwareHandler` without a `CommandService` (`cmd/simplus-agent/main.go:197-208`), and no production caller constructs `NewCommandService` or `OpenOutcomeStore`. This matches `docs/architecture.md:258`; it is dormant historical infrastructure, not an active generic command route.

Generated OpenAPI/sqlc/Web files were not judged as handwritten owners. Their sources, generated registry and callers were checked. Ignored `.umi*` and `web/dist` trees are build output, not runtime source owners.

## False-positive rationale for high-risk scans

| Candidate | Why it is not a finding |
| --- | --- |
| `AT/QMI` in `internal/application/messaging/service.go:125-127` | A prohibition comment; command text is not constructed or passed. |
| `QMI/APDU`, VID/PID and interface vocabulary in `internal/domain/hardware/**` | Passive normalized capability/validation vocabulary. No I/O or model command selection exists in domain imports. |
| Relative USB address and VID:PID in OpenAPI/Web | Explicitly permitted read-only candidate metadata (`docs/architecture.md:74,97`); mutation sends only opaque `candidateId`. Absolute sysfs and device nodes are rejected/omitted. |
| Agent `Endpoint.Node`, interface number and probe endpoint observations | Output-only internal Agent discovery evidence. `application/inventory/agent_source.go:52-170` maps capabilities and relative display metadata and does not use endpoint nodes/interfaces for business behavior; no public OpenAPI mapper exposes them. |
| `os/exec`, filesystem and HTTP in Mihomo application | Fixed official artifact probe/config-validation policy assigned by ADRs; arbitrary binaries/args are not accepted, and runtime process ownership is supervisor-bound. V-04 separately records the concrete-construction leak. |
| Network/process/sysfs keywords in netd/container entrypoints | Located in the designated Agent/netd runtime owners and use fixed, validated inputs. The native duplicated ID writer is the sole confirmed exception (V-01). |
| AF_UNIX, socket path and RAND/AUTN in the product C plugin | The path is fixed by the private netd-generated configuration, the route and challenge shape are compiled in, and responses are fenced/correlated. No public or supervisor request supplies a command, path, model, AT or APDU payload (AE-09). |
| Non-adjacent imports from `cmd/**` | Composition roots necessarily construct multiple layers. Only behavior beyond construction/lifecycle is V-02. |

## Explicit clean-layer assertions

- No `internal/api/httpapi` production import of storage, Agent, hardwareprobe, modemadapter, attransport or supervisor packages was found.
- No lower owner under domain, storage, Agent, hardwareprobe, modemadapter, attransport or the supervisor packages imports upward into `internal/api/httpapi` or `internal/application`. This assertion is about lower-to-upper dependency direction; reverse application imports are separately classified as typed-client use (AE-03), confirmed construction leaks (V-03/V-04), or the dormant SQLite-type concern (C-02).
- Public OpenAPI contains no command, AT/QMI, device-path, interface-number, shell-command, network-command or vendor-payload request field. RF is a boolean typed operation; Modem/Line mutations use stable IDs.
- Raw Fetch/EventSource ownership is exactly the two documented Web owners; pages do not construct either.
- Production AT/APDU command strings are model-adapter/model-driver owned. Generic AT transport has no model command constants.
- Discovery selects adapter/endpoints but obtains command plans from capability interfaces; generic transport owns framing/I/O only.
- Storage/filesystem adapters do not call upward into application/HTTP or own modem/network protocol behavior.
- Active typed Agent/Mihomo/VoWiFi handlers expose fixed operations and bounded bodies; no generic command runner route exists.
- The production strongSwan plugin emits only the fixed, fenced SIM-AKA request and fixed IMS APN hook; it exposes no AT/APDU/model/command surface and does not obtain its socket path from public or supervisor input.
- Hardware-backend assembly does not silently fall back to Simulator; messaging rejects zero, multiple or unavailable transports.

## Prioritized remediation

1. **P0 / V-01:** eliminate the native duplicate VID/PID writer and route every production driver binding through the adapter registry; preserve fail-closed write errors.
2. **P1 / V-02:** move background SMS/Agent event semantics out of `cmd/simplusd` into application coordinators.
3. **P1 / V-03:** make setup's filesystem/crypto/certificate dependencies explicit composition-root inputs.
4. **P1 / V-04:** construct local or socket Mihomo supervisors only in `cmd/simplusd` and inject one required typed API.
5. **P1 / V-05:** separate Webhook transport/provider protocol from notification use-case and persistence policy through a typed delivery port.
6. **P2 / C-01:** finish HTTP consumer-owned ports for health/setup/inventory/realtime.
7. **P2 / C-02:** before resourcelease is reactivated, remove SQLite-owned types from its application contract or retire the dormant package in a separate task.

These should be separate scoped implementation tasks. None requires HIL to establish the architectural repair; V-01's final real deployment behavior may later require separately authorized validation after its static/unit contract is corrected.

## Static-analysis limitations and residual risk

- Import and lexical analysis cannot prove behavior loaded through reflection, plugins, environment-dependent function injection or code generated only under unreviewed build tags. Linux and portable source files were text-scanned, while `go list` reflects the current Linux toolchain/package set.
- A fixed typed request can still be misused by an incorrect runtime implementation. This audit followed constructors and current concrete calls but did not dynamically instrument them.
- External provider, Mihomo, kernel, USB and network behavior was not contacted. “Clean” means no statically proven bypass in reviewed source, not proof of every runtime state.
- Generated and third-party internals were not re-audited; trust rests on checked sources/generator registry and locked provenance contracts.
- Ignored build output was not treated as source. A deployment made from stale/untracked artifacts could differ from this commit.
- No private runtime configuration was inspected, so environment-specific socket/path/capability drift remains outside this report.
- Line anchors are current for the recorded commit and will move after repairs.

## Validation record

Safe evidence commands used:

```text
git rev-parse HEAD
git status --short
GOTOOLCHAIN=local GOPROXY=off go list -f '{{.ImportPath}}|{{join .Imports ","}}|{{join .TestImports ","}}|{{join .XTestImports ","}}' ./cmd/... ./internal/...
git ls-files / rg --files inventories for cmd, internal, web/src, components, containers, scripts/release, production plugin build helpers and packaging
targeted rg scans for raw Fetch/EventSource, imports, AT/QMI/APDU, tty/dev/sysfs/USB/interface/VID/PID, process/shell/network primitives, public/Unix payload fields and generated ownership
bash scripts/dev/test-simplus-simaka-c.sh
```

Final results:

| Check | Result |
| --- | --- |
| `GOTOOLCHAIN=local GOPROXY=off go list ... ./cmd/... ./internal/...` | Pass; production/test import inventory produced without dependency download. |
| `GOTOOLCHAIN=local GOPROXY=off go vet ./cmd/... ./internal/...` | Pass. |
| `GOTOOLCHAIN=local GOPROXY=off make lint` | Partial: the `go vet` stage passed; locked `actionlint` could not start because its module was not cached and network/module lookup was deliberately disabled. No dependency was downloaded. |
| `GOTOOLCHAIN=local GOPROXY=off go test ./cmd/... ./internal/...` | Pass; all Go unit/integration packages, including compile/unit-only HIL command packages, ran without hardware or external runtime actions. |
| `COREPACK_ENABLE_NETWORK=0 corepack pnpm --dir web typecheck` | Pass (`tsc --noEmit`). |
| `COREPACK_ENABLE_NETWORK=0 corepack pnpm --dir web test` | Pass; 19 files / 70 tests. jsdom emitted only its documented non-fatal pseudo-element `getComputedStyle` notices. |
| `bash scripts/dev/test-simplus-simaka-c.sh` | Pass; dependency-free protocol/parser test compiled in a temporary directory and accessed no Agent socket or hardware. |
| `make check-container-files` plus syntax checks for the remaining release scripts | Pass. Only parsers were run; no script action was executed. |
| `GOTOOLCHAIN=local GOPROXY=off go test ./internal/containercontract` | Pass. |
| Focused raw Fetch/EventSource, upward import, lower-layer HTTP import and OpenAPI command/path-field assertions | Pass with only the two documented browser owners. |
| `git diff --check`, untracked report whitespace check and `git status --short -uall` scope assertion | Pass; every reported worktree path is under `.trellis/tasks/08-14-audit-layer-boundaries/**`. |
