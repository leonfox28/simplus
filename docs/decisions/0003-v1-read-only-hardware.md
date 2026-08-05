# 0003：V1 真实硬件运行态只读

- 状态：Accepted
- 日期：2026-08-03
- 修订：`0001`、`0002` 中要求真实模组写操作和运营商 HIL 的 V1 验收方式

## 背景

用户要求继续完成完整 V1，并允许项目实际连接 QDC507、ML307A 等模组，但明确禁止 V1 操作真实模组开启射频、建立蜂窝数据流量或执行其他写操作。原计划中的真实短信、电话、eUICC Profile 切换、Host VoWiFi 运营商鉴权和 Mihomo 真实出口 HIL 都会产生模组、SIM、网络或外部通信副作用，不能继续作为本轮 V1 的真实硬件验收条件。

## 决策

V1 分成两个明确运行面：

1. **Simulator 完整业务面**：实现并验收短信、电话、数字音频状态、eUICC 已安装 Profile 管理、Host VoWiFi direct、Mihomo required 和安装交付的完整产品交互与失败语义。
2. **真实硬件只读面**：允许枚举 USB、识别型号和端点，并通过固定白名单 AT 查询读取固件、RF 当前状态、SIM 状态、注册、信号、当前网络、活动通话数量和 USB 配置；不得改变这些状态。

生产 `simplus-agent` 使用不可配置的 read-only handler：

- 不注册 `radio.ensure-off`；
- 不注册真实 SMS list/read/send/ACK；读取短信也可能改变 unread 状态，因此不属于只读探测；
- 不注册 dial/answer/reject/hangup/DTMF；
- 不注册 eUICC enable/disable/switch/delete/download；
- 不创建蜂窝 PDP context、网络接口、路由、NAT、Host VoWiFi 或 Mihomo 数据路径；
- 不提供可通过命令行或环境变量重新打开上述能力的开关；
- `/v1/hello` 显式声明 `hardware-read-only-policy-v1`。

候选写驱动可以保留为 fixture/Simulator 证据，但不得从生产 Agent main 装配。Web/API 在 hardware backend 中只能展示只读状态，所有业务写操作必须明确 unavailable，不能回退到 Simulator 后伪装成功。

## 只读白名单

当前 QDC507 只允许握手和查询形式：`AT`、`AT+CGMI`、`AT+CGMM`、`AT+CGMR`、`AT+CFUN?`、`AT+CPIN?`、`AT+QSIMSTAT?`、`AT+CREG?`、`AT+CGREG?`、`AT+CEREG?`、`AT+COPS?`、`AT+QNWINFO`、`AT+CSQ`、`AT+CLCC`、`AT+QCFG="USBCFG"`。新增查询必须先证明无状态变化并加入测试；不允许把任意 AT/QMI 或设备路径暴露到 Web/API。

ML307A 在安全主控口和只读命令白名单获得证据前保持 descriptor-only。

## V1 完成标准变化

完整业务流程由 Simulator、fixture、持久化重启测试和浏览器/HTTP smoke 验收；真实硬件只要求完成只读连接、热插拔恢复和状态展示。未来若要恢复真实短信、电话、Profile 切换、VoWiFi 或数据出口，必须由用户做出新的明确决定和逐项副作用授权。
