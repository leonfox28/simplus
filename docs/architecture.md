# Simplus 架构地图

## 1. 设计取向

架构先服务原生蜂窝短信和电话纵切，再按验证顺序加入 eUICC、Host VoWiFi 和窄化 Mihomo 出口。优先使用少量、直接、可观察的进程与类型，不为多租户、通用代理或企业发布体系预建平台。

## 2. 运行组件

```text
LAN Browser
    │ HTTP / optional external HTTPS
    ▼
Vite dev server or control container Vite-built React SPA
    │ /api
    ▼
control container: simplusd ───── ./data/core/SQLite
    ├── shared Unix socket ──► agent container (network none)
    │                              └── bounded host /sys + /dev ──► modem(s)
    └── shared Unix socket ──► netd container (Docker bridge)
                                   ├── Mihomo + Zashboard
                                   └── per-Line netns/strongSwan/IMS worker
```

production 以 Docker Compose 保留三个进程/镜像的权限分离；它不是 privileged 单体，
也不使用 host network。固定 UID 10001/10002 和共享 runtime volume 让现有 Unix
`SO_PEERCRED` 鉴权跨容器成立。持久数据只通过 `./data/core` 与 `./data/agent` 映射；
首次容器部署是新实例，不猜测迁移原生 `/var/lib`。宿主只持有 Docker、内核与开机
加载的 USB serial `option` 模块，边界见 [`0021`](decisions/0021-container-production-deployment.md)。
当前开发 VM 的真实容器 HIL 已覆盖 Mihomo、Host VoWiFi 和单段自号码短信回环；
clean-VM 生命周期验收完成前，原生 systemd production 路径仍作为过渡回退保留。
Web 运行时已经按
[`0022`](decisions/0022-vite-react-query-web-runtime.md) 原子迁移到
Vite/React Router/TanStack Query，并直接使用 Ant Design。旧 Umi/Pro 构建输入、运行时
与兼容壳已删除；前后端契约和 `web/dist` 仍作为同一个版本交付。

### `simplusd`（目标职责）

- 提供 Web API；开发时前端由 Vite dev server 提供，生产由 `simplusd` 承载 Vite 构建后的静态资源；
- production Web/API 固定监听 IPv4 wildcard `0.0.0.0:8080`，不依赖安装时或 DHCP 分配的具体地址；开发默认保持 loopback，公网隔离由部署网络和主机防火墙负责，见 [`0014`](decisions/0014-ipv4-wildcard-management-listener.md)；
- 管理单一管理员会话；
- 保存模组、短信、联系人和通话状态；
- 为每个模组维护串行 worker；
- 把硬件结果转换成面向用户的稳定状态。

当前代码已经有 API、setup/auth、inventory 和存储，并完成 Simulator 短信纵切：发送前持久化、按 Modem 串行、GSM7/UCS-2 长短信编码、fake Agent list/read/send/ACK、入站 persist-before-ACK、最终状态和 Web 历史。Host VoWiFi 已接入同一短信业务模型；普通蜂窝 SMS backend 与电话业务尚未实现。

### `simplus-agent`（目标职责）

- 在 Linux 主机上枚举受支持模组；
- 独占必要的 tty/QMI/MBIM/音频端点；
- 暴露固定类型的探测、短信、电话和 eUICC 动作；
- 不接受来自 Web 的任意命令字符串；
- production 容器启动时只由 root entrypoint 把 Adapter registry 中的白名单 USB ID
  写入精确映射的 `option1/new_id`，随后清空 capability 并以 UID 10002 运行；本地
  HIL 开发仍可使用无 capability 的 `simplus-agent-dev` systemd 用户。

当前 Agent 保留经过 Client/Unix socket 集成测试的 typed SMS fixture contract，但 production `simplus-agent` 不注入 SMS/Call/eUICC backend，也不注册旧的 `radio.ensure-off`。正式硬件入口只包含只读探测、独立 root-only SIM 鉴权，以及使用布尔目标状态、固定型号命令和读回确认的 ML307A 运行时 RF 控制；它仍不接受任意 AT/QMI、设备路径或命令字符串。

Agent 是映射宿主设备后的单一硬件边界，不为每种模组或每个 USB 设备启动一套不同协议的 Agent。模型差异由进程内 adapter 隔离：

```text
simplusd typed SMS/Call/eUICC API
              │
              ▼
       one simplus-agent
              │
       model adapter registry
          ├── QDC507 adapter
          └── ML307A adapter
```

`internal/modemadapter` 当前负责 USB identity、内部显示名称、固定端点角色、型号 AT 探测计划、SIM/设备身份、SIM AKA/IMS、RF 控制及证据化能力；`hardwareprobe.Scanner` 只通过 registry 选择模型和编排类型化能力，重叠的匹配规则 fail closed。QDC507 只解析已验证的 `primary-at` interface 2 和 QMI interface 4；ML307A 根据官方 V1.0.1 拨号手册与本机 HIL 固定解析 `2ecc:3012` 的 interface 2 为 primary AT。这里固定的是 USB interface 的功能角色，不是 `/dev/ttyUSB*` 或 `N-P` 物理端口；设备节点和 sysfs 拓扑都在每次扫描时重新解析。看到 tty、QMI 或 UAC 只构成硬件证据，不自动注册 SMS、Call 或数字音频业务能力。Adapter 的内部显示名称不能作为 Web 型号回退值；模组页与候选列表的型号只使用只读 `AT+CGMM` 结果，模组离线、命令失败、空值或非法响应统一显示“读取失败”。

动态扫描结果不是业务配置。`DiscoveredDevice` 只代表 Agent 当前观察；管理员通过模组页把候选添加为持久 `ManagedModem`，再从已添加模组创建 Line。添加对话框可以为人工辨认显示本次扫描的相对 USB 拓扑地址、VID:PID 和 USB Serial 每实例 HMAC 的短标识；这些字段不参与业务分支、不成为持久绑定，也不包含原始 IMEI、`/dev` 节点或 sysfs 绝对路径。USB 描述符的原始 Serial 被产品定义为可直接显示的模组元数据：Agent 对它做长度和控制字符限制，已添加模组在线时由普通列表直接展示，但它不落入 Managed Modem 记录、不参与绑定或业务控制；对应的每实例 HMAC 指纹仍只作辅助观察。普通 probe、inventory、模组列表和数据库只以 HMAC 处理 IMEI；`ManagedModem` 以随机业务 ID 对应稳定 IMEI 指纹。管理员在模组页点击显示按钮时，可以通过独立的鉴权 POST 实时读取当前在线模组的 IMEI：请求以 Managed Modem ID 进入，经过当前 Agent/快照/设备代际约束并再次核对持久 IMEI 指纹，响应固定为 `no-store`，原值不落库、不进入普通日志，隐藏或刷新后从页面状态清除。拔出或换端口只令当前定位变化，不删除管理记录；重复 IMEI、缺失 IMEI，以及相同型号都不能触发猜测绑定。版本 18 的端口绑定在原设备仍位于旧位置时一次性提升为 IMEI 指纹。分层与迁移边界见 [`0017`](decisions/0017-managed-modems-and-capability-adapters.md)。

Line 也不是扫描结果。管理员只能从已添加模组当前可确认的 SIM/Profile 候选创建持久 Line；随机 `line_...` ID、显示名称及 `ManagedModem + SIM/Profile 身份` 绑定进入 core 数据库。USB/sysfs 名称、设备节点、具体型号和临时 `agent-line-...` 只留在运行时解析边界。端口变化会重新解析到同一 Line；模组离线、SIM 更换、卡槽不符或身份冲突时 Line fail closed，不猜测改绑。射频、Host VoWiFi 意图、`direct`/Mihomo 国家出口及短信/电话 transport 都不是 Line 身份的一部分，各自通过独立配置与类型化能力工作。短信、电话、出口和 Host VoWiFi 只消费稳定 Line 目录，细节见 [`0018`](decisions/0018-persistent-lines-and-runtime-resolution.md) 与 [`0019`](decisions/0019-line-identity-and-communication-paths.md)。

能力适配采用按当前纵切新增的小接口，而不是要求所有模组实现一个巨型通用接口。当前只有 `ATProbeAdapter`、`EquipmentIdentityAdapter`、`SIMPresenceAdapter`、`SIMIdentityAdapter`、`SIMAuthAdapter` 与 `RFControlAdapter`：六者分别声明型号支持的只读综合探测、读取 IMEI、读取主卡槽插入状态、读取用于稳定 Line 绑定的 ICCID 脱敏身份及活动 Profile 的有界归属运营商元数据、提供统一 SIM/IMS 身份与 AKA challenge-response，以及把布尔 RF 目标映射为固定动作并要求新鲜读回。设备身份、SIM 插入状态和 SIM 身份分别探测，不依赖 RF、通话或完整综合探测成功。归属运营商优先来自活动 Profile 的 EF_SPN，并在 Agent 内从 IMSI 与 EF_AD 只推导 MCC-MNC；原始 IMSI 不进入公共 API、数据库或日志，运营商读取失败也不影响 Line 候选。SIM presence 只输出 `present / absent / unknown`；PIN/PUK 锁卡属于 present，查询失败属于 unknown，它不读取身份、不解锁 SIM、不修改 RF，也不自动创建 Line。`sim-auth` 与 `host-vowifi-auth` 是另外两个证据：前者描述模组鉴权能力，后者描述已经验证的完整业务路径。短信和电话仍是 Line 业务，后续只在接入真实 transport 时增加最小接口；现有 QDC507 `SMSAdapter` 只是未装配到 production 的候选 transport。端点路径与 AT/QMI 命令始终留在 Agent 内部。

`USBSerialBindingAdapter` 不是业务能力；它只让容器 entrypoint 在扫描前取得经过型号
审查的 option driver 动态 ID。Registry 会拒绝非法或跨型号重复 ID，Web、Compose
和上层业务都不能提供或修改这组值。

`SIMAuthAdapter` 返回的 IMS Home Domain 必须来自当前 SIM，而不是型号或运营商常量。ML307A 先使用完整、内部一致的 ISIM 身份；ISIM 不可用时，按 [3GPP TS 23.003](https://www.etsi.org/deliver/etsi_ts/123000_123099/123003/15.10.00_60/ts_123003v151000p.pdf) 的 IMSI 派生格式，并读取 [3GPP TS 31.102](https://www.etsi.org/deliver/etsi_ts/131100_131199/131102/19.04.00_60/ts_131102v190400p.pdf) EF_AD 给出的两位或三位 MNC 长度，构造 private/public identity 与 Home Domain。不得维护运营商映射表、根据 IMSI 前缀猜 MNC 长度，或在读取失败时退回某个已知运营商；缺失、为零或非法的 MNC 长度一律 fail closed。派生结果沿类型化边界进入 SIP REGISTER 与 challenge realm 校验。

AT 实现进一步分成三层：`internal/attransport` 只负责 Linux tty 的打开、独占锁、termios、有限长度读写、超时、终止响应识别和关闭，不包含任何型号命令或厂商解析；`internal/modemadapter/standardat` 提供只有型号 adapter 明确选择后才执行的标准 3GPP 只读计划与类型化解析；具体型号 adapter 拥有命令选择、厂商响应、身份查询、SIM/APDU/IMS 和 RF 动作。`hardwareprobe` 只打开一个有界 session 并把 `Query` 委托给对应能力，不能构造 AT 命令。需要 prompt/PDU 等不同交互语义的业务 driver 可以拥有专用 transport，但同样由 driver 决定命令，transport 只处理帧与 I/O。

#### 分层控制与型号解耦

每个控制层只能依赖下一级公开的类型化能力契约，不能依赖其具体型号或实现。依赖方向固定为：

```text
Web/API -> application/Line -> typed service port -> Agent capability -> model adapter -> device protocol
```

- 上层表达业务意图，例如“读取 SIM 插入状态”“启用射频”“发送短信”；下层返回统一的类型化状态、结果和错误。型号 adapter 才负责把这些意图映射为该型号的固定 AT/QMI/其他协议动作；
- `profile`、型号名、USB VID/PID、interface number、sysfs 或 `/dev` 路径只能在发现、registry、adapter 或运行时硬件边界参与实现选择；AT/QMI 命令和厂商响应格式只能由型号 adapter/业务 driver 持有，通用 transport 不选择命令。添加模组候选 API 可以只读展示相对 USB 地址、VID:PID 和脱敏序列标识，但应用层、Line、短信、电话、Host VoWiFi 与 Web 控制流程不得据此分支；
- 业务层只持有稳定业务 ID 和能力结果。把稳定 `ManagedModem`/Line 解析到本次扫描的物理设备、端点与 adapter，只能发生在运行时硬件边界；设备移动或实现替换不能迫使业务对象改绑；
- 同一能力在不同型号上的实现差异必须收敛到该能力的小接口。若现有契约不足，先依据当前真实纵切扩展或新增最小类型化能力，不能在上层增加 `if model == ...`、传递任意命令或建立巨型“万能模组”接口；
- 不支持、尚未验证和当前不可用必须作为能力证据或类型化状态显式返回，不能由上层猜测，也不能静默回退另一型号或另一 transport；
- 接入一个已经具备现有能力的新型号，原则上只应新增或注册型号 adapter、对应 transport 及其 fixture/HIL 证据，不应修改 Line、短信、电话、Host VoWiFi 或 Web 的业务控制流程。若必须修改上层，说明能力契约仍泄漏了具体实现，代码评审时必须先修正边界或记录明确例外。

这条约束不要求提前抽象尚不存在的能力。接口随经过验证的纵切逐步出现；抽象的是已经存在的共同业务语义，而不是厂商命令的表面相似性。

### QDC507 SMS 候选传输

第一个 QDC507 SMS driver 候选采用 interface 2 上的 3GPP PDU-mode AT，而不先实现 QMI WMS。依据是：该固件的 primary AT 端点已经完成 HIL-0 映射；[Quectel EC2x 系列手册](https://forums.quectel.com/uploads/short-url/cBnrTmjnCg7OGnqRsk8dIpbHuVX.pdf)依据 [3GPP TS 27.005](https://www.3gpp.org/ftp/specs/archive/27_series/27.005/) 明确提供 `CMGF/CMGL/CMGR/CMGS/CMGD` PDU 流程；当前开发机的 `qmicli 1.36` 没有 raw WMS list/read/send/delete CLI，而 [libqmi WMS API](https://www.freedesktop.org/software/libqmi/libqmi-glib/1.26.0/) 虽然具备 raw 操作，接入它需要新增 GLib/cgo 或自行实现 QMI client。QMI WMS 仍是 AT 真机验收失败时的备选，不视为不支持。

`internal/modemadapter/qdc507sms` 当前包含 transcript-shaped PDU driver、有界 tty transport，以及只供 fixture 装配的完整 `SMSAdapter`。它们不从 production Agent 装配，因此真实 Agent 不会声明 `sms-v1`。已经由纯单元测试覆盖：

- GSM7/UCS-2 SMS-SUBMIT PDU、8-bit UDH 长短信和逐段递增 TP-MR；
- `CMGF=0` 后固定的 list/read/send/delete 命令形状与 PDU 长度；
- SMS-DELIVER sender、SCTS、GSM7/UCS-2 和分段 UDH 解码；
- `+CMS ERROR`、非法 transcript、部分长短信成功，以及提交后响应丢失的 outcome-unknown 映射；
- 入站 message ID 对 device、按 part 排序的 storage index 与原始 PDU 做摘要，不含 unread/read 状态；只装配十分钟内完整且 part 唯一的长短信，8-bit reference 复用造成的歧义组保持不可见；
- 完整入站消息先进入窄化的 `StateStore`，ACK 每确认删除一段就保存进度；删除前重新读取并校验 PDU 摘要，删除响应丢失时只做一次 read-back，不会误删已复用索引中的新短信；
- send operation 在 dispatch 前进入 accepted，相同参数的成功结果可重放，不同参数复用 ID 会冲突；部分成功、响应未知或重启遗留 accepted 统一返回 `SMS_SEND_OUTCOME_UNKNOWN`，该 code 会穿过 Agent client/gateway 保存到应用消息状态，不自动重发；
- QDC507 专用 `SQLiteStateStore` 原子保存上述入站进度和 operation；测试实际关闭并重新打开数据库后继续部分 ACK、重放成功发送，并拒绝重新 dispatch 遗留 accepted；
- 120 秒是整个 multipart modem dispatch 的总预算，不是每一段各 120 秒；Agent 与 simplusd 的外层 HTTP write budget 为 130 秒，给结果持久化和返回留出余量；
- candidate PDU driver 本身不能使真实 Agent 声明 `sms-v1`，完整 candidate adapter 也只有显式加入非默认 registry 才能暴露该能力。

`MemoryStateStore` 只用于快速 fixture；跨进程恢复语义由独立 SQLite 文件证明。这个状态层只保存 QDC507 SMS 恢复必需字段，不扩展旧的通用命令账本。按 V1 read-only policy，它不会接入 production Agent。`CMGL/CMGR` 可能把 unread 状态改为 read，`CMGD` 会删除存储副本，`CMGS` 会产生运营商副作用，因此都不允许在真实硬件 V1 执行。

### Web

- 单一前端栈是 React 19、Vite、React Router Declarative Mode、直接使用的
  Ant Design 6 与 TanStack Query；Umi Max、Ant Design Pro Components、ProLayout
  和 Umi runtime 已原子移除，不保留第二套路由、UI 或服务端状态栈；
- `AppProviders` 显式装配 Ant Design 与 QueryClient，`BootstrapGate` 统一处理 setup、
  管理员 session 和 401 恢复，`AppRouter` 与响应式 `AppShell` 拥有公共/受保护路由、
  桌面 Sider 和手机 Drawer；Login/Setup 继续使用独立页面壳；
- `api/openapi.yaml` 由 `@hey-api/openapi-ts` 同时生成 Fetch SDK、TypeScript 类型、
  Zod 结构 schema 和 TanStack Query keys/options。生成目录是公共
  operation/payload/query identity 的唯一浏览器 owner；手写 runtime 只负责同源
  cookie、double-submit CSRF、取消/超时、稳定错误和 OpenAPI 无法表达的领域跨字段
  校验，页面不得直接调用 `fetch` 或复制 API model；
- 服务端和 SQLite 是业务状态唯一权威。浏览器通过 HTTP 读取快照和提交 mutation，
  TanStack Query 只保存可丢弃的页面快照；query 只有在网络或服务端明确标记可重试时
  有界重试，mutation 默认不自动重试，敏感或业务数据不写入浏览器持久存储；
- 同源 `GET /api/v1/events` 经过 setup、管理员 session 与可信 LAN gate，只发送有界
  资源失效、重连 resync 与新短信/来电 attention hint。事件不携带正文、号码、身份、
  路径、拓扑、命令或诊断材料，也不构成业务真相源；客户端只失效当前活跃 query，
  再经 HTTP 取得权威快照，慢连接和丢失 hint 不得阻塞 mutation、后台同步或造成
  无限队列；
- Messages 与 Calls 使用按 `(createdAt, stable ID)` 稳定倒序的 opaque keyset cursor。
  短信产品会话只以 exact remote address 标识并跨 Line 合并；全局、remote-only 以及兼容
  的 Line + remote 查询都由匹配索引支持，Line-only 继续拒绝。每条消息仍保存和展示
  实际 Line，游标不含通信内容或身份；
- 短信会话摘要由后端分页返回最后消息、持久未读数和最近出站 Line。浏览器只有在会话
  detail 实际可见、页面前台且 remote-only 最新页成功渲染后，才原样提交该 HTTP snapshot
  的 opaque read-through token；联系人只在 Web 以 exact 号码关联名称，不拥有会话身份；
- 登录、基础初始化以及导航中的模组、线路、短信、语音、Mihomo、通知和系统设置页面
  使用同一套直接 Ant Design 视觉语言；桌面以 Table/Sider 为主，手机以 Card/List 与
  Drawer 为主，宽表只在自身容器滚动，加载、空、错误、部分失败和不可用原因明确可见；
- 模组页只展示管理员已添加的模组，主表固定为型号、USB Serial 序列号、默认隐藏且按需实时读取的 IMEI、在线状态、SIM 插入状态与射频开关；“添加模组”对话框以单选表格展示未添加候选的相对 USB 地址、VID:PID、型号、脱敏序列标识、支持状态、类型化不可添加原因和能力；
- 线路页只展示管理员创建的持久 Line；“添加线路”以单选表格展示所有已添加模组的当前 SIM/Profile 候选及类型化不可添加原因，创建只保存不可改绑的身份和名称；主表直接显示 IMS 注册明确返回的 E.164 手机号，无法确认时显示未获取；配置抽屉分别维护名称、显式 `direct`/Mihomo 国家出口和 Host VoWiFi 激活意图，不出现 RF 控制；
- 只展示业务术语：Modem、SIM、Line、Message、Call；
- 不把 Agent 协议、AT 指令或内部 fencing 模型泄漏到 UI。

### `simplus-netd`、Mihomo 与 Host VoWiFi

- `simplus-netd` 同时实现 Mihomo 生命周期和固定的 per-Line Host VoWiFi `start/stop/status`；启动协议只接受稳定 Line ID、由 Line 层解析的当前不透明硬件目标、`direct`/`mihomo-country` 和国家码，不接受 shell、设备路径、网络命令或任意配置参数；
- production 只有 root `simplus-netd` 拥有创建 namespace、veth、策略路由、nftables 和 XFRM 所需权限；`simplusd` 与 Web 始终没有网络管理 capability；
- 容器 production 中，root `simplus-netd` 位于普通 Docker bridge；上述网络对象都
  创建在它自己的 network namespace，启动时由临时 netns/veth/nft/XFRM probe
  验证并清理，不能改用 host network 或 privileged 模式绕过失败；
- Mihomo supervisor 只接受已安装 core 和每订阅不可变生成配置的固定路径形状，并把 listener bind error 视为启动失败；
- 每条激活 Line 由一个长生命周期 worker 独占网络边界、strongSwan ePDG 会话、Gm XFRM 和 IMS 注册；国家出口通过已生成的固定 TPROXY listener fail closed，不回退 direct；Host VoWiFi 生命周期不读取或修改模组 RF 状态；REGISTER `P-Associated-URI` 中明确的 `tel:+...` 或 `sip:+...@...` 可以规范化为运行态手机号，其他 IMS 身份不得猜测转换，状态失效时号码随之清空；
- 同一 worker 还独占 SMS over IMS 的受保护 SIP socket、Service-Route、RP reference、异步出站提交事务与待确认入站消息；root-only worker socket 只提供固定的发送、入站 list/read/acknowledge、出站报告 list/acknowledge 操作，管理进程无法提交 SIP、RPDU、APDU、设备路径或网络参数；提交报告优先按 `In-Reply-To` 并始终按仍占用的 RP reference 关联原 SIP transaction；
- Line 应用层先把稳定业务 Line 唯一解析到当前硬件目标；worker 再用该不透明目标检查具备 `sim-auth` 的就绪 SIM、identity fence、无活动呼叫与出口，并维持 IKEv2 DPD/rekey、IMS keepalive、提前刷新和有界重连。它不检查或修改 RF，停用、进程退出及服务重启都清理其临时网络对象；
- core SQLite 只保存管理员的 `desired_active` 意图，实时 online 状态只来自 `simplus-netd`；`simplusd` 启动和每十秒协调二者；
- 权限与协议决策见 [`0008`](decisions/0008-mihomo-tproxy-privilege-separation.md)、[`0012`](decisions/0012-web-managed-vowifi-runtime.md) 和 [`0016`](decisions/0016-vowifi-sms-over-ims.md)。
- netd 镜像携带固定版本和压缩/展开双摘要的官方 Mihomo amd64 core；data-init 只在
  全新且没有版本歧义的数据目录中安装默认 core，已有 manifest 或升级版本不被覆盖。
  对应 GPL-3.0 源码在 tag workflow 中独立校验并作为 Release 附件发布；
- Zashboard `v3.6.0` 是随 netd 镜像发布、由 data-init 安装到 Mihomo working directory 的固定摘要、MIT 许可静态产物，没有独立进程或服务，由运行中的 core 通过 `external-ui` 直接托管；controller 的监听范围跟随管理后台，production 为 `0.0.0.0:19090`，并使用实例独立强密码，见 [`0010`](decisions/0010-zashboard-external-ui.md) 与 [`0015`](decisions/0015-zashboard-wildcard-controller.md)。
- 订阅节点的内部稳定 ID 只用于订阅节点持久化和 API 展示；Line 不绑定节点 ID，只保存显式直连或国家选择。生成 Mihomo 配置时必须保留上游 `name`，重名节点应拒绝转换而不是暗中改名。
- 订阅本身使用随机 128-bit `subscription_...` ID 作为稳定唯一身份；显示名称只供用户识别，可重复且可编辑，新建时默认为由内部随机 ID 派生的 6 位易读标识。
- 当前订阅按实际国家预生成固定 localhost TPROXY listener。新 Line 的出口固定从 `unconfigured` 开始，只有管理员显式选择后才保存 `direct` 或一个国家 listener；该绑定不进入订阅 YAML，因此增删改 Line 不触发 Mihomo 配置重写或重启。`unconfigured` 只会出现在读取响应中，不能作为写入模式，也不能激活 Host VoWiFi。

#### strongSwan 插件构建边界

Host VoWiFi 运行时继续依赖 netd 的 Debian 13 镜像提供的 `charon-systemd`、`libcharon`、
`libsimaka` 与 `eap-aka`。Simplus 自有的 SIM AKA bridge 是同仓库内独立的
GPL-2.0-or-later 组件；它与 strongSwan 上游 `p-cscf` 插件只在发布流水线中
编译成 `simplus-strongswan-plugins` Debian 包。普通 Go/Web 开发、bundle 安装
和服务运行都不接收或查找 strongSwan source/build tree，也不手工复制裸 `.so`。

插件构建必须从发行版/架构专用锁取得精确 Debian source 和运行 ABI 输入，校验
SHA-256 后在临时 source tree 与 sysroot 中以普通用户完成。二进制记录精确 source
revision，运行依赖限制在已经评审的上游 ABI 系列；当前只支持 Debian 13/amd64。
发布同时生成包含全部锁定输入的对应源码归档、摘要和 manifest，安装/卸载只通过
`dpkg` 管理。该构建隔离不改变运行期信任边界：C 插件仍只桥接固定 SIM AKA
socket，`simplus-netd` 仍通过 VICI 与固定 worker 生命周期控制 charon。完整决策见
[`0020`](decisions/0020-strongswan-plugin-package.md)。

### 分阶段启用的组件

- V1 的媒体交互先由 Simulator 验证，不为真实硬件启动 Asterisk 或其他媒体进程；
- 当前真实 Host IMS/ePDG 已开放 SMS over IMS，并有真实单段与 multipart 入站持久化、RP-ACK 和完整重组证据；受控单段服务请求已在 SIP 接受后取得关联出站 RP-ACK 及 multipart 业务回复，公开 Web/API 已证明业务库 `unconfirmed → sent` 的异步状态提升，普通号码单段与两段 UCS-2 长短信自回环及自动重连后再次收发也已完成。其他收件人互通仍未验收，电话或媒体仍未开放；
- Mihomo 只作为 Host VoWiFi Line 的可选出口，不成为宿主通用代理。

目标 Host VoWiFi 数据流是：

```text
SIM/eUICC auth <-> Host ePDG/IKE + IMS AKA/Gm
Host VoWiFi packets -> per-Line network boundary -> direct or Mihomo -> ePDG/P-CSCF
```

ML307A 与受控测试 Profile 的首个真实纵切已按 [`0011`](decisions/0011-ml307a-host-vowifi-hil.md) 通过：第一次 IKE_AUTH 用 `IDr=ims` 选择 IMS APN，ePDG EAP-AKA 建立外层 CHILD SA；初始 SIP REGISTER 取得 IMS AKA challenge 后，Host 以 SIM 返回的 CK/IK 创建两对 Gm transport-mode ESP SA，并把 Gm 与 ePDG template 组成双层 XFRM bundle。该路径随后按 [`0012`](decisions/0012-web-managed-vowifi-runtime.md) 迁入 `simplus-netd` 长生命周期和 Web 线路页，并保持 fail closed、脱敏状态和全量异常清理。产品运行时已按 [`0017`](decisions/0017-managed-modems-and-capability-adapters.md) 与 [`0019`](decisions/0019-line-identity-and-communication-paths.md) 与 RF 解耦；公开证据仍只覆盖 RF Off，证据等级和未验证边界见 [`compatibility.md`](compatibility.md)。

动态 IMS Home Domain 只消除了 SIM 身份与 SIP 层的运营商硬编码，不等于完整的多运营商 VoWiFi 支持。当前 ePDG FQDN、IKE responder identity 和已验证远端集合仍属于首个已验证运营商的专用接入 profile；在它们也改为由 SIM/标准发现与独立 HIL 驱动前，其他运营商必须保持未验证和 fail closed。

## 3. 简化后的领域模型

```text
Modem  物理模组及其可操作能力
SIM    当前插入或激活的卡
Profile  可拔插 eUICC 中的已安装 Profile
Line   管理员绑定到一个 ManagedModem 与 SIM/Profile、用于短信/电话/Host VoWiFi 的稳定逻辑入口
Message
Call
Contact
```

现有代码已经包含 `PhysicalDevice / ModemFunction / SIMSlot / SIMMedia / SubscriptionProfile / ResourceGroup / Line` 完整图。短期不为了“变简单”先重写这套代码，但新业务和 UI 不继续扩散这些抽象；适配层把它折叠为上面的业务视图。等短信和电话纵切稳定后，再依据实际维护成本决定是否收缩底层模型。

## 4. 命令模型

每个 Modem 只有一个串行 worker：

```text
request -> validate -> enqueue -> execute -> observe -> persist result -> notify UI
```

- 短信和电话动作使用明确 request/result 类型；
- 同一个用户动作带 operation ID，避免浏览器重试造成明显重复发送或重复拨号；
- 进程重启后只恢复有持久业务意义的任务；
- 超时或响应丢失时先读取模组状态，不盲目重发有费用或外部副作用的动作；
- 不再为所有动作构建通用 ResourceGroup lease、跨层 generation/fencing 和多套 durable outcome 真相源。

其中 `notify UI` 只表示发布有界资源失效/attention hint；持久结果必须先由业务服务
写入权威状态，浏览器再通过 HTTP 重新读取。SSE 不能承载 mutation 结果或建立第二套
事件状态。

现有 `radio.ensure-off` ledger 代码与 ResourceGroup lease 可以保留为 fixture/历史基础设施，但 production Agent 不注册该命令。新的 RF 路径只接受目标开关状态，在 Agent 内串行执行固定命令并立即读回；新的 SMS/Call 纵切也不应扩展通用分布式命令平台。

## 5. 关键数据流

### 发送短信

```text
Web -> simplusd validation -> persist queued message
    -> typed sender -> Agent cellular SMS or simplus-netd per-Line IMS worker
    -> persist sent/unconfirmed/failed -> bounded messages invalidation
    -> active Web query refetches authoritative HTTP snapshot
```

Simulator 继续使用进程内 Local Agent client。Host VoWiFi Line 使用独立 typed gateway，把 GSM7/UCS-2 产生的 SMS-SUBMIT TPDU 封装为 RP-DATA 和 binary SIP MESSAGE。worker 收齐各分段 SIP 最终响应即返回，绝不为 RP 报告占住业务请求；SIP `202` 只把带 provider ID 的消息持久化为橙色 `unconfirmed`。后台随后消费异步 RP 报告：全部匹配的 RP-ACK 才提升为 `sent`，单段 RP-ERROR 可成为 `failed`，multipart 的部分拒绝仍是 `unconfirmed`；响应丢失或报告超时也保持 `unconfirmed` 且不自动重发。同一个 multipart 操作始终逐段各提交一次，不会因为第一段缺少报告而截断，也不会重新提交已经处理过的分段。普通 hardware cellular SMS sender 仍未接入 production Agent。

### 接收短信

```text
modem/IMS worker -> bounded typed read -> simplusd
    -> transactionally persist raw/decoded message + unread marker
    -> confirm persistence
    -> delete/ack modem copy when applicable
    -> bounded messages invalidation/attention
    -> active Web summary/history queries refetch authoritative HTTP snapshots
    -> visible detail submits its snapshot read-through token
```

Simulator 提供固定 welcome 入站消息；Host VoWiFi worker 则解析 SIP MESSAGE、network→MS RP-DATA 和 SMS-DELIVER，包括数字号码与 GSM7 字母型 TP-Originating-Address。后台执行有界同步：单段消息先落库再发送新的 SIP MESSAGE/RP-ACK；multipart 每片先进入持久 spool 再独立确认，完整且唯一的组才解码为一条可见消息。控制面重启后可继续组装，ACK 失败会保留已落库记录并重试，引用号复用导致的歧义组 fail closed。

### 电话

```text
Web -> emergency/number validation -> modem worker
    -> Agent call action -> observed call state -> bounded calls invalidation
    -> active Web query refetches authoritative HTTP snapshot
```

音频路径必须由硬件报告证明，不能从 USB descriptor 或 AT capability 推断。

## 6. 存储

当前代码使用五个 SQLite 数据库和独立录音目录。这不是 MVP 必须维持的领域边界，但立即合库会延迟核心功能，因此：

- 当前 migration 和数据库继续工作；
- messages v7 使用 `(remote_address, created_at_unix_ms DESC, message_id DESC)` 支持跨 Line
  历史和摘要；`sms_message_unread` 以 `AUTOINCREMENT` 到达序号记录新 schema 生效后的
  首次入站。计数从 marker 派生，message 删除级联 marker，旧历史升级时 ledger 为空；
- core 数据库保存 Host VoWiFi `desired_active`，但不保存网络运行事实或鉴权材料；
- 新表放到语义最接近的现有库；
- 不新增 dataset identity、备份协议或跨库事务框架；
- MVP 后再评估是否合并为一个 SQLite 数据库；
- 目录与数据库保持普通 `0700/0600` 权限即可，不继续扩展 inode/mount 身份策略。

## 7. 必须保持的机械不变量

1. 同一个硬件端点只有一个 owner；
2. 每个 Modem 的写命令严格串行；
3. Web/API 不能提交任意 AT/QMI/设备路径；
4. 入站短信先成功持久化，再删除或 ACK 模组副本；
5. 已知紧急号码和无法可靠判断的短号码在硬件副作用前拒绝；
6. 硬件能力必须来自真实 HIL 证据；
7. hardware backend 失败时不能静默回退 Simulator。
8. eUICC Profile 切换后必须重新读取并确认活动 Profile；
9. `mihomo-required` Line 在 Mihomo 不可用时不能回退 direct。
10. Host VoWiFi 只有 ePDG、Gm 和受保护 IMS REGISTER 同时成立才能报告 online；其生命周期不得隐式控制 RF；
11. `simplus-netd` 是 Host VoWiFi 临时网络对象的唯一 owner，Web/API 不得传入底层网络参数。
12. SMS over IMS 不能把 SIP 2xx 当作短信提交成功，且入站 RP-ACK 只能发生在可恢复持久化之后。
13. 每个控制层只能依赖下一级的类型化能力契约；新增实现同一能力的模组型号不得要求上层业务按型号分支。
14. 浏览器 mutation 和权威快照只走鉴权 HTTP；SSE 只能发送有界失效/attention hint，慢订阅者不得阻塞业务写入。
15. 公共浏览器 operation、payload 结构和 query identity 由 OpenAPI 生成边界拥有；页面不得直接 fetch 或另建 payload 契约。
16. Messages/Calls 分页必须使用与 SQLite 索引一致的 `(createdAt, stable ID)` keyset 顺序，不能退化为 offset。
17. 短信会话只按 exact remote address 跨 Line 合并；Line 仍是每条消息的事实与发送时的
    显式身份，最近 Line 不可用时不得静默切换。
18. 未读只能由首次入站持久化在 messages 数据集内原子创建 marker；已读只能使用成功
    显示的 HTTP snapshot opaque 水位清除不晚于它的 marker，SSE 不承载未读真相。

这些规则应优先由类型、测试和小型检查器强制执行，而不是在多份文档中重复描述。

## 8. 当前技术债处理原则

- 已完成但超出新 MVP 的 setup/auth/topology/lease 代码先保持可用；
- 不为删代码而中断短信纵切；
- 当旧抽象实际阻碍一个纵切时，用小型执行计划删除或折叠；
- 每次只保留一个业务真相源，避免在 daemon、Agent 和 Web 分别维护同一状态；
- 新设计优先选普通 Go、SQLite、Unix socket 和明确 JSON schema。
