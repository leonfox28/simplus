# Design: QDC507 HIL cleanup and EG25-G serial

## 1. Boundaries

This task has one product result: the repository contains the production QDC507 cellular-SMS implementation and normal deterministic tests, but no executable one-off QDC507 HIL toolkit. The same cleanup exposes the model adapter as the only owner of a fixed EG25-G module-serial query.

Keep:

- production `QDC507SMS`, `qdc507sms.Adapter`, complete Driver/TTY transport, SQLite v2 state and normal SMS router;
- PDU encoding/decoding, 8/16-bit inbound concatenation, payload-echo handling, persist-before-ACK and outcome-unknown behavior;
- shared operation gate, production SMS target resolver, Agent protocol/backend, application Line transport selection and public Web/API behavior;
- deterministic unit, integration, Web and container tests for those production contracts.

Remove:

- `internal/qdc507hil/**` and every `cmd/simplus-qdc507-hil*/**` entrypoint;
- every `qdc507_hil` tagged file and the Make target that compiles them;
- HIL-only classifier/preservation/cleanup/recovery/outbound-confirmation state, SQLite and transport code;
- untagged interfaces/types/functions whose only consumers were those runners, including split inbox/outbound routers/adapters, dedicated HIL target resolvers and subscriber-number observation;
- public operational prose for commands that no longer exist. Preserve only the sanitized compatibility conclusion that real inbound/outbound SMS was accepted.

The removal is reference-driven: after deleting obvious tagged files, search each HIL-only symbol and remove it only when no production consumer remains. Production code must not be rewritten merely to reduce line count.

## 2. Module serial contract

### Adapter

Add a narrow model capability owned by `internal/modemadapter`, for example:

```go
type ModuleSerialAdapter interface {
    Adapter
    ReadModuleSerial(context.Context, attransport.Query) (string, error)
}
```

Only QDC507 implements it in this task. The fixed query sequence is model-private:

```text
AT+CGSN=?
AT+CGSN=0
AT+CGSN=1
```

The parser requires one terminal `OK`, bounded printable response lines, an explicit syntax advertisement containing both supported parameters, a non-empty bounded SN, a valid IMEI from parameter 1, and `SN != IMEI`. Unsupported, ambiguous, echoed, malformed, oversized, control-character or equal-to-IMEI results return unavailable. There is no `EGMR`, set form, fallback or guessed normalization.

The existing equipment identity remains the stable HMAC-bound IMEI flow. Module SN is display metadata and never replaces the fingerprint or persistence key.

### Agent and inventory flow

```text
QDC507 ModuleSerialAdapter
  -> existing bounded AT session during DeviceProbe
  -> agentapi.ModemIdentity.SerialNumber
  -> inventory PhysicalDevice.ModemSerialNumber
  -> application modem/line view serialNumber
  -> existing OpenAPI/Web serialNumber field
```

Do not put module SN in `USBIdentity.SerialNumber`; that field and its fingerprint continue to mean USB descriptor iSerial. Add a distinct internal module-serial field, then select module SN first and USB iSerial only as a display fallback. Candidate `usbSerialHint` and stable USB fingerprint behavior remain unchanged.

The public schema shape does not change. Descriptions change from “USB Serial” to “observed module serial, otherwise USB serial”. No raw value is persisted by Managed Modem storage; it is a current observation.

## 3. Fixed real-device observation

After implementation approval but before committing parser evidence:

1. Confirm exactly one QDC507 and record the existing Compose owner state privately.
2. Stop the dependent local Compose services so no second process owns the primary AT endpoint.
3. Use a temporary source/binary outside the repository, with a typed allowlist containing only `ATI`, `AT+QGMR`, `AT+CGSN=?`, `AT+CGSN=0`, and `AT+CGSN=1`.
4. Stop on target ambiguity, open/query failure, unexpected transcript or timeout. Do not retry with alternate commands.
5. Restore the exact Compose services and verify `agent/netd/app` health plus HTTP health.
6. Display manufacturer/model/firmware/SN/IMEI directly in the private conversation as authorized. Do not save raw output in tracked files, tests, task artifacts or ordinary logs.

Synthetic tests may reproduce only the public response grammar and boundary class, never the observed identifiers.

## 4. Documentation and privacy

- `docs/development.md` loses all QDC507 tagged-runner build/execution instructions.
- `docs/architecture.md` retains production SMS architecture and sanitized HIL evidence, not historical runner object graphs.
- `docs/compatibility.md` keeps accepted capability and unverified boundaries.
- `.trellis/spec/core/infra/hardware-and-hil-safety.md` removes executable contracts for deleted runners and adds one narrow rule: explicitly authorized low-sensitivity identifiers may be shown in the direct private session, but never written to repository files/logs; credentials/raw protocol/private topology remain prohibited.
- Archived task history remains documentation, not executable code. It must contain no actual private values.

## 5. Git history reconstruction

The current branch is exactly three commits ahead of `origin/main`, and none is pushed. After the desired tree passes all checks:

1. Record the full desired worktree/index and verify no private values.
2. Move `main` back to `origin/main` with the explicitly authorized non-hard history reconstruction, leaving the desired tree intact.
3. Create a new cleaned feature commit containing production QDC507 SMS, this cleanup and module-SN support, excluding Trellis bookkeeping.
4. Recreate the prior QDC507 task archive commit and journal commit, updating journal references to the new feature hash.
5. Archive and journal this task through normal Trellis commits.
6. Verify no branch/tag/ref names any old commit, expire reflogs, run immediate Git GC/prune, and prove each old hash fails `git cat-file -e`.
7. Never create a backup branch/tag and never push.

This is intentionally unrecoverable after the final prune, matching the user's explicit request.

## 6. Deployment and rollback

Only after the cleaned feature commit hash exists, rebuild `control`, `agent` and `netd` with tag `dev`, validate Compose config and run `docker compose up -d`. Preserve `./data`. Verify long-running service health, bootstrap exit 0, HTTP health and image revision.

Before history GC, a failed implementation can be abandoned while the working tree still contains all changes. After GC there is no rollback to the old HIL sources; production rollback remains possible through `origin/main` or a newly built prior production image, but no HIL toolkit is retained.
