# Retire native production deployment: execution plan

## Phase A — Remove obsolete production entry points

- [x] Delete the native bundle builder, installer, uninstaller and model-specific driver bind helper.
- [x] Remove deleted paths from strongSwan workflow filters without changing the package build job.
- [x] Verify remaining container host/release scripts retain their current ownership and syntax.

## Phase B — Align canonical product documentation

- [x] Update README and the documentation map to describe container-only production.
- [x] Convert `docs/installation.md` from a transition guide to the sole Compose production guide while retaining clean-VM and migration limitations.
- [x] Remove the native systemd fallback from architecture and handoff status.
- [x] Update the active MVP plan and ADR 0021 implementation status without rewriting native-cellular-SMS meanings or inventing evidence.

## Phase C — Synchronize Trellis contracts

- [x] In Phase 3.3, update documentation ownership and infrastructure specs so production is Compose-only and legacy service checks remain fail-closed.
- [x] Confirm native development/HIL and strongSwan packaging contracts remain present.

## Phase D — Verify and review

- [x] Prove deleted script names have no live references outside task/history evidence.
- [x] Prove production dynamic USB IDs have one registry-backed path and no native hardcoded writer remains.
- [x] Run `make check-docs`, `make check-container-files`, remaining release-script syntax checks and `go test ./internal/containercontract`.
- [x] Run lint/doc checks available offline, record any locked-tool cache limitation, and run `git diff --check` plus task context validation.
- [x] Dispatch an independent Trellis check pass and incorporate only in-scope fixes.

The retired file names remain only in current task/audit and archived task evidence.
`simplus-ml307a-bind.service` remains intentionally in the container host conflict
guard, its static contract test and the installation guide so active or enabled
legacy units fail closed.

## Validation commands

```bash
rg -n 'bind-ml307a|build-debian-bundle|install-debian|uninstall-debian|simplus-ml307a-bind' .
rg -n '2ecc[ :]+3012|option1/new_id' cmd internal containers scripts compose.yaml Dockerfile
make check-docs
make check-container-files
go test ./internal/containercontract
git diff --check
```

Do not start Compose or services, run host preparation, access hardware/private state, or execute HIL/communication/network mutations.
