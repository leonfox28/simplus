# 大疆 QDC507 原生蜂窝短信：技术设计

## 1. Design Goals and Invariants

### 1.1 Problem statement

用最小、可证据化的机制把 QDC507 接入现有 `ManagedModem -> Line -> messaging` 纵切：稳定识别当前模组与 SIM，显式开启 RF 后观察蜂窝注册，通过同一 SIM 的原生 AT PDU transport 收发短信，并保证 QDC507 不会走 Host VoWiFi。

### 1.2 Non-negotiable invariants

1. 只有 `simplus-agent` 的 discovery/registry/adapter/driver 接触 modem endpoint 和型号命令；Web/API、domain 与应用服务只依赖按能力拆分的统一类型化接口，不见 AT/QMI、interface、sysfs 或 `/dev`，也不按 QDC507/型号/VID/PID 做控制分支。
2. Managed Modem 由 IMEI 的每实例 HMAC 稳定绑定，Line 由 Managed Modem + SIM ICCID 的每实例 HMAC + slot 稳定绑定。临时 USB device ID 只用于本次 Agent 定位。
3. RF 只由管理员的显式 Managed Modem mutation 改变；SMS 永不自动开启 RF。
4. 每条 Line 在每次操作只解析一个 SMS transport。零匹配或多匹配均失败；选定后不 fallback。
5. outbound 先落应用库，再 dispatch；modem 接受后结果不明时持久为 outcome unknown，永不自动重发。
6. inbound 先落应用库，再删除 SIM storage；删除前核对 PDU，部分删除进度可跨 Agent restart 恢复。
7. capability evidence 决定可见性。Fixture 不能提升 production `sms-v1`；只有获准 HIL 通过后才启用 observed 能力和生产装配。
8. Agent 仍无网络 attachment；真实蜂窝副作用发生在 modem/SIM，不通过宿主数据网络、netd 或 VoWiFi。

## 2. Architecture and Ownership

```text
Web Modems / Messages
        |
        | stable ManagedModem ID / Line ID
        v
simplusd
  modem.Service ----------> RuntimeStatusReader ------+
  messaging.Service ------> per-Line SMS Resolver     |
                                   |                  |
                 +-----------------+------------------+
                 |                                    |
                 v                                    v
       AgentSMSGateway                        VoWiFiSMSGateway
       (Line capability SMS)                 (HostVoWiFiAuth)
                 |                                    |
                 | expected SIM fingerprint           | existing worker
                 v                                    v
simplus-agent Unix socket                    simplus-netd supervisor
  fenced SMS service
        |
  per-device operation gate
        |
  fresh typed probe + SIM identity fence
        |
  QDC507 composite adapter
        |
  fixed AT PDU driver -> primary AT interface 2 -> SIM / cellular base station
```

The QDC507 path never invokes the right-hand VoWiFi branch. `simplus-netd`, Mihomo, ePDG and SIP are not QDC507 SMS dependencies.

### 2.1 Package ownership

| Concern | Owner |
| --- | --- |
| QDC507 USB match, fixed endpoints, identity/RF commands, evidence | `internal/modemadapter` |
| PDU/SIM-storage command flow and SMS durable recovery | `internal/modemadapter/qdc507sms` |
| device snapshot, fresh probe, endpoint ownership, per-device serialization | `internal/hardwareprobe` |
| typed/fenced Unix request, response and stable errors | `internal/agentapi` |
| stable Line resolution and per-Line transport selection | `internal/application/line`, `internal/application/messaging` |
| Managed Modem cellular projection | `internal/application/modem`, `internal/domain/modem` |
| public contract and responsive display | `api/openapi.yaml`, `internal/api/httpapi`, `web/src/pages/Modems.tsx` |
| process composition, state root, timeout, shutdown | `cmd/simplus-agent`, `cmd/simplusd`, container/native launch files |

## 3. QDC507 Typed Capabilities

### 3.0 Common capability boundary

“统一接口”按能力拆分，不是要求每个模组实现一个巨型接口。上层只依赖它实际需要的最小契约；某型号缺少或尚未验收的能力就不实现、不声明，调用方 fail closed：

| 上层用途 | 上层/Agent 统一契约 | 型号私有实现 |
| --- | --- | --- |
| 稳定模组身份 | `EquipmentIdentityReader` / typed Agent identity request | `EquipmentIdentityAdapter` |
| SIM/Profile 身份 | typed topology and Line identity | `SIMIdentityAdapter` |
| 本机号码 best effort 观测 | scanner/HIL-only `SubscriberNumberAdapter` | fixed `AT+CNUM` parser |
| 蜂窝运行状态 | `RuntimeStatusReader` returning typed `RuntimeStatus` | `ATProbeAdapter` with a fixed private `ProbePlan` |
| RF 开关 | `RFController.Set(..., bool)` / typed Agent RF request | `RFControlAdapter` |
| 短信收发 | application `Sender`/`Inbox`, typed Agent SMS requests | `SMSAdapter`, then model-private `PDUDriver` |

QDC507 的 AT 文本、响应解析、endpoint role 和 SIM storage 选择只存在于最后一列及通用有界 tty transport。Registry 依据 discovery profile 选择实现；选择完成后，Agent 服务、Line、messaging、Managed Modem、HTTP/OpenAPI 和 Web 均不再判断型号。Composition root 可以显式构造并注册具体 adapter，展示层可以显示只读型号名称，但二者都不能把型号作为业务控制条件。

架构验收使用类型断言、registry/router 测试和一个合成的第二型号 adapter fixture，证明同能力实现可替换而无需修改上层业务流；不为测试增加第二个生产型号或未来空接口。

### 3.1 Stable equipment and SIM identity

`modemadapter.QDC507` gains the three existing small interfaces rather than a QDC-specific upper-layer branch:

- `EquipmentIdentityAdapter.ReadEquipmentIdentity`: fixed `AT+CGSN`, accept only a Luhn-valid 15-digit IMEI via the shared `equipmentIMEI` helper.
- `SIMIdentityAdapter.ReadSIMIdentity`: fixed `AT+QCCID`, parse `+QCCID:` with the shared ICCID pseudonymizer.
- Home operator metadata: reuse the existing SIM-file decoders and EF_AD MNC-length logic with neutral names. Read EF_SPN best effort, read IMSI only inside the adapter, and return only normalized `MCC-MNC`. Raw IMSI is discarded before leaving the function. A Chinese Unicom fixture is data, not a branch or fallback constant.

Identity and metadata stay independent: a valid ICCID always yields the stable fingerprint/hint even when SPN/IMSI/EF_AD metadata is absent or malformed.

### 3.1.1 Private best-effort subscriber number

`SubscriberNumberAdapter.ReadSubscriberNumber` 是 model-neutral 的最小只读能力，只能由 scanner 与 build-tag HIL runtime 调用；QDC507 实现固定执行 `AT+CNUM`，不开放任意 AT 或 endpoint。Parser 只接受一个 `+CNUM` record、TON 145 且 number 字段本身已经是唯一规范的 `+E.164`；空响应正常返回 unavailable，重复（即使相同）、歧义、national format、缺少 `+`、malformed/control/overflow、terminal/query failure 全部 unavailable，不通过 TON、运营商或外部 destination 猜测补全。

HIL 只能在唯一 target、SIM identity/generation fence、RF On、蜂窝注册成功并再次 fence 后调用。返回值只在当前进程内瞬时存在，不作为 SMS readiness、注册成功、Line identity 或 inbound sender 的条件；unavailable 不阻断后续 `SM` preflight 与 SMS。该值不进入 Agent/public protocol、OpenAPI/Web、数据库、日志、错误、stdout/stderr 或 task/HIL evidence；typed `RunResult` 与 CLI 只携带去标识的 available/unavailable 状态，即使后续 SMS 阶段失败也可保留该能力结论。获批 external destination 仍是唯一预期 inbound sender。

### 3.2 RF control

`QDC507.SetRFState` follows the existing typed RF contract:

1. bounded `AT` handshake;
2. fixed `AT+CFUN=1` for On or `AT+CFUN=4` for Off;
3. immediate `AT+CFUN?` read-back;
4. success only when the typed observation equals the requested state.

The Agent RF service already fences instance/snapshot/device generation and checks active-call count before the adapter write. No `AT&W`, boot policy, network selection or retry loop is added. The existing inconsistent `rf-control: observed` evidence is corrected so the evidence and actual interface move together; a model with no usable AT endpoint reports unavailable.

### 3.3 Capability shape

- Base/discovery `QDC507{}` keeps `sms-control` non-production (`documented`) until HIL.
- The final production composite adapter implements the same discovery/identity/RF interfaces plus `SMSAdapter`, and only after the evidence gate replaces `sms-control` with `observed` when the primary AT endpoint exists.
- `host-vowifi-auth`, cellular voice and digital voice media remain unverified/unsupported as today and are not exposed as business capabilities.
- `operator-selection` is not promoted by observing registration; reading `COPS?` is not permission to scan or select networks.

## 4. Cellular Status Contract

### 4.1 Shared classifier

Add one pure classifier next to the typed Agent cellular observations. It accepts `DeviceProbe` and returns:

- `state`: `registered-home`, `registered-roaming`, `searching`, `denied`, `not-registered`, `rf-off`, `sim-not-ready`, `unavailable`, or `unknown`;
- `readyForSMS`: boolean;
- `reasonCode`: empty when ready, otherwise a transport-neutral bounded code.

Precedence is deterministic:

1. incomplete/invalid probe -> `unavailable / CELLULAR_STATUS_UNAVAILABLE`;
2. SIM not present+ready -> `sim-not-ready / CELLULAR_SIM_NOT_READY`;
3. RF not On -> `rf-off / CELLULAR_RF_OFF`;
4. any registered-home/SMS-home/CSFB-home domain -> `registered-home`;
5. any registered-roaming/SMS-roaming/CSFB-roaming domain -> `registered-roaming`;
6. otherwise denied, searching, not-registered, emergency-only, then unknown.

`readyForSMS` is true only for steps 4–5. Denied maps to `CELLULAR_REGISTRATION_DENIED`; all non-registered/searching/emergency/unknown terminal cases map to `CELLULAR_NOT_REGISTERED` or `CELLULAR_STATUS_UNAVAILABLE` as appropriate. Stable identities are separate fences layered on top: the public Managed Modem status does not expose them, while Agent SMS requires the current equipment and SIM fingerprints to equal the Line target. The Agent SMS preflight maps the neutral reason to `SMS_*` errors; Managed Modem presentation exposes `CELLULAR_*`. Neither reimplements the state table.

### 4.2 Application model

Replace the RF-only read port with a `RuntimeStatusReader` that returns one `RuntimeStatus` from one targeted Agent probe:

```text
RuntimeStatus
  RFState
  SIMPresence
  CellularState
  CellularErrorCode
  Registrations[3] { domain, state }
  OperatorName
  OperatorCode       # normalized current PLMN, e.g. 460-01; empty if unknown
  RAT
  SignalState
  SignalRSSIDBm      # optional
  ObservedAt
```

`AgentRFController` is split by responsibility: the status reader owns the current probe; a narrow RF setter owns only the fenced boolean mutation. `modem.Service.List` calls the status reader once for each online managed modem, so this adds no second probe beyond the existing RF read. Offline/unreadable status is explicitly unavailable and never reuses an older registered observation. `SetRFState` returns the write result, then the ordinary list invalidation obtains fresh full status.

### 4.3 Public OpenAPI/UI

`ManagedModem` gains a required nested `cellular` object. To keep the OpenAPI 3.0 generator contract simple, absent observations use required sentinel fields rather than nullable values:

- `state`, `errorCode`;
- registration entries ordered `cs`, `packet`, `eps`, using the Agent registration enum; a missing domain is normalized to `unknown` so the public object always has three entries;
- `operatorName`, normalized `operatorCode`, `rat`;
- `signalState`, integer `signalRssiDbm` (`0` unless `signalState=measured`);
- string `observedAt` (RFC3339 when fresh, empty when unavailable).

OpenAPI is the source; generated Go/TypeScript/Zod/Query files are regenerated. The Web uses generated unions and exhaustive presentation maps. Both desktop table and compact card show the cellular summary, current network/RAT, signal and observation age/error context. No raw identity, endpoint, functional-mode string, probe detail or device path is public.

## 5. Agent Operation and SIM Fencing

### 5.1 One per-device operation owner

Replace `hardwareprobe.Scanner.controlMu` and `modemadapter.SMSRouter`'s private gates with one injected, context-aware keyed gate. Scanner probe, equipment identity, SIM AKA, RF and SMS acquire it by current Agent device ID. Each method:

1. validates the target and resolves the current device before allocating/acquiring;
2. acquires with context cancellation;
3. re-resolves the current snapshot after waiting; if the device disappeared or its generation changed, return the typed stale/unavailable error;
4. performs the complete endpoint session and state persistence while holding it;
5. releases on every return.

Different devices may proceed independently; all operations on one modem are serialized. The tty `flock` remains a defensive cross-process check, not the primary scheduler. Tests cover same-device serialization, different-device progress, cancellation and hotplug while queued.

### 5.2 Internal SMS request fence

The public browser request still contains only a stable Line ID. `line.Service.Topology` already carries the resolved Line's `SubscriptionProfileID`, the current internal subscription profiles and the physical device's equipment fingerprint. Messaging derives a private `SMSRuntimeLine` by requiring exactly one matching active profile and valid equipment/SIM fingerprints; it never parses a display hint and does not add either fingerprint to `inventory.Line`, OpenAPI, public response models or logs.

The Agent SMS request structs add `deviceGeneration`, `expectedEquipmentFingerprint` and `expectedSubscriptionFingerprint` for List/Read/Send/ACK. These are internal Unix protocol fields with strict bounds. Server logs must not emit either fingerprint. The SMS backend, while holding the device gate:

1. verifies Agent instance, current device presence and exact device generation; ordinary SMS requests do not pin the whole snapshot revision, so unrelated hotplug on another modem cannot invalidate a long send;
2. performs a fresh typed probe;
3. requires valid current equipment and SIM fingerprints and SIM READY;
4. compares both in constant time with the expected Line target;
5. applies the shared cellular readiness check for Send only and maps its neutral reason to a stable SMS error;
6. constructs `SMSRuntimeTarget{DeviceReport, SubscriptionKey}` for the adapter.

This closes modem hotplug/replacement and SIM-swap TOCTOU between application Line resolution and modem dispatch. A change during payload dispatch is handled by the existing definite-before-dispatch versus outcome-unknown-after-dispatch rules.

### 5.3 Stable Agent errors

Add sentinels, HTTP error codes, `ErrorResponse.Is` mappings and gateway mappings for:

| Agent condition | Agent code | Durable application code |
| --- | --- | --- |
| SIM absent/locked/identity missing | `SMS_SIM_NOT_READY` | `SMS_SIM_NOT_READY` |
| SIM differs from Line binding | `SMS_SIM_IDENTITY_CHANGED` | `SMS_SIM_IDENTITY_CHANGED` |
| RF not On | `SMS_RF_OFF` | `SMS_RF_OFF` |
| registration denied | `SMS_REGISTRATION_DENIED` | `SMS_REGISTRATION_DENIED` |
| no registered domain/searching | `SMS_NOT_REGISTERED` | `SMS_NOT_REGISTERED` |
| probe/control unavailable | `SMS_STATUS_UNAVAILABLE` | `SMS_STATUS_UNAVAILABLE` |
| payload may already have dispatched | `SMS_SEND_OUTCOME_UNKNOWN` | existing `SMS_SEND_OUTCOME_UNKNOWN` |

Definite preflight failures produce the existing durable `failed` message status. Only dispatch uncertainty produces `unconfirmed`. Error strings remain locale-neutral and omit identifiers, addresses, bodies and raw modem responses.

## 6. QDC507 SMS Runtime and Durable State

### 6.1 Adapter contract

Change `SMSAdapter` to receive `SMSRuntimeTarget` rather than a bare `DeviceReport`:

```text
SMSRuntimeTarget
  Device             # current transient endpoint map
  SubscriptionKey    # validated per-install SIM fingerprint, Agent-internal
```

The wire response still sets `DeviceID` to `target.Device.ID` for request/response correlation. All state keys and hashes use `SubscriptionKey`.

### 6.2 SIM-owned message storage

Every List/Read/Send/Delete preparation executes fixed commands and validates terminal responses:

1. `AT+CMGF=0`;
2. `AT+CPMS="SM","SM","SM"`.

`SM` binds read/delete, write/send and future receive storage to the physical (U)SIM. It is an inbound staging area, not the product's only or long-term message store: after the complete message is durably committed to the application database, Agent ACK re-reads and verifies each PDU and removes the safely adopted segments from `SM`. Outbound `AT+CMGS` is sent directly and does not rely on a persistent sent-message copy on the SIM. The Agent owns this runtime SMS mode; it does not restore it after each call and never issues `AT&W`. HIL must prove QDC507 accepts the exact selection and stores/reads the approved inbound message there. If `SM` is unavailable, the operation fails; it does not use `ME`/`MT` as fallback.

### 6.3 State schema v2

There are two distinct local persistence layers. The application message database is the authoritative user-visible history and stores the complete inbound/outbound message records. The Agent-private QDC database is a recovery ledger: it retains the minimum message candidate and operation/delete progress needed to survive restart, deduplicate delivery and avoid unsafe resend or deletion. Selecting `SM` does not eliminate either local database.

The candidate v1 database was never production and cannot be safely attributed from transient device IDs to SIMs. The final production path uses a fixed v2 filename under the configured Agent state root (for example `qdc507-sms-v2.sqlite3`) and `PRAGMA user_version = 2`. It does not silently migrate or delete an existing v1 file.

Schema changes:

- replace `device_id` state namespace with `subscription_key` (64-hex check);
- make inbound uniqueness `(subscription_key, message_id)`;
- include `subscription_key` in operation records as an explicit checked column as well as the request digest;
- keep message bodies/senders only because they are required for restart-stable list/read; the private database is mode 0600 in a mode 0700 Agent directory;
- preserve `synchronous=FULL`, one connection, WAL and checkpoint-on-close.

Message/digest rules:

- inbound message ID = hash(subscription key, ordered storage indexes, exact PDU bytes);
- send digest = hash(subscription key, destination, body);
- ACK digest = hash(subscription key, message ID);
- hotplug to another USB port returns the same state; another SIM cannot see or acknowledge it.

Operations accepted before a crash remain outcome unknown. No cleanup job deletes unresolved operation evidence automatically.

### 6.4 Application inbound ordering

The existing application ordering remains authoritative:

```text
Agent List -> Agent durable candidate -> application message/fragment transaction
          -> Agent Read -> validate -> application commit -> Agent ACK
          -> modem re-read + PDU digest match -> CMGD -> Agent progress commit
```

For multipart, the QDC adapter exposes a complete uniquely assembled message; its Agent ACK owns all physical segments and persists each successful delete. The existing application fragment spool still supports other transports and is not removed.

## 7. Per-Line SMS Transport Resolution

### 7.1 Resolver contract

Replace the global `sender/inbox + hostVoWiFiSMS` switch with registered transport bundles:

```text
SMSTransport
  Name
  Eligible(line) bool
  Available(ctx, line) bool
  Sender
  Inbox
  optional SubmitReportInbox
```

Registered hardware bundles are:

- `agent-native`: eligible iff `line.Capabilities.SMS`;
- `host-vowifi`: eligible iff `line.Capabilities.HostVoWiFiAuth`, available iff the existing per-Line VoWiFi service is available.

Each bundle has both `Sender` and `Inbox`; Host VoWiFi additionally implements submit reports. Registration rejects incomplete bundles at startup, so a transport cannot be selected for Send but silently disappear from inbound sync.

Resolution is intentionally two-step: first compute eligible bundles from Line capabilities and require exactly one; zero means unsupported and more than one means ambiguous. Only then check the selected bundle's availability. A selected-but-unavailable bundle returns a stable unavailable error and never asks another bundle to handle the operation. Current QDC507 and ML307A capabilities are mutually exclusive, so no user-facing selector is needed in this MVP.

### 7.2 Send and inbound consistency

`Send` resolves once before creating the queued record so unsupported/unavailable Lines do not create misleading dispatch work; it then persists before calling the selected sender. `SyncInbound` resolves each ready Line independently and invokes the inbox/reports from that same bundle under the existing modem-function gate. A failure on one Line is collected and does not prevent other Lines from synchronizing; it still never tries a second transport for the failed Line. Tests use per-Line call counters to prove both isolation and no fallback.

Simulator registers one synthetic Agent bundle and keeps current behavior. Hardware always registers the Agent bundle after the typed Agent policy passes, and registers the VoWiFi bundle only when its supervisor exists.

## 8. Production Composition and Lifecycle

### 8.1 Agent startup

Add one required private `--state-root` (with fixed environment/default paths in native and container launchers), not an enable flag. The composition root:

1. opens the existing identity key;
2. opens QDC507 v2 SQLite state beneath the validated root;
3. constructs tty transport -> PDU driver -> composite QDC adapter;
4. constructs a runtime registry with that adapter and `ML307A{}`;
5. injects the same registry and keyed operation gate into scanner and SMS backend;
6. refreshes monitor;
7. constructs RF, identity and SMS services;
8. exposes the managed handler with `sms-v1` and uses `WriteTimeout >= SMSRequestTimeout`;
9. on shutdown, stops HTTP/monitor, closes listeners, then checkpoints/closes SMS state.

Any failure before step 8 exits non-zero. `DefaultRegistry()` remains discovery/test-safe and continues to supply reviewed USB driver IDs; no duplicate QDC match is registered.

### 8.2 simplusd startup

`requireTypedHardwareAgent` is updated atomically after HIL to require `rf-control-v1`, equipment identity read and `sms-v1`, while continuing to reject retired generic command features. It constructs `AgentSMSGateway` even when a VoWiFi supervisor exists, then registers both bundles with messaging. If the Agent does not advertise the required typed feature, hardware startup fails rather than silently losing native SMS.

Native development unit, Debian installer and container entrypoint all pass the fixed Agent state root. Existing private mounts/StateDirectory and Agent no-network/capability boundaries are sufficient; no new device cgroup rule, host network, capability or writable sysfs path is added.

## 9. Evidence Promotion and HIL

### 9.1 Two-stage activation

Production activation is deliberately split:

**Stage A — fixture-ready, production closed**

- implement typed identity/RF/status/SMS/gating/routing and deterministic tests;
- keep base QDC SMS evidence non-observed and ordinary production product path unable to use it;
- add a `qdc507_hil` build-tag test/runner that composes the same scanner, gate, target resolver, driver and durable adapter. It selects exactly one QDC507 from discovery, accepts no device path, AT/QMI text or caller marker, and reads only the private destination from bounded stdin JSON; argv contains only bounded timeouts. The runner generates a 128-bit `crypto/rand` uppercase-hex GSM-7 marker in memory and stops if entropy fails. Its typed completion result reports only subscriber-number available/unavailable, never the value.

**Stage B — after exact approval and successful HIL**

- run the bounded HIL sequence;
- add only a sanitized compatibility conclusion;
- promote `sms-control` to observed in the production composite;
- inject the shared Agent SMS handler and update `simplusd` typed policy/composition;
- run the complete non-HIL gate again.

If HIL fails, Stage B is not applied. The failure does not authorize QMI WMS, VoWiFi, broader commands, data attachment or permission expansion.

### 9.2 Bounded HIL sequence

Before execution, restate and obtain approval for the exact Line/number, synthetic body, number of sends, timeout, service ownership switch and RF restoration. The runner then:

1. proves one unique QDC507, current generation and SIM READY/fingerprint using typed read-only inspection;
2. records only in memory the initial RF state; refuses ambiguity or an unexpected active call;
3. explicitly requests RF On if needed and verifies read-back;
4. polls fixed read-only status until registered or the approved deadline, then rechecks the target/SIM fence;
5. attempts the typed fixed `CNUM` subscriber-number read best effort, discards the result without output/persistence, and continues when unavailable;
6. verifies `CPMS` selection through the real adapter;
7. performs exactly the approved outbound operation with one operation ID and the internally generated marker; unknown outcome stops all sends;
8. waits for the same private destination to echo the in-memory marker, imports exactly that inbound message, and verifies persist-before-delete and empty pending state;
9. restores initial RF and service ownership; cleanup failure is reported and blocks acceptance.

Raw output, identifiers, phone number, body, PDU, timestamps tied to the run, database, logs and screenshots stay outside the repository. Public docs record only capability, HIL evidence level, remaining boundaries and the automated regression that protects it.

### 9.3 Outcome-unknown inbound recovery

The observed outbound was physically delivered while the protocol result stayed
unknown; the reply arrived only after the original runner stopped, and RF cleanup
was blocked by two active data calls. This remains a partial failure, not HIL pass.
No outbound retry, hangup, RF force-off, phone/data control, or production promotion
is permitted.

The most likely bounded protocol mismatch is submit payload echo: after Ctrl-Z the
modem may echo the exact hexadecimal PDU before `+CMGS` and `OK`, while the original
transport filtered only the `AT+CMGS` command echo. The fixed transport may suppress
at most one line that is byte-for-byte the payload just written (with or without the
terminal Ctrl-Z); a different hexadecimal line, second echo, or URC remains visible
and makes the driver fail closed. Synthetic tty and complete Driver tests lock this
behavior, but the historical unknown operation remains immutable and this diagnosis
is not HIL evidence until a separately authorized new outbound succeeds.

That follow-up uses a distinct build-tag-only outbound-confirmation binary rather than the complete
echo-back runner. It accepts only the private destination through bounded stdin and generates a new
marker and operation ID from `crypto/rand`; it sends at most once and does not request an inbound reply.
Its concrete graph contains only `QDC507Outbound` read-only registration/equipment/QCCID observation,
the shared per-device gate, `SMSOutboundRouter`, outbound-only adapter/driver/tty transport, an
operation-only existing-v2 recovery SQLite store, and an application repository that inserts/transitions
only its new outbound row. It contains no Line/topology service, RF mutation, inbound List/Read/ACK/Delete,
subscriber-number, call/data/QMI/SIM-AKA or generic command capability. Preflight requires the historical
unknown app/ledger evidence unchanged, no prior confirmation-attempt row, no pending inbound state, RF
already On, SIM ready and no voice/fax/unknown call. Existing CLCC mode-1 data bearers may coexist
without being created, changed, exposed, or torn down by SMS. Target selection may occur while registration is still
searching so the bounded registration wait has effect; the typed dispatch performs another locked ready,
identity and generation fence immediately before sending. Definite pre-prompt failure marks only the new
row failed; any post-prompt ambiguity marks the new operation/row outcome-unknown and stops without retry.
The old unknown operation is never resent or rewritten. The separately authorized execution completed
with durable application/Agent success and modem confirmation; only the sanitized conclusion is retained.

The tagged recovery command is a separate binary with no stdin payload and only
bounded timeout argv. Its composition retains a discovery-only scanner, an
equipment/SIM-identity-only resolver, a receive-only registry/router,
`PDUInboxDriver{List,Read,Delete}`, a command-only TTY transport with no Prompt,
application message repository, and a metadata-only unknown-send inspector. It
constructs neither the complete QDC/ML307A adapters nor the standard RF/registration
probe. Neither the runner interface nor its concrete object graph contains Send,
RF, registration, call, data, or subscriber-number capability. It requires the complete private application database to
contain exactly one outbound `unconfirmed/SMS_SEND_OUTCOME_UNKNOWN` candidate and
at most its exact persisted reply. Destination and marker are derived from that
row, validated as bounded E.164 and the exact generated GSM-7 marker grammar, and
cross-checked by operation ID plus current fenced subscription against exactly one
outcome-unknown recovery operation; raw digest, address, body and fingerprint are
never returned.

The runner takes/restores only fixed service ownership, selects one current QDC507,
requires SIM ready and exact equipment/SIM/generation fence, and does not inspect
registration or RF. It lists fixed `SM`, requires every raw PDU to belong to the
unique durable pending message set, fails on multiple, incomplete, or unexpected inbound,
persists the unique exact reply to the application database, rechecks the fence,
then invokes existing PDU-revalidating ACK/delete semantics and verifies pending is
empty. Restart/replay may finish an already persisted reply, but never mutates or
removes the original outbound unconfirmed row.

### 9.4 Read-only multiple-candidate classifier

Recovery 在 `SM` 观察到多条候选并停止后，候选分类使用第三个 build-tag 隔离二进制，不能
通过缩窄接口复用完整 recovery 对象。它的 concrete graph 只含 discovery、设备/SIM identity
resolver、generation fence、固定 service ownership、真正 `mode=ro` 的 application messages
与 Agent recovery SQLite handles，以及一个只有 unexported fixed transcript exchange 的
EF_SMS reader。它固定执行 `CRSM` GET RESPONSE 与 READ RECORD，file ID 是 decimal `28476`
(`6F3C`)，每次显式 path `7F106F3C`，READ RECORD 使用 1-based record、absolute P2=4、
P3=176；不构造 application `Set`、`SQLiteStateStore`、SMS adapter/inbox backend、generic
`Command`/`Prompt`/APDU transport，因而不存在 caller command/path、`CMGF/CPMS/CMGL/CMGR`、
`CSIM/UPDATE`、Create/write、Send、single Read、ACK/Delete、RF、注册、电话、数据或
subscriber-number capability。只读 SQLite 不运行 schema/migration、写 PRAGMA 或 checkpoint，
关闭只释放 handles。

GET RESPONSE 只接受一条 `+CRSM: 144,0,"..."` 与最终 `OK`，且 payload 必须是确证
linear-fixed EF_SMS、record length 176、显式 bounded count 的 documented legacy 15-byte
shape 或完整 UICC FCP；不猜 count，不接受 continuation/non-`90 00` status、retry、alternate
path 或 fallback。每条 READ 必须恰好返回 176 bytes。status byte `00` 是 free 并忽略，`01`
received-read 与 `03` received-unread 在剥离 status 后进入既有 bounded PDU/multipart assembler；
其他 used/outgoing/status-report/unrecognized status 一律使完整性 unexpected，不能被 exact match
掩盖。

入口从 application DB 的单一 read-only snapshot 唯一推导既有 outbound
`unconfirmed/SMS_SEND_OUTCOME_UNKNOWN` source + marker，再在当前 SIM fence 下要求 recovery
ledger 恰有一个 outcome-unknown send 且 operation ID/request digest 精确匹配。设备 generation、
equipment 与 SIM fence 在扫描前、中、后都重查并与整个双扫描共用同一 operation gate。完整
EF 扫描两次，只有 count 与所有 record bytes byte-for-byte 相同才继续分类；网络到达导致的
record 变化、overflow、cancel、malformed/incomplete 或 identity 变化全部 non-actionable。
所有 received PDU 在内存中解析和 multipart 组装；
任意 outgoing/unexpected、malformed、重复、超时跨度或 incomplete group 都阻止 exact-match 结论，
不能因为另一个完整候选匹配就忽略。输出只有候选/匹配 `zero|one|multiple|unknown` 与完整性
`complete|incomplete|malformed|unexpected|unknown`，不含地址、marker、正文、PDU、时间、index、
路径或身份。多条中恰一条匹配也只报告，不落库、不 ACK/delete；执行后在任何接管动作前形成
新的精确授权硬停点。该 classifier 的 CRSM parser、双扫描、fence 与 mode=ro SQLite 对象图
已实现和完成合成测试；标准证明 READ RECORD 与 UPDATE 分离，但没有 QDC507 firmware-specific
证据证明 response shape、modem cache 行为或 unread byte 保持。因此真实构造以明确的 CRSM
firmware blocker 保持 fail closed；必须先为当前固件取得新的精确授权、验证 shape 与 unread
preservation 并重新通过质量门，才能提出一次真实分类执行授权。该验证必须使用另行获批、
为本次验证新建的受控 synthetic unread fixture；现有私有 inbox 候选不能隐式充当 preservation
fixture。

为收敛这一 blocker，另有独立 `simplus-qdc507-hil-crsm-preserve` build-tag entrypoint。其
concrete graph 不含上述两个 read-only DB handles 或任何 candidate correlation，只含
discovery-only scanner、QDC507Inbox identity/SIM-ready resolver、per-device gate、fixed service
ownership 与同一 fixed CRSM enumerator。总 deadline 从 runtime 构造前开始；stdin 只允许 PTY
或 `/dev/null`，argv 只有 bounded arrival/total timeout。它在同一 gate 中以每轮前后 fresh
equipment/SIM/generation fence 完成两次 byte-identical zero-unread baseline，flush
`ready-for-one-controlled-inbound` 后等待用户从未来获批私有 peer 手工发送 exactly one 新消息。
只接受唯一 `0x00 -> 0x03` 且 payload 非空的 record transition，其他 records 必须与 baseline
逐 byte 相同；随后三次 interval-separated full scans 要求新 record 全部 176 bytes 保持一致且
status 始终 unread。任何 existing unread、read arrival、非唯一变化、count/shape/byte/fence
变化、cancel 或 timeout 都 fail closed。raw EF 只在内存，不 log/persist/hash output；成功输出
只有 response-shape/unread-preserved observed 与 changes=one。该实现为 Fixture、从未运行；
首次 firmware-specific read 理论上仍可能改变 unread，因此未来授权必须精确接受风险、创建全新
controlled fixture、限制一次执行且 no retry。成功后仍需 code/spec review 才能打开 classifier，
不授权真实分类、adoption、ACK/delete 或 production promotion。

### 9.5 Reviewed candidate cleanup

用户在仓库外私有记录中逐条审阅并批准清空两条 pending assembled recovery candidate 后，删除入口
使用第五个独立 `qdc507_hil` tagged binary `simplus-qdc507-hil-clear-reviewed`。它不读取私有 TXT；
reviewed count `2`、Agent identity key、recovery DB 和 service owner 都是 compiled-in 固定值。CLI
无私密 stdin/argv，只有 bounded total timeout；结果固定为
`reviewed=2; cleared=0|1|2; pending=unknown|zero; stage=input|service|state|target|revalidate|delete|commit|verify|cleanup|complete`。
`stage` 只表示去标识的有界状态机阶段；成功必须是 `complete`，close 或 owner restore 失败必须是
`cleanup`，不得输出底层错误、路径、身份或记录事实。

State machine 必须先取得 fixed Agent service ownership，随后才打开 identity key/DB，并在所有退出
路径先关闭 cleanup store、再无条件恢复原 owner。专用 store 先以 `mode=ro` 核验 main/WAL/SHM/
journal 的 private mode、owner、link count 与 identity，再核验 `user_version=2`、完整 schema manifest、
WAL、FULL synchronization 和 integrity；合法 existing main DB 即使当前没有 WAL/SHM，也只允许 SQLite
在固定辅助路径创建经 `O_NOFOLLOW`、同 UID、regular、single-link、0600 与 inode revalidation 的 WAL/SHM，
main inode 必须不变，journal/new main/symlink/hardlink 仍拒绝。artifact 未变化后才以 `mode=rw` 打开。其 interface 只有 load exactly two current-SIM
pending inbound metadata、CAS persist delete-started then deleted for one segment、atomically mark both records acknowledged、DB-only
final verify 与 close。SQL 不更新 operations、outbound ledger、sender 或 body；任一 count!=2、already
acknowledged、malformed row/segment、subscription mismatch 或 CAS/commit failure 都 fail closed。

Concrete modem graph 只有 QDC507Inbox discovery/CPIN/equipment/QCCID identity resolver、一个覆盖完整
cleanup 的 device gate，以及专用 `ReviewedCleanupDriver{Read,Delete}`。Driver transport 接收 typed
operation/index，不能接收 command/path；只生成 exact `AT+CMGR=<stored index>` 和无 flag 的
`AT+CMGD=<same index>`，严格解析结果，不 list/prepare/storage-select/reconcile/retry。每个未完成 segment
顺序为 fresh fence -> CMGR -> constant-time digest+index validation -> durable delete-started CAS -> fresh fence -> CMGD -> post fence ->
durable deleted CAS；delete-started 遗留表示物理结果不确定，restart 必须在再次读取或删除该 index 前停止。
两条 record 全部 segment deleted 后再 fresh fence，并在一个事务中同时 acknowledged。`CMGR` 可能在删除
前把 `REC UNREAD` 改为 `REC READ`，该风险是这两条 reviewed deletion 授权的一部分。delete response
丢失、identity/generation/SIM/index/digest 变化或 DB update 失败立即停止；已经 commit 的 segment/record
进度保持单调，但 runner 不自动 retry uncertainty。两条 record 完成后只查询 DB 确认两者 acknowledged
且同 subscription 无 pending，不再执行 CMGL/CRSM/全 SIM list。

该入口和合成测试已经实现；后续 WAL auxiliary 与 bounded-stage 诊断修复只运行合成/编译检查，未再次
执行真实 cleanup、打开真实私有 DB、停止服务或接触 modem/SIM。production/default registry 和普通
binary 继续 closed；实现与当前精确授权不构成 HIL pass、production promotion 或更宽删除权。

### 9.6 Fresh normal inbound E2E

Reviewed cleanup 完成后，正常入站证据不再复用旧 marker-bound recovery candidate，而使用第六个独立
`qdc507_hil` tagged binary `simplus-qdc507-hil-fresh-inbound`。State machine 先取得固定 service
ownership，随后才打开 fixed identity key、existing Agent v2 recovery DB 与 application messages DB；
close state 和恢复原 owner 在所有已取得 ownership 的退出路径无条件执行。CLI stdin 只允许 PTY 或
`/dev/null`，argv 只有 bounded `--arrival-timeout`/`--total-timeout`，新正文 `OK` compiled-in。

应用侧以一个 `mode=ro` transaction snapshot 唯一选择原 outbound
`unconfirmed/SMS_SEND_OUTCOME_UNKNOWN`，只把其 bounded E.164 remote address、stable Line、operation
ID 与旧 body 用于 peer/subscription ledger correlation；新入站不匹配旧 body。Narrow application store
只有 LoadPeer、idempotent CreateInbound、exact replay/final read verification 和 Close，没有 outbound
mutation/delete。当前 SIM v2 operations ledger 通过独立 `mode=ro` handle 捕获唯一 unknown-send 完整行，
核对 exact operation ID/request digest，并在最终逐字段复核未改变。Concrete modem graph 只含 QDC507Inbox identity/SIM-ready resolver、
generation fence、一个 operation gate、无 Prompt 的 InboxTransport、`List/Read/Acknowledge` adapter 与
durable recovery store；不含 Send/RF/registration/call/data/subscriber capability。

Fresh path 先要求 current subscription recovery pending zero，随后 strict fixed-`SM` List 必须返回 zero
且不能隐藏 incomplete/malformed/unexpected PDU，再次确认 recovery zero 与 fresh equipment/SIM/generation
fence 后同步 flush `ready-for-one-controlled-inbound; stage=ready`。之后只接受 expected peer 的 exactly
one complete candidate，Read body 必须 exact `OK`；multiple/unexpected/incomplete/malformed/fence change
全部 fail closed。应用 inbound/provider-id 与 unread marker 在一个 transaction 中先持久化，再 fresh fence
后 ACK；既有 adapter 对每段 stored index 做 CMGR PDU digest revalidation、single-index CMGD 与 progress
commit。Final verify 要求同 provider-id 应用行恰好一次、原 outbound byte-for-field unchanged、unknown
ledger still exact、recovery pending zero 且 strict `SM` List zero。Restart 仅允许启动 snapshot 已有唯一
peer+`OK` exact app row、同一 SIM 恰好一个 candidate 的每个未删 segment 仍物理存在且 ACK operation 从未
开始时续作；fresh application replay、ledger-only segment 或 accepted/uncertain ACK 一律停止，不再输出
ready、请求第二条或重复删除。CLI 结果只有
`ready/persisted/cleared/pending-zero` 与 bounded stage。该实现为 implemented-not-run Fixture；合成测试、
compile target 和文档更新不构成 HIL execution/pass 或 production promotion。

## 10. Compatibility, Documentation, and Migration

- Add ADR 0026 because production Agent mutation surface, native/VoWiFi transport selection and QDC evidence promotion are durable architecture decisions. It supersedes only the prior statements that production hardware Agent is SMS-free; it preserves typed boundaries and no-fallback ADRs.
- Update `docs/architecture.md`, the sole active MVP plan, `docs/compatibility.md`, `docs/troubleshooting.md`, `docs/development.md`, installation/entrypoint descriptions and sanitized handoff after Stage B.
- No core/messages database migration is required. Stable Managed Modem/Line schemas and current internal topology already hold the identity evidence required to construct the private messaging target.
- The QDC SMS state database is Agent-private v2. Candidate v1 state is neither imported nor deleted automatically; an unexpected old file blocks only if configured as the production v2 target.
- Existing ML307A Host VoWiFi behavior, Simulator SMS, message history/cursors and notification flows remain compatible.

## 11. Rollback and Failure Semantics

Code rollback removes production registry/handler/app composition and returns QDC SMS evidence to non-observed. It must not delete the v2 Agent state database: accepted/outcome-unknown and partially acknowledged evidence may be needed when the fixed version returns.

Operational rollback after HIL or startup failure:

- stop the candidate owner before restoring the previous Agent service;
- restore the recorded RF state if and only if the target identity/generation still matches;
- do not resend an unknown outbound operation;
- do not delete a modem message whose current PDU no longer matches the persisted digest;
- keep Host VoWiFi behavior independent; never use it to mask a QDC failure.

## 12. Principal Risks and Mitigations

| Risk | Mitigation |
| --- | --- |
| QDC firmware differs from EC25 documentation | transcript fixture + exact typed HIL; no capability promotion on mismatch |
| modem or SIM swapped between Line resolution and dispatch | device generation + expected equipment/SIM fingerprints + fresh locked probe + constant-time comparison |
| USB port change breaks replay state | durable namespace uses SIM fingerprint; wire device ID remains transient only |
| modem `ME` storage leaks old-SIM messages | require `CPMS=SM,SM,SM`; no fallback storage |
| partial send duplicates on retry | persist accepted before dispatch; partial/post-prompt failures become outcome unknown |
| partial ACK deletes a reused index | re-read exact index and compare PDU digest before every delete; persist per-segment progress |
| probe/RF/SMS contend for one tty | one context-aware per-device gate plus defensive tty flock |
| global transport silently changes QDC to VoWiFi | per-Line unique resolver; ambiguity/unavailability fail closed; call-count tests |
| registration UI disagrees with send preflight | one shared cellular classifier |
| state dependency silently missing | startup fails before advertising `sms-v1`; private state/root contract tests |
| HIL evidence leaks telecom data or marker | destination only through private stdin, internal crypto marker, `CNUM` discarded in memory, de-identified conclusion only, docs/privacy checks |
