# 修复短信会话时间乱序实施计划

## 1. 执行前置与约束

- 本计划不授权实现。只有用户审批最终规划摘要后，主会话才运行 `task.py start`。
- 这是一个原子跨层交付；messages v8、cursor、store/service、Web 和文档必须作为同一兼容版本完成。
- Phase 2 按 Trellis sub-agent 流程执行；实现与检查 agent 都从 active task 的 curated context 开始，且不得回退其他人的改动。
- 修改 schema version、cursor version、查询顺序、索引或前端 helper 前先全仓搜索；不手改生成 Go/TypeScript 文件。
- 不执行真实短信、电话、RF、模组持久写、Host VoWiFi 操作或 HIL。测试只用合成数据，现场数据只做必要的只读、脱敏核对。

## 2. WP0 — 激活、基线与 durable decision

依赖：用户审批最终规划。

- [x] 运行 `task.py start` 并加载 Phase 2.1/`trellis-before-dev` 上下文。
- [x] 确认工作树与当前 Compose 基线，保留用户改动；运行 focused storage/messaging/Web ordering tests。
- [x] 新增 ADR 0024，局部 supersede ADR 0023 的 SMS created-time ordering，冻结 record sequence、cursor compatibility 和 display-time 边界。

验证：

```bash
git status --short
go test ./internal/storage/sqlite ./internal/application/messaging
corepack pnpm --dir web test -- messages Messages
```

## 3. WP1 — Messages v8 与 sequence cursor

所有权：messages migration/store、pagination domain、co-located Go tests。

- [x] 增加 `00008_sms_record_sequence.sql`，重建 messages 表、回填 sequence、复制 unread ledger、更新 indexes/schema version，并实现对称 Down。
- [x] 扩展 cursor codec：Calls v1 不变；新增有 kind/version 的 SMS v2 sequence cursor及 v1 compatibility decode。
- [x] 将 global/remote/Line+remote history、conversation latest/order、last outbound Line 和 next cursor 改为 sequence。
- [x] 保证 new outbound/inbound 首次插入自动分配 sequence，replay/status/delete 行为不改变或复用序号。
- [x] 增加 migration、foreign key、unread watermark、历史缺陷回填、同毫秒/时钟偏差、分页/删除/concurrency 测试。

验证：

```bash
go test ./internal/domain/pagination ./internal/storage/sqlite
go test -race ./internal/storage/sqlite
```

回滚点：migration、cursor/store 必须一起回滚；Down 保留业务消息与 unread marker。

## 4. WP2 — Messaging service、HTTP/OpenAPI 契约

依赖：WP1 sequence/cursor 稳定。

- [x] 让 messaging service 为 SMS encode/decode v2，并对 v1 做过渡；Calls 明确拒绝 SMS cursor kind。
- [x] 更新 OpenAPI 的 newest-first 描述，不公开 sequence；必要时通过既有 generator 更新产物。
- [x] 覆盖 handlers 的消息/会话顺序、旧/新 cursor、非法 kind/version/length、稳定分页错误和公共时间字段不变。
- [x] 确认 unread read-through token 与 SSE publish/invalidation 无改动。

验证：

```bash
make generate
go test ./internal/application/messaging ./internal/application/calls
go test ./internal/api/openapi ./internal/api/httpapi
make verify-generated
```

## 5. WP3 — Web 服务端顺序消费

依赖：WP2 response/cursor 契约。

- [x] 用“拼接服务器页面后整体反转”的纯 helper 替换 `createdAt/message ID` 排序，保持数组不可变。
- [x] 更新 Messages Vitest：反向业务时间、同时间随机 ID、多页、conversation last/preview、SSE refetch 与 scroll anchor。
- [x] 更新 synthetic Playwright desktop/mobile fixture，证明 outbound 先显示、later-persisted inbound 后显示且 preview 正确。
- [x] 保持 bubble 元信息显示原 `createdAt`，不公开 sequence。

验证：

```bash
corepack pnpm --dir web test -- messages Messages
corepack pnpm --dir web typecheck
corepack pnpm --dir web e2e
```

## 6. WP4 — 文档、规范与独立检查

- [x] 更新 ADR 0024、`docs/architecture.md`、active MVP follow-up 和相关 backend/frontend Trellis specs，删除 SMS createdAt-keyset 陈述，保留 Calls 条款。
- [x] 使用 `trellis-update-spec` 做 source-backed 一致性检查。
- [x] 派发 `trellis-check` 独立检查 PRD、迁移、cursor 兼容、跨层顺序、unread、Web、隐私和测试质量；修复确认的问题。

验证：

```bash
make check-docs
git diff --check
```

## 7. WP5 — 全量门禁、提交与本地部署

- [x] 运行 targeted→broad gates；只修本任务归因问题。
- [x] 确认 diff 不含号码、Line/SIM/设备身份、截图、日志、数据库或私密现场材料。
- [x] 按逻辑边界提交代码/文档；不 push。
- [x] 构建三个 `dev` 镜像并 `SIMPLUS_IMAGE_TAG=dev docker compose up -d` 原地更新。
- [x] 检查 data-init/bootstrap exit 0、app/agent/netd healthy、镜像 revision、HTTP 200；不发送真实短信。

```bash
make check-format
make verify-generated
make lint
make test
make security
make build
make check-docs
corepack pnpm --dir web e2e
git diff --check
make container-build CONTAINER_IMAGE_TAG=dev
SIMPLUS_IMAGE_TAG=dev docker compose up -d
```

## 8. 完成定义

- [x] 所有 PRD acceptance criteria 有对应自动化测试或部署观察证据。
- [x] 已有缺陷形态和未来写入都按首次本地持久化顺序展示；分页、摘要和最近 Line 一致。
- [x] 迁移与 Down 保留消息/unread，Calls 与公共 JSON/SSE 行为不回归。
- [x] 全量 gate 与独立 Trellis 检查通过，工作树无私密材料。
- [x] 本地运行容器是本次产品提交且健康；没有真实通信副作用。

## 9. 实现证据（2026-08-11）

- focused Go：`go test ./internal/domain/pagination ./internal/storage/sqlite ./internal/application/messaging ./internal/application/calls ./internal/api/openapi ./internal/api/httpapi` 通过；
- storage race：`go test -race ./internal/storage/sqlite` 通过；
- focused Web：18 个 Vitest 文件、62 个测试通过，`pnpm --dir web typecheck` 通过；
- synthetic browser：desktop/mobile Chromium 共 2 个 Playwright 测试通过；
- broad gates：`make check-format`、`make verify-generated`、`make lint`、`make test`、
  `make security`、`make build`、`make check-docs` 和 `git diff --check` 通过；
- 非失败 warning：Vitest/jsdom 报告不支持 pseudo-element `getComputedStyle`；Playwright
  WebServer 报告 `FORCE_COLOR` 覆盖 `NO_COLOR`；Vite 报告既有大于 500 kB chunk；
- 独立 `trellis-check` 未发现产品代码缺陷，并补强 sequence non-reuse、Down/Up unread
  水位、service cursor continuation 与会话预览的断言；全部门禁重跑通过；
- 产品代码按 `f20cd83`、`d36c2d6`、`b815a7b` 三个逻辑边界提交，未 push；
- 三个 `dev` 镜像构建为 revision `b815a7bece69` 并原地更新 Compose；data-init/bootstrap
  退出码为 0，app/agent/netd healthy，本机 HTTP 返回 200；
- messages schema 为 v8，对报告缺陷对应既有记录的脱敏只读核对得到
  `outbound -> inbound`；未输出或保存号码、Line、消息 ID、正文、数据库或截图；
- 未执行真实短信、RF、模组写入或 HIL。
