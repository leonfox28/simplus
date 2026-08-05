# Simplus 产品说明

## 1. 一句话目标

Simplus 是运行在 Linux 主机上的局域网 Web 工具，用来控制一台或多台 4G/5G 蜂窝模组收发短信、接打电话，并保存基本历史记录。

它不是电信平台、云服务、蜂窝网关发行版或企业安全产品。

## 2. 使用环境

- 一台用户自己管理的 Linux 主机；
- 一个可信局域网；
- 一个管理员；
- 通过 USB 或 UART 连接的少量 4G/5G 模组；
- 浏览器从同一局域网访问；

全新 Debian 实例由安装器创建唯一管理员 `simplus_admin` 和每实例随机强密码；管理员首次登录只完成必要的本机初始化。HTTPS、模组和线路配置在后台管理页内分别维护，不再依赖 root Bootstrap URL。
- 不直接暴露到公网，不考虑互不信任的多租户。

如果实际网络不可信，部署者应使用现成的 VPN 或反向代理提供 HTTPS；Simplus MVP 不自建 CA、证书轮换或公网访问体系。浏览器通话音频需要 secure context 时，也使用外部 HTTPS 终止。

## 3. MVP 功能

### 3.1 模组与线路

- 发现支持的模组并显示型号、SIM 状态、注册状态和信号；
- 为每个可用模组展示一个易懂的 `Modem / SIM / Line` 视图；
- 支持少量模组并行，但同一模组上的命令串行执行；
- 模组断开、重连或命令失败时给出明确状态。

### 3.2 短信

- 从指定线路发送普通短信；
- 接收入站短信并持久化；
- 展示会话、时间、状态和失败原因；
- 支持常见 GSM 7-bit/UCS-2 与长短信；
- 只有数据库成功保存后，才允许删除模组中的对应入站短信。

### 3.3 电话

- 从指定线路拨号；
- 展示来电并支持接听、拒接、挂断和 DTMF；
- 同一模组遵守实际并发能力；
- 不支持紧急呼叫；
- 只有硬件完成真实双向数字音频验证后，才启用该模组的浏览器通话音频。

### 3.4 基本管理

- 一个管理员账号；
- 基本联系人、短信历史和通话历史；
- 手工清理历史记录；
- 简单日志和健康状态；
- 中文界面优先，已有英文文案可以保留，但英文完整度不是 MVP 发布门槛。

### 3.5 eUICC 管理

- 识别可拔插 eUICC；
- 读取 EID 和已安装 Profile 列表；
- 显示当前活动 Profile；
- 在同一 Modem 串行 worker 中切换已安装 Profile，并在切换后重新读取确认；
- 将 Line 配置与具体 Profile 关联。

首阶段至少交付“枚举已安装 Profile + 切换”。SM-DP+ 下载、删除和完整消费级 eSIM RSP 尚未定义，不能在没有单独决策时顺手扩展。

### 3.6 Host VoWiFi 与 Mihomo

- 使用实体 SIM 或 eUICC Profile 的运营商鉴权能力建立 Host VoWiFi；
- 至少完成一家真实运营商的 ePDG/IMS、短信和电话验证；
- 每条 Host VoWiFi Line 可选择 `direct` 或 `mihomo-required`；
- Mihomo 只承载 Simplus Host VoWiFi 所需流量，不成为通用系统代理或蜂窝数据网关；
- 选择 `mihomo-required` 时，Mihomo 不可用必须明确离线，不得静默直连。

这两项是完整产品必需能力，但排在原生蜂窝短信、电话和 eUICC 基础之后，不能阻塞第一条可用 SMS 纵切。

### 3.7 Mihomo 管理与通知渠道

- 管理 MetaCubeX 官方 stable Mihomo core 的下载、校验、版本和启停状态；
- 管理用户提供的订阅、刷新结果、节点摘要及 Host VoWiFi 专用出口 Profile；
- Mihomo 不开放 LAN 代理，不接管宿主默认代理；
- 配置企业微信与飞书出站通知、测试投递及错误状态；
- 通知渠道不接受远程控制命令，credential 不回显。

## 4. 明确不属于当前产品范围

- 通用蜂窝数据上网、热点、NAT 和系统级代理；
- 完整 eSIM RSP、未定义的 SM-DP+ 下载/删除；
- 复杂 PIN/PUK 凭据保管；
- 运营商扫描和手动 PLMN 策略；
- Telegram、通用 Webhook 和第三方远程控制 token；
- 多租户、角色权限、企业审计和合规证明；
- 内建 CA、证书自动续期、候选证书提交/回滚；
- 加密诊断包、hash-chain/WORM 风格审计；
- Web 内升级、签名 metadata、emergency checkpoint 和自动回滚；
- Backup、RecoverySet、WebDAV；
- 多浏览器 PhoneConsoleLease 与复杂接管时序；
- 录音、72 小时 soak、SBOM/provenance 和多发行版认证作为首个可用版本的前置门槛。

这些能力只有在核心短信/电话已稳定、出现真实用户需求，并形成新的决策记录后才重新评估。

## 5. 保留的基本安全与可靠性

局域网部署不等于完全取消边界。MVP 保留：

- 管理员登录，防止局域网中误操作；
- production Web/API 与带实例密码的 Mihomo controller 监听所有 IPv4 接口，由部署者使用路由与主机防火墙确保它们只在可信 LAN 可达；开发模式默认只监听回环地址；
- `simplus-agent` 以非 root 服务运行，Unix socket 限制调用者；
- Web/API 只能调用固定类型的模组动作，不能传任意 AT/QMI 命令或设备路径；
- 同一模组上的命令串行，超时后读取真实状态再决定结果；
- 发送短信、拨号和可能改变 RF/模组持久状态的 HIL 操作需要明确测试授权；
- 外呼在接触硬件前拒绝已知紧急号码和无法可靠判断的短号码；
- 密码、cookie、数据库和运行目录使用常规安全默认值。

不再把每个动作都建模成通用分布式事务、跨进程 fencing、hash chain 或企业审计事件。现有实现中的相关代码可以暂时保留，但新功能只使用解决当前故障模型所需的最小机制。

## 6. MVP 成功标准

在一台目标 Linux 开发机上，业务能力先由 Simulator 完整验收；真实模组默认只做只读连接验证，Host VoWiFi 再按独立决策逐步做受控 HIL：

1. 从全新数据目录启动并登录；
2. 只读识别真实模组、SIM、当前 RF/注册/信号状态，断开/重连后状态恢复；
3. Simulator 连续发送和接收短信，重启后不丢失已保存消息；
4. Simulator 完成呼入、呼出、接听、挂断和 DTMF；
5. Simulator 完成数字音频状态和浏览器媒体路径；真实硬件明确标注为未验证/不可操作；
6. 浏览器能看懂错误，不需要阅读日志才能完成日常操作；
7. Simulator 可以读取可拔插 eUICC 的已安装 Profile 并完成 A→B→A；hardware backend 不提供切换；
8. Simulator 完成 Host VoWiFi direct 的短信和电话状态机；
9. Simulator 通过 `mihomo-required` 完成受控出口与无 direct fallback 验证；
10. ML307A 受控纵切在 RF Off 下完成脱敏 SIM 身份、AKA、ePDG 和 IMS 注册验证，任何失败均不回退蜂窝或直连；
11. SMS over IMS 先通过 fixture 验证 RPDU、SIP transaction、异步提交报告和 persist-before-RP-ACK，再在单独授权下完成真实单段与长短信收发；
12. 安装和运行步骤可以由仓库文档在目标机器上复现。

真实硬件默认禁止开启/关闭射频、建立蜂窝数据流量，以及短信、电话、eUICC 切换等任何会改变模组、SIM、网络或外部通信状态的操作。ML307A Host VoWiFi 注册仅按 [`0011`](decisions/0011-ml307a-host-vowifi-hil.md) 的明确授权例外执行；真实 SMS over IMS 还必须遵循 [`0016`](decisions/0016-vowifi-sms-over-ims.md) 的单独授权，其余边界仍见 [`0003`](decisions/0003-v1-read-only-hardware.md)。
