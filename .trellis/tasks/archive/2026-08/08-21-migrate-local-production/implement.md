# Implementation plan

## 1. Pre-cutover inventory and gates (current service stays running)

- [x] Resolve explicit source, target (`/opt/simplus`), backup, and temporary paths; reject
      pre-existing unexpected target content and confirm sufficient space.
- [x] Record only current container names, service states, safe OCI revision labels, exact
      bind-mount metadata, and data-root owner/mode/size. Do not inspect environment, data,
      device identity, or logs.
- [x] Download the `v0.1.1` Compose archive, SHA-256 file, and image digest manifest from the
      matching GitHub Release into `mktemp -d`; verify checksum before extraction.
- [x] Verify the extracted top-level directory and exact eight-file allowlist, expected modes,
      `VERSION`, literal three-image `v0.1.1` references, and absence of `build`, `latest`, or
      image-tag substitution.
- [x] Install bundle files under `/opt/simplus` with explicit root ownership/modes; create a
      three-key `.env` with ports 8080/19090 and device GID 20. Do not copy source Compose.
- [x] Run `sudo bash /opt/simplus/prepare-container-host.sh`, then the read-only host check and
      `docker compose config --quiet` from `/opt/simplus`.
- [x] Pull all images anonymously, compare the resolved platform/digests and OCI
      source/version/revision/license labels with the Release manifest, and stop before
      downtime on any mismatch.

### Pre-cutover result (2026-08-21)

- All seven gates passed while the existing `:dev` deployment remained running and healthy;
  no source container or persistent-state directory was stopped, restarted, or modified.
- The verified Pre-release is `v0.1.1`, commit
  `81d3f3ac3a7868ea39e90bb3533b599fd2939ccd`, platform `linux/amd64`.
- `/opt/simplus` contains the eight root-owned Release bundle files plus the exact three-key
  root-owned `.env`; `/opt/simplus/data` remains absent until the quiesced restore stage.
- Host preparation installed `/etc/modules-load.d/simplus.conf` as root-owned mode `0644`,
  loaded `option`, and did not write a USB ID. The host check and Compose render passed.
- Anonymous pulls used an empty Docker credential directory. Resolved public image digests:
  - control: `sha256:f93f88dd228f610d8e4c9e4e9470b8a42248a1ec6b1d887365092169a7a5d57d`
  - agent: `sha256:9640e6c5c59590dde07cdf44242f43d8e3bd6f61641abe9716d5767445d28716`
  - netd: `sha256:c6423e1cf21b84ad1864fead9e4bbdac556689286575460d3614e962508f0f39`

## 2. Quiesced snapshot and restore (downtime begins)

- [x] Announce the cutover stage; do not allow user/business writes until acceptance ends.
- [x] From the repository project, run ordinary `docker compose down` without `-v`; verify old
      business containers are no longer running and original bind data still exists.
- [x] Create a unique root-only `/var/backups/simplus/<cutover-id>` and archive the complete
      source `data/` with numeric owner, ACL/xattr, sparse and one-filesystem preservation.
- [x] Write and verify SHA-256, test archive readability without listing private paths, and
      compare the stopped source tree to the archive.
- [x] Restore the same archive beneath `/opt/simplus`, verify owner/mode and absence of
      symlinks, then compare the restored tree to the archive without printing private paths.
- [x] Re-run `/opt/simplus/check-container-host.sh /opt/simplus` and
      `docker compose config --quiet`; do not start on any mismatch.

### Quiesced snapshot result (2026-08-21)

- The source Compose was stopped with ordinary `down`; named volumes and the original bind
  data were retained. No production container was started in this stage.
- A root-only cutover directory contains the complete compressed archive and its verified
  SHA-256 file, both root-owned mode `0600`. Archive readability and the stopped
  source/archive comparison passed without emitting private paths.
- `/opt/simplus/data` was restored from that same archive. The restored data root is
  root-owned mode `0755`; `core` is `10001:10001` mode `0700` and `agent` is
  `10002:10002` mode `0700`. No symlink was found and the restored/archive comparison passed.
- The post-restore host check and Compose render passed. The old business services remain
  stopped for the production-start acceptance stage.
- An initial checksum-verification command could not enter the intentionally root-only backup
  directory as the unprivileged caller. The rollback gate restored all three old services to
  healthy before a new unique stopped snapshot was taken; the unused attempt remains retained.
- Two later target attempts passed every production health/image/mount gate but were rolled
  back by extra, unreviewed diagnostics. Each rollback restored the source services healthy;
  each failed target data tree was preserved in a unique root-only quarantine. The final
  cutover therefore took a fresh stopped snapshot after proving zero source-config container
  mounts and no open writer against the stopped source data.

## 3. Production start and metadata-only acceptance

- [x] Run `/opt/simplus` `docker compose up -d`; poll in intervals shorter than 60 seconds and
      require `agent`, `netd`, and `app` healthy plus `data-init`/`bootstrap` exit 0.
- [x] Inspect only safe Docker metadata to assert all running image tags/digests match the
      Release manifest and exact core/agent mount sources resolve under `/opt/simplus/data`.
- [x] Confirm the source Compose has no running containers, the production Compose renders
      again, the original source data still has its pre-cutover metadata, and no unexpected
      new admin/bootstrap output was read.
- [x] Leave `/opt/simplus`, the preserved source data, backup archive/checksum, old images, and
      runtime volumes in place. Remove only the temporary public Release download directory.

### Production acceptance result (2026-08-21)

- The authoritative final run accepted `app`, `agent`, and `netd` as running/healthy and
  `data-init`/`bootstrap` as exited with status 0; no bootstrap or service log was read.
- All five containers identify `/opt/simplus/compose.yaml` and `/opt/simplus` through their
  Compose labels. No source-config container remains.
- The literal `v0.1.1` tags, three public Release digests, and required OCI
  source/version/revision/license labels match. The five persistent bind checks resolve only
  to `/opt/simplus/data`, `/opt/simplus/data/core`, or `/opt/simplus/data/agent` as designed.
- Source `core`/`agent` roots retain their fixed owner/mode and are not mounted into the
  production containers. The final archive/checksum, original source state, every earlier
  migration attempt, failed target quarantine, old images, and runtime volumes remain.
- The temporary public Release download and the synthetic tar-semantics probe were removed;
  no private data, archive, log, credential, identity, or topology was written to Git.

## 4. Failure rollback gate

- [x] Before production startup: keep old Compose running, or if already quiesced restart it
      from the repository using preserved data; do not use the partially restored target.
- [x] After failed target startup but before user writes: run target `docker compose down`
      without `-v`, restart source Compose, and verify its three business services healthy.
- [x] If any new business write may have occurred, stop and request a separate data recovery
      decision instead of silently returning to the pre-cutover database.
- [x] Never broaden hardware inspection, print raw logs, modify RF/SIM state, or delete data to
      repair a failed gate.

### Rollback result (2026-08-21)

- The source-restart branch was exercised after one checksum invocation error and two
  validation-harness errors. Each time it stopped the target without `-v` and restored all
  three source business services healthy before a fresh snapshot was attempted.
- No known user write occurred in an acceptance window. Every retry nonetheless used a new
  unique stopped snapshot; no earlier archive was reused as current state.
- The two healthy-target rollbacks were caused by checks absent from the approved gate list,
  not by an image, health, digest, OCI-label, Compose-label, mount, or data-restore mismatch.
  The reusable prevention contract is recorded in the infra spec and validation-loop research.

## 5. Completion and repository hygiene

- [x] Record only sanitized versions, public digests, states, owner/mode summaries, and whether
      each acceptance gate passed in the task notes/journal.
- [x] Run `git diff --check` and a scoped secret/private-data review of task artifacts.
- [x] Use `trellis-check` for final spec/plan/result consistency; update infra spec only if an
      actual reusable contract changed. The live-cutover acceptance contract did change after
      the repeated rollback-loop analysis.
- [x] Commit only Trellis task/journal records, then archive the task. Production data,
      archives, Release downloads, logs, and host evidence remain outside Git.
