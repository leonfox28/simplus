# Resolve resource lease storage coupling

## Goal

Retire the dormant `internal/application/resourcelease` orchestration package,
which exposes SQLite-owned command/record/kind types through an application
contract. Preserve the released runtime-database migration and the isolated
SQLite repository implementation as historical compatibility infrastructure,
without activating a new production capability or changing current runtime
behavior.

## Background

The layer-boundary audit recorded C-02 as a Low architecture concern. The
application package defines a repository abstraction but exposes
`internal/storage/sqlite.ResourceLease*` types in its own service contract.
Repository-wide audit evidence found no production constructor or caller, so
this is dormant coupling rather than an active runtime bypass.

Current evidence starts at
`.trellis/tasks/08-14-audit-layer-boundaries/audit.md:207` and
`internal/application/resourcelease/service.go`.

## Requirements

- R-01: Trace every source, test, migration, generated query, documentation,
  and production reference to resource leases before selecting removal or
  decoupling.
- R-02: Do not reactivate resource leasing, add a new API/command, or change
  current product/runtime behavior as part of this boundary repair.
- R-03: Remove the unreachable application package and its package-local tests.
  Do not move its policy into another active layer or retain a compatibility
  facade for a contract with no caller.
- R-04: Preserve current migration and downgrade compatibility unless evidence
  proves a schema artifact is both unreachable and safe to remove under the
  project's migration policy.
- R-05: Keep validation offline and synthetic; no service, Compose, external
  endpoint, modem, RF, SMS/call, device write, or HIL action is authorized.
- R-06: Keep `internal/storage/sqlite/resource_leases.go`, its focused tests,
  and runtime migration 00005 unchanged as a storage-owned historical fixture;
  future reactivation requires a separately designed application-owned model
  and SQLite mapping rather than reuse of the retired cross-layer contract.

## Out of Scope

- Designing or enabling a new resource-leasing product feature.
- Refactoring active Line, Modem, Agent, SMS, Call, Setup, or realtime flows.
- Rewriting historical migrations or deleting persisted user data without a
  separately approved migration decision.
- Removing the independent Agent `radio.ensure-off` outcome/fencing ledger,
  which has a similarly named private table but is not this application
  package or runtime-database repository.

## Acceptance Criteria

- [ ] No application contract exposes a concrete SQLite-owned resource-lease
  type.
- [ ] `internal/application/resourcelease` and its package-local tests no longer
  exist, and no replacement production assembly or caller is introduced.
- [ ] The chosen repair is supported by a complete live-reference and
  compatibility inventory.
- [ ] Production behavior, public/internal APIs, generated contracts, and
  existing data compatibility remain unchanged.
- [ ] The released runtime migration and isolated SQLite resource-lease
  repository/tests remain unchanged and continue to pass their focused tests.
- [ ] Focused tests/scans plus required repository checks pass without live
  runtime or hardware access.

## Decision

- Retire only the dormant application orchestrator. Repository-wide current and
  initial-release scans find no production constructor or caller, and the
  canonical product architecture explicitly rejects extending generic
  ResourceGroup leases into current SMS/Call flows.
- Retain the SQLite repository, focused tests, and already released runtime v5
  migration. This avoids rewriting migration history or deleting persisted
  data while keeping the repair limited to the actual cross-layer contract.
- Do not introduce application-owned lease types speculatively. If a future
  feature needs leasing, design its current business contract first and add an
  explicit mapping in a new approved task.
