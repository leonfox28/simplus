# 飞书绑定 Begin 结果误拒绝：根因证据

## Observed product behavior

- 用户点击“绑定飞书私聊”后仍看到 idle 文案，页面显示 `FEISHU_BINDING_RESULT_INVALID` 对应中文错误。
- `StartFeishuBinding` 只有在 `FeishuRegistrar.Begin` 成功后才公开 waiting URL；因此失败发生在 Begin 或其本地结果验证，不在授权、测试消息或持久化阶段。

## Repository evidence

- `internal/application/notification/feishu.go:88-109` 只解码 `expire_in`，并用 `providerCredentialPattern` 验证 device code；该正则最大 512 字节且仅接受 `[A-Za-z0-9._-]`。
- `internal/application/notification/feishu.go:122-125` 只接受 `accounts.feishu.cn` 验证 URL。
- `internal/application/notification/feishu.go:195-197` 的 Poll 复用 strict-2xx `doJSON`；`feishu.go:297-304` 在 JSON 语义处理前拒绝任何非 2xx。
- `internal/application/notification/binding_test.go:300-322` 的原 fixture 使用短 device code、`expire_in` 和 `accounts.feishu.cn`，因此没有覆盖当前现场结构。

## Official implementation evidence

- 飞书官方 Go SDK v3.9.10 `scene/registration/registration.go` 的 `beginRegistration` 只要求 device code 和 verification URI 非空；它不把 device code 当 App credential 验证。
- 同一 SDK 使用 600 秒默认过期时间，构造 QR URL 时直接基于 provider 返回值追加 query，并在 registration request 中先解码 JSON，不把 HTTP 非 2xx 作为解码前置失败。
- 官方文档确认一键创建基于 RFC 8628，回调提供 URL/expire seconds，最终返回 Client ID/Secret 和授权用户信息：<https://github.com/larksuite/oapi-sdk-go#one-click-app-registration>。
- Hermes 的公开实现也明确说明注册端点会在 HTTP 4xx 返回 `authorization_pending` JSON，并在 Begin 只检查 device code 非空。Hermes 仅作为交叉验证；官方 SDK 和现场结构是主证据。

## Privacy-bounded live structure probe

在不打开 URL、不创建应用、不轮询和不发送消息的前提下，对固定飞书中国 Begin 端点执行一次请求，仅计算结构属性：

- 对象字段包含 `device_code`、`expires_in`、`interval`、`user_code`、`verification_uri`、`verification_uri_complete`；
- device code 非空、长度 514，未命中当前 Simplus 正则；未记录其值；
- verification URL 是 HTTPS，精确主机为 `open.feishu.cn`，未记录路径、query 或完整值；
- interval 为 5，过期字段名为 `expires_in`；
- 响应不含 provider error 字段。

该结构会先在 `feishu.go:97` 被拒绝；即使只修 device code，也会在 `feishu.go:124` 再次被拒绝。根因已闭合，无需用户提供任何敏感响应。

## Technical conclusion

这是供应商 opaque device-flow 字段被误用 App credential 校验、且合成 fixture 固化旧响应形态导致的协议兼容缺陷。最小修复是：device code 改为有界 opaque 值、兼容 `expires_in`、精确加入当前中国官方验证主机，并让 registration-only 客户端解析有界 4xx JSON；不应删除 URL allowlist、响应上限、Lark 拒绝或 token/message strict success checks。
