# Simplus Agent Instructions

## Project Map

- Start with [the documentation map](docs/README.md).
- Product scope and non-goals: [product.md](docs/product.md).
- Process ownership and data flow: [architecture.md](docs/architecture.md).
- Current work and implementation status: [active plan](docs/plans/active/mvp.md) and [handoff](docs/handoff.zh-CN.md).
- Before editing code, read the relevant [engineering guidelines](docs/engineering/README.md) and nearby tests.
- Development commands and their side effects: [development.md](docs/development.md).
- Production deployment: [installation.md](docs/installation.md).
- Evidence levels and public records: [compatibility.md](docs/compatibility.md) and [privacy-and-publication.md](docs/privacy-and-publication.md).

## Working Conventions

- Keep product scope, architecture, and operational knowledge in their canonical documents; update links and summaries when those owners change.
- Use [decision records](docs/decisions/) for durable scope or architecture changes and follow [the plan guide](docs/plans/README.md) for complex or high-risk work.
- Search for existing narrow ports, helpers, and tests before adding abstractions. Keep application services separate from hardware protocols and persistence adapters.
- Start public API changes in `api/openapi.yaml`. Update generated Go, TypeScript, and sqlc outputs through their declared generators.
- Keep HTTP snapshots authoritative; browser realtime events only invalidate queries or signal attention.

## Validation

- Choose checks for the affected surface; start with focused tests and expand according to risk.
- Documentation: `make check-docs`.
- Go: affected package tests, then `make check-format`, `make lint`, and `make test` as needed.
- Web: `corepack pnpm --dir web typecheck`, `test`, `build`, and relevant fixture-based `e2e` checks.
- API, sqlc, or generator changes: `make verify-generated`.
- Container contracts: `make check-container-files` and `go test ./internal/containercontract`.
- Ordinary validation does not authorize deployment or real hardware side effects.

## Simplus Safety Boundaries

- Never publish credentials, private endpoints or topology, SIM/device identity, raw HIL evidence, packet captures, screenshots, or private troubleshooting material.
- Explicit user approval is required before real SMS/calls, RF changes, modem-persistent writes, or any HIL-1/HIL-2 action. Relevant fixed, read-only HIL-0 inspection is allowed.
- Keep hardware behind typed capabilities; never expose arbitrary AT/QMI commands or device paths through Web/API boundaries.
- Keep validation and repairs scoped to the requested change; a failing check does not authorize unrelated modifications.
