# Hook Guidelines

> Query, mutation, pagination, and realtime hook patterns in `@simplus/web`.

## Server Queries

Use TanStack Query with Hey API generated options:

```tsx
const linesQuery = useQuery(listManagedLinesOptions())
```

Do not wrap a generated option in a project hook unless the hook adds reusable
domain behavior for multiple consumers. Query defaults are created once in
`createAppQueryClient`: snapshots are stale after 10 seconds, garbage-collected
after 5 minutes, refetched on focus, and retried at most twice only for
`ApiClientError.retryable` failures.

## Mutations and Invalidation

Use generated mutation options and invalidate the smallest generated key that
represents the changed snapshot:

```tsx
const mutation = useMutation({
  ...updateManagedLineMutation(),
  onSuccess: () => queryClient.invalidateQueries({
    queryKey: listManagedLinesQueryKey(),
  }),
  onError: setOperationError,
})
```

Mutations never retry automatically. This is mandatory for SMS, calls, RF,
eUICC, VoWiFi, and configuration changes where repetition can cause duplicate
effects or obscure an uncertain outcome. Clear/close a form only after success.

The dedicated equipment-identity POST is a sensitive read, not a cached
mutation result. Invoke its generated SDK operation directly, keep its
busy/error/reveal state in the Modems page, and discard the returned IMEI when
the user hides it, refreshes the page data, or unmounts the page.

Refetch candidate/discovery queries when their modal opens; do not treat an old
candidate snapshot as current hardware evidence.

## Cursor Pagination

Messages and Calls use generated infinite-query options with opaque cursors:

```tsx
const options = listMessagesInfiniteOptions({ query: { limit: 20, ...filter } })
const query = useInfiniteQuery({
  ...options,
  initialPageParam: { query: {} },
  getNextPageParam: (page) => page.nextCursor,
})
```

- Pass `nextCursor` back unchanged; never decode or synthesize it in the UI.
- Stop when `nextCursor` is absent.
- Conversation filters require both `lineId` and `remoteAddress`.
- Flatten pages for display without mutating generated data.
- A cursor failure is surfaced as `PAGE_CURSOR_INVALID`; offer a fresh reload
  rather than trying to repair the cursor.

## Realtime Lifecycle

Only `RealtimeBridge` owns `EventSource('/api/v1/events')`. It:

1. listens to named `update` and `resync` events;
2. validates JSON with the generated schema plus exact-key/unique-topic checks;
3. maps bounded topics to generated query tags;
4. invalidates/refetches only active matching queries;
5. shows generic attention for `sms.received` or `call.incoming` once per event
   ID;
6. closes while the document is hidden, resyncs active queries on visibility,
   and reconnects with bounded exponential backoff;
7. probes the HTTP session after errors and lets the normal 401 path clear
   state and redirect.

SSE is not used as a query function and never patches resource records. VoWiFi
and other silent updates invalidate snapshots without producing a toast.

## Narrow Runtime Fallback

VoWiFi and Mihomo runtime status additionally use a 10-second TanStack Query
`refetchInterval` while their page is mounted, with
`refetchIntervalInBackground: false`. This is a narrow convergence backstop for
stream loss and runtime recovery, not the primary change signal. Do not extend
it to stable catalogs or Messages/Calls, and do not implement it with a manual
timer. SSE, window-focus refetch, and explicit refresh remain the normal paths.

## Effects and Derived Values

- Effects must clean up listeners, timers, and EventSource instances.
- Do not make an effect callback `async`; invoke an async helper with `void`.
- Use `useMemo` for non-trivial joins/flattening that feeds a large render, not
  for trivial primitives.
- Use an operation key/ID when busy state belongs to one row; do not block the
  whole page unnecessarily.

## Avoid

- Manual `useEffect` fetching or 5-second polling for server snapshots; the
  documented VoWiFi/Mihomo active-page fallback is the only current exception.
- Handwritten query keys when a generated key/options helper exists.
- Automatic mutation retries or optimistic confirmation of uncertain effects.
- Opening multiple EventSource connections from pages.
- Refetching inactive queries for every realtime event.
- Treating reconnect as proof that no updates were missed; `resync` and
  visibility recovery always return to HTTP.
