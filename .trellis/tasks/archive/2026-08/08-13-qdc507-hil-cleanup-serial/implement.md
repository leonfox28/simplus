# Implementation Plan

## Phase A — Controlled private serial observation

- [x] Confirm branch/upstream and exactly one QDC507 without printing identifiers into task files.
- [x] Restate the five-command read-only allowlist and stop conditions in the interactive update.
- [x] Stop the current Compose modem owner, run a temporary non-repository typed probe for `ATI`, `AT+QGMR`, `AT+CGSN=?`, `AT+CGSN=0`, `AT+CGSN=1`, remove it, then restore Compose.
- [x] Report actual low-sensitivity manufacturer/model/firmware/SN/IMEI only in the direct conversation; persist only bounded support/shape conclusions.

## Phase B — Remove HIL-only source and scaffolding

- [x] Delete `internal/qdc507hil`, all `cmd/simplus-qdc507-hil*`, all `qdc507_hil` tagged files and `build-qdc507-hil-fixture`.
- [x] Delete orphaned HIL-only QDC507 inbox/outbound adapters, routers, target resolvers, dedicated drivers/transports/stores, strict classifier categories, subscriber-number seam and tests.
- [x] Collapse production code only where needed to remove a deleted dependency, preserving the complete production SMS adapter/router/state path.
- [x] Run `rg` assertions proving no `qdc507_hil`, `qdc507hil`, HIL command path or deleted symbol remains outside archived task history.

## Phase C — Typed module serial

- [x] Add the narrow module-serial adapter capability and strict QDC507 `CGSN` parser based on the observed public grammar.
- [x] Add module serial to Agent probe identity and internal physical-device observation without changing USB descriptor serial/fingerprint semantics.
- [x] Prefer module serial for existing Managed Modem/Line `serialNumber`, with USB iSerial fallback and empty unavailable behavior.
- [x] Update OpenAPI descriptions and regenerate owned Go/TypeScript outputs if bytes change.
- [x] Add synthetic success, unsupported, equal-to-IMEI, malformed, overflow/control/terminal, fallback, protocol mapping, application service and Web display tests.

## Phase D — Docs/spec and privacy

- [x] Remove QDC tagged-runner instructions/object graphs from development and architecture docs while retaining sanitized accepted SMS evidence.
- [x] Update compatibility/active plan/handoff only where their current claims become stale.
- [x] Remove deleted-runner scenarios from the HIL safety spec and add the explicitly authorized direct-conversation/repository privacy distinction without actual identifiers.
- [x] Scan the intended tree for the observed SN, IMEI, phone number, SMS body, raw transcript, device path and temporary probe artifacts.

## Phase E — Verification

- [x] Focused: `go test ./internal/modemadapter ./internal/hardwareprobe ./internal/application/inventory ./internal/application/modem ./internal/application/line ./internal/agentapi ./internal/api/httpapi` and QDC SMS packages.
- [x] `make check-format verify-generated check-container-files check-docs`.
- [x] `make lint`, `make test`, Web build, desktop/mobile `make web-e2e`, and `make security`.
- [x] `git diff --check`, no-HIL-source assertion, privacy scan and full diff review.
- [x] Use Trellis check review; fix only scoped findings and rerun affected gates.

## Phase F — Rewrite unpushed history and deploy

- [x] Reconfirm `origin/main..HEAD` is the expected old three-commit sequence and remote is unchanged.
- [x] Reconstruct the desired tree on `origin/main` into a cleaned feature commit, prior task archive and updated prior journal; do not create backup refs and do not push.
- [x] Archive/journal this task after the cleaned feature commit.
- [x] Verify old hashes absent from every ref, expire reflogs, GC/prune now and prove old objects cannot be resolved.
- [x] Build `dev` control/agent/netd images from the new feature revision, validate Compose and update the existing local stack without deleting data.
- [x] Verify `agent/netd/app` healthy, bootstrap exit 0, HTTP health, image revision, clean Git state and no active task.

## Hard stops

- More than one candidate QDC507, a competing unknown owner, an unsupported response requiring a new command, any write/RF/SIM/SMS/data action, private-value persistence, unexpected dirty work, failed full gate, or an unexpected upstream change stops before history rewrite.
- Any failure after moving branch history but before new commits stops before reflog expiry/GC; preserve the working tree and repair the scoped commit reconstruction first.
