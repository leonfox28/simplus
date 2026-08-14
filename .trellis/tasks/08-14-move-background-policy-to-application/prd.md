# Move background policy into application coordinators

## Goal

Resolve layer-audit finding V-02 by moving SMS synchronization semantics and hardware-Agent change semantics out of `cmd/simplusd` into narrow application-owned coordinators, leaving the executable root responsible only for dependency construction, lifecycle, configuration and operational logging.

## Background

- The layer audit classified V-02 as a confirmed Medium violation. The active anchors are `cmd/simplusd/main.go:281-283,394-553`, with policy tests at `cmd/simplusd/main_test.go:53-99`.
- `runSMSSync` currently interprets `messaging.InboundSyncResult`, chooses realtime topics and attention, synthesizes the inbound-SMS notification, and owns synchronization retry/backoff.
- `runAgentChanges` currently interprets Agent instance/generation changes, chooses the Inventory/Modems/Lines invalidation topics, and owns watch retry/backoff.
- `.trellis/spec/core/backend/directory-structure.md` assigns executable roots only composition and lifecycle; `.trellis/spec/core/backend/api-contracts.md` requires application/background tests for durable realtime publication semantics.
- The user approved the recommended repair and required current behavior to remain unchanged.

## Requirements

- R1. Remove SMS result interpretation, notification decisions, realtime-topic selection, Agent-change interpretation and retry/backoff policy from `cmd/simplusd`.
- R2. Add an application-owned SMS synchronization coordinator beside `internal/application/messaging`. It must consume the smallest interfaces for inbound synchronization, notification delivery and realtime publication; it must not depend on `slog`, HTTP, storage implementations or concrete notification/realtime implementations.
- R3. Preserve the current SMS cycle semantics and ordering: run immediately; bound `SyncInbound` to 20 seconds; publish the Messages topic for `Persisted`, `AlreadyKnown`, `OutboundSent`, `OutboundFailed` or `OutboundUnconfirmed`; attach `sms.received` attention only when `Persisted > 0`; do not publish for acknowledgement-only results; notify only for newly persisted inbound messages with the existing event and text; bound notification delivery to 15 seconds using a context detached from parent cancellation; publish the Notifications topic after that delivery attempt; and let only the synchronization error control retry.
- R4. Preserve the current SMS scheduling policy: normalize intervals below one second to two seconds, reset retry delay after a successful synchronization, otherwise begin at `max(15 seconds, interval * 4)`, double, and cap at five minutes.
- R5. Add an application-owned Agent change coordinator beside `internal/application/inventory`. It must consume a narrow Agent change-source interface plus a narrow realtime publisher and must not depend on `slog` or a concrete Agent client/Hub.
- R6. Preserve the current Agent watch semantics: initialize from a non-probing snapshot; long-poll changes for 25 seconds using the current instance ID and generation; invalidate Inventory, Modems and Lines together for an explicit change or for an instance/generation change observed after reconnect; start retries at one second, double to a 30-second cap, reset after a successful change response, and stop on context cancellation.
- R7. Keep operational log rendering in `cmd/simplusd` through typed coordinator reports/callbacks. Application coordinators decide business/event behavior but do not choose log handlers or process exit behavior.
- R8. Require mandatory coordinator dependencies explicitly at construction. Keep interfaces next to their consumers; do not introduce a generic `background`, `common`, event bus or catch-all service package.
- R9. Move deterministic policy tests from `cmd/simplusd/main_test.go` to the owning application packages and add fake-port coverage for partial progress, notification failure, Agent reconnect/generation changes, backoff caps and cancellation.
- R10. Do not change the public API, SSE payload schema, persistence schema, container/runtime configuration, notification channel behavior, Agent protocol, hardware support or user-visible SMS/Realtime behavior.
- R11. Validation must be synthetic and non-HIL. Do not start services/Compose, inspect private state, contact notification endpoints, access hardware, send SMS/place calls, or perform RF/SIM/eUICC/network mutation.

## Acceptance Criteria

- [x] AC1. `cmd/simplusd` constructs and starts the two coordinators and contains no SMS/Agent business-policy helpers, topic mapping or retry-delay functions from V-02.
- [x] AC2. The SMS coordinator owns the exact durable-change, attention, notification, publication-order, timeout and retry semantics listed in R3-R4 through consumer-owned interfaces.
- [x] AC3. The Agent change coordinator owns the exact snapshot/change, invalidation-topic, reconnect, retry and cancellation semantics listed in R5-R6 through consumer-owned interfaces.
- [x] AC4. Application-package fake tests cover success, partial progress with error, acknowledgement-only results, notification failure, Agent changed/restarted/generation-changed results, retry caps and cancellation without real waits, network or hardware.
- [x] AC5. Existing operational log messages remain available from the composition root, and notification failure still does not cause SMS synchronization retry.
- [x] AC6. No public/API/generated/storage/protocol/container contract changes, generic background package or new lower-layer concrete dependency is introduced.
- [x] AC7. Targeted Go tests, formatting, vet/lint components available offline, full relevant package tests, task-context validation and `git diff --check` pass.
- [x] AC8. An independent Trellis check confirms V-02 is removed without moving policy into another composition or transport boundary.

## Out of Scope

- Changing SMS synchronization, retry or notification product behavior.
- Generalizing all background jobs or refactoring the existing VoWiFi reconciliation callback and Feishu binding callback.
- Repairing audit findings V-03 through V-05 or Low concerns C-01/C-02.
- Changing `realtime.Hub`, SSE transport/authentication/backpressure, notification provider delivery, or Agent wire validation.
- Hardware, deployment, HIL or external-provider validation.
