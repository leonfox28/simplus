# Design: retire dormant resource-lease application orchestration

## 1. Boundary outcome

The current invalid direction is dormant but explicit:

```text
application/resourcelease contract
  -> storage/sqlite ResourceLease types and kind constants
```

The repair removes that unused application contract. It does not replace it
with another port because no production consumer exists:

```text
production runtime: unchanged (no resourcelease application path)

storage/sqlite historical repository + embedded runtime v5 migration
  -> retained only as isolated storage compatibility/fixture code
```

Any future lease feature must start with its own application/domain vocabulary
and map to persistence explicitly. The retained SQLite types are not an
approved application API.

## 2. Exact source scope

Delete the complete package:

- `internal/application/resourcelease/service.go`
- `internal/application/resourcelease/service_test.go`
- `internal/application/resourcelease/umask_test.go`

Do not add a replacement constructor, facade, alias, command, API route, or
composition-root assembly. No active package currently imports these files, so
the deletion has no call-site migration.

Retain without behavior changes:

- `internal/storage/sqlite/resource_leases.go`
- `internal/storage/sqlite/resource_leases_test.go`
- `internal/storage/sqlite/migrations/runtime/00005_resource_group_leases.sql`
- the independent `internal/agentapi` outcome/fencing ledger.

## 3. Documentation and specification ownership

Update only current architecture claims that still group all lease code as
available application infrastructure. State precisely that the application
orchestrator is retired while the released runtime schema and SQLite fixture
remain for compatibility. Keep ADR 0001 as historical decision evidence; its
“not immediate deletion” statement remains true and should not be rewritten.

After implementation and independent checking, capture these durable rules in
the backend specs:

- application contracts must not expose storage-owned values even when the
  repository itself is abstracted;
- the old resource-lease orchestrator is not a supported application package;
- runtime migration 00005 and the SQLite lease repository are dormant
  historical storage infrastructure, not a source for new application types;
- released migrations are not edited as part of dead-code retirement.

## 4. Compatibility and validation

Static scans must prove the package is absent, no import/caller/replacement was
introduced, and the retained storage/migration files have no implementation
diff. Focused real-SQLite tests preserve the historical repository behavior;
the supported Go package test/vet/lint scope proves the deletion did not hide a
caller. Documentation, generation-drift, task-context, formatting, and diff
checks complete the source-only validation.

No service, Compose stack, external endpoint, private runtime database, modem,
SIM, RF, SMS/call, persistent device write, or HIL evidence is needed or
authorized.

## 5. Risks and rollback

The main risk is deleting a hidden caller. Repository-wide current/import
scans, the initial-release history scan, and broad compilation close that
risk. The second risk is accidentally treating a historical table as safe to
drop; keeping the migration and storage implementation byte-for-byte avoids
schema/data impact.

Rollback is a source-only restoration of the deleted application package and
documentation. No external or database rollback is required.
