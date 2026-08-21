# Bug Analysis: unreviewed acceptance checks caused healthy-target rollbacks

### 1. Root Cause Category

- **Category**: E - Implicit Assumption.
- **Specific Cause**: The execution harness assumed host `pgrep` distinguishes
  native processes from container processes, even though rootful Docker exposes
  container PIDs in the host PID namespace. It also let checks added during
  execution share the authoritative `set -e` rollback trap.

### 2. Why Fixes Failed

1. The first target passed health, image, OCI-label, Compose-label, and mount
   gates, but an extra post-start source/archive comparison was added. Its
   failure triggered rollback and erased the quiesced state needed to classify
   the historical member difference without exposing private filenames.
2. The retry added a pre-start no-writer proof, then appended a post-start
   `pgrep -x` assertion not present in the approved plan. It saw the healthy
   container processes as host-native and rolled the target back again.

Both were validation-scope/mental-model failures, not production image, mount,
health, or persistent-data failures.

### 3. Prevention Mechanisms

| Priority | Mechanism | Specific Action | Status |
| --- | --- | --- | --- |
| P0 | Runtime | Limit automatic rollback to the frozen reviewed gate list | DONE |
| P0 | Architecture | Identify post-start ownership through Docker Compose labels/mounts, never host PID names | DONE |
| P1 | Rollback | After source restart, quarantine target and create a fresh stopped snapshot before retry | DONE |
| P1 | Documentation | Add the live-cutover acceptance scenario to the infra spec | DONE |

### 4. Systematic Expansion

- **Similar Issues**: `ps`, `pgrep`, and `/proc` checks on a Docker host all see
  container processes unless they also classify cgroups/Compose metadata.
- **Design Improvement**: Separate authoritative acceptance functions from
  diagnostics; only the former may invoke rollback.
- **Process Improvement**: Never add a new rollback gate after planning without
  reviewing its semantics and side effects.

### 5. Knowledge Capture

- [x] Updated `.trellis/spec/core/infra/containers-and-privileges.md` with the
  executable cutover/retry contract and wrong/correct command shape.
- [x] No matching project-specific spec template exists under
  `src/templates/markdown/spec/`; there is no generated mirror to sync.
- [x] Commit the spec and task record with the completed deployment result.
