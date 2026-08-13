# 大疆 QDC507 原生蜂窝短信

## Goal

让安装中国联通实体 SIM 的大疆 QDC507 成为可稳定添加、可创建 Line、可观察蜂窝注册状态的模组，并通过运营商蜂窝基站完成原生短信收发。整个纵切明确不使用 VoWiFi，也不包含电话或通用蜂窝数据。

## Background and Confirmed Facts

- QDC507 与 ML307A 共用一个 `simplus-agent`、Agent 协议和 adapter registry；型号差异留在类型化 adapter 内，不建立第二套 Agent（`docs/architecture.md:59-72`、`docs/compatibility.md:24`）。
- 当前 QDC507 adapter 已支持 USB 识别、固定 primary AT interface 2、QMI interface 4、标准只读 AT probe 和 SIM presence，但尚未实现稳定设备身份、SIM/Profile 身份或可调用的 RF adapter（`internal/modemadapter/qdc507.go:15-64`、`docs/compatibility.md:21`）。因此它还不能满足稳定 `ManagedModem -> Line` 绑定。
- 仓库已有未装配的 QDC507 PDU-mode SMS driver、Linux tty transport、durable SQLite recovery 和 fixture。默认 registry 与 production Agent 均不声明 `sms-v1`（`docs/architecture.md:105-122`、`internal/modemadapter/qdc507sms/adapter.go:18-47`、`cmd/simplus-agent/main.go:127-156`）。
- 该项是任务开始时的 baseline；受控 HIL 完成后，production composite 现已装配 `sms-v1`，
  而安全的 `DefaultRegistry()` 仍保持 SMS closed。
- 标准 probe 已读取 RF、SIM、CS/packet/EPS 注册域、当前运营商/RAT 和信号；模组页目前只显示 online、RF 与 SIM presence，不能判断是否连上基站（`internal/modemadapter/standardat/probe.go:25-98`、`internal/application/modem/service.go:96-130`、`api/openapi.yaml:2141-2155`）。
- hardware `simplusd` 当前把短信 sender/inbox 全局设置为 Simulator、Host VoWiFi 或空；原生 Agent SMS 与 Host VoWiFi 无法同时按 Line 路由（`cmd/simplusd/main.go:83-169`、`internal/application/messaging/service.go:199-270`）。既有 ADR 要求依据实际 transport、Line 能力和实时可用状态 fail closed，禁止静默 fallback（`docs/decisions/0019-line-identity-and-communication-paths.md:21`）。
- RF 属于 `ManagedModem` 且只能由管理员显式改变；添加模组、创建 Line、短信与 Host VoWiFi 均不得隐式改变 RF（`docs/decisions/0017-managed-modems-and-capability-adapters.md:25`、`docs/decisions/0019-line-identity-and-communication-paths.md:16-18`）。
- Simplus 产品范围包含原生蜂窝短信，但明确排除通用蜂窝数据上网、热点、NAT 和蜂窝网关（`docs/product.md:5`、`docs/product.md:84`）。电话属于未来产品范围，但不在本 MVP。
- Quectel EC25/EC21 官方资料为 `CGSN/QCCID/CIMI/CRSM/CFUN/CPMS/CMGF/CMGL/CMGR/CMGS/CMGD` fixture 提供依据；这不能代替 QDC507 固件和当前 SIM 的真实 HIL。DJI 官方资料说明 SIM PIN 会阻止联网；Simplus 只报告锁卡，不修改 PIN。
- 2026-08-12 的固定只读 HIL-0 尝试因开发 Agent 未运行而在接触 modem 前停止；本任务尚无新的 QDC507/SIM/RF/注册现场证据，也未部署或重启服务。

## Requirements

### Sanitized partial HIL evidence

- 获批的单次出站 marker 已由测试 peer 实际收到，但 runner/协议结果保持
  outcome-unknown，因此应用消息仍为 `unconfirmed`，禁止重发；这不是完整 HIL pass。
  代码复核定位到一个能解释该现象的高概率协议差异：真实 modem 可能在 payload dispatch 后
  回显刚发送的 exact hexadecimal PDU，而旧 tty transport 只过滤 `AT+CMGS` 命令回显；后续
  `+CMGS`/`OK` 因多出一行而被保守归类为 malformed post-dispatch/unknown。实现现只过滤最多
  一次、逐字节等于刚发送 payload（可带 Ctrl-Z）的回显；不同 hex、重复回显与 URC 仍 fail closed。
  该推断没有原始私有 transcript 可反证，必须通过一条新的独立获批出站验证，且不回改旧 unknown。
- 为该新出站实现了独立 build-tag send-only confirmation runner；它只保留固定注册/身份观测、
  typed outbound router/adapter/driver、operation-only existing-v2 recovery store 与单行应用状态迁移，
  没有 RF/入站/电话/数据/号码/generic command 能力。它在发送前要求同一 fenced SIM、RF 已 On、
  无语音/传真/未知通话、无 pending inbound、无历史 confirmation attempt，并生成新的随机 marker/
  operation ID；最多发送一次，旧 unknown app/ledger 永不重发或修改。该入口已按精确授权执行，
  去标识结果为 persisted/confirmed/complete；历史 unknown 仍未修改或重发。
- 本机号码的瞬时私有观测为 available；号码本身未记录。peer 的入站回复在 runner 停止后
  到达。获批的 receive-only recovery 随后在 `SM` 发现多条入站候选并按歧义停止；它没有
  接管到应用库，也没有 ACK 或删除任何 SIM 短信。后续代码复核确认该列表路径可能把
  `REC UNREAD` 改成 `REC READ`，并把已组装候选写入 Agent 私有 recovery 数据库；没有
  把这些候选提升为用户可见消息。继续分类或接管需要新的精确授权。
- 初始 RF Off 未能恢复，因为清理阶段观察到两个 active mode=1 data calls。没有加入 hangup、
  强制 RF Off、电话或数据控制。后续确认入口证明 mode=1 是可与 SMS 共存的既存 data bearer；
  它未被创建、配置、挂断或公开，语音/传真/未知通话仍阻断。入站与新确认出站 HIL 均通过后，
  production SMS 已按 typed adapter/store/router 装配。
- 针对上述多候选歧义，build-tag 隔离的只读 classifier 已实现并通过合成测试，但尚未在
  真实 modem/SIM 或私有 HIL 数据库上执行。它只报告去标识的候选/匹配 cardinality 与
  PDU 完整性，不写应用/Agent 数据库，不 ACK/delete/send，不改变 RF 或最终服务状态。
  即使报告多条中恰一条匹配，也必须在新的精确授权前停止，不能据此接管或删除。
- 复核发现 3GPP/当前 Quectel 契约下 `CMGL=4` 会把 `REC UNREAD` 改为 `REC READ`；因此
  旧 classifier 已由 build-tag-only 固定 CRSM EF_SMS 双扫描替换。新 concrete graph 只发
  GET RESPONSE/READ RECORD，固定 `6F3C`/`7F106F3C`/absolute P2=4/176-byte record，
  不含 `CMGF/CPMS/CMGL/CMGR/CSIM/UPDATE` 或 fallback。标准与官方 Quectel family fixture
  只证明 parser/transcript 设计，不证明当前 QDC507 固件的 CRSM response shape 或 unread
  preservation；真实 constructor 因而继续 fail closed。验证该固件并执行分类都需要新的
  精确授权，既有“只读分类”授权不覆盖新的 CRSM 验证动作。preservation 验证必须使用
  另行获批、为本次验证新建的受控 synthetic unread fixture，不得把现有私有 inbox 候选
  隐式当作验证输入。
- 获批的 CRSM preservation 验证已严格执行一次，但在输出 ready 之前以去标识 unknown
  结束，因此没有请求或产生新的受控入站短信，也没有重试。该结果无法区分现存 unread、
  固件响应 shape 不兼容、基线不稳定或 fence 失败；没有新增授权前不得追加扫描诊断。
- 独立 build-tag-only preservation verifier 已实现并通过合成 fixture，但从未真实运行。它不打开
  application/recovery DB，不读取既有私有候选；固定 service ownership 后在同一 device gate 中
  要求 zero-unread 双扫描 baseline，只接受一条新 controlled inbound 导致的唯一 free-to-unread
  变化，再要求三次 full scans 中全部 176 bytes 与所有其他 records 保持不变。CLI 只有有界
  arrival/total timeout，ready/final output 去标识；真实执行、从获批 peer 手工发送 exactly one
  新消息以及首次 CRSM read 可能改变 unread 的风险仍需要新的精确授权，且只能运行一次、不得
  retry。成功也只解除后续 code/spec review blocker，不授权 classifier/adoption/ACK/delete。
- 用户已经在仓库外私有 TXT 中逐条审阅两条 pending assembled recovery candidate，并精确批准
  清空这两条记录对应的 SIM stored segments；公开任务材料不记录其内容、sender、index、PDU、
  identity、时间或私有路径。独立 build-tag-only reviewed cleanup 入口已经实现并通过合成测试，
  但代码/测试阶段从未运行，没有访问真实 recovery DB、modem/SIM 或服务。授权 count 固定为 2，
  只覆盖当前 fenced SIM 的恰好两条 unacknowledged inbound recovery records；不覆盖新到/未记录
  短信、bulk delete/list、application/operations/outbound mutation、发送、RF、注册、电话或数据。
- 获批的 reviewed cleanup 随后严格执行一次并在逐段删除前返回去标识
  `cleared=0; pending=unknown`，没有重试。DB-only 进度检查确认两条记录共三个 stored segments
  全部仍为 pending，`delete-started=0`、`deleted=0`，因此没有物理 PDU 被删除或留下删除不确定态；
  私有 TXT 当时保留。随后修复 SQLite WAL/SHM 合法辅助文件的 false-negative，并按用户要求
  继续同一已审阅清理目标；第二次执行返回 `cleared=2; pending=zero; stage=complete`。三个 PDU
  分段均逐段复核后删除，两条 recovery records 在一个事务中 acknowledged，私有 TXT 与临时
  二进制已删除，服务状态保持 inactive/disabled。
  每段必须 fresh fence、stored-index `CMGR` + 常时 digest 复核、single-index `CMGD`、progress commit；
  `CMGR` 可能在删除前把 unread 改成 read，该两条记录的删除授权明确接受此中间变化。任何歧义或
  不确定立即停止且不自动 retry；`CMGD` 前先持久化 delete-started，只有确定删除、post-fence 与
  最终 CAS 都成功才变为 deleted，重启发现 delete-started 时不会再次读取或删除该 index。完成后
  只通过 recovery DB 验证两条在一个事务中同时 acknowledged 和 subscription pending empty。

- 上述 reviewed cleanup 完成后，用户已明确确认继续一次正常蜂窝短信端到端入站验证：独立
  build-tag-only fresh-inbound runner ready 后，由原已批准测试 peer 手工发送恰好一条正文精确为
  `OK` 的新短信，目标是 application DB persist -> PDU revalidate/single-index delete -> recovery/
  `SM` pending empty。本轮不再做 CRSM 辅助验证。该 runner 已实现并通过合成测试，但代码/测试
  阶段未运行，没有停止服务、打开真实私有 DB 或访问 modem/SIM。它只从原 outcome-unknown outbound
  的单一 read-only application snapshot 推导 bounded E.164 peer 与 Line，并只读核对同 SIM unknown
  ledger；新正文固定编译为 `OK`，不复用旧 marker，也不修改原 outbound/operation。

- 该 fresh-inbound 纵切最终已完成：唯一受控 `OK` 先持久化到应用 DB，再经 PDU revalidate、
  single-index delete 与 durable progress 清理，最终 application/recovery/`SM` 一致且 pending zero；
  原 outcome-unknown outbound 与同 SIM ledger 保持不变。公开材料只记录去标识结论，不记录号码、
  PDU、设备/SIM 身份或私有路径。该成功只覆盖当前 SIM/批准 peer 的入站，不自动开放 production。

- fresh-inbound 真实基线随后暴露出合成 fixture 缺失的标准 16-bit concatenation UDH
  (`IEI 0x08`)；原 parser 只接受 8-bit `IEI 0x00`，因此把可组装的真实长短信误报为 malformed。
  实现现已无损支持 8/16-bit 入站 reference，同时保持出站只生成既有 8-bit transcript，并新增
  合成回归。修复后基线正确收敛为 non-empty physical inbox：当前另有两条完整 pending 记录，
  未写应用库、未 ACK/delete；内容只导出到本地排除的 0600 私有 TXT 供用户审阅。

- 用户审阅并批准上述新增两条后，exact-two cleanup 返回
  `cleared=2; pending=zero; stage=complete`，私有 TXT 与临时二进制已删除。随后的正常
  fresh-inbound 已成功 flush ready，但两分钟窗口内没有新短信到达，以
  `ready=true; persisted=false; cleared=false; pending-zero=false; stage=arrival` 安全结束；
  DB-only 复核为 recovery pending zero，没有应用写入、ACK/delete 或不确定进度。

- 用户随后重新开启 fresh-inbound 并在 runner flush ready 后发送唯一一条正文精确为 `OK` 的短信。
  首进程在 arrival 轮询失败后停止，但 Agent recovery 已有唯一 candidate，应用仍 absent、ACK 未开始，
  因此未要求重发。去标识复核证明 modem 报告的是批准 `+86` E.164 对端的精确 11 位国内表示法；
  HIL 仅在逐位等于该唯一批准 peer 后规范化为批准 E.164。一次性 persist-only bridge 先落应用库，
  随后现有 replay 路径完成 fresh fence、PDU digest 复核、single-index delete 与最终全量验证，返回
  `persisted=true; cleared=true; pending-zero=true; stage=complete`。没有新出站、RF、电话、数据或 VoWiFi 动作。

- R1 — 路径与范围：QDC507 本任务内的通信只走运营商蜂窝网络；不启动、不依赖、不回退 Host VoWiFi。MVP 不启用电话、数字通话媒体、蜂窝数据、APN、热点、NAT 或网关。
- R2 — 统一的类型化能力边界：Web/API 和应用层只提交稳定业务 ID、固定业务意图，并按身份、SIM、运行状态、RF、SMS 等小型能力接口工作；不得按 `QDC507`、型号、VID/PID 或厂商做控制分支。VID/PID、interface、设备路径、AT/QMI 文本与厂商响应只能存在于 discovery/registry/adapter/driver 内；不得新增任意命令或设备路径入口。以后接入另一款已验证的同能力模组时，只需实现并注册相同 adapter 能力，不应修改 Line、消息、Managed Modem、HTTP/OpenAPI 或 Web 业务流程。
- R3 — 稳定身份：QDC507 必须用型号固定的只读流程取得经校验的 IMEI，并转为每实例 HMAC 绑定；当前 SIM/Profile 必须用 ICCID 的每实例 HMAC 和掩码提示表示。原始 IMEI 仍只允许经现有显式 no-store 读取，原始 ICCID/IMSI 不进入公共 API、数据库或普通日志。
- R4 — 运营商与本机号码元数据：归属运营商名称与 MCC-MNC 必须从当前 SIM/Profile 的类型化读取结果 best effort 推导；不得把“中国联通”或 `460-01` 写成 QDC507 型号常量。HIL 可在唯一设备/SIM fence 且蜂窝注册成功后，通过型号 adapter 的固定只读 `CNUM` best effort 读取本机号码；只有唯一、TON 145 且响应已显式携带 `+` 的 E.164 才可作为瞬时私有观测，空、重复、歧义、national format、malformed、overflow 或查询失败均视为 unavailable，不猜测、不阻断注册或短信 readiness。该值不进入公共 HTTP/OpenAPI/Web、数据库、普通日志或 task/HIL 报告；成功 HIL 只可输出去标识的 available/unavailable 能力结论。
- R5 — 可观察注册：Managed Modem 列表必须显示有界的蜂窝状态摘要、CS/packet/EPS 注册域、当前运营商/PLMN、RAT、信号与观察时间，或稳定失败码。状态只来源于现有只读 probe；不增加扫描/选网/拨号动作。
- R6 — 显式 RF：QDC507 RF On/Off 只接受管理员在 Managed Modem 上发起的布尔目标，使用型号固定动作并立即读回。不得提供持久上电 RF 策略；添加模组、创建 Line 和短信动作均不得自动开启 RF。
- R7 — Line 与 transport：管理员可基于稳定 QDC507 与当前 SIM/Profile 创建 Line。原生 Agent SMS 与 Host VoWiFi SMS 必须按每条 Line 选择唯一 transport：`SMS` 能力匹配 Agent，`HostVoWiFiAuth` 匹配 VoWiFi；零个或多个匹配都 fail closed，选定 transport 失败时不得尝试另一个。QDC507 Line 不声明 `HostVoWiFiAuth`，因此只能匹配 Agent。
- R8 — 发送前置：应用到 Agent 的内部 SMS 请求必须携带当前设备代际，以及 Line 所绑定的预期模组/SIM 脱敏指纹；原生 SMS Send 在同一 Agent 设备操作锁内重新确认 device generation、完整 probe、当前模组与 SIM 指纹仍匹配、SIM READY、RF On 和至少一个 CS/packet/EPS 注册域处于 home/roaming/SMS-only 注册。任何失败返回稳定错误，不自动 RF On、改 PIN、选网、设 APN 或重发；这些指纹不得进入公共 HTTP/API 或日志。
- R9 — 短信功能：复用现有 GSM7/UCS-2 单段与长短信编码、120 秒整次 multipart dispatch budget、success/definite failure/outcome-unknown 语义和应用层发送前持久化。部分发送、prompt 后响应丢失、进程重启遗留 accepted 均不得自动重发。
- R10 — 入站与删除：List/Read/ACK 同样携带设备代际与预期模组/SIM 脱敏指纹，并要求当前 target 与身份仍匹配、SIM READY，但不要求当前在网；它们固定选择 (U)SIM 的 `SM` 作为物理收件暂存区，而不是把 SIM 当作产品唯一或长期消息库。完整短信成功写入本机应用消息数据库后才能 ACK；Agent 随后在删除前重新读取并核对 storage index + PDU digest，逐段保存删除进度，再从 SIM 删除已安全接管的 PDU，防止索引复用误删。
- R11 — 稳定恢复命名空间：QDC507 SMS durable state、入站 message ID 和 send/ACK digest 必须按当前 SIM 的脱敏稳定指纹分区，不按临时 USB 端口 ID 分区。换端口继续恢复同一 SIM，换 SIM 形成隔离命名空间；Agent wire response 仍只回显当前临时 device ID。
- R12 — 生产装配门：production Agent 只有在真实 HIL 通过、durable state/registry/router 完整构造后才把 QDC507 `sms-control` 提升为 observed 并声明 `sms-v1`。最终已验收构建中，state 打不开、schema 不兼容、adapter 构造失败或依赖缺失必须启动失败，不可退化为无恢复发送；HIL 前构建则完全不装配该 backend。
- R13 — 真实验收：任务完成必须包含当前 QDC507 + 中国联通 SIM 的受控 HIL：显式 RF On 与读回、蜂窝注册、至少一条获批出站和一条获批入站短信、初始 RF/服务状态恢复。具体号码、内容、次数和停止条件在执行前重新陈述并取得独立授权。

## Acceptance Criteria

- [x] AC1–AC11：实现、fixture、受控入站/出站 HIL、证据提升与 production typed 装配均已完成；
  细项保持以下 AC12 的非 HIL 最终门禁，真实标识和原始证据均未进入仓库。
- [x] AC12（R1–R13）：`go test` focused suites、OpenAPI/Go/TypeScript generation drift、Web typecheck/Vitest/build/desktop+mobile E2E、container/deployment contracts、docs/privacy checks、lint 和全量测试通过；类型断言、registry/router 与合成第二型号 adapter fixture 证明上层仅依赖统一能力接口，应用/domain/HTTP/OpenAPI/Web 不含 QDC507/AT/QMI/设备路径控制分支；所有自动化只使用合成数据，不触发真实 RF、短信、电话、数据或 VoWiFi。

## Out of Scope

- Host VoWiFi 配置、SIM AKA、ePDG、SIP/IMS-over-Wi-Fi 注册，及任何 native/VoWiFi fallback。
- 通用蜂窝数据 attachment/PDP context、APN、宿主上网、热点、NAT、蜂窝网关、手动选网和网络制式/频段配置。
- QDC507 电话控制、呼入/呼出、DTMF、来电媒体与 USB 数字音频。
- SIM PIN/PUK 修改、自动解锁、eUICC mutation、运营商账户/套餐管理，以及把号码发现做成产品功能、Line 身份、公共字段或持久状态；只保留本任务 HIL 内固定 `CNUM` 的 best-effort 瞬时私有观测。
- QMI WMS；只有 AT PDU-mode HIL 证明该方案无法满足目标后，才能另建需求与授权评估替代方案。
- 未经执行时独立批准的 Agent 部署/重启、真实短信、RF 变更、modem 持久化写入或其他 HIL-1/HIL-2 动作。
- 与该纵切无关的 modem、网络或消息历史重构。

## Deferred Authorization and Technical Evidence

- 现有 outcome-unknown 操作的入站恢复另用 build-tag 隔离、构造上无 Send capability 的固定
  runner；它只能从单条应用 `unconfirmed` 与同 SIM 的唯一 recovery unknown operation 推导
  相关数据，先持久化唯一 exact reply 再 ACK/PDU 复核删除。它不要求注册、不改变 RF、
  不修改原出站记录，也不把当前部分证据提升为 production readiness。

- 多候选后的只读 classifier 是比 recovery 更窄的独立对象图：两个 SQLite 只以真正
  read-only 模式打开，SM 原始 PDU 只在内存解析，入口无私密 argv/stdin，且没有应用写入、
  recovery state mutation、Send、ACK、Delete、Prompt、RF、注册、电话、数据或本机号码能力。
  classifier 的一次报告不是 adoption/ACK/delete 授权，也不改变 HIL 或 production 证据等级；
  固定 CRSM EF_SMS 双扫描实现已通过合成 fixture，但当前固件的 response shape 与 unread
  preservation 仍未 HIL 验证，真实入口保持 fail closed，不会形成该报告。

- 当前短信主路径的 `CGSN/QCCID/CFUN/CNUM/CPMS`、注册域与指定 SIM/peer 互通已由 fixture +
  最小受控 HIL 收敛；CRSM unread preservation、其他 SIM/运营商/peer 与长期稳定性仍未知。
  `CNUM` unavailable 不阻断，其余关键路径失败时 production 保持 fail closed。
- 真实 HIL 的精确 Line/号码、合成短信内容、动作次数、前置状态、超时、停止条件、服务切换和回滚将在自动化检查通过后单独列出。此前对“需要完成 HIL”的范围决策不等于执行这些动作的授权，也不扩大到电话、蜂窝数据、VoWiFi、PIN、选网或其他写入。
