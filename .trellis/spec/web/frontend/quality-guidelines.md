# Frontend Quality Guidelines

> Required checks and regression patterns for `@simplus/web`.

## Quality Gate

```bash
corepack pnpm install --frozen-lockfile
corepack pnpm --dir web typecheck
corepack pnpm --dir web test
corepack pnpm --dir web build
corepack pnpm --dir web e2e
make verify-generated
make security
```

The Vite build regenerates API output, type-checks, and builds production
assets. Root `make test` includes Web Vitest/typecheck with the wider Go and
Simulator suite. CI installs the pinned Playwright Chromium and runs the same
synthetic desktop/mobile E2E suite. There is currently no separate frontend
ESLint/formatter target; do not report `make lint` as TypeScript linting.

## Vitest Organization

Vitest runs in jsdom with setup shims under `web/vitest.setup.ts`. Tests are
co-located with source and use Testing Library for visible behavior.

- API runtime: Fetch configuration, CSRF, timeouts, abort/network errors,
  generated validation, error normalization, and session expiry.
- Realtime: exact event validation, topic mapping, active-only invalidation,
  lifecycle/reconnect/visibility, attention de-dup, and 401 behavior.
- App shell/bootstrap: setup/auth redirects, cache clearing, selected
  navigation, desktop Sider, mobile Drawer, and logout failure/success.
- Pages: loading/error/unavailable/success states, form/action behavior,
  candidate freshness, mutation invalidation, and responsive rendering.
- Pure feature logic: message ordering/conversations/status, call media cleanup,
  and Mihomo transforms.

Prefer queries by role, accessible name, label, or visible text. Use test IDs
only when a row-specific value/action has no stable semantic selector. Assert
observable output, generated calls, cache invalidation, or persisted state—not
component internals.

## Playwright Contract

`web/e2e/app.spec.ts` uses deterministic synthetic route fixtures and a fake
EventSource. It must cover:

- desktop login, navigation/core flow, cursor “load more,” realtime
  invalidation, and no page-level horizontal overflow;
- mobile login/navigation Drawer, no unintended input focus, record-card
  presentation, and no horizontal overflow.

E2E must not depend on a modem, SIM, RF, private endpoint, existing database,
real SMS/call, or HIL. Do not put sensitive evidence in screenshots, traces, or
fixtures. Playwright reports and test-results remain ignored artifacts.

## Architectural Regression Searches

For stack/boundary changes, search the source and lockfile for:

- `@umijs`, `@ant-design/pro-`, `ProTable`, `ProForm`, `ProLayout`, `.umi`;
- `react-router-dom` (current Router 8 imports come from `react-router`);
- direct `fetch` outside `api/runtime.ts`/generated runtime;
- `new EventSource` outside `app/RealtimeBridge.tsx`;
- deprecated Ant Design APIs and debug logging.

A search is supporting evidence, not a substitute for type, unit, E2E, and
production-build checks.

## Generated and Dependency Evidence

- OpenAPI or generator changes must pass deterministic regeneration and
  `make verify-generated`; generated output is committed.
- Dependency changes must update `pnpm-lock.yaml`, pass frozen install, and run
  both production and full audits through `make security` or equivalent
  focused commands.
- Do not silence a high/critical advisory merely because an affected feature is
  unused when a compatible patched package is available.
- Record nonfatal bundle-size warnings separately. A warning does not fail the
  build, but new large chunks require review rather than suppression.

## Review Checklist

- Routes and shell remain explicit Vite/React Router code with direct Ant
  Design imports.
- Pages use generated Query/SDK contracts and do not call Fetch/EventSource.
- HTTP remains authoritative; SSE only invalidates/refetches and carries generic
  attention.
- Messages/Calls preserve opaque cursor order and stop at absent `nextCursor`.
- Loading, empty, error, disabled, retry, and mutation-failure states are
  visible.
- Desktop and mobile presentations expose the same essential record data and
  actions without page overflow.
- Sensitive identifiers and private hardware/network details stay absent.
- Focused Vitest, typecheck, production build, Playwright, generation drift,
  and dependency audit pass in proportion to the change.

## Wrong vs Correct

```text
Wrong: “Vite build passed,” with no request validation, mobile, or SSE test.
Correct: focused unit/component tests + strict typecheck + production build +
         synthetic desktop/mobile E2E + generated drift/security checks.
```

## Failure Handling

Fix failures attributable to the scoped change. Report unrelated or
environmental failures without weakening assertions, validation, capability
gates, or safety boundaries. Never run real SMS/call/RF/modem/HIL actions to
make a frontend regression suite pass.
