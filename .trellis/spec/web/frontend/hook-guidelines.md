# Hook Guidelines

> The React hook and data-loading patterns actually used by `@simplus/web`.

## Current Boundary

There are no project-defined `use*` hooks or `src/hooks/` directory. Stateful
logic lives in the routed component that owns it. `Lines.tsx`, `Modems.tsx`,
and `Mihomo.tsx` are the most complete examples; smaller pages use the same
primitives in a compressed form.

The package includes and enables Umi's React Query plugin, but no source file
uses `useQuery`, `useMutation`, a query key, or a `QueryClient`. Do not assume a
cache/invalidation convention that the application does not yet have.

## Initial Loads

For a page with reloadable server data, the repeated pattern is a stable
`useCallback` followed by an effect that invokes it:

```tsx
// web/src/pages/Modems.tsx
const load = useCallback(async () => {
  setLoading(true)
  setError('')
  try {
    setModems(await listManagedModems())
  } catch (loadError) {
    setError(displayError(loadError))
  } finally {
    setLoading(false)
  }
}, [])

useEffect(() => { void load() }, [load])
```

`Lines.tsx` and `Mihomo.tsx` use the same `load`/`useCallback` boundary. Fetch
independent resources concurrently with `Promise.all`, as those pages and
`Messages.tsx` do, then commit each result to its typed state.

Simple one-shot pages (`Dashboard.tsx` and `Setup.tsx`) call the API directly
from a mount effect with `.then(...).catch(...)`. This is existing compact code,
not a separate data layer. Page components currently do not create
`AbortController`s, although API functions accept optional `AbortSignal`s.

## Polling Effects

Polling is used only for state that genuinely changes while a page is open:

- `Lines.tsx` loads the Line catalog and configuration once, then polls only
  `listVoWiFiLines()` every five seconds. `Lines.test.tsx` explicitly asserts
  that polling does not reload managed Lines.
- `Messages.tsx` refreshes message, contact, and Line data every five seconds;
  its effect uses `disposed` and `inFlight` flags to avoid post-cleanup work and
  overlapping refreshes.
- Both polling effects skip work while `document.visibilityState` is not
  `visible` and clear their interval in the effect cleanup.

Representative shape:

```tsx
// web/src/pages/Lines.tsx
useEffect(() => {
  if (!lines.some((line) => line.capabilities.hostVoWifiAuth) || voWiFiError === 'VOWIFI_UNAVAILABLE') return undefined
  const timer = window.setInterval(() => {
    if (document.visibilityState !== 'visible') return
    void listVoWiFiLines()
      .then((states) => { setVoWiFiStates(states); setVoWiFiError('') })
      .catch((cause) => setVoWiFiError(errorText(cause)))
  }, 5000)
  return () => window.clearInterval(timer)
}, [lines, voWiFiError])
```

Keep the polled boundary as narrow as the runtime state. Do not turn a small
status poll into a full page reload unless the server contract requires it.

## Derived Values

Use `useMemo` for non-trivial derived collections that feed large component
trees. `Lines.tsx` joins Lines, egress bindings, and VoWiFi states into
`LineRow[]`, and separately builds country options:

```tsx
const rows = useMemo<LineRow[]>(() => lines.map((line) => ({
  ...line,
  binding: bindings.find((item) => item.lineId === line.id),
  voWiFi: voWiFiStates.find((item) => item.lineId === line.id),
})), [bindings, lines, voWiFiStates])
```

Simple filters and lookups remain inline or ordinary local constants in
`Messages.tsx`, `Calls.tsx`, `Mihomo.tsx`, and `Modems.tsx`. There is no pattern
of wrapping every handler or computed primitive in `useMemo`/`useCallback`.

## Async Handlers

- Await mutations before refreshing or changing success UI.
- Track a page error and clear it when starting a deliberate retry. The larger
  pages use `try`/`catch`/`finally`; caught values are `unknown` and normalized
  with an error helper or `instanceof Error`.
- Use `void` when a React event or effect intentionally launches a promise,
  such as `onClick={() => void scan()}` in `Modems.tsx` and
  `useEffect(() => { void load() }, [load])` in `Lines.tsx`.
- Mutations that affect several snapshots call the shared `load` callback.
  Mutations returning the complete changed object may patch the relevant local
  array, as RF and VoWiFi actions do in `Modems.tsx` and `Lines.tsx`.

## Library Hooks

Call library hooks at the top of the component. The established responsive
pattern is `Grid.useBreakpoint()` followed by `const compact = !screens.md` (or
the compact one-line equivalent). It appears in `Lines.tsx`, `Mihomo.tsx`,
`Dashboard.tsx`, `Calls.tsx`, and `Notifications.tsx`. Ant Design message APIs
come from `App.useApp()` in `Login.tsx` and `Messages.tsx`, under the
`<AntdApp>` wrapper installed by `rootContainer` in `app.tsx`.

## Avoid

- Do not describe server data as React Query state or introduce query-key
  conventions based only on the installed dependency; current pages load and
  synchronize explicitly.
- Do not create a custom hook for logic with only one page consumer. Existing
  reusable logic is extracted as plain functions/classes under feature folders.
- Do not leave timers active after unmount or poll hidden tabs.
- Do not reload stable catalogs during a narrow runtime-status poll; the Line
  test suite protects this distinction.
- Do not make an effect callback itself `async`; current effects invoke a
  promise-returning helper and return only cleanup when applicable.
