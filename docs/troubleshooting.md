# 通用排障指南

本文只描述可以公开的状态语义和复查顺序，不保存某台主机、某张 SIM、某个订阅或某次运行的时间线。现场分析应在私有记录系统完成，再把通用修复固化为代码、测试和这里的稳定规则。

## 先区分事实层级

1. **用户意图**：数据库中是否保存了激活、订阅选择或 Line 配置；
2. **运行事实**：`simplus-netd` 是否报告进程、网络边界和 worker 正在运行；
3. **协议事实**：ePDG、XFRM、IMS REGISTER 是否实际成立；
4. **业务事实**：短信、电话或媒体是否经过单独验证。

下层成功不能替代上层。例如 Mihomo `running` 不等于目标 UDP 可达，IMS `online` 也不等于短信或电话已可用。

## Host VoWiFi 稳定错误码

| 错误码 | 含义 | 首要复查方向 |
| --- | --- | --- |
| `STRONGSWAN_VICI_UNAVAILABLE` | strongSwan 控制接口不可用 | 服务、socket 权限和固定运行目录 |
| `STRONGSWAN_PLUGINS_MISSING` | 必需插件没有加载 | 安装版本、ABI 和固定插件路径 |
| `STRONGSWAN_CONNECTION_LOAD_FAILED` | 固定连接配置没有被接受 | 生成配置与当前 strongSwan 版本 |
| `STRONGSWAN_CONNECTION_VERIFY_FAILED` | 加载后的连接与预期不一致 | 固定 connection name 和安全参数 |
| `EPDG_CONNECT_FAILED` | ePDG IKE/CHILD 建链失败 | DNS、目标 UDP、证书/身份、SIM AKA 和出口 |
| `IMS_INITIAL_NO_RESPONSE` / `IMS_INITIAL_SEND_FAILED` / `IMS_INITIAL_READ_FAILED` | 初始 IMS REGISTER 未形成可用响应 | ePDG 内层路由、P-CSCF UDP 5060、socket 与事务超时 |
| `IMS_INITIAL_RESPONSE_INVALID` / `IMS_INITIAL_NOT_CHALLENGED` / `IMS_CHALLENGE_INVALID` | 初始或 AUTS 重同步后的 challenge 不完整、不匹配或未刷新 | Call-ID/CSeq、AKA nonce、Security-Server 和服务器拒绝类别 |
| `IMS_AKA_FAILED` | SIM AKA 未生成会话密钥，或最多两次 AUTS 重同步后仍失败 | SIM 返回状态、AUTS 重同步能力和新 challenge；不得输出 AKA 材料 |
| `IMS_XFRM_INSTALL_FAILED` | Gm transport-mode state/policy 未完整安装 | 容器 capability、数值 UDP selector、Line netns 中保留 priority/reqid 的幂等清理 |
| `IMS_PROTECTED_NO_RESPONSE` / `IMS_PROTECTED_RESPONSE_UNMATCHED` | 受保护 REGISTER 没有匹配响应 | Gm 两条 flow、嵌套 XFRM policy 和 SIP transaction |
| `IMS_REGISTER_REJECTED` | 受保护 REGISTER 收到非成功响应或成功响应不完整 | 脱敏 SIP 状态类别、注册周期和关联身份 |
| `IPSEC_STATE_LOST` | 运行所需 XFRM state 不完整 | ePDG/Gm 生命周期、DPD/rekey 和网络 owner |
| `IMS_KEEPALIVE_FAILED` | 已注册 session 的 keepalive 失败 | Gm flow、socket 状态和受保护路径 |
| `IMS_REAUTH_REQUIRED` | 刷新需要重新鉴权 | 观察 worker 是否按有界退避重新注册 |
| `IMS_REFRESH_INTERVAL_REJECTED` | P-CSCF 拒绝请求的注册周期 | 服务器 `Min-Expires` 是否被保存并复用 |
| `IMS_REFRESH_REJECTED` | 刷新收到其他非成功 SIP 响应 | 脱敏响应类别和事务状态 |
| `IMS_REFRESH_NO_RESPONSE` | 刷新等待窗口内没有匹配响应 | 两条 Gm flow、XFRM 汇总和目标 UDP 路径 |
| `IMS_REFRESH_RESPONSE_UNMATCHED` | 收到 SIP，但与当前事务不匹配 | Call-ID、CSeq 和并发刷新 |
| `IMS_REFRESH_FAILED` | 本地构造、发送、读取、解析或未分类错误 | 安全日志上下文、socket deadline 和对应单元测试 |

错误码必须保持脱敏，不得通过错误字符串补充地址、身份、SPI、节点名称或鉴权内容。

## SMS over IMS 结果语义

| 错误码 | 含义 | 行为边界 |
| --- | --- | --- |
| `IMS_SMS_ACCEPTED_AWAITING_REPORT` | 所有分段已取得 SIP `2xx`，正在等待异步 RP 最终报告 | 带 provider ID 持久化为橙色 `unconfirmed`；后台可在收到 RP-ACK 后自动提升为 `sent`，报告窗口到期则改为 `SMS_SEND_OUTCOME_UNKNOWN`，不得自动重发 |
| `SMS_SEND_OUTCOME_UNKNOWN` | 已发起提交，但 SIP transaction 的结果不完整或连接中断 | 持久化为橙色 `unconfirmed`，不得自动重发；由用户决定是否创建新的发送操作 |
| `SMS_REJECTED` / `IMS_SMS_REJECTED` | 在产生提交副作用前被 SIP 拒绝，或单段提交收到 RP-ERROR | 保留业务状态和入站分片；不要把 SIP 2xx 单独记为 sent |
| `IMS_SMS_PARTIAL_OR_REJECTED` | multipart 至少一段收到 RP-ERROR，其他段可能已经被接受 | 保持 `unconfirmed`，不得自动补发整条消息 |
| `SMS_UNAVAILABLE` | 当前 Line、IMS session 或 SIM 短信中心路由材料不可用 | 先恢复 `online` 和 `+g.3gpp.smsip` 前置条件，不得回退为伪成功 |

发送请求只等待每个分段的 SIP 最终响应，不等待 RP-ACK/RP-ERROR。后续报告通过后台同步更新已持久化消息；报告超时不会变成 `failed`。长短信仍会将同一次用户操作的所有分段各提交一次，这属于完成同一 multipart 消息，不是自动重发。若在部分分段之后发生拒绝或连接丢失，仍保持 `unconfirmed`，不能降格为确定 `failed`，也不能自动补发。

入站确认默认携带 SMS-DELIVER-REPORT 和 `In-Reply-To`；普通短信成功报告只包含 `TP-MTI=00` 与空 `TP-PI=00`，不附加虚构的 PID、DCS 或 UD 字段。若网关的 submit report 没有相关性 header，worker 只在仍占用且唯一的 RP reference 可匹配时接受并记住该会话不支持该 header。能力未知时，只有相关 delivery report 被明确以 SIP `488` 拒绝，才对同一 RP-ACK 做一次不带该 header 的兼容重试；其他拒绝、无响应或事务不匹配不切换报文形态。短信同步失败后从 15 秒开始指数退避并封顶五分钟，成功后恢复正常周期。运行诊断只允许输出 SIP/RP 类型、解析失败与关联失败计数；原始 SIP、Call-ID、身份、号码、PDU 和正文不得进入公开日志。

运营商通知和验证码可能使用 GSM7 字母型 TP-Originating-Address，而不是电话号码。入站排查必须同时覆盖该地址类型；只看到 IMS MESSAGE 却没有业务消息时，先用脱敏计数区分 PDU 解码失败、持久化失败和 RP-ACK 失败，不得为诊断输出发送方、正文或验证码。

## 固定复查顺序

1. 查看 Line 的 `state`、`stage`、`attempt`、`registeredAt`、`nextRefreshAt` 和稳定错误码；
2. 确认目标 Line 唯一解析到 SIM READY 的鉴权能力、无活动呼叫，且硬件 generation/identity fence 未变化；若要复现当前已验证基线，再单独确认 RF Off；
3. 若使用 Mihomo，确认 selected 与 running 工件一致、目标国家组存在且 core 自检成功；
4. 将 TCP、普通 UDP、DNS、STUN 和目标 ePDG UDP 分开验证，不根据节点配置字段推断；
5. 进入 IMS 阶段后，区分“请求被拒绝”“无响应”“事务不匹配”和本地失败；
6. 只查看 XFRM state 数量和累计 packet/byte 等汇总，不导出原始 state；
7. 核对每条 Line 只有一套 namespace、nftables table、policy rule、worker 和 runtime directory；
8. 修复刷新后必须同时证明状态未离开 `online`、`attempt` 未增加、注册与下次刷新时间向后推进。自动重连恢复不等于平滑刷新成功。

## Mihomo 配置失败

- 下载、YAML 解析、节点提取、生成和 core `-t` 自检是不同阶段，应保留稳定阶段码；
- 失败工件不能更新 current 指针，也不能隐式启动或重启；
- 创建或修改 Line 只绑定既有国家组，不应重写 Mihomo 配置；
- 切换订阅只在自检成功后改变 selected；是否重启必须由显式生命周期操作决定；
- controller 和 External UI 使用独立 secret，不在 Web 错误、URL 或日志中回显。

## 飞书私聊绑定错误

| 错误码 | 含义 | 建议 |
| --- | --- | --- |
| `FEISHU_BINDING_ACTIVE` | 已有等待或测试中的绑定 | 完成或取消当前等待，不要重复创建应用 |
| `FEISHU_BINDING_NOT_CANCELLABLE` | 已进入测试与持久化阶段 | 等待确定终态，避免产生不明确的半绑定 |
| `FEISHU_BINDING_DENIED` | 管理员拒绝授权 | 确认意图后重新生成短期链接 |
| `FEISHU_BINDING_EXPIRED` | 验证链接已过期 | 重新发起绑定，不复用旧链接 |
| `FEISHU_BINDING_LARK_UNSUPPORTED` | 授权结果来自当前不支持的 Lark 租户 | 使用飞书中国版租户或保留 Webhook 渠道 |
| `FEISHU_BINDING_RESULT_INVALID` | 授权结果缺少必需字段或不符合边界 | 重新发起；持续出现时检查版本与供应商状态 |
| `FEISHU_BINDING_PROVIDER_FAILED` | 注册或供应商网络失败 | 稍后重试；既有通知渠道不受影响 |
| `FEISHU_BINDING_TEST_FAILED` | 应用已授权但测试私聊失败 | 检查飞书侧应用状态；应用可能已保留，本地没有成功渠道 |
| `FEISHU_BINDING_PERSIST_FAILED` | 测试已发送但本地保存失败 | 检查 core 数据库；不要自动重发测试，飞书侧应用可能已保留 |

排查只使用状态和上述稳定码。不要复制验证 URL、App ID/App Secret、access token、
`open_id`、供应商响应正文或页面截图。删除本地渠道不会停用或删除飞书侧应用。

## 服务恢复

- `simplusd` 保存用户意图，不伪造网络运行事实；
- `simplus-netd` 启动时先清理自己的 stale manifest，再允许协调器恢复明确激活的 Line；
- Compose `agent`、`netd` 和 `app` 使用 typed health dependency。先区分 data-init、
  option/sysfs、Agent socket、netd kernel preflight 和 app HTTP 哪一层不健康，不要直接
  把 app 未启动归因于 Mihomo、ePDG 或 SIM；
- netd preflight 失败时检查 rootful/userns、AppArmor、network namespace、veth、nft
  TPROXY 与 XFRM；不得改成 privileged 或 host network 掩盖内核/权限缺口；
- Agent 看不到 tty 时检查宿主 `option` 自动加载、精确 `new_id` 映射、ttyUSB 数字 GID
  和 ModemManager 争用；不要扩大为整个 sysfs 可写或暴露任意设备 major；
- 不要在正式 worker 运行时并行启动一次性 HIL runner，也不要手工删除其网络对象。

## 公开问题所需材料

可以提供：版本、架构、稳定状态码、脱敏阶段、是否可复现、最小配置形状和自动化测试结果。

不得提供：订阅 URL、代理凭据、节点地址、真实 LAN 拓扑、设备序列号、SIM/IMS 身份、AKA 材料、完整 SIP 头、Cookie、管理员密码、数据库、抓包或原始服务日志。详细边界见 [`privacy-and-publication.md`](privacy-and-publication.md)。
