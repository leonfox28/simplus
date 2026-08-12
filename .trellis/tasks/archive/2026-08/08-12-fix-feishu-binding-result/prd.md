# 修复飞书一键绑定授权结果解析

## Goal

修复已部署的飞书私聊一键绑定：管理员点击“绑定飞书私聊”后，应获得可打开的短期授权 URL，而不是看到“飞书返回了无法使用的授权结果”，同时继续保持最小权限、测试后落库和凭据不回显边界。

## Confirmed Background

- 用户在 2026-08-12 的本地 `dev` 容器中实际点击绑定后，页面停留在“尚未发起绑定”，并显示“飞书返回了无法使用的授权结果”。
- 该文案对应稳定错误 `FEISHU_BINDING_RESULT_INVALID`，表明失败发生在正式渠道创建前；当前截图没有显示授权 URL，也没有证据表明已发送测试消息或写入渠道。
- 一次不打开 URL 的脱敏 Begin 探针确认：飞书当前返回 `expires_in`、514 字节 opaque `device_code` 和 `open.feishu.cn` 授权页；当前实现只读 `expire_in`、把 device code 限制为最多 512 个受限字符，并只允许 `accounts.feishu.cn`（`internal/application/notification/feishu.go:88-124`）。因此至少两个本地校验会拒绝当前官方有效响应。
- 飞书官方 Go SDK v3.9.10 对 device code 只做非空校验、对过期秒数提供默认值、直接使用返回的验证 URL，并按 JSON 语义处理注册轮询；Hermes 同样会解析注册端点在 HTTP 4xx 中返回的 `authorization_pending` JSON。当前 Simplus 通用 JSON helper 会在解码前拒绝非 2xx，修复 Begin 后仍会导致轮询提前失败。
- 当前实现只允许飞书中国版、授权人私聊、`im:message:send_as_bot`、无入站能力；本次缺陷修复不得放宽这些产品和安全边界。

## Requirements

- R1：以代码、官方协议实现和脱敏运行证据定位错误；研究与公开输出不得包含 device code、credential、`open_id`、完整 URL、原始响应或现场日志。
- R2：修正一键创建应用 begin/poll 协议，使官方有效响应能进入 `waiting` 并返回短期验证 URL；缺字段、错误租户、非零 provider code、非法 URL、过期或超大响应仍应 fail closed。
- R2a：device code 作为不透明、非空且有总长度上限的轮询凭据处理，不按 App credential 字符集验证；授权 URL 只允许当前和兼容的飞书中国官方精确主机，仍拒绝重定向、userinfo、端口与 fragment。
- R2b：优先支持当前 `expires_in` 并兼容旧 `expire_in`；注册端点允许解析有界 JSON 错误状态以识别 pending/slow-down/denied/expired，tenant token 与消息端点继续要求 HTTP 2xx 和显式 `code=0`。
- R3：保持 create-only、最小权限、无事件/回调、授权用户私聊、测试成功后才加密持久化及本地解绑语义不变。
- R4：增加能复现现场失败形态的合成回归测试，并覆盖官方有效响应与关键非法响应；自动化不得创建真实飞书应用或发送真实消息。
- R5：修复后更新本地 `dev` 容器，并只做不触发真实飞书授权的健康/契约检查；真实 URL 生成由用户在产品内再次主动点击验证。

## Acceptance Criteria

- [x] AC1：根因由官方协议/SDK 源代码、当前实现和脱敏结构探针之间的具体差异解释，不记录任何临时授权值。
- [x] AC2：合成官方 begin 成功响应返回 `waiting`、HTTPS 飞书授权 URL 和过期时间，且不会被误判为 `FEISHU_BINDING_RESULT_INVALID`。
- [x] AC3：非零/缺失 provider code、缺失 device code、非飞书 URL、Lark、过期及非法授权结果继续被稳定拒绝且不产生渠道。
- [x] AC4：现有状态机、竞态、SQLite、HTTP 隐私与 Web 绑定测试继续通过，旧 Webhook 行为不变。
- [x] AC5：本地容器重建后服务健康，新绑定路由仍受管理员认证保护；自动化与代理不进行真实飞书授权或投递。

## Out of Scope

- 改变私聊目标、增加群聊、Lark、入站消息或远程命令。
- 要求用户披露飞书账户、App ID/App Secret、`open_id`、完整授权 URL 或原始 provider 响应。
- 由开发代理代替用户执行真实授权或测试消息。
- 改动 OpenAPI、Web 交互、SQLite schema 或既有渠道数据；本次错误无需这些层的契约变更。

## Risks and Deferred Items

- 飞书一键注册端点不是普通 OpenAPI，HTTP status 与 JSON device-flow 状态并非一一对应；修复必须只对固定注册端点启用“解析非 2xx JSON”，不能放宽 token/消息客户端。
- device code 是敏感且短期的不透明值。放宽字符集不等于无界接受：仍需响应体上限、device code 长度上限、进程内瞬时保存和终态清理。
- 自动化只能证明与已观察结构及官方实现兼容；修复后真正打开 URL、创建应用和收到测试私聊仍由用户主动完成。
