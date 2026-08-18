# Retire dormant resource-lease application orchestration: execution plan

## Phase A — Remove the invalid dormant contract

- [x] Delete `internal/application/resourcelease/service.go` and its two
  package-local test files.
- [x] Confirm no production/test caller outside the deleted package needs a
  replacement and add no compatibility facade or new assembly.
- [x] Confirm the SQLite repository, focused tests, released runtime migration,
  and independent Agent outcome ledger are untouched.

## Phase B — Align current architecture documentation

- [x] Update `docs/architecture.md` to distinguish the retired application
  orchestrator from the retained SQLite/runtime-migration historical fixture.
- [x] Preserve ADR 0001's historical decision text and avoid claiming the
  dormant store is an active production capability.

## Phase C — Validate source, behavior and compatibility

- [x] Run focused SQLite tests to retain replay/fencing/reopen evidence.
- [x] Run the supported broad Go test/vet/lint scope to prove no hidden caller
  or compile dependency remains.
- [x] Run scans for the deleted package/import, forbidden storage-owned
  application lease types, retained migration/storage paths, and unchanged
  composition/API/generated surfaces.
- [x] Run formatting, generated/docs, task validation, and `git diff --check`.

## Phase D — Review and capture the rule

- [x] Complete an independent Trellis check of deletion scope, migration/data
  compatibility, documentation accuracy, and validation evidence.
- [x] Synchronize the verified application/storage ownership and historical
  fixture contract into the relevant backend specs.
- [ ] Commit, archive the child task, update the parent audit task record, and
  record the Trellis session journal.

## Validation commands

Safe commands may include:

```bash
test ! -e internal/application/resourcelease
rg -n 'internal/application/resourcelease|resourcelease\.|storage\.(ResourceLease|ResourceLeaseAcquire|ResourceLeaseOperation|ResourceLeaseCall)' cmd internal --glob '*.go'
go test -count=1 ./internal/storage/sqlite
go test -count=1 ./cmd/... ./internal/...
go vet ./cmd/... ./internal/...
make check-format
make lint
make verify-generated
make check-docs
python3 ./.trellis/scripts/task.py validate .trellis/tasks/08-17-resolve-resourcelease-storage-coupling
git diff --check
```

Inspect `git diff -- internal/storage/sqlite/resource_leases.go
internal/storage/sqlite/resource_leases_test.go
internal/storage/sqlite/migrations/runtime/00005_resource_group_leases.sql` and
require it to be empty.

## Prohibited validation

Do not start services or Compose, contact external endpoints, inspect private
runtime state, access a modem/SIM, send SMS/calls, change RF, write persistent
device state, or run HIL. This is a source-only boundary retirement.

## Risky files and rollback points

- `internal/application/resourcelease/`: delete the complete package; leaving a
  partial test/helper or moving its policy elsewhere would not complete the
  retirement.
- `docs/architecture.md`: describe current state without rewriting the
  historical ADR or implying the retained SQLite fixture is production-wired.
- `.trellis/spec/core/backend/application-boundaries.md`,
  `directory-structure.md`, and `storage-and-migrations.md`: update only after
  independent verification so the durable contract matches the final code.

Rollback is a source-only revert; no migration, data restoration, service
cleanup, or hardware action is involved.

## Implementation evidence (2026-08-18)

- Deleted the complete `internal/application/resourcelease` package and added
  no replacement package, adapter, caller, API, command, or composition-root
  assembly.
- Updated the current-state ResourceGroup lease paragraph and technical-debt
  principle in `docs/architecture.md`; ADR 0001 remains unchanged.
- The retained SQLite source, focused test, and runtime migration 00005 still
  have their pre-implementation SHA-256 values `4f39108e...`, `684fa62b...`,
  and `30cc22b5...`; their protected `git diff` is empty. The independent
  Agent ledger, API/generated sources, and production composition also have no
  scoped implementation diff.
- Passed `go test -count=1 ./internal/storage/sqlite`,
  `go test -count=1 ./cmd/... ./internal/...`,
  `go vet ./cmd/... ./internal/...`, `make check-format`, `make lint`,
  `make verify-generated`, `make check-docs`, task-context validation, and
  `git diff --check` using only offline/source-local validation.
- Task-context validation reports only informational size warnings for three
  curated files and otherwise passes all nine implement and nine check
  entries.
- Independent spec follow-up confirmed the new application-boundary scenario
  has all seven code-spec sections and quotes the current SQLite signatures.
  It removed an unsupported claim that the retained artifacts predate the
  product reset, avoided prescribing a speculative future application API,
  and synchronized backend-index routing without changing product code.
