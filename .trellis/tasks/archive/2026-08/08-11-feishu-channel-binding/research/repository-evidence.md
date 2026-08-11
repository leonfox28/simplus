# 飞书一键绑定：仓库与平台证据

## 用户已确认的产品决策

- 目标体验参考 Hermes：Simplus 生成一个 URL，管理员打开后完成飞书渠道配置与绑定。
- MVP 只绑定完成授权的用户私聊，不支持群聊发现、选择或投递。
- 绑定前零表单：默认渠道名为“飞书私聊”，默认订阅全部五类现有事件；绑定后可以编辑名称和事件。
- 解绑只删除 Simplus 本地绑定，飞书侧自动创建的应用保留，并在 UI 中明确提示。

## 当前仓库事实

- 产品与 ADR 将企业微信/飞书限定为单向出站通知，禁止入站命令并要求 credential 不回显：`docs/product.md:81-87`、`docs/decisions/0005-management-completion-mihomo-notifications.md:23-26`。
- 当前飞书实现只支持群自定义机器人 Webhook 与可选签名：`internal/application/notification/service.go:71-99`、`internal/application/notification/service.go:239-298`。
- 当前 Web 只提供手工 Webhook 表单：`web/src/pages/Notifications.tsx:55-66`。
- 当前公共契约把通知渠道建模为 Webhook：`api/openapi.yaml:367-408`、`api/openapi.yaml:2920-2954`。
- Webhook/签名使用实例主密钥的带标签 AES-GCM 密文，列表不回显原值：`internal/security/secretbox/keyring.go:63-91`、`internal/application/notification/service.go:308-309`。
- core v12 的 `notification_channels` 强制 `webhook_ciphertext NOT NULL`；直接把应用凭据塞入该列会破坏语义与旧逻辑：`internal/storage/sqlite/migrations/core/00012_notification_channels.sql:2-15`。
- 当前删除通知渠道只删除本地记录：`internal/application/notification/service.go:153-165`、`internal/storage/sqlite/notifications.go:75-81`。
- 通知 API 已使用管理员 cookie 与变更请求 CSRF 校验：`internal/api/httpapi/server.go:1754-1844`、`internal/api/httpapi/server.go:1080-1101`。
- Web 以生成的 TanStack Query 契约为准，HTTP/SQLite 是权威状态；页面不得直接 `fetch`：`.trellis/spec/web/frontend/hook-guidelines.md`、`.trellis/spec/web/frontend/state-management.md`。

## 飞书官方能力

- 飞书官方提供基于 OAuth 2.0 Device Authorization Grant（RFC 8628）的一键创建应用流程。流程先产生约 600 秒有效的验证 URL，授权完成后返回 App ID、App Secret 与授权人的 `open_id`：<https://open.feishu.cn/document/mcp_open_tools/integrating-agents-with-feishu/create-an-app-in-one-click-java>。
- 官方 Go SDK 的一键注册支持 `CreateOnly`、应用名称预填、最小基础模板以及增量权限；`Preset=false` 配合唯一租户权限 `im:message:send_as_bot` 可避免平台默认的更宽权限、事件与回调：<https://github.com/larksuite/oapi-sdk-go#one-click-app-registration>。
- 官方发送消息 API 支持 `receive_id_type=open_id`；向用户首次发送会形成机器人与该用户的 p2p 会话：<https://open.feishu.cn/document/server-docs/im-v1/message/create>、<https://open.feishu.cn/document/server-docs/group/chat/intro>。
- 群消息需要先将机器人加入群并取得 `chat_id`，因此不属于“一条 URL 即完成”的 MVP。
- 删除飞书自建应用是开发者后台操作；已发布应用可能先需企业管理员停用，个人版也可能无法删除已发布应用：<https://open.feishu.cn/document/develop-process/operations-analysis/transfer-owner-and-collaborative-members>。

## Hermes 参考实现

- `NousResearch/hermes-agent` 在提交 `936dd7346fd7fd8107af1ce7fc019c07c001c1bd` 的飞书适配器中使用同一设备授权端点，展示验证 URL，轮询授权结果并保存 App ID/App Secret。
- Hermes 的完整消息网关还启用 WebSocket/入站消息并另行选择 home channel；Simplus 只复用“一键创建应用”的体验，不复制入站网关或群聊能力。

## 技术结论

- 设备授权是出站轮询，不要求 Simplus 提供公网 OAuth 回调、入站 Webhook 或 WebSocket，符合可信 LAN 与暂停内建 HTTPS 的现状。
- 待授权状态应只驻留内存；验证 URL、device code、App Secret 与 `open_id` 不进入 SQLite、SSE 或日志。只有授权结果通过校验且测试私聊发送成功后，才创建正式渠道记录。
- 新的应用渠道使用 core v23 独立表，而不是重建/复用旧 Webhook 表。这样升级不触碰旧渠道，旧版回滚只需显式 Down 丢弃无法表示的新应用渠道。
- App ID、App Secret 与 `open_id` 均以独立标签加密；列表只返回 `deliveryMode=feishu_app` 与 `targetType=authorized_user`，不返回凭据或用户标识。
- 平台 I/O 使用项目自有的窄化、固定主机、无重定向、有超时与响应大小上限的客户端。官方 SDK/协议作为行为依据，但不引入通用飞书 API 或入站 Channel 模块。
- 自动化验证全部使用合成 transport/响应；不创建真实飞书应用，也不发送真实消息。真实测试消息只会在管理员主动打开验证 URL并确认授权后由产品流程发出。
