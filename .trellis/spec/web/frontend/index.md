# `@simplus/web` Frontend Guidelines

> Source-backed conventions for the React management application in `web/`.

## Scope

The frontend is a React 19 single-page application built by Umi Max. Explicit
routes feed ProLayout; management pages compose Ant Design 6 and Ant Design Pro
Components. Persistent state belongs to the Go API, and the browser talks to it
through a runtime-validated client whose shared types mostly come from OpenAPI.

The stack and public UI boundary are recorded in
`docs/decisions/0009-ant-design-pro-web.md` and the Web section of
`docs/architecture.md`. These guides add the concrete patterns found in current
source and tests.

## Pre-Development Checklist

- Read `docs/architecture.md` and
  `docs/decisions/0009-ant-design-pro-web.md` for the product/UI boundary.
- Read [Directory Structure](./directory-structure.md) and the focused
  component, hook, state, or type guide below for every affected surface.
- Start public contract work at `api/openapi.yaml`; inspect
  `web/src/api/client.ts` and [Type Safety](./type-safety.md) before changing a
  request, response, or runtime guard.
- Find the nearest page or feature test before changing a visible workflow or
  deterministic transform. Read [Quality Guidelines](./quality-guidelines.md)
  for the established test shapes and required checks.

## Guide Index

| Guide | Project-specific contract |
| --- | --- |
| [Directory Structure](./directory-structure.md) | Explicit Umi routes, page and feature placement, imports, and generated files |
| [Component Guidelines](./component-guidelines.md) | Pro Component composition, local typed view helpers, responsive UI, and sensitive displays |
| [Hook Guidelines](./hook-guidelines.md) | Page-local hooks, manual loading, polling cleanup, and current React Query non-use |
| [State Management](./state-management.md) | Backend authority, Umi bootstrap state, page snapshots, operation state, and synchronization |
| [Quality Guidelines](./quality-guidelines.md) | Vitest/Testing Library patterns, required boundaries, checks, and actual formatting status |
| [Type Safety](./type-safety.md) | Strict TypeScript, OpenAPI aliases and current exceptions, runtime guards, exhaustive mappings, and assertion limits |

## Architecture at a Glance

```text
web/config/routes.ts
        │
        ▼
web/src/app.tsx (auth/setup + ProLayout)
        │
        ▼
web/src/pages/*.tsx ──► web/src/{messages,calls,mihomo} (non-visual feature logic)
        │
        ▼
web/src/api/client.ts (requests, CSRF, errors, runtime validation)
        │
        ├──► web/src/api/hardwareSchema.ts (topology invariants)
        └──► web/src/api/schema.d.ts (generated OpenAPI types)
```

## Core Decisions

- Add routes explicitly in `web/config/routes.ts`; routed screens default-export
  a page component from `web/src/pages/`.
- Build management UI with existing Pro primitives (`PageContainer`,
  `ProTable`, `ProForm`, `ProCard`, and `ProDescriptions`) and Ant Design's
  responsive/layout components.
- Keep reusable non-visual logic in a feature folder with a co-located test;
  keep one-page view helpers in the page.
- Keep state local to its route unless it is Umi bootstrap/layout state.
  Current source has no global store, custom hooks, or React Query cache.
- Never fetch from a component. Extend `web/src/api/client.ts`, reuse generated
  `components['schemas']` types, validate successful response data at runtime,
  and keep constrained request guards at that boundary.
- Keep UI capability-driven and limited to product terms. Do not expose or
  branch on model-specific commands, Agent protocols, device paths, or private
  identity.
- Protect visible workflows and boundary invariants with co-located Vitest
  tests. Page tests use Testing Library; deterministic feature logic uses plain
  unit tests.

## Quality Check

- Routes, page placement, Pro component composition, and responsive behavior
  still follow the linked directory/component guides.
- Pages do not call `fetch` or redefine API payloads; generated types and
  successful-response runtime guards remain owned by `web/src/api/`.
- Server snapshots remain backend-authoritative, async failures are visible,
  and polling/timers clean up without broadening a narrow status refresh.
- The UI remains capability-driven and does not expose Agent commands,
  hardware paths, private identity, or model-specific branching.
- The nearest co-located test covers the changed workflow or boundary, and the
  focused Web checks below pass.

## Verification

Run focused Web checks from the repository root:

```bash
corepack pnpm --dir web test
corepack pnpm --dir web typecheck
corepack pnpm --dir web build
```

When `api/openapi.yaml` or generated types change, also run:

```bash
make verify-generated
```

The repository CI runs broader `make test`, `make lint`, security, generation,
and build targets. `make lint` currently covers Go and GitHub Actions only;
there is no frontend lint/format command to substitute for the Web tests and
strict type-check.

## Evidence Used

The most representative implementation files are:

- `web/src/pages/Lines.tsx` and `Lines.test.tsx`
- `web/src/pages/Modems.tsx` and `Modems.test.tsx`
- `web/src/pages/Mihomo.tsx`
- `web/src/api/client.ts`, `hardwareSchema.ts`, and `client.test.ts`
- `web/src/messages/order.ts`, `status.ts`, and their tests
- `web/src/app.tsx`, `config/routes.ts`, and `global.css`

These guidelines describe the repository at bootstrap time. Formatting and
form typing are explicitly noted where current source is mixed rather than
being presented as an already-enforced ideal.
