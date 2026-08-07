# Repair and Complete the Trellis Bootstrap

## Goal

Bring the local Trellis installation to the functional state expected from a
correct initialization: future work must resolve to a valid package, discover
the real project-specific specs for both the Go root and the Web workspace, and
retain the repository's pre-existing safety and development instructions.

## Background and Confirmed Facts

- The repository is a polyglot monorepo: `go.mod:1` defines the Go module at
  the repository root, while `pnpm-workspace.yaml:1-2` declares `web/` as the
  Node workspace.
- `.trellis/config.yaml:161-164` declares only the `web` package and sets
  `default_package: @simplus/web`. Trellis validates a default against package
  keys, so that npm package name does not resolve to the configured `web` key
  (`.trellis/scripts/common/config.py:507-546`).
- Package context currently reports `web` with `default: false`; the bootstrap
  task consequently has `package: null`.
- The seven source-backed Web frontend specs are complete and independently
  checked. Web tests, type-check, docs checks, and lint passed.
- `trellis init` replaced the previous `AGENTS.md` content with the Trellis
  managed block. The previous guide is recoverable from Git `HEAD`; most of
  its detailed process guidance belongs in Trellis specs, while its privacy
  and real-hardware authorization rules must remain visible before any
  package-specific task context is selected.
- Codex project config, workflow-state hook, sub-agent context hook, and Trellis
  agent definitions exist. The current session has demonstrated active-task
  resolution and implement/check dispatch.

## Requirements

### R1. Correct package resolution

- Keep the stable `web` package key mapped to `web/`.
- Add a stable `core` package key mapped to the repository root (`.`).
- Set `default_package: core` so an unspecified future task resolves to an
  existing package key.
- Do not rename or move the existing Web spec tree.

### R2. Bootstrap the root package specs from repository evidence

Create project-specific, placeholder-free specs beneath `.trellis/spec/core/`
for these actual ownership layers:

- `backend`: Go package layout, application/port boundaries, HTTP/OpenAPI
  contracts, SQLite migrations/generated code, error handling, and testing.
- `infra`: build/release commands, containers and privilege separation,
  hardware/HIL authorization boundaries, generated artifacts, and validation.
- `docs`: canonical documentation ownership, decision records, public/private
  information boundaries, and documentation checks.

Every material rule must cite representative source, test, config, or canonical
documentation paths. The specs must describe current behavior, including
known limitations, rather than inventing aspirational architecture.

### R3. Keep a Trellis-first instruction entry point without losing safety

- Preserve the current `<!-- TRELLIS:START -->` / `<!-- TRELLIS:END -->`
  managed block exactly as the Trellis-owned section.
- Do not restore the prior Git `HEAD` guide verbatim. Replace its scattered
  process and architecture guidance with the new package/layer specs.
- Add only a concise user-owned safety section outside the managed block so
  future `trellis update` operations preserve it. It must keep these always-on
  constraints: never publish sensitive hardware/network/SIM evidence; require
  explicit approval for real SMS/calls/RF changes/device writes/HIL-1/2; allow
  only relevant read-only HIL-0 inspection; never expose arbitrary AT/QMI or
  device paths; and do not expand into unrelated fixes after a failed check.
- Put product scope, documentation ownership, detailed privacy rules, hardware
  architecture, and executable validation conventions in `.trellis/spec/`.
- Keep `AGENTS.md` concise and Trellis-first rather than maintaining a second
  complete development guide.

### R4. Preserve and verify context injection

- Keep the Codex hook and agent wiring aligned with the current Trellis
  workflow; do not modify it unless a concrete defect is demonstrated.
- Curate this task's `implement.jsonl` and `check.jsonl` with real spec/research
  entries so the repair itself exercises the normal sub-agent context path.
- Confirm future task/package resolution and spec discovery using the local
  Trellis scripts, not by relying on visual inspection alone.

### R5. Preserve unrelated work

- Do not modify product source, tests, generated API/SQL code, dependencies, or
  runtime data as part of this repair.
- Do not hand-edit `.trellis/.template-hashes.json` or `.trellis/.runtime/`.
- Do not re-run `trellis init` and do not modify the global Trellis package.
- Keep all pre-existing uncommitted initialization files unless a verified
  repair explicitly targets them.

## Acceptance Criteria

- [x] All seven `.trellis/spec/web/frontend/` guides contain source-backed
  conventions and real examples with no template placeholders.
- [x] The Web spec pass has passed independent review, 73 tests, type-check,
  `make check-docs`, and `make lint`.
- [x] `get_context.py --mode packages --json` reports both `core` and `web`,
  with `core` marked as the sole default.
- [x] Trellis package resolution returns `core` when no explicit package is
  supplied and accepts `web` when explicitly selected.
- [x] `.trellis/spec/core/{backend,infra,docs}/index.md` exist, match their
  final linked file sets, and all linked specs contain verified project paths,
  examples, anti-patterns, and checks without placeholder prose.
- [x] `AGENTS.md` keeps the Trellis managed block as its primary content and
  adds only the concise always-on Simplus safety section outside it; the old
  repository guide is not restored verbatim and its detailed rules are covered
  by the new root specs.
- [x] Codex hook JSON/TOML and Python entry points remain syntactically valid;
  an implement and check sub-agent receive this task's artifacts/manifests.
- [x] Spec links, code fences, cited repository paths, and task manifests all
  validate.
- [x] `make check-docs`, `make lint`, `make test`, and the Web type-check pass;
  any pre-existing non-fatal warnings are reported rather than hidden.
- [x] No application source, dependency, generated product file, runtime state,
  or template hash is changed by the repair.

## Out of Scope

- Fixing Trellis CLI upstream auto-detection or opening an upstream issue/PR.
- Product features, application refactors, dependency upgrades, database
  migrations, hardware actions, HIL execution, deployment, or remote pushes.
- Reorganizing the existing Web source or rewriting the already reviewed Web
  specs except for a narrowly required cross-link correction.
