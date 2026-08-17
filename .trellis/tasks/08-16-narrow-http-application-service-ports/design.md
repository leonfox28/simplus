# Design: HTTP-owned application service ports

## 1. Boundary and ownership

The runtime flow remains:

```text
cmd/simplusd concrete assembly
  -> httpapi consumer-owned interface
     -> existing application implementation
        -> its existing typed lower ports
```

`internal/api/httpapi` owns the interfaces because its handlers are the
consumer. The application packages continue to own their request/result values
and behavior. `cmd/simplusd` continues to select and inject concrete services.

## 2. Port contracts

Add four roles beside the current HTTP interfaces in `server.go`:

```go
type HealthReader interface {
    Snapshot(context.Context) (health.Snapshot, error)
}

type SetupManager interface {
    Status(context.Context) (setupapp.Status, error)
    ConsumeBootstrap(context.Context, string) (setupapp.SessionGrant, error)
    ReadSession(context.Context, string) (setupapp.Session, error)
    ConfigureAdministrator(context.Context, string, setupapp.AdministratorInput) (setupapp.Session, error)
    ConfigureStorage(context.Context, string, setupapp.StorageInput) (setupapp.Session, error)
    ConfigureHTTPS(context.Context, string, setupapp.HTTPSInput) (setupapp.Session, error)
    ConfirmHTTPS(context.Context, string, string) (setupapp.Session, error)
    ReadRootCertificate(context.Context, string) ([]byte, string, error)
    ConfirmHardwareReview(context.Context, string, setupapp.HardwareReviewInput) (setupapp.Session, error)
    Complete(context.Context, string, setupapp.HardwareReviewInput) (setupapp.Completion, error)
    BeginAdministratorSetup(context.Context) (setupapp.SessionGrant, error)
}

type InventoryReader interface {
    Snapshot(context.Context) (inventory.Snapshot, error)
    Topology(context.Context) (inventory.Topology, error)
}

type RealtimeManager interface {
    Subscribe() *realtime.Subscription
    Publish([]realtime.Topic, realtime.Attention)
}
```

The exact names above are part of the planned contract. The Setup method set is
larger than the other ports because HTTP owns the full interactive Setup flow;
it intentionally excludes the control-only `GenerateBootstrap` and
`ProvisionAdministrator` operations.

## 3. Construction and compatibility

- Change only the first three `New` parameter types and corresponding `Server`
  fields. Preserve argument order, variadic contacts, logger default, return
  type, and all call sites.
- Change `WithRealtime` and the `realtime` field to `RealtimeManager`. The
  existing `*realtime.Hub` structurally satisfies the port.
- Store interface values unchanged so concrete Health/Setup/Inventory nil
  receivers can continue returning their stable configuration errors. Use one
  boundary-local typed-nil-aware absence predicate only in Login, realtime
  authorization, publication, and stream availability checks. This preserves
  pointer-era optional checks without validating or constructing dependencies.
- Keep application-owned DTOs in signatures. This repair removes concrete
  implementation coupling, not legitimate typed application vocabulary.

No OpenAPI, generated code, storage schema/data, environment option, process
lifecycle, handler behavior, or application implementation changes.

## 4. Test design

Add a co-located `internal/api/httpapi/server_ports_test.go` (or an equivalent
focused section in the existing test) with:

- compile-time assertions that the four production implementations satisfy the
  HTTP ports;
- independent fake implementations proving `New`/`WithRealtime` do not require
  concrete service values;
- bounded calls proving fake realtime Subscribe and Publish are reached;
- raw-nil and typed-nil cases for all four converted dependencies, covering
  Setup/realtime availability checks and Health/Setup/Inventory nil-receiver
  error dispatch separately.

Retain the existing `httptest` suite as behavior evidence for Setup gates,
Health/Inventory mapping, authenticated SSE, heartbeat/session expiry, and
publication. Tests must use synthetic channels/stores only.

## 5. Architecture verification

Focused scans must show no `*health.Service`, `*setup.Service`,
`*inventory.Service`, or `*realtime.Hub` in production `httpapi` fields or
constructor/option parameters, while every new port method has a live handler
call. `cmd/simplusd` must remain the sole production assembly point and import
no new adapter.

## 6. Risk and rollback

The primary risk is typed-nil interface drift: plain interface equality loses
optional absence checks, while normalization to true nil loses deliberate
nil-receiver error dispatch. A boundary-local predicate plus both regression
classes closes it. A secondary risk is widening the Setup port by copying every
exported method; the live-call matrix and an explicit negative scan for the two
excluded methods close that risk.

Rollback is a source-only revert. There is no migration or external state to
undo.
