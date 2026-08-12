# 飞书绑定结果误判复发：根因证据

## Failure stage

`Notifications.tsx` 顶部 Alert 由 start mutation/query error 产生；binding error code 只在卡片内部显示。截图同时出现顶部 `FEISHU_BINDING_RESULT_INVALID` 文案和缓存 idle 文案，证明 `POST start` 同步失败，后台 Poll 尚未开始。

运行容器版本确认是 `9f9ac73`，三个服务健康；不是旧镜像未部署。

## Privacy-bounded Begin structure

一次不打开 URL的固定 Begin 请求只输出以下结构：

- HTTP 200；字段为 `device_code`、`expires_in`、`interval`、`user_code`、`verification_uri`、`verification_uri_complete`；
- device code 为 514 字节且在 4096 上限内，未记录值；
- verification URL 为 56 字节、精确 authority `open.feishu.cn`、无 userinfo/fragment，未记录路径/query/完整值；
- interval=5，`expires_in=3600`，无 legacy `expire_in`，无 error；
- 即使保守预留 1024 字节 query，最终 URL仍远小于 4096。

当前 `feishu.go` 在 expiry 校验处拒绝任何大于 900 秒的正值，因此该成功结构确定返回 result-invalid。

## Privacy-bounded immediate Poll

另一次 Begin 后立即 Poll（不打开 URL）得到：HTTP 400，字段 `code/error/error_description`，稳定 error 为 `authorization_pending`，无 client credential 或 user info。当前 registration-only non-2xx JSON 分支已正确处理，无需再次修改。

## Why the first fix missed it

第一轮探针输出了 legacy `expire_in` 为 null，却没有同时输出当前 `expires_in` 的值。设计随后只修字段名、opaque code、authority 和非 2xx JSON，保留了原始 `>900` 假设。合成 current fixture 又使用 60 秒，未复现真实 3600 秒。

## Conclusion

复发根因是 provider duration 的无依据小上限与选择性结构证据共同造成。最小根修复是把 expiry 归一化为有界 duration，接受当前一小时值，并用独立 24 小时本地安全上限防止无限期内存轮询；其余第一轮修复保持不动。

## Sanitized live acceptance

本地 `dev` 容器更新到修复版本后，用户从产品页面主动重新发起并完成授权，确认 Simplus 绑定成功，飞书中可见对应私聊。任务记录不保存授权 URL、应用凭据、用户身份、消息内容或现场截图。
