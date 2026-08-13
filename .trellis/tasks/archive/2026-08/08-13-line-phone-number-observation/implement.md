# Implementation Plan

## Phase A — Typed cellular observation

- [x] Add the model-neutral `SubscriberNumberAdapter` seam and QDC507 fixed `AT+CNUM` parser with strict synthetic fixtures.
- [x] Attach the best-effort result only to a ready, identity-known `SIMObservation`; add Agent protocol validation and round-trip tests.
- [x] Propagate the independent cellular observation through Agent `SIMObservation`, hardware `SubscriptionProfile`, inventory mapping, clone/sort/validation and topology digest without coupling it into stable SIM identity reads.
- [x] Add absent/locked/swapped/identity-failure regressions proving no stale number survives and no probe readiness regression occurs.

## Phase B — Line-owned source merge

- [x] Add Line domain observation/source types and a consumer-owned optional `PhoneNumberSource` port.
- [x] Carry the current cellular observation through resolved managed Lines without persisting it.
- [x] Implement a narrow supervisor-backed IMS source keyed by stable Line ID; inject it for display views only, without introducing a Line↔VoWiFi dependency cycle or changing business `Topology` availability.
- [x] Implement deterministic empty/single/same/different merge behavior and source ordering; make IMS source failure best effort.

## Phase C — OpenAPI and Web

- [x] Add `PhoneNumberObservation[]` to `ManagedLine`, remove `phoneNumber` from public `VoWiFiLineState`, then regenerate Go/TypeScript/Zod outputs.
- [x] Map Line domain observations in HTTP handlers and remove VoWiFi phone mapping.
- [x] Refactor the Lines page to render only `ManagedLine.phoneNumbers`, including source tags, same-number deduplication and two different-number rows.
- [x] Update focused HTTP, desktop/mobile Web tests and E2E fixtures; use synthetic values only.

## Phase D — Docs/spec and verification

- [x] Correct architecture/product wording so Line owns phone observations and cellular/IMS are sources; add the executable boundary to the relevant Trellis spec.
- [x] Run focused and race Go tests for modemadapter, hardwareprobe, agentapi, hardware/domain, inventory, Line, VoWiFi source and HTTP.
- [x] Run OpenAPI generation/verification, focused Web tests/typecheck/build, format/docs/container checks, `make lint`, `make test`, desktop/mobile E2E and security.
- [x] Run source/privacy scans proving no real phone number, raw CNUM transcript, arbitrary AT surface, model branch above adapter or new phone-number persistence.
- [x] Dispatch independent Trellis check, fix scoped findings, and rerun affected plus final broad gates.

## Phase E — Commit and local deployment

- [x] Present one batched commit plan and commit only after user confirmation; do not push.
- [x] Archive/journal the task after the work commit.
- [x] Build `dev` control/agent/netd images with the work revision, validate Compose, update in place without deleting data.
- [x] Verify bootstrap exit 0, `agent/netd/app` healthy, HTTP health ok, image revision, clean Git state and no active task.
- [x] Privately confirm the running Line API/UI receives the real QDC cellular observation without persisting or logging its value.

## Hard stops

- Any need for `CPBS/CPBW`, phonebook preference, RF/SIM/SMS/data mutation, arbitrary AT/API surface, private-value persistence, unexpected competing device owner, unrelated dirty work or failed broad gate stops the relevant phase.
- No product code or HIL action starts until the final planning summary is explicitly approved and `task.py start` succeeds.
