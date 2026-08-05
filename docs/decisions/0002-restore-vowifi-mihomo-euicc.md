# 0002：恢复 VoWiFi、Mihomo 与 eUICC 管理

- 状态：Accepted
- 日期：2026-08-03
- 修订：`0001-product-scope-reset.md` 中移除这三项能力的决定

## 背景

第一次范围重置将 Host VoWiFi、Mihomo 和 eUICC 管理与企业级安全、发布平台等冗余一起移出了产品。用户随后明确确认，这三项仍是项目需要的核心能力。

范围精简的目标是删除不必要的安全和平台工程，而不是改变产品对不同蜂窝接入路径和可拔插 eUICC 的需求。

## 决策

### Host VoWiFi

Host VoWiFi 恢复为完整产品的必需能力：

- 使用 SIM/eUICC 提供的运营商鉴权；
- 在主机建立 ePDG/IMS 路径；
- 至少验证一家真实运营商的短信、呼入、呼出和双向音频；
- 与模组内部原生蜂窝短信/VoLTE 明确分开。

具体使用 strongSwan、独立 IMS worker 或其他组件，应由实现阶段的兼容性 spike 决定，不在当前文档中预先锁死完整平台架构。

### Mihomo

Mihomo 恢复为 Host VoWiFi 的可选专用出口：

- 每条 Host VoWiFi Line 选择 `direct` 或 `mihomo-required`；
- 只代理该 Line 的 entitlement/ePDG/IMS 流量；
- 不接管宿主全局代理，不提供普通蜂窝数据、热点或 LAN 网关；
- `mihomo-required` 失败时保持该 Line 离线，不回退 direct。

### eUICC

可拔插 eUICC 管理恢复为必需能力，最低范围是：

- 读取 EID；
- 列出已安装 Profile；
- 显示活动 Profile；
- 串行切换 Profile 并重新读取确认；
- 保存每个 Profile 对应的 Line/接入配置。

SM-DP+ Profile 下载、删除和完整 eSIM RSP 尚未获得明确范围决定，暂不承诺。

## 实施顺序

这些能力是最终产品要求，但不回到“所有高级能力阻塞第一条短信”的旧模式：

1. 原生蜂窝单模组 SMS；
2. 多模组和电话控制；
3. 数字音频；
4. eUICC 已安装 Profile 管理；
5. Host VoWiFi direct；
6. Mihomo required egress；
7. 可安装版本和兼容性收束。

## 不恢复的旧范围

本决定不恢复企业连接器、内建 PKI 生命周期、hash-chain 审计、签名升级平台、备份恢复、多租户或多发行版认证门槛。
