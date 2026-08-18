# Research: dormant ResourceGroup lease coupling

## Baseline and finding

- Reviewed commit: `06316b169f61fd924c30cd46e652f380eb728a2b`.
- Parent finding: C-02 in
  `.trellis/tasks/08-14-audit-layer-boundaries/audit.md:207`.
- Severity is Low because the coupling is compile-time dormant: no production
  binary constructs or calls the application service.

`internal/application/resourcelease/service.go` declares a `Repository`, but
the port accepts and returns `storage/sqlite.ResourceLeaseAcquire` and
`storage/sqlite.ResourceLease`, and its validation branches on SQLite-owned
lease-kind constants. This makes persistence vocabulary the application
contract even though the package presents an interface.

## Complete reference inventory

| Surface | Evidence | Disposition |
| --- | --- | --- |
| Application orchestration | `internal/application/resourcelease/service.go` owns topology generation/capability policy, purpose rules, lease-ID generation, and the SQLite-typed repository contract. | Delete as unreachable current-product policy. |
| Application tests | `service_test.go` is the only caller of the service and uses a temporary real SQLite set plus Simulator topology; `umask_test.go` exists only for those fixtures. | Delete with the package. |
| Production assembly/callers | Current repository-wide Go/import scans find none outside the package tests. A `git grep` at the initial public release also finds no external application caller. | No runtime replacement or compatibility adapter is needed. |
| SQLite repository | `internal/storage/sqlite/resource_leases.go` owns the historical request/record types and Acquire/Renew/Release/Active SQL. Its only consumers are its own focused tests and the package being retired. | Retain unchanged as an isolated storage-owned historical fixture. |
| SQLite tests | `internal/storage/sqlite/resource_leases_test.go` covers replay, fencing, expiry/reopen, conflicts, and concurrent acquisition against real temporary databases. | Retain as migration/repository compatibility evidence. |
| Released migration | `internal/storage/sqlite/migrations/runtime/00005_resource_group_leases.sql` creates the three runtime tables and has a Down path to v4. It is embedded and applied by normal `OpenSet`. | Retain byte-for-byte; do not rewrite released history or drop user data. |
| Generated/API/Web | No OpenAPI, generated Go/TypeScript, sqlc query, Web, command, or environment surface refers to this application package or its lease types. Runtime migrations are hand-written/embedded, not sqlc inputs. | No generated or public contract change. |
| Product documentation | `docs/architecture.md:252,258,358` rejects a generic lease/fencing platform for current actions, distinguishes the retired application orchestrator from the retained migration/SQLite fixture, and records the same boundary in the current technical-debt principles. ADR 0001 records the historical product reset. | Align every current architecture statement with targeted application-package retirement; preserve ADR history. |
| Similar Agent table | `internal/agentapi/outcome_store.go` has a private `resource_group_fences` table for the dormant `radio.ensure-off` outcome ledger. It is a separate database/schema and does not import or call this package. | Explicitly out of scope. |

Both `service.go` and the SQLite repository first appear in the initial public
source release and have no later file history. Within the available repository
history, the application capability was therefore never production-wired.

## Product and migration constraints

- `docs/decisions/0001-product-scope-reset.md:39` says not to expand multi-layer
  ResourceGroup leases/generation/fencing into SMS/Call prerequisites; current
  flows use per-Modem serialization and minimal operation state.
- ADR 0001's line 62 deliberately avoided an immediate broad rollback. A later
  targeted deletion of one unreachable application package is consistent with
  that decision and does not require rewriting its historical rationale.
- `docs/architecture.md:258` allows the old lease store to remain a fixture or
  historical mechanism, while the production Agent does not register the old
  command.
- `.trellis/spec/core/backend/storage-and-migrations.md` forbids editing an
  already released migration and requires real-SQLite evidence for storage
  compatibility.

The application package owns no persisted data itself. Deleting it cannot
remove rows; retaining runtime migration 00005 keeps old databases, downgrade
history, and fresh `OpenSet` behavior unchanged.

## Options considered

1. **Delete only the dormant application package (recommended).** This removes
   the offending cross-layer contract and dead topology/capability policy with
   no production, schema, or data change. Storage code/tests remain a clearly
   labeled historical fixture.
2. Move lease types into application/domain and add SQLite mapping. This would
   spend code and test surface preserving a capability with no caller and
   conflicts with the current product simplification until a real use case
   exists.
3. Delete application plus SQLite repository/tests or add a drop migration.
   This is broader than the finding; a drop migration changes schema/data, and
   removing all storage evidence weakens compatibility coverage without a
   current benefit.

## Planning conclusion

Delete the three files under `internal/application/resourcelease`, update the
current architecture text so it distinguishes the retired orchestrator from
the retained runtime-database fixture, and synchronize the verified ownership
rule into Trellis backend specs after independent review. Do not edit the
SQLite repository, its test, migration 00005, Agent outcome ledger, OpenAPI,
generated files, or production composition.

No additional product decision remains before implementation; approval of this
task approves the recommended narrow retirement, not future schema removal.
