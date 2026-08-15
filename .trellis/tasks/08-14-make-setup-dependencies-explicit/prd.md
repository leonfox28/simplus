# Make Setup dependencies explicit

## Goal

Resolve layer-audit finding V-03 by making every Setup application dependency explicit, moving concrete filesystem/password/secret/certificate assembly to `cmd/simplusd`, and replacing lower-layer result types at the application seam without changing the active Setup product flow.

## Background

- The layer audit classified V-03 as a confirmed Medium violation. Current anchors are `internal/application/setup/service.go:23-27,58-97,218-248,503-547,586-605,711-717` and production assembly at `cmd/simplusd/main.go:69-77`.
- `setup.New(stateStore, authorization)` currently type-asserts `stateStore` into Administrator, Storage, Management TLS, Hardware Review and Completion roles, then constructs the default password hasher, concrete filesystem preparer, concrete secretbox path/opener and concrete Local CA generator inside the application package.
- `DirectoryPreparer` returns `storage/filesystem.DirectoryIdentity` and `LocalCAGenerator` returns `managementcert.Bundle`, so concrete adapter vocabulary also crosses the application boundary.
- Setup is active through the public OpenAPI/HTTP/Web bootstrap and first-run flow. ADR 0001 retains existing Setup/Auth behavior even though further expansion is not an MVP priority; deleting or redesigning the product flow is not this repair.
- Current production supplies one SQLite `Set` implementing all persistence roles. Tests also use intentionally minimal state-only services for unrelated ready-instance HTTP gates; those reduced capabilities must remain explicit and deterministic rather than inferred from a dynamic store type.

## Requirements

- R1. Remove production imports of `internal/security/managementcert`, `internal/security/password`, `internal/security/secretbox` and `internal/storage/filesystem` from `internal/application/setup`.
- R2. Replace `setup.New(stateStore, authorization)` with an explicit application-owned `Dependencies` input. `StateStore` is mandatory; Authorization, Administrator, Storage, Management TLS, Hardware Review and Completion roles are named fields rather than runtime type assertions.
- R3. Keep reduced test/service capability shapes explicit. A nil optional capability remains unavailable through the existing method-level configuration errors, while incomplete implementation pairs—Administrator without PasswordHasher, Storage without DirectoryPreparer, or SecretProtectorOpener without LocalCAGenerator—are rejected at construction with a stable configuration error. Local-CA adapters must not be accepted without a Management TLS store.
- R4. Define application-owned `DirectoryIdentity` and `LocalCABundle` values containing only the fields Setup consumes. `DirectoryPreparer` and `LocalCAGenerator` use those types; concrete filesystem and certificate bundles must not leak into the application contract.
- R5. Keep `SecretProtector`, password hashing, directory preparation and Local CA generation behind the smallest Setup-owned ports. Preserve all current labels, key clearing, SAN/fingerprint/certificate fields, directory identity checks and durable integer bounds.
- R6. Move concrete production selection to the `cmd/simplusd` composition root, preferably in a focused executable helper: pass the SQLite set explicitly for each persistence role, construct the default Argon2id hasher, translate `storage/filesystem` identity, open `secretbox` at the same fixed instance key path, and translate `managementcert.GenerateLocalCA` output into the application bundle.
- R7. Derive the instance secret-key path once in `cmd/simplusd` and use the same path for Setup protection and the existing notification/instance keyring. Preserve startup failure behavior and do not create a caller-selected path surface.
- R8. Make construction failure explicit to callers and handle it as a startup/configuration failure. Optional deterministic `io.Reader` and clock inputs may be supplied through `Dependencies`; production defaults remain `crypto/rand.Reader` and `time.Now`, with unchanged 10-minute bootstrap and 30-minute session lifetimes.
- R9. Update every constructor caller. Full Setup integration tests receive explicit fakes/adapters; status-only HTTP fixtures receive an explicit StateStore-only dependency set. Setup application tests must no longer mutate private Service fields to replace clock, randomness, password, filesystem, secret or certificate behavior.
- R10. Add constructor validation tests and a `cmd/simplusd` composition test proving the production dependency set is complete. Preserve existing Setup service, control bootstrap and HTTP integration coverage using synthetic/temp-directory inputs only.
- R11. Do not change OpenAPI, generated clients, Setup endpoints, cookies, state machine, SQLite schemas/data, password parameters, secretbox format/path, certificate contents/lifetimes, directory security checks, setup completion behavior or Web UI.
- R12. Do not start services/Compose, inspect private state, contact a network endpoint, access hardware/HIL, send SMS/place calls, or mutate RF/SIM/eUICC/network state during validation.

## Acceptance Criteria

- [x] AC1. `internal/application/setup` imports no concrete security/storage adapter package, performs no dynamic store capability assertion, and exposes only application-owned dependency/result types.
- [x] AC2. `Dependencies` explicitly represents every Service field required by active Setup behavior; mandatory and paired dependency validation returns a stable error and valid reduced capability shapes remain intentional.
- [x] AC3. `cmd/simplusd` is the sole production owner of password/filesystem/secretbox/Local-CA implementation selection and uses the unchanged fixed instance secret-key path for both current consumers.
- [x] AC4. Setup storage identity, HTTPS Local CA field/label/order and format behavior, bootstrap/session timing, hardware review and completion behavior remain compatible at their existing observable boundaries.
- [x] AC5. All production/test constructor callers compile; Setup tests inject deterministic dependencies without assigning private Service fields, and focused tests cover missing/partial dependencies plus full production assembly.
- [x] AC6. No API/generated/storage migration/container/Web or unrelated audit-finding change is introduced.
- [x] AC7. Formatting, targeted race tests, supported full Go tests/vet/lint components, task validation, focused dependency scans and `git diff --check` pass without service, external or hardware side effects.
- [x] AC8. An independent Trellis check confirms V-03 is removed rather than relocated into HTTP, persistence or another application package.

## Out of Scope

- Removing or simplifying the active Setup/OpenAPI/Web flow, Local CA mode, hardware review or legacy Setup tables.
- Changing cryptographic algorithms, parameters, key/certificate formats, secret labels, lifetimes or filesystem security policy.
- Refactoring `httpapi.Server`'s concrete Setup pointer (audit concern C-01) or other application constructors (V-04/V-05).
- Adding a generic dependency container, service locator, adapter registry or shared catch-all setup package.
- Deployment, HIL, real private-data or external-network validation.
