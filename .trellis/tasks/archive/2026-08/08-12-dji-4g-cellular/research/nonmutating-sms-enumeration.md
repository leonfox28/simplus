# Research: non-mutating-sms-enumeration

- Query: Find a fixed QDC507/Quectel + 3GPP SIM `SM` inbound-candidate enumeration that does not change SMS unread/read or persistent SIM/ME state, replacing `AT+CMGL=4`; evaluate `AT+CRSM` over `EF_SMS` (`6F3C`), `EF_SMSP`/`EF_SMSS`, and Quectel vendor alternatives.
- Scope: mixed (repository plus primary 3GPP/ETSI/Quectel documentation only)
- Date: 2026-08-13

## Findings

### Decision

There is **no QDC507-firmware-specific primary evidence yet** which proves a truly non-mutating list/read command. Therefore the current real classifier must remain closed. Do not replace its fail-closed constructor with an executable reader based only on this research.

The best *qualified design candidate* is a separate, fixed `AT+CRSM` reader that uses only UICC `GET RESPONSE` and `READ RECORD` against `EF_SMS`, not `CMGF`, `CPMS`, `CMGL`, `CMGR`, `CSIM`, `UPDATE BINARY`, or `UPDATE RECORD`. At the UICC command level `READ RECORD` is a read operation, whereas the standard `+CMGL` and `+CMGR` contracts expressly change received-unread to received-read. This is strong enough to select the implementation direction, but not strong enough to claim QDC507 behaviour or authorize a real run.

If “does not change *any* SIM/ME state” includes transient current-file selection or undocumented modem cache/locking effects, no AT-level standard proves that absolute property: TS 27.007 says the MT performs locking and file selection internally and coordination with the MT application is implementation-dependent. The proposed contract can claim only: **no persistent EF update command is emitted and no known status-changing SMS command is emitted**. QDC507 HIL evidence is still required before claiming preservation of the actual unread byte.

### Primary standards and official module sources

| Source | What it establishes |
| --- | --- |
| [3GPP TS 27.005 Rel-18 / ETSI TS 127 005 V18.0.0, clauses 4.1–4.2](https://www.etsi.org/deliver/etsi_ts/127000_127099/127005/18.00.00_60/ts_127005v180000p.pdf) | `+CMGL[=<stat>]` and `+CMGR=<index>` return the PDU but change `received unread` to `received read`; neither is acceptable for this authorization. |
| [3GPP TS 27.007 / ETSI TS 127 007 V13.7.0, clause 8.18](https://www.etsi.org/deliver/etsi_ts/127000_127099/127007/13.07.00_60/ts_127007v130700p.pdf) | `+CRSM=<command>[,<fileid>[,<P1>,<P2>,<P3>[,<data>[,<pathid>]]]]`; `176` READ BINARY, `178` READ RECORD, `192` GET RESPONSE, and the two UPDATE commands are distinct. MT handles file selection/locking; SIM-command coordination is implementation-dependent; `pathid` is a UICC path from MF. |
| [3GPP TS 31.102 / ETSI TS 131 102 V12.10.0, clause 4.2.25](https://www.etsi.org/deliver/etsi_ts/131100_131199/131102/12.10.00_60/ts_131102v121000p.pdf) | USIM `EF_SMS`: FID `6F3C`, linear-fixed records, length 176 bytes, byte 1 status and bytes 2–176 SMSC address plus TPDU; access is READ PIN / UPDATE PIN. Incoming read and unread status encodings are defined here. |
| [3GPP TS 51.011 / ETSI TS 151 011 V4.10.0, clause 10.5.3](https://www.etsi.org/deliver/etsi_ts/151000_151099/151011/04.10.00_60/ts_151011v041000p.pdf) | Legacy SIM equivalent: `EF_SMS` FID `6F3C`, 176-byte linear-fixed records, READ CHV1 / UPDATE CHV1. |
| [3GPP TS 31.102 / ETSI TS 131 102 V8.21.0, clause 4.7](https://www.etsi.org/deliver/etsi_ts/131100_131199/131102/08.21.00_60/ts_131102v082100p.pdf) | UICC filesystem diagram places `EF_SMS` (`6F3C`), `EF_SMSP` (`6F42`), and `EF_SMSS` (`6F43`) beneath MF/`DF_TELECOM` (`7F10`), rather than assuming the currently selected ADF. |
| [ETSI TS 102 221 V13.0.0, clauses 8.4.2–8.4.3](https://www.etsi.org/deliver/etsi_ts/102200_102299/102221/13.00.00_60/ts_102221v130000p.pdf) | Path-from-MF rules and the special `7FFF` ADF marker. A MF path must not start with `3F00`; selecting by the explicit `7F106F3C` path avoids reliance on the modem’s current ADF/DF selection. |
| [Quectel EC25/EC21 AT Commands Manual V1.3, `CRSM` and `CMGL`](https://quectel.com/content/uploads/2021/03/Quectel_EC25EC21_AT_Commands_Manual_V1.3.pdf) | Official Quectel LTE-family evidence for `CRSM` response shape and for `CMGL` changing unread records to read. It is a fixture source only, not QDC507 evidence. |
| [Quectel EG06xK/Ex120K/EM06xK AT Commands Manual, `CRSM` and `CMGL`](https://forums.quectel.com/uploads/short-url/zwVbq0bdsIXR1qTexLq478k8enT.pdf) | Another official Quectel LTE-family manual: `CRSM` supports `pathId`, and says CRSM configuration is not saved; its `CMGL` documentation again says unread becomes read. It does not establish QDC507 support. |
| [Quectel GSM SMS Application Note V1.1](https://quectel.com/content/uploads/2021/03/Quectel_GSM_SMS_Application_Note_V1.1.pdf) | Old GSM-family extension documents `CMGL` `<mode>=1` as “not change status.” It is neither in current TS 27.005 nor documented in the EC25/EC21 LTE manual, so it cannot be assumed on QDC507. |
| [Quectel BG77xA/BG95xA AT Commands Manual, `QCMGR`](https://quectel.com/content/uploads/2024/05/Quectel_BG77xA-GLBG95xA-GL_AT_Commands_Manual_V1.1.pdf) | `QCMGR` is explicitly similar to `CMGR` and text-mode-only; it is not a proven state-preserving PDU enumeration alternative. |

### Fixed CRSM candidate protocol

`EF_SMS` has FID `0x6F3C` = decimal `28476`. Use the explicit UICC MF path `"7F106F3C"` in every operation; do **not** assume an ADF-USIM path and do not prefix `3F00`. Treat lack of the path argument or a path rejection as unsupported, not as permission to silently retry in another current DF/ADF.

The command grammar requires placeholders before `pathid`. Logical calls (shown only as a proposed compiled-in transcript, never a user/API-supplied command) are:

1. `GET RESPONSE`: `AT+CRSM=192,28476,,,,,"7F106F3C"`.
   The empty P1/P2/P3/data fields are deliberate. Require exactly `+CRSM: 144,0,"<bounded-even-hex>"` followed by `OK`; reject CME/ERROR, any non-`90 00` status word, missing/extra result data, malformed quoting/hex, or unexpected transcript/URC handling. Parse only a documented legacy response or a UICC FCP that establishes a linear-fixed `EF_SMS`, record length exactly 176, and an explicit record count within a compiled maximum. Otherwise return `unsupported`/`invalid`, never guess a slot count.
2. For each record number `n` in the validated inclusive range `1..count`, issue READ RECORD in absolute mode: `AT+CRSM=178,28476,n,4,176,,"7F106F3C"`.
   `178` is READ RECORD, P1 is the 1-based record number, P2=`4` is absolute record mode, and P3=`176` requests the whole record. Again require one `+CRSM: 144,0,"<352 hex chars>"` plus `OK`; no continuation/status-word recovery or partial result is safe for a classifier.
3. Interpret record byte 1 before any PDU parser. Standard received-read is `0x01`; received-unread is `0x03`; free is `0x00`; the remaining 175 bytes begin with the TS-Service-Centre-Address and then the TPDU. Do not emit these bytes, their digest, index, sender, time, or decoded body. Only decoded incoming (`0x01`/`0x03`) records may feed the existing in-memory multipart classifier. Free records are ignored; any other used/outgoing/status-report/unrecognised status makes the result `unexpected` rather than letting an exact match hide it.

The response parser must cap all line, response, record-count, and total-byte budgets. It must accept neither an arbitrary `+CRSM` command number nor an arbitrary file/path. `+CRSM` is a *restricted access* AT command, not an excuse to introduce generic APDU or AT execution.

This design intentionally does **not** emit `AT+CMGF=0` or `AT+CPMS="SM","SM","SM"`: both are ME settings, and neither is required to read the explicitly-addressed EF. It also deliberately does not use `EF_SMSP` (`6F42`) or `EF_SMSS` (`6F43`): they are SMS parameters/status metadata, do not enumerate inbound PDUs, and may expose unrelated subscriber/SMSC state. No `UPDATE` command is ever in the reader’s type or command allowlist.

### State-preservation and race boundary

- Standards support that the emitted UICC operations are GET RESPONSE/READ RECORD, not UPDATE, and do not specify an unread-to-read transition for them. This is materially different from `CMGL`/`CMGR`, for which TS 27.005 expressly mandates the transition.
- TS 27.007 does **not** promise that `+CRSM` is free of transient MT selection, locks, caches, or contention; it says the MT handles locking/file selection and coordination is implementation-dependent. Quectel manuals inspected provide no QDC507-specific promise that raw `READ RECORD` bypasses an ME SMS cache. Treat any such promise as unproven until the exact firmware is observed.
- A per-device operation gate and existing pre/post equipment/SIM generation fences exclude local concurrent actors but cannot freeze a network-delivered SMS or SIM Toolkit action during a record-by-record scan. A safe classifier must scan the bounded EF twice and require the same count and byte-for-byte same records (comparison only in memory); any difference, transport loss, status word other than `90 00`, identity change, overflow, malformed PDU, or incomplete multipart group returns only the existing de-identified `unknown`/integrity outcome. This reduces, but cannot create, an atomic UICC snapshot; the result remains evidence only and never authorizes adoption/ACK/delete.
- Raw EF record number is likely the physical SIM slot, but TS 27.005 does not prove it equals the Quectel `CPMS="SM"` index used by later `CMGD`. The classifier does not need this mapping. Any later ACK/delete path needs a new authorization plus explicit QDC507 proof of record-number/index mapping and PDU-digest revalidation; it must not infer it from this classifier.

### Quectel alternatives assessed

| Candidate | Evidence | Result |
| --- | --- | --- |
| `CMGL=4` / `CMGR=<index>` | TS 27.005 and official Quectel LTE manuals explicitly mark received-unread as read. | Rejected. |
| `CMGL=<stat>,1` | The old Quectel GSM SMS Application Note documents a nonstandard `<mode>=1`, but current TS 27.005 syntax and Quectel EC25 LTE manual expose only `<stat>`. No QDC507 manual/firmware evidence was found. | Rejected unless a QDC507 primary manual and exact firmware HIL prove it; no fallback/probe on a real card under the existing authorization. |
| `QCMGR` / Quectel concatenation helpers | Official manuals describe it as CMGR-like and text-mode-only, with no state-preservation guarantee. | Rejected. |
| QMI WMS or a generic `CSIM` APDU path | No QDC507 primary-source evidence was found in scope. `CSIM` would also violate the repository’s no-generic-command object-graph rule. | Out of scope; do not substitute. |
| `CRSM` GET RESPONSE + READ RECORD of `7F10/6F3C` | Standard file/record semantics provide the only supported non-UPDATE candidate; Quectel LTE-family manuals document CRSM shape/path but not QDC507. | Recommended design direction, still unavailable until fixture and HIL evidence. |

### Files found

| File | Description |
| --- | --- |
| `internal/modemadapter/qdc507sms/readonly_classifier.go` | Tagged classifier now implements the recommended fixed CRSM EF_SMS double scan and bounded legacy/FCP parser; this is synthetic fixture readiness, not firmware evidence. |
| `internal/modemadapter/qdc507sms/driver.go` | Inbox driver likewise uses `CMGL=4` and `CMGR`; `prepare` changes SMS-mode/storage selection. |
| `internal/qdc507hil/classifier_hardware_qdc507_hil.go` | Real classifier construction remains deliberately closed with a QDC507 CRSM response-shape/unread-preservation firmware blocker. |
| `internal/modemadapter/qdc507sms/unread_preservation.go` | Tagged zero-unread baseline, exact free-to-unread transition and three-stable-scan verifier; synthetic fixture only. |
| `internal/qdc507hil/unread_preservation_hardware_qdc507_hil.go` | Constructible independent future-HIL graph with no application/recovery DB; implemented but never run. |
| `cmd/simplus-qdc507-hil-crsm-preserve/main_qdc507_hil.go` | De-identified ready/final CLI with only bounded arrival/total timeouts and PTY or `/dev/null` stdin. |
| `.trellis/tasks/08-12-dji-4g-cellular/design.md` | Design declares the precise non-mutating-enumeration gate before real classification can be proposed. |
| `.trellis/spec/core/infra/hardware-and-hil-safety.md` | Requires an object graph without generic command/prompt, Send, ACK/Delete, RF, registration, call/data, or subscriber-number capabilities; explicitly calls `CMGL` non-read-only. |

### Code patterns

- The tagged classifier's only modem operations are compiled-in GET RESPONSE and READ RECORD request variants; the transport exchange is unexported and accepts no caller command, APDU, path, endpoint, or fallback.
- The normal inbox still lists with `CMGL=4` and reads with `CMGR`, `internal/modemadapter/qdc507sms/driver.go:152-185`; its preparation changes `CMGF`/`CPMS` at `:260-282`.
- The real HIL constructor remains deliberately unavailable because standards/Quectel-family fixtures do not prove the current QDC507 firmware's CRSM response or unread preservation.
- The task design prohibits promotion or execution until a newly authorized firmware-specific preservation HIL succeeds.
- The hardware spec requires the narrower concrete composition and fails closed until the exact enumeration is proven, `.trellis/spec/core/infra/hardware-and-hil-safety.md:50-73`.

### Required capability/object graph and tests

Implement only after task design/spec review, with a distinct `EFSMSRecordEnumerator` (or equivalently named) whose sole public operation is classification and whose command table is an unexported fixed list of the two CRSM operation forms above. It owns a bounded typed transcript transport, never a generic `Command`, APDU, prompt, or device-path API. It must not implement or embed the regular SMS driver/inbox backend.

Required synthetic tests:

- exact GET RESPONSE and READ RECORD transcripts; malformed AT framing, duplicate/unexpected lines, non-`90,0` SW values, FCP/legacy response mismatch, record length/count bounds, path-placeholder serialization, and no undocumented retry;
- `EF_SMS` record fixtures for free, read, unread, outgoing/unexpected, wrong-length, malformed PDU, read/unread single part, all multipart completion/duplicate/span cases, and proof that status byte is removed before PDU decoding;
- double-scan equality, pre/post device/SIM generation fence failures, changing record count/bytes, cancellation, and overflow all yield a non-actionable classification;
- reflection/static command-allowlist tests proving absence of CMGL/CMGR/CMGF/CPMS/CSIM/UPDATE/Send/ACK/Delete/RF/registration/call/data/subscriber-number capabilities; output tests prove no PDU/index/address/body/identity leaks;
- fixtures proving no reliance on `CPMS` index mapping. Any future mapping test belongs to the separately authorized ACK/delete capability, not this classifier.

Authorization boundary: compilation and synthetic tests are non-HIL. A real unread-preservation proof cannot be obtained merely by a no-message HIL-0 probe: it requires an authorised, controlled unread record and a privacy-preserving before/after comparison of its EF status byte using this same fixed reader. Creating such a record entails a real inbound SMS, so it needs new exact HIL authorization; never use an existing private inbox as an implicit test fixture. Even a successful classifier report is not adoption, ACK, deletion, resend, RF, call/data, or production-promotion authorization.

## Related specs

- `.trellis/spec/core/infra/hardware-and-hil-safety.md` — typed hardware boundary, classifier composition, real-HIL authorization, and fail-closed condition.
- `.trellis/spec/core/backend/application-boundaries.md` — bounded typed errors and transport detail containment.
- `.trellis/spec/core/backend/quality-and-testing.md` — fixture-first hardware validation, deterministic tests, privacy, and fail-closed unsupported hardware cases.
- `.trellis/tasks/08-12-dji-4g-cellular/prd.md` R10–R13 / AC8–AC12 and `.trellis/tasks/08-12-dji-4g-cellular/design.md` section 9.4 — SMS fence, durable handling, existing classifier stop condition, and authorization requirements.

## Caveats / Not Found

- No public primary QDC507 AT manual, firmware release note, or Quectel/DJI statement was found that confirms `CRSM`, the `7F106F3C` path, EF response shape, record/index mapping, or a non-mutating vendor SMS list/read command for the exact device/firmware.
- No current standard `CMGL` “do not change status” parameter exists in the reviewed TS 27.005 Release 18 contract. The old Quectel GSM extension is not portable evidence.
- `EF_SMS` can be optional (USIM service availability and card policy), requires PIN/CHV read access, and can be unavailable/locked. Never enter PIN, alter access conditions, select another storage, or fall back to `CMGL`/`CMGR` when it is inaccessible.
- UICC receive/storage races cannot be eliminated with CRSM alone. The design can be non-mutating and fail closed, but cannot truthfully promise an atomic inbox snapshot or safe later deletion without the separate mapping/evidence/authorization boundary.
