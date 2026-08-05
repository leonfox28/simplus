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
| 21：Host VoWiFi 短信 | 真实单段与 multipart 入站、Web/API 异步提交结果、单段与两段 UCS-2 自回环，以及自动重连后再次收发 |

## Milestone 20：公开源码准备

- [x] 定义公开产品文档与私有实验记录的边界；
- [x] 将原始 HIL、逐节点网络测试、完整现场 handoff 和旧私人归档迁出公开文档树；
- [x] 建立公开兼容性摘要、通用排障指南和发布隐私规范；
- [x] 对公开文档增加本机路径、私网地址、订阅/代理凭据和通信身份防泄漏检查；
- [x] 由仓库所有者选择并确认 PolyForm Noncommercial 1.0.0 非商业源码可用许可证，并记录单独许可材料边界；
- [x] 从脱敏工作树创建不包含现有私有历史的全新 Git 仓库；
- [x] 对全新工作树与完整新历史运行 secret scan，并人工复核首个发布树；
- [x] 经仓库所有者最终确认后创建公开远程并推送；
- [ ] 解决 Umi 构建工具链尚未消除的传递依赖审计告警，并继续复核后续 Actions、issues 和 release assets。

## Milestone 21：Host VoWiFi 短信

- [x] 按 TS 24.341/24.011 实现 RP-DATA、RP-ACK、RP-ERROR 和 binary SIP MESSAGE 的有界编解码；
- [x] 通过固定 root-only 读取发现 SIM 已配置短信中心，并仅在可用时注册 `+g.3gpp.smsip`；
- [x] 在 per-Line worker 内实现受保护 SIP 发送、`In-Reply-To` 关联的异步提交报告、入站队列和 persist-before-RP-ACK；
- [x] 通过 `simplus-netd` 类型化 API 接入既有短信服务、历史页、幂等 operation 和后台入站同步；
- [x] 增加持久化 multipart 入站分片 spool、逐分片 ACK、歧义拒绝、重启恢复和过期清理；
- [x] 在明确授权后完成真实 multipart 入站的逐片持久化、delivery report、完整重组和 worker 队列清空，并把脱敏结论写入兼容性文档；
- [x] 完成真实 GSM7 字母型发送方单段入站、业务落库、RP-ACK 与队列清空 HIL；
- [x] 将发送结果未知建模为持久 `unconfirmed` 状态并在 Web 中单独显示，保持 operation 幂等且禁止自动重发；
- [x] 用 SIP/RP fixture 证明提交请求不等待 RP 最终报告：同一 multipart 操作逐段各提交一次并先保持 `unconfirmed`，后续匹配的 RP-ACK 经持久化同步后才成为 `sent`；
- [x] 修正标准 RP-ERROR cause-length 解析、普通 SMS-DELIVER-REPORT TPDU、REGISTER `P-Associated-URI` 身份选择、RP reference 生命周期和 RFC Call-ID 校验；
- [x] 短信页面在可见期间有界自动刷新，避免后台已接收消息仍需手动刷新；
- [x] 完成一条受控单段 GSM7 服务请求的出站 RP 最终结果 HIL：SIP `accepted` 后取得关联 RP-ACK，且对应 multipart 业务回复完成持久化与重组；
- [x] 完成从公开 Web/API 发起的单段服务请求 HIL，并确认业务库由带 provider ID 的 `unconfirmed` 异步提升为 `sent`；
- [x] 完成普通号码的单段自号码回环 HIL：出站取得关联 RP-ACK，随后同一业务消息重新入站、持久化并确认；
- [x] 完成普通号码的两段 UCS-2 长短信自号码回环 HIL，并逐字符确认出站与重组后入站正文一致；
- [x] 完成自动重连后的短信行为 HIL：重新注册后再次取得出站关联 RP-ACK，并完成两段消息入站重组与确认。

## 后续产品工作

以下能力尚未完成，且不能因为 Simulator 或 Host VoWiFi 注册成功而宣称可用：

1. 真实模组原生蜂窝短信收发；
2. 真实呼入、外呼、DTMF 和媒体；
3. 真实 eUICC 已安装 Profile 切换；
4. SMS over IMS 的其他收件人互通、VoWiFi 通话和 RTP/RTCP；
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

公开仓库准备完成不等于 V1 的模组原生蜂窝短信和电话目标完成。
