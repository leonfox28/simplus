# Design: container-only production retirement

## 1. Retirement boundary

The repair removes an obsolete deployment implementation rather than adapting it to the current registry. The deleted production entry points are:

- `scripts/release/bind-ml307a.sh`
- `scripts/release/build-debian-bundle.sh`
- `scripts/release/install-debian.sh`
- `scripts/release/uninstall-debian.sh`

The remaining `scripts/release/prepare-container-host.sh` and `check-container-host.sh` belong to Compose. The check keeps rejecting legacy service names because contention is still unsafe even after the repository stops supplying those units.

## 2. Runtime and build boundaries preserved

Container startup remains:

```text
containers/agent-entrypoint.sh
  -> simplus-agent register-option-driver
  -> modemadapter.DefaultRegistry().USBSerialIDs()
  -> fixed mapped option1/new_id attribute
```

No host-path selector or caller-provided VID/PID is added. `packaging/strongswan-plugins`, its product C bridge, Docker build stages and release artifacts remain because netd images consume them. Native Go/Node development and `simplus-agent-dev` are development/HIL workflows, not production deployment fallbacks.

## 3. Documentation ownership

- `README.md` and `docs/README.md` are entry points and link to the canonical installation guide.
- `docs/installation.md` becomes the container-only operational guide. It continues to state current clean-VM/evidence limitations and that legacy state is not automatically migrated.
- `docs/architecture.md` owns the production process shape and removes the obsolete native systemd fallback.
- `docs/handoff.zh-CN.md` and `docs/plans/active/mvp.md` align current status and remaining milestones.
- ADR 0021 already decided that Compose becomes the sole production deployment after container HIL; add an implementation-status note instead of rewriting its historical rationale.
- Trellis documentation/infra specs are synchronized in Phase 3.3 so future sessions do not revive the fallback.

“Native” references are changed only when they describe host/systemd deployment. QDC507 native cellular SMS and native Linux development remain valid, distinct concepts.

## 4. Compatibility and safety

Removal intentionally breaks the ability to create a new native bundle. It does not alter Compose configuration or application APIs. Existing legacy private state is neither read nor deleted; migration remains unsupported. No runtime validation starts Compose, touches hardware or creates network objects.

Rollback is source-level restoration of deleted scripts and documentation only. The already completed host uninstall is not automatically rolled back and private state remains available for an explicitly authorized recovery decision.

## 5. Verification strategy

- scan tracked source for deleted script names and native-fallback claims;
- scan production driver binding for hardcoded/out-of-registry writes;
- run shell syntax checks for remaining release/container scripts;
- run documentation and container contract checks;
- validate CI YAML through the existing lint path when the locked tool is available, reporting an offline cache limitation rather than downloading dependencies;
- run `git diff --check` and task context validation.
