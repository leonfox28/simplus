# Implementation Plan: Trellis Bootstrap Repair

## 1. Reconfirm the baseline

- [x] Snapshot `git status --porcelain=v1 -uall` and preserve all unrelated
  user/init changes.
- [x] Re-read `prd.md`, `design.md`, the repair research note, current package
  context, `AGENTS.md`, and Git `HEAD:AGENTS.md`.
- [x] Confirm `.codex/hooks.json`, `.codex/config.toml`, and hook scripts exist;
  do not modify them without a demonstrated defect.

## 2. Correct package configuration

- [x] Add `core: {path: .}` while preserving `web: {path: web}`.
- [x] Change `default_package` from `@simplus/web` to `core`.
- [x] Do not add `session.spec_scope` or edit Trellis runtime/template hashes.

Validation gate:

```bash
python3 ./.trellis/scripts/get_context.py --mode packages --json
python3 -c 'from pathlib import Path; from importlib import import_module; import sys; sys.path.insert(0, ".trellis/scripts"); from common.config import resolve_package, validate_package; assert resolve_package(repo_root=Path.cwd()) == "core"; assert validate_package("web", Path.cwd())'
```

## 3. Keep `AGENTS.md` Trellis-first and retain only universal safety

- [x] Preserve the current Trellis managed block exactly.
- [x] Do not restore `git show HEAD:AGENTS.md` verbatim.
- [x] Add one concise user-owned `Simplus Safety Boundaries` section outside
  the managed block containing the privacy, explicit real-side-effect/HIL
  approval, typed-device-boundary, and no-unrelated-repair constraints defined
  in `design.md`.
- [x] Move the old guide's detailed documentation, product, architecture,
  privacy, and validation knowledge into the relevant `core` specs.
- [x] Confirm there is exactly one Trellis block and one short safety section,
  with no duplicated full repository guide.

## 4. Bootstrap root specs

- [x] Create `.trellis/spec/core/backend/` and its source-backed index/guides.
- [x] Create `.trellis/spec/core/infra/` and its source-backed index/guides.
- [x] Create `.trellis/spec/core/docs/` and its source-backed index/guides.
- [x] Use representative code/tests/config/docs from the paths catalogued in
  `research/trellis-bootstrap-repair.md`.
- [x] Include actual patterns, anti-patterns, privacy/safety boundaries, and
  reliable validation commands; remove all template prose.
- [x] Keep existing Web specs unchanged unless a verified cross-link defect
  requires a narrow correction.

## 5. Validate content and context wiring

- [x] Confirm package output lists `core (default)` with `backend`, `docs`, and
  `infra`, plus `web` with `frontend`.
- [x] Validate both task manifests with `task.py validate` / `list-context`.
- [x] Scan for placeholders and broken relative Markdown links.
- [x] Verify every cited repository path exists and every Markdown fence is
  balanced.
- [x] Parse `.codex/hooks.json`, `.trellis/.template-hashes.json`, and relevant
  task JSON; parse `.codex/config.toml` with Python's `tomllib`.
- [x] Syntax-check the Codex hook Python entry points without changing them.
- [x] Have the required `trellis-implement` and final full-scope
  `trellis-check` agents report that injected task artifacts/manifests were
  available.

## 6. Run repository checks

Run in increasing scope and stop to diagnose any attributable failure:

```bash
make check-docs
corepack pnpm --dir web typecheck
make lint
make test
```

- [x] Report existing non-fatal jsdom warnings separately.
- [x] Do not repair unrelated application/test failures without new scope.

Implementation evidence (2026-08-07): all four required commands passed.
Vitest completed 73 tests and emitted only non-fatal jsdom CSS parsing and
pseudo-element `getComputedStyle` warnings. The implement hook supplied all
four manifest entries followed by `prd.md`, `design.md`, and `implement.md`;
the final full-scope check hook likewise supplied all four check-manifest
entries in order followed by `prd.md`, `design.md`, and `implement.md`.

## 7. Final review and handoff

- [x] Re-read the complete diff for lost instructions, generic spec prose,
  stale paths, duplicated sources of truth, or application changes.
- [x] Mark PRD acceptance boxes only after evidence supports them.
- [x] Run the required Trellis spec-sync judgment. The final review's new
  knowledge is already captured in the owning specs: mixed secret/plaintext
  storage in `core/backend/storage-and-migrations.md`, Compose candidate
  maturity in `core/infra/containers-and-privileges.md`, and the required Web
  entry-point checklists in `web/frontend/index.md`. No additional guide or
  duplicate task-only rule is needed.
- [x] Present a scoped commit plan; do not stage, commit, archive, or push
  without the workflow's explicit confirmation gates.

## Rollback Point

Before any commit, rollback consists only of restoring the four in-scope areas
listed in `design.md`; never use destructive Git commands or touch unrelated
uncommitted files.
