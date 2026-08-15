# Inject Mihomo supervisor explicitly

## Goal

Resolve audit finding V-04 by making `cmd/simplusd` the only owner of the
local-versus-socket Mihomo supervisor implementation choice and requiring the
application runtime manager to receive one explicit typed supervisor API.

## Current Evidence

- `internal/application/mihomo/runtime.go` currently imports the typed
  supervisor API but its convenience constructor also calls
  `mihomosupervisor.NewLocal(root)` and discards the returned error.
- `cmd/simplusd/main.go` constructs and injects a Unix-socket client only when
  `SIMPLUS_MIHOMO_SUPERVISOR_SOCKET` is set; the empty-socket branch delegates
  concrete local construction to the application package.
- Compose always sets the supervisor socket, so production behavior already
  uses the intended netd boundary. The local implementation remains useful for
  explicit development/Simulator execution and must not become a production
  fallback.
- `internal/mihomosupervisor/local.go` owns bounded process and filesystem
  policy. That implementation remains in place; only its assembly location is
  changing.

## Requirements

- R1. Replace the two runtime-manager constructors with one constructor that
  accepts a required `mihomosupervisor.API` and returns a stable configuration
  error when any mandatory dependency is absent or the root is invalid.
- R2. Remove all concrete local-supervisor construction and ignored
  constructor errors from `internal/application/mihomo` while preserving its
  existing typed start/status/stop behavior and application-owned artifact
  policy.
- R3. Make `cmd/simplusd` explicitly select and construct either the local
  implementation for the existing empty-socket development mode or the Unix
  client for a configured socket, then inject the result through the single
  application constructor.
- R4. Treat both concrete-supervisor construction failures and application
  dependency configuration failures as startup configuration failures: log a
  bounded operational error, close the stores and exit non-zero.
- R5. Add command-level composition tests proving local and socket modes select
  the expected implementation and reject invalid paths without starting a
  Mihomo process or contacting a socket.
- R6. Refactor application runtime tests to inject a deterministic fake
  supervisor. Preserve the lower package's existing local process tests as the
  owner of concrete process behavior.
- R7. Update the backend architecture specification so future application
  constructors cannot reintroduce hidden local/client defaults.

## Acceptance Criteria

- [x] AC1. No production code under `internal/application/mihomo` calls
  `mihomosupervisor.NewLocal` or selects a concrete supervisor implementation.
- [x] AC2. There is one `NewRuntimeManager` constructor; it requires a typed
  supervisor and returns an error for invalid root, store, artifact resolver,
  core reader, or supervisor dependencies.
- [x] AC3. `cmd/simplusd` owns both local and socket construction branches,
  handles every returned error, and injects the selected API explicitly.
- [x] AC4. Existing runtime start/status/stop/restart semantics and Compose's
  socket-backed production path are unchanged.
- [x] AC5. Focused application and command tests cover the two composition
  modes, invalid construction, and typed runtime behavior without launching a
  real process from the application test.
- [x] AC6. Focused race tests, supported Go vet/lint checks, task validation,
  focused ownership scans, and `git diff --check` pass without deployment,
  Compose startup, private-state access, HIL or hardware/network side effects.

## Out of Scope

- Changing the Mihomo supervisor Unix protocol, request types, path policy,
  process lifecycle or listener validation.
- Removing the local development/Simulator supervisor.
- Changing OpenAPI, Web, SQLite schema/data, generated files, container
  privileges, Compose settings or Host VoWiFi behavior.
- Starting Mihomo, Compose, netd, hardware probes, HIL or any real network or
  modem operation as validation.
