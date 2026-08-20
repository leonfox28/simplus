# Forward SMS content to Feishu notifications: technical design

## 1. Boundary and data-flow change

The durable SMS record remains the source of truth. No API, SSE, database, provider wire format, or channel-binding contract changes.

```text
Inbox transport
  -> validate/decode SMS
  -> CreateInboundSMS (durable first-persistence decision)
  -> InboundSyncResult counters + ordered narrow sender/body notification values
  -> messaging.SyncCoordinator singular received-SMS notification operation
  -> notification.Service provider-aware fan-out
       -> Feishu channels: one sender/body text per SMS
       -> non-Feishu channels: existing count summary once per sync cycle
```

`internal/application/messaging` continues to own synchronization result interpretation, notification intent, realtime publication order, timeout, and retry isolation. `internal/application/notification` owns channel filtering, provider-specific rendering/fan-out, delivery, and delivery-status persistence. `cmd/simplusd` remains composition/lifecycle only.

## 2. Messaging result contract

Extend the internal synchronization result with an ordered collection of narrow, transport-neutral received-SMS notification values containing only `Sender` and `Body`. Capture those two fields from the message returned by `CreateInboundSMS` only when `replayed == false`, and aggregate them in the same Line/message traversal order as the existing counters.

The collection and `Persisted` counter describe the same first-persistence events, but the collection is only how one poll reports several independent events; it is not a batch-delivery contract. Replayed/already-known messages never enter it. If transport ACK fails after persistence, retain both the counter and narrow notification value in the partial result so the current notify-before-retry behavior remains intact and the subsequent replay does not duplicate notification.

Do not carry the complete `sms.Message` across the notification boundary: Line ID, message/operation/provider IDs, timestamps, status, and other fields are unnecessary. Keep the sender/body collection inaccessible to composition-root logging where practical; ordinary `SyncReport` logging and realtime publication remain counter-only.

## 3. Consumer-owned notification operation

Narrow the messaging coordinator's `NotificationSender` need from generic event/text rendering to two semantic operations:

- a singular received-SMS content operation accepting only one sender/body value and targeting enabled subscribed Feishu channels;
- a count-summary operation used once per synchronization cycle for enabled subscribed non-Feishu channels, preserving their current behavior.

The concrete notification service owns the provider filtering for both operations. Its existing generic `Notify(event, message)` method remains unchanged for all other callers.

The coordinator retains the existing sequence and scheduling semantics:

1. bounded inbound synchronization;
2. Messages realtime publication, with `sms.received` attention for first persistence;
3. for each newly persisted SMS in discovery order, create a fresh detached 15-second context and invoke the singular content operation exactly once;
4. cancel that per-SMS context, collect a non-sensitive indexed error if present, and continue regardless of success/failure;
5. invoke the non-Feishu count-summary operation once under its own bounded context;
6. publish Notifications after all attempts, including when one or more fail;
7. select retry based only on synchronization error.

The loop is sequential to preserve discovery order and avoid unbounded external concurrency. Fresh contexts make attempts independent: a slow or failed delivery may delay the cycle, but it cannot consume the next SMS's timeout or suppress its call. Joined report errors may contain channel ID and message index only, never sender/body.

## 4. Channel-specific fan-out

For one singular content operation, the notification service applies the existing enabled/event filters for `sms.received` and selects Feishu channels only.

- Each Feishu channel (`provider == feishu`) receives one plain-text delivery per newly persisted SMS, in input order:

  ```text
  [Simplus] 新短信
  发件人：<RemoteAddress>
  内容：
  <Body>
  ```

- The body is not trimmed, truncated, re-encoded, or interpreted as markup. JSON escaping remains the responsibility of the existing provider adapters.
- Both legacy Feishu bot Webhook and Feishu private-app delivery modes use the same formatted text.
- The separate summary operation selects non-Feishu channels only and sends the current single `[Simplus] 收到 N 条新短信` text once per synchronization cycle; it never receives the SMS body through this change.
- A Feishu failure is scoped to that individual SMS attempt. It does not stop the coordinator from invoking the singular operation for later messages.
- Errors use non-sensitive channel/message-index context. No sender/body, credential, target, or provider response is added to returned error text.

Existing per-delivery status recording remains authoritative: the channel row describes the latest individual attempt, so a later success may replace an earlier failure and vice versa. There is no aggregate batch status because there is no batch notification transaction. No persistent outbox or notification retry mechanism is introduced.

## 5. Compatibility, privacy, and rollback

- No OpenAPI/generated code, Web UI, SQLite schema/query/migration, SSE payload, event-kind list, channel configuration, ciphertext label, or provider endpoint changes.
- No SMS body appears in public API/SSE/logs/errors; it leaves the process only through the user-configured Feishu outbound channel requested by this feature.
- WeCom retains the existing count-only notification. Other event kinds and explicit channel tests retain the generic path.
- Independent fresh deadlines can lengthen one synchronization cycle during a large provider outage; this is accepted in preference to sharing a timeout or starting unbounded goroutines. Failed individual notifications are not automatically replayed.
- The background-coordination scenario in `.trellis/spec/core/backend/application-boundaries.md` currently describes one count-summary delivery under one timeout. Update that executable project contract after implementation to describe the approved independent per-SMS Feishu attempts and preserved non-Feishu summary.
- Rollback is source-only: restore the counter-only result/port and generic summary call. No data migration or external cleanup is needed.
- Verification is synthetic and must not send a real SMS or Feishu message, access hardware, alter RF/SIM/modem state, or inspect private runtime evidence.

## 6. Verification design

- Messaging service tests prove first persistence returns the exact narrow sender/body values, replay omits them, multipart completion returns the assembled body, and partial ACK failure retains the notification value without exposing unrelated message fields.
- Coordinator fake-port tests prove two discovered SMS values cause two singular calls with fresh detached deadlines; failure/timeout of the first does not suppress the second; replay/ack-only results do not notify; failures still publish Notifications and do not select retry.
- Notification service recording-fake tests prove each singular SMS operation yields one exact message for Feishu Webhook and Feishu app modes, multiline/Unicode bodies survive intact, the separate non-Feishu operation emits one count summary, filters remain effective, latest-attempt status is preserved, and failures do not leak message content in errors.
- Run targeted package tests first, then race/full safe checks and generated-drift/worktree checks required by the backend quality guide.
