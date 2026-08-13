# 大疆 QDC507 原生蜂窝短信：实施计划

## 1. Execution Shape

这是一个原子跨层纵切，不拆成可独立归档的 child task：稳定身份、注册状态、SIM fence、durable SMS、per-Line transport 和生产 evidence gate 互相构成准入条件。任何一项单独上线都会留下不可安全使用的半成品。

实施分为两个代码阶段，中间有一个强制的真实 HIL 停止点：

- Stage A 完成全部类型化实现、合成 fixture、应用/Web 集成与 build-tag HIL runner，但保持 production QDC SMS 关闭。
- Stage B 只有在 Stage A 质量门通过、重新取得精确 HIL 授权且真实纵切成功后，才提升证据并装配 production Agent/simplusd。

不得在 `task.py start` 前执行本清单，不得把任务创建或本规划审批解释为 HIL 执行授权。

## 2. Ordered Implementation Checklist

### Phase A1 — Shared identity and QDC base adapter

- [x] 阅读本任务 PRD/design、backend/infra/docs/Web specs、ADR 0017/0019 和研究记录；确认 worktree 中其他改动并保留。
- [x] 搜索所有 `QDC507{}`、`DefaultRegistry`、identity/operator helpers 和 capability assertions，建立修改前影响清单。
- [x] 把 ML307A 内可复用的 EF_SPN、EF_AD MNC length、IMSI bounded parser 提取为 SIM-neutral helper；保持 SIM AKA 专用逻辑与错误不被短信需求污染。
- [x] 为 `QDC507` 实现 `EquipmentIdentityAdapter`、`SIMIdentityAdapter`、`RFControlAdapter`，命令固定为设计中的 allowlist；identity metadata best effort，RF 必须 handshake + write + read-back。
- [x] 增加合成 QDC fixtures：15 位有效/无效 IMEI、`+QCCID` quoted/unquoted/malformed、SPN、两/三位 MNC、缺文件/非法响应、中国联通 profile data 无硬编码。
- [x] 修正 QDC capability evidence/interface 一致性，并锁定电话、数字媒体、Host VoWiFi、operator selection 仍未被提升。
- [x] 用编译期类型断言与合成第二型号 adapter fixture 验证 registry 和公共 capability services 只依赖小型统一接口；不得为 QDC507 在 application/domain/API/Web 增加型号分支。
- [x] 增加 scanner/HIL-only `SubscriberNumberAdapter` 与 QDC 固定 `CNUM` parser；只接受唯一显式 `+E.164`，所有 unavailable 结果均不影响 identity、注册或 SMS readiness，且不进入公共/持久/日志边界。

Target files/areas:

- `internal/modemadapter/qdc507.go`
- `internal/modemadapter/identity.go`
- `internal/modemadapter/*operator*`, `*usim*`, corresponding tests
- `internal/hardwareprobe/at_runtime*.go`
- `internal/modemadapter/registry_test.go`

Focused validation:

```bash
go test ./internal/modemadapter ./internal/hardwareprobe
```

Rollback point: QDC identity/RF capability commit boundary. No real hardware action is part of this phase.

### Phase A2 — One cellular classifier and Managed Modem status

- [x] Add the pure typed classifier for SIM/RF/registration states with an exhaustive table test covering every Agent registration enum and precedence conflict.
- [x] Replace the application RF-only status read with a single full `RuntimeStatusReader`; keep the RF setter as a separate mutation port.
- [x] Map one targeted probe into `domain/modem.CellularStatus`, including normalized PLMN, bounded operator/RAT/signal, observation time and stable error code. Never persist or cache a registered status for an offline modem.
- [x] Extend `api/openapi.yaml` with the nested cellular object and closed enums; regenerate Go and Web outputs through project generators.
- [x] Update HTTP response mapping/tests for exact safe fields and explicit absence of private Agent fields.
- [x] Update `Modems.tsx` desktop and compact renderers with exhaustive status tags, network/RAT/signal/age display, unavailable context and responsive tests.
- [x] Update synthetic desktop/mobile E2E fixtures and assertions without real modem data.

Target files/areas:

- `internal/agentapi/protocol*.go` or a focused cellular classifier file
- `internal/application/modem/agent_rf.go` (split/rename as appropriate)
- `internal/application/modem/service*.go`
- `internal/domain/modem/model.go`
- `api/openapi.yaml`, generated Go/Web outputs
- `internal/api/httpapi/server*.go`
- `web/src/pages/Modems*.tsx`, `web/e2e/app.spec.ts`

Focused validation:

```bash
go test ./internal/agentapi ./internal/application/modem ./internal/api/httpapi
make generate
make verify-generated
corepack pnpm --dir web typecheck
corepack pnpm --dir web test -- Modems
corepack pnpm --dir web build
```

Rollback point: OpenAPI source plus all generated outputs must move together. Do not hand-edit generated files.

### Phase A3 — Shared Agent operation gate and fenced SMS target

- [x] Introduce one context-aware per-device gate with no unbounded allocation for invalid device IDs.
- [x] Inject it into scanner and SMS runtime; migrate probe, identity, SIM AKA and RF paths from the global mutex without changing their typed behavior.
- [x] Add a messaging-owned private runtime-Line resolver that joins the already resolved Line, current subscription profile and physical equipment observation to bounded equipment/SIM fingerprints; do not add either fingerprint to public `inventory.Line`, revision payloads or OpenAPI.
- [x] Extend Agent List/Read/Send/ACK requests with device generation and bounded expected equipment/SIM fingerprints; update client/server validation, correlation, local simulator fixtures and no-store/logging contracts.
- [x] Build SMS target resolution under the same gate: re-resolve the device/generation, fresh probe, constant-time equipment+SIM comparison and Send-only cellular readiness.
- [x] Add stable Agent error sentinels, HTTP codes, retryability, `ErrorResponse.Is` and Agent gateway mappings; distinguish definite failure from outcome unknown.
- [x] Add concurrency, cancellation, queued hotplug, SIM-swap TOCTOU, malformed response and privacy tests.

Target files/areas:

- `internal/hardwareprobe/scanner.go`, RF/identity/SIM AKA files and tests
- `internal/modemadapter/sms.go`, `registry_test.go`
- `internal/agentapi/sms_protocol.go`, client/server and tests
- `internal/application/inventory`, `internal/application/line`
- `internal/application/messaging/agent_sms.go` and tests

Focused validation:

```bash
go test -race ./internal/hardwareprobe ./internal/modemadapter ./internal/agentapi
go test ./internal/application/inventory ./internal/application/line ./internal/application/messaging
```

Rollback point: protocol client/server/request fixtures and internal Line privacy audit must be reviewed as one change.

### Phase A4 — QDC durable SMS v2 and SIM storage

- [x] Change `SMSAdapter` calls to use `SMSRuntimeTarget{Device, SubscriptionKey}`; keep wire `DeviceID` correlation transient.
- [x] Update driver preparation to validate exact `CMGF=0` then `CPMS="SM","SM","SM"`; no `ME/MT`, persistent setting, or arbitrary command fallback.
- [x] Refactor inbound IDs/state keys/send and ACK digests from transient device ID to subscription key.
- [x] Implement private state schema v2 with explicit checked subscription namespace, atomic operations, mode/ownership validation, WAL/FULL sync and checkpoint close. Use a fixed v2 production filename; do not migrate or delete v1 automatically.
- [x] Preserve/extend existing GSM7/UCS-2, multipart, partial send, outcome unknown, application-database-before-ACK, Agent recovery-state-before-delete, index reuse and delete reconciliation behavior; `SM` is a physical staging area, not the only or long-term message store.
- [x] Add reopen/hotplug/swap tests: same SIM on new port sees the same pending state; another modem/SIM cannot list/read/ACK/replay it; stale generation or expected identity fails before driver I/O.
- [x] Verify no fixtures contain real phone/SIM/device data and no logs add body/address/PDU/fingerprint.

Target files/areas:

- `internal/modemadapter/qdc507sms/adapter.go`, `driver.go`, `message.go`, `state.go`, `sqlite_state.go`
- corresponding adapter/driver/transport/state tests
- `internal/modemadapter/sms.go`

Focused validation:

```bash
go test -race ./internal/modemadapter/qdc507sms ./internal/modemadapter
```

Rollback point: retain any v2 database; never delete uncertain operation or partial ACK evidence during rollback.

### Phase A5 — Per-Line transport resolver

- [x] Replace `sender/inbox/hostVoWiFiSMS` global selection with explicit transport bundles and one resolver owned by messaging.
- [x] Define eligibility separately from availability and return ambiguity for multiple eligible bundles; never priority-select or fallback.
- [x] Resolve once for Send, persist before the selected dispatch, and carry the resolved SIM fingerprint to Agent.
- [x] Resolve each Line independently for inbound and use inbox/reports from the same selected bundle; collect a failed Line without starving other ready Lines, and never try another transport for it.
- [x] Update Simulator composition to one synthetic Agent bundle and hardware composition test fixtures to Agent + optional VoWiFi bundles.
- [x] Add native-only, VoWiFi-only, zero, ambiguous, selected-unavailable and selected-runtime-failure tests with call counters; retain existing Host VoWiFi submit-report behavior.
- [x] Add Web/API message status presentation for new bounded preflight failure codes without exposing raw transport detail.

Target files/areas:

- `internal/application/messaging/service.go`, `inbound.go`, gateways and tests
- `cmd/simplusd/main.go`, `main_test.go`
- `web/src/messages/status*.ts`, `web/src/api/errors.ts`, focused page tests

Focused validation:

```bash
go test -race ./internal/application/messaging ./cmd/simplusd
corepack pnpm --dir web test -- Messages
corepack pnpm --dir web typecheck
```

Rollback point: global transport fields must be removed atomically; no hybrid global/per-Line path may remain.

### Phase A6 — Fixture-only HIL composition and pre-HIL quality gate

- [x] Add a `qdc507_hil` build-tag typed runner/test using the same runtime scanner, gate, target resolver, driver and store. It discovers exactly one QDC507, reads only the private destination from bounded stdin JSON, accepts only timeout argv, and internally generates a bounded 128-bit `crypto/rand` GSM-7 marker; entropy failure stops before runtime construction. It accepts no caller marker, AT/QMI or device path.
- [x] Attempt typed `CNUM` only after successful registration and a fresh unique target/SIM fence; discard the value in memory, report only de-identified available/unavailable, continue on unavailable, and keep the approved external destination as expected inbound sender.
- [x] Make initial state capture, operation count, unknown-outcome stop, identity/generation recheck and RF/service cleanup explicit and testable with fakes.
- [x] Keep the runner out of normal build/test and ordinary production handler; no long-lived `--enable-qdc507-sms` or generic HIL socket.
- [x] Run an architecture-boundary audit: QDC507/VID/PID/AT/QMI/device-path knowledge may appear only in composition, discovery, registry, adapter/driver, private fixtures and documentation—not in Line, messaging, Managed Modem, HTTP/OpenAPI or Web control decisions.
- [x] Run all Stage A deterministic checks and a privacy diff review.
- [x] Stop and present the exact real HIL proposal. Do not deploy/restart Agent, set RF, select CPMS on hardware, send/receive SMS or run the tagged HIL until the user explicitly approves that exact proposal.

Target files/areas:

- focused build-tag HIL test/runner near `internal/hardwareprobe` / `qdc507sms`
- Makefile build-only target and `docs/development.md` command classification, if a persistent target is retained
- test fixtures for the runner state machine

Pre-HIL validation:

```bash
go test ./internal/agentapi ./internal/attransport ./internal/modemadapter/... ./internal/hardwareprobe/...
go test ./internal/application/inventory ./internal/application/modem ./internal/application/line ./internal/application/messaging
go test ./internal/api/httpapi ./cmd/simplus-agent ./cmd/simplusd ./internal/containercontract
make check-format
make verify-generated
make check-container-files
make check-docs
make lint
make test
corepack pnpm --dir web build
make web-e2e
make security
```

Safe static/build commands do not authorize HIL. Diagnose scoped failures only; do not repair unrelated work.

### Phase H — Exact authorization and controlled HIL (hard stop)

Observed partial result: the one approved outbound was delivered to the peer but
remains protocol outcome-unknown/unconfirmed; subscriber-number observation was
available; the reply arrived after runner stop; RF Off restoration was blocked by
two active mode=1 data calls. The separately approved receive-only recovery was run
once and stopped on multiple inbound candidates before application persistence or
ACK/delete; no retry was attempted. Later code/standard review established that its
`CMGL=4` list may have marked unread records read and that assembled candidates were
written to the Agent-private recovery database during listing. They were not promoted
to the user-visible application database. The later fresh inbound path and separately approved new
outbound confirmation both completed; the confirmation returned persisted/confirmed/complete while the
historical unknown remained unchanged. Stage B is unblocked for this exact SMS capability.

- [x] Diagnose the delivered-but-unknown submit path without resending: add exact one-time payload-echo
  suppression below `TTYTransport.Prompt`, preserve every non-exact/duplicate/URC line, and add low-level plus
  complete `Driver.Send` regressions for `CMGF/CPMS -> prompt -> payload echo -> +CMGS -> OK`. Treat this as a
  high-probability root cause only; never mutate or resend the original unknown operation.
- [x] Implement a separate build-tag `simplus-qdc507-hil-outbound-confirm` runner for a future newly
  authorized one-shot submit. Its concrete graph is send-only: `QDC507Outbound` fixed registration/
  equipment/QCCID observation, shared operation gate, `SMSOutboundRouter`, outbound-only tty driver,
  operation-only existing-v2 SQLite store, and a narrow application store that can create/finish only
  its new row. It retains no Line/topology service, RF setter, inbound List/Read/ACK/Delete, subscriber,
  call/data/QMI/SIM-AKA or generic AT capability. It requires RF already On, SIM ready, no voice/fax/unknown call,
  no pending inbound state and no prior confirmation attempt; waits boundedly for registration, creates
  a crypto-random operation ID and marker, sends at most once, preserves the original unknown app/ledger
  rows byte-for-field, and never retries unknown. Add it to the compile-only Make target; implementation
  and tests do not authorize running it. The exactly approved run is complete; existing mode-1 data bearers
  were left untouched and were not treated as voice calls.

- [x] Add a separate tagged inbound-recovery command whose interfaces and object
  graph contain no Send/RF/registration/call/data/subscriber capability or
  destination/marker input; use discovery-only, identity/SIM-only and
  command-only concrete types rather than hiding complete objects behind interfaces.
- [x] Correlate the complete private application message set with exactly one
  same-SIM recovery outcome-unknown operation using operation ID and constant-time
  request-digest comparison; never query recovery SQLite schema from `qdc507hil`.
- [x] Add receive-only fencing, exact sender/body cardinality, persist-before-ACK,
  strict raw-PDU cardinality, PDU revalidation/delete, pending-empty,
  replay/restart and service-restore tests.
- [x] Obtain separate exact authorization before executing recovery. Do not use
  recovery implementation or checks to access the real modem/SIM/database, start
  services, read/delete real SMS, change RF, or introduce hangup/data controls.

- [x] Execute the authorized receive-only recovery exactly once with a two-minute
  inbound deadline and five-minute total deadline. It stopped on multiple inbound
  candidates before application persistence or ACK/delete; no outbound, RF,
  registration, call/data action, or retry occurred. Temporary binary removed and
  both fixed Agent services verified inactive/disabled with sockets absent. Later
  review records the possible unread-to-read transition and the expected private
  recovery-ledger candidate writes; neither is described as a fully read-only run.
- [x] Obtain exact authorization for one read-only classification of whether the
  multiple candidates contain exactly one source+marker match. This authorization
  does not include adoption/ACK/delete and is not exercised by ordinary implementation checks.
- [x] Implement a third build-tag-isolated classifier binary whose concrete graph has
  no application/recovery write, Send, ACK/Delete, RF, registration, call/data,
  subscriber-number, arbitrary Command or Prompt capability. Open application and
  recovery SQLite with `mode=ro` only; derive the candidate from one application
  snapshot, correlate the unique same-SIM unknown ledger, parse fixed-SM raw PDUs in
  memory, recheck the complete fence before/after, and emit only bounded de-identified
  cardinality/integrity categories.
- [x] Add deterministic construction/reflection, true read-only DB/no-artifact,
  exactly-one/zero/multiple, unexpected/malformed/incomplete, fence, cleanup, CLI and
  privacy tests. Include the classifier in the compile-only tagged Make target while
  keeping production/default registry closed.
- [x] Review identified that 3GPP/current Quectel `CMGL=4` changes `REC UNREAD` to
  `REC READ`; keep the real classifier constructor fail closed while retaining tagged
  parser/fence/SQLite fixtures. The existing read-only authorization does not cover
  this SIM state mutation.
- [x] Select and implement the fixed CRSM EF_SMS GET RESPONSE/READ RECORD double-scan candidate,
  update PRD/design/spec/docs, and cover exact legacy/FCP fixtures, status bytes, parser bounds,
  scan/fence changes, concrete graph and production-closed behavior. This is fixture-ready only.
- [x] Implement a separate tagged CRSM unread-preservation verifier with no application/recovery DB,
  candidate input or broad modem capability: fixed service ownership, unique QDC507, one gate across
  zero-unread double-scan baseline, de-identified ready flush, exact free-to-unread transition and three
  stable full scans with fresh fences. Add bounded arrival/total CLI, privacy/object-graph/state-machine
  tests, compile-only Make wiring and implemented-not-run docs. Do not execute it.
- [x] Under a new exact authorization, run the current QDC507 CRSM preservation verifier once with
  a two-minute arrival deadline and five-minute total deadline. It failed with a de-identified unknown
  result before `ready-for-one-controlled-inbound`, so no new SMS was requested/sent and no retry was
  attempted. Temporary binary was removed; both fixed Agent services remained inactive/disabled and
  sockets absent. This did not verify response shape or unread preservation.
- [x] Cancel additional CRSM scans: the user-reviewed exact cleanup and subsequent normal
  fresh-inbound validation superseded this diagnostic branch, so no renewed authorization
  was requested and the real classifier remains fail closed.
- [x] Do not execute the tagged read-only classifier against the real modem/SIM/private HIL
  databases; retain it only as a compile-tested fixture until a separate future task proves
  the firmware contract and obtains new exact authorization.
- [x] Preserve the hard stop: no classifier result was used for adoption, application write,
  ACK/delete or recovery action.

- [x] After the user privately reviewed exactly two pending assembled recovery candidates and
  explicitly authorized clearing only those two, implement a separate
  `simplus-qdc507-hil-clear-reviewed` tagged entrypoint. It does not read the private TXT and
  hard-codes reviewed count 2. Its concrete graph retains only fixed service ownership,
  QDC507 equipment/QCCID/SIM-ready fencing, one full-cleanup operation gate, a dedicated
  `CMGR=<stored index>` / single-index `CMGD=<same index>` driver, and a narrow v2 recovery
  store that validates private artifacts/schema/WAL/FULL/integrity before `mode=rw` and can mutate only
  monotonic segment delete-started/deleted progress plus one atomic both-record ACK transaction.
  A delete-started crash residue stops before another read/delete. Document that CMGR may mark unread read before
  the approved deletion.
- [x] Add tagged race/fixture coverage for exact-two, malformed/already-ack/subscription mismatch,
  read-digest-delete-persist ordering, multipart progress, first-success/second-failure,
  generation/SIM/index/digest/delete uncertainty, DB failure, operations/sender/body immutability,
  schema/tamper/artifact/symlink/mode/WAL, service restoration, object-graph/command allowlist,
  output privacy, no list/bulk delete, and ordinary production closure. Add the binary to the
  compile-only Make target and update canonical docs/spec/design as implemented-not-run.
- [x] Execute the reviewed cleanup only in the main session under the already exact authorization.
  Code/test agents must not stop services, open the real private database, touch modem/SIM, run
  the tagged binary, or delete the private TXT. Stop without automatic retry on any uncertainty;
  execution is not part of implementation validation and does not promote HIL/production evidence.
  The main session deletes the outside-repository private TXT only after a successful DB-only cleared
  verification; every failure or unknown result retains it for review.
- [x] The one authorized run returned `reviewed=2; cleared=0; pending=unknown` before segment deletion.
  A subsequent read-only recovery-DB progress summary (no SIM access) confirmed three pending segments,
  zero `delete-started`, and zero deleted segments. No retry occurred; the private TXT remains. Obtain
  renewed exact authorization before any diagnostic scan or later cleanup attempt.
- [x] Apply a diagnostic-only cleanup follow-up: allow SQLite to create only revalidated private WAL/SHM
  auxiliaries for an existing valid main DB, add the exhaustive bounded `stage` output contract, and make
  ordinary QDC runtime fail closed on `delete-started` while preserving legacy segment JSON. This follow-up
  used only tagged synthetic/compile checks; it did not rerun cleanup, open the real private DB, stop services,
  or touch modem/SIM. The historical one-run output above predates the `stage` field.
- [x] After the user explicitly requested continuing the already-authorized cleanup without repeated approval,
  rerun the same exact-two target once after independent review. It completed with
  `reviewed=2; cleared=2; pending=zero; stage=complete`; all three stored PDU segments were revalidated and
  deleted, both recovery records were atomically acknowledged, and the private TXT/temporary binary were
  removed. Both fixed Agent services remained inactive/disabled with sockets absent.

- [x] After the user confirmed the next normal inbound flow, implement a separate
  `simplus-qdc507-hil-fresh-inbound` tagged runner rather than reusing the old marker-bound recovery.
  Derive only bounded E.164 peer and Line from one read-only application snapshot of the unique original
  outcome-unknown outbound; correlate its unchanged same-SIM unknown ledger read-only; compile expected
  body `OK` in and accept no private input. Take service ownership before opening state and restore it
  unconditionally. Require recovery pending zero plus strict fixed-SM physical zero before a fresh fence and
  flushed ready; after ready accept exactly one complete peer+`OK`, persist application DB before ACK, then
  require PDU revalidation/single-index delete/progress and final application/provider idempotency, original
  outbound unchanged, recovery zero and SM zero. Support only startup-snapshot exact app-persisted and
  physically-complete SIM-pending replay before any acknowledgement operation has started; reject a
  fresh-branch application replay, ledger-only segment, or accepted/uncertain ACK without another delete.
- [x] Add tagged state-machine/application-store/object-graph/CLI/source/privacy tests for wrong sender/body,
  multiple/incomplete/malformed, persist/fence failure, delete uncertainty, replay/idempotency, final zero,
  cleanup and no Send/Prompt/RF/register/call/data/subscriber/outbound mutation. Add the command to the
  compile-only Make target and update docs/spec/design as implemented-not-run. Do not execute it or access
  real service/modem/SIM/private DB during implementation checks; this flow uses no CRSM auxiliary validation.
- [x] In the main execution session only, use the already user-confirmed flow: two-minute arrival/five-minute
  total deadline; wait for the flushed ready line, then ask the original approved peer to send exactly one
  new body `OK`; stop without automatic retry on any error or uncertainty. Record only bounded stage/result.
- [x] Diagnose the pre-ready malformed baseline without exposing private material: add bounded decode/incomplete/
  content categories, confirm the protocol difference with a one-shot private structural probe, and add
  16-bit concatenation UDH (`IEI 0x08`) decoding plus regression while retaining 8-bit-only outbound encoding.
  After the fix, retain the correct non-empty baseline stop; export the exactly two newly assembled pending
  records only to an excluded 0600 private TXT for user review, without application persistence, ACK or delete.
- [x] After the user reviewed and approved those exactly two new pending records, execute the fixed exact-two
  cleanup once; it completed with `cleared=2; pending=zero; stage=complete`, then remove the private TXT and
  temporary binary. A following fresh-inbound run reached and flushed ready but received no new message within
  the two-minute window; it stopped at `stage=arrival` with DB-only recovery pending still zero and no app/ACK/delete side effect.
- [x] On the next approved fresh-inbound window, retain the unique arrived `OK` candidate after an arrival-loop stop;
  prove it is the exact 11-digit domestic representation of the one approved `+86` peer, add a narrowly bounded
  comparison/canonicalization regression, persist it once through a build-tag one-shot bridge with no ACK/Delete,
  then remove that bridge and finish through the ordinary replay path. Final result:
  `ready=false; persisted=true; cleared=true; pending-zero=true; stage=complete` with no resend or broader radio action.

- [x] Complete the separately authorized fresh inbound and new outbound confirmation without reusing or mutating the historical unknown operation; retain only sanitized results.
- [x] Confirm no native/container Agent competed, keep RF unchanged, restore service ownership, stop on uncertainty, and remove temporary binaries/input artifacts.

This phase intentionally has no unconditional shell command in the plan; the exact command depends on the approved target and current service ownership and must be restated at execution time.

### Phase B1 — Evidence promotion and production Agent composition

Prerequisite: Phase H passed completely.

- [x] Promote only QDC composite `sms-control` evidence to observed; keep base/default discovery registry non-production.
- [x] Add required private `state-root` composition, fixed v2 state filename, adapter/runtime registry/gate/SMS backend construction and failure cleanup in `cmd/simplus-agent`.
- [x] Expose SMS only through the typed managed hardware handler; make `WriteTimeout >= agentapi.SMSRequestTimeout` and add contract tests.
- [x] Close/checkpoint the store after servers and monitor stop; startup and shutdown failures remain non-zero.
- [x] Update container entrypoint, native dev unit, Debian installer and tests with the fixed state root. Do not add capabilities, network attachment, device rules or writable sysfs.
- [x] Test missing identity key/state root, non-private path, symlink, incompatible schema, backend construction failure, clean shutdown and no enable flag.

Target files/areas:

- `cmd/simplus-agent/main.go`, tests
- `internal/agentapi/server.go`, client/server tests
- `containers/agent-entrypoint.sh`, `scripts/dev/install-agent.sh`, `scripts/release/install-debian.sh`
- `internal/containercontract/contract_test.go`

Focused validation:

```bash
go test ./cmd/simplus-agent ./internal/agentapi ./internal/containercontract
make check-container-files
```

### Phase B2 — Production simplusd composition and public docs

- [x] Update typed Agent policy to require RF, equipment identity and SMS features while rejecting retired generic mutation features.
- [x] Always construct/register Agent native SMS in hardware backend; independently register Host VoWiFi when configured.
- [x] Add composition tests proving simultaneous availability, QDC native selection and ML307A VoWiFi regression.
- [x] Add ADR 0026 and update architecture, active MVP plan, compatibility matrix, development/HIL classification, installation state-root details and sanitized handoff in their canonical owners.
- [x] Compatibility wording names HIL (not Runtime), exact capability, remaining unverified phone/data/other-SIM/operator/long-term boundaries and automated guards; it contains no raw evidence.
- [x] Run the complete non-HIL final gate and inspect the full diff for credentials, phone/SIM/device identity, private paths/topology, raw transcripts/PDU/logs/screenshots and accidental product-scope expansion.

Target files/areas:

- `cmd/simplusd/main.go`, tests
- `docs/decisions/0026-*.md`
- `docs/architecture.md`, `docs/plans/active/mvp.md`, `docs/compatibility.md`
- `docs/troubleshooting.md`, `docs/development.md`, `docs/installation.md`, `docs/handoff.zh-CN.md`

Final validation:

```bash
go test -race ./internal/application/messaging ./internal/hardwareprobe ./internal/modemadapter/...
make check-format
make verify-generated
make check-container-files
make check-docs
make lint
make test
corepack pnpm --dir web build
make web-e2e
make security
git diff --check
```

No final validation command may repeat real SMS/RF/HIL. Phase H evidence is separate.

## 3. Acceptance Mapping

| PRD acceptance | Owning phases |
| --- | --- |
| AC1 identity/operator/subscriber fixtures | A1, A6 |
| AC2 stable modem/Line/privacy/port change | A1, A3, A4 |
| AC3 registration Web status | A2 |
| AC4 explicit RF only | A1, A2, A3 |
| AC5 unique per-Line transport/no fallback | A5, B2 |
| AC6 fenced preflight/stable failures | A2, A3, A5 |
| AC7 outbound encoding/idempotency/unknown | A4 |
| AC8 inbound durability/SIM storage/swap | A4 |
| AC9 production Agent lifecycle | B1 |
| AC10 evidence gate | A6, H, B1, B2 |
| AC11 controlled real HIL | H |
| AC12 repository quality gate | A6, B2 |

## 4. High-Risk Files and Review Points

- `internal/application/messaging/service.go` / `inbound.go`: preserve persist-before-dispatch, persist-before-ACK, per-modem gate and Host VoWiFi submit reports while removing global selection.
- `internal/modemadapter/qdc507sms/*`: no state-key use of transient device ID; no loss of outcome-unknown or PDU index-reuse guards.
- `internal/hardwareprobe/*`: no deadlock when a fenced SMS preflight probes under the shared gate; use a locked internal helper rather than recursively acquiring.
- `internal/agentapi/*`: request/response correlation, bounded fingerprint validation, stable error mapping and log privacy must change on both ends together.
- `internal/application/inventory` / `line` / `messaging`: construct the private runtime target from existing topology evidence without adding fingerprints to public Line/API or logs.
- `cmd/simplus-agent/main.go`: construction must close every partially opened dependency and align HTTP timeout with 130-second Agent request budget.
- `cmd/simplusd/main.go`: hardware Agent policy promotion only after HIL; no Simulator fallback and no VoWiFi override.
- architecture boundary: concrete QDC507 construction is allowed in the Agent composition root and model metadata may be displayed, but application/domain/API/Web control flow must remain replaceable by any adapter implementing the same capability contracts.
- `api/openapi.yaml` + generated outputs: one source, deterministic generation, no private probe fields.
- deployment/docs: state root is private data, Agent remains no-network and unprivileged after entrypoint.

## 5. Rollback Rules

- Before HIL: remove/adjust Stage A code normally; production remains closed.
- After unknown send: never retry the same business message with a new operation ID and never delete its Agent operation state.
- After partial inbound ACK: retain v2 state and let a corrected build reconcile only after PDU revalidation.
- On failed production rollout: remove Agent/simplusd production wiring and return QDC SMS evidence to non-observed, but retain the private v2 database.
- Restore RF/service ownership only for the same fenced target; ambiguity blocks automatic cleanup and requires user direction.
- Never use VoWiFi, QMI WMS, data attachment, broader privileges or an unrelated code repair as an implicit rollback.

## 6. Follow-up Checks Before `task.py start`

- [x] `prd.md` has no unresolved product decision or open question.
- [x] `design.md` fixes data flow, contracts, no-fallback semantics, state namespace, HIL gate and rollback.
- [x] This implementation plan maps every AC and has a hard authorization stop before HIL.
- [x] `implement.jsonl` and `check.jsonl` contain only relevant specs/research/docs and validate.
- [x] Latest planning summary is shown to the user.
- [x] A subsequent user message explicitly approves that exact summary for implementation.
