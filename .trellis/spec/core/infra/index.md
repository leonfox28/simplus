# Core Infrastructure Guidelines

> Source-backed conventions for local tooling, generated/release inputs,
> production containers, and hardware/HIL safety.

## Scope

These guides apply to `Makefile`, root toolchain/version files, `Dockerfile`,
`compose.yaml`, `containers/**`, `scripts/**`, `packaging/**`, `third_party/**`,
and infrastructure-focused workflows/tests. Product code still follows the
backend or Web package guides.

Daily development is native Linux worktree development. The three-image Docker
Compose shape is the sole supported production deployment path, although it
remains a deployment candidate rather than a stable release until clean-VM
lifecycle evidence is complete. Real hardware and Host VoWiFi validation remain
separately authorized evidence paths. This distinction is defined in
`docs/development.md`, `docs/installation.md`, and
`docs/decisions/0021-container-production-deployment.md`.

## Pre-Development Checklist

- Read `docs/development.md` before choosing a Make target; many similarly
  named targets have very different privilege and side-effect profiles.
- Read [Build and Generated Files](./build-and-generated-files.md) before
  changing tool versions, generators, vendored assets, packaging, or release
  workflows.
- Read [Containers and Privileges](./containers-and-privileges.md) before
  changing images, Compose services, mounts, UIDs, capabilities, sockets, or
  host preparation.
- Read [Hardware and HIL Safety](./hardware-and-hil-safety.md) before any
  command that discovers, deploys to, authenticates with, writes to, or sends
  traffic through real hardware.
- Search `Makefile`, `.github/workflows/**`, docs, scripts, and contract tests
  for the target/value being changed so mirrored operational paths stay
  aligned.

## Guide Index

| Guide | Project-specific contract |
| --- | --- |
| [Build and Generated Files](./build-and-generated-files.md) | Pinned native toolchain, Make targets, generated ownership, third-party inputs, and release validation |
| [Containers and Privileges](./containers-and-privileges.md) | Three-image boundary, fixed identities, exact mounts/capabilities, private data, and contract tests |
| [Hardware and HIL Safety](./hardware-and-hil-safety.md) | Typed device boundary, approval gates, HIL evidence levels, sensitive artifacts, and fail-closed behavior |

## Quality Check

- Native development, container production, and HIL paths remain distinct;
  ordinary validation does not acquire root, attach devices, or contact a real
  network/SIM unexpectedly.
- A generator/release input has one declared source and a reproducible,
  checked output path; generated or downloaded artifacts are not hand-edited.
- Container changes preserve the separate control/Agent/netd privilege model,
  fixed UID/socket contract, Agent no-network boundary, and netd ownership of
  temporary network objects.
- Host preparation and entrypoints fail closed on missing ownership,
  capability, kernel, path, or identity conditions rather than widening
  privileges.
- Hardware evidence and public claims state the correct level from
  `docs/compatibility.md`; no raw evidence or private environment detail enters
  the repository.

## Verification

Choose checks by the touched surface:

```bash
make doctor
make check-container-files
go test ./internal/containercontract
make verify-generated
make check-docs
make lint
make test
```

`make container-config` and `make container-build` require Docker but do not by
themselves authorize `docker compose up`, host preparation, deployment, or
HIL. Packaging targets may download locked inputs; record that network need
before running them.
