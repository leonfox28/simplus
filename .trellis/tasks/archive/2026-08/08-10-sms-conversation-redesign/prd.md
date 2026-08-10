# 重做短信会话页面

## Goal

将当前短信页面重做为接近即时通讯软件的收件人会话体验，让管理员能够快速切换会话、清楚区分收发短信、识别每条短信使用的线路与发送状态，并在页面重载或服务重启后仍看到可靠的未读数量。

用户价值：把当前分散的发送表单、联系人表单和历史表格收敛为一个日常可持续使用的聊天工作区，同时保留多 Line 的明确发送控制与短信结果不确定时的防重复边界。

## Background and Repository Evidence

- 当前短信页由独立的“发送短信”“联系人”“短信历史”区域组成，桌面历史使用表格、窄屏使用记录卡片，并通过手工输入 Line 和号码筛选会话（`web/src/pages/Messages.tsx:30`）。
- 前端已有未接入页面的会话分组函数，但它按 `(lineId, remoteAddress)` 分组，与本任务批准的“只按 remote address、跨 Line 合并”不一致（`web/src/messages/conversations.ts:11`）。
- 当前消息模型支持 `queued / unconfirmed / sent / failed / received`，集中展示逻辑已区分正在发送、等待运营商确认、结果未知、已发送、发送失败和已接收（`web/src/messages/status.ts:8`）。
- 当前历史接口使用稳定游标、单页最多 50 条，会话过滤要求 Line 与 remote address 成对提供；仓库没有会话摘要、跨 Line 号码历史或标记会话已读的 operation（`api/openapi.yaml:409`, `api/openapi.yaml:2399`）。
- 只在浏览器中对最近一页消息分组会遗漏仅存在于更早历史的会话，因此完整左栏需要后端分页会话摘要。
- 当前短信模型和 messages 数据库没有“管理员已查看”状态；`sms.received` SSE 只是即时 attention hint，不能在刷新或重启后还原未读数量。
- 联系人以电话号码唯一，现有 API 支持新增、编辑、删除；联系人可用于显示名称，但不应成为消息历史的持久身份（`api/openapi.yaml:2272`, `web/src/pages/Messages.tsx:102`）。
- Managed Lines 按创建时间、再按稳定 ID 返回；现有发送表单只允许 `ready` 且具备 SMS 或 Host VoWiFi SMS 能力的 Line（`internal/storage/sqlite/managed_lines.go:11`, `web/src/pages/Messages.tsx:99`）。
- 入站 remote address 可为数字或 GSM7 字母型服务地址，发送请求只接受数字号码，因此字母地址会话不能用现有契约回复（`api/openapi.yaml:421`, `api/openapi.yaml:2478`）。
- HTTP/SQLite 是权威状态；SSE `messages` topic 只让 generated Query 失效，不携带号码、正文、未读数或硬件信息（`.trellis/tasks/archive/2026-08/08-07-frontend-communication-refactor/design.md:50`）。
- 项目要求桌面和手机均能完成主要工作流，页面不得产生全局横向溢出（`.trellis/spec/web/frontend/index.md:1`）。

## In Scope

- 桌面双栏、窄屏主从模式的收件人会话页面和聊天气泡。
- 按 remote address 跨 Line 合并的会话摘要与历史分页。
- 单管理员持久 unread ledger/watermark、会话未读数和自动已读行为。
- composer Line 选择、默认/不可用 Line 行为、发送状态和明确失败后的人工重新编辑。
- 新建临时会话、联系人选择与联系人管理。
- 单条短信删除、摘要/未读重算和空会话处理。
- supporting messages SQLite migration、应用服务、OpenAPI、生成客户端、HTTP handler、前端 Query/UI、synthetic tests、ADR/架构/active plan/spec 更新。

## Requirements

- **R1**：左栏为会话选择栏，每个 remote address 显示为一个独立会话入口。
- **R2**：会话只按 remote address 合并；同一对端通过不同 Line 收发的短信显示在同一个会话，不因 Line 不同拆分。
- **R3**：选择左栏会话后，右栏只展示该 remote address 跨 Line 合并后的短信内容。
- **R4**：管理员发出的短信靠右显示，收到的短信靠左显示。
- **R5**：每条发出短信下方显示当前状态，至少区分“已发送”“失败”“等待运营商确认”，并保留正在发送和结果未知的准确语义。
- **R6**：会话栏底部提供短信输入区，用于编写并显式发送短信。
- **R7**：输入区上方提供 Line 选择；默认最近一次给当前 remote address 发信所用 Line；没有出站历史时默认稳定 Line 列表中的第一条可发送 Line。
- **R8**：管理员可在发送前手动切换 Line；一次发送只使用当时明确选中的 Line，不静默回退。
- **R9**：保留稳定 keyset pagination、消息状态刷新、mutation 不自动重试，以及结果未知时避免误导重复发送的既有行为。
- **R10**：匹配联系人时优先显示联系人名称，同时始终保留可见号码；联系人重命名或删除不改变历史会话身份。
- **R11**：桌面与窄屏均能完成会话选择、历史阅读、联系人操作和短信编写，不产生页面级横向溢出。
- **R12**：字母型服务地址会话可阅读历史，但输入区明确显示不可回复状态，不提交必然无效的发送请求。
- **R13**：窄屏默认显示会话列表，点选后进入全屏会话，顶部可返回列表；不把两栏上下堆叠。桌面保持并列双栏。
- **R14**：左栏显示后端持久化的未读数量/角标；页面刷新、浏览器重开和服务重启不得丢失。
- **R15**：只有新持久化的入站短信增加未读数；出站短信及其状态变化不增加。
- **R16**：新短信继续通过既有 `messages + sms.received` SSE 提示前端刷新；SSE 不承载号码、正文、未读数或权威会话数据。
- **R17**：未读状态按合并后的 remote address 会话保存，同一对端从不同 Line 收到的未读短信计入同一角标。
- **R18**：当前会话实际可见且最新一页 snapshot 成功加载后，使用该响应的服务端 unread watermark 自动标记当时已有短信为已读；未选中、后台或加载失败的会话不得被误标已读。
- **R19**：会话保持打开时，新入站短信成功显示后自动提交新 snapshot 的 watermark；晚于旧 snapshot 到达的新短信不得被旧水位清除。
- **R20**：左栏顶部提供“新建短信”；用户可选择已有联系人或直接输入并校验数字号码。
- **R21**：确认收件人后打开前端临时空会话；首条短信在服务端形成持久消息记录后，该 remote address 才进入正式会话列表。
- **R22**：临时会话和未发送草稿不持久化；首条短信提交前离开时允许丢弃，不新增草稿模型。
- **R23**：最近一次发信 Line 当前不可发送时，选择器保留该 Line 并显示原因，不自动切换；明确改选可发送 Line 前发送禁用。
- **R24**：最近一次发信 Line 已被删除时，以不可选的“历史线路（已删除）”表达原选择，不把另一条 Line 伪装成默认值。
- **R25**：左栏条目优先显示联系人名称，否则显示号码；号码始终作为辅助信息可见。
- **R26**：会话条目显示一行最新短信摘要、短信时间和未读角标；出站摘要用“我：”等方向提示区分。
- **R27**：会话按最新短信 `(createdAt, ID)` 稳定倒序；仅有旧短信 `updatedAt`/状态变化时不重排。
- **R28**：左栏标题区提供联系人管理抽屉/弹窗，支持新增、编辑、删除，不在会话列表旁常驻联系人表单。
- **R29**：“新建短信”可搜索联系人；联系人变化后名称由权威联系人 Query 刷新，会话仍以消息号码为身份。
- **R30**：`failed` 出站气泡提供“重新编辑”，回填正文和原 Line，但不自动提交；再次发送使用新的 operation ID。
- **R31**：`queued` 和 `unconfirmed` 不提供重发；系统不得自动重发任何短信。
- **R32**：保留单条删除；入口位于气泡“更多”菜单，桌面和触屏均可访问，并在执行前二次确认。
- **R33**：删除后重新计算最新摘要和未读数；删除该 remote address 的最后一条短信后，从左栏移除会话并清理失效选择。
- **R34**：每条气泡显示收发时间与实际 Line 名称；发出气泡额外显示发送状态。
- **R35**：当前 Managed Line 目录无法解析历史 Line 时，气泡显示明确的历史 Line 回退，不展示误导性当前 Line 名称。
- **R36**：首次部署未读功能时，把升级前每个现有会话初始化为已读；只有新 schema 生效后持久化的入站短信增加未读数。

## Acceptance Criteria

### 会话、布局与历史

- [x] 左栏能识别、切换不同 remote address；同一 remote address 在不同 Line 上的短信只形成一个会话，不同 remote address 不误合并。
- [x] 切换后右栏只显示当前会话；入站靠左、出站靠右，并按稳定时间/ID 正序呈现。
- [x] 左栏每项显示联系人名（如有）、号码、最新短信单行摘要、时间和未读角标；新短信把会话移到顶部，纯状态变化不重排。
- [x] 每条气泡显示实际 Line 和时间，出站同时显示准确状态；已删除 Line 使用明确回退。
- [x] 历史超过单页上限时，左栏不遗漏旧会话，当前会话可继续加载更早消息且无重复/漏项。
- [x] 桌面显示双栏；窄屏初始显示列表，点选进入全屏会话并可返回；两种 viewport 均无页面级横向溢出。

### Composer、Line 与发送安全

- [x] composer 位于会话底部；已有出站历史时默认最近 Line，无历史时默认第一条可发送 Line。
- [x] 第一条可发送 Line 来自 Managed Line 稳定顺序，不选择离线、SIM 不可用或无短信能力的 Line。
- [x] 最近 Line 不可用或已删除时保留并解释原选择，不自动改选；管理员明确选择可发送 Line 后才能发送。
- [x] 当前选择的 Line 原样进入发送请求；不存在 silent fallback、optimistic success 或 mutation 自动重试。
- [x] 字母型服务地址可查看但不可回复，并显示原因。
- [x] `failed` 可回填重新编辑，只有再次点击发送才产生新 operation；`queued/unconfirmed` 没有重发入口。

### 未读、SSE 与迁移

- [x] 新入站持久化后，相应合并会话未读数增加；同一 remote address 跨 Line 累加，出站/状态变化不误增。
- [x] `sms.received` SSE 让活跃摘要和历史经 HTTP 刷新，事件本身不含通信数据。
- [x] 当前会话成功显示最新 snapshot 后自动清除该 snapshot 已包含的未读；未打开、后台、加载失败或更晚并发到达的新消息不被清除。
- [x] 已读结果与未读数量在刷新、浏览器重开和服务重启后保持；乱序/重复 watermark 请求不能清除更晚的未读 marker。
- [x] v6 旧数据库升级后现有会话 unread=0；升级后新入站按规则未读；Down/再 Up 保留所有业务短信。

### 新建、联系人与删除

- [x] “新建短信”可选择/搜索联系人或输入合法号码，并打开临时空会话。
- [x] 未发送临时会话不进入刷新后的列表；首条 `sent/failed/unconfirmed` 持久记录均形成正式会话，持久化前失败不制造空会话。
- [x] 联系人管理支持新增、编辑、删除；联系人改名、换号或删除后，既有历史仍按原始消息号码稳定显示并合理回退。
- [x] 每条短信可从触屏友好的“更多”菜单删除并二次确认；删除最新/未读/最后一条后摘要、未读、选择和空态正确。

### 权威边界与验证

- [x] 发送、接收、状态变化、已读和删除后的页面均从权威 HTTP/SQLite 收敛，不恢复旧整页轮询或让 SSE 成为第二数据源。
- [x] 既有无过滤与 Line+remote 历史调用保持兼容；新增 remote-only 调用跨 Line 返回，Line-only 继续稳定拒绝。
- [x] 所有验证只使用 synthetic fixtures、临时 SQLite、httptest、Vitest 和 Playwright；不发送真实短信，不修改 RF、模组、SIM 或网络状态。

## Key Decisions

- **KD1 — Recipient identity**：会话只取持久化 remote address，不取 Line；在缺少国家上下文时不猜测本地号与国际号等价。
- **KD2 — Explicit Line**：Line 是 composer 的显式发送选择；最近 Line 不可用时 fail closed，由管理员明确改选。
- **KD3 — Responsive navigation**：桌面双栏，窄屏 list→full-screen detail→back。
- **KD4 — Durable unread**：未读属于 messages SQLite 的单管理员 unread marker ledger；count 从 marker 派生，SSE 只触发 HTTP 刷新。
- **KD5 — Automatic read**：只有 detail 可见且最新页成功显示后，才提交该 HTTP snapshot 的 opaque watermark。
- **KD6 — Temporary new conversation**：首条持久消息之前只存在前端临时状态，本次不保存草稿。
- **KD7 — Unavailable historical Line**：保留原选择与原因，不自动改变发送身份。
- **KD8 — Conversation summary**：联系人/号码、最新摘要、时间、未读角标，并按最新消息创建顺序排序。
- **KD9 — Contact ownership**：联系人管理收进左栏操作区；Contact 只影响显示/选择，不拥有会话身份。
- **KD10 — Manual resend**：明确失败只允许回填后人工重发；不确定状态不提供重发，每次重发是新 operation。
- **KD11 — Message deletion**：单条删除位于气泡更多菜单；权威重取驱动摘要、未读和空会话变化。
- **KD12 — Bubble metadata**：每条气泡显示时间与 Line；出站额外显示状态。
- **KD13 — Upgrade baseline**：旧历史初始化为已读，未读从 v7 migration 完成后的新入站开始。

## Risks and Deferred Items

- Remote address 使用精确存储值；带/不带国际区号的同一现实号码可能仍显示为两个会话，直到有可靠规范化产品决策。
- 自动已读必须防止 hidden tab、加载失败、删除 token boundary、同毫秒随机 ID 和并发到信竞态；具体 monotonic unread watermark 契约见 `design.md`。
- 会话摘要 SQL 需要匹配 remote-prefixed indexes，并在 10,000 条历史容量下验证分页与查询计划。
- 草稿持久化、会话搜索、置顶、归档、批量删除/已读和每管理员独立 unread ledger 延期。

## Out of Scope

- 修改真实 SMS transport、运营商协议、编码、发送状态机、Line 身份或硬件能力。
- 真实短信、电话、RF、eUICC、Host VoWiFi、模组持久写或任何 HIL 操作。
- 输入状态、运营商之外的已读回执、附件、表情反应、群聊等即时通讯专属能力。
- 本地/国际号码自动等价、联系人合并或历史号码迁移。
