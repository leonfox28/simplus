# Design: Layer-boundary audit

## 1. Audit model

This is a static, evidence-backed architecture audit. Directory distance is a candidate signal, not the verdict. A hard violation requires a production behavior path that bypasses the next typed boundary, leaks a concrete lower-layer implementation into an upper-layer decision, or performs device/protocol work outside its owner.

The report uses four result classes:

1. **Confirmed violation** — a production call chain crosses a disallowed behavior boundary.
2. **Architecture concern** — a concrete type/import or responsibility leak increases coupling but does not itself prove a runtime bypass.
3. **Allowed exception** — composition, passive shared types, generated ownership, OS/runtime ownership, or deliberate integration-test assembly matches a documented rule.
4. **False positive / non-product evidence** — lexical hits, generated output, fixtures, archived code references, or comments do not create a product behavior path.

Severity is based on impact:

- **Critical**: Web/API or another external boundary exposes arbitrary modem, device, shell, network, AT/QMI/APDU, or path control.
- **High**: production code outside the reviewed hardware/runtime owner directly selects or executes modem/device protocol behavior.
- **Medium**: a production layer bypasses its consumer-owned port, constructs a lower concrete implementation outside a composition root, or moves model/path decisions upward.
- **Low**: compile-time concrete-type leakage or responsibility coupling without a demonstrated behavior bypass.

## 2. Runtime paths and allowed edges

The audit treats the repository as several typed paths rather than one artificial linear stack.

| Caller/owner | Allowed next behavior boundary | Notes |
| --- | --- | --- |
| Web pages/components | generated Query/SDK helpers and narrow handwritten API helpers | Pages must not own raw Fetch/EventSource, payload contracts, device paths, model commands, or backend state. |
| Browser API runtime | authenticated public HTTP/SSE contract | Raw Fetch and EventSource are allowed only in their documented boundary owners. |
| `internal/api/httpapi` | consumer-owned application service interfaces | OpenAPI/domain types may be referenced for mapping; handlers must not call storage, Agent implementations, supervisors, or device protocols directly. |
| `internal/application/**` | consumer-owned repository/service ports, stable domain types, typed Agent/supervisor clients | Application code may express business intent but must not select models, endpoints, commands, device paths, SQL implementations, or fallback transports. |
| Persistence adapters | SQLite/sqlc/filesystem primitives owned by the adapter | They implement application-facing contracts and do not call upward into HTTP/application behavior. |
| Typed Agent/supervisor clients and servers | fixed protocol requests/handlers | Wire boundaries may validate and translate typed operations, but may not expose arbitrary command/path payloads. |
| Hardware discovery/runtime | registry-selected capability interfaces | Discovery may inspect bounded sysfs/device metadata and resolve endpoints; it must not invent model commands or expose runtime targets upward. |
| Model adapters and model-owned business drivers | bounded protocol transport | Only these owners select fixed AT/QMI/APDU/vendor semantics and parse model responses. |
| Generic protocol transports | OS/device I/O | Transport owns framing, limits, timeouts and lifecycle; it must not select model commands or business behavior. |
| netd runtime workers | bounded process/network primitives | Only netd-owned workers may construct the reviewed temporary network/SIP runtime from typed stable inputs. |
| strongSwan SIM-AKA C bridge | fixed Agent SIM-AKA Unix request and fixed IMS APN hook | The product plugin may translate strongSwan's typed AKA call to the reviewed fenced Agent route; it must not own AT/APDU/model selection or accept a public/runtime-selected command or socket path. |

`cmd/**` files are composition roots. They may import several layers to construct the object graph, but the audit will still flag business policy, model branching, raw user-provided protocol parameters, or lower-layer behavior implemented there instead of merely wired there.

`internal/domain/**` is treated as passive shared business vocabulary, not a callable runtime layer. HTTP or storage references to domain records are not automatically layer skips; behavior implemented in domain packages must remain protocol- and persistence-neutral.

## 3. Evidence collection

The primary audit combines independent evidence sources:

- enumerate hand-written production Go, Web, product-owned C and production shell/build source separately from tests, generated output, vendored/upstream source, tool caches, build artifacts, task history and private data paths;
- generate production and test Go import graphs with `go list`, then group edges by architectural owner;
- enumerate TypeScript imports and search pages/components for raw network, duplicated payload, device/model and protocol use;
- search for concrete lower-layer imports and constructors in HTTP/application/domain packages;
- search for AT/QMI/APDU/vendor command strings, tty/serial/termios, `/dev`, sysfs, USB/interface/VID/PID, shell/process/network primitives and generic command surfaces;
- inspect OpenAPI plus typed Unix request types/routes for raw command, device path or arbitrary vendor payload exposure;
- trace every candidate from caller through interfaces, constructor wiring and concrete implementation until the behavior owner is proven;
- compare the proven path to the allowed-edge table and accepted architecture/ADR contracts.

Lexical scans only create candidates. Every confirmed finding requires current `file:line` anchors and a summarized call chain.

## 4. Coverage and report contract

The final `.trellis/tasks/08-14-audit-layer-boundaries/audit.md` must contain:

1. executive conclusion and finding counts by class/severity;
2. scope, exclusions and safety statement;
3. the final allowed-edge matrix;
4. a coverage matrix with every runtime owner—including the product-owned strongSwan SIM-AKA C bridge and Compose/container/release/packaging owners—its inspected source/package set, actual outbound behavior boundary, candidate count and conclusion;
5. confirmed violations ordered by severity;
6. architecture concerns and allowed exceptions, including composition roots and integration tests;
7. test/tool-only observations;
8. false-positive rationale where a high-risk keyword was dismissed;
9. static-analysis limitations and prioritized, minimal remediation recommendations.

Each confirmed violation or concern records: ID, severity/class, caller layer, target layer, source anchors, call chain, violated rule, impact, minimum remediation direction and suggested non-HIL verification.

## 5. Safety, compatibility and rollback

- No product code, spec, generated output, configuration or dependency is changed by the audit.
- Do not run deployment, Compose startup, host preparation, hardware probe, HIL runner, real SMS/call, RF, SIM/eUICC, network mutation or arbitrary device command.
- Do not inspect `data/**`, private runtime stores, device nodes, raw logs, captures or screenshots. Do not quote identities, endpoints, topology or raw protocol evidence into the report.
- Safe analysis is limited to repository source inspection, import metadata, textual/AST-like searches and non-executing repository metadata commands.
- Rollback is deletion or revision of task-local planning/report artifacts only; no application state exists to roll back.

## 6. Independent review

The primary audit should be performed by a Trellis research/check agent constrained to read-only product inspection and task-local evidence writing. A separate Trellis check pass must verify that every layer is represented, every finding is call-chain-backed, every allowed exception is justified, no product file was modified, and no prohibited hardware/private action occurred.
