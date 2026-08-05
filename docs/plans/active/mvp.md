# 活跃计划：可信局域网通信控制 MVP

- 状态：In Progress
- 产品范围：[`../../product.md`](../../product.md)
- 架构：[`../../architecture.md`](../../architecture.md)
- 公开状态：[`../../handoff.zh-CN.md`](../../handoff.zh-CN.md)

## 目标

在 Debian Linux 主机上提供单管理员 Web 后台，以统一类型化接口管理 4G/5G 模组、线路、短信、电话、可拔插 eUICC 已安装 Profile，以及 Host VoWiFi 的专用 Mihomo 出口。

完整业务交互先在 Simulator 验证。真实硬件默认保持只读；已经单独决策并验证的 Host VoWiFi 路径仅在 RF Off 下使用固定 SIM AKA、ePDG/IMS 和专用网络生命周期，不扩大为通用硬件写平台。

## 已完成里程碑

| 里程碑 | 结果 |
| --- | --- |
| 0：范围与开发环境 | 缩减产品范围，建立文档地图、Linux 本地工具链和验证入口 |
| 1：短信纵切 | Simulator 完成编码、长短信、持久化、幂等、入站 ACK 和会话 Web；真实 Agent 保持只读 |
| 2：多模组与日常使用 | 双线路并发边界、联系人、历史删除、容量提示和热插拔刷新 |
| 3：电话控制 | Simulator 完成呼入/呼出、接听、拒绝、挂断、DTMF、紧急号码拒绝和历史 |
| 4：数字音频 | Simulator 完成浏览器麦克风与扬声器 fixture；硬件无未验证媒体入口 |
| 5：eUICC | Simulator 完成已安装 Profile 列表、A→B→A、读回确认和持久化 |
| 6–7：Host VoWiFi 与 Mihomo 模型 | Simulator 完成 access-path 状态机、专用出口和 fail-closed |
| 8：可安装版本 | Debian bundle、systemd 服务、安装/卸载和 fresh-instance smoke |
| 9–11：管理完整性 | 管理员维护、模组/线路页面、Mihomo 管理和单向通知渠道 |
| 12–15：Simplus-owned Mihomo | 有界订阅转换、不可变工件、国家组、共享 DoH、TPROXY 与最小权限 supervisor |
| 16：管理后台迁移 | React、Ant Design Pro、Pro Components 和 Umi Max 单一前端栈 |
| 17：External UI | 固定版本 Zashboard、Mihomo 托管和私有 controller secret |
| 18：真实 Host VoWiFi 纵切 | ML307A 类型化 SIM AKA、ePDG、Gm IPsec 和最小 IMS 注册 |
| 19：Web 管理的持续运行态 | per-Line 生命周期、keepalive、提前刷新、有界重连、恢复和脱敏 Web 状态 |

## Milestone 20：公开源码准备

- [x] 定义公开产品文档与私有实验记录的边界；
- [x] 将原始 HIL、逐节点网络测试、完整现场 handoff 和旧私人归档迁出公开文档树；
- [x] 建立公开兼容性摘要、通用排障指南和发布隐私规范；
- [x] 对公开文档增加本机路径、私网地址、订阅/代理凭据和通信身份防泄漏检查；
- [x] 由仓库所有者选择并确认 PolyForm Noncommercial 1.0.0 非商业源码可用许可证，并记录单独许可材料边界；
- [ ] 从脱敏工作树创建不包含现有私有历史的全新 Git 仓库；
- [ ] 对全新工作树与历史运行 secret scan，并人工复核生成物、Actions、issues 和 release assets；
- [ ] 经仓库所有者最终确认后创建公开远程并推送。

## 后续产品工作

以下能力尚未完成，且不能因为 Simulator 或 Host VoWiFi 注册成功而宣称可用：

1. 真实模组短信收发；
2. 真实呼入、外呼、DTMF 和媒体；
3. 真实 eUICC 已安装 Profile 切换；
4. SMS over IMS、VoWiFi 通话和 RTP/RTCP；
5. 显式 IMS de-registration、IKE/CHILD rekey 与多日稳定性；
6. ARM64、其他发行版、签名包和供应链发布材料。

每项真实副作用都需要独立设计、最小实现、明确授权和与风险相称的 HIL；Web/API 不得暴露任意 AT/QMI、设备路径、网络命令或配置路径。

## 非目标

- 公网 SaaS、多租户和远程入站控制；
- 通用代理、通用蜂窝数据网关或完整 eSIM RSP；
- 企业 PKI、企业审计、Web 自更新和备份恢复平台；
- 为未来假设场景预建通用硬件命令或安全编排框架。

## 验收方式

按风险选择最小有效验证：

1. 受影响包的针对性单元或集成测试；
2. Simulator 用户路径；
3. OpenAPI 生成一致性、Web typecheck 和 build；
4. 受影响二进制构建；
5. 公开资料检查与 secret scan；
6. 真实硬件只执行当前任务明确授权的 HIL。

公开仓库准备完成不等于 V1 的真实短信和电话目标完成。
