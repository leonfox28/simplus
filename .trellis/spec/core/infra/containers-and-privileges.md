# Containers and Privilege Boundaries

## Production Shape

`Dockerfile` has three production runtime targets:

- `control`: `simplusd`, `simplusctl`, and built Web assets, running as
  UID/GID 10001 without added capabilities;
- `agent`: the hardware binary and entrypoint, initially root only to prepare
  the runtime directory and register reviewed USB serial IDs, then running as
  UID 10002 with its capability bounding set cleared;
- `netd`: Mihomo, strongSwan, the two plugin packages, network tooling, and
  `simplus-netd`, kept root because it owns per-Line network objects.

`compose.yaml` orchestrates those images through five services:
`data-init`, `agent`, `netd`, `app`, and one-shot `bootstrap`. This does not
collapse the runtime into five business processes: data-init/bootstrap are
bounded lifecycle steps around the three responsibility boundaries described
in `docs/architecture.md` and
`docs/decisions/0021-container-production-deployment.md`.

## Non-Negotiable Isolation

Preserve the contracts asserted by
`internal/containercontract/contract_test.go`:

- every service drops the default capability set, uses a read-only root
  filesystem, and avoids `privileged` mode;
- Agent has `network_mode: none`, only ttyUSB device cgroup access, read-only
  USB/sysfs discovery trees, `/dev` mapping, and the single writable
  `option1/new_id` bind point;
- dynamic USB serial IDs come only from
  `modemadapter.DefaultRegistry().USBSerialIDs()`; Compose/Web/config cannot
  supply arbitrary VID/PID values (`internal/modemadapter/registry.go` and
  `cmd/simplus-agent/main.go`);
- `containers/agent-entrypoint.sh` validates the numeric device GID and real
  mounted directories, registers IDs, then uses `setpriv` to become UID 10002
  with no inherited, ambient, or bounding capabilities;
- app remains UID 10001 with no capabilities; it reaches Agent/netd only over
  shared Unix sockets;
- netd alone receives the reviewed network capability set, stays on the
  ordinary `runtime` bridge, and never uses host networking;
- fixed UIDs and `SO_PEERCRED` are part of the socket authorization contract.
  `internal/agentapi/listener.go` enforces allowed peer UIDs in addition to
  path modes. User-namespace remapping is therefore outside current support.

Do not fix a mount, UID, AppArmor, kernel, or preflight failure by enabling
`privileged`, host networking, a broad writable sysfs tree, all-device cgroup
access, or an arbitrary command/path input.

## netd Ownership and Preflight

`containers/netd-entrypoint.sh` validates private runtime/data mounts and runs
`containers/netd-preflight.sh` before starting the supervisor. The preflight
creates disposable netns, veth, nft TPROXY, and XFRM objects inside the netd
container namespace and then removes them. Failure keeps netd unhealthy, so
app/bootstrap do not proceed.

`cmd/simplus-netd/main.go` exposes fixed supervisor operations over an
authenticated Unix socket. Per-Line workers receive validated stable Line ID,
current opaque hardware target, runtime directory, and typed egress choice;
they do not accept shell, device, SIP, AT/QMI, or arbitrary network commands.

Changing any capability, path, UID, socket, network, or worker argument
requires updating its source, entrypoint/Compose wiring, contract test, and
canonical architecture/install docs together.

## Data and Initialization

Compose uses bind-mounted `./data/core` and `./data/agent`. `data-init` fixes
ownership/modes, installs the checked Zashboard tree, and seeds the pinned
Mihomo core only for unambiguous new state. It refuses symlinks and refuses to
guess an active version when existing version data lacks a current manifest
(`containers/data-init.sh` and its contract test).

Container deployment is a new instance and does not infer or migrate native
`/var/lib` state. Runtime named volumes contain sockets/temporary state, not
backups. Database files, credentials, subscriptions, logs, and Compose data
remain private and excluded by `.gitignore`/`.dockerignore` as described in
`docs/installation.md` and `docs/privacy-and-publication.md`.

The one-shot bootstrap waits for typed app health, then idempotently provisions
the sole administrator. Initial credentials appear only on first bootstrap;
never copy them into docs, fixtures, commands, or issue output.

## Host and Lifecycle Boundary

The current Compose deployment candidate is scoped to Debian 13/amd64 with
rootful Docker, Compose 2.24+, and no userns remap. Clean-VM lifecycle Runtime
evidence is still pending (`docs/installation.md` and
`docs/compatibility.md`), so do not present that scope as completed production
support. `scripts/release/check-container-host.sh` checks the candidate
boundary, existing native-service conflicts, `option1/new_id`, Docker, and the
data path. `scripts/release/prepare-container-host.sh` is a root mutation: it
configures module loading and loads `option`, though it does not write a USB
ID. Run it only with explicit deployment authorization.

`make container-build` builds the three images and `make container-config`
renders Compose. Starting Compose maps host devices and creates runtime
network objects; it is deployment/HIL-adjacent and is not authorized merely by
a request to lint or build container files. Native production and Compose must
not concurrently own the same modem or ports (`docs/installation.md`).

## Verification

Static checks are safe ordinary validation:

```bash
make check-container-files
go test ./internal/containercontract
make lint
```

When Docker is available and the task includes image/config verification:

```bash
make container-config CONTAINER_IMAGE_TAG=dev
make container-build CONTAINER_IMAGE_TAG=dev
```

Do not proceed from build/config to `docker compose up`, host preparation,
clean-VM smoke, or hardware HIL without the task's explicit scope and the
approval rules in
`.trellis/spec/core/infra/hardware-and-hil-safety.md`.
