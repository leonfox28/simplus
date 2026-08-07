# State Management

> Where state lives in the current single-administrator management UI.

## Overview

Backend APIs remain authoritative for persistent Modem, Line, Message, Call,
notification, and Mihomo state. The browser keeps typed snapshots and ephemeral
interaction state in the page that renders them. There is no Redux, Zustand,
React context store, Pinia, or project Umi model in current source.

`web/config/config.ts` enables empty `model` and `reactQuery` plugin
configuration, but the application does not define a model, call `useModel`,
or use TanStack Query hooks. Treat those entries as framework configuration,
not as an established state-management abstraction.

## State Categories

### Application bootstrap state

`web/src/app.tsx` is the only application-wide state boundary. Umi calls
`getInitialState()` to load the administrator session and setup status, and the
`layout` runtime hook reads that result for the avatar and auth redirect:

```ts
// web/src/app.tsx
export async function getInitialState(): Promise<{ session?: AuthSessionResponse; setupRequired: boolean }> {
  if (window.location.pathname === '/login') return { setupRequired: false }
  try {
    const [session, setup] = await Promise.all([getAuthSession(), getSetupStatus()])
    if (setup.setupRequired && window.location.pathname !== '/setup') window.location.replace('/setup')
    return { session, setupRequired: setup.setupRequired }
  } catch {
    if (window.location.pathname !== '/login') window.location.replace('/login')
    return { setupRequired: false }
  }
}
```

The ProLayout menu and auth guard are centralized there: menu clicks push a
fixed internal path and dispatch `popstate`, while the guard redirects with
`window.location.replace`. Login, setup completion, password changes, and
logout also use `window.location.replace` in `Login.tsx`, `Setup.tsx`,
`Settings.tsx`, and `app.tsx`. Routes themselves live in
`web/config/routes.ts`; page state is not serialized into the URL today.

### Server snapshots owned by a page

Pages initialize typed arrays or optional objects and populate them through
`@/api/client`:

```tsx
// web/src/pages/Lines.tsx
const [lines, setLines] = useState<ManagedLine[]>([])
const [bindings, setBindings] = useState<LineEgressBinding[]>([])
const [voWiFiStates, setVoWiFiStates] = useState<VoWiFiLineState[]>([])
```

The same pattern is used for managed Modems in `Modems.tsx`, Mihomo core/runtime
and subscriptions in `Mihomo.tsx`, and messages/contacts/Lines in
`Messages.tsx`. There is no cross-page client cache: returning to a route loads
a fresh snapshot.

### Ephemeral interaction state

Modal visibility, selected IDs, drafts, errors, and operation progress stay
local. Examples include `addOpen`, `candidateID`, and `drawerLineID` in
`Lines.tsx`; `selectedCandidate`, `modalError`, and per-Modem busy IDs in
`Modems.tsx`; and the `busy` operation key in `Mihomo.tsx`.

Use an ID/key when progress belongs to one row or operation:

```tsx
// web/src/pages/Lines.tsx
setBusy(`vowifi:${lineID}`)
// web/src/pages/Mihomo.tsx
loading={busy === `refresh:${record.id}`}
```

This lets unrelated rows remain readable while preventing duplicate work on
the active item. Simpler one-operation pages use a boolean loading state.

### Derived presentation state

Derive joins, options, and labels from the authoritative snapshots. `Lines.tsx`
uses `useMemo` for `rows` and country options; `Mihomo.tsx` derives the current
runtime label and subscription name; `Modems.tsx` filters capability tags at
render time. Do not persist these display forms separately.

## Synchronization After Mutations

There is no optimistic-update framework. Current code uses one of two explicit
patterns:

1. Await the mutation and call the page's `load()` when several resources or
   server-derived fields may have changed. Line creation, Line naming, egress
   changes, Mihomo operations, messages, calls, and notification channels do
   this in their respective page files.
2. Replace only the returned resource when the endpoint returns the complete
   new state. `Modems.tsx` maps an RF update into the Modem array, and
   `Lines.tsx` replaces one returned `VoWiFiLineState` by `lineId`.

`Lines.tsx`, `Modems.tsx`, and `Mihomo.tsx` leave the previous snapshot visible
when a caught mutation fails and surface an error. These larger management
pages reset busy/loading flags in `finally` blocks.

## Polling and Runtime State

Runtime state may be fresher than configuration state. `Lines.tsx` therefore
polls only VoWiFi runtime state, while managed Lines and egress bindings remain
stable until a user action or explicit reload. `Lines.test.tsx` verifies that
separation. `Messages.tsx` polls its changing history with an overlap guard.
Both polling paths pause while the document is hidden and clean up intervals.

## Sensitive and Durable State

- Do not use `localStorage` or `sessionStorage`; current source uses neither.
- Authentication is a server cookie session. `api/client.ts` reads the CSRF
  cookie only to attach the double-submit header to mutations.
- IMEI is deliberately transient state in `Modems.tsx`: hidden initially,
  fetched from a dedicated endpoint, removed when hidden, and cleared on every
  reload.
- Discovered Modem/Line candidates are modal snapshots, not durable business
  objects. The backend creates stable managed IDs only after an explicit user
  action. `Modems.test.tsx` and `Lines.test.tsx` assert that scan results are not
  promoted automatically.

## Avoid

- Do not make the browser the source of truth for managed hardware or runtime
  state, and do not infer a business identity from a table row or USB path.
- Do not add a global store simply to share data that each route currently
  reloads from the API.
- Do not cache secrets or raw equipment identity in browser storage.
- Do not mutate API arrays in place. `messages/order.ts` copies before sorting,
  and `messages/order.test.ts` explicitly checks the input remains unchanged.
- Do not couple independent state categories: `docs/architecture.md` and the
  Line tests keep Line identity, RF state, egress, and VoWiFi activation as
  separate operations.
