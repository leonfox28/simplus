# Design: Move background policy into application coordinators

## 1. Boundary decision

The repair uses two owner-specific coordinators rather than a shared background framework:

- `internal/application/messaging` owns interpretation of `InboundSyncResult`, notification intent and message-related realtime publication.
- `internal/application/inventory` owns interpretation of typed Agent snapshot/change envelopes and inventory-related realtime invalidation.
- `cmd/simplusd` constructs both coordinators, supplies concrete services/Hub, starts their goroutines, supplies configured intervals and renders operational logs.

This keeps dependencies next to their consumers and avoids turning a local extraction into a generic scheduler or event bus. Both application packages may name their own small publisher interface; that deliberate interface duplication preserves consumer ownership while the existing `*realtime.Hub` implements both structurally.

## 2. SMS synchronization coordinator

Add an owner-specific coordinator file under `internal/application/messaging/`. Its intended contract is:

```go
type InboundSyncer interface {
    SyncInbound(context.Context) (InboundSyncResult, error)
}

type NotificationSender interface {
    Notify(context.Context, string, string) error
}

type RealtimePublisher interface {
    Publish([]realtime.Topic, realtime.Attention)
}
```

The coordinator constructor requires all three ports and returns an explicit configuration error for a missing dependency. A typed cycle report/callback carries only the synchronization result plus synchronization/notification errors needed for operational logging. The application package does not import `log/slog`, know the process logger or decide an exit code.

One cycle preserves this order:

1. create a 20-second synchronization context from the run context;
2. call `SyncInbound` and retain both partial result and error;
3. classify a durable message-state change from exactly `Persisted`, `AlreadyKnown`, `OutboundSent`, `OutboundFailed`, and `OutboundUnconfirmed`;
4. publish `TopicMessages`, adding `AttentionSMSReceived` only when `Persisted > 0`;
5. when `Persisted > 0`, deliver event `sms.received` with `[Simplus] 收到 %d 条新短信` under a 15-second `context.WithoutCancel` deadline, then publish `TopicNotifications` regardless of delivery success;
6. expose the typed cycle report to the composition-root logger;
7. use only the `SyncInbound` error to select normal interval versus retry delay.

`Acknowledged` and `OutboundReportsAcknowledged` remain operational counters but do not independently cause publication. Notification failure remains observable but never causes an SMS re-poll/retry.

`Run` performs a cycle immediately, then waits. Intervals below one second normalize to two seconds. Failed synchronization begins at `max(15s, interval*4)`, doubles, caps at five minutes and resets after a successful synchronization. Cancellation must stop a pending wait promptly. Small unexported cycle/classification/delay seams may be used for deterministic same-package tests; no production clock abstraction is required unless it is the smallest way to eliminate real waits.

## 3. Agent change coordinator

Add an owner-specific coordinator file under `internal/application/inventory/`. It defines a separate consumer-owned source rather than widening the existing inventory `AgentClient`, whose current consumers need only Snapshot/Probe:

```go
type AgentChangeSource interface {
    Snapshot(context.Context, bool) (agentapi.Snapshot, error)
    Changes(context.Context, string, uint64, int) (agentapi.ChangeResponse, error)
}

type AgentChangePublisher interface {
    Publish([]realtime.Topic, realtime.Attention)
}
```

The constructor requires both ports. `Run` accepts a typed failure callback that distinguishes snapshot initialization from long-poll failure so `cmd/simplusd` can preserve its existing log messages without importing `slog` into application code.

The state machine remains unchanged:

1. obtain `Snapshot(ctx, false)`;
2. after a reconnect, publish Inventory/Modems/Lines together when instance ID or generation differs from the prior successful snapshot;
3. call `Changes` with current instance ID/generation and a 25-second bounded wait;
4. update the current/previous snapshot and reset retry to one second after every successful response;
5. publish the same three topics when `Changed` is true;
6. on source error, wait using one-second exponential retry capped at 30 seconds;
7. exit promptly when the context is cancelled.

The coordinator does not probe hardware, interpret devices, change the Agent protocol or expose snapshot data to SSE.

## 4. Composition-root change

After the concrete messaging, notification and realtime services exist, `cmd/simplusd` constructs the SMS coordinator and treats an impossible missing-dependency configuration as startup failure. It starts only `go coordinator.Run(ctx, configuredInterval, reportCallback)`.

When a hardware Agent client exists, the root similarly constructs and starts the Agent change coordinator. Simulator assembly remains without an Agent watcher. Report callbacks preserve current warning/info content; construction, logger selection, goroutine ownership and shutdown context remain executable responsibilities.

Delete `runSMSSync`, `publishSMSSyncResult`, the command-local Agent source interface, `runAgentChanges`, both retry helpers and the shared wait helper from `cmd/simplusd/main.go`. Remove their policy tests from `cmd/simplusd/main_test.go` after equivalent owner-package tests exist.

## 5. Compatibility and safety

- No OpenAPI, generated source, HTTP/SSE schema, SQLite migration/query, Agent protocol, configuration key or container file changes.
- Publication remains advisory metadata only; no message, identity or Agent snapshot is added to SSE.
- No notification endpoint, service, Compose stack or hardware is contacted during validation.
- The refactor is rollback-safe as one source change: restoring command-local functions and their tests returns the prior ownership without data migration.

## 6. Verification design

Messaging fake-port tests assert call order and exact outputs for no change, acknowledgement-only, persisted inbound, outbound-only, partial result plus sync error and notification error. Pure delay tests prove initial delay, interval influence, doubling and cap; a cancelled context proves prompt exit.

Inventory fake-port tests assert explicit change, instance restart, generation change, unchanged reconnect, long-poll failure classification, retry reset/cap and cancellation. Test data remains synthetic.

The composition check proves `cmd/simplusd` only constructs/runs coordinators and retains operational log callbacks. Focused scans ensure no V-02 helper remains in `cmd/**` and neither application coordinator imports concrete implementation packages or `log/slog`.
