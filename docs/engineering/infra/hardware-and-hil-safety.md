# Hardware and HIL Safety

## Default Boundary

Real hardware is not an ordinary test dependency. `docs/compatibility.md`
defines evidence levels: Designed, Fixture, HIL-0 (fixed allowlisted read-only
inspection), HIL (an explicitly authorized controlled hardware vertical), and
Runtime. Code, a descriptor, a tty/QMI/UAC endpoint, or a passing fixture does
not promote a business capability by itself.

Relevant fixed HIL-0 inspection is allowed. Explicit user approval is required
before real SMS or calls, RF changes, modem-persistent writes, SIM/eUICC
mutation, external network/communication side effects, or any HIL-1/HIL-2
action. Approval is action- and scope-specific; an earlier decision or HIL on
one SIM/model/operator does not authorize a new one.

`docs/decisions/0003-v1-read-only-hardware.md` is the default hardware policy.
Later narrow decisions such as
`docs/decisions/0011-ml307a-host-vowifi-hil.md`,
`docs/decisions/0016-vowifi-sms-over-ims.md`, and
`docs/decisions/0017-managed-modems-and-capability-adapters.md` document
specific typed exceptions and explicitly leave other actions unauthorized.

## Typed Device Boundary

Maintain the enforced chain:

```text
Web/API -> application intent -> Agent typed service -> model adapter -> device
```

- Web/API may send stable business IDs and fixed typed intent only. It never
  sends AT/QMI/APDU text, a `/dev` or sysfs path, an interface number, a shell
  command, a network command, or an arbitrary vendor payload.
- `internal/agentapi/server.go` exposes bounded routes selected by constructor;
  `internal/agentapi/listener.go` verifies the Unix peer UID.
- `internal/modemadapter/registry.go` owns reviewed model matching, endpoint
  roles, dynamic USB IDs, capability evidence, and fixed model actions.
- `internal/attransport/transport.go` owns bounded tty I/O only. A compiled-in
  adapter chooses the command and parses semantics.
- `internal/modemadapter/registry_test.go` and
  `internal/agentapi/client_server_test.go` protect ambiguous matches,
  unverified capability non-advertisement, serialization, and absent write
  routes.

Never add a debug escape hatch that turns a typed route into a generic command
runner. A local-only flag or Unix socket does not make arbitrary hardware
commands safe.

One-off recovery, classification, preservation, cleanup, fresh-inbound, and
outbound-confirmation runners are not retained as a repository toolkit. A new
real-hardware workflow must define the smallest typed object graph, authorization,
stop conditions, and private evidence handling for its own current purpose; old
HIL acceptance does not authorize reconstructing or rerunning a deleted tool.

When the user explicitly authorizes it, bounded low-sensitivity manufacturer,
model, firmware, module serial, IMEI, phone number, and message-content values
may be displayed only in the direct private conversation. They must not be
written to repository files, task artifacts, tests, fixtures, ordinary logs,
commit messages, or public documentation. Credentials, private endpoints or
topology, SIM/IMS identity, raw protocol transcripts, PDU, captures, databases,
and screenshots remain outside this low-sensitivity allowance.

## Scenario: module serial observation and display fallback

### 1. Scope / Trigger

- Trigger: a supported modem exposes a module-owned serial through a fixed
  read-only adapter command while its USB descriptor iSerial may be absent.

### 2. Signatures

- Adapter: `ReadModuleSerial(context.Context, attransport.Query) (string, error)`.
- Agent observation: `ModemIdentity.SerialNumber`.
- Inventory observation: `PhysicalDevice.ModemSerialNumber` and
  `PhysicalDevice.ObservedSerialNumber()`.
- Existing public display fields remain `serialNumber` and
  `managedModemSerialNumber`; no new public endpoint is introduced.

### 3. Contracts

- QDC507 owns the exact bounded sequence `AT+CGSN=?`, `AT+CGSN=0`, then
  `AT+CGSN=1`; upper layers never select a model or command.
- The syntax response must explicitly advertise parameters 0 and 1. Parameter
  0 must yield one printable serial record plus final `OK`; parameter 1 must
  yield one valid 15-digit IMEI plus final `OK`; the values must differ.
- Module serial is best-effort current display metadata. Failure leaves it
  empty and does not fail the otherwise complete probe.
- Stable modem binding remains the per-install IMEI fingerprint. USB iSerial
  and its fingerprint retain their existing meaning.
- Managed Modem and Line displays prefer module serial, then fall back to USB
  iSerial, then show unavailable. No observed raw serial is persisted as the
  stable equipment key.

### 4. Validation & Error Matrix

- unsupported or non-exact syntax response -> module serial unavailable;
- query failure, non-`OK` terminal, echo/URC/duplicate line -> unavailable;
- empty, control-containing, whitespace-padded, unquoted, or over-128-byte SN
  -> unavailable;
- malformed/check-digit-invalid IMEI or SN equal to IMEI -> unavailable;
- valid SN plus valid distinct IMEI -> populate the internal serial observation.

### 5. Good/Base/Bad Cases

- Good: module SN is valid and distinct -> display it while identity binding
  remains the IMEI fingerprint.
- Base: module SN is unavailable but USB iSerial exists -> display USB iSerial.
- Bad: `CGSN=0` returns IMEI or an ambiguous transcript -> expose neither as
  module serial and do not guess.

### 6. Tests Required

- Adapter fixtures cover success, optional documented spacing, unsupported
  syntax, command echo, malformed terminal/line count, bounds/control bytes,
  invalid IMEI and equal values.
- Agent protocol round-trip and validation cover the optional serial field.
- Inventory/domain/service tests assert module-first, USB fallback and empty
  behavior without changing USB fingerprint or equipment identity.
- Web tests assert the existing serial column/card renders the normalized
  public field; all fixtures remain synthetic.

### 7. Wrong vs Correct

Wrong: copy the IMEI into `USB.SerialNumber` or use it as a display fallback,
which conflates three identities and can leak stable binding semantics upward.

Correct: keep the fixed command and parser in the model adapter, carry module
serial in its own internal observation, preserve USB iSerial separately, and
choose the display fallback only in the domain/application view.

## Scenario: QDC507 SMS call-mode safety and production state

### 1. Scope / Trigger

- Trigger: assembling the HIL-accepted QDC507 SMS adapter in production and
  distinguishing an existing `CLCC mode=1` data bearer from a blocking call.

### 2. Signatures

- Agent: `--identity-key <absolute-file> --state-root <absolute-private-dir>`.
- Adapter seam: `ReadSMSBlockingCallCount(context.Context, attransport.Query) (int, bool)`.
- Runtime feature: `sms-v1` only when a complete `SMSBackend` is injected.

### 3. Contracts

- `state-root` is required, absolute, clean, non-root, private, and owns the
  fixed v2 recovery filename plus WAL/SHM; no enable flag exists.
- production composition is `TTYTransport -> Driver -> SQLiteStateStore ->
  SMSAdapter -> Registry -> shared OperationGate/Scanner -> SMSBackend`.
- the safe `DefaultRegistry()` remains SMS-closed; only the production QDC507
  composite reports `sms-control: observed`.
- a complete probe with zero CLCC rows proceeds. If rows exist, the adapter
  performs one fixed `AT+CLCC` classification: mode `1` is an existing data
  bearer and does not block SMS; every other documented mode blocks; malformed,
  unknown, or non-terminal transcripts return unknown and fail closed.
- SMS never starts, configures, tears down, or exposes a mode-1 bearer. RF
  mutation continues to reject every active CLCC row.

### 4. Validation & Error Matrix

- missing/relative/root state root -> startup argument error;
- unsafe/incompatible state artifacts or adapter/backend construction failure
  -> non-zero startup, no `sms-v1`;
- voice/fax/alternate call -> `ErrRFActiveCall`, no SMS payload;
- malformed/unknown call classification -> `ErrSMSStatusUnavailable`, no SMS payload;
- only mode-1 rows -> continue to the ordinary registration/SIM/device fence;
- selected native transport unavailable -> application error, never Host
  VoWiFi fallback.

### 5. Good/Base/Bad Cases

- Good: fenced ready QDC507, only pre-existing mode-1 rows, private v2 state ->
  one typed SMS dispatch.
- Base: no CLCC rows -> existing zero-call behavior.
- Bad: mode 0/2/unknown row, unsafe state root, duplicate eligible transport,
  or stale device/SIM -> fail before payload.

### 6. Tests Required

- parser fixtures assert exact mode-1 allowance and rejection of voice,
  malformed, unknown mode, unexpected lines, and terminal ambiguity;
- resolver tests assert zero payload/RF writes for every failure and prove
  receive-only operations are not coupled to call mode;
- Agent tests assert feature/route presence follows backend injection and HTTP
  write timeout covers `SMSRequestTimeout`;
- registry/composition tests assert production observed evidence, default
  closure, private state-root wiring, native+VoWiFi unique routing, and no
  fallback; ordinary tests must never contact hardware.

### 7. Wrong vs Correct

Wrong: reject SMS whenever `ActiveCallCount != 0`, which conflates QDC firmware
mode-1 data bearers with voice calls, or relax the count globally and thereby
allow voice/RF mutation.

Correct: keep the common probe count, invoke a model-owned fixed classifier
only for SMS send, allow exactly mode 1, reject every other/unknown shape, and
leave the existing bearer untouched.

## Safe Command Classification

Read the target before running it:

| Command/path | Classification |
| --- | --- |
| Unit/integration/Simulator checks (`go test`, `make test`, `make dev-sim`) | No real hardware evidence or authorization implied. |
| `make dev-hardware-probe` | Fixed read-only HIL-0 probe through `simplusctl`; relevant use is allowed, but output is sensitive and must stay private. |
| `make dev-agent-deploy` | Root-owned install/restart with a fixed probe; it mutates the host service and requires explicit approval even though its hardware probe is read-only. |
| `make dev-hardware` / `dev-hardware-lan` | Starts the hardware backend. Startup alone does not authorize clicking RF or communication actions; LAN exposure remains trusted-network only. |
| `make build-vowifi-hil` and protocol fixture tests | Build/test only; they do not authorize a real runner. |
| `scripts/dev/run-ml307a-vowifi-hil.sh`, Compose hardware startup, real messaging/calls/RF/eUICC | Controlled side effects; require an approved plan, exact target/preconditions, and explicit authorization. |
| `scripts/release/prepare-container-host.sh` | Host mutation, not HIL-0; requires deployment authorization and rollback understanding. Native production install/uninstall flows are retired. |

`docs/development.md` is the operational source for current command meanings.
If a target's behavior changes, update that document and this classification
in the same task.

## Preconditions for Authorized Hardware Work

Before an approved side-effecting action:

- restate the exact action, target model/Line, expected external effect, and
  stop condition;
- confirm no active or enabled legacy production/development Agent competes
  with the container Agent for the modem or ports;
- use the narrow typed runner and current adapter evidence; never substitute a
  model, interface, path, or command from memory;
- start with a fixture and, when useful, fixed HIL-0 state confirmation;
- preserve serial ownership, operation IDs, timeouts, read-back/state
  observation, and fail-closed behavior;
- do not silently retry an uncertain SMS/call/RF outcome or fall back to
  Simulator/direct egress;
- prepare cleanup/rollback for temporary network and service state before the
  action.

For Host VoWiFi, `docs/development.md` requires the already verified RF-Off
baseline, unique target, SIM ready, no active call, and explicit authorization
for the current runner. Product decoupling of RF and VoWiFi does not prove an
RF-On scenario compatible.

## Sensitive Evidence

Never commit or publish credentials, subscription/node material, private
endpoints or LAN topology, SIM/eUICC/IMS/device identity, phone numbers, raw
AKA/SIP material, packet captures, databases, screenshots, raw HIL/service
logs, command transcripts, or private troubleshooting timelines.

`docs/privacy-and-publication.md` requires raw evidence to stay in the external
private record system. Public results go to `docs/compatibility.md` as a
minimal statement of capability, evidence level, unverified boundaries, and
the automated regression that protects the conclusion. Stable public failure
semantics go to `docs/troubleshooting.md`, without raw addresses, payloads, or
identity material.

## Failures and Scope

- A failed HIL or check does not authorize a broader probe, a write, a new
  model command, a privilege expansion, a host-network/privileged container,
  or an unrelated repair.
- Stop on target ambiguity, identity/generation change, missing capability,
  unexpected RF/SIM/call state, persistence uncertainty, cleanup failure, or
  output that risks disclosure.
- Report the verified stage and stable error. Do not rerun a side-effecting
  action without new evidence and renewed authorization when its outcome may
  already have occurred.
- Never weaken `unsupported`/`unverified` evidence or public compatibility
  wording to match an implementation that lacks the required fixture/HIL.
