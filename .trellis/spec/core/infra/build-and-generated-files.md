# Build and Generated Files

## Native Toolchain Contract

The repository develops and tests directly in a Linux checkout. Versions are
owned by `.go-version`, `.node-version`, the root `package.json` `packageManager`
field, and `scripts/dev/toolchain-checksums.txt`. `scripts/dev/setup-toolchain.sh`
derives those versions and installs the verified tools into ignored `.tools/`;
`Makefile` prefers that local Go toolchain and keeps caches under `.tools/`.

Use the existing entry points:

```bash
make dev-toolchain
make bootstrap-dev
make doctor
```

`make doctor` compares actual Go, Node, and pnpm versions with the repository
locks. Do not update only one of a version file, checksum list, root package
metadata, Docker build image, CI setup, or documentation. Search all of those
surfaces first and update the reproducibility evidence together.

Daily Go/Web work does not run inside a development image. `docs/development.md`
and `docs/decisions/0021-container-production-deployment.md` explicitly keep
native builds, Simulator, tests, and frontend hot reload separate from
production image construction.

## Make Target Semantics

`Makefile` is the executable command catalog:

- `verify-modules` checks `go mod tidy -diff` and module hashes without
  changing dependency intent.
- `check-format` reports Go files that need `gofmt`; `format` is the mutating
  formatter.
- `lint` runs `go vet` for root Go packages and locked `actionlint` for GitHub
  workflows.
- `test` runs Go, Web Vitest/typecheck, worktree-manifest, and Simulator
  supervisor checks.
- `security` runs locked `govulncheck` and pnpm audit; root `pnpm-workspace.yaml`
  documents reviewed audit exceptions.
- `build`, `build-go`, and `build-linux` create ignored binaries under
  `.dev/bin`; version/commit linker values are validated before use.
- `check-docs` and `check-container-files` are fast mechanical boundary checks.

Prefer these targets over reconstructing tool invocations in a new script.
When a new repeatable operation is needed, add one clear Make target and reuse
it from CI/docs instead of creating divergent command sequences.

## Generated Sources and Outputs

The `generate` target has three owners:

| Source | Generator definition | Output |
| --- | --- | --- |
| `api/openapi.yaml` | `internal/api/openapi/generate.go` + `api/oapi-codegen.yaml` | `internal/api/openapi/generated.go` |
| `internal/storage/sqlite/migrations/core/**` + `internal/storage/sqlite/queries/core/**` | `sqlc.yaml` | `internal/storage/sqlite/generated/core/*.go` |
| `api/openapi.yaml` | `web/openapi-ts.config.ts` via root `api:generate` -> Web `generate:api` | `web/src/api/generated/` Fetch SDK, TypeScript, Zod, and TanStack Query output |

`Makefile` `GENERATED_PATHS` is the drift-check registry. `verify-generated`
copies all declared outputs, runs generation, byte-compares the results, and
compares a whole-worktree content manifest. A new generated output must be
added to that registry and tested; a generated header is not permission to
patch the result.

Do not run generation casually in a dirty tree without first understanding
the source change. If verification reports drift, fix the source/generator or
commit the intentional regenerated result; do not conceal drift with a second
formatter or local post-processing script.

## Third-Party and Packaging Inputs

Third-party assets carry their own provenance and license boundaries:

- `third_party/mihomo/` records the official binary version, source metadata,
  compressed/expanded digests, exact source commit, corresponding-source
  archive, and GPL license in its `VERSION`, `SOURCE`, and `README.md` files.
  `Dockerfile` verifies these values during the `mihomo-fetch` stage.
- `third_party/zashboard/` records the fixed official release asset in its
  `VERSION`, `SOURCE`, `README.md`, and `LICENSE` files. The checked-in
  `third_party/zashboard/dist/` tree is vendor output, not an application
  source tree.
- `components/strongswan-simplus-simaka/` is a separately licensed in-repo C
  component. `packaging/strongswan-plugins/debian-13-amd64.lock` pins source
  and runtime-ABI inputs; `packaging/strongswan-plugins/build-deb.sh` builds in
  temporary source/sysroot directories and emits the `.deb`, source archive,
  checksums, and manifest.
- `THIRD_PARTY_NOTICES.md` and `docs/decisions/0020-strongswan-plugin-package.md`
  are part of the release contract, not optional release notes.

Never replace a locked download with an unverified URL, use the host's
installed libraries as implicit packaging input, edit a vendored minified
asset to make a product change, or publish a binary without its required
license/source material.

`make build-strongswan-plugins-deb` and
`make test-strongswan-plugins-package` require Debian 13/amd64 semantics and
may download verified inputs into ignored `.dev/cache`. Ordinary Go/Web tests
do not require those sources or packages.

## CI and Release Alignment

`.github/workflows/ci.yml` calls the same Make checks used locally.
It also installs the pinned Playwright Chromium and runs `make web-e2e`; those
fixtures are synthetic and must remain independent of private endpoints,
hardware, RF, SMS, calls, and HIL.
`.github/workflows/strongswan-plugins.yml` owns package evidence, and
`.github/workflows/containers.yml` owns Compose contract, image, and tagged
corresponding-source publication. When changing a path filter, release asset,
or Make target, verify the relevant workflow still runs for every source that
can affect its output.

## Avoid

- Floating tool/image tags or `latest` in a reproducible path.
- Committing `.tools/`, `.dev/`, build caches, databases, package output, or
  runtime data.
- Duplicating generation commands in docs/workflows without updating
  `Makefile`.
- Treating a successful compile as generated-drift, license, package ABI, or
  release-source verification.
- Running `clean` or another deletion target as validation in a dirty worktree.

## Verification Matrix

```bash
make doctor
make verify-modules
make check-format
make verify-generated
make check-container-files
make lint
make test
```

Add `make security`, `make build`, package tests, or container builds only when
the affected risk surface calls for them. Report skipped environment-dependent
checks and the exact next command.
