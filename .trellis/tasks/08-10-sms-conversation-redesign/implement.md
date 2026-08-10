# 重做短信会话页面实施计划

## 1. 执行前置与约束

- 本计划不授权实现。只有用户审批最终规划后，主会话才运行 `task.py start` 并进入 Phase 2。
- 这是一个原子跨层交付，不拆成可独立发布的父子任务。工作包是顺序检查点；OpenAPI、生成物、messages schema 与 Web 必须保持同一版本。
- Phase 2 按 Trellis sub-agent 流程执行；每次 dispatch 以 active task 路径开头，并给不重叠的文件所有权。生成目录、OpenAPI、messages migration 和 `Messages.tsx/global.css` 各保持单一 owner。
- 修改任何既有常量、schema version、route、query filter、状态标签或 CSS 值前先全仓搜索。
- 不手改 `internal/api/openapi/generated.go` 或 `web/src/api/generated/**`；从 `api/openapi.yaml` 运行既有生成入口。
- 不执行真实短信、电话、RF、eUICC、Host VoWiFi 或任何 HIL-1/HIL-2。所有通信数据使用明确 synthetic fixtures。

## 2. WP0 — 激活、基线与 durable decision

依赖：用户审批最终规划。

- [x] 运行 `python3 ./.trellis/scripts/task.py start`，确认状态为 `in_progress`。
- [x] 读取 Phase 2.1 注入上下文并记录 `git status --short`；保留任何用户新增改动。
- [x] 运行生成 drift 与 Messages 针对性基线，确认失败不是已有环境问题。
- [x] 新增 `docs/decisions/0023-recipient-sms-conversations.md`：记录 remote-only 会话、跨 Line 合并、持久 unread marker/watermark、升级旧历史已读，以及只局部取代 ADR 0022 第 6 条。
- [x] 在唯一 active MVP plan 中增加新里程碑，先标记为进行中，不提前声称完成。

验证：

```bash
make verify-generated
go test ./internal/application/messaging ./internal/storage/sqlite ./internal/api/httpapi
corepack pnpm --dir web test -- Messages
```

回滚点：仅规划/ADR/计划状态；尚无 runtime schema 或公共 API 行为变化。

## 3. WP1 — Messages v7 与应用服务

依赖：WP0 的 ADR/契约冻结。

所有权：

- `internal/storage/sqlite/migrations/messages/00007_*.sql`
- `internal/storage/sqlite/messages.go` 及 storage tests
- `internal/application/messaging/service.go` 及 messaging tests
- 必要的 `internal/domain/sms/**` 小型类型

步骤：

- [x] 追加 messages v7 migration：remote-only 历史索引、`AUTOINCREMENT` unread marker 表/索引、空表代表旧历史已读、schema version Up/Down。
- [x] 扩展 `ListSMSPage` 支持 remote-only，同时保留 global 与 Line+remote；Line-only fail closed。
- [x] 实现稳定分页的会话摘要查询：last message、unread count、last outbound Line、cursor/next page。
- [x] 把入站消息首次插入与 unread marker 创建放进同一 transaction；duplicate/replay 不重复计数，消息删除级联 marker。
- [x] 实现 versioned opaque unread watermark token，以及 transaction 内的 remote/boundary 校验和 `unread_id <= token` 删除；旧/重复 token 幂等 no-op。
- [x] 在 messaging Service/Repository port 增加最小 summary/read 方法、limit/cursor/address 验证、typed not-found/invalid/persistence errors。
- [x] 保持发送、入站 persist-before-ACK、状态推进、operation replay 和删除行为不变。

必须覆盖：

- [x] v6 真实旧 schema + 旧消息升级后 unread=0，迁移后新入站 unread>0，关闭/重开保持；Down 后消息仍在、表/索引消失、再 Up 正常。
- [x] 同号码跨 Line 合并；不同号码隔离；相同毫秒按 message ID；分页无重复/遗漏；并发前插不破坏边界。
- [x] exact remote identity，不把带/不带 `+` 的值合并；字母型地址可以列出。
- [x] watermark 重复/乱序不清除更晚 marker；同毫秒随机 ID 与并发新入站仍未读；删除未读/最新/最后消息后派生结果正确。
- [x] watermark token 的空值、超长、版本、base64url、message ID、unread ID/remote 不匹配均被稳定拒绝；另一标签页已清除的同一 token 幂等成功。
- [x] boundary 不存在、remote 不匹配、context cancellation 与数据库错误映射。

验证：

```bash
go test ./internal/storage/sqlite
go test ./internal/application/messaging
```

风险/回滚：migration 与 store 必须一起回退；Down 只删除 read 状态和新索引，不删除/重建 `sms_messages`。

## 4. WP2 — OpenAPI、HTTP handler 与生成客户端

依赖：WP1 的服务签名和错误语义稳定。

所有权：

- `api/openapi.yaml`
- `internal/api/httpapi/server.go` 与 handler tests
- OpenAPI/Web 生成物（只通过生成器）

步骤：

- [x] 先更新 OpenAPI：`SMSConversationSummary/ListResponse`、remote-only history 的可选 `readThroughToken`、read request、conversation list/read-state paths，以及 `GET /messages` 新过滤矩阵。
- [x] 更新 `Messenger` consumer interface、ListMessages presence validation、conversation list handler、read-state handler和稳定错误映射。
- [x] 会话 list 返回 conversation/message totals、capacity/nearCapacity；不得把号码放入 cursor 或 error/log。
- [x] read-state mutation 仅在实际删除 unread markers 时发布既有 `messages` topic；不新增 SSE payload 字段或第二个真相源。
- [x] 运行 `make generate`，检查 generated Go/TS/Zod/Query diff，没有手写后处理。
- [x] 覆盖 auth、CSRF、trusted-LAN、limit/cursor 显式空值、remote-only/paired/Line-only、404 boundary、204 idempotency、next cursor 和 response bounds。

验证：

```bash
make generate
go test ./internal/api/openapi
go test ./internal/api/httpapi
make verify-generated
```

风险/回滚：API source、Go handler、generated Go/Web 必须同一回滚点；禁止只回退一侧。

## 5. WP3 — 前端会话领域逻辑与页面骨架

依赖：WP2 generated Query/mutation contract。

所有权：

- `web/src/pages/Messages.tsx` 及必要的 `web/src/pages/messages/**` page-local components
- `web/src/messages/**` 非视觉 transforms/tests
- `web/src/global.css` 的 Messages 专属规则
- `web/src/pages/Messages.test.tsx`

步骤：

- [x] 删除旧“发送/联系人/历史表格”组合，构建桌面双栏与窄屏 list→detail 主从状态；不改变 route/AppShell。
- [x] 接入 generated conversation infinite query、remote-only history infinite query、Contacts 与 Managed Lines joins。
- [x] 实现会话摘要、稳定选择、cursor 加载更多、desktop 默认最新会话、mobile 初始列表和删除空会话后的选择恢复。
- [x] 实现历史正序气泡、较早消息顶部加载/滚动锚点、Line/时间/status 元信息、字母地址只读、历史 Line 回退和气泡更多/删除。
- [x] 实现 composer：最近 outbound Line 默认、无历史首条 eligible Line、不可用/已删除 Line fail closed、显式切换、1600 字上限和发送 busy/error。
- [x] 实现 `failed` 重新编辑与新 operation ID；`queued/unconfirmed` 无 retry；所有发送保持 mutation retry=false、无 optimistic success。
- [x] 实现新建短信对话框、联系人搜索/号码输入、临时会话，以及联系人管理抽屉/弹窗的新增/编辑/删除。
- [x] 实现自动已读 gate：detail active + document visible + 最新页成功渲染 + 原样返回的 `readThroughToken`；成功后失效 summary，失败保留角标。
- [x] 复用并按需要扩展现有 `smsStatusPresentation`、排序/映射 helpers；删除已不符合新会话身份的死 helper，不保留 Line+remote 双逻辑。

可观察测试：

- [x] 两 Line 同号码只显示一个会话，气泡各自显示正确 Line；不同号码分开。
- [x] 联系人名称/号码、预览、时间、未读 Badge、latest-created 排序；状态 update 不重排。
- [x] desktop 双栏；mobile list→detail→back；无全局横向 overflow 的 DOM 前置断言。
- [x] 当前 detail 成功加载才 mark read；hidden/unselected/error 不 mark；SSE 后历史显示再 mark。
- [x] 最近 Line 可用、不可用、已删除、无 outbound、无 eligible Line 和手动切换矩阵。
- [x] 新建临时会话不持久；首条 sent/failed/unconfirmed 形成会话；持久化前错误不制造空会话。
- [x] failed 回填但不发送；unconfirmed 无重发；删除最新/未读/最后消息；联系人 CRUD 与名称回退。

验证：

```bash
corepack pnpm --dir web test -- Messages
corepack pnpm --dir web test -- messages
corepack pnpm --dir web typecheck
corepack pnpm --dir web build
```

风险/回滚：`Messages.tsx`、page-local components 和 Messages CSS 作为一个 UI 回滚点；不要留下旧表格与新会话页并行的隐藏双实现。

## 6. WP4 — 浏览器回归与跨层场景

依赖：WP1–WP3 targeted tests green。

- [x] 扩展 synthetic Playwright fixtures，覆盖 conversation summary、remote-only history、read-state 和 contact/Line joins；所有号码和消息均为公开安全的合成值。
- [x] Desktop：双栏、切换、发送状态、Line 选择、load older、删除与 SSE 新短信刷新/自动已读。
- [x] Mobile：列表、进入全屏会话、返回、composer/键盘空间、触屏更多菜单、无意外 autofocus。
- [x] 两个 viewport 都检查 page root 无横向溢出；不记录或提交 screenshot/trace。
- [x] API/storage 集成覆盖 migration→handler→JSON round trip，确保生成 response 字段和数据库排序一致。

验证：

```bash
corepack pnpm --dir web e2e
go test ./internal/storage/sqlite ./internal/application/messaging ./internal/api/httpapi
```

## 7. WP5 — 架构、规范与完成事实

依赖：实现行为与 tests 稳定。

- [x] 更新 `docs/architecture.md`：remote-only 会话、summary/unread watermark 数据流、消息分页索引、HTTP/SSE不变量。
- [x] 把 active MVP plan 的本里程碑验收项更新为真实完成状态；按实际影响更新 sanitized handoff，不复制规范全文。
- [x] 更新 `.trellis/spec/core/backend/api-contracts.md`、`storage-and-migrations.md` 与 `.trellis/spec/web/frontend/hook-guidelines.md`/相关 state/quality 事实，移除“filter 必须 Line+remote”的陈旧规则。
- [x] 若实施没有改变批准设计，不额外发明 ADR 或产品范围；若有 material 设计变化，返回规划并重新取得审批。
- [x] 运行 `trellis-update-spec` 对受影响规范做 source-backed 检查。

验证：

```bash
make check-docs
```

## 8. WP6 — 全量质量门与交付检查

依赖：WP0–WP5 完成。

- [x] 先运行 targeted checks，再运行全量 gates；失败只修本任务归因问题。
- [x] 检查 source/generated/migration/docs/spec diff 一致，manifest seed placeholder 已移除，没有真实通信或隐私材料。
- [x] 由 `trellis-check` 独立检查 PRD、数据流、migration、generated drift、桌面/移动端、未读 race、Line fail-closed、重发安全与测试质量。
- [x] 对照所有 Acceptance Criteria，记录任何未运行的环境检查及原因。

```bash
make check-format
make verify-generated
make lint
make test
make security
make build
make check-docs
corepack pnpm --dir web e2e
git status --short
```

禁止把 container deploy、真实短信、RF、Host VoWiFi 或 HIL 当作本任务 gate。

## 9. 完成与回滚定义

- [x] PRD 每条 acceptance criterion 都能指向测试或可观察 UI 行为。
- [x] v6→v7→Down→v7 migration 保留全部短信；v7 unread 表初始为空，旧历史已读，新入站原子创建持久 marker。
- [x] OpenAPI、generated Go/Web、handler、service、store 与页面只存在一套匹配契约。
- [x] HTTP 是唯一权威；SSE payload 不变且仅触发 refetch。
- [x] 没有自动/乐观短信重发，没有不可用 Line 的静默回退。
- [x] desktop/mobile 主路径、分页、未读、删除和联系人均通过 synthetic tests。
- [x] 文档与 Trellis spec 描述完成后的真实实现，无私密数据或 HIL 声明。
