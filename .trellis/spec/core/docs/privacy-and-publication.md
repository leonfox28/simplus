# Privacy and Publication

## Public and Private Records

`docs/privacy-and-publication.md` permits public product/architecture/decision
content, general development/install/recovery instructions, synthetic fixtures,
Simulator data, automated tests, dependency provenance/licenses, and sanitized
compatibility conclusions.

Keep these outside the public repository and release artifacts:

- credentials, subscription URLs/YAML, proxy node endpoints, controller
  secrets, cookies, administrator passwords, private keys, and authentication
  material;
- real LAN/VPN topology, host addresses/configuration, usernames, personal
  absolute paths, and private endpoints;
- SIM/eUICC/IMS/device identities, serials, phone numbers, AKA material,
  complete SIP headers, and other linkable telecom identifiers;
- raw HIL or per-node results, command/service logs, packet captures,
  databases, screenshots, recordings, and private troubleshooting timelines;
- abandoned private planning and environment recovery notes.

This applies to code comments, tests, fixtures, commit messages, PR/issues,
Actions output, release assets, copied terminal output, Trellis task material,
and chat-derived documentation—not only files under `docs/`.

## Synthetic Fixtures and Logging

Use clearly synthetic bounded values in tests. Do not paste a real identifier
and call it a fixture after masking a few digits. Current tests construct fake
instance IDs/fingerprints and temporary paths, while production code converts
sensitive identities to instance-scoped fingerprints/hints before ordinary
business use (`internal/hardwareprobe/scanner_test.go`,
`internal/application/line/service_test.go`, and
`docs/decisions/0017-managed-modems-and-capability-adapters.md`).

Public/stable errors contain a code and safe retry semantics, not raw payloads.
`internal/api/httpapi/server.go` maps service errors to `openapi.ApiError`, and
`docs/troubleshooting.md` documents safe stages/codes. Logging should retain
the minimum request/stage identifiers needed to diagnose behavior and must not
add credentials, bodies, SIM/IMS/device identity, full SIP/RP/PDU, addresses,
nodes, or hardware paths.

Dedicated sensitive reads must remain explicit and ephemeral. The managed
modem equipment-identity POST is authenticated, fenced to current hardware,
returns `no-store`, and does not put the raw value in the database or ordinary
list response (`docs/architecture.md` and
`internal/application/modem/agent_identity.go`). Do not turn it into a cached
list field or log value.

## From Private Evidence to Public Conclusion

Publish only the minimum conclusion:

1. the capability that was implemented;
2. its evidence level from `docs/compatibility.md`;
3. the important unverified boundaries;
4. the automated fixture/test or stable error that guards regression.

Remove dates/times tied to a private run, addresses, topology, nodes,
subscriptions, traffic counts, personal paths, identities, phone numbers,
raw commands, and transcripts. If a detail does not help a public reader
understand architecture, compatibility, or recovery, omit it instead of trying
to redact it.

Raw evidence belongs in the external private record system. Even there, live
credentials and authentication material require appropriate encrypted storage;
a private Git repository alone is not a secret manager.

## Runtime, Build, and Release Artifacts

`.dev/`, `.tools/`, Compose `./data`, SQLite/WAL files, logs, recordings,
captures, configuration, and local reference material are private runtime
artifacts. `internal/containercontract/contract_test.go` protects
`.dockerignore` exclusions for `.git`, `.dev`, `.tools`, `.env`, `/data`,
local references, private docs, and packet captures. Do not widen the Docker
context or add a debug `COPY` that reintroduces them.

Public release assets may include the fixed application images, the
allowlisted versioned Compose deployment archive and checksum, its three-image
digest manifest, the strongSwan-plugin package plus corresponding
source/manifest, and required Mihomo corresponding source when the locked
release workflow produces them. The deployment archive contains only the
literal-tag Compose file, three-key `.env.example`, reviewed host scripts,
README, version metadata, root license, and third-party notices; it never
contains `.env`, source, Git metadata, runtime data, logs, or private evidence.
The allowed set and license/provenance are defined by `Dockerfile`,
`third_party/**`, `packaging/container/**`,
`packaging/strongswan-plugins/**`, `THIRD_PARTY_NOTICES.md`, and
`.github/workflows/containers.yml`. Container data, site config, logs, or
credentials are never release assets.

## Publication Procedure

For an actual public publication, follow the full procedure in
`docs/privacy-and-publication.md`:

- move necessary raw/private records to the external private system and remove
  their public references;
- run `make check-docs` and a redacting secret scan over the intended tree;
- create a sanitized new Git history rather than making an existing private
  history public;
- scan/review the new history and inspect workflows, issues, and release
  assets manually;
- rotate any live credential that entered chat, logs, a worktree, or old
  history;
- obtain repository-owner review of `LICENSE`,
  `THIRD_PARTY_NOTICES.md`, and the final file/asset list before changing a
  public remote.

Scanner output and review reports can themselves contain detected secrets;
keep them redacted and out of Git.

## Avoid

- Assuming `.gitignore` removes tracked data or old history.
- Treating partial masking as sufficient when the detail need not be public.
- Posting raw logs/screenshots to explain a compatibility claim or support
  issue.
- Publishing a generated database, Compose data directory, diagnostic bundle,
  or release build context for convenience.
- Calling the root PolyForm license "open source" or erasing the separate
  licenses of the in-repo strongSwan component and third-party assets
  (`docs/decisions/0013-polyform-noncommercial-license.md`).
- Letting a successful HIL authorize publication of its raw evidence; approval
  to execute an action and approval to disclose its artifacts are different
  boundaries.
