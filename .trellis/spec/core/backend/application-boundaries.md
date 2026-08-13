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
