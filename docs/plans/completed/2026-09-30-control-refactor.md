# 全仓重构与连接状态通知验收记录

- 状态：Completed
- 决策：[0028：统一 control 状态、安装初始化与持久通知](../../decisions/0028-control-state-and-durable-notifications.md)
- 产品后续工作：[唯一活跃计划](../active/mvp.md)

## 交付范围

保留 Go、React、SQLite 和 control／agent／netd 三进程。control 使用新布局中的
`state/control.sqlite3`；Agent 的短信恢复数据库保持独立。安装事务同时创建管理员和
标记实例就绪，登录后直接进入管理界面。旧数据库布局拒绝启动，不自动迁移或删除。
旧设置接口、本地 CA、首次登录审查／录音目录、资源租约、旧命令账本和占位回放已移除。

短信操作按当前 Agent 实例、设备代际和设备／SIM 身份解析目标。重复请求先查持久结果；
发送后响应丢失、超时或损坏保留 `unconfirmed`，不自动重发。前端保留原操作标识与
原始内容，可在设备离线时查询已有结果；会话切换后丢弃旧异步响应。

应用服务通过小接口使用 Agent/netd、飞书、订阅下载、Mihomo 文件和进程适配器。
HTTP 按认证、设备、通信、网络和通知拆分，生产 IMS／SIP／短信／strongSwan 实现移入
`internal/ims`，HIL 保留显式验证入口。依赖检查约束应用层不引入具体协议和 I/O 适配器。
退出时停止接入、取消并等待请求及后台任务、回收所拥有的子进程和网络资源，再关闭数据库。

入站短信、未读状态和通知任务在同一事务提交后才确认硬件消息。通知 worker 独立投递，
暂时失败按 15 秒至 5 分钟退避，永久失败保留原因，重启恢复中断任务。停用渠道、取消
订阅、删除渠道或短信会取消相关未投递任务；订阅起点阻止补发此前缓存的连接事件。

新增四类连接通知，使默认订阅扩展到九类。VoWiFi 使用带实例和序号的 1024 条有序事件
缓存；蜂窝使用独立的五秒只读探测。同模组不重叠，慢对象不阻塞其他对象。首次确认在线
通知，首次离线建立基线，此后每次确认变化立即入队；未知、通信失败、漫游切换和 IMS
刷新不产生虚假变化。检查点、游标和任务原子提交；同对象、连接类型和渠道保持投递顺序。

前端按短信、线路、通知功能拆分，HTTP 快照继续作为权威状态；通知页显示待投递和失败
数量。OpenAPI、Go／TypeScript／sqlc 生成物同步更新。构建与生成分离，固定 pnpm 存储，
限制测试并发；拆分共享包后最大 JavaScript chunk 从约 640 kB 降至约 365 kB。
netd 仅挂载所需 Mihomo 数据与运行目录，不能访问 control 数据库或密钥目录。

## 问题与回归证据

| 审查问题 | 修复及行为证据 |
| --- | --- |
| Agent 重启后目标过期、设备换端口或换 SIM | `smstransport`、`modemagent`、`agentinventory` 与 messaging 测试覆盖当前实例、代际、身份和持久结果优先 |
| 提交后响应丢失导致误判可重发 | `agentapi.TestSendTreatsLostOrDamagedReplyAsUnknown`、netd 客户端和短信服务测试覆盖未知结果及请求重放 |
| 一条慢线路／模组拖慢其他对象 | messaging worker、VoWiFi service、CellularMonitor 和 AgentSource 测试覆盖独立截止时间、退避和设备互斥 |
| 短信、未读与通知不一致 | `TestInboundUnreadAndNotificationAreAtomic` 覆盖事务失败；入站同步测试验证持久化后 ACK |
| 通知重启丢失、重复入队或乱序 | `TestConnectionCheckpointAndOutboxCommitTogetherAndReplayAfterRestart` 与通知 worker 集成覆盖提交失败、重开数据库、租约、响应丢失重试和流间独立性 |
| 停用／退订／删除后仍投递 | `TestDeliveryRetryLeaseIndependentStreamsAndCancellation` 覆盖取消与迟到完成；订阅起点测试覆盖新订阅、退订重订和缓存历史 |
| 首次在线、快速断开恢复、未知状态与游标缺口 | connectivity monitor、connectivityagent 和 supervisor change 测试覆盖基线、重放、缓存溢出、来源重启、漫游、RF／SIM／拔出及 IMS 刷新 |
| worker 累计输出超过 1 MiB 停止监测 | `TestWorkerOutputContinuesPastOneMiBAndIgnoresRegistrationRefresh`；仍保留单条消息长度限制 |
| C 桥接接受畸形响应 | C fixture 覆盖协议版本、字段、重复字段、截断、尾随内容、长度和敏感缓冲清理；地址及未定义行为检查通过 |
| 退出后访问数据库或遗留子进程 | lifecycle drain、通知 worker shutdown、Mihomo 丢失 manifest 仍回收子进程及开发 supervisor 集成测试 |
| 会话切换、未知短信结果和登录过期 | SessionGate、API session 和短信表单测试；桌面／手机浏览器 fixture 流程 |
| 只有前端 fixture，缺少真实后端贯通 | `web/e2e-simulator/control.spec.ts` 启动真实 `simplusd`、临时数据库与 Simulator，覆盖初始化、登录、模组／线路、短信、电话、四类连接通知和渠道取消 |

## 验证结果

以下均已通过；测试使用合成身份、临时数据库、局部 HTTP fixture 和 Simulator。

- `make test`：Go 包测试、20 个 Web 测试文件中的 65 项测试、TypeScript、工作树清单及开发 supervisor 测试。
- `go test -race -p 4 ./cmd/... ./internal/...`；最终短信通知对象关联、五秒探测调度和构造依赖调整另经受影响包的竞态测试。
- `make lint`、`make check-format`、`make check-docs`、`make check-container-files` 和容器合同测试。
- `make verify-generated`：生成前后完整工作树清单一致。
- `go mod tidy -diff`、`go mod verify`：在仅含 Go 源码和模块清单的临时副本检查，避免遍历本地运行数据。
- `make build`、`make web-e2e`（桌面与手机）和 `make web-e2e-simulator`（真实后端）。
- `scripts/dev/test-simplus-simaka-c.sh` 以及同组 C fixture 的 AddressSanitizer／UndefinedBehaviorSanitizer 检查。
- `make security`：Go 无可达漏洞，pnpm 无已知漏洞；Go 依赖模块另有四项未被当前代码调用的漏洞提示。

## 证据边界

本记录对应源码实现及自动化验证，尚未发布包含这些变更的新版本。旧版发布包的数据库
布局和初始化流程不适用于新代码；安装说明要求使用包含决策 0028 的新发布包及独立目录。

未执行部署、真实短信／电话、RF 写入、HIL 或 clean-VM 容器生命周期验收；既有硬件
证据不能自动视为本次重构的实机回归。生产镜像安装与 clean-VM 生命周期仍在活跃计划中。

外部通知平台响应丢失时允许重复投递，不承诺外部恰好一次；未知平台错误按暂时失败重试。
蜂窝采样无法保证捕捉两次探测之间已恢复的瞬时变化。VoWiFi 来源重启或游标过期会记录
监测缺口并重新建立快照基线，不推断缺失区间内的状态。
