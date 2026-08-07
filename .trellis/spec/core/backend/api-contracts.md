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

Do not hand-edit `internal/api/openapi/generated.go` or
`web/src/api/schema.d.ts`. `internal/api/openapi/spec_test.go` validates the
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
