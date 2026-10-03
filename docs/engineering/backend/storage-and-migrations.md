# Storage and Migrations

## Storage shape

`internal/storage/sqlite/store.go` opens one control database at
`<data-root>/state/control.sqlite3`, with WAL and `0700/0600` directory/file
permissions. `Set.DB` is the shared transaction boundary. Agent hardware SMS
recovery remains an independent private database owned by the Agent.

ADR 0028 intentionally breaks the old five-database layout. Its migration files,
setup grants, recording/local-CA tables and dormant resource leases are removed.
The new root uses `simplus-control-state-v2`; control and the container initializer
reject old layouts before modifying them. There is no automatic import or cleanup.
Future migrations evolve this new layout without changing released migrations.

## Repository and transaction rules

Keep domain-named repositories under `internal/storage/sqlite`. Accept domain or
application values, use context-aware queries and return stable conflicts or
not-found errors. Use constraints and persistent comparisons for idempotency.

- Administrator creation and installation ready state commit together.
- Incoming SMS, unread marker and subscribed notification tasks commit together,
  before acknowledging or deleting a hardware message.
- Per-event subscription start times prevent buffered connection history from
  being sent after a new subscription or re-enable. Unchanged subscriptions keep
  their original start time when the channel is renamed.
- Connection cursor, confirmed checkpoint and its tasks commit together. Failed
  commits do not advance a cursor; replay cannot enqueue duplicate events.
- Outbound operation IDs remain unique and immutable across payload comparisons.
- Notification cancellation is in the channel update/delete or message delete
  transaction. A late worker completion cannot revive a cancelled task.
- Claims have bounded leases and attempt fencing. SQL blocks later events in the
  same channel/object/connection stream, while other streams remain claimable.

## Migrations and generated queries

Migrations live in `migrations/control`, are embedded and applied by Goose.
Append a numbered migration with Up/Down and update `dataset_metadata` version.
Enforce invariants with CHECK, uniqueness and foreign keys. Exercise transaction
failure, restart, replay, cancellation and integrity against temporary SQLite.
`store_test.go` also verifies rejection of unsafe roots and unmodified old files.

`sqlc.yaml` consumes this one schema with queries under `queries/core`; generated
Go stays under `generated/core`. Change schema/query sources then use `make generate`
and `make verify-generated`; never manually patch generated outputs. Existing
repository transactions may combine generated queries and context-aware SQL.

## Sensitive Persistence

Persist only the business data the feature contract allows:

- `ManagedModem` and Line bindings store instance-scoped fingerprints and
  masked hints, not raw equipment/SIM/IMS identities, sysfs paths, or device
  nodes (`docs/architecture.md`,
  `internal/storage/sqlite/managed_modems.go`, and
  `internal/storage/sqlite/managed_lines.go`).
- Runtime Host VoWiFi state stores desired intent; live network/protocol facts
  remain owned by `simplus-netd` (`docs/architecture.md`).
- Secret storage is mixed today and must be described precisely. Notification
  legacy Webhook URLs and optional signing secrets use
  `internal/security/secretbox/` through the labels
  `notification-channel:v1:<channel-id>:webhook` and
  `notification-channel:v1:<channel-id>:signing` owned by
  `internal/application/notification/service.go`. Create/replacement Update
  persist the adapter-normalized URL ciphertext plus a hostname-only public
  hint; empty URL and signing-secret Update inputs preserve their current
  ciphertexts, and empty URL input also preserves the hint. Delivery keeps
  plaintext only for the current call and passes it through one bounded
  in-memory adapter request, while `internal/notificationwebhook` owns no Store
  or ciphertext. The new layout retains these domain-separated encryption labels.
  Administrator passwords are stored only as Argon2id hashes produced by
  `internal/security/password/argon2id.go`. The Local CA/setup secret flow has been removed.
  Mihomo subscription URLs are a current exception:
  `internal/application/mihomo/subscriptions.go` writes `url_plaintext` in the
  private control database;
  `internal/application/mihomo/subscriptions_integration_test.go` explicitly
  protects that behavior. Do not claim universal at-rest encryption, log or
  publish any of these values, or change their storage contract without an
  explicit migration and corresponding tests.
- Database files, WAL/SHM files, copied fixtures containing real data, and
  Compose `./data` are private runtime artifacts and never repository inputs
  (`docs/privacy-and-publication.md`).

## Verification

Run affected repositories and use-case tests, then `make verify-generated` and
`make check-docs`. Race/lifecycle tests must prove no database access survives
shutdown. See the active plan for the cross-layer acceptance record.
