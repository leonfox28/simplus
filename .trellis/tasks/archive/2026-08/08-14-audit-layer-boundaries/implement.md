# Audit Execution Plan

## Phase A — Freeze scope and build the dependency baseline

- [x] Record the current commit and clean/product-file baseline without reading private runtime data.
- [x] Inventory hand-written production Go, Web, product-owned C and production shell/build source; classify tests, generated/build output, HIL/development tools, locked upstream source, third-party/tool caches and task history separately.
- [x] Build production and test Go import graphs independently and a hand-written Web import/network-ownership inventory.
- [x] Materialize the allowed behavior-edge matrix from `design.md` into the working audit report.

## Phase B — Audit runtime layer behavior

- [x] Review Web pages/components → generated client/runtime → public HTTP and prove raw Fetch/EventSource/payload ownership is confined to allowed boundaries.
- [x] Review public HTTP → application services and identify any direct storage, Agent/supervisor implementation, process, network or hardware behavior.
- [x] Review every `internal/application/**` package for consumer-owned ports, concrete storage/Agent leakage, cross-application coupling, model/path branching and fallback behavior.
- [x] Review storage/filesystem adapters for upward calls or business/protocol ownership, while treating generated sqlc as output.
- [x] Review typed Agent, Mihomo and VoWiFi client/server protocols plus the strongSwan SIM-AKA C bridge for bounded operations, stable inputs and absence of arbitrary command/path payloads.
- [x] Review `cmd/simplusd`, `cmd/simplus-agent` and `cmd/simplus-netd` as composition roots, distinguishing dependency construction from business or protocol implementation.

## Phase C — Audit modem and privileged runtime boundaries

- [x] Search Go/Web/C and production shell/build source for AT/QMI/APDU/vendor strings, tty/termios/serial I/O, `/dev`, sysfs, USB/interface/VID/PID, Unix/socket, shell/process and network primitives.
- [x] Trace every hit outside `internal/modemadapter/**`, model-owned drivers, `internal/attransport`, `internal/hardwareprobe`, and netd-owned workers to determine whether it is allowed runtime ownership or a real bypass.
- [x] Verify discovery/registry code selects adapters and endpoints but does not manufacture commands; verify generic transports frame I/O but do not choose model semantics.
- [x] Inspect OpenAPI and typed Unix route/request definitions for arbitrary command, device path, interface, topology or vendor-payload exposure.
- [x] Scan tests and development/HIL tools separately for reusable escape hatches or architecture drift, without executing them or treating deliberate fixtures as production behavior.

## Phase D — Produce the evidence-backed report

- [x] Create `audit.md` with executive summary, scope, allowed-edge matrix and one coverage row for every runtime owner.
- [x] For each candidate, record current `file:line` anchors and a summarized call chain, then classify it as confirmed violation, architecture concern, allowed exception, test/tool-only observation or false positive.
- [x] Give confirmed issues a severity, impact, minimum remediation direction and safe suggested validation; do not implement repairs.
- [x] Record clean layers explicitly and state static-analysis limits and residual risk.
- [x] Redact or omit any identity, private endpoint/topology, raw HIL/protocol/log material or data-path content encountered incidentally.

## Phase E — Independent verification

- [x] Dispatch a separate Trellis check pass in report-only mode; prohibit product/spec/generated edits and all hardware/runtime actions.
- [x] Verify all production packages and Web runtime owners map to exactly one coverage row, all candidate classes are mutually clear, and every confirmed claim has a valid source anchor plus call chain.
- [x] Re-run safe import/search assertions needed to challenge the report, including direct Fetch/EventSource ownership and modem/device keyword confinement.
- [x] Run `git diff --check` and confirm changed files are confined to `.trellis/tasks/08-14-audit-layer-boundaries/**`.
- [x] Incorporate only task-report corrections, then present the final audit result and any proposed follow-up tasks to the user.

## Phase F — Close the remediation program

- [x] Verify all seven approved child tasks are completed and archived, and
  map every V-01–V-05 / C-01–C-02 finding to its functional commit.
- [x] Add a dated remediation-closure section to `audit.md` while preserving
  the original baseline, evidence classes and historical source anchors.
- [x] Run cumulative source-only checks for all repaired boundaries plus the
  supported Go, lint, generated, documentation and container-contract gates.
- [x] Complete an independent final Trellis check that the closure table and
  current source agree and no audit finding remains open.
- [x] Commit the closure record, archive the parent task and record the session
  journal.

### Phase F validation evidence — 2026-08-18

- All seven child task records are completed under
  `.trellis/tasks/archive/2026-08/**`; their listed functional commits are
  ancestors of the current `HEAD` and match the closure table.
- Focused current-source assertions passed for registry-backed driver
  registration, application-owned background coordinators, explicit Setup and
  Mihomo dependencies, the bounded legacy-Webhook port, HTTP-owned adjacent
  service ports, and absence of ResourceLease vocabulary from application
  source.
- The retained SQLite ResourceGroup lease repository, focused test and
  `00005_resource_group_leases.sql` have identical SHA-256 values at the audit
  baseline, immediately before C-02 and in the current worktree. No migration
  or protected-file diff exists across the remediation program.
- `make check-format`, `go test -count=1 ./cmd/... ./internal/...`, `go vet
  ./cmd/... ./internal/...`, `make lint`, `make verify-generated`, `make
  check-docs`, `make check-container-files`, `go test -count=1
  ./internal/containercontract`, Web typecheck, task validation and `git diff
  --check` passed without external access.
- During independent review, a stricter `GOPROXY=off make lint` repetition
  reached and passed `go vet`, then its pinned `go run ...@v1.7.12` attempted a
  cached-module metadata lookup. The exact cached Actionlint v1.7.12 binary
  passed all workflow files, so the lint contract was still independently
  completed without network access.
- No service, Compose, provider endpoint, private runtime state, hardware,
  modem, RF, SMS/call, SIM/eUICC, network mutation or HIL action was used.
- The closure record was committed as `dce8356`; parent-task archival and the
  session journal are completed by the Trellis finish-work flow.

## Validation commands

Only safe, non-HIL variants are permitted. The exact search expressions may be refined during execution, but the command classes are:

```bash
go list -f '{{.ImportPath}}|{{join .Imports ","}}|{{join .TestImports ","}}|{{join .XTestImports ","}}' ./cmd/... ./internal/...
rg --files cmd internal web/src components containers scripts/release packaging
rg -n '<boundary candidate patterns>' cmd internal web/src components containers scripts/release packaging api/openapi.yaml
git diff --check
git status --short
```

Do not run hardware/deployment Make targets, Compose, HIL commands, device discovery, real communications, RF/SIM/eUICC mutations or commands that read private runtime data.

## Risk and rollback points

- Keyword matches are expected to have a meaningful false-positive rate; no candidate is a finding until the call chain is inspected.
- Composition roots and passive domain types can look like skipped layers in an import graph; classify them through responsibility and behavior.
- Reflection, function injection, build tags and generated wrappers can hide the concrete call target; record any unresolved target as a limitation rather than asserting compliance.
- If an analysis command would touch hardware, network services, private stores or host configuration, stop and replace it with source inspection.
- Since only task-local artifacts are written, rollback is limited to correcting or removing those artifacts. Product code must remain untouched.
