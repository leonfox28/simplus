# Core Backend Guidelines

> Source-backed conventions for the root Go module and its public, hardware,
> and persistence boundaries.

## Scope

These guides apply to `cmd/**`, `internal/**`, `api/**`, `sqlc.yaml`, and Go
tests in the root module declared by `go.mod`. The React workspace has its own
guidance under `.trellis/spec/web/frontend/`.

The current runtime is assembled as three responsibilities: `simplusd` owns
the HTTP control plane and SQLite state, `simplus-agent` owns modem endpoints,
and `simplus-netd` owns privileged network objects. The normative process and
data-flow map is `docs/architecture.md`; concrete assembly is in
`cmd/simplusd/main.go`, `cmd/simplus-agent/main.go`, and
`cmd/simplus-netd/main.go`.

## Pre-Development Checklist

- Read `docs/product.md` for product scope and `docs/architecture.md` for the
  invariants touched by the change.
- Read the focused guides below for every affected boundary. API or database
  work normally needs both its topic guide and
  [Quality and Testing](./quality-and-testing.md).
- Search the relevant package under `internal/application/` for an existing
  narrow port, sentinel error, and co-located test before adding an
  abstraction.
- For public HTTP changes, start at `api/openapi.yaml`; for Agent or netd Unix
  protocols, inspect the protocol types and both client/server validation.
- For modem work, also read
  `.trellis/spec/core/infra/hardware-and-hil-safety.md` before running anything
  against real hardware.

## Guide Index

| Guide | Project-specific contract |
| --- | --- |
| [Directory Structure](./directory-structure.md) | Root Go package ownership, executable assembly, generated boundaries, and test placement |
| [Application Boundaries](./application-boundaries.md) | Typed ports/values, retired storage-typed ResourceGroup lease orchestration, HTTP-owned adjacent application ports, explicit Setup/Mihomo/Webhook adapter composition, background coordination/realtime policy, stable business identity, hardware adapters, fail-closed behavior, and side-effect ordering |
| [API Contracts](./api-contracts.md) | OpenAPI-first public HTTP, cursor pagination, authenticated realtime invalidation, bounded Unix protocols, timeouts, and stable errors |
| [Storage and Migrations](./storage-and-migrations.md) | Five-dataset SQLite reality, dormant ResourceGroup lease compatibility, keyset indexes, Goose migrations, sqlc ownership, transactions, and sensitive persistence |
| [Quality and Testing](./quality-and-testing.md) | Trusted test patterns and targeted-to-broad Go validation |

## Architecture Rules at a Glance

```text
Web / public HTTP
        |
        v
internal/api/httpapi -> application domain service -> narrow ports
                                                     |-> internal/storage/sqlite
                                                     |-> typed Unix client
                                                             |
                         internal/agentapi or supervisor API  v
                                                             |
                                   model adapter / worker owns protocol details
```

- Business packages consume stable `ManagedModem` and `Line` identities, not
  USB paths or transient Agent targets. `internal/application/line/service.go`
  is the resolver between persisted Lines and the current inventory.
- Model and command details terminate at `internal/modemadapter/**`; the public
  `attransport.Query` in `internal/attransport/transport.go` is available only
  to compiled-in adapters.
- Unsupported, unverified, unavailable, and ambiguous hardware states remain
  explicit and fail closed. `internal/modemadapter/registry.go` rejects
  overlapping matches, while `internal/application/inventory/agent_source.go`
  advertises only observed capabilities.
- `api/openapi.yaml` and embedded SQLite migrations are sources; generated Go,
  TypeScript, and sqlc outputs are not hand-edited.

## Quality Check

- The change respects the dependency direction above and does not introduce
  model, VID/PID, interface, AT/QMI, sysfs, or `/dev` branching above the
  adapter/runtime boundary.
- Inputs are bounded and validated at the boundary; errors exposed outside a
  package use stable typed/sentinel semantics rather than raw implementation
  strings.
- New behavior has a co-located test at the narrowest useful layer, including
  failure, replay/restart, and privacy cases when those contracts apply.
- Generated outputs are refreshed only through their declared generators and
  `make verify-generated` is used when a source contract changes.
- Real hardware or network side effects were not used as ordinary validation;
  any authorized evidence is summarized according to
  `docs/privacy-and-publication.md`.

## Verification

Start with the affected package, then expand only as risk requires:

```bash
go test ./internal/application/messaging
go test ./internal/api/httpapi
go test ./internal/storage/sqlite
make check-format
make lint
make test
```

Run `make verify-generated` after OpenAPI, sqlc schema/query, or generator
changes. Run `make check-docs` when canonical documentation or public claims
change.
