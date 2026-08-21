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

## Vite Trusted-LAN Development Proxy

`scripts/dev/run-sim.sh` keeps `simplusd` on the loopback
`SIMPLUS_LISTEN_ADDR` and supplies that address as `VITE_API_PROXY_TARGET`.
Only Vite may bind `SIMPLUS_DEV_WEB_HOST=0.0.0.0` for an explicitly requested
trusted-LAN preview. The Web remains same-origin: browser `/api` and SSE traffic
flows through Vite to the loopback API.

The proxy must use `changeOrigin: false`. `simplusd` validates the request Host
as loopback/private trusted-LAN authority, and setup completion derives its
browser-facing `managementUrl` from that validated authority. Rewriting Host to
the API target can redirect a remote browser to its own loopback interface.
Do not replace this contract with unvalidated `X-Forwarded-Host` handling.

Keep `web/src/viteConfig.test.ts` and a real proxy smoke when changing this
wiring. Authentication-context and redirect assertions are specified in
[`web/frontend/state-management.md`](../../web/frontend/state-management.md#scenario-separate-setup-and-administrator-authorization).

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

## Scenario: Versioned container deployment release

### 1. Scope / Trigger

Apply this contract when changing the production Compose template, deployment
bundle contents, GHCR image metadata, container workflow triggers, Release
assets, or production installation/upgrade instructions.

### 2. Signatures

- Script:
  `scripts/release/build-container-release-bundle.sh <vX.Y.Z> <40-lowercase-hex-commit> <non-negative-source-date-epoch> <existing-non-symlink-output-directory>`.
- Make wrapper:
  `make container-release-bundle RELEASE_TAG=<vX.Y.Z> RELEASE_COMMIT=<40-hex> RELEASE_SOURCE_DATE_EPOCH=<epoch> RELEASE_OUTPUT_DIR=<directory>`.
- Source validation:
  `make container-config CONTAINER_IMAGE_TAG=<development-tag>`.
- Production installation has no image-tag input. Its only editable keys are
  `SIMPLUS_HTTP_PORT`, `SIMPLUS_CONTROLLER_PORT`, and `SIMPLUS_DEVICE_GID`.

### 3. Contracts

- The source `compose.yaml` requires an explicit `SIMPLUS_IMAGE_TAG` and is a
  development template. The builder replaces exactly five controlled
  placeholders with one strict version tag.
- `simplus-compose-<tag>-linux-amd64.tar.gz` contains exactly one root
  directory with `.env.example`, `LICENSE`, `README.md`,
  `THIRD_PARTY_NOTICES.md`, `VERSION`, `check-container-host.sh`,
  `compose.yaml`, and `prepare-container-host.sh`. Scripts are `0755`; all
  other files are `0644`; archive owner/group are numeric zero; order, mtime,
  and gzip header are deterministic.
- The checksum asset is
  `simplus-compose-<tag>-linux-amd64.tar.gz.sha256` and names only the archive
  basename.
- Pull requests and `workflow_dispatch` build the bundle and all three
  `linux/amd64` targets with `push: false`. Only an upstream push event whose
  ref name matches `^v[0-9]+\.[0-9]+\.[0-9]+$` may publish.
- Tag publication creates or reuses a published Pre-release, publishes the
  deployment and corresponding-source assets before the image matrix, then
  publishes `control`, `agent`, and `netd`. Same-ref runs are serialized. Each
  matrix item reuses an existing tag only after its commit, platform, and OCI
  labels match; otherwise it stages new content by digest, rechecks that the
  version tag is absent, and promotes the digest. It never moves an existing
  version tag or emits `latest`, `main`, or branch tags.
- Existing same-name Release assets are accepted only when byte-identical.
  Before upload, the source job checks the exact seven-file public asset
  allowlist and requires every entry to be a regular `0644` file. The final
  `simplus-images-<tag>.json` records `version`, the full `commit`,
  `platform: linux/amd64`, and exactly three target/reference/digest entries.
- `contents: write` and `packages: write` remain job-scoped. Image builds keep
  the OCI source/version/revision/license labels, SBOM, provenance, and
  per-target cache scopes.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| tag is not strict `vX.Y.Z`, commit is not 40 lowercase hex, epoch is invalid, or output directory is missing/symlinked | bundle build fails before publishing an archive |
| source Compose has a missing/extra placeholder, unexpected image, `build`, or `latest` | bundle build fails closed |
| Release asset build or corresponding-source validation fails | no Pre-release asset publication and no image push |
| an existing asset has different bytes | fail; never clobber it |
| an existing image tag has different commit/platform/OCI metadata | fail before push; never move it |
| a version tag appears after digest staging | fail before promotion; never overwrite the tag |
| one image build/push fails | Release remains a Pre-release and the digest manifest is not published |
| digest artifact is missing, duplicated, malformed, or not `sha256:<64-lowercase-hex>` | manifest publication fails |
| first GHCR packages are still private | do not claim anonymous installation; owner must make all three public and visibility cannot be reverted to private |
| a code change is needed after tagging | do not move/reuse the tag; publish the next patch version |

### 5. Good / Base / Bad Cases

- Good: a strict tag first publishes deterministic install/source assets,
  then three `linux/amd64` images, then a complete digest manifest; an
  unauthenticated client can inspect and pull all three after owner visibility
  approval.
- Base: a PR or manual run renders the source and release Compose files and
  builds each target without registry login or publication.
- Bad: let production users set `SIMPLUS_IMAGE_TAG`, fall back to local builds,
  publish a rolling tag, overwrite a differing asset, mark the candidate
  stable, or treat an image rollback as a database-safe downgrade.

### 6. Tests Required

- `internal/containercontract/release_bundle_test.go` asserts strict metadata,
  two-build byte identity, exact allowlist/modes/owner/mtime, checksum,
  licenses, three literal image references, and absence of development/private
  inputs.
- Container workflow contracts assert the upstream/strict-tag gate,
  PR/manual `push: false`, source-before-image dependency, amd64, OCI labels,
  SBOM/provenance, and immutable digest manifest shape.
- Run `make check-container-files`, `go test ./internal/containercontract`,
  `make container-config CONTAINER_IMAGE_TAG=dev`, `make check-docs`,
  `make lint`, `make test`, `make security`, and `git diff --check` before the
  PR is merged.
- Before tagging, run a redacting secret scan over the worktree, full Git
  history, and unpacked final assets, then review the exact asset/license list.
- After publication, verify checksum, OCI metadata, manifest digests, anonymous
  inspect/pull, and extracted `docker compose config --quiet`/`pull`. These
  checks do not authorize host preparation, `compose up`, or HIL.

### 7. Wrong vs Correct

Wrong: make a source checkout or floating image input part of production:

```bash
git clone https://github.com/leonfox28/simplus
SIMPLUS_IMAGE_TAG=latest docker compose up -d
```

Correct: verify a versioned bundle, retain its literal image references, and
pull before any separately authorized start:

```bash
sha256sum -c simplus-compose-v0.1.0-linux-amd64.tar.gz.sha256
tar -xzf simplus-compose-v0.1.0-linux-amd64.tar.gz
docker compose -f simplus-compose-v0.1.0-linux-amd64/compose.yaml pull
```

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
