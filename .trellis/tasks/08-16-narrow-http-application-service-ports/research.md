# Research: HTTP application service coupling

## Baseline

- Reviewed commit: `19225e92a3f187cf7b2d7b0b428901906441e01c`.
- Parent finding: C-01 in
  `.trellis/tasks/08-14-audit-layer-boundaries/audit.md:198`.
- Severity is Low: no direct storage, hardware, supervisor, provider, or raw
  transport call was found in `internal/api/httpapi`.

At the baseline, `internal/api/httpapi/server.go:163-185` stores
`*health.Service`, `*setup.Service`, `*inventory.Service`, and
`*realtime.Hub`. `New` requires the first three concrete pointers at
`server.go:274-293`; `WithRealtime` requires the Hub at `server.go:254-259`.
Production injects those implementations at `cmd/simplusd/main.go:335-354`.

## Exact live HTTP method surface

| Proposed consumer role | Live methods called by HTTP | Baseline call anchors |
| --- | --- | --- |
| Health reader | `Snapshot(context.Context) (health.Snapshot, error)` | `server.go:3031` |
| Setup manager | `Status`, `ConsumeBootstrap`, `ReadSession`, `ConfigureAdministrator`, `ConfigureStorage`, `ConfigureHTTPS`, `ConfirmHTTPS`, `ReadRootCertificate`, `ConfirmHardwareReview`, `Complete`, `BeginAdministratorSetup` with their existing `setup` values | `server.go:436,669-926,1018,1145-1152` |
| Inventory reader | `Snapshot(context.Context) (inventory.Snapshot, error)`, `Topology(context.Context) (inventory.Topology, error)` | `server.go:842-869,1226,2844` |
| Realtime manager | `Subscribe() *realtime.Subscription`, `Publish([]realtime.Topic, realtime.Attention)` | `server.go:390,486` |

Repository search confirms HTTP does not call Setup's `GenerateBootstrap` or
`ProvisionAdministrator`. The former appears only in HTTP integration-test
fixture setup through a concrete local variable, not through `Server`.

## Existing behavior to preserve

- `cmd/simplusd` remains the composition root and can inject the existing
  concrete values by structural interface satisfaction; no wrapper adapter is
  needed.
- Setup status continues to gate business routes, login bootstrap-session
  creation, and realtime session revalidation.
- Realtime remains one bounded topic/attention mechanism with one initial
  resync subscription and non-blocking publication semantics owned by
  `internal/application/realtime`.
- Public OpenAPI, cookies, error/status mapping, handler timeouts, SSE wire
  shape, application DTOs, stores, and generated outputs do not change.
- Existing concrete pointer parameters have two distinct nil behaviors:
  Login/realtime availability checks treat nil Setup/Hub pointers as absent,
  while Health, Setup Status, and Inventory methods deliberately accept nil
  receivers and return stable configuration errors. Go interface conversion
  can hide nil from equality checks, but replacing the interface with true nil
  would also destroy nil-receiver dispatch. The refactor therefore needs a
  typed-nil-aware absence predicate only at optional availability branches.

## Test evidence and gaps

- `internal/api/httpapi/server_test.go` already exercises the real router,
  Setup flow/gates, Inventory/Health mapping, authenticated SSE subscription,
  session expiry, and realtime publication using concrete implementations.
- The missing proof is architectural: no test assembles the HTTP server from
  independent fakes for these four roles, and no assertion prevents the fields
  or parameters from returning to concrete pointers.
- A co-located port test can provide structural assertions for production
  implementations, independent fake implementations, bounded fake
  subscribe/publish calls, optional raw/typed-nil absence handling, and
  nil-receiver error preservation without starting a server or external
  runtime.

## Planning conclusion

Define exactly four HTTP-owned interfaces beside the existing HTTP consumer
interfaces. Keep one Setup workflow interface even though it has eleven
methods: all eleven are live calls of the same boundary consumer, while
splitting it would require injecting the same concrete service repeatedly and
would not reduce authority. Do not create an adapter package, shared service
interface package, dependency-object migration, or application-layer change.

No user-owned product, compatibility, UX, scope, or risk decision remains.
