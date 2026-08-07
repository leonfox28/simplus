# Hardware and HIL Safety

## Default Boundary

Real hardware is not an ordinary test dependency. `docs/compatibility.md`
defines evidence levels: Designed, Fixture, HIL-0 (fixed allowlisted read-only
inspection), HIL (an explicitly authorized controlled hardware vertical), and
Runtime. Code, a descriptor, a tty/QMI/UAC endpoint, or a passing fixture does
not promote a business capability by itself.

Relevant fixed HIL-0 inspection is allowed. Explicit user approval is required
before real SMS or calls, RF changes, modem-persistent writes, SIM/eUICC
mutation, external network/communication side effects, or any HIL-1/HIL-2
action. Approval is action- and scope-specific; an earlier decision or HIL on
one SIM/model/operator does not authorize a new one.

`docs/decisions/0003-v1-read-only-hardware.md` is the default hardware policy.
Later narrow decisions such as
`docs/decisions/0011-ml307a-host-vowifi-hil.md`,
`docs/decisions/0016-vowifi-sms-over-ims.md`, and
`docs/decisions/0017-managed-modems-and-capability-adapters.md` document
specific typed exceptions and explicitly leave other actions unauthorized.

## Typed Device Boundary

Maintain the enforced chain:

```text
Web/API -> application intent -> Agent typed service -> model adapter -> device
```

- Web/API may send stable business IDs and fixed typed intent only. It never
  sends AT/QMI/APDU text, a `/dev` or sysfs path, an interface number, a shell
  command, a network command, or an arbitrary vendor payload.
- `internal/agentapi/server.go` exposes bounded routes selected by constructor;
  `internal/agentapi/listener.go` verifies the Unix peer UID.
- `internal/modemadapter/registry.go` owns reviewed model matching, endpoint
  roles, dynamic USB IDs, capability evidence, and fixed model actions.
- `internal/attransport/transport.go` owns bounded tty I/O only. A compiled-in
  adapter chooses the command and parses semantics.
- `internal/modemadapter/registry_test.go` and
  `internal/agentapi/client_server_test.go` protect ambiguous matches,
  unverified capability non-advertisement, serialization, and absent write
  routes.

Never add a debug escape hatch that turns a typed route into a generic command
runner. A local-only flag or Unix socket does not make arbitrary hardware
commands safe.

## Safe Command Classification

Read the target before running it:

| Command/path | Classification |
| --- | --- |
| Unit/integration/Simulator checks (`go test`, `make test`, `make dev-sim`) | No real hardware evidence or authorization implied. |
| `make dev-hardware-probe` | Fixed read-only HIL-0 probe through `simplusctl`; relevant use is allowed, but output is sensitive and must stay private. |
| `make dev-agent-deploy` | Root-owned install/restart with a fixed probe; it mutates the host service and requires explicit approval even though its hardware probe is read-only. |
| `make dev-hardware` / `dev-hardware-lan` | Starts the hardware backend. Startup alone does not authorize clicking RF or communication actions; LAN exposure remains trusted-network only. |
| `make build-vowifi-hil` and protocol fixture tests | Build/test only; they do not authorize a real runner. |
| `scripts/dev/run-ml307a-vowifi-hil.sh`, Compose hardware startup, real messaging/calls/RF/eUICC | Controlled side effects; require an approved plan, exact target/preconditions, and explicit authorization. |
| `scripts/release/prepare-container-host.sh` or install/uninstall flows | Host mutation, not HIL-0; require deployment authorization and rollback understanding. |

`docs/development.md` is the operational source for current command meanings.
If a target's behavior changes, update that document and this classification
in the same task.

## Preconditions for Authorized Hardware Work

Before an approved side-effecting action:

- restate the exact action, target model/Line, expected external effect, and
  stop condition;
- confirm no competing native/container Agent owns the modem or ports;
- use the narrow typed runner and current adapter evidence; never substitute a
  model, interface, path, or command from memory;
- start with a fixture and, when useful, fixed HIL-0 state confirmation;
- preserve serial ownership, operation IDs, timeouts, read-back/state
  observation, and fail-closed behavior;
- do not silently retry an uncertain SMS/call/RF outcome or fall back to
  Simulator/direct egress;
- prepare cleanup/rollback for temporary network and service state before the
  action.

For Host VoWiFi, `docs/development.md` requires the already verified RF-Off
baseline, unique target, SIM ready, no active call, and explicit authorization
for the current runner. Product decoupling of RF and VoWiFi does not prove an
RF-On scenario compatible.

## Sensitive Evidence

Never commit or publish credentials, subscription/node material, private
endpoints or LAN topology, SIM/eUICC/IMS/device identity, phone numbers, raw
AKA/SIP material, packet captures, databases, screenshots, raw HIL/service
logs, command transcripts, or private troubleshooting timelines.

`docs/privacy-and-publication.md` requires raw evidence to stay in the external
private record system. Public results go to `docs/compatibility.md` as a
minimal statement of capability, evidence level, unverified boundaries, and
the automated regression that protects the conclusion. Stable public failure
semantics go to `docs/troubleshooting.md`, without raw addresses, payloads, or
identity material.

## Failures and Scope

- A failed HIL or check does not authorize a broader probe, a write, a new
  model command, a privilege expansion, a host-network/privileged container,
  or an unrelated repair.
- Stop on target ambiguity, identity/generation change, missing capability,
  unexpected RF/SIM/call state, persistence uncertainty, cleanup failure, or
  output that risks disclosure.
- Report the verified stage and stable error. Do not rerun a side-effecting
  action without new evidence and renewed authorization when its outcome may
  already have occurred.
- Never weaken `unsupported`/`unverified` evidence or public compatibility
  wording to match an implementation that lacks the required fixture/HIL.
