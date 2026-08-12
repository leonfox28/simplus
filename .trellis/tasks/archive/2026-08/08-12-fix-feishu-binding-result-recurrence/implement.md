# 飞书绑定 expiry 上限复发修复：实施计划

## 1. Fix and tests

- [x] 提取有界 expiry 归一化 helper，以 24 小时 duration 常量替代 900 秒魔法上限并避免整数溢出。
- [x] 将当前结构 fixture 改为 `expires_in=3600`，断言 service 进入 waiting 且期限为一小时。
- [x] 增加 24 小时边界/越界、短于 interval、冲突、default、legacy 与极大整数测试。
- [x] 确认 opaque code、exact authority、registration 4xx、Poll expiry、token/message strict success 回归不变。

## 2. Verification

- [x] `go test -race ./internal/application/notification`
- [x] `go test ./internal/api/httpapi ./internal/storage/sqlite`
- [x] `make check-format && make lint && make test && make verify-generated`
- [x] `make check-docs && make security && git diff --check`
- [x] 检查 diff/任务/日志不含临时值、完整 URL、credential、身份或 provider body。

## 3. Prevention and delivery

- [x] 使用 `trellis-break-loop` 补充分析“探针选择性报告导致错误上限幸存”的防复发机制。
- [x] 按 `trellis-update-spec` 将 provider duration 的安全上限与 fixture 字段完整性规则写入 backend code-spec。
- [x] 按 Phase 3.4 提交确认流程提交；未推送远端。
- [x] 经最终规划批准后重建 `dev` 镜像、原地更新 Compose、确认新镜像/版本/health/401；未打开 URL。
- [x] 用户重新点击、主动完成授权并确认绑定成功，飞书中可见对应私聊。

## 4. Start gate

- [x] 根因由截图路径和脱敏 Begin/Poll 结构证据闭合。
- [x] 没有未决产品、安全或兼容决策。
- [x] 用户在最终规划摘要后另行批准实施与本地部署。
