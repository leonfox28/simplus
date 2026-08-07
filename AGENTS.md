<!-- TRELLIS:START -->
# Trellis Instructions

These instructions are for AI assistants working in this project.

This project is managed by Trellis. The working knowledge you need lives under `.trellis/`:

- `.trellis/workflow.md` — development phases, when to create tasks, skill routing
- `.trellis/spec/` — package- and layer-scoped coding guidelines (read before writing code in a given layer)
- `.trellis/workspace/` — per-developer journals and session traces
- `.trellis/tasks/` — active and archived tasks (PRDs, research, jsonl context)

If a Trellis command is available on your platform (e.g. `/trellis:finish-work`, `/trellis:continue`), prefer it over manual steps. Not every platform exposes every command.

If you're using Codex or another agent-capable tool, additional project-scoped helpers may live in:
- `.agents/skills/` — reusable Trellis skills
- `.codex/agents/` — optional custom subagents

Managed by Trellis. Edits outside this block are preserved; edits inside may be overwritten by a future `trellis update`.

<!-- TRELLIS:END -->

## Simplus Safety Boundaries

- Never publish credentials, private endpoints or topology, SIM/device identity, raw HIL evidence, packet captures, screenshots, or private troubleshooting material.
- Explicit user approval is required before real SMS/calls, RF changes, modem-persistent writes, or any HIL-1/HIL-2 action. Relevant fixed, read-only HIL-0 inspection is allowed.
- Keep hardware behind typed capabilities; never expose arbitrary AT/QMI commands or device paths through Web/API boundaries.
- Keep validation and repairs scoped to the requested change; a failing check does not authorize unrelated modifications.
