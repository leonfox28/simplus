# 飞书通知渠道一键绑定：实施计划

## 1. 先固化持久决策

- [x] 新增 ADR 0025，记录一键创建应用、授权用户私聊、最小出站权限、无入站能力、仅本地解绑和既有 Webhook 兼容。
- [x] 在 `docs/plans/active/mvp.md` 增加新的未完成里程碑，实施完成后再标记完成。

## 2. 扩展领域与存储

- [x] 为 notification domain 增加穷尽的 `DeliveryMode` 变体和飞书应用密文字段，不复用 Webhook 字段保存应用凭据。
- [x] 新增 core v23 migration，创建独立 `feishu_app_notification_channels` 表并提供 Down；不重建 v12 Webhook 表。
- [x] 扩展 SQLite store，使 list/read/delete/delivery status 可以安全合并两个表，upsert 根据 mode 写入唯一所有者表，并拒绝跨表 ID 冲突。
- [x] 增加 fresh schema、v22→v23 升级、Down、旧 Webhook 保留、app channel reopen、删除与密文不回显测试。

## 3. 实现窄化飞书客户端与绑定状态机

- [x] 在 notification application 内定义 `FeishuRegistrar` 与 `FeishuMessenger` 小接口，并实现固定主机/路径、HTTPS、无重定向、超时、响应上限和严格结果校验。
- [x] 实现 RFC 8628 begin/poll/slow-down/expiry/cancel，固定 `createOnly`、应用预设、`preset=false` 和唯一 `im:message:send_as_bot` 权限；拒绝 Lark tenant、空/非法 App ID/App Secret/open_id 和非飞书验证 URL。
- [x] 实现 tenant token + `open_id` 文本私聊发送，不缓存 provider token，不将响应正文/凭据写入日志。
- [x] 实现单实例绑定状态机、generation fence、进程上下文取消、终态清理和稳定错误码。
- [x] 授权成功后先用瞬时凭据发送一次绑定测试，再以字段独立标签加密并持久化默认“飞书私聊”+全部事件+enabled+success 状态；测试/持久化失败不创建成功渠道。
- [x] 扩展现有 Create/Update/Test/Notify/Delete，使 Webhook 与 app mode 都走穷尽分支；app mode 的设置更新不得接受替换 Webhook/签名字段。
- [x] 用合成 RoundTripper/clock/store 覆盖成功、拒绝、过期、slow-down、Lark、非法结果、网络/超大响应、重复 start、cancel race、stale generation、测试失败、持久化失败、正常通知与删除。

## 4. 扩展 OpenAPI、HTTP 与装配

- [x] 先修改 `api/openapi.yaml`：channel discriminants、Feishu binding state schema、singular GET/POST/DELETE resource 和稳定错误响应。
- [x] 运行 `make generate`，只通过生成器更新 Go/Web contract；不得手改 generated 文件。
- [x] 扩展 `NotificationManager` 与 handler：管理员认证、POST/DELETE CSRF、`Cache-Control: no-store`、冲突/供应商/终态错误映射、成功后的 notifications invalidation。
- [x] 在 `cmd/simplusd` 注入进程 context、窄化外部客户端和后台成功变更回调；shutdown 必须取消等待中的 poll。
- [x] 增加真实 router 的 auth/CSRF/no-store/响应隐私/状态映射/后台成功可见性测试，确认 API 从不返回 App ID/App Secret/open_id/device code。

## 5. 完成 Web 绑定与渠道编辑体验

- [x] 使用生成 Query/Mutation options 增加“绑定飞书私聊”区域；无本地预填表单。
- [x] waiting 状态展示可复制 URL、外链按钮和过期时间；仅 waiting/testing 使用前台 TanStack Query 状态轮询并在终态停止。
- [x] 成功后失效 channel list；失败、拒绝、过期、取消和重复发起显示稳定中文状态并允许重试。
- [x] 保留手工企业微信/飞书 Webhook 创建作为次级入口。
- [x] 表格/移动卡片根据 `deliveryMode`/`targetType` 显示 Webhook 或“授权用户私聊”。
- [x] 增加编辑名称/事件 modal，复用现有 update mutation；启停、测试、删除保持逐行 busy/error。
- [x] app channel 删除确认明确“只删除 Simplus 绑定，飞书应用保留”。
- [x] 更新 `web/src/api/errors.ts` 或窄化状态映射，不显示原始 provider 错误。
- [x] 用 Vitest 覆盖 idle→waiting→testing→success、终态、URL 隐私、query invalidation、编辑、模式化删除提示和响应式必要字段；用 synthetic Playwright 覆盖桌面/移动主路径和无页面横向溢出。

## 6. 更新规范所有者与公开说明

- [x] 更新 `docs/product.md` 的通知渠道体验与非目标措辞。
- [x] 更新 `docs/architecture.md` 的通知数据流、瞬时绑定状态、独立表与密文边界。
- [x] 更新 active MVP plan、`docs/handoff.zh-CN.md` 与 `docs/troubleshooting.md` 的稳定绑定错误；不加入真实 URL、账号、open_id、截图或日志。
- [x] 按 `trellis-update-spec` 将短期外部授权、测试后落库、凭据无回显和迁移/验证矩阵沉淀到 backend API code-spec。

## 7. 验证顺序

先运行最窄检查，再扩展：

```bash
go test ./internal/application/notification
go test ./internal/storage/sqlite
go test ./internal/api/httpapi
make check-format
make verify-generated
corepack pnpm --dir web typecheck
corepack pnpm --dir web test
corepack pnpm --dir web build
make lint
make test
make web-e2e
make check-docs
make security
```

检查生成前后差异，确认 `git diff` 中没有手工 generated 修改、真实凭据/URL/身份或无关文件。

## 8. 风险文件与回滚点

- `internal/storage/sqlite/migrations/core/00023_*`：回滚必须先执行 Down；Down 只保留旧 Webhook 渠道并移除本地 app binding 表。
- `api/openapi.yaml` 及生成树：任何 contract 变更都必须原子生成并通过 drift 检查。
- `internal/application/notification/*`：测试必须证明 background completion、cancel/restart 和 test-before-persist 顺序。
- `web/src/pages/Notifications.tsx`：必须同时保护桌面表格、移动卡片、旧手工渠道和新绑定状态。
- 不运行真实飞书注册或真实投递作为自动化检查。产品内的真实测试消息只由管理员主动打开链接并授权触发；如需由开发代理执行现场验证，必须另行取得明确授权并只报告脱敏结论。

## 9. `task.py start` 前检查

- [x] `prd.md` 已完成收敛重写且无阻塞问题。
- [x] `design.md` 与本实施清单已由用户审阅。
- [x] `implement.jsonl` 和 `check.jsonl` 均只包含真实 spec/research 上下文条目。
- [x] 用户在最终规划摘要之后另行明确批准实施。
