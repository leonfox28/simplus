# 0018：持久 Line 与运行时硬件解析分离

- 状态：Accepted
- 日期：2026-08-05
- 修订：[`0017`](0017-managed-modems-and-capability-adapters.md) 中暂留自动 Line 作为业务绑定的迁移安排
- 后续修订：[`0019`](0019-line-identity-and-communication-paths.md) 删除 Line 接入方式，将 RF、Host VoWiFi、出口与 transport 分成独立配置

## 背景

Agent 会根据当前 USB 拓扑、模组和活动 SIM/Profile 自动生成硬件 Line。这个 ID 适合作为一次运行观察中的目标，却会随底层实现、设备重新枚举或 SIM 变化而改变。若短信、电话、出口和 Host VoWiFi 直接保存它，动态扫描事实就会再次成为业务配置；线路页面也必须理解 Agent、USB 和具体型号。

线路是管理员选择的业务入口。它需要在设备换插口和服务重启后保持稳定，同时在模组离线、SIM 更换或身份冲突时停止解析，而不是猜测绑定到另一个目标。

## 决定

1. 管理员只能从已添加 `ManagedModem` 当前观察到的 SIM/Profile 候选创建 Line。候选使用短生命周期的不可逆摘要 ID，只能提交给创建接口，不是业务身份。
2. 每条 Line 使用随机 128-bit `line_...` ID。持久记录只保存 `ManagedModem` ID、SIM 卡槽索引、SIM/Profile 的每实例身份指纹、脱敏显示提示、显示名称和接入方式；不保存 Agent Line ID、USB/sysfs 名称、设备节点或型号专用字段。
3. `ManagedModem + SIM/Profile 身份` 是唯一绑定。USB 端口变化后通过 `ManagedModem` 的设备身份重新解析；模组离线、SIM/Profile 缺失、卡槽或身份不一致以及多重匹配时，Line 分别显示离线或不可用，并且不静默改绑。
4. 动态硬件 inventory 继续生成临时硬件 Line，但它只属于观察与适配边界。Line 应用服务是唯一把稳定业务 Line 解析到当前硬件目标的组件；短信、电话、线路出口和 Host VoWiFi 只依赖这个业务目录。
5. 线路的显示名称和接入方式可以编辑，编辑不会改变硬件绑定。创建 Line 不修改 RF、不启动网络、不激活 Host VoWiFi，也不产生短信或电话副作用。
6. Web/API 只公开稳定 Line、已添加模组、脱敏 SIM/Profile 提示、状态与证据化能力。运行时硬件目标和身份指纹不得进入公开响应或普通日志。
7. Host VoWiFi supervisor 接收稳定 Line ID 和当前不可执行的硬件目标 ID：前者派生网络对象并承载运行状态，后者只用于固定 SIM preflight。旧版只含临时 Agent Line 的网络清单可以在升级时读取和清理，但不能重新启动。
8. 本纵切不提供删除。短信、通话、出口意图和 Host VoWiFi desired state 都会引用 Line；删除、停用或保留历史的语义应在出现真实需求时单独决定，不能用级联删除隐式处理。

## 后果

- 更换 USB 端口不会改变业务 Line，SIM/Profile 更换也不会被误认作原线路；
- 新模组型号只需要提供当前纵切所需的小型能力适配器，线路、短信、电话和 Host VoWiFi 不出现型号分支；
- 没有管理员创建的 Line 时，业务页面没有可操作线路，动态扫描不会自动开放通信动作；
- 旧数据库中的历史消息或运行意图可以保留，但不会自动绑定到新 Line；管理员必须显式完成“添加模组 → 添加线路”；
- 删除与硬件改绑继续 fail closed，等待单独产品决策。
