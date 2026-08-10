# Repository evidence — SMS record ordering

## Confirmed data flow

1. `internal/application/messaging/service.go:281-285` assigns outbound `CreatedAt` from the local clock before `CreateOutboundSMS` and before transport dispatch.
2. `internal/application/messaging/inbound.go:229-237` preserves provider/modem `receivedAt` as inbound `CreatedAt`, while assigning the local persistence clock to `UpdatedAt` in the same create request.
3. `internal/storage/sqlite/messages.go:316-459` currently orders all message pages, conversation last messages, and last outbound Line by `created_at_unix_ms` plus random stable `message_id`.
4. `web/src/messages/order.ts:3-11` independently reconstructs chronological display order from the same fields.
5. `internal/application/messaging/service.go` never advances inbound status after insertion; outbound status mutations change `UpdatedAt`. Therefore the recoverable first-persistence proxy for v7 history is `CASE direction WHEN 'inbound' THEN updated_at_unix_ms ELSE created_at_unix_ms END`.

## Sanitized defect evidence

A read-only local query confirmed the reported shape without recording any number, Line, message ID, provider ID, body, screenshot, or database copy:

- inbound provider time was at the whole-second boundary;
- outbound local creation was about one tenth of a second later;
- outbound completion happened next;
- inbound local persistence happened last.

This proves that `createdAt` describes two different clocks/precisions and is not a valid causal ordering key. The persisted local observation order is the correct product truth for chat placement.

## Existing contracts to preserve

- ADR 0023 owns exact remote-address conversations, cross-Line history, unread markers, read-through tokens, and the current created-time cursor decision.
- `internal/domain/pagination/cursor.go` v1 is shared by Calls and SMS. SMS must gain a distinct version/ordering mode without changing Calls semantics.
- Messages schema v7 has `sms_message_unread` referencing `sms_messages(message_id)` and relies on preserved AUTOINCREMENT unread IDs. A v8 table rebuild must copy this ledger and its IDs before removing the old table.
- The Web uses generated infinite queries; SSE only invalidates/refetches authoritative HTTP pages. No new SSE field is needed.
- Current history is capped at 2000 records, but pagination still requires matching SQLite indexes and strict keyset behavior.

## Recommended implementation shape

- Rebuild `sms_messages` in v8 with `record_sequence INTEGER PRIMARY KEY AUTOINCREMENT` and retain `message_id` as a unique public/business identifier.
- Backfill sequence in recoverable local-persistence order, recreate unread foreign keys and sequence-aware global/remote/Line indexes, and provide a lossless v8 Down path to v7.
- Add a versioned SMS sequence cursor while retaining v1 decode compatibility for in-flight clients; keep Calls on v1.
- Return SMS pages newest-first by sequence. The Web should preserve server order and reverse the flattened pages for oldest-first rendering instead of sorting by timestamps.
- Record the durable correction in a new ADR that narrowly supersedes ADR 0023's `(createdAt, message ID)` SMS ordering clauses.
