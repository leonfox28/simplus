# Bug Analysis: 飞书有效 Begin 响应被本地过度校验拒绝

## Bayesian diagnosis

### Priors

| Hypothesis | Prior | Reason |
| --- | ---: | --- |
| H1：飞书服务或网络返回错误 | 35% | 外部注册端点失败是常见原因 |
| H2：Simplus 请求参数与官方协议不一致 | 30% | 自定义窄客户端可能遗漏 SDK 行为 |
| H3：Simplus 对有效响应做了错误的额外验证 | 35% | 页面稳定错误是 result-invalid，且失败发生在 Begin |

### Discriminating evidence and update

- 代码证据：表单字段与官方 SDK 一致，降低 H2；Begin 对 device code、field name 和 URL host 增加了 SDK 没有的约束，提高 H3。
- 脱敏结构探针：官方端点返回成功对象且无 error，显著降低 H1；其 514 字节 opaque code、`expires_in` 和 `open.feishu.cn` 会连续触发本地拒绝，使 H3 超过 99%。
- 官方 SDK/Hermes：device code 只做非空处理，registration 状态可位于非 2xx JSON，再次支持 H3，并暴露出修完 Begin 后的串联 Poll 风险。

结论置信度超过 99%：缺陷来自本地隐式假设与不真实 fixture，而非用户操作、飞书账户或网络。

## 1. Root Cause Category

- **Category**：E — Implicit Assumption（主因），D — Test Coverage Gap 与 A — Missing Spec（促成因素）。
- **Specific Cause**：实现把 provider-owned `device_code` 错当成 App credential，复用了 512 字节受限字符正则；同时假定旧字段 `expire_in` 和旧验证 authority。原测试 fixture 恰好复制这些假设，没有代表当前官方返回，也没有覆盖 registration 4xx JSON 语义。

## 2. Why Fixes Failed

1. **初次实现研究充分但契约提取不完整**：阅读了官方 SDK的一键注册流程，却在自定义安全客户端中加入了未由官方协议保证的 device-code grammar 和单一 authority，没有逐条标注“官方保证”与“本地防御”。
2. **合成测试形成封闭自证**：测试使用短字母数字 code、`expire_in`、`accounts.feishu.cn` 和全 2xx 响应，恰好与生产实现一致，因此能证明代码内部一致，却不能证明供应商兼容。
3. **第一轮独立审查关注 fail-closed 强度**：修复了过期轮询和显式成功码，但没有一份当前 provider Begin 结构作为对照，无法发现过度校验。
4. **没有先做最小结构探针**：在不打开 URL、不创建应用的前提下，本可安全观察字段名、长度、authority 和 HTTP status；缺少这一步让现场差异直到用户点击才暴露。

## 3. Prevention Mechanisms

| Priority | Mechanism | Specific Action | Status |
| --- | --- | --- | --- |
| P0 | Architecture | registration-only bounded JSON/status policy 与 strict-2xx token/message policy 分离 | DONE |
| P0 | Runtime validation | opaque code 只做非空、4096 字节上限、瞬时保存和原样 round-trip；验证 URL 使用精确 authority allowlist | DONE |
| P0 | Test coverage | 加入当前结构、legacy 结构、4xx device-flow 状态、expiry 冲突、authority 混淆及 strict token/message 回归 | DONE |
| P0 | Code-spec | 在 backend API spec 固化字段、状态、错误矩阵与 Wrong/Correct 示例 | DONE |
| P1 | Test guideline | 外部协议 fixture 必须锚定官方实现/当前脱敏结构，opaque 字段不得复用其他 credential grammar | DONE |
| P1 | Review evidence | 安全允许时先做只输出字段/类型/长度/authority 的结构探针，不采集真实值 | DONE |

## 4. Systematic Expansion

- **Similar Issues**：扫描确认当前 `device_code` 只存在于飞书注册客户端；`providerCredentialPattern` 继续用于 App ID/Secret、`open_id` 与 access token，不应全局删除。未来 OAuth/device-flow、异步 job token、cursor 或 challenge 等 provider-owned opaque 字段都应遵循同一规则。
- **Design Improvement**：把 transport 的“有界读取/JSON 解码”和 operation 的“允许哪些 HTTP status/业务状态”分层，避免一个通用 helper 同时过宽或过窄。
- **Process Improvement**：新增外部协议时，测试矩阵至少有一条 fixture 来自官方实现当前字段形态，并明确哪些校验由协议保证、哪些是本地安全上限。
- **Knowledge Gap**：opaque 不代表无限信任；正确做法是限制大小、来源、生命周期、公开面和使用位置，而不是猜测字符集。

## 5. Knowledge Capture

- [x] 更新 `.trellis/spec/core/backend/api-contracts.md` 的飞书短期授权可执行契约。
- [x] 更新 `.trellis/spec/core/backend/quality-and-testing.md` 的外部协议 fixture 与 opaque-state 测试规则。
- [x] 在本任务保存脱敏根因证据和本复盘；无需另建 issue，根修复已在当前任务完成。
- [x] 检查模板同步目标：本仓库没有 `src/templates/markdown/spec/`，项目 code-spec 无对应模板副本可同步。
