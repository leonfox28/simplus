# Simplus 当前开发交接

> 更新：2026-08-05
>
> 状态：V1 管理面与 Host VoWiFi 常驻运行纵切已完成；脱敏后的首次公开源码仓库已经建立。

本文只记录可以公开的代码状态、验证等级和下一步；现场与个人环境材料遵循 [`privacy-and-publication.md`](privacy-and-publication.md) 留在仓库外。产品范围以 [`product.md`](product.md) 为准，进程与安全不变量以 [`architecture.md`](architecture.md) 为准，任务状态以 [`plans/active/mvp.md`](plans/active/mvp.md) 为准。

## 已落地能力

### 管理与安装

- 单管理员 setup、登录、CSRF、会话撤销和修改密码；
- React、Ant Design Pro、Pro Components 与 Umi Max 后台；
- 概览、模组、线路、短信、语音、Mihomo、通知和系统设置页面；
- Debian bundle、安装/卸载脚本，以及 `simplus-agent`、`simplus-netd`、`simplusd` 三个受限 systemd 服务；
- 全新实例由安装器生成随机管理员密码，升级不覆盖凭据。

### Simulator 业务纵切

- 短信 GSM7/UCS-2、长短信、persist-before-ACK、幂等发送、会话视图和联系人；
- 电话呼入/呼出/接听/拒绝/挂断/DTMF、紧急号码前置拒绝和通话历史；
- 浏览器数字音频 fixture；
- 可拔插 eUICC 的已安装 Profile 列表、切换确认和重启持久化；
- Host VoWiFi 与 Mihomo access-path 状态机及 fail-closed 行为。

这些 Simulator 能力不会在 hardware backend 失败时伪装成真实硬件成功。

### 真实硬件与 Host VoWiFi

- Agent 使用统一 adapter registry 识别 QDC507 与 ML307A，不为每个型号复制平台协议；
- production Agent 默认只暴露类型化只读状态，不接受任意 AT/QMI 命令或设备路径；
- ML307A 的 SIM AKA 只通过固定请求结构提供给受限 Host VoWiFi worker，身份和鉴权材料不持久化；
- `simplus-netd` 独占 Mihomo、namespace、路由、nftables、strongSwan 和 XFRM 生命周期；
- Host VoWiFi 已完成真实 ePDG/IMS 注册、持续 keepalive、连续提前刷新、有界重连、停用清理和服务恢复验证；
- Web/API 只返回阶段、在线状态、出口、注册时间、下次刷新和稳定错误码，不返回内部地址、进程、SPI、P-CSCF 或鉴权材料。

### Mihomo

- core 元数据、下载、摘要校验、版本 probe 和原子安装；
- 订阅创建、编辑、更新、切换、删除及不可变 raw/generated/metadata 工件；
- 自动国家分组、共享 DoH、受限 TPROXY listener 和 fail-closed 规则；
- `simplusd` 无网络 capability，固定 Unix API 调用 `simplus-netd`；
- Zashboard 作为 Mihomo `external-ui` 托管，controller secret 私有保存。

## 已验证边界

- 完整短信、电话、数字音频和 eUICC 交互目前只在 Simulator 验证；
- QDC507 的真实短信候选驱动仅有 fixture/transcript 证据，没有进入 production Agent；
- Host VoWiFi 在线只证明 ePDG/IMS 注册与刷新，不代表 SMS over IMS、通话或媒体已经可用；
- 当前没有验证显式 IMS de-registration、数日稳定性或 IKE/CHILD 小时级 rekey；
- Mihomo 配置中的协议字段、URL-Test 或普通 UDP 成功不能替代目标业务的真实 UDP/ePDG 探针；
- 项目只面向可信 LAN，不应直接暴露到公网。

更细的能力等级见 [`compatibility.md`](compatibility.md)，运行故障按 [`troubleshooting.md`](troubleshooting.md) 处理。原始硬件日志、订阅节点、真实拓扑和逐次排错记录不属于公开仓库。

## 当前下一步

1. 跟踪并处理 Umi 构建工具链当前未消除的传递依赖审计告警，不以降低审计门槛代替上游修复；
2. 继续复核二进制发布产物中的第三方许可证、源码提供义务和 notice；
3. 后续真实短信、电话、媒体或 eUICC 写能力必须分别建立决策、实现与获准 HIL。

## 验证入口

根据改动风险选择最小有效检查：

```bash
make check-docs
make verify-generated
make test
make build
```

真实硬件操作不属于普通验证。HIL-0 只读探测可以按开发文档执行；SIM AKA、网络建链和任何外部通信都必须满足对应决策与明确授权。
