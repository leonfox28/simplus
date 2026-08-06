# 0019：Line 身份与通信路径解耦

- 状态：Accepted
- 日期：2026-08-06
- 修订：[`0011`](0011-ml307a-host-vowifi-hil.md) 与 [`0012`](0012-web-managed-vowifi-runtime.md) 中把 RF Off 作为产品运行前置条件的部分，以及 [`0018`](0018-persistent-lines-and-runtime-resolution.md) 中由 Line 保存和编辑互斥接入方式的部分

## 背景

持久 Line 已经把管理员业务身份和 Agent 的临时硬件目标分开，但 Line 仍保存 `cellular-native / host-vowifi-only / hold-rf-off` 三选一的接入方式。这个枚举把几类本来可以独立存在的状态错误地绑定在一起：模组射频属于 `ManagedModem`，Host VoWiFi 是 Line 的会话意图，Mihomo 是该会话的一种出口，短信和电话则各自由实际装配的 transport 决定。

继续使用互斥枚举会产生隐式副作用：创建 Line 时必须提前选择未来通信方式，切换用途可能影响射频或 VoWiFi，尚未配置出口又容易被误解成直连。它也阻碍同一 Line 在具备多个经过验证的 transport 后按业务能力独立工作。

## 决定

1. Line 固定为“管理员已添加的 `ManagedModem` + 当前 SIM/Profile 身份 + 卡槽”的稳定业务对象。持久记录只保存随机 Line ID、不可改绑的身份关系、脱敏提示和可重复的显示名称，不保存接入方式、RF 策略、Agent Line ID、设备路径或型号字段。
2. 添加 Line 只提交当前候选 ID 和显示名称。服务必须重新扫描并确认同一模组、卡槽和 SIM/Profile 身份仍唯一可解析；候选过期、SIM 变化、设备离线、重复添加或身份冲突均拒绝。添加动作不修改 RF、不启动或重启 Mihomo、不激活 Host VoWiFi，也不发送短信或发起通话。
3. Line 创建后不允许改绑；本阶段不提供删除。唯一身份是随机 Line ID，显示名称可以重复且只能单独修改。
4. RF 状态、运行时 RF 开关及以后经过验证的上电策略只属于 `ManagedModem`。Host VoWiFi 的激活、停用和恢复不检查、不提示也不修改 RF。现有真实 Host VoWiFi 证据只覆盖 RF Off，这仍是兼容性证据边界，不是产品耦合。
5. Host VoWiFi 只有在稳定 Line 当前可解析、声明 `hostVoWifiAuth` 且管理员已明确选择出口时才可激活。新 Line 的出口为 `unconfigured`，绝不能隐式直连；写入只接受显式 `direct` 或当前订阅存在的 `mihomo-country`。修改出口不重写 Mihomo 配置，也不自动切换 VoWiFi 运行态。
6. 旧数据库升级时保留 Line ID、名称、SIM 绑定、已有国家出口和 `desired_active`。仍存在于更早 Mihomo egress-profile 表中的启用国家选择，在当前 Line 尚无绑定时一次性提升为 `mihomo-country`；旧 `host-vowifi-only` Line 如果之后仍没有持久出口，因旧语义曾把缺失解释为直连，迁移时物化为显式 `direct`。新建 Line 不继承这些兼容规则。
7. 短信和电话不读取 Line 接入方式。业务服务依据实际装配的 transport、Line 的证据化能力及该 transport 所需的实时可用状态 fail closed；本决策不选择原生蜂窝短信或电话 transport。
8. 公共 API 删除 access-mode 与 `rfSafety` 字段和修改入口；同时删除已无消费者、会重复保存出口的旧 Simulator access-path 与 Mihomo egress-profile API、应用服务和表，出口只保留 Line egress 这一处真相源。候选接口返回每个已添加模组的实时型号、USB Serial、脱敏 SIM/Profile 提示、能力和类型化不可添加原因，但不返回 SIM 身份指纹、Agent/USB 运行时目标、sysfs 或 `/dev` 路径。

## 后果

- 同一 Line 的身份不再因射频、VoWiFi、出口或 transport 配置变化；各配置可以独立保存和演进；
- 新 Line 在管理员明确选择出口前保持不可激活，避免无意直连；
- 旧数据库保持既有业务身份与运行意图，同时删除不再有语义的 access-mode 真相源；
- 线路页面成为稳定 Line 列表和配置入口：添加弹窗只选择候选，配置抽屉分别保存名称、出口和 Host VoWiFi 意图，不提供 RF 控制；
- 新 transport 必须通过独立的小型类型化能力接入，不得重新引入全局接入模式或型号分支。
