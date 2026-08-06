# Simplus 当前开发交接

> 更新：2026-08-06
>
> 状态：V1 管理面、持久模组/线路层、Host VoWiFi 与 SMS over IMS 纵切已完成；生产 Docker Compose 已在 Debian 13 开发 VM 完成三镜像构建、隔离 smoke、真实模组发现、Mihomo/Host VoWiFi 和单段自号码短信回环 HIL，clean-VM 生命周期验收尚待完成。

本文只记录可以公开的代码状态、验证等级和下一步；现场与个人环境材料遵循 [`privacy-and-publication.md`](privacy-and-publication.md) 留在仓库外。产品范围以 [`product.md`](product.md) 为准，进程与安全不变量以 [`architecture.md`](architecture.md) 为准，任务状态以 [`plans/active/mvp.md`](plans/active/mvp.md) 为准。

## 已落地能力

### 管理与安装

- 单管理员 setup、登录、CSRF、会话撤销和修改密码；
- React、Ant Design Pro、Pro Components 与 Umi Max 后台；
- 概览、模组、线路、短信、语音、Mihomo、通知和系统设置页面；
- production Dockerfile 保持 `simplus-control`、`simplus-agent`、`simplus-netd` 三个镜像，Compose 以 `data-init / agent / netd / app / bootstrap` 编排全新实例；原生 Debian bundle 和三个 systemd 服务在 clean-VM 生命周期验收前保留为回退，二者不共享数据也不能同时运行；
- Host VoWiFi 所需的两个 strongSwan 插件由锁定 Debian 输入独立构建为 `simplus-strongswan-plugins` 包，并随对应源码、摘要和 manifest 发布；netd 镜像安装该包和 Debian runtime，不要求用户或普通开发者提供 strongSwan 源码树；
- 全新容器实例由 bootstrap 生成随机管理员密码；原生安装器保持相同语义，升级不覆盖凭据。

### 容器部署候选

- 首版只面向 Debian 13/amd64、Compose 2.24+ 和 rootful、无 userns-remap 的 Docker；
  日常 Go/Node/pnpm 开发与 CI 不进入开发容器；
- 宿主只持有 Docker、内核及开机加载的 `option`。Agent 容器精确映射 USB sysfs、
  `/dev` 与单个 `option1/new_id`，动态 ID 只来自 Adapter registry；root entrypoint
  完成注册后降到 UID 10002，Agent 没有网络；
- app 固定 UID 10001 且无 capability；netd 使用普通 Docker bridge 和现有受限网络
  capability，临时 netns/veth/nft/XFRM probe 失败即不健康，不使用 privileged 或
  host network；
- bind-mounted 数据固定为 `./data/core` 与 `./data/agent`。data-init 固定所有权并
  安装 Zashboard 和固定摘要的 Mihomo core，已有 core 不覆盖；bootstrap 首次创建
  `simplus_admin`，密码只写首次容器日志；tag release 同时附带 Mihomo GPL 源码；
- app/Agent/netd 的 typed health、Compose YAML 权限 contract、Shell 语法、workflow
  actionlint、目标 Go 测试和 Compose `config --quiet` 已通过；当前 Debian 13/amd64
  开发 VM 还完成了三个 production target 构建，以及使用隔离空 USB/sysfs 的全栈
  Compose smoke：netd 临时 network namespace/veth/nft/XFRM preflight、固定 UID 与
  capability、首次管理员创建、Web 登录、幂等 bootstrap 和保留数据重建均通过；
  正式切换后还完成真实模组发现、Mihomo 国家出口、ePDG、Digest AKA AUTS 重同步、
  Gm XFRM、IMS 注册和单段自号码短信回环 HIL，全程没有请求 RF 写入；这仍不能替代
  clean-VM 生命周期验收或其他收件人的短信互通证据。

### Simulator 业务纵切

- 短信 GSM7/UCS-2、长短信、persist-before-ACK、幂等发送、会话视图和联系人；
- 电话呼入/呼出/接听/拒绝/挂断/DTMF、紧急号码前置拒绝和通话历史；
- 浏览器数字音频 fixture；
- 可拔插 eUICC 的已安装 Profile 列表、切换确认和重启持久化；
- Host VoWiFi、出口与 fail-closed 行为的独立契约 fixture；旧 Simulator access-path 与 Mihomo egress-profile API/表已移除，避免与 Line egress 形成第二真相源。

这些 Simulator 能力不会在 hardware backend 失败时伪装成真实硬件成功。

### 真实硬件与 Host VoWiFi

- Agent 使用统一 adapter registry 识别 QDC507 与 ML307A，不为每个型号复制平台协议；
- 动态发现候选与管理员已添加模组已经分离；模组页主表只显示 `AT+CGMM` 实时型号、直接读取的 USB Serial 序列号、默认掩码且按需实时读取的 IMEI、在线状态、SIM 插入状态和射频开关，型号不可读时明确显示“读取失败”且不回退 Adapter 名称；“添加模组”对话框才扫描未添加候选，并以单选表格显示相对 USB 地址、VID:PID、同一 AT 型号结果和 USB Serial 的本机脱敏短标识；
- IMEI 不进入普通列表或数据库；只有管理员点击显示时才通过带 Agent/快照/设备代际约束的类型化操作读取，并在返回前核对持久指纹，响应禁止缓存，隐藏或刷新即从页面状态清除；
- `ManagedModem` 以 ML307A IMEI 的每实例 HMAC 指纹稳定绑定，USB Serial 指纹只作辅助，USB 拓扑名与设备节点只作本次扫描定位；旧端口绑定在原设备仍可确认时一次性提升；
- 线路页只展示管理员显式创建的持久 Line；添加时从已添加模组当前 SIM/Profile 候选建立 `ManagedModem + 身份 + 卡槽` 绑定，稳定业务 ID 不依赖 USB 位置或 Agent 临时 Line；
- Line 只保存稳定身份与名称，不再保存互斥接入方式。候选页会同时说明模组离线、未插 SIM、SIM/Profile 不可用、已添加和身份冲突；提交时重新扫描，过期候选不会被猜测绑定。添加本身不修改 RF、不启动 Mihomo/VoWiFi，也不执行通信动作；
- 线路主表使用 ProTable 展示名称、实时模组型号/USB Serial、脱敏 SIM/Profile、运行态手机号、状态、VoWiFi 与出口，不重复罗列能力标签；响应式配置抽屉分别保存名称、出口和 VoWiFi 意图。新 Line 出口固定为 `unconfigured`，只有显式 `direct` 或 `mihomo-country` 可进入激活准入；
- 短信、通话、Mihomo 出口和 Host VoWiFi 已全部改用稳定 Line 目录，并且不读取旧 access-mode。模组离线、SIM/Profile 更换、卡槽不符或身份冲突时原 Line fail closed，不自动改绑；
- 当前只新增 ML307A `ATProbeAdapter`、`EquipmentIdentityAdapter`、`SIMPresenceAdapter`、`SIMIdentityAdapter`、`SIMAuthAdapter` 与 `RFControlAdapter`；Linux AT transport 只处理 tty 生命周期和有界 I/O，型号 adapter 选择综合探测计划并拥有 SIM 插卡状态、身份、SIM/IMS/APDU 与 RF 命令。`SIMIdentityAdapter` 还会以 best-effort 方式读取活动 Profile 的 EF_SPN，并在 Agent 内从 IMSI 与 EF_AD 只推导 MCC-MNC；API 仅返回运营商名称和代码，不返回或持久化原始 IMSI，读取失败也不阻止添加 Line。设备身份、SIM 插入状态和 SIM 身份相互独立，不再依赖 RF/通话综合探测成功。模组页显示主卡槽“已插入 / 未插入 / 未知”，并对不可添加候选给出类型化原因，但不会自动创建 Line。没有为未来短信、电话或 eUICC 预建空接口；短信与电话继续属于 Line 业务；
- production Agent 暴露类型化只读状态、root-only SIM 鉴权和带读回确认的 ML307A 运行时 RF 开关，不接受任意 AT/QMI 命令或设备路径；
- ML307A 的 SIM/IMS 身份与 AKA challenge-response 只通过固定请求结构提供给受限消费者，身份和鉴权材料不持久化；IMS Home Domain 优先来自完整 ISIM，缺少 ISIM 时只依据 IMSI 与 EF_AD 明确的 MNC 长度动态派生，无法可靠判定时 fail closed；Host VoWiFi 的 SIP 层消费该动态身份，不检查或修改 RF；
- `simplus-netd` 独占 Mihomo、namespace、路由、nftables、strongSwan 和 XFRM 生命周期；
- Host VoWiFi 已完成真实 ePDG/IMS 注册、持续 keepalive、连续提前刷新、有界重连、停用清理和服务恢复验证；
- Web/API 返回阶段、在线状态、出口、注册时间、下次刷新、稳定错误码，以及 IMS 明确授权时从 `P-Associated-URI` 提取的 E.164 手机号；无法确认时返回空值，重连或停用时清空，不返回原始 IMPU、内部地址、进程、SPI、P-CSCF 或鉴权材料。
- Host VoWiFi worker 已实现条件 `+g.3gpp.smsip` 注册、binary SIP MESSAGE、RP-DATA/RP-ACK/RP-ERROR、REGISTER `P-Associated-URI` 身份选择、`In-Reply-To`/RP reference transaction 关联和类型化 `simplus-netd` 短信 API；现有短信历史页直接复用该 transport；
- multipart 入站使用 SQLite 分片 spool：每片落库后独立 RP-ACK，十分钟内唯一完整组才成为可见消息，并已用关闭/重开数据库的 fixture 验证恢复；短信页面在可见期间有界刷新，后台新落库消息不要求用户手动刷新浏览器；
- 出站请求收齐各段 SIP 最终响应即返回，不等待 RP 报告；SIP 已接受时带 provider ID 持久化为 `unconfirmed`，后台取得关联 RP-ACK 后才异步提升为 `sent`，报告缺失、响应未知和 multipart 部分拒绝均不自动重发。入站只有业务数据库持久化后才发送 RP-ACK；普通成功 SMS-DELIVER-REPORT 使用带空 TP-PI 的两字节 TPDU，不虚构 PID/DCS/UD 可选字段。受控 HIL 已完成真实单段与 multipart 入站，以及一条单段 GSM7 服务请求的关联出站 RP-ACK 和新 multipart 业务回复；失败同步使用有界指数退避。

### Mihomo

- core 元数据、下载、摘要校验、版本 probe 和原子安装；
- 订阅创建、编辑、更新、切换、删除及不可变 raw/generated/metadata 工件；
- 自动国家分组、共享 DoH、受限 TPROXY listener 和 fail-closed 规则；
- `simplusd` 无网络 capability，固定 Unix API 调用 `simplus-netd`；
- Zashboard 作为 Mihomo `external-ui` 托管，controller secret 私有保存。

## 已验证边界

- 模组原生蜂窝短信、电话、数字音频和 eUICC 交互目前只在 Simulator 验证；Host VoWiFi 的 SMS over IMS 证据单独列在下方；
- QDC507 的真实短信候选驱动仅有 fixture/transcript 证据，没有进入 production Agent；
- QDC507 尚未实现稳定设备身份读取，因此当前不能添加为 `ManagedModem`；其 RF 证据也尚未对应新的类型化 `RFControlAdapter`。两项都留给 QDC507 专项工作，不能复制 ML307A 命令或退回端口绑定；
- Host VoWiFi 在线只证明 ePDG/IMS 注册与刷新；SMS over IMS 已有真实单段与 multipart 入站、服务请求出站 RP-ACK、公开 Web/API `unconfirmed → sent`、普通号码单段与两段 UCS-2 自回环，以及自动重连后再次收发证据，但不能外推到其他收件人，通话和媒体也仍不可用；
- SMS over IMS 的其他收件人互通和真实网络侧失败映射尚未完成运营商 HIL；
- 当前没有验证显式 IMS de-registration、数日稳定性或 IKE/CHILD 小时级 rekey；
- 动态 IMS Home Domain 已在一张无可用 ISIM 应用的真实 SIM 上完成只读 HIL-0，确认会读取 EF_AD 后派生；两位/三位 MNC 组合另有 Fixture。真实 Host VoWiFi 业务 HIL 仍只覆盖首个已验证运营商的接入 profile，ePDG FQDN、IKE responder identity 和远端选择尚未通用化；
- Mihomo 配置中的协议字段、URL-Test 或普通 UDP 成功不能替代目标业务的真实 UDP/ePDG 探针；
- 项目只面向可信 LAN，不应直接暴露到公网。
- 新 strongSwan 插件包已经通过静态包内容与可复现输入验证；切换到 `dpkg` 管理后的 clean-VM 安装、升级、卸载和完整 bundle smoke 尚待补充，不能沿用旧裸 `.so` 安装证据代替。
- 容器的 network namespace、固定 UID/socket peer credential 和 capability 边界已有
  隔离 Fixture smoke，真实设备映射、Mihomo、Host VoWiFi 和单段自号码短信回环已有
  独立容器 HIL；仍没有 clean-VM 生命周期证据，也不能把自回环外推为其他收件人互通。

更细的能力等级见 [`compatibility.md`](compatibility.md)，运行故障按 [`troubleshooting.md`](troubleshooting.md) 处理。原始硬件日志、订阅节点、真实拓扑和逐次排错记录不属于公开仓库。

## 当前下一步

1. 在安装 Docker 的 clean Debian 13/amd64 VM 验证 Compose render、三个镜像、全新初始化、升级、停止、卸载、固定 UID/socket 和 netd namespace 隔离；
2. 基于稳定 Line 接入第一个真实模组原生蜂窝短信 transport，保持短信业务与具体型号驱动分离；
3. 在具备合适测试号码时补充 Host VoWiFi 短信其他收件人互通，并继续跟踪 Umi 传递依赖审计告警和发布材料许可证；
4. ML307A RF、电话、媒体、eUICC 写能力以及 QDC507 稳定身份/adapter HIL 继续分别授权和设计。

## 验证入口

根据改动风险选择最小有效检查：

```bash
make check-docs
make verify-generated
make test
make build
```

真实硬件操作不属于普通验证。HIL-0 只读探测可以按开发文档执行；SIM AKA、网络建链和任何外部通信都必须满足对应决策与明确授权。
