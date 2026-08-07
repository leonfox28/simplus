# Technical Design: Trellis Bootstrap Repair

## 1. Boundaries

The repair changes only local Trellis configuration/spec/task material and the
repository instruction entry point:

- `.trellis/config.yaml`
- `.trellis/spec/core/**`
- `.trellis/tasks/00-bootstrap-guidelines/**`
- `AGENTS.md`

Existing generated Trellis runtime/scripts, `.codex/` integration files,
application code, and the completed `.trellis/spec/web/frontend/**` tree are
read-only unless verification proves a concrete in-scope inconsistency.

## 2. Package and Spec Model

Use stable Trellis identifiers rather than language package display names:

```yaml
packages:
  core:
    path: .
  web:
    path: web
default_package: core
```

`core` represents root-owned Go, operational, and documentation work. `web`
continues to represent the pnpm workspace. The root path physically contains
`web/`, but Trellis package selection is task metadata rather than an automatic
filesystem ownership detector. Cross-package work must therefore curate both
packages' relevant specs explicitly in task manifests.

The spec tree becomes:

```text
.trellis/spec/
├── core/
│   ├── backend/
│   ├── infra/
│   └── docs/
├── web/
│   └── frontend/
└── guides/
```

Each new layer owns an `index.md` that states applicability, pre-development
checks, quality checks, and links to focused source-backed guides.

## 3. Root Spec Contents

### Backend

- `directory-structure.md`: `cmd/`, `internal/`, `api/`, package ownership,
  generated boundaries, and test co-location.
- `application-boundaries.md`: typed ports, application services, agent/netd
  privilege boundaries, stable business identifiers, and fail-closed rules.
- `api-contracts.md`: OpenAPI-first public HTTP types, bounded internal Unix
  protocols, authentication/CSRF/error mapping, and generation checks.
- `storage-and-migrations.md`: SQLite stores, sqlc/generated files, migration
  ownership, privacy-sensitive persistence, and migration tests.
- `quality-and-testing.md`: Go test style, fixture/integration evidence,
  targeted-to-broad validation, formatting/lint, and generated checks.

### Infrastructure

- `build-and-generated-files.md`: Make targets, locked toolchain, generated
  files, third-party artifacts, and safe verification paths.
- `containers-and-privileges.md`: three-container production boundary,
  fixed-user/Unix-socket contracts, container validation, and rollback shape.
- `hardware-and-hil-safety.md`: typed hardware capability boundary, explicit
  approval requirements, read-only HIL-0 allowance, sensitive-data handling,
  and prohibited arbitrary device commands.

### Documentation

- `documentation-ownership.md`: `docs/README.md` map, canonical document
  updates, ADR and active-plan rules, progressive disclosure, and checks.
- `privacy-and-publication.md`: public/private evidence boundary and safe
  compatibility/troubleshooting publication flow.

File names may be merged only if the implementing agent proves that separation
would duplicate the same rules. The three layer indexes are mandatory.

## 4. Trellis-First Instruction Strategy

`AGENTS.md` uses two ownership zones:

1. The existing Trellis-managed block remains at the top and may be refreshed
   by `trellis update`.
2. A short user-owned `Simplus Safety Boundaries` section follows the closing
   marker and remains preserved across updates.

The old repository guide is not copied back wholesale. Its documentation map,
product scope, architecture, privacy details, and validation conventions move
to the appropriate `core` specs. Only rules that must apply before task/package
selection stay in `AGENTS.md`:

- do not publish credentials, private endpoints/topology, SIM/device identity,
  raw HIL evidence, packet captures, or private troubleshooting material;
- require explicit user approval for real messaging/calls, RF changes,
  modem-persistent writes, or HIL-1/2 actions;
- permit relevant read-only HIL-0 inspection, but never expose arbitrary
  AT/QMI commands or device paths through Web/API boundaries;
- scope validation and repairs to the requested change; a failing check does
  not authorize unrelated modifications.

This keeps Trellis as the primary instruction system, avoids a duplicated
development guide, and preserves the small set of universal safety constraints
that must be visible even when no package spec has been selected yet.

## 5. Context Flow

The intended future flow is:

```text
task create (no --package)
  -> resolve_package()
  -> package = core
  -> planning reads get_context --mode packages
  -> implement.jsonl/check.jsonl select relevant core/web specs
  -> Codex SubagentStart hook injects manifests + task artifacts
  -> trellis-implement writes
  -> trellis-check reviews and fixes
```

An explicit `--package web` remains valid. Existing tasks are not migrated
automatically; the current special bootstrap task may retain `package: null`
because its curated manifests explicitly span configuration and both spec
areas.

## 6. Compatibility and Template Updates

- Do not rename `web`; existing paths and completed specs remain valid.
- Do not edit `.trellis/.template-hashes.json`. Local customization is expected
  to be detected as user-modified by a future `trellis update`.
- Do not reinitialize. The local project files are the supported customization
  surface.
- Do not add `session.spec_scope`; its current absence lets planning discover
  all configured packages. Task manifests provide the precise injection scope.

## 7. Validation and Rollback

Validation must prove configuration behavior, content quality, hook syntax,
and repository health. The implementation plan lists exact commands.

Rollback is file-local and does not touch application state: revert the
configuration stanza, remove the newly created `core` spec tree, and restore
the pre-repair Trellis-only `AGENTS.md`. No database, dependency, generated
product output, or runtime migration is involved.
