# 继续修复飞书绑定结果误判

## Goal

修复飞书私聊一键绑定在第一轮协议兼容补丁与本地容器更新后仍返回 `FEISHU_BINDING_RESULT_INVALID` 的复发问题，使管理员能稳定看到并使用短期授权 URL，同时不弱化最小权限、隐私和 fail-closed 边界。

## Confirmed Background

- 用户在运行版本 `9f9ac73` 的本地 `dev` 容器更新后，从桌面浏览器再次触发绑定，页面仍显示“飞书返回了无法使用的授权结果”。
- Web 顶部 Alert 只由 start mutation/query error 产生，而卡片仍显示缓存的 idle（`web/src/pages/Notifications.tsx:79-82,121-140`）；这证明 POST 在同步 Begin 阶段失败，尚未进入后台 Poll。
- 第二次不打开 URL 的脱敏 Begin 探针确认：HTTP 200、514 字节 opaque device code、`open.feishu.cn` 精确 authority、interval=5、当前字段 `expires_in=3600`，其余已修边界均通过。当前代码仍拒绝 `expiresIn > 900`（`internal/application/notification/feishu.go:114-124`），因此根因闭合。
- Begin 后立即执行一次不打开 URL 的脱敏 Poll，得到 HTTP 400、稳定状态 `authorization_pending`、无 credential/user info；当前 registration-only JSON 逻辑已覆盖该串联路径。
- 第一轮修复只报告旧字段 `expire_in` 为缺失，没有记录当前 `expires_in` 的值，随后设计继续沿用原始 900 秒上限；这是第一次修复未覆盖复发的直接原因。
- 上一轮研究与复盘位于 `.trellis/tasks/archive/2026-08/08-12-fix-feishu-binding-result/`。产品边界仍是飞书中国版、授权人私聊、仅 `im:message:send_as_bot`、测试后落库、凭据不回显和仅本地解绑。

## Requirements

- R1：把 provider `expires_in` 作为正数秒数处理，兼容 legacy `expire_in` 和两者冲突检测；当前 3600 秒响应必须成功进入 waiting。
- R2：用独立常量设置 24 小时本地安全上限并做 `time.Duration` 安全转换；零/负值继续使用官方 SDK兼容的 600 秒默认值，超出上限、短于 interval 或冲突值继续 fail closed。
- R3：保持 4096 字节 opaque code、两个精确飞书中国 authority、registration-only 非 2xx JSON、Lark 拒绝、token/message strict success、测试后落库和旧 Webhook 行为不变。
- R4：增加精确复现 `expires_in=3600` 的合成回归，并覆盖边界值/越界值、legacy/default/conflict 与相邻 Poll 路径；不得使用现场 device code、完整 URL、身份或原始供应商正文。
- R5：修复后执行独立检查、`trellis-break-loop` 复盘、提交确认和经用户批准的本地 `dev` 容器更新；代理不得打开授权 URL、创建应用或发送消息。

## Acceptance Criteria

- [x] AC1：合成当前 Begin 结构（包括 `expires_in=3600`）返回 `waiting`、可信 URL 和正确的一小时本地期限，不再返回 result-invalid。
- [x] AC2：24 小时边界可接受，超出一秒、短于 interval、conflict、非法 authority/opaque code 仍被拒绝且不产生渠道。
- [x] AC3：HTTP 400 pending/slow-down/denied/expired、token/message strict success、notification race、HTTP privacy、SQLite/Webhook 和全量门禁继续通过。
- [x] AC4：任务/测试/日志/API 不包含任何临时授权值、完整 URL、credential、身份或原始 provider body。
- [x] AC5：更新本地容器后服务健康、运行版本正确、绑定 API 受认证保护；用户重新发起并主动完成授权后确认绑定成功，飞书中可见对应私聊。

## Out of Scope

- 群聊、Lark、入站消息、远程命令或权限扩展。
- 更改 Web/OpenAPI/SQLite schema；截图已经证明错误来自服务端 Begin 上限。
- 由开发代理打开授权 URL、创建飞书应用或发送测试消息。
- 要求用户披露临时 code、完整 URL、身份、credential 或原始响应。

## Risks and Deferred Items

- RFC 8628/官方 SDK没有规定很小的通用 expiry 上限；24 小时是 Simplus 的本地资源与安全护栏，不是对 provider 字段语法的猜测。未来若飞书返回更长期限，应先重新评估，而不是自动长期持有轮询状态。
- 自动化和不打开 URL 的探针能证明 Begin/Poll pending 兼容；创建应用与测试私聊的最终现场证据仍依赖用户主动授权。
