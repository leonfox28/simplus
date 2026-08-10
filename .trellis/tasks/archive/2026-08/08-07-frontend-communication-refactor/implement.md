# 前端与通信重构实施计划

## 1. 执行约束

- 本文件是规划，不代表已获准实现。只有用户审批最终规划后才运行 `task.py start`。
- 最终交付是一个原子 Web/API 版本；工作包是可验证检查点，不是独立 production 发布物。
- 不运行真实短信、电话、RF、eUICC、Host VoWiFi 或任何 HIL-1/HIL-2；本任务的验证限于单元测试、httptest、SQLite 临时库、浏览器 fixture 和 Simulator。
- `api/openapi.yaml`、迁移和生成器配置是源文件；禁止手改 generated Go/TypeScript。
- 开始每个工作包前检查工作树并保留用户已有修改。修改依赖版本、常量、route、生成路径或 Make target 前先全仓搜索。
- 子代理只能在满足依赖后领取不重叠的文件所有权；lockfile、OpenAPI、生成目录、AppShell 和共享 runtime 由单一 owner 串行修改。

## 2. 工作包与依赖

### WP0 — 建立基线与 durable decision

依赖：无。

- [x] 记录 `pnpm list/audit/why`、`web/dist` bytes/files、现有测试和构建结果。
- [x] 新增 ADR 0022，supersede ADR 0009 的 Umi Max/Pro Components 决定，但保留 React 19、Ant Design、单一前端栈、cookie/CSRF 和同源静态承载。
- [x] 在 active MVP plan 中登记新里程碑和原子迁移边界。
- [x] 在 `api/openapi.yaml` 与 `Makefile` 修改前确认当前生成物无 drift。

验证：

```bash
git status --short
make verify-generated
corepack pnpm --dir web test
corepack pnpm --dir web typecheck
corepack pnpm --dir web build
```

回滚点：纯文档/基线；不改变运行行为。

### WP1 — 定义分页与 realtime OpenAPI 契约

依赖：WP0。

- [x] 为 Messages/Calls 添加 `limit`、`cursor` 和 `nextCursor`；Messages 添加成对的 `lineId/remoteAddress` filter。
- [x] 定义统一 cursor 参数边界和 `PAGE_CURSOR_INVALID` 错误响应。
- [x] 新增 `RealtimeTopic`、`RealtimeAttention`、`RealtimeEvent` 和 `GET /api/v1/events` 的 `text/event-stream` 契约。
- [x] 确认所有新增 schema `additionalProperties: false`、数组/字符串有界，且不包含通信正文、号码、硬件身份或路径。
- [x] 生成 Go/Web 契约并检查 diff；生成 Web client 的目录/文件一次性登记到 `GENERATED_PATHS`。

验证：

```bash
make generate
make verify-generated
go test ./internal/api/openapi
```

回滚点：OpenAPI 与生成物一起回退；不得只回退一侧。

### WP2 — 实现 SQLite keyset pagination

依赖：WP1 的最终 cursor/response contract。

- [x] 在 messages 追加 migration，建立全局与 `(line_id, remote_address)` 的 `(created_at_unix_ms DESC, message_id DESC)` 索引。
- [x] 在 calls 追加 migration，建立 `(created_at_unix_ms DESC, call_id DESC)` 索引。
- [x] 实现版本化、长度受限的 opaque cursor encode/decode；共享算法放在最窄的可复用层，不让 HTTP、service 和 store 各自解析。
- [x] Repository/Service 接口接收 typed page request 并返回 items/next cursor；删除内部固定 50/100 假分页。
- [x] HTTP handler 校验 Messages filter 必须成对出现，并映射 stable 400 error。
- [x] 覆盖同毫秒 ID tie-break、第二页、末页、非法/旧版本 cursor、filter、并发前插、reopen、Up/Down migration。

验证：

```bash
go test ./internal/storage/sqlite
go test ./internal/application/messaging ./internal/application/calls
go test ./internal/api/httpapi
make verify-generated
```

风险文件/边界：

- `internal/storage/sqlite/messages.go`, `calls.go`, 两个 dataset migrations；
- application repository interface 的所有 fake；
- `api/openapi.yaml` 与 generated server interface。

回滚点：Down migration 只删除索引，不删除业务数据。

### WP3 — 实现 realtime hub、SSE 与发布源

依赖：WP1；可在 WP2 完成后合并验证，但不得与同一 OpenAPI/generated owner 并行。

- [x] 新建有界 realtime hub：typed topics、合并、慢 subscriber resync、取消和无阻塞 publish。
- [x] 实现 SSE handler：业务 API gate、管理员 cookie、headers、initial resync、JSON schema、heartbeat、flush、session 周期复验和 disconnect cleanup。
- [x] 让 `apiTimeout` 仅对 events endpoint bypass；调整 `simplusd` 全局 WriteTimeout，使 JSON 仍由 endpoint timeout 保护而 SSE 不被截断。
- [x] HTTP mutation 成功后发布对应 topic；发布发生在权威操作成功后、写响应前后语义保持一致。
- [x] `runSMSSync` 在持久状态变化时发布 `messages`，新入站附 `sms.received`。
- [x] hardware backend 使用 Agent `/v1/changes` 有界长轮询发布 `inventory/modems/lines`；异常退避且不改变 Agent 权限或 API。
- [x] Host VoWiFi reconcile 和现有 Mihomo/Call/Contact/Notification/eUICC mutation 接入相同 publisher。
- [x] 测试 payload 无正文、号码、身份、路径或 raw error；测试 slow client 不阻塞业务操作。

验证：

```bash
go test ./internal/application/realtime
go test ./internal/api/httpapi
go test ./cmd/simplusd
make check-format
make lint
```

风险文件/边界：`internal/api/httpapi/server.go` 的 timeout/recovery 顺序、`cmd/simplusd/main.go` 的 goroutine 生命周期、session 复验与 server shutdown。

回滚点：Hub/SSE 可作为一个提交整体回退；不能只恢复全局 WriteTimeout 而留下 stream endpoint。

### WP4 — 建立生成 Web client、runtime 与 QueryClient

依赖：WP1；与 WP2/WP3 合并生成物后执行。

- [x] 添加固定版本的 Hey API、Fetch client、Zod v4 与所需 TanStack Query generator 配置；输入只使用本地 `../api/openapi.yaml`。
- [x] 生成 types/SDK/Zod/query keys/options/infinite options，加入 drift gate。
- [x] 实现唯一 runtime：same-origin credentials、CSRF、AbortSignal、operation timeout、ApiClientError 与 401 signal。
- [x] 把现有 `hardwareSchema.ts` 接到生成结构 schema 之后，保留跨引用验证；删除重复的手写 API response 类型。
- [x] 配置 QueryClient query retry 与 mutation no-retry；实现集中 error translation。
- [x] 实现 EventSource decoder、topic-to-query mapping、initial resync、attention 去重、reconnect/session probe 和 cleanup。
- [x] 为现有页面暂时提供最薄 compatibility facade，只允许作为本任务内迁移桥；最终 WP9 必须删除。

验证：

```bash
corepack pnpm --dir web generate:api
corepack pnpm --dir web test
corepack pnpm --dir web typecheck
make verify-generated
```

风险文件/边界：`web/src/api/**`、生成 config、Makefile `GENERATED_PATHS`、root lockfile。该工作包由一个 owner 独占。

### WP5 — 用 Vite/React Router 建立应用壳

依赖：WP4 的 runtime/QueryClient contract。

- [x] 新增 Vite config：React plugin、`@` alias、`dist`、hash assets、`VITE_API_PROXY_TARGET` `/api` proxy（含 SSE streaming）。
- [x] 新增 `main.tsx`、AppProviders、BootstrapGate、AppRouter、AppShell 与集中 navigation metadata。
- [x] 使用 Ant Design `ConfigProvider/App/Layout/Menu/Drawer` 重建桌面与手机导航、用户菜单和 logout。
- [x] 用 Router replace/navigation 替代 Umi initialState/layout hooks 和手写 history/popstate。
- [x] 迁移 Login/Setup 门禁但保留 password-manager attributes、setup fragment/session 和必要 origin reload。
- [x] 更新 Vitest config/types、`run-sim.sh`、supervisor test 文案与启动参数，使它们不再假设 Umi。
- [x] 保持 `web/dist` 和 `cmd/simplusd/web.go` SPA fallback contract。

验证：

```bash
corepack pnpm --dir web test -- app Login Setup
corepack pnpm --dir web typecheck
corepack pnpm --dir web build
make test-dev-sim
```

风险文件/边界：认证 redirect loop、setup 未初始化状态、Vite host/port、SSE proxy、生产 deep-link fallback。

回滚点：Vite shell 与启动脚本同一提交回退；不要留下 `max dev` 或 `.umi` type include。

### WP6 — 建立最小 Ant Design 页面原语

依赖：WP5。

- [x] 从现有多页面使用提炼 `PageHeader`、`AsyncState` 和 AppShell；没有两个以上消费者的业务 UI 不抽象。
- [x] 定义桌面 table / 手机 card-list 的响应式约定、局部横向滚动和 action placement。
- [x] 定义统一 loading/empty/error/partial/disabled/busy 视觉状态。
- [x] 保持当前 token、中文文案、可访问 label、confirmation 与 sensitive value 约束。
- [x] 用真实 DOM 测试 desktop/mobile navigation 和 Drawer cleanup。

验证：

```bash
corepack pnpm --dir web test -- AppShell
corepack pnpm --dir web typecheck
```

### WP7 — 迁移核心切片 Modems -> Lines -> Messages

依赖：WP2、WP3、WP4、WP5、WP6 全部完成。

#### Modems

- [x] 用 Ant Design Table/Card/Modal/Form 替换 PageContainer/ProTable/ProDescriptions 等。
- [x] Managed Modems 与 candidates 使用共享 generated queries；candidate 只在对话框打开时查询。
- [x] RF mutation 只按返回对象窄更新/失效；IMEI 仍显式读取、默认隐藏、hide/reload/unmount 清除。
- [x] 保留 unsupported/unknown fail-closed、型号读取失败不回退和候选不自动添加测试。

#### Lines

- [x] 共享 Managed Lines、egress、VoWiFi queries；配置项仍分别 mutation，不合并身份/RF/出口/VoWiFi。
- [x] SSE/reconcile 替代手写 interval；只让 VoWiFi runtime query 在该页 active 时更新。
- [x] 保留 candidate reason、未配置出口禁用激活、partial runtime failure、端口/身份不泄漏测试。

#### Messages

- [x] 使用 infinite query 展示 recent history/会话并支持加载更多；会话查询传成对 filter。
- [x] Contacts 和 Managed Lines 使用共享 cache，不再每 5 秒重复加载。
- [x] send/delete/contact mutation 做精确 cache update 或失效；发送/删除失败必须保留可见状态。
- [x] 只在 `sms.received` attention 显示页面内提示；普通状态变化静默。
- [x] 网络结果未知时提示刷新历史且不要重复发送，不自动 retry mutation。

每个子切片验证：

```bash
corepack pnpm --dir web test -- Modems
corepack pnpm --dir web test -- Lines
corepack pnpm --dir web test -- Messages
corepack pnpm --dir web typecheck
```

并行规则：三页 UI 在共享 API/query contract 冻结后可由不重叠 owner 处理；`api/events.ts`、generated files、global CSS 与共享 components 仍由 foundation owner 串行合并。

### WP8 — 迁移其余页面

依赖：WP7 已验证核心模式。

- [x] Calls：游标加载、incoming attention、action state、DTMF/媒体 fixture、安全号码错误；mutation 不自动重试。
- [x] Dashboard：health/topology queries、partial error 与响应式 Statistic/Card。
- [x] Mihomo：拆分 core/config/runtime/subscription queries，保留不回退 direct 和长操作进度。
- [x] Notifications：typed Form/Table/Card、CRUD/test 错误、credential 不回显。
- [x] Settings：密码校验、成功后 session cache 清除与 replace 登录。
- [x] Setup/Login：补齐初始化、错误、session 与导航回归。
- [x] 按用户批准的暂缓例外隐藏模组页 eUICC 标签、提示、Profile 和操作，并确保桌面/紧凑视图不发起 eUICC 请求；后端/OpenAPI 能力保留。

验证：对每个页面运行 focused Vitest，然后运行全 Web test/typecheck/build。

并行规则：页面文件及其 feature/test 可在 WP7 pattern 冻结后分配给不同 owner；不允许各页建立自有 fetch、错误类、query-key taxonomy 或 responsive framework。

### WP9 — 删除旧栈与迁移桥

依赖：WP8 所有页面均已使用目标边界。

- [x] 删除所有 `@umijs/max`、`@ant-design/pro-components` imports、`web/config/`、Umi locales/runtime hooks 和 compatibility facade。
- [x] 删除旧手工 operation client/重复 guards，只保留 Simplus 特有跨字段 guard。
- [x] 从 package/lock/workspace overrides 中移除仅由 Umi/Pro 需要的依赖和 reviewed exceptions；逐条用 `pnpm why` 确认，不盲删共享安全 override。
- [x] 检查不存在 Umi、Pro Components、Ant Design 4、React Router 6、SWR 第二状态层或页面直接 fetch。
- [x] 记录最终 `pnpm list/audit/why`、依赖总数、`web/dist` bytes/files 与基线差异。

验证：

```bash
rg -n "@umijs|max |@ant-design/pro-|\.umi|ProTable|ProForm|ProLayout" web package.json pnpm-workspace.yaml pnpm-lock.yaml
rg -n "fetch\(" web/src --glob '*.{ts,tsx}'
corepack pnpm why @umijs/max @umijs/plugins @ant-design/pro-components antd react-router swr
corepack pnpm audit --prod --audit-level high
corepack pnpm audit --audit-level high
corepack pnpm --dir web test
corepack pnpm --dir web typecheck
corepack pnpm --dir web build
```

期望：前两类 legacy 搜索无命中（唯一 fetch owner 除外），audit 为 0 high/critical，无重复 Ant Design 主版本。

### WP10 — 浏览器回归、CI、文档与规范

依赖：WP9。

- [x] 添加 Playwright Chromium desktop/mobile fixtures；API 用脱敏 deterministic route fixtures，不需要真实硬件或外部通信。
- [x] 覆盖登录后导航、Drawer、Modem/Line/Message 核心路径、load more、SSE invalidation、全局 overflow 和 autofocus。
- [x] 为 E2E 建立明确 Make target/CI step；不让缺少浏览器的普通 targeted unit test 隐式下载工具。
- [x] 更新 `docs/architecture.md`、active plan、`docs/development.md` 和 ADR 链接。
- [x] 重写 `.trellis/spec/web/frontend/**` 为 Vite/Router/Antd/Query 的真实完成后规范；同步 backend API/storage/testing、infra generated/build 和 docs spec 中受影响的事实。
- [x] 运行 docs privacy 检查，确认没有真实通信、身份、路径、拓扑、日志或截图材料。

验证：

```bash
corepack pnpm --dir web e2e
make check-docs
make test-worktree-manifest
```

### WP11 — 全量质量门与交付检查

依赖：WP0–WP10 全部完成。

- [x] 运行 targeted checks 后再运行全量 gates。
- [x] 检查 `git diff` 中 source/generated/lock/docs 一致，迁移有 Down，旧栈无残留。
- [x] 核对所有 PRD acceptance criteria，并记录未运行的环境型检查及原因。
- [x] 由独立 check agent 检查 spec compliance、跨层数据流、错误/会话/SSE、隐私、重复依赖与测试质量。

```bash
make verify-generated
make check-format
make lint
make test
make security
make build
make check-docs
corepack pnpm --dir web e2e
git status --short
```

禁止把 container/HIL 命令作为本任务普通 gate；Docker build 只有在实际修改 Docker build contract 且环境可用时才追加。

### Runtime follow-up — 无头开发机 Setup/Login 循环

- [x] 根因归类为跨层契约与集成测试缺口：setup-session 的正常 401 被全局 interceptor 当成管理员 session 过期；两个 route guard 因而互相重定向。
- [x] 第二根因归类为迁移传播遗漏：旧登录页允许安装器预置管理员在 `uninitialized` 状态登录，并由后端签发 setup session；新门禁遗漏了这条首次登录路径。
- [x] 按请求 path 区分 setup、rejected login 与 protected administrator 401，并恢复首次登录与 direct setup 两条 setup-required 公共路径。
- [x] Vite `/api` proxy 保留浏览器 LAN Host，避免 setup completion 把远端浏览器重定向到 loopback API target。
- [x] 添加 runtime、Login/真实 SetupPage route、Vite config 和实际 proxy Host 回归；无头浏览器从 1.5 秒 154 次循环导航收敛为有界启动导航，普通入口稳定停在 `/login`，direct setup 稳定停在 `/setup`。
- [x] 将授权域、route-loop 与 dev proxy authority 约束写入 Web/infra code-spec 和 cross-layer checklist。

### Runtime follow-up — 桌面 Header 用户区错位

- [x] 根因归类为隐式布局假设与测试缺口：desktop 保留空 leading Flex，`space-between` 未把唯一可见的账号动作推到右侧；原测试只覆盖点击导航。
- [x] 只在 compact 模式渲染 leading group，并用独立 action group 的 `margin-inline-start: auto` 明确右对齐。
- [x] 桌面/手机 Playwright 分别验证 24px/12px 右边距、动作不越界和无全局 overflow；组件测试验证可访问账号入口及 Drawer 清理。
- [x] Brand 按用户确认统一为单行 `Simplus`，删除 `LAN Control Center` 副标题。

## 3. 完成定义

- [x] 所有 PRD acceptance criteria 可指向测试、生成检查、审计输出或可观察 UI 行为。
- [x] `design.md` 的 query/event/cursor/auth 数据流与实现一致。
- [x] 旧 Umi/Pro/手写通信层已删除，没有长期双栈。
- [x] 生产静态 Web 与 API 仍由同一 Simplus release 原子交付。
- [x] 没有执行未经授权的真实通信或硬件副作用。
