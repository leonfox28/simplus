# Narrow HTTP application service ports

## Goal

Remove the HTTP boundary's compile-time dependency on the complete concrete
health, setup, inventory, and realtime application implementations. The HTTP
server should depend only on consumer-owned ports for the operations it uses,
without changing public API behavior, application behavior, or runtime
composition.

## Background

The layer-boundary audit recorded C-01 as a Low architecture concern. The
composition root currently injects `*health.Service`, `*setup.Service`,
`*inventory.Service`, and `*realtime.Hub`; `internal/api/httpapi.Server` stores
those concrete pointers even though its other adjacent application dependencies
are expressed as HTTP-owned interfaces. This is not a direct storage, hardware,
or transport bypass, but it unnecessarily couples the HTTP package to complete
implementations.

Current evidence is recorded in
`.trellis/tasks/08-14-audit-layer-boundaries/audit.md:198` and the live fields,
constructor, option, and calls in `internal/api/httpapi/server.go`.

## Requirements

- R-01: Define HTTP-owned interfaces containing only the health, setup,
  inventory, and realtime operations the HTTP server actually invokes.
- R-02: Change `Server`, `New`, and the realtime option/assembly seam to accept
  those interfaces instead of concrete pointers.
- R-03: Preserve the existing application DTOs, error mapping, request/response
  behavior, authentication/setup gating, realtime topics, heartbeat behavior,
  and composition-root implementation choices.
- R-04: Keep each port cohesive and consumer-specific; do not introduce a
  shared catch-all interface or move application behavior into HTTP.
- R-05: Add compile-time test fakes or assertions that prove the HTTP server can
  be assembled without depending on concrete service values.
- R-06: Keep validation synthetic and offline; no service startup, Compose,
  external network, modem, RF, SMS/call, or HIL action is authorized.
- R-07: Preserve both existing nil behaviors when concrete pointers become
  interfaces: optional Setup/realtime availability checks must treat typed nil
  as absent, while direct Health/Setup/Inventory handler calls must retain the
  stable errors returned by their nil-receiver methods.

## Out of Scope

- Changing OpenAPI schemas, routes, status codes, payloads, or Web behavior.
- Refactoring the internals of health, setup, inventory, or realtime services.
- Changing stores, hardware adapters, supervisors, deployment, or runtime
  lifecycle policy.
- Addressing the separate dormant `resourcelease` SQLite-type concern (C-02).

## Acceptance Criteria

- [ ] `internal/api/httpapi.Server` has no concrete `*health.Service`,
  `*setup.Service`, `*inventory.Service`, or `*realtime.Hub` field.
- [ ] HTTP construction accepts narrow consumer-owned ports and production
  assembly still injects the existing concrete implementations.
- [ ] Every interface method corresponds to a live HTTP call; no unused or
  lower-layer operation is exposed through the new ports.
- [ ] Existing HTTP router/handler tests pass, and focused interface-fake tests
  cover the changed construction boundary including realtime subscription and
  publication.
- [ ] Raw and typed-nil dependencies remain unavailable to optional
  Setup/realtime branches, and typed-nil Health/Setup/Inventory implementations
  retain their pre-refactor nil-receiver error dispatch.
- [ ] Focused and repository-required formatting, lint/type, generated/docs,
  task validation, ownership scans, and diff checks pass without live runtime
  or hardware access.

## Technical Notes

- Concrete application DTO types may remain in method signatures; the concern
  is dependency on complete implementations, not use of application-owned
  request/result vocabulary.
- Use four cohesive ports: one each for health, Setup workflow, inventory, and
  realtime subscription/publication. The Setup port deliberately excludes
  `GenerateBootstrap` and `ProvisionAdministrator`, which HTTP does not call.
- Keep the current `New` argument order/return shape and the optional
  `WithRealtime` assembly shape. A dependency-object migration or constructor
  validation redesign is not part of this concern.
