# 0016：Host VoWiFi 的 SMS over IMS 纵切

- 状态：Accepted
- 日期：2026-08-05
- 扩展：[`0012`](0012-web-managed-vowifi-runtime.md) 的 per-Line IMS worker

## 背景

Host VoWiFi 已经能在 ML307A、RF Off 和受控出口下建立 ePDG、Gm IPsec 并持续维持 IMS 注册。产品下一步需要复用这条 IMS 会话收发传统短信，而不是启用模组蜂窝短信命令，也不能把 SIP、APDU、设备路径或任意网络参数开放给 Web。

SMS over IMS 不是纯文本 SIP MESSAGE。3GPP TS 24.341 要求使用 `application/vnd.3gpp.sms` 承载 TS 24.011 RPDU，RP-User-Data 再承载 TS 23.040 TPDU。SIP `202 Accepted` 只确认 IP-SM-GW 接受 SIP transaction；移动始发短信的最终提交结果由后续 SIP MESSAGE 中的 RP-ACK 或 RP-ERROR 表示。

## 决定

1. 只有 SIM 的短信中心配置通过固定只读接口成功取得时，IMS REGISTER 的 Contact 才声明 `+g.3gpp.smsip`。缺少或非法配置时继续允许基础 IMS 注册，但 SMS 必须 fail closed。
2. ML307A 首个实现通过 root-only、固定 `AT+CSCA?` 读取已配置的 TS-Service-Centre-Address，并按 TS 24.341 允许的回退规则构造 `tel:` PSI。该接口不接受用户命令、文件 ID、APDU、设备路径或自定义 URI，结果不进入管理 API、日志或持久存储。
3. 每条 Line 的 worker 是受保护 SIP socket、Service-Route、RP reference、出站提交事务和待确认入站消息的唯一 owner。`simplus-netd` 只代理固定的短信发送、入站 list/read/acknowledge、出站报告 list/acknowledge JSON 操作；`simplusd` 和 Web 不接触 IMS 身份、P-CSCF、内层地址、Security-Verify、Call-ID 或原始 RPDU。报告诊断只保留 SIP/RP 类型、解析失败和关联失败计数。
4. 出站短信先按现有 GSM7/UCS-2 规则编码为一个或多个 SMS-SUBMIT TPDU，再逐个封装为 MS→network RP-DATA。worker 收齐各分段的 SIP 最终响应后立即返回，不同步等待 RP 最终报告；SIP `2xx` 只产生带 provider ID 的 `accepted` 结果，业务库先持久化为 `unconfirmed` 且不得自动重发。后续匹配的 network→MS RP-ACK 才异步提升为 `sent`，RP-ERROR 才按单段明确失败或 multipart 可能部分生效进行归类；有界报告窗口到期或响应未知始终保持 `unconfirmed`，但会结束“等待确认”显示。RP reference 在整个待报告窗口及短期隔离期内不复用，晚到报告由 message ID、provider ID、RP reference 和可用时的 `In-Reply-To` 关联，业务状态落盘后才从 worker 确认移除。明确的本地失败或任何分段产生副作用前的 SIP 拒绝才直接使用 `failed`。
5. 入站 SIP MESSAGE 先返回对应 SIP transaction 响应并解析 network→MS RP-DATA。完整消息由 `simplusd` 持久化后，才通过新的 SIP MESSAGE 发送 MS→network RP-ACK；ACK 操作带稳定 operation ID，可安全重放。普通 SMS-DELIVER 的成功 SMS-DELIVER-REPORT TPDU 使用 `TP-MTI=00` 与空 `TP-PI=00`，不虚构 PID/DCS/UD 可选字段；只有 USIM data download 等确实需要返回这些字段的流程才设置 TP-PI。正常路径携带 `In-Reply-To`；worker 也接受不支持该 header 的 IP-SM-GW 按仍被保留且唯一的 RP reference 返回 submit report，并记住会话能力。能力未知时，只有携带正确关联值的 delivery report 被明确以 SIP `488` 拒绝，才允许对同一 RP-ACK 做一次不带该 header 的有界兼容重试；其他拒绝、无响应和事务不匹配均保持失败。
6. 单段入站 SMS-DELIVER 先写入业务消息库再发送 RP-ACK。multipart 的每个分片先进入 `messages` 数据集内的持久 spool，再单独发送 RP-ACK；只有十分钟重组窗口内 envelope 一致、part 唯一且完整的组才解码并进入业务消息库，歧义组保持不可见且不确认当前分片。分片记录保留七天后清理，服务重启不依赖 worker 内存恢复已经确认的分片。
7. `simplusd` 使用现有短信历史和 Web/API，不新增第二套 VoWiFi 消息模型。Host VoWiFi Line 即使不具备蜂窝 `sms-control`，只要具备 `host-vowifi-auth`、配置为 Host VoWiFi 且运行态 online，也可使用专用 IMS transport。
8. 真实发送、接收和运营商互通仍属于外部通信 HIL，必须在仓库所有者明确授权后执行。fixture、编译和普通服务重启不能提升兼容性等级。

## 后果

- SMS 协议与 Gm socket 留在现有 per-Line worker，不新增通用 SIP daemon；
- `simplus-agent` 仍是唯一模组端点 owner，新增的短信中心读取仍受 RF Off、SIM identity fence 和 root-only socket 限制；
- 后台正常每两秒进行一次有界短信同步，同时消费出站 RP 最终报告和入站消息；离线 Line 被跳过，失败后按 15 秒起步指数退避并封顶五分钟；新消息通知只在数据库首次创建完整入站消息后发送；
- 受控 HIL 已完成真实单段与 multipart 入站、服务请求关联 RP-ACK、公开 Web/API 异步 `sent` 状态提升、普通号码单段与两段 UCS-2 自回环，以及自动重连后再次收发。该证据覆盖 Web/API、业务数据库、typed supervisor 与 IMS worker，不替代其他收件人互通或真实网络侧失败映射。
