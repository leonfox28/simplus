# 0011：受控启用 ML307A Host VoWiFi 实机 HIL

- 状态：Accepted
- 日期：2026-08-04
- 修订：[`0003`](0003-v1-read-only-hardware.md) 对 ML307A Host VoWiFi 实验的全面禁止
- 后续修订：[`0017`](0017-managed-modems-and-capability-adapters.md) 与 [`0019`](0019-line-identity-and-communication-paths.md) 将 RF 从 Line/Host VoWiFi 产品运行条件中解耦；本记录的 RF Off 仅保留为当次 HIL 证据边界

## 背景

Simulator 已验证 Host VoWiFi 状态机，Mihomo 也已生成按国家独立、同时监听 TCP/UDP 的 TPROXY 入口，但真实 Line 尚未接入。本决策授权使用 ML307A 中一个受控测试 Profile 尝试真实 VoWiFi 激活。

ML307A 官方资料确认 `2ecc:3012` 的 USB Interface `02` 是 AT 口。本机 HIL-0 已进一步确认该端口返回 `ML307A-DSLN-MTSH1S00`、SIM `READY`、`CFUN=4`，并接受 `CRSM`、`CSIM`、`CCHO`、`CGLA` 的能力查询。

## 决定

1. 正式安装的默认硬件策略继续保持 read-only；本决策只为 ML307A Host VoWiFi 纵切增加显式、类型化且可关闭的实验能力，不开放任意 AT/APDU 或设备路径。
2. ML307A adapter 固定把 Interface `02` 识别为 primary AT。只读 probe 可读取型号、固件、RF、SIM 锁状态和 ICCID；ICCID 必须在 Agent 内立即转换成每安装实例的 keyed pseudonym 和末四位提示，原值不得进入 Web/API、数据库或日志。
3. 只有 SIM `READY`、身份伪名存在且 RF 明确为 `off` 时，才物化真实 SubscriptionProfile 和 Line。新 Line 默认 `hold-rf-off`，管理员显式选择 `host-vowifi-only` 后仍不得打开蜂窝 RF。
4. Line 的接入模式与 Mihomo 配置分离。当前激活订阅预先生成每个实际国家的固定 listener；Line 只保存 `direct` 或一个国家 listener 绑定，创建或修改 Line 不重写或重启 Mihomo。
5. 本次实机授权允许：从当前活动 SIM 读取 AKA 所需身份、把 ePDG 给出的 RAND/AUTN 通过类型化 SIM 鉴权动作提交给卡、建立 IKEv2/ePDG 与最小 IMS 注册路径，以及为该 Line 创建和清理专用网络命名空间、路由与 TPROXY 规则。AKA/APDU 请求和响应、IMSI、ICCID、IMS 私有身份与鉴权头不得持久化或记录。
6. 本决策不授权真实短信发送、拨号、来电处理、eUICC Profile 切换、PIN/PUK 写入、蜂窝数据拨号、`CFUN` 写入或模组持久配置。需要这些动作时必须再次取得对应授权。
7. 任一前置条件、DNS、代理 UDP、IKE、证书校验、AKA、路由或 IMS 步骤失败时，该 Line 保持离线且 RF Off；`mihomo-required` 不得回退 direct。实验结束或进程异常退出时必须撤销临时网络状态。

## 后果

- ML307A 从 descriptor-only 提升为有证据的只读 Modem/SIM/Line，但 Host VoWiFi 业务能力只有在独立 HIL 组件完成后才标记可用；
- `simplus-agent` 继续是唯一硬件端点 owner，`simplus-netd` 继续是唯一网络特权边界；
- 实机成功标准必须包含 ePDG IKE SA、SIM AKA 成功和 IMS 注册的脱敏状态证据，单纯 TCP 出口或端口可达不算激活成功；
- 本授权只适用于记录在私有 HIL 中的受控 ML307A 激活尝试，不自动授权未来其他 SIM、运营商或有费用的通信动作。
