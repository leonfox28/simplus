# Backend Directory Structure

## Root Ownership

`go.mod` declares one Go module at the repository root. Keep code with the
runtime or domain that owns it:

| Path | Current owner and evidence |
| --- | --- |
| `cmd/` | Per-binary flag parsing, dependency construction, lifecycle, operational log rendering, and executable-only helpers. `cmd/simplusd/main.go` wires stores, services, transports, HTTP and application-owned background coordinators; `cmd/simplus-agent/main.go` wires the typed hardware server; `cmd/simplus-netd/main.go` wires the privileged supervisors. |
| `internal/application/` | Domain use-case services, background business coordinators and consumer-owned ports. `internal/application/messaging/service.go` owns its repository, Line source, sender, and inbox contracts; Messaging and Inventory own their SMS/Agent-change coordination policy; `internal/application/realtime/` owns bounded topic publication/subscriptions. |
| `internal/domain/` | Business records, enums, validation, and domain errors without transport assembly. `internal/domain/hardware/` defines normalized topology; `internal/domain/pagination/` owns shared opaque cursor validation/encoding. |
| `internal/api/httpapi/` | Public HTTP authentication, middleware, request/response mapping, and generated OpenAPI server implementation (`internal/api/httpapi/server.go`). |
| `api/` and `internal/api/openapi/` | `api/openapi.yaml` is the public API source; `internal/api/openapi/generate.go` and `api/oapi-codegen.yaml` define Go generation; `internal/api/openapi/generated.go` is output. |
| `internal/agentapi/` | Bounded Unix-socket hardware protocol, peer validation, typed clients/servers, and protocol tests. |
| `internal/modemadapter/`, `internal/hardwareprobe/`, `internal/attransport/` | Model facts and commands, inventory orchestration, and generic bounded tty I/O respectively. |
| `internal/storage/sqlite/` | SQLite opening, migrations, repositories, generated sqlc package, and persistence tests. |
| `internal/*supervisor/` | Typed clients and local implementations for privileged Mihomo/Host VoWiFi runtime ownership. |

## Placement Rules

- Keep `cmd/**` as the composition root. New business decisions belong in an
  application or domain package, even when only one binary currently calls
  them. `cmd/simplusd/main.go` demonstrates the intended assembly: it selects
  the backend, builds narrow services/coordinators, starts their lifecycle,
  renders operational reports, and injects services into `httpapi.Server`.
- Put an interface next to its consumer when it represents that consumer's
  needs. The small interfaces at the top of
  `internal/api/httpapi/server.go` and
  `internal/application/messaging/service.go` avoid a shared catch-all service
  package.
- Keep protocol-neutral business records in `internal/domain/**`. Wire and
  persistence representations may convert at their boundaries; they must not
  become the domain source of truth.
- Keep Linux-specific implementations behind portable files or interfaces.
  Examples include `internal/attransport/session_linux.go` plus
  `internal/attransport/session_other.go`, and
  `internal/agentapi/peercred_linux.go` plus platform fallbacks.
- Keep tests beside the package they prove. Representative files are
  `internal/application/messaging/service_test.go`,
  `internal/api/httpapi/server_test.go`,
  `internal/modemadapter/registry_test.go`, and
  `internal/storage/sqlite/store_test.go`; cross-package integration tests are
  still located with the owning package, such as
  `internal/application/notification/integration_test.go`.

## Generated and Embedded Files

Never hand-edit files that declare themselves generated:

- `internal/api/openapi/generated.go` comes from `api/openapi.yaml` via
  `internal/api/openapi/generate.go` and `api/oapi-codegen.yaml`.
- `web/src/api/generated/` comes from the same OpenAPI source through
  `web/openapi-ts.config.ts` and the root `api:generate` script.
- `internal/storage/sqlite/generated/core/*.go` comes from `sqlc.yaml`,
  `internal/storage/sqlite/migrations/core/`, and
  `internal/storage/sqlite/queries/core/`.

`internal/storage/sqlite/store.go` embeds
`internal/storage/sqlite/migrations/*/*.sql`, so migration files are runtime
inputs rather than documentation. `cmd/simplusd/web.go`
similarly owns production static-Web serving; `web/dist` is a build output and
is not a source directory.

The authoritative generated-path list and regeneration sequence are the
`GENERATED_PATHS`, `generate`, and `verify-generated` definitions in
`Makefile`. If a generated target is missing from that list, update the source
and verification contract together instead of inventing a manual refresh.

## Avoid

- Do not put SQL, HTTP status mapping, modem commands, or filesystem paths into
  domain records merely because a handler needs them.
- Do not create a general `utils`, `common`, or universal modem interface
  before searching the owning packages for an existing narrow pattern.
- Do not import a concrete SQLite store or Agent implementation into an
  application service when a consumer-owned port is sufficient.
- Do not create a second public API type tree beside `api/openapi.yaml`, or a
  second hardware protocol beside `internal/agentapi` for one model.
- Do not move tests to a distant umbrella directory; co-location is what makes
  current package contracts discoverable.

## Placement Check

Before adding a file, answer which current owner above would change if its
behavior changed. If the answer spans executable wiring, business semantics,
wire format, and device protocol, split those responsibilities at the same
boundaries used by `cmd/simplusd/main.go`, `internal/application/messaging/`,
`internal/api/httpapi/`, and `internal/modemadapter/`.
