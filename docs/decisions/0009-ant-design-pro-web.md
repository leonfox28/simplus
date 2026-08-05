# 0009：Web 管理后台迁移到 Ant Design Pro

- 状态：Accepted
- 日期：2026-08-04

## 背景

当前 Web 功能仍处于早期，但已经形成登录、初始化和多个业务页面。继续扩展自制 Vue 页面后再统一后台信息架构，会增加重复迁移成本。产品需要常见的左侧导航管理后台，而不是面向消费者的展示站点。

## 决定

Web 一次性迁移到 React 19、Ant Design 6、Ant Design Pro Components 与 Umi Max。登录和初始化保持独立页面；登录后的 Dashboard、模组配置、线路配置、短信、语音通话、Mihomo 配置、通知与系统设置使用统一 ProLayout。

迁移复用现有 OpenAPI client、cookie session、double-submit CSRF 和 Go 的静态资源承载契约，不改变后端业务 API。旧 Vue、Pinia、Vue Router、Vite 与 Tailwind 实现全部删除，不长期维护双框架。

## 后果

- 新页面优先复用 Ant Design Pro 的表格、表单、描述和布局组件；
- 前端生产构建由 Umi Max 输出到 `web/dist`，仍由 `simplusd` 提供；
- TypeScript、Vitest 和 OpenAPI 生成继续作为提交前检查；
- 框架迁移本身不扩大可信局域网、单管理员或 Mihomo/硬件安全边界。
