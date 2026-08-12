# 飞书一键绑定授权结果兼容修复：技术设计

## 1. Root cause

`FeishuClient.Begin` 将三个非官方保证误当成协议契约：

1. `device_code` 必须匹配 App credential 正则且不超过 512 字节；
2. 过期字段只可能叫 `expire_in`；
3. `verification_uri_complete` 只能位于 `accounts.feishu.cn`。

当前飞书中国端点的脱敏结构是：514 字节 opaque device code、`expires_in` 和 `open.feishu.cn` 验证页。device code 校验先失败；即使移除它，URL 主机校验仍会失败。此后 Poll 还有一处潜在串联缺陷：注册端点可在 HTTP 4xx 中返回正常 device-flow JSON，而当前 `doJSON` 在解码前统一拒绝非 2xx。

## 2. Boundary-preserving fix

改动只位于 `internal/application/notification/feishu.go` 及其合成测试，不改变 application state machine、HTTP/OpenAPI、Web、SQLite 或持久凭据模型。

### Begin response

- 用具名的内部 begin response 类型同时接收 `expires_in` 与兼容字段 `expire_in`。
- 当前字段优先；两者同时为正但不一致时 fail closed；两者都缺失/非正时继续使用官方 SDK 的 600 秒默认值。
- 保留 interval、expiry 和总响应体的有界检查。
- device code 只要求非空、JSON 字符串且字节长度不超过 4096；不 trim、不修改、不记录，原样经 `url.Values` 编码用于 Poll。

### Verification URL

- HTTPS 精确主机 allowlist 为 `open.feishu.cn`（当前返回）与 `accounts.feishu.cn`（兼容旧官方 SDK fixture）。
- 继续拒绝非 HTTPS、非默认端口、userinfo、fragment、空/超长 URL 和其他主机。
- `createOnly=true`、最小 `preset=false`、唯一 `im:message:send_as_bot` scope 及应用名称/描述 query 保持不变。

### Registration HTTP semantics

- 将 bounded JSON decode 与“必须 2xx”拆开。
- 只有固定 `/oauth/v1/app/registration` 调用可以解码非 2xx 且非空、未超限的 JSON；Poll 随后按 `error` 字段映射 pending、slow_down、denied、expired 或 provider failure。
- Begin 的错误 JSON 不得因为字段为空而伪装成有效结果；映射为稳定 provider failure/result-invalid，不回显 error description。
- tenant token 与 message create 继续使用 strict-2xx helper，且必须有显式 numeric `code=0`。

## 3. Test design

- 用合成当前结构复现现场：大于 512 字节、包含旧正则不接受字符的 opaque device code，`expires_in`，`https://open.feishu.cn/...`；断言 Begin 成功并保留原 device code。
- 保留 legacy `expire_in` + `accounts.feishu.cn` fixture。
- 增加 conflicting expiry fields、空/超长 device code、非 allowlist URL、userinfo/port/fragment 和超大响应拒绝测试。
- Poll fixture 用 HTTP 400 返回 `authorization_pending`/`slow_down`，随后 200 成功；denied/expired 仍映射正确。
- token/message 的非 2xx 或缺失/非零 `code` 继续失败。
- 运行 notification race tests、HTTP privacy tests 与全量回归，确认无需生成文件变化。

## 4. Operational verification

实施与检查不打开验证 URL。完成、提交并获得部署授权后，从当前源码重建 `dev` 镜像并原地执行 Compose 更新，保留 `./data`；只检查服务 health、版本以及未登录绑定路由仍返回 401。用户随后在页面主动点击并打开新 URL 完成真实验收。

## 5. Rollback

修复不涉及 schema 或持久数据。回滚只需恢复前一应用镜像/提交；正在等待的内存绑定会在进程重启后回到 idle，不产生半配置渠道。
