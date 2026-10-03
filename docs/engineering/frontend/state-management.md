# Browser State and Session Management

HTTP snapshots and backend persistence are authoritative. TanStack Query caches
replaceable snapshots; SSE only invalidates topics or signals attention. Pages
must not reconstruct server state from event payloads or persist credentials,
messages, SIM identities or pending submissions in browser storage.

## Installation and authentication

`SessionGate` first reads public system health. A new instance asks the operator
to finish administrator provisioning on the server; it has no Web setup flow.
Provisioning creates the administrator and ready state atomically. Ready instances
probe the administrator session, show `/login` when absent, and enter `/dashboard`
after successful authentication. `/setup` and its APIs have been removed.

Login credential rejection stays an operation error. A protected endpoint 401
advances the in-memory session generation, cancels queries, clears private caches
and replaces the route with `/login`. Repeated 401s do not create a clear/refetch
loop. Login and logout advance the same generation. Responses started under an
older session are discarded before they can repopulate a new user's cache.
A 403 remains an operation error; it must not trigger a login loop.

## Async forms and SMS uncertainty

Mutations never retry automatically. Each SMS submission owns an operation ID
and immutable payload until a definitive result. `queued`, `unconfirmed`, lost
response and timeout preserve that request. Querying it again uses the same ID
even if the current device is unavailable. The backend checks its durable result
before resolving hardware and never resends an uncertain operation.

An asynchronous completion clears only the draft and conversation it submitted.
Switching conversations or sessions must not clear a newer draft, attach an old
result to a different user or silently select a replacement Line. Pending IDs are
local component state; the UI asks users to keep the page open until confirmation.
Authoritative SMS records remain persisted and can be inspected after navigation.

Unread markers are created by first inbound persistence. Clear them only with
the opaque read-through token from a successfully rendered latest HTTP snapshot,
while that conversation is visible and the document is in the foreground.

## Verification

Use `SessionGate.test.tsx`, API runtime tests, message feature tests, fixture
Playwright and `e2e:simulator`. Cover wrong credentials, expiry, logout/login with
in-flight responses, unknown SMS results, immutable request replay and conversation
switches during submission. Simulator browser tests use the real Go backend and
a local rejecting notification proxy, with disposable state and no hardware.
