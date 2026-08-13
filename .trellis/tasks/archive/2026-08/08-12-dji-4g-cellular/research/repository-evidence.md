# QDC507 原生蜂窝短信：仓库与公开资料证据

## 1. 问题重述

本任务不是让大疆模组成为通用蜂窝数据网卡，而是让 QDC507 在 Simplus 现有 `ManagedModem -> Line -> messaging` 模型中，以中国联通实体 SIM 经蜂窝基站完成原生短信收发，并确保整个路径不依赖、不启动也不回退 Host VoWiFi。

## 2. 当前仓库事实

### 2.1 单一 Agent 与型号边界已经存在

- QDC507 与 ML307A 已使用同一个 `simplus-agent`、Agent 协议和 adapter registry；不应新增第二套 Agent。`docs/architecture.md:62-78`、`docs/compatibility.md:24`。
- QDC507 当前已固定 USB 匹配、primary AT interface 2、QMI interface 4、标准 3GPP 只读 probe 和 SIM presence 查询。`internal/modemadapter/qdc507.go:15-64`。
- QDC507 当前只实现 `ATProbeAdapter` 与 `SIMPresenceAdapter`，尚未实现 `EquipmentIdentityAdapter`、`SIMIdentityAdapter` 或 `RFControlAdapter`，因此不能满足稳定 Managed Modem/Line 绑定。`internal/modemadapter/qdc507.go:17-20`、`docs/compatibility.md:21`。
- 现有共享 identity helper 已验证 IMEI Luhn 校验、ICCID 长度限制、每实例 pseudonym 与末四位提示；ML307A 已提供可复用的类型化实现形状。`internal/modemadapter/identity.go:10-70`、`internal/modemadapter/ml307a.go:69-130`。

### 2.2 短信候选实现完整，但尚未 production 装配

- `internal/modemadapter/qdc507sms` 已有 PDU driver、Linux tty transport、完整 `SMSAdapter`、durable SQLite state，以及发送重放/结果未知和入站 persist-before-delete 语义。`internal/modemadapter/qdc507sms/adapter.go:18-47`、`internal/modemadapter/qdc507sms/driver.go:124-250`、`docs/architecture.md:105-122`。
- 完整 adapter 目前刻意不进入 `modemadapter.DefaultRegistry`；默认 registry 只有发现型 `QDC507{}` 与 `ML307A{}`，因此 production Agent 不声明 `sms-v1`。`internal/modemadapter/qdc507sms/adapter.go:27-30`、`internal/modemadapter/registry.go:186-192`。
- production Agent 只创建默认 scanner，并用不接受 SMS backend 的 managed-hardware handler 启动。`cmd/simplus-agent/main.go:127-156`、`internal/agentapi/server.go:45-52`。
- Agent 容器与原生 systemd 已有 UID 10002/私有 state directory/无网络边界；`./data/agent` 映射到 `/var/lib/simplus-agent`，适合保存 QDC507 durable SMS state，不需要新增权限或网络。`compose.yaml:20-41`、`containers/agent-entrypoint.sh:18-58`、`scripts/release/install-debian.sh:86-103`。

### 2.3 应用短信 transport 目前是全局单选，不能满足无回退要求

- hardware `simplusd` 当前只创建 Agent inventory；若配置 Host VoWiFi supervisor，则全局把 messaging sender/inbox 设为 VoWiFi gateway。它没有装配原生 Agent SMS gateway。`cmd/simplusd/main.go:105-144`。
- hardware policy 当前明确拒绝 Agent `sms-v1`，这是旧 read-only 边界，真实 HIL 通过后必须原子更新。`cmd/simplusd/main.go:542-557`。
- `messaging.Service` 当前只有一个全局 sender/inbox，并用 `hostVoWiFiSMS` 布尔值切换全体 Line 的 capability/availability 判断。`internal/application/messaging/service.go:199-266`、`internal/application/messaging/inbound.go:28-71`。
- ADR 0019 已规定短信由实际 transport、Line 能力与实时状态选择，必须 fail closed，不得恢复全局 access mode 或静默切换 transport。`docs/decisions/0019-line-identity-and-communication-paths.md:15-22`。

设计结论：messaging 需要按 Line 选择唯一 transport。`line.Capabilities.SMS` 选择原生 Agent；只有不具备原生 SMS、但具备 `HostVoWiFiAuth` 且对应 worker 在线的 Line 才选择 Host VoWiFi。选中的 transport 不可用时直接失败，不尝试另一条路径。

### 2.4 注册状态已在 Agent 解析，但未进入业务/Web

- 标准 probe 已读取 `CFUN/CREG/CGREG/CEREG/COPS/CSQ` 并输出 RF、三个注册域、当前运营商/RAT 和信号。`internal/modemadapter/standardat/probe.go:25-98`、`internal/modemadapter/standardat/parse.go:98-218`。
- `inventory.AgentSource` 使用该 probe 生成设备/SIM/Line，但只映射身份、SIM presence 与 capability，丢弃注册、网络和信号。`internal/application/inventory/agent_source.go:35-169`。
- Managed Modem domain/OpenAPI/Web 当前只展示 online、RF、SIM presence、型号、序列号与能力，无法从 Web 判断是否已连接基站。`internal/domain/modem/model.go:43-54`、`api/openapi.yaml:2126-2155`、`web/src/pages/Modems.tsx:1-240`。

设计结论：复用 Agent targeted probe，通过应用层类型化 status reader 将注册摘要、域、运营商、RAT 与有界信号状态投影到 Managed Modem API/UI；不引入网络扫描、手动选网、APN 或任意命令入口。

### 2.5 Agent 内部串行边界需要统一

- `hardwareprobe.Scanner` 用自己的 `controlMu` 串行 probe/RF/identity；`modemadapter.SMSRouter` 另有独立 per-device gate。两者没有共享同一 modem operation owner。
- QDC507 tty transport 的 non-blocking flock 能避免同时打开，但只能产生 busy/failure，不能保证 probe、RF 和 SMS 的业务级顺序。

设计结论：新增一个 Agent 内部、按 device ID 的 context-aware operation gate，由 scanner 的 probe/RF/identity 与 SMS router 共用。它不跨进程、不进入 Web/API，也不替代 operation ID 与 durable SMS state。

### 2.6 durable SMS 状态不能绑定临时 USB 位置

- 现有候选 adapter 把 `DeviceReport.ID` 写进入站 message ID、state key 和 send/ACK request digest；该 ID 来自本次 USB 拓扑位置，换端口会变化。`internal/modemadapter/qdc507sms/message.go:135-171`、`internal/modemadapter/qdc507sms/adapter.go:101-108`、`internal/modemadapter/qdc507sms/state.go:60-124`。
- Agent 只在综合只读 probe 中取得当前模组/SIM 的每实例 HMAC 指纹；普通 Agent SMS 请求目前只有临时 device ID。应用的当前内部 topology 已含 persistent Line 绑定可重新核对的设备/Profile 证据，因此 messaging 可构造私有 runtime target；内部 Agent SMS contract 必须增加 device generation 与预期模组/SIM 指纹。Router 在同一设备锁内重新 probe、确认三者仍匹配，再把 SIM 指纹作为 adapter state namespace。这样关闭 Line 解析后换模组/换卡、dispatch 前误发的 TOCTOU。原始 IMEI/ICCID/IMSI、号码、指纹和设备路径不得进入公共 contract 或普通日志。
- 现有 driver 只执行 `CMGF=0`，未固定 `CPMS`。官方 EC25/EC21 手册说明 `CPMS` 的 `mem1` 决定读/删存储、`mem2` 决定写/发存储、`mem3` 决定收到的短信存储，且 `SM` 表示 (U)SIM 存储。若沿用默认 `ME`，换 SIM 后旧短信可能与新 SIM 混淆。

设计结论：production adapter 使用稳定 SIM 指纹作为 durable namespace、operation digest 和入站 message ID 的输入；Agent wire response 仍回显本次请求的临时 device ID。每个短信操作先执行固定 `CMGF=0` 与 `CPMS="SM","SM","SM"`，从而把入站存储与物理 SIM 关联；禁止 `AT&W` 或其他持久化配置命令。旧的 fixture-only schema v1 不做不安全的自动归属迁移，production 使用明确的 v2 state 文件/schema，并在发现旧版本时 fail closed。

### 2.7 public modem status 应扩展现有 targeted probe，而不是再造网络控制

- `AgentSource` 已在同一 snapshot 周期内缓存完整 probe，当前只在映射 inventory 时消费身份/SIM/capability 字段。`internal/application/inventory/agent_source.go:34-58`、`internal/application/inventory/agent_source.go:167-186`。
- `ManagedModem.Service.List` 已通过 `AgentRFController.State` 做一次新鲜 targeted probe，但只取 RF 字段。`internal/application/modem/service.go:96-130`、`internal/application/modem/agent_rf.go:22-39`。

设计结论：把现有 RF-only reader 扩为一次返回 RF/SIM/注册/运营商/RAT/信号的 runtime status reader；模组列表仍只增加当前已经存在的那一次 targeted probe，不再叠加第二次状态读取。一个共享、纯函数的 cellular classifier 同时服务模组页状态和 Agent SMS preflight。公共 API 只输出枚举化注册摘要、三个注册域、当前运营商/PLMN、RAT、有界信号值、观察时间和稳定失败码；不增加网络扫描、手动选网、APN、数据拨号或任意命令入口。

## 3. 公开技术资料

- Quectel EC25/EC21 官方 AT 手册说明 `AT+CGSN` 返回设备 IMEI、`AT+QCCID` 返回 SIM ICCID、`AT+CIMI` 和 `AT+CRSM` 提供 SIM 归属元数据、`AT+CFUN` 控制并读回 RF 功能级，以及 `CMGF/CMGL/CMGR/CMGS/CMGD` 的 3GPP PDU-mode SMS 流程：<https://quectel.com/content/uploads/2021/03/Quectel_EC25EC21_AT_Commands_Manual_V1.3.pdf>。
- 3GPP 27 系列将 TS 27.005 定义为 SMS/CBS 的 DTE-DCE 接口，将 TS 27.007 定义为 UE AT 命令集：<https://www.3gpp.org/DynaReport/27-series.htm>。
- DJI 官方 FAQ 说明 Cellular 模块使用运营商 nano-SIM，SIM PIN 会导致联网失败。Simplus 因此只显示锁卡并 fail closed，不自动解锁或修改 PIN：<https://repair.dji.com/help/content?customId=01700008632&lang=zh-CN&paperDocType=ARTICLE&re=CN&spaceId=17>。

公开资料只能支持命令选择与 fixture。QDC507 实际固件响应、RF 写入、运营商注册及真实短信仍必须由本型号和当前 SIM 的受控 HIL 证明。

## 4. 证据提升与 production 装配策略

1. 基础 `modemadapter.QDC507` 增加只读设备/SIM 身份和显式 RF capability，但继续把 `sms-control` 保持 `documented`。
2. `qdc507sms.Adapter` 在完整 tty driver + durable store 被注入后覆写 capability evidence；只有真实 HIL 被接受后，组合 adapter 才把 `sms-control` 声明为 `observed`。
3. `modemadapter.DefaultRegistry` 保持 discovery-only，防止无 durable dependency 的 scanner 误报 SMS。production Agent 由 composition root 构造含组合 QDC507 adapter 的 runtime registry，并把同一 registry 注入 scanner 与 SMS router。
4. HIL 前的 ordinary production composition 完全不装配 SMS backend；HIL 通过后的最终 production composition 若 durable state、registry/driver/adapter 或 handler 构造失败，Agent 必须启动失败，不能以无 durable state 的方式发送或继续声明 `sms-v1`。
5. HIL 前先用 build-tag 隔离、只选择唯一 QDC507 的固定 typed runner 组合相同 scanner/router/adapter；该 runner 不接受 AT、QMI、设备路径或任意命令。HIL 通过后才更新兼容性结论、production policy 和 observed capability。不得增加 `--enable-qdc507-sms` 之类长期 runtime 开关，也不得开放 arbitrary AT/QMI。
6. production Agent 只接受一个绝对、私有的 state 根目录（原生和 Compose 都已有 `/var/lib/simplus-agent`）；固定文件名由 Agent 内部派生，SQLite open/schema/registry/backend 任一失败都使进程启动失败。Agent shutdown 必须关闭并 checkpoint 该 store，shared HTTP `WriteTimeout` 必须覆盖 `SMSRequestTimeout`。

## 5. 发送前置、错误和状态

- SMS Send 在 shared operation gate 内执行固定只读前置检查：完整 probe、SIM READY、稳定 SIM 指纹、RF On、至少一个 CS/packet/EPS 域为 home/roaming/SMS-only 注册；随后才选择 SIM storage、设置 PDU mode 并 dispatch。List/Read/ACK 同样要求 SIM READY 与相同指纹，但不要求当前仍在网，以便处理已经存储的入站消息。
- RF Off、SIM locked/absent、searching、denied、not registered 和 unknown 分别映射到有界稳定错误；这些错误经过 Agent client/gateway 进入现有 durable message state，不包含原始响应、号码或设备路径。
- List/Read 可以读取现有 modem SMS storage；Acknowledge 仍严格遵守应用入库成功后才删除。发送前置失败不得触发 RF、选网、PIN、APN 或 transport fallback。
- 部分 multipart 成功、prompt 后响应丢失和重启遗留 accepted 继续使用现有 `SMS_SEND_OUTCOME_UNKNOWN`，不自动重发。

## 6. 仍需真实证据验证的技术未知

- 当前 QDC507 固件对选定只读 IMEI/ICCID/SIM operator 查询的实际响应形状。
- QDC507 的 `CFUN=1/4` typed write/read-back 及服务恢复行为；不验证持久上电策略。
- 当前中国联通 SIM 在现场的 SIM READY、归属元数据、EPS/packet/CS 注册组合及短信承载可用性。
- PDU-mode 单段/长短信、入站存储、索引复用与自号码或获批测试号码的实际互通。
- `AT+CPMS="SM","SM","SM"` 在当前 QDC507 固件上的接受、读回及重新启动后的接收存储行为。
- 当前开发 Agent 未运行，因此本轮只读 HIL-0 在接触设备前即停止，没有取得新的设备证据；部署/重启 Agent 与任何后续 RF/SMS 动作都等待独立授权。

这些未知不改变 MVP 设计，但决定 capability 能否从 fixture/documented 提升到 HIL/Runtime。失败时保持 production fail closed，并在同一任务内修正 adapter/transport 后重新走授权门；不得改用 VoWiFi、QMI WMS、蜂窝数据或更宽权限作为未经批准的替代。
