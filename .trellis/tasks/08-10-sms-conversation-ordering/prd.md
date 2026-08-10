# 修复短信会话时间乱序

## Goal

短信会话必须按 Simplus 首次持久化每条短信的稳定先后顺序展示和分页，而不是用精度、时钟来源不同的业务时间推断先后。这样管理员先发出一条短信、随后收到同内容回传时，发送气泡稳定出现在收到气泡上方；刷新、分页、会话摘要和服务重启后顺序保持一致。

## Background

- 当前 SQLite 历史、会话摘要和 Web 都按 `(createdAt, message ID)` 排序。
- 出站 `createdAt` 来自本机创建发送操作的高精度时间；入站 `createdAt` 来自模组/运营商的 `receivedAt`，可能只有整秒精度，也可能与本机时钟存在偏差。
- 已确认的脱敏现场模式是：入站业务时间落在整秒，出站本地创建时间晚约百毫秒，但入站实际在出站完成后才持久化。页面只显示到秒，因此两个标签看似相同，现有排序却把入站放在出站之前。
- 当前入站 `updatedAt` 在首次持久化时取本机时间，之后不会因状态推进而变化；出站的首次持久化时间是 `createdAt`，而其 `updatedAt` 会随发送状态推进，不能用于排序。

## Requirements

### R1 — 持久、单调的记录顺序

- messages 数据集必须为每条首次成功插入的短信分配一个全局单调、删除后不复用的 `recordSequence`。
- 出站序号在发送副作用前的持久化步骤分配；入站序号与首次消息持久化、未读 marker 位于同一个事务。
- 入站重复同步、operation replay、发送状态更新和消息删除不得创建、改变或复用既有序号。

### R2 — 统一使用记录顺序

- 全局短信历史、remote-only 历史、兼容的 Line + remote 历史、会话最后消息、会话列表顺序和最近出站 Line 都必须使用 `recordSequence`。
- 状态从 queued 推进到 sent/failed/unconfirmed 不得让气泡或会话重排。
- `createdAt`、`updatedAt` 和 `sentAt` 的公共含义保持不变；界面时间仍显示业务 `createdAt`，不把内部序号伪装成运营商时间。

### R3 — 稳定分页与兼容

- 新短信页和会话页 cursor 必须携带版本化、长度受限的记录顺序边界，使用严格 keyset `<`，不得退化为 offset。
- 新 cursor 在 boundary 消息被删除后仍能继续分页；不得包含号码、正文、Line、SIM/设备身份或其他私密数据。
- 部署前已经发出的 v1 `(createdAt, message ID)` cursor 应在 boundary 仍存在时兼容解析并映射到记录顺序；非法、跨类型或不一致 cursor 继续返回稳定分页错误。
- Calls 分页继续使用原有 `(createdAt, stable ID)` 契约，不得被 SMS cursor 变化影响。

### R4 — 历史迁移

- messages schema v8 必须保留全部短信、状态、provider ID、未读 marker 及其水位，并为旧记录回填唯一序号。
- 旧记录按可恢复的本地首次持久化时间回填：入站使用 `updatedAt`，出站使用 `createdAt`，再以既有 `(createdAt, message ID)` 作为确定性 tie-break。
- 因此当前已经存在的“先发送、后入站，但入站业务时间被截到同一秒更早位置”的记录在升级后也应纠正。
- Down migration 回到 v7 时必须保留业务短信与未读 marker；仅移除 v8 顺序元数据。再次 Up 必须成功且数据完整。

### R5 — Web 使用服务端顺序

- Web 不得再用 `createdAt/message ID` 重建服务端顺序；它只把各个 newest-first 页面按服务端顺序拼接并反转为聊天的 oldest-first 展示。
- 顶部加载更早消息、滚动锚点、接近底部自动跟随、SSE 触发 HTTP 刷新和自动已读行为必须保持。
- 同一秒内入站业务时间早于出站、但入站后持久化的回归场景必须证明发送气泡在上、收到气泡在下，且会话预览认定入站为最后消息。

### R6 — 既有边界不变

- exact remote-address 会话身份、跨 Line 合并、每条消息的 Line、最近 Line fail-closed、联系人、未读 ledger/read-through token、发送状态机和 SSE payload 均保持不变。
- 不增加公开 `recordSequence` 字段；它是服务端持久化和 opaque cursor 的内部排序事实。
- 不通过真实短信、RF、模组持久写、Host VoWiFi 或 HIL 验证；只使用合成 fixture 与对现有本地数据的只读检查。

## Acceptance Criteria

- [ ] AC1：新建出站记录后再持久化一个业务 `createdAt` 更早的入站记录，所有历史查询均返回入站为 newest，Web 展示为出站在上、入站在下。
- [ ] AC2：上述两条属于同一 remote address 时，会话摘要的 `lastMessage` 是入站，会话排序与最近出站 Line 同时正确。
- [ ] AC3：相同毫秒、时钟回拨、批量入站、并发写入和随机 message ID 不改变首次成功持久化顺序；replay/status update 不重排。
- [ ] AC4：多页 global、remote-only、Line + remote 和 conversation keyset 分页无重复或遗漏；新 v2 cursor 的 boundary 删除后仍可继续，合法旧 v1 cursor 可过渡使用。
- [ ] AC5：v7→v8 保留消息与 unread marker，并按入站 `updatedAt`/出站 `createdAt` 回填；当前缺陷形态升级后顺序被纠正。
- [ ] AC6：v8→v7→v8 保留业务消息与未读状态，迁移对象、schema version、索引和 AUTOINCREMENT 行为符合预期。
- [ ] AC7：Calls cursor round-trip/分页不变；非法 SMS cursor 仍映射到现有稳定 HTTP 错误，OpenAPI/generated drift 为零。
- [ ] AC8：Messages 的 Vitest 与桌面/移动 Chromium E2E 覆盖反向业务时间、分页/滚动和 SSE 刷新，均不使用真实通信。
- [ ] AC9：全量格式、生成、lint、test、security、build 与 docs gate 通过，工作树不含私密现场数据、截图或临时数据库副本。
- [ ] AC10：在用户批准本规划后，构建新的本地 `dev` 镜像并原地更新当前 Compose；app/agent/netd 均 healthy、一次性任务退出 0、HTTP 200，且不触发真实短信测试。

## Out of Scope

- 修改运营商/模组提供的 SMS 时间、纠正远端时钟、在 UI 显示内部序号或新增“接收时间/运营商时间”双时间标签。
- 猜测不同号码格式等价、改变会话身份、联系人模型、未读规则、发送线路或发送状态。
- 对无法恢复本地持久化先后的更老同毫秒历史做内容匹配或方向启发式重排；迁移只使用现有可靠字段和确定性 tie-break。
- 真实短信回环、RF/模组操作、HIL、保存现场截图/日志/数据库，或推送远端仓库。

## Blocking Open Questions

无。用户已选择稳定本地记录顺序，并要求按推荐方案修复。
