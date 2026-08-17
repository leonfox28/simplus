# Implementation Plan

## Phase A — Define the HTTP consumer ports

- [x] Add `HealthReader`, `SetupManager`, `InventoryReader`, and
  `RealtimeManager` with only the researched live method sets.
- [x] Replace the four concrete `Server` fields and the matching `New` /
  `WithRealtime` parameter types without changing argument order or behavior.
- [x] Keep interface values intact and use a typed-nil-aware absence predicate
  at the four optional Setup/realtime checks, preserving nil-receiver errors.

## Phase B — Prove compatibility and narrowness

- [x] Add production structural assertions and independent interface fakes in
  the HTTP package.
- [x] Prove fake-only construction plus realtime Subscribe/Publish delegation.
- [x] Cover raw-nil and typed-nil health/setup/inventory/realtime inputs plus
  Health/Setup/Inventory nil-receiver error dispatch.
- [x] Retain all existing router/handler/SSE tests unchanged except for any
  mechanical fixture typing required by the new seam.

## Phase C — Validate boundaries and capture the contract

- [x] Run focused HTTP and command tests, then focused race and the supported
  broad Go test/vet/lint scope.
- [x] Run ownership scans for removed concrete pointers, exact live port
  methods, unchanged composition, and absence of OpenAPI/generated drift.
- [x] Run formatting, docs/generated checks, task validation, and
  `git diff --check`.
- [x] Complete an independent Trellis check, then synchronize the verified
  HTTP consumer-port rule into the relevant backend specs.
- [x] Commit, archive the child task, update the parent audit task record, and
  record the Trellis session journal.

## Validation Commands

Safe commands may include:

```bash
go test -count=1 ./internal/api/httpapi ./cmd/simplusd
go test -count=1 -race ./internal/api/httpapi ./cmd/simplusd
go test -count=1 ./cmd/... ./internal/...
go vet ./cmd/... ./internal/...
make check-format
make lint
make verify-generated
make check-docs
python3 ./.trellis/scripts/task.py validate .trellis/tasks/08-16-narrow-http-application-service-ports
git diff --check
```

## Prohibited Validation

Do not start services or Compose, contact external endpoints, inspect private
runtime state, access a modem/SIM, send SMS/calls, change RF, write persistent
device state, or run HIL. This source-boundary refactor requires no live
environment evidence.

## Risky Files and Rollback Points

- `internal/api/httpapi/server.go`: interface method signatures and the
  typed-nil-aware optional-availability predicate are the main compile/behavior
  risk.
- `internal/api/httpapi/server_ports_test.go` or `server_test.go`: fakes must
  record observable calls rather than copy production behavior.
- `.trellis/spec/core/backend/application-boundaries.md` and
  `directory-structure.md`: update only after independent verification so the
  recorded contract matches the final code.

Rollback is a source-only revert; no migration or external cleanup is needed.

## Implementation Validation Notes

- Independent review found no attributable defect and made no code/test edit.
- Focused HTTP and `cmd/simplusd` tests plus their uncached race runs passed.
- Uncached `go test -count=1 ./cmd/... ./internal/...`, `go vet`, and
  `make lint` (vet plus locked actionlint) passed.
- `make check-format`, `make verify-generated`, `make check-docs`, Trellis task
  validation, ownership/method-set/composition scans, and `git diff --check`
  passed before final spec synchronization.
- Existing `internal/api/httpapi/server_test.go` and production
  `cmd/simplusd/main.go` required no edits.
- Full `make test` and Web E2E were intentionally not run: this backend-only
  change has no Web contract, and the repository target starts a development
  Simulator service, which the task safety boundary prohibits. The complete
  affected Go scope and generated-contract checks passed instead.
- No service, Compose, external endpoint, private runtime state, hardware, HIL,
  RF, SIM/eUICC, SMS, or call action was used.
- Functional work was committed as `aa8b41f`; task archival and journal
  recording are completed by the Trellis finish-work flow.
