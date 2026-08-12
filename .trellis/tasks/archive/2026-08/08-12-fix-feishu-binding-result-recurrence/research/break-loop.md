# Bug Analysis: 飞书 expiry 误判在第一轮修复后复发

## 1. Root Cause Category

- **Category**：E — Implicit Assumption（直接原因），D — Test Coverage Gap 与 C — Change Propagation Failure（第一轮修复不完整）。
- **Specific Cause**：初版代码假设一键注册最多 900 秒。第一轮已发现 provider 改用 `expires_in`，但脱敏探针只报告 legacy `expire_in` 缺失，没有报告当前字段的值；实现与 fixture 因而继续使用 60 秒并保留 900 秒上限。真实值 3600 秒在所有其他校验通过后仍被拒绝。

## 2. Why the First Fix Failed

1. **证据选择性过强**：探针验证了字段名、device code 长度和 authority，却没有列出每个参与生产校验的非敏感数值。
2. **修复按已知差异枚举，而非重新执行完整校验路径**：字段名修好后，没有用真实结构逐项重放 interval/expiry/URL 最终长度。
3. **fixture 只模拟形状，不模拟边界值**：所谓 current fixture 使用 `expires_in=60`，证明了新字段能解码，却没有证明当前 provider 的 3600 秒能通过。
4. **旧魔法值缺少来源标注**：900 秒既不是 RFC 8628、官方 SDK，也不是产品决策，因此在审查中没有明显触发重新验证。

## 3. Prevention Mechanisms

| Priority | Mechanism | Specific Action | Status |
| --- | --- | --- | --- |
| P0 | Runtime | 用命名的 24 小时本地 lifetime 常量和溢出安全 helper 替代 900 秒魔法数 | DONE |
| P0 | Regression | current fixture 使用真实结构值 3600，并从 service 层断言 waiting + 一小时期限 | DONE |
| P0 | Boundary tests | 覆盖 24h、24h+1s、短于 interval、current/legacy/default/conflict 和最大整数 | DONE |
| P0 | Cross-arch | 在 386 上执行 duration helper/current fixture，并完成 Linux arm64 编译 | DONE |
| P1 | Code-spec | 固化 Feishu expiry 归一化、错误矩阵和必测边界 | DONE |
| P1 | Test process | 隐私探针必须清点所有参与校验的非敏感字段，不能只报告字段存在性 | DONE |

## 4. Systematic Expansion

- **Similar Issues**：外部 API 的 TTL、retry-after、poll interval、cursor version、upload size 等数值都可能被旧 fixture 隐式约束。需要区分 provider 契约、产品限制与本地资源护栏。
- **Design Improvement**：数值归一化集中到小 helper，先用整数与显式上限比较，再转换 duration/size，避免溢出和散落魔法数。
- **Process Improvement**：现场结构探针的输出 schema 应由生产 validator 的输入字段反向生成；每个被读取字段必须有类型和值域摘要或明确的隐私豁免。
- **Knowledge Gap**：fail-closed 不等于越窄越安全。无依据的小上限会拒绝合法供应商状态，正确边界应兼顾明确的本地资源风险和官方行为。

## 5. Knowledge Capture

- [x] 更新 `.trellis/spec/core/backend/api-contracts.md` 的 lifetime 签名、矩阵与测试要求。
- [x] 更新 `.trellis/spec/core/backend/quality-and-testing.md` 的探针完整性、数值边界与跨架构规则。
- [x] 当前任务包含复发根因和本复盘，无需另建 issue。
- [x] 仓库没有 `src/templates/markdown/spec/`，没有项目模板副本需要同步。
