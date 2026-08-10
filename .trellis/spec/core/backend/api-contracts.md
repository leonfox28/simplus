# API Contracts

## Public HTTP Is OpenAPI-First

`api/openapi.yaml` is the public `/api/v1` source of truth. It defines paths,
operation IDs, request/response schemas, enums, and locale-neutral error
shapes. The generated Go implementation contract lives in
`internal/api/openapi/generated.go`; `internal/api/httpapi/server.go`
implements it and registers it with `openapi.HandlerFromMux`.

For a public contract change:

1. edit `api/openapi.yaml` first;
2. update application/domain behavior and the `httpapi.Server` mapping;
3. run `make generate` to refresh Go and Web types;
4. add/adjust handler and client tests;
5. run `make verify-generated` before broad tests.

Do not hand-edit `internal/api/openapi/generated.go` or anything under
`web/src/api/generated/`. `internal/api/openapi/spec_test.go` validates the
embedded spec, while `Makefile` verifies that regeneration leaves every
declared generated path unchanged.

## HTTP Boundary Behavior

`internal/api/httpapi/server.go` centralizes the public boundary:

- `Router` applies security headers, trusted-LAN authority validation, request
  IDs, panic recovery, and endpoint-aware timeouts before dispatching the
  generated handler.
- `decodeJSON` caps request bodies at 4096 bytes, rejects unknown fields, and
  requires exactly one JSON value. A route with a genuinely different bound
  must document and test it rather than bypassing the decoder casually.
- `requireBusinessAPI` gates initialized business routes and administrator
  authentication. Mutating methods require the double-submit CSRF cookie and
  `X-Simplus-CSRF` header; cookie behavior is implemented by
  `administratorTokens`, `requireAdministrator`, and the session helpers.
- `trustedLANHostOnly` accepts localhost, loopback, and private IP authorities
  and rejects malformed/public authority values. It is a trusted-LAN guard,
  not authorization to expose the service publicly (`docs/product.md`).
- Expected service errors map to stable `openapi.ApiError` codes and explicit
  HTTP statuses. Unknown internal detail is logged server-side and returned as
  a bounded code, as shown by `writeAuthenticationError`,
  `writeMessageError`, and the other domain mappers.
- `timeoutJSON` buffers the handler response so a timeout produces one stable
  `API_TIMEOUT` response. The 130-second message-send budget corresponds to
  the 120-second multipart transport budget; asynchronous submit reports do
  not extend the request (`apiTimeout` and
  `internal/application/messaging/service.go`).

Tests in `internal/api/httpapi/server_test.go` use `httptest` to protect auth,
CSRF, trusted authorities, panic recovery, timeouts, stable errors, and
sensitive response shapes. Add boundary tests there rather than relying only
on application tests.

## Scenario: Cursor History and Realtime Invalidation

### 1. Scope / Trigger

Use this contract for durable lists that can grow while a browser is reading
them and for background changes that must become visible without polling.
Messages and Calls are the current cursor-paginated resources; `/api/v1/events`
is the shared invalidation stream.

### 2. Signatures

- `GET /api/v1/messages?limit={1..50}&cursor={opaque}`; optional exact
  `remoteAddress` returns one recipient across Lines, while optional `lineId`
  is valid only together with `remoteAddress` for the legacy exact-Line view.
- `GET /api/v1/message-conversations?limit={1..50}&cursor={opaque}` returns
  recipient summaries ordered by their last message tuple.
- `PUT /api/v1/message-conversations/read-state` accepts an exact remote
  address and the opaque read-through token returned by that recipient's
  newest message page.
- `GET /api/v1/calls?limit={1..50}&cursor={opaque}`.
- Message responses are newest-first by first-successful local persistence
  sequence. Call responses remain newest-first by
  `(created_at_unix_ms DESC, call_id DESC)`. Both may contain `nextCursor`.
- `GET /api/v1/events` returns authenticated `text/event-stream` named events
  `update` or `resync` with `RealtimeEvent` JSON data.
- `RealtimeEvent` is `{topics: RealtimeTopic[], attention?:
  'sms.received'|'call.incoming'}`.

### 3. Contracts

- Omitted `limit` means 20. Explicit `limit=0`, an explicitly empty cursor,
  malformed base64url, unsupported cursor version, or a cursor longer than 256
  bytes is invalid rather than omitted.
- Cursors are opaque, versioned, length-bounded base64url values. Calls use v1
  UTC Unix milliseconds plus call ID. SMS responses use an SMS-kind v2 positive
  record sequence plus message ID; SMS temporarily accepts v1 only when the
  boundary message still exists and its time/filter match. Calls reject v2.
  Clients return cursors byte-for-byte and never inspect them.
- Stores fetch `limit+1`; they return at most `limit` records and emit a cursor
  from the last returned row only when another row exists. SMS v2 uses strict
  sequence `<`, so deletion of that boundary does not invalidate the page.
- Remote addresses are compared exactly and never normalized. Global,
  remote-only, and paired Line + remote message reads are valid; Line-only or
  an explicitly empty filter is invalid.
- A remote-only newest page may return an opaque read-through token derived
  from the same SQLite snapshot. It contains no address or body. Marking read
  deletes only that remote address's unread markers at or below the token's
  monotonic boundary; a later inbound marker is never cleared by an old token.
- Conversation summaries contain the last message, durable unread count, and
  optional most recent outbound Line. Contacts remain a separate browser join.
- Generated query binding can reject malformed pagination before the operation
  handler runs. Every new cursor-paginated path must be added to the centralized
  pagination-parameter error classifier so malformed/empty `limit` and
  `cursor` still map to `PAGE_LIMIT_INVALID` / `PAGE_CURSOR_INVALID`, not the
  generic `API_REQUEST_INVALID` fallback.
- SSE is advisory and privacy-bounded. It carries topic/attention metadata, not
  resource records, message bodies, addresses, identities, or secrets.
- Every subscriber first receives `resync` for all topics. A slow subscriber's
  buffered update is replaced by one all-topic `resync`, so publication cannot
  block business operations.
- SSE revalidates the administrator session periodically, sends heartbeats, and
  bounds session checks plus write/flush operations locally. Ordinary JSON
  endpoints retain buffered endpoint timeouts; the HTTP server must not apply a
  global `WriteTimeout` that cuts off a healthy stream.

### 4. Validation & Error Matrix

| Condition | HTTP/result |
| --- | --- |
| `limit` absent | 20 |
| `limit < 1` or `limit > 50`, including explicit zero | `400 PAGE_LIMIT_INVALID` |
| cursor present but empty/malformed/version/ID invalid | `400 PAGE_CURSOR_INVALID` |
| SMS v2 passed to Calls, or sequence/ID mismatch while boundary exists | `400 PAGE_CURSOR_INVALID` |
| legacy SMS v1 boundary missing or outside the exact filter | `400 PAGE_CURSOR_INVALID` |
| remote address only | cross-Line recipient history |
| Line + remote address | exact legacy Line history |
| Line only or any explicitly empty filter | `400 MESSAGE_FILTER_INVALID` |
| read token malformed or mismatched | `400 MESSAGE_READ_STATE_INVALID` |
| read boundary message deleted | `404 MESSAGE_READ_BOUNDARY_NOT_FOUND` |
| no live administrator session | `401 AUTH_SESSION_UNAUTHORIZED` |
| instance not ready | 409 typed `ApiError` |
| trusted-LAN authority invalid | 421 typed `ApiError` |
| SSE auth unavailable | 503 typed `ApiError` before streaming |
| stalled/disconnected SSE client | close subscription; never block publisher |

### 5. Good / Base / Bad Cases

- Good: read 20 messages, use the returned SMS v2 cursor unchanged, delete the
  boundary if desired, and still receive the next strictly older sequences.
- Base: a background VoWiFi change publishes topics only; clients refetch the
  current HTTP snapshot silently.
- Bad: offset pagination while inserts arrive can skip/duplicate rows; sending
  a full SMS in SSE creates a second, privacy-sensitive source of truth.

### 6. Tests Required

- Domain cursor round-trip plus malformed, kind, version, length, sequence,
  time, ID, legacy compatibility, and Calls-isolation cases.
- Store tests for equal/reversed timestamps, page boundaries, boundary deletion,
  remote-only cross-Line
  history, conversation isolation, no duplicates/skips, unread watermark
  races/idempotency/deletion, and index migration Down/reopen.
- HTTP tests for omitted versus explicit empty/zero parameters, exact filters,
  stable JSON errors, auth/trusted-LAN, next-cursor behavior, and generated
  binding failures on every paginated route before its handler is entered.
- Hub/SSE tests for initial resync, normalized topics, privacy, backpressure,
  heartbeat, session expiry, write deadlines, and cancellation.
- Application/background tests proving durable changes publish the correct
  topic and only inbound SMS/incoming calls carry attention.

### 7. Wrong vs Correct

```go
// Wrong: SMS reconstructs persistence order from provider/local business time.
ORDER BY created_at_unix_ms DESC, message_id DESC

// Correct: SMS uses its persisted monotonic fact and SMS-only cursor.
ORDER BY record_sequence DESC
nextCursor, err := pagination.EncodeSMS(boundary)
```

## Bounded Internal Unix Protocols

The Agent protocol is code-owned rather than OpenAPI-owned, but it follows the
same typed discipline:

- `internal/agentapi/protocol.go` owns protocol/version envelopes, bounded
  request/response structures, capability evidence, and stable error codes.
- `internal/agentapi/server.go` exposes fixed routes. Constructors make the
  surface explicit: `NewReadOnlyHardwareHandler` cannot receive write or SMS
  backends, and `NewManagedHardwareHandler` adds only typed RF/identity
  services. There is no arbitrary command/path route.
- `internal/agentapi/client.go` validates protocol versions, instance IDs,
  generations, typed states, and response/request correlation instead of
  trusting decoded JSON.
- `internal/agentapi/listener.go` requires an absolute Unix path, restrictive
  modes, owned non-symlink paths, and an allowed UID verified with peer
  credentials.
- `internal/agentapi/protocol_test.go` and
  `internal/agentapi/client_server_test.go` reject
  malformed typed responses and prove unavailable routes stay unavailable.

Mihomo and Host VoWiFi follow the same fixed-operation approach in
`internal/mihomosupervisor/` and `internal/vowifisupervisor/`. `simplus-netd`
may accept a stable Line ID and typed egress selection, but not shell text,
network commands, arbitrary configuration paths, SIP/RP payloads, modem
commands, or device paths (`cmd/simplus-netd/main.go` and
`docs/architecture.md`).

## Compatibility Rules

- Add fields compatibly where possible; client and server validation must
  agree in the same change.
- Keep error codes locale-neutral, bounded, and free of identities, addresses,
  credentials, raw protocol messages, or hardware paths. Public explanations
  belong in `docs/troubleshooting.md`.
- Keep secrets and sensitive identities out of ordinary list/read contracts.
  The equipment-identity flow is a dedicated authenticated POST with
  `Cache-Control: no-store`, implemented across
  `internal/application/modem/service.go` and `httpapi.Server`.
- A hardware/backend failure must be returned as unavailable or a typed
  failure; never fall back to Simulator and report success.

## Avoid

- Defining handler-only request structs for a public operation already owned
  by OpenAPI.
- Passing raw `map[string]any`, arbitrary strings, filesystem paths, AT/QMI,
  SIP, or network commands across a trust boundary.
- Returning `err.Error()` to browsers or Unix clients as a public contract.
- Adding an unbounded body reader, client without response validation, or
  timeout that conflicts with the underlying operation budget.
- Treating the Unix socket's filesystem mode as sufficient without preserving
  peer-credential and fixed-UID checks.
