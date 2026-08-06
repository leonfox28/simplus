# 0012：Web 管理的 Host VoWiFi 持续运行态

- 状态：Accepted
- 日期：2026-08-05
- 扩展：[`0011`](0011-ml307a-host-vowifi-hil.md) 的一次性 ML307A HIL
- 后续修订：[`0017`](0017-managed-modems-and-capability-adapters.md) 与 [`0019`](0019-line-identity-and-communication-paths.md) 删除产品运行时的 RF 前置检查，并要求新 Line 显式配置出口

## 背景

ML307A 的受控测试 Profile 已经通过一次性 root-only HIL 完成 ePDG EAP-AKA、IMS AKA、Gm IPsec 和受保护 REGISTER `200 OK`。该证据证明协议与硬件路径可行，但临时脚本会在成功后立即撤销网络状态，不能让管理员从日常 Web 页面激活 Line，也不能在 IKE rekey、SIP 注册到期、进程重启或短时网络故障后持续恢复。

用户现在明确要求把这条路径做成正式产品能力：管理员在 Web 中激活或停用 Host VoWiFi，并由系统持续保活。

## 决定

1. `simplusd` 保存每条 Line 的 `desired_active` 用户意图并提供受管理员 session、ready gate 和 CSRF 保护的激活/停用 API。全新 Line 默认停用；服务重启后只恢复此前明确激活的 Line。
2. `simplus-netd` 是唯一 Host VoWiFi 网络运行态 owner。它通过固定 typed Unix API 接受 Line ID 和已经持久化的 `direct` 或 Mihomo 国家出口选择；协议不接受 shell、AT/APDU、设备路径、任意 binary/config 路径、路由、nftables 或 strongSwan 参数。
3. `simplus-netd` 为每条运行 Line 独占创建命名空间、veth、策略路由、nftables、strongSwan、ePDG/Gm XFRM 和 IMS 注册 worker。它在安装形态中以 root 运行，但继续使用 `NoNewPrivileges`、固定可执行文件、私有运行目录、受限地址族和 systemd filesystem/device/kernel hardening；不把任何网络 capability 授予 `simplusd` 或 Web。
4. `simplus-agent` 继续独占 ML307A AT/APDU 端点。只有 root `simplus-netd` 及其同一 cgroup 内的 strongSwan 插件能访问单独的 mode `0600` SIM AKA socket；管理 HTTP API 永不返回 IMSI、IMPI、原始 IMPU、AKA challenge/keys、鉴权头或节点凭据。IMS REGISTER 明确授权 `tel:+...` 或 `sip:+...@...` 国际号码时，可以只提取并返回规范化 E.164 手机号；不得把无 `+` 的私有身份或任意 SIP 用户名猜成手机号。
5. 激活前必须重新观察：唯一目标 ML307A、相同 SIM 伪名、SIM READY、RF Off、无活动呼叫、Host VoWiFi capability 和可用出口。`mihomo-country` 还必须要求当前选择订阅已经实际运行且包含该国家；失败时不得回退 direct。
6. 在线状态只在 ePDG IKE/CHILD SA、IMS AKA、Gm SA 和受保护 REGISTER 2xx 全部成立时产生。worker 维持 IKE DPD/rekey、定期 IMS keepalive，并在注册有效期内提前刷新。任一阶段失效时立即撤销 online，执行有界退避重连；停用、SIM/Agent generation 改变、进程退出或系统重启必须清理该 Line 的全部临时网络对象。
7. Web 只显示 `stopped/starting/connecting/registering/online/reconnecting/stopping/failed`、当前出口、运营商明确返回的 E.164 手机号、最近上线时间、下次刷新时间和稳定错误码。手机号只属于当前在线运行事实，重连、停用或 worker 退出时立即清空；不得显示 PID、SPI、IPsec key、内层地址、P-CSCF、原始运营商身份或内部网络命令。
8. 本决策只把已经验证的 ML307A 注册路径产品化。它仍不授权短信、拨号、接听、媒体、eUICC Profile 切换、PIN/PUK、`CFUN` 写入、蜂窝数据拨号或模组持久设置；这些能力继续需要各自的实现和明确授权。

## 后果

- 一次性 HIL helper 继续保留为诊断和回归工具，但 Web 不直接调用 shell runner；生产状态机复用同一套协议解析和安全前置检查；
- SQLite 的 `desired_active` 是用户意图真相源，`simplus-netd` 的实时状态是运行事实真相源；二者不互相伪造；
- `simplusd` 启动后进行一次恢复协调，随后按状态查询展示结果；运行失败不会把 Line 错报为在线；
- 真实短信和电话仍未完成，因此“VoWiFi 在线”当前只证明长期 ePDG/IMS 注册路径，不代表短信、来电、外呼或音频已经可用。
