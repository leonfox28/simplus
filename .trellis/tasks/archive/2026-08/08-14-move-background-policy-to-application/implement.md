# Implementation plan: Move background policy into application coordinators

## Phase 1 — Messaging coordinator

- [x] Add the consumer-owned inbound sync, notification and realtime publisher ports beside `internal/application/messaging`.
- [x] Implement required-dependency construction, one-cycle interpretation/publication/notification ordering, typed operational reporting and the existing immediate-run/interval/retry loop.
- [x] Add same-package fake-port tests for durable-change classification, attention, notification conditions/failure, partial progress with synchronization error, exact ordering, backoff and cancellation.

## Phase 2 — Inventory Agent-change coordinator

- [x] Add a narrow Agent change source and realtime publisher beside `internal/application/inventory` without widening the existing Snapshot/Probe `AgentClient`.
- [x] Implement snapshot/reconnect/change topic mapping, typed failure reporting, retry reset/backoff and cancellation with unchanged watch parameters.
- [x] Add deterministic fake-source tests for changed, restarted, generation-changed, unchanged, error, retry-cap/reset and cancellation paths.

## Phase 3 — Composition root

- [x] Construct both coordinators in `cmd/simplusd`, preserve current startup/backend conditions and translate their typed reports into the existing operational log messages.
- [x] Delete command-local SMS/Agent policy functions, ports and delay helpers only after equivalent application tests pass.
- [x] Remove migrated policy tests from `cmd/simplusd/main_test.go`; retain unrelated executable-helper and hardware-feature tests.

## Phase 4 — Validation and review

- [x] Run formatting and targeted tests for `internal/application/messaging`, `internal/application/inventory` and `cmd/simplusd`.
- [x] Run focused dependency/ownership scans, `go vet`/locked lint components available offline, the relevant broader Go suite, `git diff --check` and Trellis task validation.
- [x] Dispatch an independent Trellis check for behavior preservation, cancellation/concurrency, port ownership and V-02 removal; incorporate only in-scope corrections.
- [x] In Phase 3.3, synchronize backend specs with the implemented coordinator contract if the final code establishes reusable guidance not already explicit.

## Independent check evidence

- Reviewed the new coordinators against the pre-extraction `cmd/simplusd` implementations and confirmed partial-result publication, attention classification, notification ordering/isolation, timeout, retry and cancellation behavior are preserved.
- Confirmed Agent snapshot/reconnect/generation state, 25-second watch arguments, three-topic mapping, retry reset/cap and cancellation are preserved.
- Confirmed `cmd/simplusd` now owns construction, goroutine lifecycle and operational log rendering only; the application packages own narrow ports and do not import `log/slog` or concrete notification/realtime/storage implementations.
- Passed `make check-format`, `make lint`, targeted tests and race tests, `go test -count=1 ./cmd/... ./internal/...`, Web Vitest/typecheck, the worktree-manifest regression, `git diff --check` plus untracked-file whitespace inspection, and `task.py validate`.
- The raw `go test ./...` traversal could not enter the local `data/agent` runtime directory due filesystem permissions; the repository-supported Go package scope above passed. `make test` was not run because its simulator-supervisor step starts a service, which this task explicitly forbids.

## Risk and rollback points

- The highest-risk behavior is partial progress with a non-nil synchronization error: durable state must still publish/notify, while the next cycle must use retry delay.
- Notification failure must remain isolated from synchronization retry; `TopicNotifications` still publishes after the attempt.
- Reconnect comparison must retain the previous successful Agent snapshot until the next snapshot is evaluated.
- Tests must not sleep through production backoff windows or use external/network/hardware dependencies.
- No migration or persistent state exists. If a phase regresses behavior, revert only its new coordinator/composition edits before continuing; do not change API, storage, Agent or container contracts to accommodate it.
