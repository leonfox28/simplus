# Documentation Ownership

## Canonical Map

Use the owner already declared by `docs/README.md`:

| Document | Owns |
| --- | --- |
| `docs/product.md` | Trusted-LAN product goal, supported phased capabilities, non-goals, success criteria, and universal product safety boundaries |
| `docs/architecture.md` | Current process/data flow, dependency direction, state ownership, typed hardware/network boundaries, persistence shape, and mechanical invariants |
| `docs/plans/active/mvp.md` | The sole active cross-layer execution plan, progress, next product work, and plan-level acceptance |
| `docs/handoff.zh-CN.md` | Sanitized current implementation/evidence handoff; it is a status summary, not a new source of scope or architecture |
| `docs/development.md` | Native Linux setup, validation, Simulator, container-build distinction, HIL-0 commands, and controlled HIL entry points |
| `docs/installation.md` | Current Compose candidate support, host preparation, lifecycle, privilege/data boundaries, and native-production transition |
| `docs/compatibility.md` | Public capability matrix, Designed/Fixture/HIL-0/HIL/Runtime evidence, and explicit unverified boundaries |
| `docs/troubleshooting.md` | Stable public error codes, fact layers, safe diagnostic order, and permitted support material |
| `docs/privacy-and-publication.md` | Public/private record boundary and publication procedure |
| `docs/decisions/*.md` | Accepted changes to product scope, architecture, licensing, deployment, or evidence authorization |

Root `README.md` is a public project entry point and links to this map. Keep it
brief; do not turn it, `AGENTS.md`, or the handoff into a copy of all canonical
documents.

## Product Scope Guardrails

`docs/product.md` defines Simplus as a single-administrator trusted-LAN Linux
tool for a small number of 4G/5G modems, stable Lines, SMS and calls, with
phased removable-eUICC installed-profile management and narrowly scoped Host
VoWiFi/Mihomo egress. It explicitly is not a public SaaS, multi-tenant system,
general cellular data gateway/hotspot, host-wide proxy, full eSIM RSP platform,
enterprise PKI/audit/update system, or general remote-control platform.

Do not broaden those terms through an implementation or documentation edit.
New scope requires an explicit user decision and a new/superseding ADR. The
history in `docs/decisions/0001-product-scope-reset.md` and
`docs/decisions/0002-restore-vowifi-mihomo-euicc.md` shows why the current
narrow wording must be resolved through decisions rather than inferred from
old plans or existing code.

## Architecture and Code Truth

Update `docs/architecture.md` when ownership, dependency direction, a public
data flow, a persisted truth source, or a mechanical invariant changes. Cite
representative code/tests and describe current assembly, including limitations.
For example, the architecture correctly distinguishes persisted Line identity
from transient Agent targets and states that ordinary cellular SMS/calls are
not yet wired in production; that evidence is visible in
`internal/application/line/service.go`, `cmd/simplusd/main.go`, and
`internal/modemadapter/registry_test.go`.

When code and docs appear inconsistent:

1. inspect the current implementation, tests, accepted decisions, and public
   compatibility claim;
2. determine whether the code or documentation is stale;
3. fix the approved source of truth and its enforcing test/check;
4. update summaries/links only after the canonical owner is correct.

Do not document an aspirational architecture as current, and do not treat dead
or fixture-only code as a production capability.

## Decision Records

ADRs in `docs/decisions/` are numbered, dated, and normally contain status,
background, decision, and consequences. Superseding decisions name the earlier
record they revise; examples include
`docs/decisions/0017-managed-modems-and-capability-adapters.md` and
`docs/decisions/0019-line-identity-and-communication-paths.md`.

Create or update an ADR for a durable product-scope, architecture, deployment,
license, security boundary, or side-effect authorization decision. Do not
rewrite historical context to make the latest design look inevitable; preserve
the earlier decision and record the revision. Implementation detail that does
not alter a durable decision belongs in code/tests or the active plan, not a
new ADR.

License statements must match `LICENSE`, `THIRD_PARTY_NOTICES.md`, and
`docs/decisions/0013-polyform-noncommercial-license.md`: the root project is
noncommercial source-available, not OSI open source, and separately marked
components/assets retain their own licenses.

## Plans and Handoffs

`docs/plans/README.md` requires a plan for cross-layer verticals, migrations,
or high-risk hardware work and says small single-file changes need none. A plan
records goal, non-goals, steps, acceptance, progress, and decisions. Keep
exactly one Markdown plan under `docs/plans/active/`; archive a completed plan
as that guide directs and remove its active-map reference.

The handoff summarizes sanitized current capability and the next work. Update
it after meaningful progress changes, but keep normative scope in product,
invariants in architecture, evidence in compatibility, and execution detail in
the plan. `scripts/dev/check-docs.py` rejects retired remote-development
markers in the handoff.

Trellis `.trellis/tasks/**` PRD/design/implement files are task-control
artifacts and may be more detailed or temporary. They do not replace the
public active plan or accepted ADR when a lasting project decision changes.

## Operational and Evidence Docs

- Put reproducible native development and safe command meanings in
  `docs/development.md`; keep private host paths and one-off recovery steps out.
- Put production install/lifecycle facts in `docs/installation.md`; distinguish
  implemented contract, development-VM smoke, clean-VM Runtime, and hardware
  HIL.
- Put only sanitized evidence summaries with explicit levels in
  `docs/compatibility.md`. A fixture, endpoint, or successful build does not
  imply HIL/Runtime.
- Put stable codes and general diagnostic order in `docs/troubleshooting.md`.
  Raw SIP, identities, addresses, payloads, logs, and incident timelines stay
  private.

## Mechanical Checks

`scripts/dev/check-docs.py`, invoked by `make check-docs`, currently verifies:

- required root/canonical documents exist;
- exactly one active plan exists;
- public private-record directories are absent;
- `AGENTS.md` stays at or below 100 lines;
- retired handoff markers are absent;
- local Markdown links resolve inside the repository;
- public Markdown lacks known private addresses/paths, credential URLs/query
  values, proxy share URIs, telecom identity values, and site-specific markers.

Keep this checker small and deterministic. If a new invariant is objective and
important, add a regression to the checker; do not claim it is enforced when
it is only prose. Passing it is necessary but does not replace human privacy,
scope, evidence, and license review.

## Avoid

- Copying the product non-goals or full validation guide into every entry
  point.
- Using a handoff, old plan, issue, chat, private record, or Trellis task as the
  sole source for a durable public decision.
- Deleting an accepted ADR because a later decision revised it.
- Updating a compatibility claim without naming evidence level and remaining
  limits.
- Adding external links where a stable repository-owned explanation is the
  actual canonical source.
