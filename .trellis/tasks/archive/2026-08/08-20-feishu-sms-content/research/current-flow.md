# SMS-to-Feishu current flow

## Current behavior

1. `messaging.Service.SyncInbound` polls every ready Line and returns `InboundSyncResult` counters only (`internal/application/messaging/inbound.go:18-70`).
2. `persistAndAcknowledgeInbound` has the complete persisted `sms.Message`, including `RemoteAddress`, `Body`, `LineID`, and receive time, but discards the repository return value and exposes only `Persisted` / `AlreadyKnown` counters (`internal/application/messaging/inbound.go:244-266`).
3. `SyncCoordinator` interprets `Persisted > 0` as `sms.received`, renders `[Simplus] 收到 N 条新短信`, and calls the generic notification port once (`internal/application/messaging/sync_coordinator.go:83-108`).
4. `notification.Service.Notify` sends the same supplied text to every enabled subscribed channel; Feishu bot Webhook and Feishu private-app channels share this call path (`internal/application/notification/service.go:242-359`).

## Relevant invariants

- First persistence and transport replay are already distinguished. An ACK failure may return partial progress with `Persisted == 1`; the notification still happens once, while the next poll sees `AlreadyKnown` and must not duplicate it (`internal/application/messaging/inbound.go:244-266`; `.trellis/spec/core/backend/application-boundaries.md`, background-coordination scenario).
- Valid inbound bodies are non-blank and bounded to 1,600 runes / 6,400 bytes; senders are bounded to a 20-character numeric or alphanumeric address. The agreed prefix and labels therefore remain below the existing 4,000-rune notification limit (`internal/application/messaging/inbound.go:277-288`, `internal/application/messaging/service.go:57-61`, `internal/application/notification/service.go:25-30`).
- `cmd/simplusd` may construct and run the coordinator, but must not regain SMS notification rendering or channel-selection policy (`.trellis/spec/core/backend/application-boundaries.md`, background-coordination scenario).
- Notification delivery errors remain report-only for SMS scheduling and must not roll back or retry SMS persistence.
- Realtime remains an invalidation/attention channel; SMS content must not enter SSE, logs, public API, or channel views.
- Automated checks use recording fakes/synthetic transports only. No real Feishu call, SMS, RF, modem write, or HIL action is allowed.

## Planning conclusion

- Carry only ordered sender/body notification values alongside the existing counters; do not widen the boundary with complete `sms.Message` records.
- Treat a multi-message polling result as several independent notification events. Invoke a singular Feishu-content operation once per message with a fresh detached deadline, continue after failures, and keep non-Feishu providers on a separate one-per-cycle count summary.
- Let `internal/application/notification` own provider filtering and rendering for both semantic operations; the messaging coordinator owns independent attempt timing/order and failure isolation.
- Keep the existing generic `Notify(event, message)` operation for call, send-failure, degraded-system, HTTP-owned, and test-notification paths.
