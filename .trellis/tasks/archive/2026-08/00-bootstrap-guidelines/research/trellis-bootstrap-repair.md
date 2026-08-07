# Trellis Bootstrap Repair Audit

## Summary

The Trellis runtime and Codex integration are present and operational, but the
auto-detected package configuration is internally inconsistent and covers only
the pnpm child workspace. The repair should preserve the working integration,
correct package metadata, add source-backed root specs, and recover the
repository rules displaced from `AGENTS.md`.

## Verified Configuration Findings

- `.trellis/config.yaml:161-164` declares package key `web` at path `web`, but
  sets `default_package: @simplus/web`.
- `.trellis/scripts/common/config.py:495-504` validates a package by exact key.
- `.trellis/scripts/common/config.py:507-546` resolves the default only when it
  exactly matches a configured key; there is no npm-name translation.
- `.trellis/scripts/common/packages_context.py:106-120` likewise marks a
  package default only when `pkg_name == default_package`.
- Observed `get_context.py --mode packages --json` output reports monorepo mode,
  one package named `web`, `default: false`, and
  `defaultPackage: "@simplus/web"`.
- `.trellis/tasks/00-bootstrap-guidelines/task.json:9` records `package: null`.

## Verified Repository Boundaries

- `go.mod:1-3` defines `github.com/leonfox28/simplus` with Go 1.26 at the
  repository root.
- `pnpm-workspace.yaml:1-2` defines `web` as the sole Node workspace.
- `web/package.json` owns the React/Umi package, TypeScript check, build, and
  Vitest suite. Its Trellis frontend specs are already populated.
- `cmd/` owns executable assembly (`simplusd`, `simplus-agent`,
  `simplus-netd`, control/HIL helpers).
- `internal/application/` contains typed application services and ports;
  `internal/api/httpapi/` owns public HTTP/auth/timeout/error boundaries;
  `internal/agentapi/` owns the bounded Unix hardware protocol.
- `internal/modemadapter/`, `internal/hardwareprobe/`, and
  `internal/attransport/` implement the typed/fail-closed hardware boundary.
- `internal/storage/sqlite/`, its migrations, `sqlc.yaml`, and generated
  packages define persistence ownership.
- `Makefile`, `Dockerfile`, `compose.yaml`, `containers/`, `packaging/`, and
  `scripts/` define build, deployment, and HIL/release operations.
- `docs/README.md`, `docs/product.md`, `docs/architecture.md`,
  `docs/development.md`, `docs/privacy-and-publication.md`, and
  `docs/decisions/` are the canonical project knowledge sources.

## Representative Evidence for Specs

### Backend and contracts

- `internal/application/messaging/service.go`: narrow repository/transport
  ports, typed commands/results, serial gates, validation, and stable errors.
- `internal/api/httpapi/server.go`: OpenAPI server interface, narrow manager
  ports, auth/CSRF, trusted-LAN authority checks, timeouts, panic recovery, and
  JSON error mapping.
- `api/openapi.yaml`, `internal/api/openapi/generate.go`, and
  `internal/api/openapi/spec_test.go`: API source-of-truth and generated drift
  checks.
- `internal/agentapi/server.go` and protocol tests: bounded typed hardware
  operations and peer/error handling.
- `internal/modemadapter/registry.go` plus registry/model tests: model adapters,
  capability evidence, explicit endpoint roles, ambiguous-match rejection,
  and fail-closed business capability advertisement.

### Storage

- `internal/storage/sqlite/store.go`, domain store files, and co-located tests:
  transaction/store conventions and persistence behavior.
- `internal/storage/sqlite/migrations/**`: domain-scoped migration sequences.
- `internal/storage/sqlite/generated/**` and `sqlc.yaml`: generated ownership;
  generated files must not be hand-edited.
- Privacy-sensitive identity and state behavior is defined in
  `docs/architecture.md` and enforced by storage/modem tests.

### Infrastructure and safety

- `Makefile:45-231`: locked development, generation, docs, formatting, lint,
  test, security, build, container, simulator, and hardware targets.
- `compose.yaml`, `Dockerfile`, `containers/*`, and
  `internal/containercontract/contract_test.go`: three-image production and
  privilege boundary contracts.
- `docs/development.md`: ordinary native development versus privileged/hardware
  and release/HIL workflows.
- `docs/architecture.md`: control/agent/netd responsibility split, typed device
  boundary, stable Line identity, and fail-closed rules.
- `docs/privacy-and-publication.md`: prohibited public artifacts and safe
  publication flow.

## Instruction and Hook Findings

- Git `HEAD:AGENTS.md` contains the repository guide, product-scope exclusions,
  sensitive-data restrictions, explicit approval requirements for real side
  effects/HIL, and risk-proportional verification guidance. The user does not
  want that scattered guide restored verbatim and delegated the retention
  decision. Detailed guidance belongs in specs; privacy and real-side-effect
  authorization remain universal safety constraints.
- Current `AGENTS.md` contains only the Trellis-managed block, even though that
  block states content outside it is preserved on future updates.
- `.codex/hooks.json` registers `UserPromptSubmit` workflow-state injection and
  `SubagentStart` context injection for the three Trellis agents.
- `.codex/config.toml` keeps `AGENTS.md` as the project instruction file and
  caps agent recursion at one level. The current session demonstrates that the
  workflow-state and active-task mechanisms are functioning.
- No evidence currently justifies changing `.codex/` hook or agent files.

## Repair Constraints

- Do not re-run initialization or edit Trellis upstream/global packages.
- Do not edit `.trellis/.template-hashes.json` or runtime session state.
- Do not modify application code while producing specs.
- Keep `web` stable; add `core` and default to it.
- Keep `AGENTS.md` Trellis-first; retain only a concise user-owned safety
  section outside the generated block and migrate detailed knowledge to specs.
- Populate specs from the evidence above, then independently verify every
  important claim against the underlying source.
