# Forward SMS content to Feishu notifications: implementation plan

## 1. Carry newly persisted SMS records through synchronization

- [x] Extend the internal synchronization result with ordered newly persisted notification values containing only sender/body while retaining existing counters; do not expose the complete `sms.Message` to notification or logging boundaries.
- [x] Capture the narrow value from the repository-returned inbound message only on first persistence and aggregate it through message, Line, and overall synchronization results.
- [x] Preserve partial-progress behavior when acknowledgement fails and ensure replay/already-known records do not re-enter the notification slice.
- [x] Add/adjust co-located messaging service tests for single, multiple, replay, multipart, and partial-ACK cases without hardware.

## 2. Introduce received-SMS notification intent at the coordinator boundary

- [x] Replace the coordinator's generic event/rendered-text dependency with a singular received-SMS content operation plus a non-Feishu count-summary operation, leaving provider selection in the notification owner.
- [x] Loop over newly persisted values in discovery order; create and cancel a fresh detached 15-second context for each singular call, continue after every failure, and join only non-sensitive indexed errors.
- [x] Invoke the non-Feishu summary once per synchronization cycle under a separate bounded context.
- [x] Preserve Messages → notification → Notifications ordering, attention, partial-result behavior, error reporting, and sync-only retry policy.
- [x] Update coordinator fakes/tests to assert exact ordered sender/body handoff, distinct per-message deadlines, first-failure/second-attempt behavior, and no replay/ack-only notification.

## 3. Render and fan out inside the notification owner

- [x] Add the singular Feishu-content and non-Feishu-summary operations to `internal/application/notification` while leaving generic `Notify` unchanged for all other events/callers.
- [x] Format one bounded plain-text Feishu delivery per SMS as `[Simplus] 新短信`, sender, and complete body; use it for both Feishu Webhook and Feishu private-app channels.
- [x] Preserve one count-only summary per cycle for enabled subscribed non-Feishu channels.
- [x] Keep each SMS attempt independent, join failures without sender/body or credential leakage, and retain last-individual-attempt channel status rather than inventing aggregate batch status.
- [x] Add recording-fake tests for both Feishu delivery modes, sequential independent calls, Unicode/multiline body preservation, filtering, WeCom compatibility, latest-attempt status, and safe failure behavior.

## 4. Validate compatibility and safety

- [x] Run `gofmt` on changed Go files and `git diff --check`.
- [x] Run `go test -count=1 ./internal/application/messaging ./internal/application/notification`.
- [x] Run the affected packages under `go test -race`.
- [x] Run `make check-format`, `make verify-generated`, `make lint`, and `make test` as the safe full regression ladder.
- [x] Confirm diffs contain no API/generated/schema/Web/SSE/log-content/provider-endpoint changes and no real communication/HIL evidence.
- [x] Complete an independent Trellis quality review against the PRD/design and address verified findings only.
- [x] Use the Trellis spec-update step to revise `.trellis/spec/core/backend/application-boundaries.md` from the old one-summary/one-timeout contract to the approved independent per-SMS Feishu attempt contract, while preserving the non-Feishu summary and retry-isolation rules.

## Rollback points

- The change is source-only. Reverting the messaging result field, specialized coordinator port, notification fan-out, and their tests restores the previous count-only behavior.
- No migration, persistent data rewrite, external provider setup, or hardware action is involved.
