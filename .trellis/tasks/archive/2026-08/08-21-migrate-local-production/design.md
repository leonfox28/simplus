# Design: local production cutover with preserved state

## Boundaries

This is an operational cutover, not a product-code or schema change. The verified
`v0.1.1` Release bundle remains immutable. Its files are installed under `/opt/simplus`,
and its relative `./data` mounts resolve to `/opt/simplus/data`. The repository Compose and
`.env` are never rewritten; migration commands never overwrite/delete `<repo>/data`, while a
rollback may resume its ordinary service writes before a fresh retry snapshot.

The target retains the fixed `simplus` Compose project name and the existing three-image
privilege model. Old and new Compose definitions must never be running concurrently.

## Storage layout

```text
<repo>/data/                    preserved source state / immediate rollback source
/var/backups/simplus/<id>/      root-only archive, checksum, and sanitized cutover metadata
/opt/simplus/                   verified v0.1.1 deployment bundle
/opt/simplus/data/              restore of the stopped pre-cutover archive
```

The backup directory is mode 0700 and the archive/checksum are mode 0600. The deployment
root and public bundle files are root-owned and readable; `.env` contains only the three
non-secret production parameters. `data-init` preserves/fixes the defined UID/mode contract
for the restored `core` and `agent` directories.

## Cutover data flow

1. While the current deployment remains healthy, download the `v0.1.1` deployment archive,
   checksum, and image digest manifest into a temporary directory. Verify checksum, strict
   asset allowlist, VERSION, literal image tags, absence of `build`/`latest`, Compose render,
   anonymous pulls, and pulled digests.
2. Install the verified managed files under `/opt/simplus`, create the exact three-key
   `.env`, run the bounded host preparation, and run the host check. No current container is
   interrupted unless all pre-cutover gates pass.
3. Enter the no-write acceptance window and stop the source Compose with `down` and no
   volume flag. Confirm no old business container remains running.
4. As root, archive the whole `<repo>/data` with numeric ownership, ACL/xattr, sparse-file,
   and single-filesystem semantics. Store and verify SHA-256, test the archive, restore that
   same archive to `/opt/simplus/data`, then compare both source and restored trees against
   the archive without emitting filenames or contents.
5. Re-run host/config checks and start only `/opt/simplus/compose.yaml`. Let the standard
   typed startup register reviewed USB IDs, create bounded runtime network objects, and
   restore existing service intent. Do not trigger any manual business or hardware action.
6. Verify service health, one-shot exit codes, image tags/digests, Compose labels, and exact
   bind-mount sources using metadata-only Docker inspection. Never print bootstrap output or
   raw service logs.

## Compatibility and side effects

`v0.1.1` is still a Pre-release deployment candidate. Starting it may perform forward-only
database migration on `/opt/simplus/data`; it cannot write the preserved source copy.
Normal Compose startup may briefly interrupt the existing modem/network service and restore
previously saved activation intent. Host preparation idempotently writes only the fixed
module-load configuration and loads `option`; it does not write an arbitrary USB ID, RF
setting, SIM state, message, or call.

The production HTTP/controller ports continue listening on the same addresses and ports as
before. Firewall or exposure changes are outside this task.

## Failure and rollback

Every staging/check failure before old Compose shutdown is fail-closed and leaves the old
deployment running. During cutover, any archive/restore comparison failure stops before new
Compose startup.

If target startup or metadata-only acceptance fails before user traffic resumes:

1. stop `/opt/simplus` Compose without `-v`;
2. keep the failed production copy and private archive for diagnosis;
3. start the repository Compose with its unchanged `.env` and preserved `<repo>/data`;
4. verify the three old services return healthy using metadata-only checks.

Once the target has accepted new business writes, rollback to the pre-cutover source would
discard those writes. At that point the workflow stops and requires a separate data decision;
it never silently changes image tags or restores an older database.

The original data and same-disk archive are intentionally not deleted after success. Their
later retention/cleanup is a separate destructive action requiring explicit authorization.

## Privacy

Commands may record public image digests, versions, exit states, owners/modes, byte counts,
and mount destinations. They must not emit archive listings, filenames within private state,
database values, credentials, device/SIM identity, endpoints/topology, raw logs, or HIL
transcripts. Failure inspection is limited to stable container state and already-redacted
health information; otherwise roll back and report the stage.
