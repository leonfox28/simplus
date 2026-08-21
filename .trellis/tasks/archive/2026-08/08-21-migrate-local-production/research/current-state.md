# Sanitized current-state inventory

## Host

- Debian 13, Linux amd64.
- Rootful Docker Engine is reachable and Docker Compose exceeds the supported minimum.
- Non-interactive sudo is available for the bounded `/opt`, `/var/backups`, archive, and
  module-load operations in the approved plan.
- The fixed option-driver sysfs point satisfies the release preflight.
- ModemManager is inactive. All allowlisted legacy production services are absent/inactive;
  the development Agent is inactive and disabled.

## Running deployment

- Compose project: `simplus`, sourced from the repository development Compose.
- `app`, `agent`, and `netd` are healthy; `data-init` and `bootstrap` exited successfully.
- All three images use the explicit `:dev` tag and share an older source revision than
  the `v0.1.1` release.
- Only the safe OCI labels, container states, Compose labels, and mount metadata were read.
  No container environment, database, bootstrap output, device identity, or raw log was read.

## Persistent state

- Core: `<repo>/data/core` -> `/var/lib/simplus`, UID/GID 10001, mode 0700.
- Agent: `<repo>/data/agent` -> `/var/lib/simplus-agent`, UID/GID 10002, mode 0700.
- The combined state is approximately 51 MiB and no symlink was observed from within the
  respective containers.
- Named runtime volumes contain sockets/temporary state and are not part of the migration
  backup contract.
- Available space on the current filesystem is sufficient for the source data, restored
  production copy, and one compressed migration archive. The archive remains same-disk
  rollback protection, not disaster recovery.

## Selected target

- Deployment root: `/opt/simplus` (currently absent).
- Private migration backup root: `/var/backups/simplus` (currently absent).
- Migration commands do not overwrite/delete the source data. Production starts from a
  verified restore of the stopped archive; a rollback may resume normal source-service writes,
  so every retry requires a fresh stopped archive.
