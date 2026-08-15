# Implementation Plan

## Phase A — Make the application dependency explicit

- [x] Add the stable runtime-manager configuration error.
- [x] Replace both constructors with one validated constructor requiring the
  typed supervisor API.
- [x] Update application tests to use a deterministic fake and cover invalid
  dependencies without launching a process.

## Phase B — Move concrete selection to the composition root

- [x] Add a command-owned local/socket supervisor selection helper.
- [x] Update `cmd/simplusd/main.go` to handle selection and application
  construction errors explicitly before continuing assembly.
- [x] Add non-connecting/non-executing command composition tests for both
  branches and invalid paths.

## Phase C — Validate and preserve the contract

- [x] Run focused formatting and tests for `internal/application/mihomo`,
  `internal/mihomosupervisor`, and `cmd/simplusd`, including race coverage.
- [x] Run supported Go vet/lint and broader non-HIL checks proportional to the
  shared constructor change.
- [x] Run focused scans for hidden concrete construction, ignored errors and
  stale constructor names; run task validation and `git diff --check`.
- [x] Update the backend boundary/directory specification after independent
  review.
- [x] Commit the completed child task as `ffbc69b`; archive it through the
  Trellis finish-work flow.

## Prohibited validation

Do not start Mihomo, `simplus-netd`, Compose or deployment services; do not
read private runtime stores; do not run hardware discovery, HIL, SMS/call, RF,
SIM/eUICC or other network/device actions.
