# Containers and Privilege Boundaries

## Production Shape

Docker Compose is the sole supported production deployment path. Native Linux
development and `simplus-agent-dev` remain separate development/HIL workflows;
there is no native production bundle, installer, or uninstaller.

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

The Compose deployment candidate is scoped to Debian 13/amd64 with rootful
Docker, Compose 2.24+, and no userns remap. Clean-VM lifecycle Runtime evidence
is still pending (`docs/installation.md` and `docs/compatibility.md`), so do not
present it as a stable release. `scripts/release/check-container-host.sh`
checks that boundary, active or enabled legacy/development service conflicts,
`option1/new_id`, Docker, and the data path.
`scripts/release/prepare-container-host.sh` is a root mutation: it configures
module loading and loads `option`, though it does not write a USB ID. Run it
only with explicit deployment authorization.

Production administrators download a versioned GitHub Pre-release deployment
bundle and pull its literal-tag GHCR images. They do not clone the source tree
or build images locally. The root `compose.yaml`, `make container-build`, and
`make container-config CONTAINER_IMAGE_TAG=dev` remain source-development
validation interfaces only; the source Compose must not default to a production
version.

Starting Compose maps host devices and creates runtime network objects; it is
deployment/HIL-adjacent and is not authorized merely by a request to lint,
build, package, publish, inspect, or pull container artifacts. The host check
must reject an active or enabled legacy production unit or
`simplus-agent-dev` before Compose can own the modem and ports
(`docs/installation.md`).

## Scenario: Container-only production and legacy service exclusion

### 1. Scope / Trigger

Apply this contract when changing production deployment commands, host
preflight, release scripts, service ownership, or the Agent's dynamic USB ID
registration path.

### 2. Signatures

- Host preparation: `sudo bash scripts/release/prepare-container-host.sh`.
- Read-only preflight: `bash scripts/release/check-container-host.sh <deployment-root>`.
- Container-only driver registration: `simplus-agent register-option-driver`,
  invoked by `containers/agent-entrypoint.sh`.
- No supported native production bundle/build/install/uninstall command exists.
- Container release bundle:
  `scripts/release/build-container-release-bundle.sh <vX.Y.Z> <40-lowercase-hex-commit> <source-date-epoch> <existing-output-directory>`.

### 3. Contracts

- Compose is the only production deployment owner; native Go/Node development,
  Simulator and `simplus-agent-dev` do not become production alternatives.
- The production installation input is the versioned Release bundle. Its
  Compose contains literal `ghcr.io/leonfox28/simplus-{control,agent,netd}:vX.Y.Z`
  references; only the source-development Compose accepts
  `SIMPLUS_IMAGE_TAG`.
- Host preparation may install the fixed `option` module-load configuration and
  run `modprobe option`; it never writes a USB ID.
- Host preflight rejects any active **or enabled**
  `simplus-ml307a-bind.service`, `simplus-agent.service`,
  `simplus-netd.service`, `simplusd.service`, or
  `simplus-agent-dev.service`. Checking only current activity is insufficient
  because an enabled unit can reclaim the resource after reboot.
- The Agent container obtains every dynamic ID from
  `modemadapter.DefaultRegistry().USBSerialIDs()` and writes only the fixed
  mapped `option1/new_id` attribute. No Web, Compose, environment, flag, or
  caller supplies a VID/PID or path.
- Legacy private `/var/lib` state is neither inspected, migrated nor deleted by
  Compose. Its existence does not restore native production support.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| service-conflict check reaches a legacy/development unit that is active or enabled | preflight fails and names the unit; caller must stop and disable it |
| unit absent, or both inactive and disabled | the service-conflict check passes; earlier host checks remain authoritative |
| `option1/new_id` absent, non-root-owned or not owner-writable | preflight fails; do not widen the sysfs mount |
| rootless/userns, old Compose, symlink data path or unsupported host | preflight fails with the bounded host reason |
| registry contains zero, invalid or duplicate dynamic IDs | Agent registration fails before runtime startup |
| sysfs write returns `EEXIST` | treat only that condition as already registered |
| any other registration write/open/close failure | Agent registration fails non-zero |

### 5. Good / Base / Bad Cases

- Good: supported rootful host, no active/enabled conflicting unit and fixed
  sysfs point -> preflight passes; the Agent can then attempt registry-backed
  registration and its remaining entrypoint checks.
- Base: preserved legacy private data exists but every legacy unit is inactive
  and disabled -> preflight does not read that state, and an authorized Compose
  start creates/uses only its own deployment-root `./data`.
- Bad: revive a model-specific shell writer, accept a caller path/VID/PID, or
  check only `systemctl is-active` and allow an enabled unit to return on boot.

### 6. Tests Required

- `TestContainerHostCheckRejectsActiveOrEnabledLegacyServices` asserts the
  complete service set, active-or-enabled condition, and fail-closed message.
- Container contract tests assert the single writable sysfs bind, Agent
  no-network boundary, and entrypoint runtime/state contracts.
- Agent/registry tests assert ID validation, copied-result immutability,
  registry-owned selection, the exact fixed path, `EEXIST` handling, and
  failure of other write errors.
- `make check-container-files`, documentation checks, and a stale-reference
  scan protect shell syntax and the absence of native production entry points.

### 7. Wrong vs Correct

Wrong: check only current activity, or restore a model-specific host writer:

```bash
systemctl is-active --quiet "$service" && exit 1
printf 'vendor product\n' >/sys/bus/usb-serial/drivers/option1/new_id
```

Correct: reject current and next-boot owners, then leave ID selection to the
fixed container command:

```bash
if systemctl is-active --quiet "$service" || systemctl is-enabled --quiet "$service"; then
  exit 1
fi
simplus-agent register-option-driver
```

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
