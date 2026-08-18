# Audit layer boundaries

## Goal

对 Simplus 代码库进行一次静态架构审查，确认生产运行时各层只通过允许的下一层公开、类型化接口协作，识别跨层调用、边界绕过和非硬件适配层直接操作模组的代码，并形成可复核的证据清单与整改建议。

## Repository Evidence

- `docs/architecture.md:90-100` 将硬件控制依赖固定为 `Web/API -> application/Line -> typed service port -> Agent capability -> model adapter -> device protocol`，并规定型号、VID/PID、interface、sysfs、`/dev`、AT/QMI 只能在发现、registry、adapter 或运行时硬件边界参与实现选择。
- `docs/architecture.md:334-346` 规定 Web/API 不得提交任意 AT/QMI/设备路径、netd 独占底层网络对象，并要求每个控制层只依赖下一级类型化能力。
- `.trellis/spec/core/backend/directory-structure.md:8-19` 将 `cmd/**` 定义为依赖装配根，允许其同时构造 API、application、storage、Agent/supervisor client 与硬件实现；装配根的多层 import 因而不能仅凭路径距离判为运行时越层。
- `.trellis/spec/core/backend/directory-structure.md:69-80` 禁止 application service 在 consumer-owned port 足够时依赖具体 SQLite/Agent 实现，也禁止 domain 持有 SQL、HTTP 映射、模组命令或文件系统路径。
- 初步 Go import 图显示 HTTP 层同时引用 application、OpenAPI 和用于响应映射的 domain 类型；application 层主要引用 domain 与 typed Agent/supervisor contracts，但已有需在正式审查中核实的具体依赖候选：`internal/application/resourcelease/service.go:14,46-50` 暴露 SQLite-owned 类型，`internal/application/setup/service.go:27,84,233-235` 装配具体 filesystem/secret 默认实现。
- Web 手写页面使用生成的 Query/SDK 契约，原始 HTTP/SSE 机制集中在 `web/src/api/runtime.ts:55` 与 `web/src/app/RealtimeBridge.tsx:48` 等边界。`web/src/pages/Modems.tsx:140-148` 直接使用生成 SDK 完成不进入 mutation cache 的敏感读取，是必须通过责任和调用链而非关键词判断的代表候选。
- 初步关键词扫描把底层 AT 命令定位在 `internal/modemadapter/**`，把 sysfs/设备节点访问定位在 `cmd/simplus-agent/main.go` 与 `internal/hardwareprobe/**`；这只是候选分布，尚不能证明不存在别名、封装或间接越层。
- 现有规范有意允许部分跨包集成测试使用临时真实 SQLite；测试依赖需与生产依赖分开定性。
- `internal/api/openapi/generated.go`、`internal/storage/sqlite/generated/**`、`web/src/api/generated/**` 和 `web/dist/**` 是生成或构建输出，不是手写责任边界。

## Scope and Judgment

- 硬性合规范围是产品自有的手写生产 Go、TypeScript/React、strongSwan SIM-AKA C 组件与生产入口/构建脚本，包括进程装配、Web、公开 HTTP、application、domain、storage/filesystem、typed Unix client/server、Agent/netd、hardware discovery、model adapter/driver、protocol transport、Compose/container/release/packaging 边界和产品 C 插件；锁定输入中的上游 `p-cscf` 实现仍按第三方源码处理。
- 测试与开发/HIL 工具作为次级范围单列结论；生成输出只审查来源、调用方式与公开契约；第三方依赖、`.tools/**`、构建产物和私有运行数据不做内部实现审查。
- 非相邻 import、共享 domain 类型和具体类型泄漏只产生候选。只有调用链证明绕过下一层类型化能力、在非装配层构造具体实现、直接访问更底层存储/协议，或让上层根据型号、端点、路径、命令等实现细节分支时，才判为运行时越层违规。
- `cmd/**` 装配根、OS-specific runtime、生成边界、共享被动 domain 类型和有意的集成测试不会自动视为违规，但仍检查其中是否混入业务策略、底层参数上浮或任意命令入口。
- 本任务只生成任务内审查报告，不修改产品代码、规格、生成输出、配置或依赖，也不执行部署、硬件探测、真实通信、RF/SIM/eUICC 变更、HIL-1/HIL-2 或任何具有设备/网络副作用的操作。

## Requirements

- R1. 依据规范和实际装配代码建立可审计的层级、包职责与允许行为边清单，而不是仅凭目录名推断分层。
- R2. 按上述范围逐层检查实际依赖和行为调用，覆盖所有产品运行时 owner；没有发现问题的层也必须留下可复核记录。
- R3. 专项检查 AT/QMI/APDU、串口/tty、`/dev`、sysfs、USB/interface、VID/PID、shell/process、底层网络参数和任意 vendor payload 是否只存在于其合法 owner，且未通过 Web/API 或 typed Unix 边界暴露。
- R4. 每个候选必须经过接口、构造和具体调用链核实；结论区分已确认违规、架构气味/具体类型泄漏、允许例外、测试/工具发现和误报。
- R5. 每项已确认违规或架构气味记录严重度、当前 `file:line`、调用方层、目标层、调用链、违反的规则、影响、最小整改方向及不依赖 HIL 的建议验证方式。
- R6. 输出覆盖矩阵，列出每个运行时 owner 的允许边、实际出站行为、候选数量和结论。
- R7. 在 `.trellis/tasks/08-14-audit-layer-boundaries/audit.md` 交付执行摘要、允许边矩阵、覆盖矩阵、分类发现、静态审查限制、剩余风险和优先整改建议。

## Acceptance Criteria

- [x] AC1. 分层与允许行为边模型同时有规范和实际装配代码证据，并覆盖全部产品运行时 owner。
- [x] AC2. 每个产品层均有审查状态，其直接行为依赖已与允许边比对；无发现的层也出现在覆盖矩阵中。
- [x] AC3. 所有跨层或直接模组操作候选均完成调用链复核，并以当前 `file:line` 证据归入一个明确类别。
- [x] AC4. 每个已确认问题均有严重度、风险、最小整改方向和安全验证建议，且审查没有自动实施修复。
- [x] AC5. `audit.md` 明确静态审查限制与剩余风险，不把未发现静态引用等同于运行时绝对不存在违规。
- [x] AC6. 审查只改动本任务目录，未读取私有运行数据，也未触发部署、真实 SMS/通话、RF、SIM/eUICC、模组持久写入、HIL-1/HIL-2 或任意直接硬件/网络操作。
- [x] AC7. 报告将生产违规、架构气味/具体类型泄漏、允许例外、测试/工具发现和误报分栏记录，读者无需重新执行扫描即可理解每项结论的证据链。

## Out of Scope

- 自动修复发现的问题，或因审查结果更新产品规范、公开文档和架构决策。
- 动态插桩、运行时流量观测、部署环境探测、真实硬件验证或私有证据采集。
- 对第三方依赖、生成文件内部实现、缓存、构建产物和私有数据做架构合规审查；只有产品代码如何调用这些边界属于范围。
