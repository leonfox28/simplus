# 飞书一键绑定授权结果兼容修复：实施计划

## 1. Protocol parsing

- [x] 在 `feishu.go` 为 begin response 增加当前 `expires_in` 与 legacy `expire_in` 的有界归一化。
- [x] 将 device code 从 App credential 正则中分离，作为非空、最大 4096 字节的 opaque 临时值；禁止日志/公共错误输出。
- [x] 将验证 URL 精确 allowlist 扩展为 `open.feishu.cn` 与 `accounts.feishu.cn`，保留 HTTPS、端口、userinfo、fragment 和长度约束。
- [x] 拆分 registration-only bounded JSON decode 与 strict-2xx OpenAPI decode，使 4xx pending/slow-down/denied/expired 可按 JSON 处理，但 token/message 语义不放宽。

## 2. Regression coverage

- [x] 增加当前飞书 Begin 结构的完全合成 fixture，证明 514+ 字节/特殊字符 device code、`expires_in` 和 `open.feishu.cn` 可进入 waiting。
- [x] 覆盖 legacy fixture、冲突 expiry、空/超长 device code、非 allowlist URL、URL authority 异常和超大响应。
- [x] 覆盖 HTTP 400 pending/slow-down 后成功、denied/expired 和未知错误；确认 token/message 非 2xx 与显式 code 检查不变。
- [x] 运行 `go test -race ./internal/application/notification` 与 `go test ./internal/api/httpapi`，确认状态机/隐私回归。

## 3. Knowledge and delivery

- [x] 修复后使用 `trellis-break-loop` 分析“过度校验供应商 opaque 字段 + fixture 未贴近现场结构”的成因和预防措施。
- [x] 按 `trellis-update-spec` 更新 backend API/quality code-spec，固化 opaque state、registration-only status policy 和 fixture 证据规则。
- [x] `make check-format`、`make lint`、`make test`、`make verify-generated`、`make check-docs`、`make security` 与 `git diff --check` 全部通过。
- [x] 按 Phase 3.4 提交确认流程提交，未推送远端。
- [x] 经用户明确部署授权后重建 `dev` 镜像、原地更新 Compose、确认新镜像/版本/health/401 契约；未打开真实 URL。

## 4. Start gate

- [x] 根因已有不含临时值的结构证据。
- [x] 产品、安全和兼容边界没有未决决策。
- [x] 用户审阅最终规划摘要后另行明确批准实施与本地 `dev` 容器更新。
