# Backend Directory Structure

## Root Ownership

`go.mod` declares one Go module at the repository root. Keep code with the
runtime or domain that owns it:

| Path | Current owner and evidence |
| --- | --- |
| `cmd/` | Configuration, concrete dependency construction and process lifecycle. |
| `internal/domain/` | Business facts, safe values, rules and neutral runtime capabilities. |
| `internal/application/` | Use-case orchestration through narrow consumer-owned interfaces. |
| `internal/api/httpapi/` | Auth, devices, communications, network and notification handlers; shared gates/timeouts/errors. |
| `internal/storage/sqlite/` | One control database, transactions, migrations and repositories. |
| `internal/agentinventory`, `modemagent`, `smstransport` | Agent/IMS protocol mapping outside business services. |
| `internal/feishu`, `notificationwebhook`, `subscriptionhttp`, `mihomoassets` | External HTTP, credentials, bounded downloads, filesystem and process adapters. |
| `internal/ims/` | Production SIP/IMS/SMS/strongSwan protocols. |
| `internal/vowifihil/` | Explicit validation entry points only. |
| `internal/lifecycle/`, `architecture/` | Request draining and dependency-direction checks. |
| `internal/simulator/` | In-memory RF/cellular/VoWiFi capabilities without hardware or network processes. |

## Placement Rules

Keep application interfaces owned by consumers, use domain values across layers,
and keep SQL, paths, protocol commands and external HTTP in adapters. Construct
validated dependencies once before serving or starting workers. Use explicit
unavailable implementations for disabled features rather than reflection or
post-construction setters.

Keep tests beside their owner; cross-package acceptance may use an external test
package and real temporary SQLite. Use behavioral assertions for replay, failure,
ordering, privacy and cancellation. Static import checks supplement those tests.
Keep Linux-specific endpoint/peer-credential code behind platform files.

## Generated and Embedded Files

Never hand-edit files that declare themselves generated:

- `internal/api/openapi/generated.go` comes from `api/openapi.yaml` via
  `internal/api/openapi/generate.go` and `api/oapi-codegen.yaml`.
- `web/src/api/generated/` comes from the same OpenAPI source through
  `web/openapi-ts.config.ts` and the root `api:generate` script.
- `internal/storage/sqlite/generated/core/*.go` comes from `sqlc.yaml`,
  `internal/storage/sqlite/migrations/control/`, and
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
- Do not type-assert one catch-all store to discover optional application
  capabilities or construct password/filesystem/secret defaults or
  local/client supervisor/Webhook HTTP implementations in an application
  constructor. Name the ports and assemble concrete adapters in `cmd/**`.
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
