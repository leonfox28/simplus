# State Management

> HTTP authority, query cache, local interaction state, and realtime hints.

## State Ownership

- The Go API and SQLite are authoritative for persistent Modem, Line, Message,
  Call, notification, eUICC, VoWiFi, and Mihomo state.
- TanStack Query owns replaceable HTTP snapshots, loading/error metadata, and
  generated query keys. It is not an offline database.
- `BootstrapGate` owns setup/session routing; only a protected administrator
  session 401 cancels and clears the query cache before redirecting to
  `/login`. Setup authorization and rejected-login 401s are separate outcomes.
- Page components own form drafts, dialog visibility, selections, reveal state,
  and per-operation progress/errors.
- There is no Redux, Zustand, Umi model, or browser-persistent business store.

## Query and Local State Rules

Use generated Query options for server data and local React state for transient
interaction:

```tsx
const modems = useQuery(listManagedModemsOptions())
const [selectedCandidate, setSelectedCandidate] = useState('')
const [operationError, setOperationError] = useState<unknown>()
```

Do not copy query results into `useState` merely to render them. Derive joins,
labels, and flattened page lists with ordinary constants or `useMemo`. Keep a
row-specific busy key when one action should not block unrelated records.

After a mutation, invalidate the affected generated query key and render the
server-confirmed result. A returned complete resource may update the exact
query cache only when the contract makes that replacement unambiguous. Never
optimistically claim success for SMS, call, RF, VoWiFi, eUICC, or another
side-effecting operation.

## Scenario: Separate Setup and Administrator Authorization

### 1. Scope / Trigger

Use this scenario when changing `BootstrapGate`, the generated-client error
interceptor, Login/Setup pages, setup cookies, or the Vite `/api` development
proxy. These surfaces jointly decide whether an anonymous browser enters setup,
stays on login, or recovers from an expired administrator session.

### 2. Signatures

- `GET /api/v1/setup/status` is public and decides whether setup is required.
- `GET /api/v1/setup/session` uses the restricted
  `simplus_setup_session` HttpOnly cookie; 401 means setup authorization is
  absent or expired.
- `POST /api/v1/auth/login` is public; 401 means the submitted credentials were
  rejected.
- `GET /api/v1/auth/session` and protected business operations use the
  administrator session; their 401 responses trigger session-expiry recovery.
- `VITE_API_PROXY_TARGET` selects the loopback API target for Vite. The `/api`
  proxy keeps `changeOrigin: false`.

### 3. Contracts

- The client error interceptor classifies a 401 using the originating request
  pathname, not status alone. Paths below `/api/v1/setup/` and exact path
  `/api/v1/auth/login` return ordinary `ApiClientError` values without calling
  `notifySessionExpired()`.
- A protected endpoint 401 still notifies session expiry. `BootstrapGate` then
  cancels requests, clears private Query snapshots, and replace-navigates to
  `/login`.
- When `setupRequired` is true, both `/login` and direct `/setup` are public.
  An ordinary root/protected visit settles on `/login`; successful login uses
  the already-fetched setup status and replace-navigates to `/setup`. Direct
  `/setup#bootstrap=...` and setup-session reloads remain reachable.
- In LAN development, Vite is the browser-facing same-origin server while the
  API remains loopback-only. The proxy preserves the browser Host so the
  backend's trusted-LAN validation and setup-completion `managementUrl` refer
  to the address the remote browser can actually reach.
- Production continues to serve Web and API directly from `simplusd`; it does
  not rely on forwarded-host headers or the Vite proxy.

### 4. Validation & Error Matrix

| Response source | Status | Required browser behavior |
| --- | --- | --- |
| `/api/v1/setup/session` | 401 | Stay on `/setup`; render the setup-authorization error; do not clear Query state or navigate to login |
| `/api/v1/auth/login` | 401 | Stay on `/login`; show rejected credentials; do not emit session expiry |
| Successful login while setup is required | 200 | Cache the administrator session and replace-navigate to `/setup`; the backend has issued the restricted setup cookie |
| `/api/v1/auth/session` | 401 | Clear private Query snapshots and replace-navigate to `/login` |
| Protected business operation | 401 | Use the same administrator session-expiry recovery |
| Protected business operation | 403 | Keep the current route and surface the operation error |
| Setup completion through Vite on a valid private/LAN Host | 200 | Return a `managementUrl` using the browser-facing Host and Vite port |
| Any API request with an untrusted Host | 421 | Fail closed with `TRUSTED_LAN_HOST_REQUIRED`; never trust arbitrary forwarding headers |

### 5. Good / Base / Bad Cases

- Good: an expired administrator cookie on `/dashboard` produces one protected
  401, clears private cache, and ends on `/login`.
- Base: an anonymous browser opens an uninitialized instance, settles on
  `/login`, authenticates with the root-provisioned administrator, and then
  reaches `/setup` with the restricted setup cookie issued by the login API.
- Base: a root-generated bootstrap URL or an authorized setup reload opens
  `/setup` directly without requiring an administrator-session probe.
- Bad: broadcasting session expiry for every 401 sends `/setup` to `/login`,
  while the setup-required gate sends `/login` back to `/setup`, creating an
  unbounded redirect loop.
- Bad: rewriting the Vite proxy Host to its loopback target makes setup
  completion redirect a remote browser to that browser's own `127.0.0.1`.

### 6. Tests Required

- Runtime unit tests: setup-session 401 and rejected-login 401 do not notify
  administrator expiry; auth-session 401 still does; 403 never does.
- Component integration: an ordinary uninitialized root/protected visit settles
  on `/login`; `/login` and direct `/setup` stay reachable without an
  auth-session probe; an unauthorized direct Setup page makes one setup-session
  request and does not loop.
- Login page: use cached setup status after a successful login and assert the
  destination is `/setup` when incomplete and `/dashboard` when ready.
- Vite config: assert `/api` has `changeOrigin: false`.
- Proxy smoke when changing dev wiring: run a real Vite-to-upstream request and
  assert the upstream receives the original private Host.
- Browser regression: observe the final path and a bounded navigation count;
  checking only the final path can miss a fast `/setup` ↔ `/login` loop.

### 7. Wrong vs Correct

```ts
// Wrong: unrelated authorization domains collapse into one global reaction.
if (response.status === 401) notifySessionExpired()

// Correct: only protected administrator requests expire that session.
const path = request ? new URL(request.url).pathname : ''
const expectedUnauthorized =
  path.startsWith('/api/v1/setup/') || path === '/api/v1/auth/login'
if (response.status === 401 && !expectedUnauthorized) notifySessionExpired()
```

## Scenario: Authoritative HTTP with Advisory SSE

### 1. Scope / Trigger

Use this scenario whenever an API resource can change outside the current
browser action (incoming SMS/call, background synchronization, Agent changes,
VoWiFi reconcile, Mihomo or inventory changes).

### 2. Signatures

- `GET /api/v1/events` -> authenticated `text/event-stream`.
- SSE named events: `update` and `resync`; each `data` is `RealtimeEvent`.
- `RealtimeEvent = { topics: RealtimeTopic[]; attention?: RealtimeAttention }`.
- `RealtimeAttention = 'sms.received' | 'call.incoming'`.
- Resource reads remain ordinary generated HTTP queries such as
  `GET /api/v1/messages`, `GET /api/v1/calls`, and `GET /api/v1/vowifi/lines`.

### 3. Contracts

- `topics` contains 1–11 unique values from the OpenAPI enum.
- SSE payloads contain no resource bodies, phone numbers, message content,
  credentials, hardware identities, or private topology.
- On a valid event, map topics to generated query tags and invalidate matching
  active queries with `refetchType: 'active'`.
- `sms.received` and `call.incoming` may show generic attention. VoWiFi and all
  other topic-only changes remain silent.
- On reconnect, tab visibility recovery, or `resync`, the browser obtains truth
  through HTTP. It never reconstructs missed state from event history.
- The stream uses the same administrator cookie session and trusted-LAN
  authority boundary as business APIs.

### 4. Validation & Error Matrix

| Condition | Required behavior |
| --- | --- |
| Unknown/extra field, invalid topic, duplicate topic, malformed JSON | Ignore event; do not mutate cache or show attention |
| Stream drops while session remains valid | Close source and reconnect with 3–30 second bounded backoff |
| Session probe returns 401 | Normal session-expiry path clears cache and redirects to login |
| Tab becomes hidden | Close source; do not reconnect while hidden |
| Tab becomes visible | Invalidate active queries, then reconnect |
| Repeated attention event ID | Invalidate as needed but suppress duplicate toast |
| SSE unavailable | Page remains usable through HTTP/manual refetch; mounted VoWiFi/Mihomo runtime queries also have a 10-second foreground-only fallback |

### 5. Good / Base / Bad Cases

- Good: an inbound SMS publishes `{topics:['messages'],
  attention:'sms.received'}`; the active message query refetches and a generic
  notice appears.
- Base: a VoWiFi state change publishes `{topics:['vowifi','lines']}`; matching
  visible snapshots refetch silently.
- Bad: putting an SMS body in SSE and appending it directly to the cache creates
  a second contract, leaks data, and can diverge after disconnects.

### 6. Tests Required

- Unit: exact event validation, unique topics, topic-to-tag invalidation, active
  query scope, and resync.
- Component: one EventSource owner, named event handling, attention de-dup,
  visibility close/resync, bounded reconnect, and 401 behavior.
- E2E: synthetic SSE causes the visible list to refetch on desktop/mobile; no
  hardware or real SMS/call action.
- Backend: auth/session revalidation, heartbeat/write deadlines, slow-client
  backpressure, bounded/privacy-safe payloads, and mutation/background
  publication.

### 7. Wrong vs Correct

```ts
// Wrong: event data becomes authoritative browser state
source.onmessage = (event) => queryClient.setQueryData(['messages'], JSON.parse(event.data))

// Correct: event is a validated hint; HTTP remains authoritative
source.addEventListener('update', (event) => {
  const hint = decodeRealtimeEvent((event as MessageEvent<string>).data)
  if (hint) void invalidateRealtimeTopics(queryClient, hint.topics)
})
```

## Sensitive State

- Authentication and CSRF are cookie-based; do not copy tokens into
  `localStorage` or `sessionStorage`.
- IMEI reveal data stays page-local, is not persisted, and is cleared on hide
  and reload.
- Candidate/discovery snapshots are ephemeral evidence. Only an explicit API
  mutation creates a managed Modem or Line.
- Do not cache secrets, raw hardware identifiers, device paths, or event
  payloads for later replay.

## Avoid

- Manual polling for resources covered by Query plus SSE invalidation.
- A global client store that duplicates server-owned entities.
- Mutating arrays obtained from Query in place.
- Cross-coupling Line identity, RF, egress, and VoWiFi state into one browser
  flag or optimistic operation.
