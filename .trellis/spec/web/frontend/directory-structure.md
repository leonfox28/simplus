# Frontend Directory Structure

> The current layout of the `@simplus/web` Vite single-page application.

## Package Boundary

`web/` is the browser package. The Go service owns persistence and business
state; `api/openapi.yaml` owns the public contract.

```text
web/
├── e2e/                         # synthetic Playwright desktop/mobile flows
├── openapi-ts.config.ts         # Hey API generator definition
├── playwright.config.ts
├── vite.config.ts               # Vite, @ alias, dev /api proxy
├── src/
│   ├── api/
│   │   ├── generated/           # generated SDK/types/Zod/Query helpers
│   │   ├── runtime.ts           # same-origin fetch, CSRF, time budgets
│   │   ├── setupClient.ts       # error normalization/session expiry
│   │   ├── queryClient.ts       # shared Query defaults
│   │   ├── events.ts            # realtime validation/topic mapping
│   │   └── hardwareSchema.ts    # topology cross-reference checks
│   ├── app/
│   │   ├── AppProviders.tsx     # Ant Design, Query, BrowserRouter
│   │   ├── BootstrapGate.tsx    # setup/session bootstrap and guards
│   │   ├── AppRouter.tsx        # explicit lazy route table
│   │   ├── AppShell.tsx         # responsive navigation and Outlet
│   │   └── RealtimeBridge.tsx   # authenticated SSE lifecycle
│   ├── components/Page.tsx      # shared page/state/responsive primitives
│   ├── pages/                   # routed screens
│   ├── calls|messages|mihomo/   # reusable non-visual feature logic
│   ├── test/                    # shared deterministic fixtures/helpers
│   ├── global.css
│   └── main.tsx
├── vitest.config.ts
└── package.json
```

## Placement Rules

- Add routes explicitly to `src/app/AppRouter.tsx`; add authenticated menu
  entries to `src/app/navigation.tsx` when the route belongs in navigation.
- Keep app-wide providers, auth/setup guards, shell layout, and realtime
  connection ownership under `src/app/`.
- Put routed screens in `src/pages/`. Keep a one-page helper beside its page;
  extract shared presentation primitives only after there is a real second
  consumer.
- Put reusable non-visual domain transforms under their feature directory with
  a co-located unit test.
- Keep transport, generated client setup, runtime validation, errors, query
  configuration, and realtime topic mapping in `src/api/`. Pages never call
  `fetch` or construct `EventSource`.
- Keep reusable page composition in `src/components/`; it must remain domain
  neutral and contain no API calls.
- Use the `@/* -> src/*` alias across directories and relative imports within a
  feature.

## Route Contract

`AppRouter` uses `Routes`/`Route` from `react-router`. `/login` and `/setup`
render outside `AppShell`; authenticated pages are children of the shell and
render through `Outlet`. Route modules are lazy-loaded, and unknown routes
replace-navigate to `/dashboard`.

Do not add a framework router, filesystem routing, or route-loader state layer.
Setup/session decisions stay in `BootstrapGate`, which can clear the query
cache and redirect after a 401.

## Generated Ownership

`corepack pnpm --dir web generate:api` reads `api/openapi.yaml` through
`web/openapi-ts.config.ts` and replaces `web/src/api/generated/`. This output
contains:

- Fetch client and typed SDK;
- TypeScript request/response models;
- Zod request/response schemas;
- TanStack Query keys, query/infinite-query options, and mutation options.

Never hand-edit anything under `generated/`. Change OpenAPI or generator
configuration, regenerate, and run `make verify-generated`. The whole generated
directory is registered in `Makefile` so added or removed files are checked.

## Avoid

- `web/config/`, `.umi/`, `src/app.tsx`, Umi runtime exports, or ProLayout.
- `@umijs/*`, `@ant-design/pro-*`, `react-router-dom`, or handwritten copies of
  generated API models.
- Direct `fetch`, manual endpoint paths, or `new EventSource` in pages.
- Browser models for backend-owned Modem, Line, Message, Call, or Mihomo state.
- Placing application code in generated output or build artifacts.
