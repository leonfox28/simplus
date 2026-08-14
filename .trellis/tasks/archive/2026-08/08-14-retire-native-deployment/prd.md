# Retire native production deployment

## Goal

Resolve audit finding V-01 by retiring the unsupported native Debian production deployment and making Docker Compose the repository's only supported production installation path.

## Context

- The owner confirmed that current deployments are container-based and authorized removal of the native production path.
- Read-only host inspection found a legacy native installation. The repository's existing non-purge uninstaller was run successfully before source removal; native units, binaries, host plugins, Web files and sysctl configuration were removed while private state directories were preserved.
- The container Agent already registers reviewed dynamic USB serial IDs through `modemadapter.DefaultRegistry()` and remains the sole production owner of the `option1/new_id` write.
- `packaging/strongswan-plugins/**` is still required to build the netd container and is not part of the obsolete native deployment.

## Requirements

- R1. Remove the native production bundle builder, installer, uninstaller and ML307A-specific bind helper from `scripts/release/`.
- R2. Remove workflow, documentation and planning references that advertise or validate the native Debian bundle as a supported production fallback.
- R3. State that Docker Compose is the only supported production deployment without claiming that pending clean-VM lifecycle evidence has already passed.
- R4. Preserve the three-image container privilege model, registry-backed driver registration, container host preparation/check scripts, strongSwan package build, native Linux development workflow and `simplus-agent-dev` HIL workflow.
- R5. Preserve references where “native” means QDC507 native cellular SMS rather than native host deployment.
- R6. Keep container host checks fail-closed when obsolete native service names are found, so an old installation cannot silently contend with Compose.
- R7. Do not purge legacy private state, inspect it, migrate it into Compose, start services/Compose, or execute hardware/HIL/network/communication actions as part of repository validation.
- R8. Keep canonical docs, accepted decision 0021, active plan, Trellis specs and CI path filters consistent with the container-only production contract.

## Acceptance Criteria

- [x] AC1. The four obsolete native release scripts are deleted and no supported command can build or install the native Debian production bundle.
- [x] AC2. No production path hardcodes the ML307A VID/PID and writes the host `option1/new_id` outside the registry-backed container Agent command.
- [x] AC3. StrongSwan package/container build inputs and the remaining container host scripts are intact and their checks pass.
- [x] AC4. README, architecture, installation, handoff, active plan and ADR/status language consistently describe Compose as the only supported production deployment while retaining current evidence limitations.
- [x] AC5. Historical/native-cellular-SMS references are not incorrectly removed or rewritten.
- [x] AC6. Workflow path filters and canonical documentation checks contain no stale references to deleted scripts.
- [x] AC7. All repository changes are scoped to this retirement and task artifacts; no private data, container runtime, modem, RF, SIM/eUICC, SMS/call or HIL action is used for validation.

## Out of Scope

- Purging or migrating preserved legacy `/var/lib` state.
- Removing native Linux development, Simulator, HIL build tools, `simplus-agent-dev`, QDC507 native cellular SMS, or strongSwan packaging.
- Changing container privileges, mounts, UIDs, protocols, hardware support or public application behavior.
- Claiming new clean-VM, release, Runtime or hardware evidence.
