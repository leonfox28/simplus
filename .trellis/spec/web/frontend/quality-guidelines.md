# Frontend Quality Guidelines

> The checks and regression patterns currently used by `@simplus/web`.

## Enforced Checks

The frontend quality gate is TypeScript, Vitest, OpenAPI generation, and the Umi
production build:

```bash
corepack pnpm --dir web test
corepack pnpm --dir web typecheck
corepack pnpm --dir web build
make verify-generated
```

`web/package.json` makes `build` regenerate `src/api/schema.d.ts`, run
`tsc --noEmit`, and then run `max build`. Root `make test` includes the Web test
and type-check among the wider Go/integration suite. CI additionally runs
`make lint`, but that target currently vets Go and checks GitHub Actions; there
is no ESLint, Biome, Prettier, or frontend-specific `lint` script. Do not claim
frontend formatting is mechanically enforced.

The package is pinned to Node 24 and pnpm 11.18 through the root manifest and
toolchain files. Use Corepack/root scripts so local checks match CI.

## Test Organization

Vitest runs in jsdom from `web/vitest.config.ts`; `vitest.setup.ts` installs
jest-dom matchers and browser shims for `matchMedia` and `ResizeObserver`.
Tests are co-located with the source under test.

### Page flows

Page tests use Testing Library, mock the API module, and exercise observable
behavior rather than component internals:

```tsx
// web/src/pages/Lines.test.tsx
const view = render(<App><Lines /></App>)
expect(await screen.findByText('VOXI Line')).toBeInTheDocument()
fireEvent.click(screen.getByRole('button', { name: '配置' }))

const activate = await screen.findByRole('button', { name: '激活 VoWiFi' })
expect(activate).toBeEnabled()
fireEvent.click(activate)

await waitFor(() => expect(api.activateVoWiFiLine).toHaveBeenCalledWith(lineID))
view.unmount()
```

`Lines.test.tsx` separately covers candidate creation and activation of an
already configured Line, plus disabled readiness states, partial runtime
failure, and polling scope. It does not currently exercise saving a renamed
Line or a changed egress binding.
`Modems.test.tsx` covers candidate selection, model-read failure, disabled RF,
and explicit IMEI reveal/hide. `Login.test.tsx` asserts native password-manager
semantics. Pages that use Ant Design app APIs are rendered inside `<App>`.

Prefer role, label, input hint, and visible-text queries. `data-testid` is
currently limited to row-specific IMEI displays/toggles and RF controls in
`Modems.tsx`.

### Boundary and pure-logic tests

- `web/src/api/client.test.ts` stubs global `fetch` and verifies paths, methods, JSON,
  CSRF headers, stable transport errors, malformed success responses, invalid
  request rejection, and cross-field invariants.
- `messages/order.test.ts`, `messages/conversations.test.ts`, and
  `messages/status.test.ts` test deterministic transformations without
  rendering React.
- `calls/simulatorMedia.test.ts` stubs browser media globals and checks secure
  context failure plus resource cleanup.
- `app.test.tsx` calls the Umi layout hook directly to protect desktop/mobile
  menu behavior.

When a change modifies a boundary invariant or visible workflow, extend the
nearest existing suite instead of relying only on a snapshot or build.

## Required Architectural Boundaries

- Pages import typed operations from `@/api/client`; only
  `web/src/api/client.ts` calls `fetch`. This centralizes CSRF attachment,
  stable error codes, existing input guards, and response validation.
- Keep generated schemas generated. Changes to `api/openapi.yaml` must be
  reflected through `generate:api` and pass `verify-generated`.
- Keep UI behavior capability-driven and business-oriented.
  `docs/architecture.md` forbids Web branching on model names, USB/sysfs paths,
  AT commands, or Agent fencing details.
- Preserve fail-closed unavailable states. `Lines.test.tsx` keeps VoWiFi
  activation disabled for an unconfigured exit while permitting only the
  explicitly supported recovery states. `Modems.test.tsx` disables RF writes
  when the observation is unknown.
- Preserve explicit user intent. Scan results do not automatically become
  managed Modems or Lines; the corresponding page tests assert this.
- Await mutations and display failure instead of silently reporting success.
  The larger management pages use error state plus `try`/`catch`/`finally`.

## Error and Privacy Behavior

The API layer normalizes transport, HTTP, request, and invalid-response failures
to stable codes. Pages with explicit error UI either display the code or
translate known codes through a local map (`displayError` in `Modems.tsx`). Do
not depend on browser-specific network error text.

Privacy boundaries are user-visible behavior and need regression coverage:
the ordinary Modem response does not expose IMEI, the reveal is a dedicated
action, and reload clears it. Line and Modem UI must not expose raw device paths
or private IMS identity. Evidence lives in `web/src/api/client.test.ts`,
`Modems.test.tsx`, `Lines.test.tsx`, and `docs/architecture.md`.

## Formatting Reality

Handwritten TypeScript generally uses single quotes and no semicolons, but
formatting is not uniform. `Lines.tsx`, `Modems.tsx`, and `Mihomo.tsx` are
expanded multi-line examples; `Calls.tsx`, `Notifications.tsx`,
`Settings.tsx`, and parts of `api/client.ts` retain dense legacy formatting.
Keep edits focused, follow the surrounding file, and do not combine a behavior
change with an unrelated repository-wide reformat.

## Review Checklist

- The route/page remains inside the explicit Umi/ProLayout structure.
- Pages use Pro Components and existing responsive patterns before adding a new
  UI abstraction.
- Successful API JSON is validated at `src/api`; constrained IDs and
  cross-field request invariants are checked there with stable error codes.
- New or changed API types reuse generated schemas; no new parallel handwritten
  response model or broad cast bypasses the contract.
- Loading, empty, disabled, partial-failure, and destructive-action states are
  represented where the operation has them.
- Sensitive identifiers stay masked/transient, and internal hardware/network
  implementation details do not enter the UI.
- A focused test covers the changed domain rule or user flow.
- Web tests and strict type-check pass; API changes also pass generation/build
  verification.
