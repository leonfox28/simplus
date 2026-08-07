# 0022：显式 Vite Web 运行时与后端权威通信

- 状态：Accepted / Implemented
- 日期：2026-08-07
- 取代：[`0009`](0009-ant-design-pro-web.md) 中使用 Umi Max、Ant Design Pro
  Components、ProLayout 和 Umi 路由/运行时的部分

## 背景

ADR 0009 完成了从 Vue 到 React 19、Ant Design 6 和单一管理后台栈的迁移，避免了
长期维护两套前端。随着页面和通信状态增加，Umi Max 与 Pro Components 又引入了
第二套 Ant Design 主版本、受框架约束的旧路由主版本以及重复的数据和样式依赖。
页面仍各自维护服务端快照、错误和刷新流程，短信等动态页面依赖粗粒度轮询，公共
OpenAPI 契约也只生成类型而没有生成浏览器 operation client。

Simplus 是可信局域网中的单管理员控制台。浏览器只需要及时获知“某类资源发生了
变化”，不需要复制后端业务状态或建立新的事件真相源。短信、电话和硬件动作还具有
外部副作用，不能因为前端重连或通用重试而自动重放。

## 决定

1. 保留 React 19、Ant Design 6、单一前端栈、中文优先管理后台以及独立 Login/Setup
   页面。构建改为 Vite，路由改为 React Router Declarative Mode；页面直接从
   `antd` 导入所需组件，由项目自己的 `AppProviders`、`BootstrapGate`、
   `AppRouter` 和响应式 `AppShell` 显式拥有 provider、认证门禁、路由和导航。
2. 删除 Umi Max、Ant Design Pro Components、ProLayout 及其运行时、路由和生成目录，
   不以另一个管理后台元框架替代，也不长期保留兼容壳或双栈。Vite 继续输出
   `web/dist`，生产静态资源与匹配的 API 仍由同一个 `simplusd` release 原子交付；
   开发服务器仍只代理同源 `/api`。
3. `api/openapi.yaml` 继续是公共 HTTP 契约的唯一来源。`@hey-api/openapi-ts` 生成链
   拥有 Fetch SDK、TypeScript 类型、Zod 结构 schema，以及 TanStack Query 的 query
   key/options；页面不直接 `fetch`、复制公共 payload 或自行定义 query identity。
   手写浏览器 runtime 只拥有 same-origin cookie、double-submit CSRF、取消/超时、
   稳定错误归一化和少量 OpenAPI 无法表达的跨字段领域校验。
4. Go 服务和 SQLite 仍是持久业务状态的唯一权威。查询与 mutation 继续通过鉴权
   HTTP 完成；TanStack Query 只缓存浏览器内的可丢弃快照，并集中处理取消、有限
   query retry、mutation 后同步和跨页面失效。Mutation 默认不自动重试，具有外部
   副作用的操作继续依赖明确用户意图、operation ID 和服务端幂等。
5. 增加同源 `GET /api/v1/events`：它经过 setup、管理员 session 和可信 LAN gate，是
   内存有界的 SSE 失效通道。它只发送资源 topic、重连 resync 和新短信/来电
   attention hint，不发送正文、号码、SIM/设备身份、硬件路径、网络拓扑、命令、原始
   错误或诊断材料。事件只是提示；客户端只使当前活跃 query 失效，断线、丢事件或
   进程重启后都通过 HTTP 重新取得权威快照。慢订阅者不能阻塞业务 mutation 或后台
   同步。
6. Messages 和 Calls 使用 `(createdAt, stable ID)` 排序的 opaque keyset cursor；
   相同时间戳由稳定业务 ID 决定顺序，不使用 offset。Messages 的会话过滤必须同时
   提供 Line 和 remote address，并由匹配索引支持。游标不包含正文、号码或硬件身份，
   也不持久化到浏览器存储。
7. 桌面继续使用扁平左侧导航和数据表格；手机使用 Drawer 导航与 Card/List 集合。
   宽表只允许在自身容器滚动。加载、空、错误、部分失败、busy 和能力不可用原因都要
   可见；未装配能力不通过隐藏入口伪装为已支持。

## 后果

- ADR 0009 关于 React、Ant Design、单栈、cookie session、double-submit CSRF 和
  `simplusd` 静态承载的决定继续有效；只有 Umi/Pro 运行时选择被本记录取代；
- 路由、provider、认证恢复、响应式布局、数据所有权和失效规则成为仓库可直接检查的
  源码边界，不再由后台元框架隐式提供；
- SSE 故障会降低更新及时性，但不能改变持久业务结果；首次查询、窗口聚焦、显式刷新
  和重连后的 active-query resync 仍可收敛；
- OpenAPI、后端、生成 client、前端页面和构建依赖必须作为一个原子版本交付，迁移
  期间不发布 Umi/Pro 与新栈并存的 production 版本；
- 本决定不扩大可信局域网、单管理员、硬件 capability 或 Host VoWiFi/Mihomo 权限
  边界，也不授权任何真实短信、电话、RF、eUICC 或其他 HIL 动作。
