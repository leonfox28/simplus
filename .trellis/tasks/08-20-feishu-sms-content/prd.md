# Forward SMS content to Feishu notifications

## Goal

Make a Feishu new-SMS notification useful without opening another interface: send one independent Feishu message per newly received SMS containing its sender and complete body instead of only a generic count reminder.

## Background

- The current user-visible Feishu notification only indicates that new SMS messages exist. Repository evidence confirms why: `InboundSyncResult` exposes counters only, and `SyncCoordinator` renders one count-only message (`internal/application/messaging/inbound.go:18-26`, `internal/application/messaging/sync_coordinator.go:96-106`).
- At successful first persistence, the application already has the transport-neutral sender, body, Line ID, and receive time; replayed/already-known messages are distinguished from newly persisted messages (`internal/application/messaging/inbound.go:245-278`, `internal/domain/sms/message.go:24-38`).
- One synchronization call may discover and persist multiple messages while walking Lines and inbox references; that collection is an ingestion detail, not one notification transaction (`internal/application/messaging/inbound.go:41-70,97-117`).
- A valid inbound body is non-blank and limited to 1,600 Unicode code points / 6,400 bytes, while notification text is limited to 4,000 Unicode code points (`internal/application/messaging/inbound.go:281-288`, `internal/application/notification/service.go:25-30,242-245`).
- The current notification service broadcasts one supplied text string to every enabled channel subscribed to the event; Feishu Webhook and Feishu private-app delivery share that contract (`internal/application/notification/service.go:242-269,336-359`).
- Historical decisions require notification channels to remain outbound-only and automated validation not to contact real Feishu endpoints (`docs/decisions/0005-management-completion-mihomo-notifications.md:23-34`, `docs/decisions/0025-feishu-private-message-binding.md:17-39`).

## Requirements

- **R1 — Feishu content delivery.** Every newly persisted inbound SMS that currently triggers `sms.received` is an independent notification event and must produce one message in each enabled, subscribed Feishu Webhook or Feishu private-app channel.
- **R2 — Agreed text format.** Each Feishu message must identify the transport-neutral sender and preserve the complete SMS body, including valid Unicode and embedded line breaks, in this plain-text shape:

  ```text
  [Simplus] 新短信
  发件人：<sender>
  内容：
  <complete body>
  ```

- **R3 — Independent delivery.** When one synchronization cycle persists multiple SMS messages, schedule one separate Feishu attempt for each in discovery order. Every SMS gets its own detached 15-second delivery context and result; a failure or timeout for one must not suppress later SMS attempts. Do not combine the messages into a shared delivery transaction or truncate their bodies.
- **R4 — Provider compatibility.** Non-Feishu notification providers must retain their existing one-per-cycle `sms.received` summary and must not receive SMS bodies as an incidental side effect.
- **R5 — Replay safety.** Notify only for first persistence. Replayed/already-known transport messages must not create duplicate content notifications, including after a prior transport ACK failure.
- **R6 — Failure isolation and status.** A Feishu delivery failure must not roll back SMS reception/storage, select the SMS synchronization retry schedule, suppress other SMS attempts, or prevent the normal Notifications invalidation after the attempts. Existing channel status remains the result of the latest individual delivery attempt, not an aggregate batch result; failures remain observable in the current synchronization report without including message content.
- **R7 — Privacy and safety.** SMS bodies may leave the process only through the user-configured Feishu outbound delivery requested here. Do not add them to public API, Web, SSE/realtime payloads, ordinary logs, errors, channel views, or unrelated providers; do not expose modem/device controls or private troubleshooting material.

## Acceptance Criteria

- [x] **AC1 / R1-R2.** A newly persisted SMS produces one exact-format sender/body message through both Feishu Webhook and Feishu private-app delivery modes.
- [x] **AC2 / R2.** Synthetic Unicode and multiline bodies reach the Feishu delivery port complete and unmodified.
- [x] **AC3 / R3.** Two or more SMS messages first-persisted in one cycle produce the same number of separate Feishu calls with distinct delivery contexts; an injected failure/timeout for the first does not prevent the second call.
- [x] **AC4 / R4.** An enabled subscribed non-Feishu channel receives its existing single count summary and no SMS body.
- [x] **AC5 / R5.** Replayed/already-known records, including an SMS re-read after persistence succeeded but ACK failed, produce no duplicate content notification.
- [x] **AC6 / R6.** SMS persistence remains successful when one delivery fails; later SMS attempts still run, notification failures remain report-only for scheduling, channel status follows the latest individual attempt, and Notifications invalidation still occurs.
- [x] **AC7 / R7.** Focused privacy assertions and diff inspection find no SMS body in API/Web/SSE/log/error surfaces and no unrelated private or hardware material.
- [x] **AC8.** The background-coordination project spec reflects the approved independent-delivery contract, and co-located synthetic tests, affected-package race tests, formatting, generated-drift checks, lint, and the safe full regression suite pass without real SMS, Feishu, hardware, RF, modem-write, or HIL actions.

## Out of Scope

- Changing non-Feishu notification behavior beyond preserving its current contract.
- Delivery retry/outbox redesign for an individual Feishu failure after the SMS has already been persisted.
- Public API, Web UI, persistence schema, realtime/SSE payload, event-kind, channel-binding, credential, or provider-endpoint changes.
- Sending test SMS messages, making calls, changing RF state, modem-persistent writes, or performing HIL-1/HIL-2 actions.
- Exposing arbitrary AT/QMI commands, device paths, SIM/device identity, raw HIL evidence, packet captures, screenshots, private endpoints, or topology.
