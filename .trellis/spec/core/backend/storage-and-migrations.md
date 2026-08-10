# Storage and Migrations

## Current Storage Shape

`internal/storage/sqlite/store.go` opens five SQLite datasets: `core`,
`contacts`, `messages`, `calls`, and `runtime`. This is current compatibility,
not a requirement to create a new database for each feature. As
`docs/architecture.md` states, new data belongs in the semantically closest
existing dataset; cross-database transactions and a new backup/dataset
framework are not current product goals.

`OpenSet` is the only normal opening path. It validates an absolute non-root
storage directory, ownership and non-symlink identities, private modes, a
storage marker, dataset identity, schema manifest, integrity, foreign keys,
and WAL artifacts. Each database uses one open connection, `busy_timeout`,
foreign keys, `synchronous=FULL`, `trusted_schema=OFF`, and WAL. Preserve those
checks rather than opening the same files ad hoc from a domain store.

## Store Methods

- Put persistence behavior in a domain-named file under
  `internal/storage/sqlite/`, such as
  `internal/storage/sqlite/messages.go`,
  `internal/storage/sqlite/managed_lines.go`, or
  `internal/storage/sqlite/vowifi.go`.
- Accept domain/application types, validate required invariants before SQL,
  use `Context` methods, and wrap operational errors with the affected action.
- Check `RowsAffected` for single-record mutations and return a stable domain
  not-found/conflict error. `UpdateManagedLine` and the outbound-message state
  transitions are representative.
- Encode idempotency in database constraints plus comparison, not only in
  process memory. `CreateOutboundSMS` uses a unique operation ID and rejects a
  replay whose Line, destination, body, or direction differs.
- Use a transaction when several reads/writes form one invariant. The inbound
  multipart spool in `internal/storage/sqlite/messages.go` uses `BeginTx`,
  deferred rollback, conflict checks, and a final commit; setup/auth/eUICC
  stores use the same rollback pattern.

Do not broaden a transaction across multiple dataset files: SQLite cannot make
the current five independent databases one implicit atomic unit. If a feature
requires a new cross-dataset guarantee, design it explicitly rather than
assuming the `Set` provides one.

## Migration Ownership

Migrations are embedded from `internal/storage/sqlite/migrations/*/*.sql` and
applied by Goose in `internal/storage/sqlite/store.go`. For each change:

- append the next zero-padded migration in the owning dataset directory;
- include `-- +goose Up` and a meaningful `-- +goose Down` path;
- update the singleton `dataset_metadata.schema_version` to the migration
  version in both directions;
- encode invariants with SQLite `CHECK`, uniqueness, and foreign keys when the
  database can enforce them;
- preserve existing user intent/data explicitly during table rebuilds;
- use `-- +goose NO TRANSACTION` only when the migration owns its explicit
  `BEGIN/COMMIT` sequence, as
  `internal/storage/sqlite/migrations/core/00022_line_identity_path_decoupling.sql`
  does while temporarily disabling foreign keys.

Migration tests must exercise an actual old schema and reopen through
`OpenSet`. `internal/storage/sqlite/store_test.go` downgrades with Goose, seeds
old rows, reopens, and verifies the message-v4/v5 and Line-v21/v22 upgrades;
it also checks schema tampering, swapped datasets, symlinks, hard links,
permissions, and foreign-key integrity.

Calls v3 adds a keyset-pagination index ordered by
`(created_at_unix_ms DESC, call_id DESC)`. Calls store methods must use the
identical tuple and strict `<` boundary, query `limit+1`, and construct the next
cursor from the final returned record. Do not substitute offsets or
timestamp-only cursors; equal call timestamps require the stable-ID tiebreaker.

Messages v8 replaces the older created-time page indexes with
`record_sequence INTEGER PRIMARY KEY AUTOINCREMENT`, plus
`(remote_address, record_sequence DESC)` and
`(line_id, remote_address, record_sequence DESC)`. Global, recipient, paired
Line/recipient, conversation-latest, conversation order, and last outbound
Line reads all use sequence. New inserts let SQLite allocate the sequence;
replay, status changes, and deletion never update or reuse it. Pages use strict
`record_sequence < ?`, query `limit+1`, and return the last included sequence
in an SMS v2 cursor, so deleting the boundary row does not break continuation.

Messages v7 adds `(remote_address, created_at_unix_ms DESC, message_id DESC)`
for recipient history and summaries plus `sms_message_unread`. Each newly
inserted inbound message creates one unread row in the same transaction;
duplicate/replayed inbound sources do not. `unread_id INTEGER PRIMARY KEY
AUTOINCREMENT` is a separate arrival order because millisecond message time and
random IDs cannot safely watermark concurrent arrivals. Counts are derived,
message deletion cascades markers, and read-state deletion is bounded by exact
remote address plus an opaque token's unread ID/message boundary. The migration
creates an empty ledger so v6 history starts read; Down removes read state and
the remote index without rebuilding or deleting `sms_messages`.

The v8 rebuild preserves that unread table and its explicit AUTOINCREMENT IDs
while changing `message_id` from the table primary key to a UNIQUE business
key referenced by the existing foreign key. v7 history is assigned explicit
sequence values in ascending recoverable persistence order:
`CASE direction WHEN 'inbound' THEN updated_at_unix_ms ELSE created_at_unix_ms END`,
then `created_at_unix_ms, message_id`. Up and Down run with foreign keys disabled
only around their explicit transaction, restore them afterward, preserve all
messages and unread markers, and must pass `foreign_key_check` plus Down/re-Up
tests. Never sort future SMS records by this backfill expression; it exists only
to recover v7 history.

Never edit an already released migration to make a new checkout pass. Add a
new migration and a regression from the prior version.

## sqlc and Generated Ownership

`sqlc.yaml` currently generates only the small `core` state query package from
`internal/storage/sqlite/queries/core/` and the core migrations. Most domain
stores are hand-written SQL today. Do not claim or assume full sqlc coverage.

Files under `internal/storage/sqlite/generated/core/` contain `DO NOT EDIT`
headers. Change their query/schema sources and run `make generate`; do not
patch generated methods. If sqlc is expanded to another dataset, update
`sqlc.yaml`, generated-path verification in `Makefile`, and tests in the same
change.

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
  webhook/signing secrets use `internal/security/secretbox/` through
  `internal/application/notification/service.go`, while administrator
  passwords are stored only as Argon2id hashes produced by
  `internal/security/password/argon2id.go`. Mihomo subscription URLs are a
  current exception: `internal/application/mihomo/subscriptions.go` writes
  `url_plaintext` in the private core database and migrates the older encrypted
  field to plaintext;
  `internal/application/mihomo/subscriptions_integration_test.go` explicitly
  protects that behavior. Do not claim universal at-rest encryption, log or
  publish any of these values, or change their storage contract without an
  explicit migration and corresponding tests.
- Database files, WAL/SHM files, copied fixtures containing real data, and
  Compose `./data` are private runtime artifacts and never repository inputs
  (`docs/privacy-and-publication.md`).

## Verification

Use the narrow store test first, then generation/full checks when applicable:

```bash
go test ./internal/storage/sqlite
make verify-generated
make test
```

Inspect both upgrade and fresh-database behavior. A migration that passes only
on an empty database is incomplete.
