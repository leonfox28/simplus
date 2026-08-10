# 修复短信会话时间乱序技术设计

## 1. 设计状态与边界

- 状态：规划完成，等待用户审批本最终规划后才可 `task.py start`。
- 交付形态：一个原子跨层修复，包含 messages SQLite v8、SMS 专用 cursor、store/service 查询、Web 排序消费、迁移与浏览器回归、ADR/架构规范，以及用户批准后的本地容器更新。
- 不拆父子任务：排序事实、分页边界、会话摘要与 Web 必须在同一版本切换，拆开会产生互相不兼容的中间状态。
- 不改变公共 `SMSMessage` 字段、短信 transport、发送/入站状态机、unread token、SSE payload、Line 或会话身份。
- 普通验证不发送真实短信，不触碰 RF、模组持久状态或 HIL。

## 2. 问题模型与目标事实

当前数据流把两个不同含义的时间放进同一个排序键：

```text
出站 createdAt = Simplus 本机在发送副作用前创建持久记录的时间（毫秒）
入站 createdAt = 模组/运营商提供的 receivedAt（可能整秒、偏钟、延迟同步）
```

两者适合展示“业务声称的发生时间”，却不能比较本机实际先看到哪条记录。新的唯一排序事实是 `recordSequence`：messages SQLite 在一条短信首次成功插入时分配的全局单调序号。

```text
出站请求 -> 先 INSERT/分配 sequence -> transport side effect -> status update（sequence 不变）
入站同步 -> transaction 内 INSERT/分配 sequence + unread marker -> commit -> ACK
查询/SSE refetch -> 后端按 sequence newest-first -> Web 反转为 oldest-first
```

SQLite 单写者与 `AUTOINCREMENT` 共同保证成功提交的首次插入具有稳定顺序，删除不会复用旧序号。随机 message ID、运营商时间精度、状态更新时间和系统时钟回拨都不参与新记录先后判断。

## 3. Messages schema v8

### 3.1 `sms_messages` 表

v8 原子重建 `sms_messages`：

- 新增 `record_sequence INTEGER PRIMARY KEY AUTOINCREMENT`；
- `message_id` 改为 `TEXT NOT NULL UNIQUE`，继续是所有公共 API、operation、provider 去重和 unread 外键使用的稳定业务 ID；
- 其余 direction、Line、remote、正文、状态与时间约束保持 v7；
- 表不再使用 `WITHOUT ROWID`，因为整数主键就是持久排序事实。

新索引与查询顺序一致：

- 全局：主键 `record_sequence` 反向扫描；
- remote-only：`(remote_address, record_sequence DESC)`；
- Line + remote：`(line_id, remote_address, record_sequence DESC)`；
- inbound provider 去重索引保持；
- 旧 created-time page indexes 删除，避免保留第二套排序暗示。

### 3.2 历史回填

v7 没有显式序号，但可恢复首次本地持久化代理：

```sql
CASE direction
  WHEN 'inbound' THEN updated_at_unix_ms
  ELSE created_at_unix_ms
END
```

迁移按该值升序，再按原 `created_at_unix_ms, message_id` 确定性排序并显式写入 `ROW_NUMBER()` 序号。这样：

- 既有出站 status update 不会把消息推后；
- 既有入站使用本地持久化时间，能修复本次整秒截断导致的倒序；
- 极少数历史记录若所有可恢复字段都同毫秒，无法重建真实因果，只维持确定性 message-ID 次序；未来记录不再有此缺口。

### 3.3 unread 与 Down

Up 在删除旧 `sms_messages` 前复制 `sms_message_unread`，保留每个 `unread_id`、message ID、remote address 和最大 AUTOINCREMENT 水位；重建后恢复指向新表 `message_id` UNIQUE key 的 `ON DELETE CASCADE` 外键与索引。

Down 对称重建 v7 `WITHOUT ROWID` 表和原 created-time indexes，同时完整复制消息与 unread ledger。`recordSequence` 在 Down 中有意移除；再次 Up 会按可恢复时间重新回填。迁移测试必须覆盖真实 v7→v8→Down→v8、外键检查、schema version、索引、未读 token 与新插入序号继续大于历史最大值。

## 4. Cursor 与分页契约

### 4.1 版本化 SMS cursor

`internal/domain/pagination` 保留 Calls 使用的 v1 `(createdAt, stable ID)`。SMS 新响应使用带显式 kind/version 的 v2 sequence cursor：

- 包含正整数 `recordSequence` 和稳定 message ID；
- base64url、最大 256 字符、无号码/正文/Line/设备身份；
- SMS service 只接受 SMS v2 或可迁移的 v1，Calls service 明确拒绝 SMS v2；
- v2 SQL 使用严格 `record_sequence < boundary`，不依赖 boundary 行存在，因此删除 boundary 后分页仍有效。

### 4.2 v1 过渡

部署前浏览器可能仍持有 v1 cursor。SMS store 在 boundary 行仍存在时用 `(message ID, createdAt)` 查到其 v8 sequence，并验证它属于当前过滤范围，再继续 sequence 分页。行已删除或字段/过滤不一致时返回既有 cursor-invalid 错误。所有新 `nextCursor` 都发 v2，因此兼容路径自然消失，不形成第二套长期排序。

### 4.3 查询 owner

- `ListSMSPage` 的 global、remote-only、Line+remote 分支全部按 sequence DESC；
- `ListSMSConversationPage` 的 window latest、会话外层排序与 next cursor 全按 latest sequence DESC；
- `lastOutboundLineId` 按 outbound sequence DESC；
- 状态更新只改变 `updatedAt/status`，不会改变页面或摘要顺序；
- read-through token 继续使用独立 unread AUTOINCREMENT 水位，不与 record sequence 合并。

## 5. API 与 Web

公共 JSON schema 不增加 `recordSequence`。`api/openapi.yaml` 只澄清消息和会话是“按首次持久化顺序 newest-first”，`PageCursor` 仍是客户端不可解析的 opaque string；按既有生成入口刷新生成物并验证无 drift。

Web 删除 `createdAt/message ID` 排序所有权。新的纯 helper：

1. 按 TanStack infinite-query 给出的页面顺序拼接（第一页最新，后续页更旧）；
2. 保持每页服务端 sequence DESC 原样；
3. 整体反转得到气泡 oldest-first；
4. 不修改 Query cache 原数组。

会话列表本来就使用服务端摘要顺序，无需客户端二次排序。显示标签继续使用 `createdAt`，因此排序与标签时间有意解耦；滚动 ownership、top pagination anchor、near-bottom follow、SSE HTTP refetch 和自动已读 gate 保持不变。

## 6. 兼容、部署与回滚

- v8 应用与 migration 必须一起上线；旧二进制不能读取 v8 schema，因此回滚应用时必须先执行 v8 Down。
- 公共消息 JSON、HTTP route、错误码、SSE topic 和生成 query identity 不变；活动浏览器的 v1 cursor 有过渡支持。
- 新增 ADR 0024，局部取代 ADR 0023 与 `docs/architecture.md` 中 SMS `(createdAt, stable ID)` 排序条款；Calls 条款不变。
- 所有门禁通过且用户批准本规划后，构建三个 `dev` 镜像并用 `docker compose up -d` 原地替换当前服务，不先 `down`、不删除数据卷。检查 data-init/bootstrap 退出 0、app/agent/netd healthy、镜像 revision 和 HTTP 200。
- 容器重建只恢复既有 desired runtime，不授权发送短信、修改 RF、持久写模组或运行 HIL。若部署需要超出当前 Compose 正常重建的权限/动作则停止并报告。

## 7. 测试策略

### Storage/migration

- 先插入 outbound，再插入 provider `CreatedAt` 更早但本地持久化更晚的 inbound；验证 sequence query 与 summary。
- 同时间、随机 ID 逆序、批量/并发插入、replay、status update、delete/non-reuse。
- global/remote/Line+remote/conversation 多页 keyset，前插、boundary 删除、v1→v2 过渡和跨 cursor kind 拒绝。
- v7→v8 回填修正、unread ID/token 保留、外键、Down/Up 与 schema/index contract。

### Service/HTTP

- v2 round-trip、v1 过渡、Calls 隔离、invalid/version/length/filter error mapping。
- JSON 数组顺序与 summary last message 使用 sequence；公共 message timestamps 原样。

### Web/browser

- helper 保留服务器顺序并反转多页，不按 timestamp/ID 排序且不 mutation。
- Messages Vitest 用反向业务时间证明 outbound bubble 在 inbound 上方，conversation preview 为 inbound。
- Desktop/mobile Playwright 保留 scroll、load older、SSE refetch/read-token checks；不保存 screenshot/trace，不发送真实短信。

## 8. 风险与缓解

| 风险 | 缓解 |
| --- | --- |
| 重建主表时 unread 外键/水位丢失 | 同一 migration 先复制 ledger，显式保留 ID，跑 foreign_key_check 与 Down/Up 测试 |
| 通用 pagination v2 误伤 Calls | cursor 带 kind/version；Calls 明确只接受 v1，并保留原测试 |
| Web 自行排序与服务端再次漂移 | helper 只消费页面顺序；反向业务时间跨层 fixture |
| 旧 cursor 无 sequence | boundary 存在时按 ID+time 映射；新 cursor 自包含 sequence；删除旧 boundary 稳定拒绝 |
| 历史同毫秒真实顺序不可恢复 | 使用入站 updatedAt/出站 createdAt 的最佳可靠代理；剩余 tie 确定性处理并明确不做内容启发式 |
| 容器迁移失败影响现有数据 | 先完成 migration tests/全量 gates；Compose 原地更新；失败时保留数据卷并停止扩展动作 |

## 9. 明确延期

- 暴露/显示本地持久化时间或 sequence、双时间 UI、按运营商时钟校准。
- 给 cursor 加保密加密、跨 endpoint cursor 通用化或重写 Calls 顺序。
- 会话搜索/归档、号码规范化、联系人/Line/未读/发送行为变化。
- 真实短信回环或任何 HIL 验证。
