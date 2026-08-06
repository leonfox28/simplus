# 0017：已添加模组与小型能力适配器

- 状态：Accepted
- 日期：2026-08-05
- 修订：[`0003`](0003-v1-read-only-hardware.md)、[`0011`](0011-ml307a-host-vowifi-hil.md)、[`0012`](0012-web-managed-vowifi-runtime.md) 与 [`0016`](0016-vowifi-sms-over-ims.md) 中把扫描结果直接物化为业务模组、以及把 RF Off 作为 Host VoWiFi 固有前置条件的部分

## 背景

当前硬件 inventory 会把 Agent 扫描到的每个设备直接投影为 Modem、SIM 和 Line。这样虽然便于早期 HIL，但会让热插拔事实自动成为业务配置，也无法表达“系统发现了设备，但管理员还没有添加”。现有 `internal/modemadapter` 已统一型号识别、固定端点角色和能力证据；Host VoWiFi 使用的 SIM AKA 与射频控制仍需要形成小而明确的型号边界。

RF 是模组的物理状态，Host VoWiFi 是 Line 的会话状态。二者可以同时存在，不应由 Host VoWiFi 的启动或停止隐式互相修改。此前真实证据只覆盖 RF Off 场景，这个证据边界继续保留，但不再升级为产品模型中的协议约束。

## 决定

1. 将设备分为三个不同生命周期：
   - `DiscoveredDevice` 是本次只读扫描观察到的候选设备；
   - `ManagedModem` 是管理员明确添加并持久保存的模组；
   - `Line` 是随后绑定到一个 `ManagedModem`、SIM 卡槽或 Profile 的业务入口。
2. 模组页只展示 `ManagedModem`。“添加模组”对话框重新读取当前候选，只显示尚未添加的设备，并展示型号、系统支持状态和证据化能力。添加动作只创建管理记录，不改变 RF、SIM、网络注册或 Line。
3. `ManagedModem` 使用随机内部 ID；显示名称只供用户识别。Agent 以模组 IMEI 的每实例 HMAC 指纹作为稳定绑定键，以 USB Serial 的每实例 HMAC 指纹作为辅助观察；原始 IMEI、USB Serial、sysfs 拓扑名和 `/dev` 节点均不进入 Web API、业务数据库或普通日志。`usbN`、`N-P`、`N-P.P` 与 `N-P:C.I` 这类 sysfs 名称只用于本次扫描和端点解析，不能成为持久身份。
4. 拔出设备不会删除 `ManagedModem`，而是显示离线。重新观察到同一绑定键时恢复在线。硬件替换或改绑必须是以后单独提供的明确操作，不能只因型号相同而静默绑定。
5. 型号差异继续留在单一 `simplus-agent` 进程内。本纵切只新增当前确实需要的 `ATProbeAdapter`、`EquipmentIdentityAdapter`、`SIMPresenceAdapter`、`SIMIdentityAdapter`、`SIMAuthAdapter` 与 `RFControlAdapter`，不预建一组未来接口，也不要求所有型号实现巨型通用接口。USB Serial 由通用 USB 扫描器读取；型号 adapter 明确选择只读综合探测，并分别提供 IMEI、SIM 插入状态与用于稳定 Line 绑定的 ICCID 查询。三者独立探测，设备身份不因 RF、通话或 SIM 状态查询失败而被丢弃。SIM presence 只报告主卡槽的 `present / absent / unknown`，锁卡仍是 present；它不读取身份、不解锁 SIM，也不触发 Line。某型号没有经过实现与证据确认的能力不得注册。
6. `SIMAuthAdapter` 提供 SIM/IMS 身份读取与 AKA challenge-response 所需的固定型号边界；Host VoWiFi 只消费统一鉴权契约，不判断 ML307A、QDC507，也不读取长期密钥。短信发送、接收和电话属于 Line 业务；后续纵切只在出现真实 transport 时增加其最小接口，不把它们先建模为模组页面上的空能力。
7. 所有控制层遵守同一依赖规则：上层只提交业务意图并消费下一级类型化能力，不读取型号、VID/PID、interface、设备路径、厂商命令或原始响应。具体型号只在发现、registry 和 adapter 边界内选择；通用 AT transport 只处理 tty 生命周期、有界 I/O 和超时，不选择命令或解析厂商语义。一个实现既有能力的新型号应能在不修改 Line、短信、电话、Host VoWiFi 和 Web 控制流程的情况下接入；完整约束见 [`architecture.md`](../architecture.md) 的“分层控制与型号解耦”。
8. RF 当前状态及其控制属于 `ManagedModem`。Host VoWiFi 的激活、停用和恢复不得检查、提示或修改 RF。公开兼容性仍必须说明目前真实 Host VoWiFi 只在 RF Off 下完成验收。
9. ML307A 官方资料记载的上电/重启自动连接策略不能直接证明“上电后 RF Off”。在获得明确命令语义、可读回状态和获准 HIL 前，模组页不提供该持久设置；添加模组本身也绝不执行持久写入。
10. 本纵切先交付 `ManagedModem` 与添加对话框。现有自动 Line 投影暂作迁移桥；下一纵切建立持久 Line 后，业务页只消费管理员创建的 Line。

## 后果

- 热插拔观察和管理员配置不再是同一真相源；
- 模组更换 USB 端口后可凭同一 IMEI 指纹恢复绑定；重复 IMEI、缺少 IMEI 或身份读取失败均不得猜测绑定；
- 新型号通过注册能力 adapter 接入，线路创建和业务页面不复制型号分支；
- 下层实现可以独立演进或替换，上层只在业务语义本身变化时调整，不因厂商协议差异联动修改；
- 旧 Host VoWiFi 的 RF Off 验证结论不丢失，但 RF 与 VoWiFi 的运行控制解耦；
- 模组持久写入仍受能力声明、类型化 API、串行执行、读回确认和明确 HIL 授权约束。
