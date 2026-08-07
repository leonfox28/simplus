# 前端与通信重构设计

## 1. 设计状态

- 状态：实现与全量验证已完成，等待归档。
- 交付形态：一个同时包含 Web 静态资源和 `simplusd` API 的原子版本，没有新旧双栈兼容版本。
- 已实现栈：Vite + React Router 8.3 Declarative Mode + 直接使用 `antd` 包 + TanStack Query；Umi Max 与全部 Pro Components 已移除。

## 2. 问题重述与设计原则

需要解决的不是“把 Umi 配置改写成另一份配置”，而是让一个单管理员局域网控制台拥有可读、显式、可升级的页面壳和通信边界。

基本事实：

1. Go API 是持久状态唯一权威，浏览器只持有快照与交互状态。
2. 需要实时性的内容是变化提示，不需要在浏览器中复制第二套事件真相源。
3. Simplus 页面数量有限，不需要另一个后台元框架替代 Umi。
4. API、Web、SQLite 和文档会同时变化，必须保留类型、安全和隐私边界。

因此采用以下原则：

- 路由、provider、权限门禁、构建和代理均使用显式源码配置。
- Ant Design 提供基础交互；项目薄组件只抽取至少有多个真实消费者的页面模式。
- HTTP 继续承载查询和 mutation；SSE 只通知资源失效与少量需要关注的事件。
- OpenAPI 生成 SDK、类型、Zod schema 和 TanStack Query options/keys；手写代码只保留 Simplus 特有的 transport、错误和跨字段验证。
- mutation 不自动重试；有外部副作用的短信、电话和硬件动作继续依赖服务端幂等与明确用户意图。

术语说明：“直接使用 Ant Design”和“使用 Ant Design 基础组件”在本设计中完全等价，都是指从 `antd` 显式导入 `Layout`、`Menu`、`Drawer`、`Table`、`Form`、`Card` 等所需组件。不存在另一个“Ant Design 基础版”依赖。Ant Design Pro 是建立在 Ant Design 之上的后台脚手架/方案，Pro Components 则提供 ProTable、ProForm、ProLayout 等更高层封装；本任务移除后两者。

## 3. 总体架构

```text
Browser
  └─ Vite-built React SPA
       ├─ React Router (declarative routes + protected layout)
       ├─ Ant Design App / ConfigProvider
       ├─ TanStack QueryClient
       ├─ generated OpenAPI SDK + Zod validation
       └─ EventSource /api/v1/events
              │                 │
              │ JSON HTTP       └─ invalidation/attention hints only
              ▼
        simplusd httpapi
          ├─ authenticated OpenAPI handlers
          ├─ bounded in-memory realtime hub
          ├─ application services
          └─ SQLite / typed Agent / typed netd boundaries
```

SSE 不携带短信正文、电话号码、SIM/设备身份、硬件路径、诊断材料或命令。收到事件后，前端只让对应的 TanStack Query 失效，再通过现有鉴权 HTTP API 获取权威快照。

## 4. 前端工程结构

完成后的结构以实际边界为单位，没有泛化 store 或设计系统：

```text
web/
├── index.html
├── vite.config.ts
├── openapi-ts.config.ts
├── playwright.config.ts
├── src/
│   ├── main.tsx
│   ├── app/
│   │   ├── AppProviders.tsx
│   │   ├── AppRouter.tsx
│   │   ├── AppShell.tsx
│   │   ├── BootstrapGate.tsx
│   │   ├── RealtimeBridge.tsx
│   │   ├── auth.ts
│   │   └── navigation.tsx
│   ├── api/
│   │   ├── generated/          # OpenAPI 生成；禁止手改
│   │   ├── runtime.ts          # fetch、cookie、CSRF、timeout、错误归一化
│   │   ├── errors.ts
│   │   ├── events.ts           # 事件 schema 解码与 query tag 映射
│   │   ├── queryClient.ts
│   │   ├── session.ts
│   │   ├── setupClient.ts
│   │   └── hardwareSchema.ts   # 继续拥有拓扑跨引用验证
│   ├── components/Page.tsx     # PageHeader/AsyncState/响应式集合薄组件
│   ├── calls|messages|mihomo/   # 已有可复用非视觉领域逻辑
│   ├── pages/
│   ├── test/
│   └── global.css
├── e2e/
└── dist/
```

约束：

- 不保留 `web/config/`、`.umi/` 类型或 Umi runtime hooks。
- 路由在 `AppRouter.tsx` 显式列出；没有文件系统路由或 React Router Framework Mode。
- Vite 保持 `dist/` 输出、hash 静态资源和 `/api` 开发代理；代理连接 loopback API 但不改写浏览器的 trusted-LAN Host，使 setup completion 返回远端浏览器实际可达的管理地址。生产继续由 `cmd/simplusd/web.go` 承载 SPA fallback。
- 使用现代 evergreen 浏览器基线，不增加 legacy/polyfill 插件；现有 React 19 + Ant Design 6 已经是现代浏览器栈。

## 5. 路由、认证与应用壳

### 5.1 路由

- 公共页面：`/login`、`/setup`，使用独立全屏壳。
- 受保护页面：`/dashboard`、`/modems`、`/lines`、`/messages`、`/calls`、`/mihomo`、`/notifications`、`/settings`。
- 未知路径重定向到 `/dashboard`；导航使用 `Link/NavLink/useNavigate`，不再手工 `pushState` 或派发 `popstate`。

### 5.2 BootstrapGate

`BootstrapGate` 明确编排两类状态：

1. 读取 setup status；需要初始化时允许 `/login` 与 direct `/setup`，普通根路径/受保护路径先 replace 到 `/login`。管理员登录成功后由后端签发受限 setup session，页面按缓存中的 setup status replace 到 `/setup`。
2. 已初始化时，受保护路由读取管理员 session；无会话时 replace 到 `/login`。

全局 transport 只对受保护管理员请求的 401 发布内存内 `session-expired` 信号。`/api/v1/setup/*` 使用独立 setup cookie，其 401 保留为 setup 授权错误；`/api/v1/auth/login` 的 401 保留为凭据错误。Provider 收到真正的管理员 session 过期后取消并清空私有 query cache，再 replace 到登录页。403 CSRF 错误保留为操作错误，不误判为会话过期。

除 HTTPS/setup 完成后确实可能改变 origin 的流程外，普通登录、登出和导航不再使用 `window.location.replace`。

### 5.3 响应式 AppShell

- 桌面：Ant Design `Layout.Sider + Header + Content`，固定扁平左侧导航。
- 手机：Header + `Drawer` 导航；切页后自动关闭 Drawer，但不自动聚焦表单。
- 不可用能力仍显示入口和禁用原因，不能因为后端未装配就让功能从导航消失。
- `PageHeader`、统一空/错/加载状态和响应式集合是项目薄组件；业务字段、动作和表单保持在 feature/page 内。
- 桌面数据密集页使用 `Table`；手机使用 Card/List 形态。确需宽表时只在表格容器内滚动，页面根节点不得横向溢出。
- 继续使用当前 Ant Design token 和中文优先文案；本任务不更换品牌或视觉组件库。

## 6. OpenAPI 生成与请求边界

### 6.1 生成链

`api/openapi.yaml` 仍是唯一公共契约源。`@hey-api/openapi-ts` 配置生成：

- Fetch client；
- SDK 与 TypeScript 类型；
- Zod v4 request/response/definition schemas；
- TanStack Query v5 query keys、query options、mutation options 和游标接口的 infinite-query options。

生成目录加入 `Makefile` 的 `GENERATED_PATHS`，`make generate` 和 `make verify-generated` 同时覆盖 Go 与 Web 输出。旧的 `openapi-typescript -> schema.d.ts` 路径和 1,072 行手写 operation client 在迁移完成后删除。

官方依据：

- Hey API Fetch client 支持 runtime config、interceptor、response validation 和原始 Response 访问：<https://heyapi.dev/docs/openapi/typescript/clients/fetch>
- Zod 插件生成 request、response 与 reusable definition schema：<https://heyapi.dev/docs/openapi/typescript/plugins/zod>
- TanStack Query 插件生成 query keys/options，并可从分页参数生成 infinite options：<https://heyapi.dev/docs/openapi/typescript/plugins/tanstack-query>

### 6.2 手写 runtime 的唯一职责

`src/api/runtime.ts` 负责：

- 相对同源 `/api/v1` 请求和 `credentials: same-origin`；
- mutation 从 `simplus_csrf` cookie 附加 `X-Simplus-CSRF`；
- 合并 TanStack Query 的 `AbortSignal` 与操作超时；
- 把 transport/HTTP/invalid-response/timeout/abort 统一为 `ApiClientError`；
- 按请求 path 区分授权域，只在受保护管理员请求 401 时通知 session boundary；
- 保留原始 HTTP status、稳定 `code`、`retryable` 和可选 `reference`。

页面不得调用 `fetch`，不得读取未经生成 schema/专用 guard 验证的网络 JSON。生成 Zod 负责结构与 OpenAPI 约束；`hardwareSchema.ts` 继续负责唯一 ID、引用关系、generation 和 capability subset 等 OpenAPI 无法表达的拓扑不变量。

### 6.3 错误显示

`ApiClientError` 至少包含：

```text
kind: transport | http | invalid-response | timeout | aborted
code: stable uppercase code
retryable: boolean
status?: number
reference?: string
```

共享中文映射只翻译稳定 code；未知错误显示通用说明和可选 reference，不显示 `err.Error()`、响应正文或浏览器网络异常文本。

## 7. TanStack Query 状态模型

### 7.1 全局策略

- Query 最多对网络错误或 `retryable=true` 做两次指数退避；401、403、4xx、invalid-response 不重试。
- Mutation 默认 `retry: false`。短信、拨号、RF、VoWiFi、eUICC 和通知测试均不得自动重放。
- Query function 必须把 `signal` 传入生成 SDK；卸载、换筛选和登出会取消请求。
- SSE 到达后只 `invalidateQueries({refetchType: 'active'})`；未挂载页面只标记 stale，不产生隐藏页流量。
- 不使用 localStorage/sessionStorage 持久化 query cache、会话、IMEI 或任何业务数据。
- 操作进度、Modal/Drawer、草稿和已揭示 IMEI 等临时状态留在页面本地。

### 7.2 查询与失效矩阵

| 资源 | 权威查询 | Query 身份 | 主动刷新/失效 |
| --- | --- | --- | --- |
| Setup | `getSetupStatus` | operation key | setup mutation 后；应用启动 |
| Session | `getAuthSession` | operation key | 登录/登出/改密；401 清除 |
| Health | `getSystemHealth` | operation key | 页面进入、窗口重新聚焦、`system` |
| Managed Modems | `listManagedModems` | operation key | add/RF 后窄更新或失效；`inventory`,`modems` |
| Modem candidates | `listModemCandidates` | operation key | 打开添加对话框；`inventory` |
| Managed Lines | `listManagedLines` | operation key | add/update 后；`inventory`,`lines` |
| Line candidates | `listLineCandidates` | operation key | 打开添加对话框；`inventory`,`lines` |
| Egress/VoWiFi | 各自 operation key | 按 Line 或列表 | mutation、10 秒 reconcile 变化、`vowifi`,`mihomo` |
| Messages | infinite key + `lineId/remoteAddress` filter | 筛选和游标参数 | send/delete/background sync、`messages` |
| Calls | infinite key | 游标参数 | dial/action/incoming、`calls` |
| Contacts | list operation key | operation key | create/update/delete、`contacts` |
| Mihomo | core/config/runtime/subscription 各自 key | operation + path/query | 相应 mutation、`mihomo`；运行态页面可有有界 fallback poll |
| Notifications | list operation key | operation key | CRUD/test、`notifications` |

生成 query key 是具体请求身份；SSE topic 到 query tag/prefix 的映射集中在 `api/events.ts`，页面不得各自维护一套事件解释。

## 8. 游标分页契约

### 8.1 Messages

`GET /api/v1/messages` 增加：

- `limit`：默认 20，范围 1–50；
- `cursor`：可选、版本化的有界 opaque base64url 值；
- `lineId` 与 `remoteAddress`：必须同时出现或同时省略，用于一条会话的历史。

响应保留 `messages/totalCount/capacity/nearCapacity`，增加可选 `nextCursor`。列表固定以 `(created_at_unix_ms DESC, message_id DESC)` 排序。读取 `limit + 1` 条决定是否产生下一游标，不使用 offset。

messages 数据库追加迁移，为全局历史与 `(line_id, remote_address)` 会话历史建立与排序完全一致的索引；Down 只删除新索引，不改业务记录。

### 8.2 Calls

`GET /api/v1/calls` 同样接受 `limit/cursor`，响应 `calls` 加可选 `nextCursor`，固定以 `(created_at_unix_ms DESC, call_id DESC)` 排序。calls 数据库追加对应索引迁移。

### 8.3 Cursor 规则

- 游标只编码版本、时间与稳定业务 ID，不包含电话号码、正文或硬件身份。
- 解码、版本、长度或 ID 不合法返回 `PAGE_CURSOR_INVALID` / 400。
- 相同时间戳由稳定 ID 决定唯一顺序；并发插入不会让翻页重复或跳过游标之前的记录。
- 前端用生成的 infinite-query options 实现“加载更多”，SSE 失效时重新校准第一页；不把游标持久化到浏览器存储。

## 9. SSE 实时失效通道

### 9.0 方案比较

| 方案 | 适合点 | 本项目中的取舍 |
| --- | --- | --- |
| 定时轮询 | 实现最少 | 来电要低延迟时必须缩短周期，会让 Messages/Calls/VoWiFi 等页面持续重复请求；仅保留为窄运行态故障降级 |
| SSE / EventSource | 标准单向 server push、文本事件、浏览器自动重连、沿用同源 HTTP/cookie | **采用**；当前需求正是后端通知浏览器“资源变了”，所有写操作仍走 HTTP |
| WebSocket | 双向、高频、可传二进制 | 当前浏览器没有必须经长连接上行的动作；引入后会重复 HTTP 已有的鉴权、错误、幂等和 mutation 协议 |
| Fetch streaming/自定义长轮询 | 可完全自定义 status/header/帧 | 能力有用但需要自建更多客户端协议与重连逻辑；当前 EventSource 足够 |

因此 SSE 是当前约束下复杂度、延迟与可靠性的最佳折中，不是所有未来场景的绝对答案。若以后出现高频双向控制，才重新评估 WebSocket；浏览器通话音频也应走专用媒体机制，而不是 SSE。

### 9.1 公共契约

新增鉴权 `GET /api/v1/events`，Content-Type 为 `text/event-stream`。OpenAPI 声明流 endpoint，并定义可生成类型与 Zod schema 的 `RealtimeEvent`：

```text
topics: 1..N of system | inventory | modems | lines | vowifi |
        messages | calls | contacts | mihomo | notifications | euicc
attention?: sms.received | call.incoming
```

Payload 不含业务正文。SSE frame 可以有进程内递增 `id`，但不提供 durable replay；断线重连后服务端先发一个 ready/resync frame，前端使所有当前活跃查询失效，因此漏掉中间 hint 不会造成持久错误状态。

### 9.2 Hub 与背压

- `internal/application/realtime` 提供有界进程内 publisher/subscriber。
- 发布操作不得阻塞 HTTP mutation、SMS 同步或 runtime reconcile。
- 相邻 topic 可合并；subscriber 落后时退化为一次全量 `resync`，而不是无限堆积事件。
- `attention` 只控制当前浏览器的提示；即使被合并/丢弃，资源 topic 仍保证重新查询，业务记录不会丢失。

### 9.3 连接生命周期

- EventSource 使用同源管理员 cookie；GET 不要求 CSRF。
- handler 在建立连接时通过 `requireBusinessAPI`，并在 heartbeat 周期重新验证 session；失效后关闭流。
- 每 15 秒发送注释 heartbeat，并建议客户端 3 秒重连。
- `apiTimeout` 对 `/api/v1/events` 明确 bypass，因为现有 `bufferedResponse` 无法 flush；其他 JSON endpoint 继续保留原超时。
- `simplusd` HTTP server 不再使用会截断长连接的全局 WriteTimeout；JSON endpoint 仍由 `apiTimeout` 控制，SSE handler 自己控制 heartbeat、认证和 request context。
- 断开、页面隐藏、登出或 provider 卸载必须关闭 EventSource；重连错误触发一次 session probe，不进行高频重连风暴。

### 9.4 事件来源

| 来源 | 发布 topic/attention |
| --- | --- |
| Modem/Line/RF/egress/VoWiFi HTTP mutation 成功 | 对应 `modems`,`lines`,`inventory`,`vowifi` |
| Agent `/v1/changes` 长轮询观察 generation 变化 | `inventory`,`modems`,`lines` |
| SMS 后台 sync 有持久状态变化 | `messages`；新入站另带 `sms.received` |
| Message send/delete | `messages` |
| Call dial/incoming/action | `calls`；新入站另带 `call.incoming` |
| Host VoWiFi reconcile 周期或显式动作 | `vowifi` |
| Mihomo/notification/contact/eUICC mutation | 对应 topic |

只有“新短信”和“来电”触发前端明显提示；普通配置/状态变化静默刷新。未来真实电话 backend 接入时必须从同一 typed publisher 发布，不能另造 Web 协议。

## 10. 页面迁移

### 10.1 首个核心切片

首批按 `Modems -> Lines -> Messages` 迁移，因为三者共同验证：

- 共享 Managed Line/Modem query；
- mutation 后的缓存同步；
- 动态 inventory 与 VoWiFi 状态；
- 消息游标与 SSE 后台同步；
- 桌面表格和手机卡片两种布局；
- capability-driven、IMEI 临时展示和不可用原因等安全边界。

### 10.2 后续页面

在核心切片稳定后迁移 Calls、Dashboard、Mihomo、Notifications、Settings、Setup 和 Login。每个页面必须显式覆盖 loading、empty、error、disabled、mutation busy 和会话失效状态；不把旧的一行式页面机械搬入新壳。

## 11. 迁移与发布策略

本任务保留一个父任务和一个原子发布，不拆成可独立发布的子任务：OpenAPI、生成器、lockfile、AppShell 和共享 query keys 会被多个交付项同时修改，平行子任务会制造冲突和临时兼容层。实现可按下面检查点顺序执行，但不在 production 同时运行两套前端或通信层：

1. 记录依赖/产物基线，新增 superseding ADR 与契约测试骨架。
2. 建立 Hey API 生成、runtime、错误模型和 QueryClient；保持旧页面暂可构建。
3. 增加分页、realtime hub/SSE 与后端测试；在同一检查点刷新生成物。
4. 用 Vite/React Router/Ant Design 建立 AppShell、门禁和响应式基础组件。
5. 迁移核心切片并删除其旧手工 load/poll；验证 HTTP+SSE 全链路。
6. 迁移剩余页面，删除所有 Pro/Umi import、旧 client 与旧配置。
7. 增加浏览器回归、更新构建/CI/开发脚本、规范与公共文档，完成依赖审计和 before/after 记录。

每个检查点均保持 typecheck/test/build 可运行。最终镜像仍把 `web/dist` 与匹配的 `simplusd` 一起发布；不承诺旧浏览器静态资源连接新 API。

## 12. 回滚

- 数据库只新增分页索引，Down migration 不删除业务数据。
- Vite 仍输出同一路径，Go 静态文件承载契约不变。
- 发布回滚使用上一完整镜像/提交，并在需要时执行索引 Down migration；不在运行时保留 Umi fallback。
- SSE 连接失败时，页面仍可通过首次查询、窗口聚焦、显式刷新以及 Mihomo/VoWiFi 的有界 active-page fallback poll 工作；这只是故障降级，不是取消 SSE 验收。
- mutation 返回不确定结果时不自动重发；短信和电话继续依赖 operation ID、历史查询和用户明确重试。

## 13. 验证设计

### Backend

- realtime hub：topic 合并、慢 subscriber、取消、无阻塞和 resync。
- SSE handler：setup/auth gate、cookie、headers、flush、heartbeat、session 失效、disconnect、payload privacy。
- pagination：空页、边界、相同时间戳、非法 cursor、filter 组合、并发插入、数据库 reopen 与 Up/Down migration。
- handler：OpenAPI query/response、stable errors、mutation 发布和 background SMS 发布。

### Frontend

- runtime：CSRF、cookie、timeout、abort、401、retryable 与 malformed success response。
- event bridge：schema rejection、topic->query mapping、active-only invalidation、reconnect resync、attention 去重和 cleanup。
- Query/mutation：取消、无 mutation auto-retry、返回对象窄更新、跨页面共享 cache。
- 页面：现有 Login/Modems/Lines 测试继续覆盖原安全行为；补齐 Messages、Calls、Dashboard、Mihomo、Notifications、Settings、Setup。
- Playwright Chromium：桌面与手机 viewport 的登录后导航、Drawer、核心 Modem/Line/Message 流、局部表格滚动、无全局横向溢出和无意外 autofocus。

### Gates

```bash
corepack pnpm --dir web test
corepack pnpm --dir web typecheck
corepack pnpm --dir web build
corepack pnpm --dir web e2e
go test ./internal/application/realtime ./internal/api/httpapi ./internal/storage/sqlite
make verify-generated
make check-format
make lint
make test
make security
make build
make check-docs
```

普通验证不接触真实设备，不发送短信/电话，不修改 RF 或模组持久状态。

## 14. 依赖与安全验收

- 移除 `@umijs/max`、`@umijs/plugins`、所有 `@ant-design/pro-*` 和其引入的第二套 Ant Design/React Router/SWR。
- Vite、React Router、Hey API、Zod、TanStack Query、Ant Design 和 Playwright 均作为用途明确的直接依赖，并由 lockfile 固定。
- `make security` 继续以 moderate 为 CI 门槛；完整树和 production tree 均不得有 high/critical。
- audit ignore 必须包含 advisory、适用范围和复查条件；能通过升级消除的项不得长期 ignore。
- 同口径结果：完整依赖树从约 1,578 项降到 268，production tree 从约 1,486 项降到 73；`web/dist` 保持 29 个文件并从 3,004,106 bytes 降到 1,387,882 bytes（约减少 53.8%）。production/full audit 均为零 advisory，且没有 audit ignore。

## 15. 文档与规范

实现必须：

- 新增 `docs/decisions/0022-...md`，明确 supersede ADR 0009 中的 Umi/Pro 决定，保留 React/Ant Design、单栈和 API 安全决定。
- 更新 `docs/architecture.md` 的运行图、Web 层和 HTTP/SSE 数据流。
- 在唯一 active plan 中增加本次里程碑，并更新审计技术债状态。
- 更新 `docs/development.md` 中 Umi 名称与 Vite 命令，但不复制架构全文。
- 刷新 `.trellis/spec/web/frontend/**` 以及受影响 backend/build/docs spec，使其描述完成后的真实代码而不是旧 Umi 现状。

## 16. 主要风险

| 风险 | 缓解 |
| --- | --- |
| 一次换壳与换数据层导致回归面大 | 核心切片优先、每阶段 green、保留已有页面测试并新增浏览器测试 |
| SSE 被 timeout/代理缓冲截断 | endpoint bypass buffered timeout、heartbeat、真实 flush 测试、同源 Vite 代理 smoke |
| 事件漏失或 subscriber 堵塞业务 | 事件仅为 hint、ready/resync、active query 重取、hub 有界且不阻塞 |
| 生成 schema 不能表达拓扑引用约束 | 保留并收敛 `hardwareSchema.ts` 的跨字段验证 |
| mutation 重试造成真实副作用 | 全局 mutation retry=false、operation ID、未知结果专用提示 |
| 分页在相同时间戳下重复/漏项 | `(timestamp,id)` 稳定排序、limit+1、边界/并发测试、匹配索引 |
| 新工具重新扩大依赖树 | 只加入有明确验收价值的直接依赖，记录 dependency/build before-after，并维持审计门禁 |

## 17. 明确延期

- WebSocket、GraphQL、gRPC-Web、离线写队列和 service worker。
- SSR/SSG、React Router Framework Mode 和面向公网的跨域访问。
- 多管理员、多租户、角色权限与跨浏览器通话租约。
- 浏览器通知权限、系统级推送和后台常驻提醒；本轮只做当前页面内提示。
- 真实硬件短信/电话/eUICC/RF/HIL；本任务只使用 deterministic fixture、Simulator 和只读代码检查。
