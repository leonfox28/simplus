# Implementation plan: Make Setup dependencies explicit

## Phase 1 — Application boundary

- [x] Add application-owned dependency, directory identity and Local CA bundle types plus stable constructor validation.
- [x] Replace dynamic StateStore capability assertions with direct dependency assignment and preserve random/clock defaults.
- [x] Remove concrete security/storage imports and make reduced optional capabilities fail closed without nil dereference.

## Phase 2 — Production composition

- [x] Add focused `cmd/simplusd` Setup assembly adapters for password, directory identity, secretbox and Local CA translation.
- [x] Derive the fixed instance secret path once, inject every SQLite role explicitly, handle constructor failure and reuse the unchanged path for the existing keyring.
- [x] Add a synthetic temp-directory composition test proving the production dependency set is complete.

## Phase 3 — Caller and test migration

- [x] Refactor Setup tests to inject clock/random/fakes through `Dependencies` and remove all direct private Service-field assignments.
- [x] Update control bootstrap tests with explicit exercised persistence capabilities.
- [x] Update HTTP tests with explicit State-only and full-Setup helpers while preserving existing integration assertions.

## Phase 4 — Validation and review

- [x] Run formatting plus targeted race tests for Setup, `cmd/simplusd`, control and HTTP API.
- [x] Run focused import/type-assertion/private-field/key-path scans, supported full Go tests/vet/lint components, task validation and `git diff --check` without runtime side effects.
- [x] Dispatch an independent Trellis check for dependency completeness, optional-shape failure behavior, concrete-type removal and observable Setup compatibility; incorporate only in-scope fixes.
- [x] In Phase 3.3, capture the explicit Setup dependency/adapter contract in backend specs if the final implementation establishes reusable guidance not already present.

### Independent check evidence (2026-08-15)

- No product or test defect was found; no reviewer code fix was required.
- Uncached race tests passed for `internal/application/setup`, `cmd/simplusd`, `internal/control` and `internal/api/httpapi`.
- Uncached supported Go tests passed for `./cmd/... ./internal/...`; Go vet, the locally cached Actionlint binary, Go formatting and Web TypeScript type-check all passed.
- Task validation, `git diff --check`, constructor-caller/import/type-assertion/private-field/key-path scans and API/generated/schema/Web scope scans passed; an offline same-version OpenAPI/sqlc/Web regeneration preserved the complete worktree content manifest byte-for-byte.
- Validation used synthetic temporary storage only. No service/Compose, external endpoint, private-data, hardware/HIL, communication, RF/SIM or network action was performed.

## Risk and rollback points

- Do not accidentally change the fixed secret-key path while deduplicating its construction.
- Directory and Local CA adapter mapping must copy every field consumed by Setup; private key slices must still be cleared after encryption.
- Status-only HTTP fixtures must remain explicit and valid, while production assembly must never omit a capability.
- Constructor validation must reject incomplete adapter pairs without forbidding the evidenced State-only fixture shape.
- No compatibility overload may keep runtime type assertions or concrete application defaults alive.
- No migration exists. If a phase regresses Setup behavior, revert the boundary and all callers together rather than weakening a test or API contract.
