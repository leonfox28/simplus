# 重做短信会话页面技术设计

## 1. 设计状态与边界

- 状态：规划完成，等待用户审批后才可 `task.py start`。
- 交付形态：一个原子跨层纵切，包含 messages SQLite migration、应用服务、OpenAPI/生成客户端、React 页面、测试和架构文档。
- 不拆父子任务：数据库、API、生成 Query 和页面必须以同一版本交付，拆成可独立发布的子任务会制造临时双契约；实施阶段可以按工作包设检查点。
- 不改变 SMS transport、编码、发送状态机、operation 幂等、Line 身份或 SSE payload。普通验证不接触真实短信、RF、模组或 HIL。
- 新 ADR 0023 局部取代 ADR 0022 中“会话必须由 Line + remote address 标识”的决定；ADR 0022 的 Vite/React Query、HTTP 权威、SSE 隐私和 mutation 不自动重试仍有效。

## 2. 总体数据流

```text
入站短信/出站结果
  -> messaging service
  -> messages.sqlite3: sms_messages
  -> messages SSE topic（仅失效提示）
  -> RealtimeBridge 失效 active generated queries
  -> GET /message-conversations + GET /messages?remoteAddress=...
  -> React 关联 Contacts 与 Managed Lines 后渲染

会话实际可见且最新页加载成功
  -> 使用该 HTTP snapshot 返回的 opaque readThroughToken
  -> PUT /message-conversations/read-state {remoteAddress, readThroughToken}
  -> messages.sqlite3: 删除不晚于该水位的 unread markers
  -> messages topic / HTTP 摘要刷新
  -> unreadCount 归零或保留并发到达的新短信
```

HTTP/SQLite 始终是会话、消息和未读状态的唯一真相源。SSE 不新增字段或 topic，不携带号码、正文、未读数、Line 或 unread watermark。

## 3. 会话身份与兼容规则

- 产品会话键是消息已持久化的 `remoteAddress` 精确字符串，不包含 `lineId`。
- 同一 remote address 的所有 Line 消息合并；每条消息仍保留原始 Line，发送也必须显式选择一个 Line。
- 不推断本地号与国际号等价，也不做国家相关规范化。仓库没有足够上下文安全判断 `138...` 与 `+86138...` 是否为同一收件人；这种地址仍是两个会话。
- 数字 remote address 可回复；GSM7 字母型服务地址只读，composer 显示不可回复原因。
- 联系人只影响显示名称与新建短信选择。会话不绑定 Contact ID；联系人修改或删除不会改变历史会话身份。
- `GET /messages` 保留无过滤和现有 Line + remote address 精确过滤，并新增 remote address 单独过滤。Line 单独出现继续返回 `MESSAGE_FILTER_INVALID`，旧客户端保持兼容。

## 4. 持久化设计

### 4.1 Messages schema v7

在 `messages` dataset 追加 migration 00007：

1. 新增 `sms_messages_remote_page_idx`：
   `(remote_address, created_at_unix_ms DESC, message_id DESC)`，服务跨线路历史和会话最新消息。
2. 新增 `sms_message_unread`：
   - `unread_id INTEGER PRIMARY KEY AUTOINCREMENT`，作为只增不复用的未读到达序号；
   - `message_id TEXT NOT NULL UNIQUE REFERENCES sms_messages(message_id) ON DELETE CASCADE`；
   - `remote_address TEXT NOT NULL`，沿用 remote address 长度约束；
   - 索引 `(remote_address, unread_id)`，服务摘要计数与按水位标记已读。
3. Up migration 创建空 unread 表；升级前历史没有 marker，因此自然初始化为已读。无需从不可恢复的旧浏览行为猜测状态。
4. Down migration 删除 unread 表和 remote-only history index，把 dataset schema version 恢复为 6；`sms_messages` 业务记录不重建、不丢失。

`AUTOINCREMENT` 是并发正确性的一部分：消息的 `createdAt` 只有毫秒精度且稳定 ID 随机，同一毫秒后来入库的消息不保证 ID 字典序更大。未读水位必须按数据库提交顺序单调增长，不能复用消息分页元组。删除短信通过外键级联删除 marker；删除整个会话不会留下可见状态。

### 4.2 Store 契约

在现有 messaging consumer-owned Repository port 上增加最小能力：

- `CreateInboundSMS` 改为一个 messages DB transaction：仅当入站消息首次插入时同时创建 unread marker，duplicate/replay 不重复计数；message + marker commit 后才允许上层 ACK。
- `ListSMSPage` 扩展为四种合法查询：全局、remote-only、Line + remote；Line-only 非法。remote-only 最新页在同一读取 snapshot 中返回当前最大 unread marker 的 token 原料。
- `ListSMSConversationPage` 返回按最后消息 `(createdAt, messageID)` 倒序的摘要页。每项含 remote address、完整最后消息、从 unread markers 派生的 `unreadCount` 和最近一次出站 `lineId`（如有）。
- `MarkSMSConversationRead(remoteAddress, unreadID, boundaryMessageID)` 在 transaction 中：
  1. 验证 boundary message 属于该 remote address；
  2. 若其 unread marker 仍存在，要求 `unread_id` 与 token 一致；
  3. 删除同一 remote address 上 `unread_id <= boundary` 的 markers；
  4. marker 已被另一标签页清除时幂等 no-op；boundary message 已删除时返回 not-found 让客户端刷新。

`unreadCount` 不作为可漂移的数值字段写入，而是 unread marker 行数。入站首次持久化、删除短信和标记已读都在同一 messages dataset 内原子改变 marker，因此重启与删除不会造成计数漂移。

### 4.3 游标与并发

- 会话摘要复用 `internal/domain/pagination` 的 v1 opaque cursor；边界取摘要最后一项的最后消息时间/ID，cursor 不包含号码。
- 摘要和消息页都使用严格 keyset `<` 边界并查询 `limit+1`，不使用 offset。
- messaging service 把 `(unread_id, boundary message ID)` 编码成有版本、长度受限的 opaque `readThroughToken`；token 不含号码或正文，客户端不得解析或合成。
- remote-only 最新页只返回该读取 snapshot 的 token。新入站即使与旧消息同毫秒、ID 字典序更小，也会得到更大的 `unread_id`，旧 token 的 delete 不会清除它。
- 多标签页或乱序 token 只能删除各自水位之前的 marker；较旧请求不会触及更大的未读序号。
- token/message 在请求前被删除或不属于该 remote address 时返回稳定 404/invalid error，前端刷新权威历史，不猜测水位。

## 5. 公共 HTTP 契约

所有 schema 从 `api/openapi.yaml` 生成，不手改 Go/TypeScript 输出。

### 5.1 `GET /api/v1/message-conversations`

参数：标准 `limit`、opaque `cursor`。

响应 `SMSConversationListResponse`：

- `conversations[]`（最多 50）：
  - `remoteAddress`；
  - `lastMessage: SMSMessage`；
  - `unreadCount`；
  - 可选 `lastOutboundLineId`。
- `conversationTotalCount`；
- `messageTotalCount`、`capacity`、`nearCapacity`，保留当前历史容量提示；
- 可选 `nextCursor`。

该 operation 使用 `messages` tag，因此现有 SSE topic mapping 会同时失效摘要和消息历史。

### 5.2 `GET /api/v1/messages`

过滤矩阵改为：

| `lineId` | `remoteAddress` | 结果 |
| --- | --- | --- |
| 省略 | 省略 | 全局历史，兼容旧行为 |
| 省略 | 提供 | 跨线路的收件人会话历史 |
| 提供 | 提供 | 单线路精确历史，兼容旧行为 |
| 提供 | 省略 | `400 MESSAGE_FILTER_INVALID` |

排序、limit、cursor、错误和 stats 字段保持不变。

Remote-only 最新页的 `SMSMessageListResponse` 额外包含可选 `readThroughToken`。无未读 marker、全局/Line+remote 查询或较旧 cursor 页可以省略该字段；前端只消费当前会话第一页的 token。

### 5.3 `PUT /api/v1/message-conversations/read-state`

请求 `MarkSMSConversationReadRequest`：`remoteAddress`、`readThroughToken`。号码放在鉴权 JSON body 中，不新增含号码的 URL path 或 cursor；token 只含版本、单调未读序号和稳定消息 ID。

- 成功：`204`，幂等；只有实际删除 unread markers 时才发布 `messages` topic，供其他标签页同步。
- `400`：字段/组合非法；`404`：boundary 不存在或不属于会话；`401/403/409/421/500/504` 沿用业务 API 边界。
- mutation 不自动 retry；失败不乐观清除角标。

## 6. 前端设计

### 6.1 Query 与本地状态

- generated infinite query 获取会话摘要；另一条 generated infinite query 以 `remoteAddress` 单独过滤当前历史。
- Contacts、Managed Lines 继续使用现有共享 generated queries；用纯派生映射关联显示名，不复制到持久浏览器状态。
- 当前 remote address、移动端 list/detail 模式、临时新会话、composer 文本、用户当前选择 Line、对话框开关和 operation error 是页面本地状态。
- 发送、删除、联系人 CRUD、标记已读均使用 generated mutation；成功后失效最窄的生成 key，失败保留已确认快照。
- 发送永不 optimistic append。服务端返回 `sent/unconfirmed/failed` 后刷新；网络结果未知时保留临时内容与提示，不自动重复提交，后续 SSE/focus/refetch 可发现已持久记录。

### 6.2 桌面与窄屏布局

- 桌面（`md` 及以上）：一个固定最小/最大宽度的会话列表栏 + `minmax(0,1fr)` 会话栏；两栏各自约束滚动，不产生页面级横向溢出。初次加载默认选中最新会话；无会话时右栏显示引导空态。
- 窄屏：默认只显示会话列表；点选后切换为全屏会话，顶部返回按钮回到列表。不会把两栏上下堆叠。
- 会话列表使用可访问的 Button/List item，不实现 clickable div；支持 cursor “加载更多”。本次不新增搜索、置顶或归档。
- 会话栏顶部显示联系人名/号码；消息区按 `(createdAt, ID)` 正序展示，较早页从顶部加载并尽量保留滚动锚点；新消息到达且用户接近底部时滚到底部，否则保留阅读位置。
- composer 固定在会话栏底部而非 viewport 全局底部，避免覆盖历史；字数上限沿用 1600。

### 6.3 会话摘要与气泡

- 左栏：联系人名优先、号码始终可见、最新短信单行预览、时间、未读 Badge；出站预览加“我：”。排序只取最后消息 `createdAt/ID`，状态 `updatedAt` 不重排。
- 入站气泡靠左，出站靠右。每条显示时间与解析后的 Line 名；历史 Line 不存在时显示“历史线路（已删除）”。
- 出站气泡调用现有集中 `smsStatusPresentation`，显示正在发送、等待运营商确认、结果未知、已发送或失败。
- 每条气泡的可访问“更多”按钮包含删除，执行前 Popconfirm/Modal 二次确认。删除最后消息后清理失效选择；移动端返回列表。
- 只有 `failed` 出站提供“重新编辑”：回填正文与原 Line，但新发送必须再次点击并生成新 operation ID。`queued/unconfirmed` 无重发入口。

### 6.4 Line 默认与 fail-closed

- 会话摘要的 `lastOutboundLineId` 是默认 Line；若从未出站，使用 Managed Lines 稳定顺序中第一条 `ready` 且具备 SMS 或 Host VoWiFi SMS 能力的 Line。
- 最近 Line 当前不可用时仍作为选中项显示原因，发送禁用，不自动选择其他 Line。
- 最近 Line 已删除时合成不可选的“历史线路（已删除）”选项，发送禁用。
- 用户手动选择可发送 Line 后才恢复发送；请求只使用界面当前选择，不回退其他 Line。
- 临时新会话没有出站历史，按第一条可发送 Line 初始化。用户离开前未发送的 remote address 与草稿不持久化。

### 6.5 自动已读

标记已读 effect 需要同时满足：

1. 会话 detail 当前实际显示（桌面选中或移动端 detail）；
2. document 为 visible；
3. 当前 remote-only 最新页成功加载且页面已渲染该 snapshot；
4. 摘要仍报告未读；
5. 使用该最新页原样返回的 `readThroughToken`，不使用 SSE 字段、摘要 ID 或客户端合成水位。

mutation 成功后刷新摘要。失败时角标保持，不把加载失败、后台标签页或未选中会话标已读。当前会话收到 SSE 后，摘要与历史经 HTTP 刷新；新 snapshot 渲染成功后再提交其水位。晚于 snapshot 到达的 marker 始终保留。

### 6.6 联系人与新建会话

- 左栏标题区有“新建短信”和“联系人管理”。联系人管理抽屉/弹窗复用 create/update/delete generated mutations。
- 新建短信弹窗支持搜索现有联系人或输入合法数字号码；确认后打开临时空会话。
- 首条发送在服务端产生持久 `SMSMessage` 后（包括 `failed/unconfirmed`）摘要接口才返回正式会话。请求在持久化前失败则不制造空会话。
- 联系人号码修改后，旧号码历史不会迁移到新号码；旧会话回退显示原号码，符合号码作为会话身份的规则。

## 7. 错误、空态与可访问性

- 会话列表、当前历史、Contacts 与 Lines 的查询错误分别可见；当前历史失败时绝不标已读。
- 无会话、未选会话、临时空会话、无可发送 Line、Line 不可用、字母地址不可回复、near-capacity、mutation busy/failed 都有明确状态。
- 图标按钮有 `aria-label`；列表选择、返回、更多、发送和联系人操作可键盘访问；不可用原因不只依赖颜色。
- 消息正文与号码只出现在鉴权 HTTP 页面，不进入 SSE、日志、公开 fixture、截图或文档示例。

## 8. 文档、迁移与回滚

- 新增 ADR 0023，局部 supersede ADR 0022 的 Line + remote 会话过滤决定。
- 更新 `docs/architecture.md` 的 Web/分页、接收短信、存储与机械不变量；更新唯一 active MVP plan 的新里程碑。
- 实现完成且验证通过后再更新 handoff/规范中的当前事实；不把规划描述成已实现。
- 回滚应用版本时同时执行 messages v7 Down migration；业务短信保留，但持久 unread markers 丢弃，旧版本恢复其既有 Line + remote UI。不得只回滚生成客户端或页面的一侧。

## 9. 风险与缓解

| 风险 | 缓解 |
| --- | --- |
| remote-only 分组遗漏旧会话或分页重复 | 后端摘要 endpoint、匹配索引、稳定 last-message cursor、equal-time/并发前插测试 |
| 已读 mutation 清掉 snapshot 之后的新短信 | unread `AUTOINCREMENT` 水位、opaque token、按序号删除和同毫秒并发到信测试 |
| 删除导致 unread/摘要漂移 | unread marker 与消息同库、入站 transaction、删除级联；删除最新/未读/最后一条测试 |
| 多 Line 合并后误用发送身份 | 每条显示 Line；composer 显式选择；不可用历史 Line fail closed、不自动回退 |
| 升级产生大量虚假未读 | v7 migration 创建空 unread marker 表，旧历史自然视为已读 |
| 联系人与 messages 位于不同 dataset | 前端按精确号码关联，不建立跨库事务或持久 Contact ID 绑定 |
| 聊天页滚动/移动端键盘回归 | 独立 pane overflow、top-load anchor、desktop/mobile Playwright 无全局 overflow |
| SSE 被误当未读真相源 | topic/tag 仅失效 Query；payload schema与 RealtimeBridge 不变 |

## 10. 明确延期

- 草稿持久化、会话搜索、置顶、归档、批量删除和批量已读。
- 每管理员独立 unread ledger（产品当前只有一个管理员）。
- 本地/国际号码等价规范化、联系人合并和历史号码迁移。
- 输入状态、送达/已读回执、附件、表情反应和群聊。
- 任何真实短信、RF、模组写入或 HIL 验证。
