# Design: Explicit Mihomo supervisor composition

## 1. Ownership correction

The corrected object graph is:

```text
cmd/simplusd
  -> choose local development implementation or Unix client
  -> construct mihomosupervisor.API with explicit error handling
  -> inject API into application/mihomo.RuntimeManager
  -> RuntimeManager expresses typed status/start/stop intent only
```

`internal/mihomosupervisor` continues to own local process/filesystem policy and
the Unix transport. `internal/application/mihomo` continues to own selection,
artifact readiness, persistence and restart/rollback semantics. No protocol or
runtime policy moves into the executable.

## 2. Constructor contract

Use one application constructor:

```go
var ErrRuntimeManagerConfiguration = errors.New("Mihomo runtime manager dependencies are invalid")

func NewRuntimeManager(
    root string,
    store RuntimeStore,
    artifacts ArtifactResolver,
    core CoreStatusReader,
    supervisor mihomosupervisor.API,
) (*RuntimeManager, error)
```

The root must be absolute and every interface is mandatory. Invalid input
returns an error wrapping the stable sentinel; it must never produce a manager
that can later panic. `Now` remains `time.Now` by default and tests may retain
the existing clock seam.

## 3. Executable composition seam

Add a small command-owned helper that returns `mihomosupervisor.API`:

- empty socket path -> `mihomosupervisor.NewLocal(root)`;
- configured socket path -> `mihomosupervisor.NewClient(socketPath)`.

Both constructors validate paths but neither starts a process nor connects to
the socket. Main handles helper failure before constructing the application
manager, then handles the application constructor error separately. Existing
exit-code and store-close conventions for dependency configuration failures are
retained.

## 4. Tests

- Application tests inject an in-memory fake implementing the typed API and
  assert observed requests/state, eliminating application-test ownership of a
  helper process.
- Constructor tests cover each missing dependency and a relative/empty root,
  using `errors.Is` against the stable sentinel.
- Command tests call only the composition helper, assert local/client concrete
  selection, and assert invalid paths fail. They do not call `Start`, contact a
  Unix socket, or execute Mihomo.
- Existing `internal/mihomosupervisor` tests remain the concrete local/client
  behavior boundary.

## 5. Compatibility and safety

Compose supplies an absolute socket and absolute data root, so its object graph
is unchanged except for using the unified constructor. An empty socket keeps
the current local development behavior. The change does not touch public data,
runtime stores, devices, network namespaces, RF, SIM state or communications.

Rollback is a source-only revert of this task. No runtime migration or data
rollback exists.
