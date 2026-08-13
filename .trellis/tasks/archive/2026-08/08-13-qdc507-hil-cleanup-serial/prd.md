# 清理 QDC507 HIL 并验证序列号

## Goal

从生产仓库中移除上次 QDC507 实机验收产生的一次性 build-tag HIL runner、命令入口、专用恢复/分类/清理实现及其孤立支撑代码，同时保留已经验收并用于生产的 QDC507 原生蜂窝短信能力。按移远 EG25-G 的固定只读方法确认当前大疆 4G 模块能否读取独立模块序列号，并在成功时让产品的“序列号”字段使用该值。

## Background

- `internal/qdc507hil` 共有 8,857 行 Go 代码；对应的七组 `cmd/simplus-qdc507-hil*` 命令另有约 1,456 行。实质实现只在 `qdc507_hil` build tag 下编译，默认/生产容器不包含这些入口。
- 除上述目录外，`internal/modemadapter/qdc507sms`、`internal/hardwareprobe`、`internal/storage/sqlite`、`internal/modemadapter`、Makefile 和公开文档中还存在仅服务这些 runner 的 tagged 文件、窄化 adapter/router/transport、subscriber-number HIL seam 和说明；只删除顶层目录会留下无生产消费者的代码。
- 生产必须保留完整 `QDC507SMS -> Driver -> SQLiteStateStore -> SMSRouter -> Agent` 收发路径、PDU 编解码、恢复/ACK 语义、operation gate、设备/SIM fence 和已经通过的普通测试。
- 当前设备的 USB descriptor 没有 iSerial，所以产品的 `serialNumber` 为空；稳定设备身份已经通过 IMEI 指纹绑定，管理员可显式 no-store 读取 IMEI。
- 移远官方 EG25-G 产品页列出适用的 EC2x/EG2x/EG9x/EM05 AT Commands Manual；移远官方技术论坛给出的 EG25-G 固件响应为 `AT+CGSN=? -> +CGSN:"sn,imei"(0,1)`，表明 `AT+CGSN=0` 是 SN、`AT+CGSN=1` 是 IMEI。当前 QDC507 adapter 只发无参数 `AT+CGSN`，没有尝试 SN form。
- 原功能提交、任务归档提交和 journal 提交仅存在于本地 `main`，相对 `origin/main` ahead 3，尚未 push。用户已明确选择在完成清理后重写这三条本地提交，并清理本地 reflog/unreachable objects，使 HIL-only 源码不出现在最终可达 Git 历史中。

## Requirements

- R1 — 删除全部一次性 QDC507 HIL 实现：删除 `internal/qdc507hil`、全部 `cmd/simplus-qdc507-hil*`、所有 `qdc507_hil` tagged supporting files、Makefile compile-only target，以及公开文档中声称这些 runner 仍可构建/执行的内容。
- R2 — 删除孤立支撑面：在删除 runner 后，通过引用审计移除只服务 HIL 的 QDC507 inbox/outbound adapter、split router/backend、dedicated TTY/driver/store、strict classifier、subscriber-number HIL seam 和测试；保留任何仍被生产短信路径使用的最小公共实现。
- R3 — 保留生产功能：QDC507 原生蜂窝短信的生产 registry、Agent backend、SIM `SM` 暂存、durable v2 state、入站 persist-before-ACK、出站 outcome-unknown、call-mode safety 和 Line transport 选择行为不得回退。
- R4 — 固定只读序列号探测：仅针对唯一当前 QDC507，在独占 endpoint 后依次执行 bounded `ATI`、`AT+QGMR`、`AT+CGSN=?`、`AT+CGSN=0`、`AT+CGSN=1`；不发写命令，不改变 RF/SIM/注册/短信/数据状态。查询结束后恢复原 Compose owner，并核对健康状态。
- R5 — typed 序列号能力：若真实固件明确支持且 `AT+CGSN=0` 返回与 `AT+CGSN=1` 可区分、格式有界的 SN，则把它封装在 QDC507 adapter 的固定只读能力中，并用于 Managed Modem `serialNumber`；若 unsupported/malformed/与 IMEI 混淆，则保持 serial unavailable，不猜测、不用 IMEI 冒充序列号。
- R6 — 隐私：手机号、模块 SN、IMEI、短信内容等低敏感私有值可在本次一对一对话中按用户明确授权直接读取和显示，但不得进入 Git tracked 文件、测试 fixture、任务/规范/公开文档、普通日志或提交信息。凭据、私有端点、SIM 身份、raw PDU/transcript 等更敏感材料仍只报告必要结论。
- R7 — 容器：代码完成并通过门禁后，重建当前 `dev` 三镜像并原地更新同一 Compose 服务；保留数据目录，最终 `agent/netd/app` 必须健康且运行新提交 revision。

## Acceptance Criteria

- [x] AC1：当前工作树中不存在 `internal/qdc507hil`、`cmd/simplus-qdc507-hil*`、`qdc507_hil` build tag、QDC507 HIL Make target或仅描述已删除 runner 的公开操作文档。
- [x] AC2：引用/类型审计和普通构建证明没有 HIL-only adapter/router/driver/store/subscriber-number seam 残留；生产 QDC507 SMS 纵切仍完整。
- [x] AC3：固定只读实机探测给出当前模块的 manufacturer/model/firmware、SN support、SN 和 IMEI 结论；获批低敏感值只显示在对话，不写入仓库。
- [x] AC4：若 SN 可用，Managed Modem/API/Web 的现有 `serialNumber` 显示它，并有 synthetic parser/adapter/service/UI 回归；若不可用，有 unsupported/malformed 回归且 UI 仍显示“未提供”。
- [x] AC5：focused Go tests、全量 `make lint`/`make test`、生成物、Web build/E2E、container/docs/privacy checks 全部通过；自动化不接触真实 modem/SIM。
- [x] AC6：本地 Compose 更新成功，持久数据保留，`agent/netd/app` 健康，HTTP health 通过，revision 指向新工作提交。
- [x] AC7：`origin/main..HEAD` 被重建为不含 HIL-only 源码的新提交序列；旧三个 commit hash 不再被任何 ref/reflog/object 解析，不创建 backup branch/tag，不 push。

## Out of Scope

- 重新执行、保留或替代任何短信 HIL runner；再次发送/接收/删除短信。
- RF、SIM、蜂窝注册、数据 bearer、电话或 modem persistent configuration 变更。
- 把手机号、IMEI 或 SIM 身份改成普通公开字段或持久化业务数据。
- 支持未实测的其他移远型号；QDC507 仍通过 typed adapter 接入，上层不出现 EG25-G/AT 分支。

## Key Decision

- 最终不增加一个仅“删除当前树”的提交；而是以 `origin/main` 为父提交重建清理后的功能提交、原任务归档和 journal，再归档本任务。旧三条未推送提交在验证新历史后清理 reflog 并立即 prune，不保留恢复引用。
