# State Management

> HTTP authority, query cache, local interaction state, and realtime hints.

## State Ownership

- The Go API and SQLite are authoritative for persistent Modem, Line, Message,
  Call, notification, eUICC, VoWiFi, and Mihomo state.
- TanStack Query owns replaceable HTTP snapshots, loading/error metadata, and
  generated query keys. It is not an offline database.
- `BootstrapGate` owns setup/session routing; a 401 cancels and clears the query
  cache before redirecting to `/login`.
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
