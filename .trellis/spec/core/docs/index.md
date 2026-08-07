# Core Documentation Guidelines

> Source-backed ownership and publication rules for Simplus's repository
> knowledge base.

## Scope

These guides apply to `README.md`, `docs/**`, `AGENTS.md`, license/notices,
public claims in release material, and the documentation checker. They do not
turn `.trellis/tasks/**` into product documentation: Trellis task artifacts
record the current engineering task, while `docs/**` remains the canonical
public product/architecture/operations record.

`docs/README.md` is the public knowledge map. Its central rule is progressive
disclosure: update the canonical owner, then link to it instead of copying the
same product, architecture, safety, or status prose into several entry points.

## Pre-Development Checklist

- Start at `docs/README.md` and identify the one canonical document that owns
  the proposed statement.
- Read [Documentation Ownership](./documentation-ownership.md) before changing
  scope, architecture, plans, status, operational instructions, or decisions.
- Read [Privacy and Publication](./privacy-and-publication.md) before adding
  hardware/network evidence, troubleshooting material, screenshots, examples,
  release assets, identities, paths, or credentials.
- Compare the statement with code/tests and current accepted decisions. When
  they conflict, investigate the implementation and update the correct source;
  do not preserve stale prose by copying it elsewhere.
- Decide whether the change also requires `docs/compatibility.md`,
  `docs/troubleshooting.md`, the sole active plan, an ADR, or a test/checker
  update.

## Guide Index

| Guide | Project-specific contract |
| --- | --- |
| [Documentation Ownership](./documentation-ownership.md) | Canonical map, product scope, ADRs, active plans, status, operational docs, and mechanical checks |
| [Privacy and Publication](./privacy-and-publication.md) | Public/private evidence boundary, safe conclusions, repository/release review, and license material |

## Quality Check

- The change updates one canonical owner and entry-point links rather than
  creating a second manual.
- Product and compatibility claims match current code, tests, accepted
  decisions, and evidence levels; unverified limits remain explicit.
- Scope/architecture changes have an ADR; complex cross-layer or high-risk
  work updates the active plan, while a small edit does not invent a plan.
- No private deployment, identity, credential, topology, path, raw log/HIL,
  screenshot, database, packet capture, or troubleshooting timeline entered
  the public tree.
- Local Markdown links resolve, exactly one active plan remains, required
  knowledge-map files exist, and `AGENTS.md` stays a concise instruction map.

## Verification

```bash
make check-docs
```

For a public release/publication step, also run a redacting secret scanner over
the working tree and intended history as required by
`docs/privacy-and-publication.md`. Scanner output is sensitive and must not be
committed.
