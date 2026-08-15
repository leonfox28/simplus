# Journal - Leon (Part 1)

> AI development session journal
> Started: 2026-08-07

---



## Session 1: Bootstrap and repair Trellis project context

**Date**: 2026-08-07
**Task**: Bootstrap and repair Trellis project context
**Package**: core
**Branch**: `main`

### Summary

Initialized Trellis and Codex integration, corrected core/web package resolution, restored concise project safety boundaries, and added source-backed backend, infrastructure, documentation, and frontend specs with full implement/check verification.

### Git Commits

| Hash | Message |
|------|---------|
| `28b413e` | (see git log) |
| `3687001` | (see git log) |

### Status

[OK] **Completed**


## Session 2: 完成前端与通信重构

**Date**: 2026-08-10
**Task**: 完成前端与通信重构
**Package**: core
**Branch**: `main`

### Summary

以 Vite、React Router、直接使用 Ant Design 和 TanStack Query 替换 Umi/Pro 前端运行时，新增 HTTP 权威快照加鉴权 SSE 失效同步，修复首次配置与页头布局，并按产品决定隐藏暂缓的 eUICC 界面；完成测试、构建和本地容器验证。

### Git Commits

| Hash | Message |
|------|---------|
| `cc02810` | (see git log) |
| `588f6c4` | (see git log) |
| `83feb39` | (see git log) |
| `6a9ceef` | (see git log) |
| `8527529` | (see git log) |

### Status

[OK] **Completed**


## Session 3: 重做短信会话页面

**Date**: 2026-08-10
**Task**: 重做短信会话页面
**Package**: core
**Branch**: `main`

### Summary

完成按收件人聚合的短信会话、持久化未读状态、响应式主从聊天界面、线路故障闭锁与联系人管理；全量质量门禁和桌面/移动端 Chromium E2E 通过，并将本地 Compose 服务更新到 107b1de。

### Git Commits

| Hash | Message |
|------|---------|
| `f8391ad` | (see git log) |
| `2dc4241` | (see git log) |
| `107b1de` | (see git log) |

### Status

[OK] **Completed**


## Session 4: 修复短信会话时间乱序

**Date**: 2026-08-11
**Task**: 修复短信会话时间乱序
**Package**: core
**Branch**: `main`

### Summary

引入 messages v8 单调 record sequence 与 SMS v2 cursor，统一后端会话/历史顺序并让 Web 只消费服务端顺序；补齐迁移、分页、并发、桌面/移动 Chromium 回归和独立检查，将本地 Compose 更新到 b815a7b，并只读确认既有缺陷记录已按发送到收到排序。

### Git Commits

| Hash | Message |
|------|---------|
| `f20cd83` | (see git log) |
| `d36c2d6` | (see git log) |
| `b815a7b` | (see git log) |
| `06cab9d` | (see git log) |

### Status

[OK] **Completed**


## Session 5: 优化短信页布局并更新本地容器

**Date**: 2026-08-11
**Task**: 优化短信页布局并更新本地容器
**Package**: core
**Branch**: `main`

### Summary

让短信工作区填满主内容区并补充桌面布局边界回归；Web 类型检查、单元测试、生产构建和 Chromium E2E 通过，并将本地 Compose 服务更新到 c4c4dd5。

### Git Commits

| Hash | Message |
|------|---------|
| `c4c4dd5` | (see git log) |

### Status

[OK] **Completed**


## Session 6: Feishu private-message channel binding

**Date**: 2026-08-12
**Task**: Feishu private-message channel binding
**Package**: core
**Branch**: `main`

### Summary

Implemented and independently verified zero-form Feishu private-message binding with create-only minimal permissions, test-before-persist encrypted app credentials, local-only unbind, Web/API/storage/UI coverage, ADR/docs, and synthetic-only validation.

### Git Commits

| Hash | Message |
|------|---------|
| `6fbef74` | (see git log) |
| `fa20c82` | (see git log) |

### Status

[OK] **Completed**


## Session 7: Fix Feishu binding result compatibility

**Date**: 2026-08-12
**Task**: Fix Feishu binding result compatibility
**Package**: core
**Branch**: `main`

### Summary

Diagnosed the live Begin shape without retaining temporary values, fixed Feishu opaque device-code, expires_in, exact verification-authority, and registration-only non-2xx JSON compatibility, added synthetic/race regressions, independently reviewed and codified prevention rules, and updated healthy local dev containers without real authorization or delivery.

### Git Commits

| Hash | Message |
|------|---------|
| `60e4066` | (see git log) |
| `9f9ac73` | (see git log) |

### Status

[OK] **Completed**


## Session 8: Fix recurring Feishu binding expiry rejection

**Date**: 2026-08-12
**Task**: Fix recurring Feishu binding expiry rejection
**Package**: core
**Branch**: `main`

### Summary

Closed the recurring Feishu Begin rejection by accepting the observed one-hour expiry through an overflow-safe 24-hour local bound, added cross-architecture and full regression coverage, strengthened provider-probe guidance, deployed healthy local dev containers, and recorded user-confirmed successful private-chat binding without retaining sensitive evidence.

### Git Commits

| Hash | Message |
|------|---------|
| `9579ea0` | (see git log) |
| `57f9f1e` | (see git log) |

### Status

[OK] **Completed**


## Session 9: QDC507 原生蜂窝短信

**Date**: 2026-08-13
**Task**: QDC507 原生蜂窝短信
**Package**: core
**Branch**: `main`

### Summary

完成 QDC507 中国联通原生蜂窝短信纵切、统一 typed capability/Line transport、durable recovery 与生产容器装配；受控入站和出站 HIL 通过，完整质量门禁通过，并更新本地 dev Compose。后续清理一次性 HIL 源码时重建了该未推送提交。

### Git Commits

| Hash | Message |
|------|---------|
| `5cc4214` | (see git log) |

### Status

[OK] **Completed**


## Session 10: 清理 QDC507 HIL 并接入模块序列号

**Date**: 2026-08-13
**Task**: 清理 QDC507 HIL 并接入模块序列号
**Package**: core
**Branch**: `main`

### Summary

移除全部一次性 QDC507 HIL 源码及孤立支撑，保留生产原生蜂窝短信；按实测固定 CGSN 协议接入模块序列号并贯通 Agent、Managed Modem/API/Web；重建三条未推送本地历史并 prune 旧对象；重建 dev 三镜像、原地更新 Compose，服务与 HTTP health 全部通过。

### Git Commits

| Hash | Message |
|------|---------|
| `5cc4214` | (see git log) |

### Status

[OK] **Completed**


## Session 11: Line phone number observations

**Date**: 2026-08-14
**Task**: Line phone number observations
**Package**: core
**Branch**: `main`

### Summary

Added typed QDC507 subscriber-number observation, propagated it through Agent and inventory, made Line the unified cellular/IMS owner, rendered all distinct values in Web, verified the real local Line UI, and deployed healthy dev images for revision 97b06e5.

### Git Commits

| Hash | Message |
|------|---------|
| `97b06e5` | (see git log) |

### Status

[OK] **Completed**


## Session 12: Audit layer boundaries and retire native deployment

**Date**: 2026-08-14
**Task**: Audit layer boundaries and retire native deployment
**Package**: core
**Branch**: `main`

### Summary

Completed repository-wide layer-boundary audit, removed the unsupported native Debian production path, strengthened Compose host conflict checks, synchronized docs/specs, and preserved legacy private state for explicit follow-up.

### Git Commits

| Hash | Message |
|------|---------|
| `0ca25d4` | (see git log) |
| `7013707` | (see git log) |

### Status

[OK] **Completed**


## Session 13: Move background policy into application coordinators

**Date**: 2026-08-14
**Task**: Move background policy into application coordinators
**Package**: core
**Branch**: `main`

### Summary

Resolved audit V-02 by moving SMS synchronization and Agent change semantics from cmd/simplusd into application-owned coordinators with narrow ports, preserving realtime, notification, retry and cancellation behavior, and adding deterministic race-tested coverage plus executable backend specs.

### Git Commits

| Hash | Message |
|------|---------|
| `024cbfb` | (see git log) |

### Status

[OK] **Completed**


## Session 14: Make Setup dependencies explicit

**Date**: 2026-08-15
**Task**: Make Setup dependencies explicit
**Package**: core
**Branch**: `main`

### Summary

Resolved audit V-03 by replacing hidden Setup store assertions and concrete security/filesystem defaults with explicit application-owned dependencies and values, moving production adapter assembly into cmd/simplusd, preserving the fixed instance key path and active Setup behavior, and adding deterministic integration/race coverage plus backend specs.

### Git Commits

| Hash | Message |
|------|---------|
| `d66860f` | (see git log) |

### Status

[OK] **Completed**


## Session 15: Make Mihomo supervisor composition explicit

**Date**: 2026-08-15
**Task**: Make Mihomo supervisor composition explicit
**Package**: core
**Branch**: `main`

### Summary

Resolved audit V-04 by moving local/socket Mihomo supervisor selection into cmd/simplusd, requiring a validated typed application dependency, replacing process-launching application tests with a fake, and recording the executable boundary contract. Independent review and safe race, Go, lint, docs, and ownership checks passed without runtime or hardware actions.

### Git Commits

| Hash | Message |
|------|---------|
| `ffbc69b` | (see git log) |

### Status

[OK] **Completed**


## Session 16: Isolate notification Webhook delivery

**Date**: 2026-08-15
**Task**: Isolate notification Webhook delivery
**Package**: core
**Branch**: `main`

### Summary

Resolved audit V-05 by introducing an application-owned typed Webhook port, moving fixed WeCom/Feishu bot target validation and HTTP protocol into a concrete adapter assembled by cmd/simplusd, preserving legacy ciphertext/API/outcome behavior, and strengthening credential-safe error tests. Independent implementation/spec review and safe race, broad Go, lint, generated-drift, docs, ownership, and privacy gates passed without any external delivery or runtime action.

### Git Commits

| Hash | Message |
|------|---------|
| `04e1770` | (see git log) |

### Status

[OK] **Completed**
