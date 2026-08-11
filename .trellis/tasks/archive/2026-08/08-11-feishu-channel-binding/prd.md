# 飞书通知渠道一键绑定

## Goal

为飞书通知提供类似 Hermes Scan-to-Create 的零表单绑定体验：Simplus 生成短期有效的飞书验证 URL，管理员打开并授权后，系统自动创建最小权限应用、绑定授权用户私聊、验证投递并安全保存渠道，无需进入飞书开发者后台或手工粘贴 App ID/App Secret。

## Confirmed Background

- Simplus 将企业微信与飞书限定为单向出站通知，不接受入站命令，且 API/Web 不回显 credential（`docs/product.md:81-87`、`docs/decisions/0005-management-completion-mihomo-notifications.md:23-26`）。
- 当前飞书实现只接受群自定义机器人 Webhook 与可选签名，服务端限定 `open.feishu.cn/open-apis/bot/v2/hook/*`（`internal/application/notification/service.go:71-99`、`internal/application/notification/service.go:269-298`），Web 只提供手工录入（`web/src/pages/Notifications.tsx:55-66`）。
- 现有 Webhook/签名通过实例主密钥做带标签 AEAD 加密；core v12 模型以 Webhook 为必填，不能无歧义地承载应用凭据（`internal/security/secretbox/keyring.go:63-91`、`internal/storage/sqlite/migrations/core/00012_notification_channels.sql:2-15`）。
- 飞书官方的一键创建应用基于 RFC 8628，先返回默认约 600 秒有效的验证 URL，授权后返回 App ID、App Secret 与授权人的 `open_id`；最小模板可以只申请 `im:message:send_as_bot`：<https://github.com/larksuite/oapi-sdk-go#one-click-app-registration>。
- 飞书发送消息 API 支持以 `open_id` 建立用户私聊；群聊则要求先把机器人加入群并选择 `chat_id`，不符合本次单 URL MVP：<https://open.feishu.cn/document/server-docs/group/chat/intro>。
- Simplus 当前删除渠道只删除本地记录（`internal/application/notification/service.go:153-165`、`internal/storage/sqlite/notifications.go:75-81`）。飞书自建应用需在开发者后台处理，已发布应用还可能要求企业管理员先停用：<https://open.feishu.cn/document/develop-process/operations-analysis/transfer-owner-and-collaborative-members>。
- Hermes 的公开飞书适配器使用同类设备授权创建应用；其入站 WebSocket 与 home channel 选择不属于 Simplus 的单向通知目标。

## Requirements

### R1 — Zero-form binding entry

已登录管理员可以直接发起飞书绑定，不先填写名称或事件。Simplus 返回可复制、可打开且带明确过期时间的飞书验证 URL，并提供等待、测试、成功、拒绝、过期、失败与取消状态。

### R2 — Least-privilege Feishu application

每次绑定只允许创建新应用，使用最小基础模板并仅申请应用身份发送消息权限。MVP 只接受飞书中国租户；不得订阅事件、配置回调、建立 WebSocket/Webhook 或提供远程命令能力。

### R3 — Authorized-user private destination and defaults

通知目标固定为完成授权的用户 `open_id` 私聊。成功渠道默认名为“飞书私聊”、立即启用，并订阅 `sms.received`、`sms.failed`、`call.incoming`、`call.missed`、`system.degraded` 全部现有事件。

### R4 — Test-before-persist and secret handling

授权结果必须先通过类型/租户校验并向授权用户发送一次绑定测试消息；只有测试成功后才能创建正式渠道。App ID、App Secret 与目标 `open_id` 使用字段独立的实例密钥标签加密，不通过普通 API、Web、SSE、日志或错误文本回显。

### R5 — Bounded and recoverable workflow

同一实例同一时刻最多一个绑定尝试。URL/device code 仅作为有界内存状态；拒绝、过期、取消、网络/供应商错误、非法结果、测试失败、持久化失败、重复发起或服务重启都不得留下被呈现为可用的半配置渠道。

### R6 — Post-binding management and local-only unbind

绑定后可修改显示名称与事件订阅，无需重新授权；启停、显式测试和投递状态沿用现有渠道能力。删除只移除 Simplus 本地记录并停止使用本地凭据，不尝试停用或删除飞书应用；确认界面必须说明飞书侧应用仍保留。

### R7 — Existing Webhook compatibility

企业微信与飞书手工 Webhook 渠道继续支持创建、凭据替换、编辑设置、启停、测试、投递状态和删除；既有记录不迁移、不重绑，新增应用渠道不能复用或改变 Webhook 密文字段语义。

### R8 — Authenticated and privacy-bounded surface

绑定读取要求管理员会话，发起/取消要求 CSRF；含绑定状态的响应必须禁止缓存。浏览器不把验证 URL 或任何凭据写入持久存储，公共错误只使用稳定、长度有界且不含账号/身份/供应商正文的代码。

## Acceptance Criteria

- [ ] AC1 / R1：从通知页点击“绑定飞书私聊”无需填写表单即可获得一个有过期时间的可复制/打开 URL；页面能观察到完整终态。
- [ ] AC2 / R2：合成协议测试证明创建请求为 create-only、minimal preset，权限集合严格等于 `im:message:send_as_bot`，且没有事件、回调或入站 transport；Lark 结果被拒绝。
- [ ] AC3 / R3：合成授权结果中的 `open_id` 是首次测试消息的唯一接收者；成功行的名称、enabled 和五类事件与默认值完全一致，流程不请求或展示群聊。
- [ ] AC4 / R4：测试失败时数据库中没有新渠道；成功时三个应用字段均为非明文密文，渠道列表/状态响应不包含 App ID、App Secret 或 `open_id`，最后投递状态为 success。
- [ ] AC5 / R5：拒绝、过期、取消、非法响应、网络失败、重复 start、stale background completion、测试/存储失败和进程取消均有确定结果；只有成功路径产生一条正式记录。
- [ ] AC6 / R6：管理员可编辑名称/事件并继续测试或启停；删除 app channel 前能看到“只删除本地绑定”的提示，删除后 Simplus 不再投递，飞书侧应用不被远程修改。
- [ ] AC7 / R7：旧 Webhook fixture/升级数据在新版本中内容与行为保持，手工企业微信/飞书的创建、更新、投递和删除回归全部通过。
- [ ] AC8 / R8：真实 router 测试覆盖 auth、CSRF、`Cache-Control: no-store`、稳定错误和敏感字段缺失；Web 测试证明 URL 只存在于瞬时页面状态且桌面/移动端均无布局回归。
- [ ] AC9：OpenAPI/Go/Web 生成一致性、后端/迁移测试、Web typecheck/Vitest/build、synthetic desktop/mobile E2E、文档检查和依赖安全检查通过；自动化过程不创建真实飞书应用或发送真实消息。

## Out of Scope

- 飞书入站消息、事件订阅、卡片回调、审批、通讯录同步或远程控制。
- 群聊发现、群选择、邀请机器人入群或向群聊投递。
- Lark 国际版一键绑定。
- 删除或强制迁移现有群自定义机器人 Webhook 渠道。
- 飞书应用的通用管理后台、已有应用接管、任意权限申请或任意 OpenAPI 代理。
- 自动停用、撤销发布或删除飞书侧应用。

## Risks and Deferred Items

- 管理进程可能在飞书已创建应用、但本地测试或持久化完成前退出；本地不会产生可用渠道，但飞书侧可能留下需人工清理的应用。MVP 明确提示该残留，不引入入站恢复或远程删除权限。
- 一次测试投递成功后数据库仍可能写入失败；用户可能收到测试消息但看不到渠道。系统必须报告持久化失败，不能据此声称绑定成功或自动重复发送。
- 本地删除遵循现有 SQLite 逻辑删除语义，不承诺从 WAL、空闲页或备份中取证级擦除；所有残留值仍是实例密钥加密的密文。
- 真实飞书注册/投递只由管理员在产品内主动打开 URL 并授权触发；自动化验收使用合成 provider，不包含现场账号、URL、open_id、截图或原始日志。
