# 兼容性与验证边界

本文只汇总可公开的兼容性结论。真实部署地址、设备身份、运营商账户、订阅、节点、逐次命令输出和原始日志不进入公开仓库。

## 证据等级

| 等级 | 含义 |
| --- | --- |
| Designed | 只有架构或接口设计，没有可运行实现 |
| Fixture | 使用固定 transcript、fake 或 Simulator 自动化验证 |
| HIL-0 | 在真实硬件上执行固定白名单只读探测 |
| HIL | 在明确授权下完成受控硬件纵切 |
| Runtime | 正式安装形态完成生命周期和恢复验证 |

看到 USB interface、tty、QMI、UAC 或配置字段只属于观察证据，不能自动提升为业务能力。

## 模组

| 型号 | 已验证 | 尚未验证或未开放 |
| --- | --- | --- |
| QDC507 | USB identity、固定 primary AT/QMI 角色、SIM/RF/注册/通话计数的 HIL-0；PDU-mode SMS 候选在 fixture 中完成编码、传输状态和 durable replay | production 短信、真实电话、数字音频、RF 写入、eUICC mutation |
| ML307A | USB identity、固定 primary AT、SIM READY/RF Off 白名单探测；类型化 SIM AKA；Host VoWiFi ePDG/IMS 注册与持续运行 | 普通真实短信/电话、数字媒体、eUICC mutation、蜂窝数据拨号 |

两个型号共用同一个 Agent 协议、adapter registry、Modem/Line 领域模型和 Web/API；差异只位于经过证据约束的型号 adapter，不包装成两套平台。

## Mihomo 专用出口

已经验证：

- 订阅仅作为代理节点来源，Simplus 自己生成 DNS、国家组、TPROXY 和 fail-closed 规则；
- 每次创建、更新或切换都先由当前 Mihomo core 自检，失败配置不会发布或启动；
- 只有一个订阅处于 selected/running 生命周期，切换不会混用多个订阅节点；
- 国家组与 Line 分离，创建 Line 不需要修改或重启 Mihomo 配置；
- 共享 DoH 使用独立节点解析路径，业务出口和 DNS 选择不互相递归；
- `simplusd` 不持有网络 capability，网络对象只由固定 `simplus-netd` API 管理。

节点配置中的 `udp: true`、协议类型、普通 URL-Test、TCP 落地或普通 UDP 成功都不能证明 ePDG 等特定 UDP 业务可用。运行环境必须对目标协议做独立探针，并把失败限制在对应 Line。

## Host VoWiFi

已经在受控硬件上验证：

- RF Off 与 SIM READY 前置条件；
- SIM AKA、IKEv2/ePDG、IMS APN、P-CSCF、Gm transport-mode IPsec 与受保护 REGISTER；
- 定期 keepalive、服务器注册周期、提前刷新、有界重连和 XFRM 健康检查；
- `simplusd` 重启不破坏现有 session；网络 owner 重启后按持久激活意图清理并重建；
- 同一 Line 不会并存多套 namespace、路由、nftables 或 worker；
- Web/API 和普通日志不暴露身份、AKA 材料、内部地址、SPI、节点凭据或完整 SIP 鉴权头。

尚未验证：

- 显式 IMS de-registration；
- 数日稳定性和小时级 IKE/CHILD rekey；
- SMS over IMS、来电、拨号、RTP/RTCP 和数字语音媒体；
- 运营商长期策略或所有国家出口组合。

## 前端与安装

- 管理界面以 React、Ant Design Pro、Pro Components 和 Umi Max 为唯一前端栈；
- 移动端使用响应式布局和抽屉导航，桌面端保持常驻侧边栏；
- Debian `linux/amd64` bundle、全新初始化、升级、默认保留数据卸载和显式 purge 已有自动化或 smoke 证据；
- ARM64、其他发行版和签名发布链仍不是已承诺支持面。

新的兼容性声明必须写清证据等级。原始证据进入私有记录系统，公开仓库只保留可以由代码、测试或脱敏结论支撑的摘要。
