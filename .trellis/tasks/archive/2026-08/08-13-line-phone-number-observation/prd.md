# 统一线路手机号观测

## Goal

让手机号成为 `Line` 的统一当前观测集合，而不是 Host VoWiFi 的 UI 附属字段。支持的型号在硬件边界内通过固定只读能力读取当前 SIM/Profile 的本机号码；IMS 注册号码作为另一种内部观测来源汇入同一 Line 语义。Web、OpenAPI 和其他上层只读取 Line，不知道 `AT+CNUM`、QDC507 或 VoWiFi 的实现细节；当来源返回不同号码时，页面同时显示并标明来源。

## Background

- 当前线路页直接读取 `line.voWiFi?.phoneNumber`（`web/src/pages/Lines.tsx:165,174`）。因此未启用 Host VoWiFi 的原生蜂窝 Line 永远显示“未获取”，即使 SIM 能返回本机号码。这违反了项目现有的 `Web/API -> application intent -> Agent typed service -> model adapter` 边界。
- `ManagedLine`、`domain/line.View`、`inventory.Line` 和 `hardware.SubscriptionProfile` 当前均没有手机号字段；`VoWiFiLineState.phoneNumber` 是唯一公开来源。
- QDC507 的普通生产 probe 已读取模块身份、SIM identity、归属运营商与注册状态，但没有调用本机号码能力。之前的严格 `CNUM` 实现只服务一次性 HIL，清理时已删除，没有迁入生产 typed capability。
- Quectel EC25/EC21（EG25-G 同系列）手册定义 `AT+CNUM` 为从 (U)SIM 读取 subscriber own number；结果可以为空或包含多条。国际号码 type 145 本身应带 `+`，上层不得依据 type、运营商或已知号码猜测补全。
- 2026-08-13 的当前设备 HIL-0 在唯一 QDC507、SIM ready、CS/PS/EPS 中国联通 LTE 注册且 Agent 独占恢复的前提下，仅执行一次固定 `AT+CNUM`。设备返回唯一显式 `+E.164`、type 145 和最终 `OK`；实际号码只显示在私有对话，未写入仓库、任务、日志或 fixture。探针在宿主权限拒绝时未打开设备，随后通过 network-none、单 tty、read-only、cap-drop 容器完成，并恢复 Agent healthy。

## Requirements

- R1 — Line 统一所有权：号码观测必须进入 Line 应用/领域视图与 `ManagedLine` OpenAPI；线路页只读取该集合，不再 join 或读取 `VoWiFiLineState.phoneNumber`。
- R2 — typed 型号能力：新增最小 model-neutral subscriber-number adapter seam；QDC507 adapter 固定执行 `AT+CNUM` 并严格解析。Web/API 不得提交型号、命令、endpoint 或来源选择。
- R3 — 当前 SIM/Profile 归属：蜂窝号码观测必须与同一次 probe 的当前 ready SIM identity 一起进入 Agent `SIMObservation`，再映射到 `SubscriptionProfile` 和对应 Line；换卡、锁卡、缺卡、probe 失败或身份不可确认时号码立即不可用，不得沿用旧卡号码。
- R4 — 严格号码语义：只接受唯一一条、字段数量和终止符精确、号码本身为 `+E.164`、type 145 的 `CNUM`。空结果是正常 unavailable；重复、多个号码、national/unknown type、缺少 `+`、控制字符、overflow、URC、echo、mixed terminal 或查询失败均 unavailable，不猜测。
- R5 — best effort：号码读取失败不得令 otherwise-complete modem/SIM probe、Line readiness、蜂窝注册或短信能力失败；但不得把 IMEI、IMSI、ICCID、测试对端号码或运营商信息冒充手机号。
- R6 — 多来源汇聚：蜂窝 `CNUM` 和 IMS `P-Associated-URI` 仅作为内部 Line number observations。Line 层负责校验当前 Line ID/SIM 绑定、按号码合并并输出公共列表；来源实现不得泄漏到 Web。只有一个来源时保留一项；同值时合并为一项并列出两个来源；不同值时保留两项，不猜测权威顺序、不阻断 Line。IMS 号码查询只参与 Line 展示视图，不进入供短信、电话、出口和 VoWiFi 控制使用的 `Line.Topology`。
- R7 — 当前观测不持久化：手机号不写 Managed Line 配置或新的 SQLite 字段；由当前硬件/IMS 快照按需返回，避免换卡或号码变更后显示陈旧值。只有经过管理员鉴权的 `ManagedLine` 业务 API 暴露号码；硬件拓扑/setup、普通日志、错误、SSE 和 task evidence 不包含真实号码。
- R8 — 公共契约收口：新增 `ManagedLine.phoneNumbers`，最多两项；每项包含一个规范 `+E.164` `number` 和非空唯一 `sources`，source 枚举仅为 `cellular-sim`、`ims`。列表按号码稳定排序；空列表表示未获取。移除公开 `VoWiFiLineState.phoneNumber`，生成物由 OpenAPI 源刷新。VoWiFi 页面仍展示运行状态，但不再拥有 Line 身份字段。
- R9 — 型号可扩展：本次只为已有真实证据的 QDC507 注册蜂窝号码能力。其他型号不实现时上层保持同一 Line 接口并显示 unavailable，不在 Line/Web 增加型号分支。
- R10 — Simulator 与回归：Simulator 提供合成 Line 号码；测试覆盖 QDC success/unavailable/malformed、Agent 协议与校验、换卡/缺卡清空、Line source merge、OpenAPI/HTTP、Web 桌面与移动展示，并确保 fixture 不含真实号码。

## Acceptance Criteria

- [x] AC1：当前 QDC507/SIM 在不启用 VoWiFi 时，线路页通过 `ManagedLine` 显示 HIL-0 已确认的本机号码；Web 源码不再读取 `voWiFi.phoneNumber`。
- [x] AC2：固定 QDC507 `CNUM` parser 仅接受唯一显式 `+E.164` type 145 + final `OK`；所有空、歧义、畸形和失败 fixture 均产生 unavailable，且不影响 Line readiness。
- [x] AC3：号码沿 `adapter -> Agent SIM observation -> inventory SubscriptionProfile -> Line application view -> OpenAPI -> Web` 贯通；任一换卡/锁卡/缺卡/identity mismatch 会清空，且无型号/AT 分支越过 adapter。
- [x] AC4：只有一个来源时 Line 返回一项；蜂窝与 IMS 同值时返回一项且列出两个来源；不同值时返回两项并分别标注来源，Web 全部展示且有桌面/移动回归。
- [x] AC5：手机号不新增数据库持久化，不进入日志/SSE/错误/任务 evidence；真实号码不进入 Git，所有测试使用 synthetic E.164。
- [x] AC6：focused/race tests、OpenAPI generate/verify、Web typecheck/tests/build、`make lint`、`make test`、docs/container/security checks 通过；普通自动化不访问真实设备。
- [x] AC7：更新本地 `dev` 三镜像并原地更新 Compose；数据目录保留，`agent/netd/app` healthy、HTTP health ok、运行 revision 指向新工作提交，真实线路页显示统一 Line 号码。

## Out of Scope

- 写入或修改 SIM 的 Own Numbers phonebook、`CPBS`/`CPBW`、Qualcomm phonebook preference 或任何 modem persistent setting。
- 通过 IMSI/ICCID/IMEI、测试对端、运营商账户或短信内容推导号码。
- 为没有真实证据的 ML307A/其他型号擅自启用 `CNUM`；手工号码编辑和持久 override 另行决策。
- RF、蜂窝注册、短信、电话、VoWiFi 激活或蜂窝数据副作用。

## Key Decision

- 用户明确要求不同来源返回不同号码时在 Web UI 全部显示。公共 Line 因而拥有来源明确的号码观测列表，不选“主号码”；同值去重并合并来源，不同值分别显示。
