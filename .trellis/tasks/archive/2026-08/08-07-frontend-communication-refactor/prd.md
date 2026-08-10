# 重构前端与前后端通信

## Goal

把 Simplus Web 重构为一个依赖更少、边界显式、易升级的单一管理后台，并让页面通过统一、类型安全且可测试的数据层获得及时的后端状态。

用户价值：管理员在桌面和手机上都能稳定完成模组、线路、短信、电话及其他日常管理；状态变化无需依靠粗粒度整页轮询；依赖升级不再被 Umi/Pro Components 的重复框架链和旧路由版本阻塞。

## Background and Evidence

### Product boundary

- 产品是可信局域网内、单管理员使用的通信控制后台，中文优先；主要页面包括概览、模组、线路、短信、电话、Mihomo、通知和系统设置（`docs/product.md:5`, `web/config/routes.ts:1`）。
- 后端是持久业务状态的唯一权威；浏览器只保存页面快照和交互状态（`.trellis/spec/web/frontend/state-management.md:5`）。
- Web 只能使用 Modem、SIM、Line、Message、Call 等业务术语，不能暴露任意 AT/QMI、设备路径、Agent 协议或私有身份（`docs/architecture.md:261`）。
- 2026-08-04 的 ADR 0009 将 Web 一次性迁移到 React、Umi Max、Ant Design 和 Pro Components，并删除旧 Vue 双栈（`docs/decisions/0009-ant-design-pro-web.md:8`）。本任务会以新 ADR supersede 其中的 Umi/Pro 决定，但保留 React、Ant Design、单栈、cookie/CSRF 和 Go 静态承载边界。

### Current frontend and communication

- 当前前端是 React 19 + Umi Max + Ant Design 6 + Pro Components；`@tanstack/react-query` 已安装且插件已启用，但业务源码没有 query、mutation、query key 或统一缓存（`web/package.json:13`, `web/config/config.ts:10`）。
- Umi 的源码耦合主要位于 `web/config/config.ts`、`web/config/routes.ts` 和 `web/src/app.tsx`；业务页面没有直接导入 `@umijs/max`。Pro Components 则覆盖 10 个页面，主要使用 PageContainer、Card、Table、Form、Descriptions、LoginForm 和 StatisticCard。
- `web/src/api/client.ts` 是 1,072 行手写边界：类型部分来自生成的 `schema.d.ts`，但 endpoint、method、错误归一化和大量成功响应 guard 由手工维护（`web/src/api/client.ts:4`, `web/src/api/client.ts:88`, `web/src/api/client.ts:369`）。
- OpenAPI 3.0 是公共 HTTP 契约源并生成 Go server 与 TypeScript schema；当前没有生成浏览器 operation client（`api/openapi.yaml:1`, `web/package.json:8`）。
- API 已使用同源 cookie session、double-submit CSRF、稳定 `ApiError {code,retryable,reference?}` 和 `no-store`；前端目前把服务端错误压缩成普通 `Error(code)`，没有保留 retryable、status 或 reference（`web/src/api/client.ts:72`, `internal/api/httpapi/server.go:47`, `internal/api/httpapi/server.go:485`, `api/openapi.yaml:2690`）。
- 页面以 `useState/useEffect/useCallback` 各自持有服务端快照；Line 的 VoWiFi 运行态与短信页每 5 秒轮询（`web/src/pages/Lines.tsx:112`, `web/src/pages/Lines.tsx:150`, `web/src/pages/Messages.tsx:12`）。仓库没有浏览器 SSE/WebSocket。
- Managed Lines 会被线路、短信和电话页重复读取；mutation 后通常整页 `load()`，没有共享失效约定（`web/src/pages/Lines.tsx:117`, `web/src/pages/Messages.tsx:12`, `web/src/pages/Calls.tsx:7`）。
- 认证门禁只在 Umi 初始化/切页集中处理；普通业务请求的 401 没有统一恢复或跳转（`web/src/app.tsx:11`, `web/src/api/client.ts:113`）。
- 页面质量不均：Lines、Modems、Mihomo 较完整，Calls、Notifications、Settings 和部分 Messages 仍是密集页面代码，若干异步失败缺少页面级反馈（`web/src/pages/Calls.tsx:7`, `web/src/pages/Notifications.tsx:8`, `web/src/pages/Settings.tsx:5`）。

### Dependency and security evidence (2026-08-07)

- `pnpm why` 显示当前同时存在 Pro Components `3.1.14-6` 与 `2.8.10`、Ant Design `6.5.3` 与 `4.24.16`。第二套由 `@umijs/max -> @umijs/plugins` 引入；Vite 也被 Umi bundler 间接控制。
- 直接使用的 Pro Components `3.1.14-6` 属于 npm `beta` 标签，用于 Ant Design 6，并额外依赖 `@umijs/route-utils`、SWR、Emotion、DnD Kit 等；npm `latest` 仍为只声明 Ant Design 4/5 peer 的 2.8.10。
- 当前 production 与完整 audit 均为 0 high、0 critical，但仍有 3 low、4 moderate。production tree 约 1,486 项，完整 tree 约 1,578 项；根工作区维护 11 条安全 override 和 4 条带适用范围说明的 advisory ignore。提交 `1eb1cbc` 专门修补了这条审计依赖链。
- Umi 4 固定 React Router 6；当前被忽略的 Router advisory 中存在要求升级新主版本或没有 6.x 修复版本的情况。直接依赖新受支持主版本可解除框架约束。
- 迁移前 `web/dist` 有 29 个文件、约 3,004,106 bytes；这些数字作为迁移后的同口径对比基线，而不是孤立的硬性大小指标。

### Current verification

- `corepack pnpm --dir web test` 通过：10 个测试文件、73 个测试；存在已知 jsdom CSS/伪元素告警。
- `corepack pnpm --dir web typecheck` 通过。
- 页面测试当前覆盖 Login、Modems、Lines 和布局菜单；Dashboard、Messages、Calls、Mihomo、Notifications、Settings、Setup 尚无各自完整页面测试。
- `internal/api/httpapi/server.go:284` 把所有 API 包在 buffered timeout 中，`internal/api/httpapi/server.go:380` 的 writer 不能流式 flush；`cmd/simplusd/main.go:267` 的 server WriteTimeout 也会截断长连接。SSE 实现必须明确处理这两个边界。
- Messages/Calls 当前固定取最近 50/100 条。两库已有 created-at + stable-ID 索引；会话筛选还需要 messages 的 `(line_id, remote_address, created_at, message_id)` 匹配索引。

## In Scope

1. 将 Web 工程栈替换为 Vite + React Router Declarative Mode + 直接使用 `antd` 包 + TanStack Query，移除 Umi Max 和全部 Pro Components。这里的“直接使用 Ant Design”与此前所称“Ant Design 基础组件”是同一方案，不存在额外的基础版包。
2. 用显式 AppProviders、BootstrapGate、AppRouter 和响应式 AppShell 重建公共/受保护路由、setup/auth 门禁、桌面侧栏和手机 Drawer。
3. 迁移全部现有页面；首个验证切片依次为 Modems -> Lines -> Messages，随后迁移 Calls、Dashboard、Mihomo、Notifications、Settings、Setup、Login。
4. 由 OpenAPI 生成 Fetch SDK、TypeScript types、Zod schemas 和 TanStack Query keys/options/infinite options；保留必要的 Simplus transport、错误和 topology 跨字段验证。
5. 以 TanStack Query 统一 server snapshot、取消、retry、cache、mutation 同步和跨页面失效；不引入 Redux/Zustand 或第二套服务端状态库。
6. 新增鉴权 SSE `/api/v1/events`，只发送资源失效 topic 和新短信/来电 attention hint；HTTP 继续承载权威查询与 mutation。
7. 为 Messages/Calls 增加稳定 keyset cursor pagination；Messages 支持按 Line + remote address 会话筛选。
8. 补齐 backend、API、storage、frontend boundary/page 和 Playwright desktop/mobile 自动化回归。
9. 更新 Make/CI/开发脚本、generated drift、安全审计、superseding ADR、architecture、active plan、development docs 和 Trellis specs。

## Out of Scope

- 用 Refine、React Admin、Next.js 或另一后台元框架替代 Umi；切换到另一套视觉组件库或进行品牌重设计。
- WebSocket、GraphQL、gRPC-Web、SSR/SSG、React Router Framework Mode、service worker、离线写队列或持久化浏览器 cache。
- 公网/CORS 产品化、多管理员、多租户、角色权限、系统级浏览器通知或后台常驻推送。
- 新增或验证真实蜂窝短信、电话、eUICC、RF、Host VoWiFi 或其他 HIL 能力。
- 修改 Agent/netd 为任意命令边界，暴露硬件路径、通信身份、原始日志/抓包或私有拓扑。

## Key Decisions

- **Frontend stack**：已确认 Vite + React Router Declarative Mode + 直接使用 `antd` + TanStack Query；页面显式导入 `Layout/Table/Form/Card` 等所需组件，彻底移除 Umi/Pro Components，不以另一个元框架替代。
- **Visual/UX continuity**：继续使用 Ant Design 视觉语言、中文优先、常见扁平左侧导航；桌面表格与手机 Card/List 双形态，宽表只在自身容器滚动。
- **Capability visibility**：已进入产品界面的未装配/不可用功能通常保持可见，并显示禁用原因；eUICC 是用户明确批准的暂缓例外，在功能完整实现前不在模组页显示标签、提示、Profile 或操作，也不由该页面发起 eUICC 查询，但保留后端与 OpenAPI 能力边界。
- **Communication**：HTTP snapshot/mutation + authenticated SSE invalidation。SSE 是 hint，不是业务数据或真相源；断线重连以 resync + HTTP refetch 收敛。
- **Attention behavior**：只有新短信和来电产生明显页面内提示；普通配置/运行态变化静默刷新。
- **Data client**：Hey API + Fetch + Zod + TanStack Query 生成链；生成结构验证后仍执行必要的 topology cross-reference guard。
- **Pagination**：Messages/Calls 使用 `(createdAt, stable ID)` opaque cursor，不使用 offset；Messages 的会话 filter 必须成对提供 Line 与 remote address。
- **API compatibility**：允许在 `/api/v1` 内调整当前契约；Web 与 `simplusd` 原子交付，不发布兼容旧 Web 的双 API 或长期双通信层。
- **Migration order**：全面、分阶段、每阶段可验证；核心切片 Modems -> Lines -> Messages 优先，最终覆盖所有页面并删除旧栈。
- **Safety**：mutation 默认不自动 retry；短信/电话/硬件动作继续依赖 operation ID、服务端幂等、未知结果提示和明确用户意图。

## Requirements

- **R1 — Explicit frontend platform**：使用已确认目标栈并直接从 `antd` 导入所需 UI 组件；构建、路由、provider、auth gate 和 proxy 均为显式直接依赖/源码；不得保留 Umi、Pro Components、重复 Ant Design 主版本或未使用的第二状态库。
- **R2 — Responsive usable UI**：所有主要工作流在桌面和手机均可完成；无全局横向溢出、导航错位、切页意外 autofocus/弹键盘；加载、空、错误、部分失败、禁用和 busy 状态可见。
- **R3 — Preserve product/security boundaries**：保持后端权威、OpenAPI-first、cookie session、CSRF、稳定错误码、trusted-LAN 和 typed hardware boundaries；不把底层或私有信息暴露给 Web/SSE。
- **R4 — Generated typed data boundary**：公共 operation、types、request/response schema 和 query identity 从 OpenAPI 生成；页面不得直接 fetch、复制 API model 或自行解析相同 payload。
- **R5 — Consistent request semantics**：取消、超时、401、403、离线、retryable error、invalid success response 和 mutation feedback 有统一可测试行为；用户不直接看到内部 code/raw error。
- **R6 — Backend-authoritative synchronization**：每类 server state 有唯一 query identity、刷新条件和 mutation 同步规则；SSE 只使 active queries 失效，隐藏页不因事件产生无意义请求。
- **R7 — Realtime robustness**：SSE 鉴权、heartbeat、session 复验、disconnect cleanup、bounded backpressure 和 reconnect resync 明确；慢浏览器不能阻塞业务 mutation/background sync。
- **R8 — Stable pagination**：Messages/Calls 的 cursor、排序、filter、索引和响应一致；相同时间戳、并发新记录、非法 cursor 和末页行为确定。
- **R9 — Maintainable feature boundaries**：复杂页面按真实消费者拆分 page/feature/query/view；共享错误、query keys、event decode、AppShell 和响应式模式只有一个 owner，避免空洞抽象。
- **R10 — Verifiable atomic migration**：每个实施检查点保持 test/typecheck/build；最终不长期维护新旧栈/通信层，Web/API/迁移有明确回滚点。
- **R11 — Security and documentation gate**：完整树与 production tree 均为 0 high/critical；较低等级 ignore 有 advisory、适用范围与复查条件；durable ADR、architecture、active plan、development docs 和 Trellis specs 与完成后代码一致。

## Acceptance Criteria

### Stack and build

- [x] `rg`/`pnpm why` 证明源码、package 和 lockfile 不再包含 Umi Max、Umi plugins、Pro Components、`.umi` 或 Ant Design 4；没有页面直接调用 `fetch`。
- [x] Vite dev/build 使用现有 host/port/proxy 环境，输出 `web/dist`；生产 `simplusd` 仍能承载根路径、hashed assets 和任意 SPA deep link。
- [x] desktop Sider、mobile Drawer、公共 Login/Setup 和受保护 routes 均有可观察导航测试；不可用入口仍显示原因。暂缓的 eUICC 例外在桌面/手机模组页均不显示且不发起查询。
- [x] 最终记录依赖树与构建产物 before/after；依赖显著收敛且无重复 UI/router/server-state 主栈。

### API, state and errors

- [x] OpenAPI 一次生成 Go server contract、Fetch SDK、types、Zod schema 和 Query options/keys；`make verify-generated` 覆盖全部生成路径且无 drift。
- [x] 每类 server state 在 `design.md` 的查询矩阵中有唯一 owner、identity、freshness 与 mutation/SSE 失效规则；跨页面 Managed Lines/Modems 读取复用 cache。
- [x] query cancellation 进入 fetch AbortSignal；GET 只在 network/`retryable=true` 时有界 retry，mutation 全局不自动 retry。
- [x] ApiClientError 保留 kind/code/retryable/status/reference；401 清除私有 cache 并 replace 登录，403 保持操作错误；invalid success、timeout、offline 均有统一中文行为。
- [x] IMEI 仍默认隐藏、显式读取、hide/reload/unmount 清除；浏览器 storage 不保存 session、cache、身份或业务数据。

### SSE and background updates

- [x] `/api/v1/events` 通过 setup/admin/trusted-LAN gate，正确设置 `text/event-stream`/`no-store`，可 flush heartbeat，并绕过 buffered JSON timeout 而不取消其他 endpoint timeout。
- [x] Event payload 只含 bounded topic/attention，不含短信正文、号码、SIM/设备身份、路径、命令、raw error 或诊断材料。
- [x] Modem/Line mutation、Agent generation、SMS background sync、Call/VoWiFi/Mihomo/Contact/Notification/eUICC 变化发布正确 topic；新短信/来电之外的变化不弹提示。
- [x] slow subscriber、断线、session 失效、服务重启和丢失 hint 均通过 bounded hub + cleanup + reconnect resync 收敛；业务写入不被 stream 阻塞。
- [x] 旧 Messages 5 秒全页轮询被移除；VoWiFi/Mihomo fallback 只作用于挂载中的窄运行态 query，hidden/unmounted 页面无周期流量。

### Pagination and UI flows

- [x] Messages/Calls 支持默认/最大 limit、next cursor、加载更多、空/末页和非法 cursor stable error；同时间戳及并发前插不会造成边界重复/漏读。
- [x] Messages 会话 filter 仅接受 Line + remote address 成对组合，并使用匹配复合索引；migration Up/Down/reopen 不丢业务记录。
- [x] Modems -> Lines -> Messages 核心桌面/手机流程覆盖 candidate、不可用原因、mutation、部分失败、cursor 和 SSE 更新。
- [x] Calls、Dashboard、Mihomo、Notifications、Settings、Setup、Login 均有页面级回归；Playwright 验证 desktop/mobile 无全局 overflow、Drawer 错位或意外 autofocus。

### Quality, security and docs

- [x] Web test/typecheck/build/E2E、backend targeted tests、`make verify-generated/check-format/lint/test/security/build/check-docs` 通过；已知非致命 jsdom warning 单独报告。
- [x] production 与完整 pnpm audit 均为 0 high/critical；`make security` 的 moderate gate 通过，剩余 ignore 均有可审查理由。
- [x] 新 ADR supersede 0009 的 Umi/Pro 决定；architecture、唯一 active plan、development docs 和 `.trellis/spec/**` 描述新的真实架构。
- [x] 所有测试仅使用 synthetic fixture、临时 SQLite、httptest、Playwright mock 或 Simulator；未执行真实短信/电话、RF、持久 modem 写或 HIL。

## Risks and Deferred Items

- 换壳与换数据层回归面大：通过核心切片、阶段 green、existing security tests 和浏览器回归缓解。
- SSE 与当前 timeout/write deadline 冲突：必须先用 handler/server 测试证明 flush、heartbeat、shutdown，再接入页面。
- 生成 schema 不表达所有跨引用不变量：保留 `hardwareSchema.ts`，不以 Zod 结构验证替代领域 guard。
- 新工具本身增加依赖：只引入能取代手写复杂度或提供关键回归的直接依赖，并以同口径 before/after 和 audit 验证。
- 系统级浏览器通知、durable event replay、SSR、离线写入和真实硬件验证延期；它们不影响本次管理后台 MVP 行为。
