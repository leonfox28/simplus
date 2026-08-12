# Backend Quality and Testing

## Trusted Test Shapes

Tests live beside the package and prove the owning contract:

- Application tests inject narrow function/interface fakes, then use a real
  temporary SQLite set when ordering or recovery matters.
  `internal/application/messaging/service_test.go` proves queued persistence
  before dispatch, operation replay/conflict, multipart behavior, and restart
  reconciliation with `t.TempDir()`.
- HTTP tests call the real router with `httptest` and explicit cookies/CSRF.
  `internal/api/httpapi/server_test.go` covers boundary mapping, authentication,
  trusted authorities, timeouts, panic recovery, response privacy, cursor
  presence semantics, and authenticated SSE lifecycle/deadlines.
- Realtime tests prove bounded publication, initial/all-topic resync,
  slow-subscriber replacement, cancellation, topic normalization, and the rule
  that only inbound SMS/incoming calls carry generic attention. Keep payload
  assertions privacy-sensitive; the stream is not a resource transport.
- Protocol tests validate both ends. `internal/agentapi/protocol_test.go` feeds
  malformed typed responses to client validation, while
  `internal/agentapi/client_server_test.go` proves production route
  availability.
- Registry/adapter tests are table-driven where several evidence cases share
  one invariant. `internal/modemadapter/registry_test.go` covers exact matches,
  ambiguity, endpoint roles, capability evidence, default non-advertisement,
  and per-device serialization.
- Storage tests reopen real files and old schemas. Do not replace
  `internal/storage/sqlite/store_test.go` upgrade/integrity cases with a mock
  database.
- Deployment shape is executable Go contract, not only YAML review:
  `internal/containercontract/contract_test.go` parses `compose.yaml` with
  known fields and checks privilege/mount/image invariants.
- External-provider protocol fixtures must be anchored to a primary contract
  or official SDK implementation, then include the currently observed
  provider shape when a privacy-safe structure probe is available. Record
  only field names, types, lengths, status classes, and approved authorities;
  never copy opaque codes, complete authorization URLs, credentials,
  identities, or raw provider bodies into tests or research.
- Before declaring such a probe complete, inventory every non-sensitive
  response property that participates in production validation and report its
  type plus normalized value or boundary class. A selective report such as
  “legacy field absent” is insufficient when the current replacement field's
  numeric value drives a limit check. Turn each observed boundary into a
  synthetic fixture and include exact-limit, limit-plus-one, default,
  conflict, and architecture/overflow cases where numeric conversion occurs.

Use deterministic clocks/readers or small fakes already exposed by the service
when exact IDs/timestamps matter. Keep fixture identities synthetic and within
the public-data rules in `docs/privacy-and-publication.md`.

## What New Tests Must Prove

Choose cases from the actual risk boundary:

- validation: empty, malformed, unknown enum, oversize, and duplicate input;
- state: success plus invalid/conflicting transition;
- persistence: fresh state, replay/idempotency, restart/reopen, and partial
  failure when the operation can outlive a request;
- typed protocols: version/instance/generation correlation and malformed
  responses, not merely successful JSON decoding;
- hardware: unsupported, unverified, unavailable, ambiguous, and changed
  hardware must fail closed; do not test only an ideal device;
- privacy: raw secrets/identities/paths are absent from responses and ordinary
  logs;
- concurrency: serialization/cancellation for resources that have one owner.
- provider-owned opaque state: bound the total bytes and lifetime, preserve it
  byte-for-byte, and exercise realistic punctuation/length/status behavior.
  Do not reuse an App credential, identifier, or token alphabet unless the
  provider contract explicitly assigns that grammar to the opaque field.
- provider-owned durations: normalize units before conversion, compare against
  an explicit local resource-lifetime constant, and test on the narrowest
  supported integer architecture. Do not retain a small magic maximum merely
  because an earlier synthetic fixture happened to fit it.

A test that merely reimplements the production branch in its expected value is
not evidence. Assert observable state, recorded typed calls, persisted rows,
or the wire response.

## Validation Ladder

Run the smallest check that can diagnose the change, then expand for shared
contracts. Representative commands from `Makefile` are:

```bash
go test ./internal/application/messaging
go test ./internal/api/httpapi
go test ./internal/agentapi ./internal/modemadapter
go test ./internal/storage/sqlite
go test ./internal/containercontract
make check-format
make verify-generated
make lint
make test
make web-e2e
```

`make lint` currently runs `go vet` over `./cmd/... ./internal/...` and
`actionlint` over GitHub workflows; it is not a frontend lint command.
`make test` runs all root Go packages, the Web Vitest suite and typecheck, the
worktree-manifest regression, and the Simulator supervisor test. Consult the
Web frontend quality spec for browser-side expectations. `make web-e2e` runs
deterministic Playwright desktop/mobile flows with synthetic HTTP/SSE fixtures;
it must not trigger hardware or real communications.

Run `make check-docs` for public docs/instruction-map changes and
`make check-container-files` for container shell/config changes. Run
`make security` or builds when the affected dependency/release/runtime surface
warrants them; neither is a substitute for a targeted behavior test.

## Generated-Drift Discipline

When a source contract changes, regenerate and inspect the outputs before
testing. `make verify-generated` snapshots every declared generated file,
runs generation, compares bytes, and compares a complete worktree content
manifest. A failure means either a source/output drift or an incomplete
generator contract; do not patch the generated result to silence it.

## Hardware Is Not an Ordinary Test Dependency

Unit, integration, Simulator, and container-contract checks must stay
deterministic without a modem, SIM, subscription, privileged namespace, or
private log. HIL-0 and side-effecting HIL are separate evidence levels defined
in `docs/compatibility.md` and governed by
`.trellis/spec/core/infra/hardware-and-hil-safety.md`.

Do not run a real RF, messaging, call, SIM/eUICC, AKA/network, device-write, or
HIL workflow merely because a unit test is missing. Add a fixture first and
request explicit authorization for the smallest real action only when the task
requires that evidence.

## Failure Handling

- Diagnose whether a failing check is caused by the scoped change before
  editing code or tests.
- Fix attributable failures and report unrelated, environmental, or flaky
  failures without expanding the task.
- Do not weaken a test, validation bound, capability status, or error mapping
  to make a check green unless the approved behavior changed.
- Report non-fatal test-runner warnings (including the current jsdom CSS
  parsing and pseudo-element `getComputedStyle` warnings) separately; do not
  hide them or mistake them for passing evidence.
