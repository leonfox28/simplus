# Simplus 架构地图

## 1. 设计取向

架构先服务原生蜂窝短信和电话纵切，再按验证顺序加入 eUICC、Host VoWiFi 和窄化 Mihomo 出口。优先使用少量、直接、可观察的进程与类型，不为多租户、通用代理或企业发布体系预建平台。

## 2. 运行组件

```text
LAN Browser
    │ HTTP / optional external HTTPS
    ▼
Umi Max dev server or embedded static Web assets
    │ /api
    ▼
simplusd ───── SQLite
    ├── typed Unix-socket API ──► simplus-agent ──► 4G/5G modem(s)
    └── fixed lifecycle API ────► simplus-netd
                                      ├── Mihomo
                                      └── per-Line netns/strongSwan/IMS worker
```

### `simplusd`（目标职责）

- 提供 Web API；开发时前端由 Umi Max dev server 提供，生产由 `simplusd` 承载构建后的静态资源；
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
- 以非 root systemd 用户运行，只有必要设备组权限。

当前 Agent 保留经过 Client/Unix socket 集成测试的 typed SMS fixture contract，但 production `simplus-agent` 使用不可配置的 read-only handler，不注入 SMS/Call/eUICC backend，不注册 `radio.ensure-off`，并声明 `hardware-read-only-policy-v1`。候选写驱动只用于 Simulator/fixture，不会随配置开关进入真实硬件运行态。

Agent 是宿主机上的单一硬件边界，不为每种模组或每个 USB 设备启动一套不同协议的 Agent。模型差异由进程内 adapter 隔离：

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

`internal/modemadapter` 当前负责 USB identity、显示名称、固定端点角色与证据化能力；`hardwareprobe.Scanner` 只通过 registry 选择模型。QDC507 只解析已验证的 `primary-at` interface 2 和 QMI interface 4；ML307A 根据官方 V1.0.1 拨号手册与本机 HIL 固定解析 `2ecc:3012` 的 interface 2 为 primary AT。看到 tty、QMI 或 UAC 只构成硬件证据，不自动注册 SMS、Call 或数字音频业务能力。

业务驱动继续采用小型能力接口，而不是要求所有模组实现一个巨型通用接口。当前 `SMSAdapter` 由统一 router 按 snapshot 中的 device/profile 分发，并在 Agent 内再次按 device 串行；只有 registry 中至少一个型号实现该接口时，`simplus-agent` 才注入 SMS backend 并声明 `sms-v1`。默认 QDC507/ML307A adapter 都还没有实现它，因此真实部署行为保持只读。不支持的能力不注册或明确返回 unsupported，端点路径和 AT/QMI 命令始终留在 Agent 内部。

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

- React 19 + Ant Design Pro 单页应用，使用 Umi Max 路由和 ProLayout；
- 登录、基础初始化以及左侧导航的模组、线路、短信、语音、Mihomo、通知和系统设置页面均使用同一套管理后台组件；
- 线路页组合维护接入方式、`direct`/Mihomo 国家出口和 Host VoWiFi 激活意图，并轮询脱敏运行状态；
- 只展示业务术语：Modem、SIM、Line、Message、Call；
- 不把 Agent 协议、AT 指令或内部 fencing 模型泄漏到 UI。

### `simplus-netd`、Mihomo 与 Host VoWiFi

- `simplus-netd` 同时实现 Mihomo 生命周期和固定的 per-Line Host VoWiFi `start/stop/status`；协议只接受 Line ID、`direct`/`mihomo-country` 和国家码，不接受 shell、设备路径、网络命令或任意配置参数；
- production 只有 root `simplus-netd` 拥有创建 namespace、veth、策略路由、nftables 和 XFRM 所需权限；`simplusd` 与 Web 始终没有网络管理 capability；
- Mihomo supervisor 只接受已安装 core 和每订阅不可变生成配置的固定路径形状，并把 listener bind error 视为启动失败；
- 每条激活 Line 由一个长生命周期 worker 独占网络边界、strongSwan ePDG 会话、Gm XFRM 和 IMS 注册；国家出口通过已生成的固定 TPROXY listener fail closed，不回退 direct；
- 同一 worker 还独占 SMS over IMS 的受保护 SIP socket、Service-Route、RP reference、异步出站提交事务与待确认入站消息；root-only worker socket 只提供固定的发送、入站 list/read/acknowledge、出站报告 list/acknowledge 操作，管理进程无法提交 SIP、RPDU、APDU、设备路径或网络参数；提交报告优先按 `In-Reply-To` 并始终按仍占用的 RP reference 关联原 SIP transaction；
- worker 重新检查固定 ML307A、SIM identity fence、SIM READY、RF Off 与出口，维持 IKEv2 DPD/rekey、IMS keepalive、提前刷新和有界重连；停用、进程退出及服务重启都清理其临时网络对象；
- core SQLite 只保存管理员的 `desired_active` 意图，实时 online 状态只来自 `simplus-netd`；`simplusd` 启动和每十秒协调二者；
- 权限与协议决策见 [`0008`](decisions/0008-mihomo-tproxy-privilege-separation.md)、[`0012`](decisions/0012-web-managed-vowifi-runtime.md) 和 [`0016`](decisions/0016-vowifi-sms-over-ims.md)。
- Zashboard `v3.6.0` 是安装到 Mihomo working directory 的固定摘要、MIT 许可静态产物，没有独立进程或 systemd unit，由运行中的 core 通过 `external-ui` 直接托管；controller 的监听范围跟随管理后台，production 为 `0.0.0.0:19090`，并使用实例独立强密码，见 [`0010`](decisions/0010-zashboard-external-ui.md) 与 [`0015`](decisions/0015-zashboard-wildcard-controller.md)。
- 订阅节点的内部稳定 ID 仅用于持久化和 Line Binding；生成 Mihomo 配置时必须保留上游 `name`，重名节点应拒绝转换而不是暗中改名。
- 订阅本身使用随机 128-bit `subscription_...` ID 作为稳定唯一身份；显示名称只供用户识别，可重复且可编辑，新建时默认为由内部随机 ID 派生的 6 位易读标识。
- 当前订阅按实际国家预生成固定 localhost TPROXY listener。真实 Line 只绑定 `direct` 或一个国家 listener；该绑定不进入订阅 YAML，因此增删改 Line 不触发 Mihomo 配置重写或重启。

### 分阶段启用的组件

- V1 的媒体交互先由 Simulator 验证，不为真实硬件启动 Asterisk 或其他媒体进程；
- 当前真实 Host IMS/ePDG 已开放 SMS over IMS，并有真实单段与 multipart 入站持久化、RP-ACK 和完整重组证据；受控单段服务请求已在 SIP 接受后取得关联出站 RP-ACK 及 multipart 业务回复，公开 Web/API 已证明业务库 `unconfirmed → sent` 的异步状态提升，普通号码单段与两段 UCS-2 长短信自回环及自动重连后再次收发也已完成。其他收件人互通仍未验收，电话或媒体仍未开放；
- Mihomo 只作为 Host VoWiFi Line 的可选出口，不成为宿主通用代理。

目标 Host VoWiFi 数据流是：

```text
SIM/eUICC auth <-> Host ePDG/IKE + IMS AKA/Gm
Host VoWiFi packets -> per-Line network boundary -> direct or Mihomo -> ePDG/P-CSCF
```

ML307A 与受控测试 Profile 的首个真实纵切已按 [`0011`](decisions/0011-ml307a-host-vowifi-hil.md) 通过：第一次 IKE_AUTH 用 `IDr=ims` 选择 IMS APN，ePDG EAP-AKA 建立外层 CHILD SA；初始 SIP REGISTER 取得 IMS AKA challenge 后，Host 以 SIM 返回的 CK/IK 创建两对 Gm transport-mode ESP SA，并把 Gm 与 ePDG template 组成双层 XFRM bundle。该路径随后按 [`0012`](decisions/0012-web-managed-vowifi-runtime.md) 迁入 `simplus-netd` 长生命周期和 Web 线路页，仍固定保持 RF Off、fail closed、脱敏状态和全量异常清理。公开证据等级和未验证边界见 [`compatibility.md`](compatibility.md)。

## 3. 简化后的领域模型

```text
Modem  物理模组及其可操作能力
SIM    当前插入或激活的卡
Profile  可拔插 eUICC 中的已安装 Profile
Line   用户用于选择收发短信/电话的逻辑入口
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

现有 `radio.ensure-off` ledger 代码与 ResourceGroup lease 可以保留为 fixture/历史基础设施，但 production Agent 不注册该命令；新的 SMS/Call 纵切不应扩展通用分布式命令平台。

## 5. 关键数据流

### 发送短信

```text
Web -> simplusd validation -> persist queued message
    -> typed sender -> Agent cellular SMS or simplus-netd per-Line IMS worker
    -> persist sent/unconfirmed/failed -> Web update
```

Simulator 继续使用进程内 Local Agent client。Host VoWiFi Line 使用独立 typed gateway，把 GSM7/UCS-2 产生的 SMS-SUBMIT TPDU 封装为 RP-DATA 和 binary SIP MESSAGE。worker 收齐各分段 SIP 最终响应即返回，绝不为 RP 报告占住业务请求；SIP `202` 只把带 provider ID 的消息持久化为橙色 `unconfirmed`。后台随后消费异步 RP 报告：全部匹配的 RP-ACK 才提升为 `sent`，单段 RP-ERROR 可成为 `failed`，multipart 的部分拒绝仍是 `unconfirmed`；响应丢失或报告超时也保持 `unconfirmed` 且不自动重发。同一个 multipart 操作始终逐段各提交一次，不会因为第一段缺少报告而截断，也不会重新提交已经处理过的分段。普通 hardware cellular SMS sender 仍未接入 production Agent。

### 接收短信

```text
modem/IMS worker -> bounded typed read -> simplusd
    -> persist raw/decoded message
    -> confirm persistence
    -> delete/ack modem copy when applicable
    -> Web update
```

Simulator 提供固定 welcome 入站消息；Host VoWiFi worker 则解析 SIP MESSAGE、network→MS RP-DATA 和 SMS-DELIVER，包括数字号码与 GSM7 字母型 TP-Originating-Address。后台执行有界同步：单段消息先落库再发送新的 SIP MESSAGE/RP-ACK；multipart 每片先进入持久 spool 再独立确认，完整且唯一的组才解码为一条可见消息。控制面重启后可继续组装，ACK 失败会保留已落库记录并重试，引用号复用导致的歧义组 fail closed。

### 电话

```text
Web -> emergency/number validation -> modem worker
    -> Agent call action -> observed call state -> Web
```

音频路径必须由硬件报告证明，不能从 USB descriptor 或 AT capability 推断。

## 6. 存储

当前代码使用五个 SQLite 数据库和独立录音目录。这不是 MVP 必须维持的领域边界，但立即合库会延迟核心功能，因此：

- 当前 migration 和数据库继续工作；
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
10. Host VoWiFi 只有 ePDG、Gm 和受保护 IMS REGISTER 同时成立才能报告 online；
11. `simplus-netd` 是 Host VoWiFi 临时网络对象的唯一 owner，Web/API 不得传入底层网络参数。
12. SMS over IMS 不能把 SIP 2xx 当作短信提交成功，且入站 RP-ACK 只能发生在可恢复持久化之后。

这些规则应优先由类型、测试和小型检查器强制执行，而不是在多份文档中重复描述。

## 8. 当前技术债处理原则

- 已完成但超出新 MVP 的 setup/auth/topology/lease 代码先保持可用；
- 不为删代码而中断短信纵切；
- 当旧抽象实际阻碍一个纵切时，用小型执行计划删除或折叠；
- 每次只保留一个业务真相源，避免在 daemon、Agent 和 Web 分别维护同一状态；
- 新设计优先选普通 Go、SQLite、Unix socket 和明确 JSON schema。
