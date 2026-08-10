# 短信会话页面仓库证据

## 当前页面与前端边界

- `web/src/pages/Messages.tsx:30` 当前把发送、联系人和历史拆成三个区域；桌面历史是表格，窄屏是记录卡片，并用手工输入的 Line + remote address 筛选会话。
- `web/src/messages/conversations.ts:11` 已有未接入页面的会话分组逻辑，但按 `(lineId, remoteAddress)` 分组，与本任务已批准的“只按 remote address、跨线路合并”冲突。
- `web/src/messages/status.ts:8` 已集中拥有 `queued / unconfirmed / sent / failed / received` 的中文状态展示，实施应扩展或复用它，不在气泡中重写状态映射。
- `web/src/api/events.ts:4` 把 `messages` SSE topic 映射到所有带 `messages` tag 的生成 Query；新会话摘要与已读状态接口继续使用该 tag 即可沿用现有失效机制。
- `.trellis/spec/web/frontend/**` 和 ADR 0022 要求页面使用生成的 Fetch/Zod/TanStack Query 契约，HTTP/SQLite 保持权威，SSE 只发送失效与通用 attention，不携带号码、正文或未读数。

## 当前公共 API

- `api/openapi.yaml:409` 的 `GET /api/v1/messages` 使用 `(createdAt, stable ID)` opaque cursor，单页上限 50；现有会话过滤要求 `lineId` 与 `remoteAddress` 同时存在。
- `api/openapi.yaml:2399` 的 `SMSMessage` 已包含消息 ID、方向、Line、remote address、正文、状态、错误码与时间，足以渲染聊天气泡。
- `api/openapi.yaml:2473` 的发送请求仍必须显式携带 Line、数字 destination、正文和 operation ID；本任务不改变发送状态机或 transport。
- `api/openapi.yaml:2272` 的 Contact 使用唯一电话号码；前端可用现有联系人 Query 按精确号码关联名称，不应在 messages 与 contacts 两个 SQLite dataset 之间建立事务或数据库 join。
- 当前没有会话摘要、跨线路号码历史或标记会话已读的公共 operation。

## 当前应用与存储

- `internal/application/messaging/service.go:35` 的窄 Repository port 已拥有消息写入、状态推进、分页、统计和删除；会话摘要与已读推进应作为同一业务服务的最小新能力加入。
- `internal/application/messaging/service.go:372` 当前只接受无过滤或 Line + remote address 成对过滤。新设计需兼容旧组合，同时允许 remote address 单独过滤；Line 单独过滤仍无效。
- `internal/storage/sqlite/messages.go:241` 有四条分页 SQL 分支，但没有 remote-only 分支或会话摘要查询。
- `internal/storage/sqlite/migrations/messages/00006_keyset_pagination.sql:1` 已有全局 `(created_at, message_id)` 与 `(line_id, remote_address, created_at, message_id)` 索引；跨线路查询需要新增以 `remote_address` 开头且排序元组一致的索引。
- messages dataset 的当前 schema version 是 6。新未读状态属于该 dataset，不能新建第六个数据库，也不能依赖跨 dataset 事务。
- `internal/domain/pagination/cursor.go:15` 的既有 cursor 只编码时间与稳定 ID。会话摘要可使用最后一条消息的时间/ID 复用该 cursor，不需要把号码放入 cursor。
- `internal/storage/sqlite/managed_lines.go:11` 按 `created_at_utc, id` 返回 Managed Lines；“第一条可发送线路”可在前端从这一稳定顺序中过滤 `ready` 且具备 SMS/Host VoWiFi SMS 能力的首项。

## 已批准的产品决策对技术设计的影响

- 会话身份是存储后的 `remoteAddress` 精确值，不包含 Line。仓库没有足够国家上下文安全推断 `138...` 与 `+86138...` 等价，因此本任务不做号码猜测或重写。
- 会话摘要必须由后端分页返回；仅对浏览器当前加载的最近 20/50 条消息分组会漏掉旧会话。
- 摘要需要返回最后一条消息、未读数和最近一次出站 Line，才能支持列表预览、持久角标和 composer 默认线路，而不加载整个会话。
- 消息分页的 `(createdAt, ID)` 不能兼任未读到达顺序：时间只有毫秒精度且消息 ID 随机，同一毫秒后入库的 ID 不保证更大。未读需要独立的 `AUTOINCREMENT` marker 序号。
- 入站消息首次持久化时应在同一 transaction 创建 unread marker；duplicate/replay 不重复创建，删除短信级联删除 marker，count 从 marker 行数派生。
- Remote-only 最新页返回基于该读取 snapshot 的 opaque unread watermark token；标记已读只删除同一 remote address 上不晚于 token 的 marker，并发新短信总有更大序号。
- 升级 migration 创建空 unread marker 表，因此旧历史自然初始化为已读；迁移后新入站才计为未读。
- 联系人名和 Line 名在浏览器分别从现有生成 Query 关联；历史 Line 已删除时显示通用回退，不把另一个 Line 当成原 Line。

## 文档与验证影响

- ADR 0022 第 6 条明确把短信会话过滤限定为 Line + remote address。本任务需要新增 ADR 0023，明确只按 remote address 的产品会话、持久 unread marker/watermark 和对 ADR 0022 该局部决定的取代。
- `docs/architecture.md` 应更新 Web、Messages 分页/会话身份、接收入站短信后的未读数据流和机械不变量；唯一 active MVP plan 增加本里程碑。
- 验证必须使用 synthetic fixtures、临时 SQLite、httptest、Vitest 和 Playwright；不得发送真实短信或执行 RF、设备写入、Host VoWiFi/HIL。
