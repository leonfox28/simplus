# Repository Guide

Start with `docs/README.md`. It is the map for the repository knowledge base.

- Product scope: `docs/product.md`
- Architecture and invariants: `docs/architecture.md`
- Current execution plan: `docs/plans/active/mvp.md`
- Current implementation handoff: `docs/handoff.zh-CN.md`
- Local development and HIL: `docs/development.md`
- Public/private documentation boundary: `docs/privacy-and-publication.md`

Keep documentation progressively disclosed: update the canonical document instead of copying the same rules into multiple files. New product scope requires a decision record. Complex work should update an active execution plan; small changes do not need one.

Documentation changes should run `make check-docs`.

The product is a trusted-LAN, single-administrator tool for controlling 4G/5G modems for SMS and calls. Host VoWiFi, a narrowly scoped Mihomo egress for VoWiFi traffic, and removable-eUICC installed-profile management are required phased capabilities. Do not expand them into a general proxy/data gateway or full eSIM RSP platform without an explicit user decision. Connectors, enterprise PKI/audit/update infrastructure, and previously rejected scope remain excluded.

Never commit raw HIL logs, subscriptions, proxy endpoints or credentials, real LAN topology, personal host paths, SIM/device identities, packet captures, screenshots, or private troubleshooting timelines. Public compatibility conclusions belong in `docs/compatibility.md`; public operational guidance belongs in `docs/troubleshooting.md`.

Real SMS, calls, RF changes, modem-persistent writes, and HIL-1/2 actions require explicit user approval. Read-only HIL-0 inspection is allowed when relevant. Never expose arbitrary AT/QMI commands or device paths through Web/API boundaries.

## Testing and Validation

After code changes, choose validation by its ability to prove the requested behavior and by the risk of the change. Consider checks in this order, but run only those that are materially relevant:

1. Targeted unit test or minimal reproduction for changed behavior.
2. Related integration test.
3. Typecheck, lint, or format check.
4. Build check for affected packages.
5. Minimal smoke test or browser/manual verification.

- Prefer a targeted check expected to finish within about 60 seconds first. If broader checks are expensive or skipped, explain why and name the next best check.
- Treat broader or full-suite validation as optional follow-up rather than the default completion condition. Consider it for shared contracts, public APIs, core common modules, database or permission changes, cross-module refactors, release preparation, or when the user explicitly requests it.
- A failing check does not automatically authorize a repair. Before editing code or tests, establish that the failure was caused by the current change and falls within the user's requested scope.
- Fix failures attributable to the current change. Report pre-existing, unrelated, environmental, or plausibly flaky failures without expanding the task to repair them.
- Do not rerun the same failing check unless code, configuration, or environment changed in a way expected to affect its result. Every repair iteration requires new root-cause evidence; if the same failure repeats without progress or repairs begin producing unrelated failures, stop and report the verified blocker.
- Do not modify tests merely to make them pass unless the requested behavior changed or evidence establishes that the test itself is incorrect.
- Completion requires the requested outcome and the important applicable validation to succeed; it does not require every repository check to be green.
- If validation cannot run, say why; never replace observed evidence with confidence.
- For Chrome MCP smoke tests, starting the server is not evidence. Make a real MCP tool call such as `new_page`, and keep the window open when the user needs to inspect it.
- Before finalizing, inspect the diff for symptom patches, duplication, hidden fallbacks, broad error swallowing, second sources of truth, dead code, unmentioned behavior changes, weak tests, security regressions, and unrelated edits. Fix clear in-scope issues before responding.
