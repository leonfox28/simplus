# 活跃计划：可信局域网通信控制 MVP

- 状态：In Progress
- 产品范围：[`../../product.md`](../../product.md)
- 架构：[`../../architecture.md`](../../architecture.md)
- 公开状态：[`../../handoff.zh-CN.md`](../../handoff.zh-CN.md)

## 目标

在 Debian Linux 主机上提供单管理员 Web 后台，以统一类型化接口管理 4G/5G 模组、线路、短信、电话、可拔插 eUICC 已安装 Profile，以及 Host VoWiFi 的专用 Mihomo 出口。

完整业务交互先在 Simulator 验证。真实硬件默认不开放业务写入；单独决策的 ML307A 运行时 RF 开关和 Host VoWiFi 使用各自的窄化类型化边界，不扩大为通用硬件写平台。现有 Host VoWiFi 真实证据均在 RF Off 下取得。

## 已完成里程碑

| 里程碑 | 结果 |
| --- | --- |
| 0：范围与开发环境 | 缩减产品范围，建立文档地图、Linux 本地工具链和验证入口 |
| 1：短信纵切 | Simulator 完成编码、长短信、持久化、幂等、入站 ACK 和会话 Web；真实 Agent 保持只读 |
| 2：多模组与日常使用 | 双线路并发边界、联系人、历史删除、容量提示和热插拔刷新 |
| 3：电话控制 | Simulator 完成呼入/呼出、接听、拒绝、挂断、DTMF、紧急号码拒绝和历史 |
| 4：数字音频 | Simulator 完成浏览器麦克风与扬声器 fixture；硬件无未验证媒体入口 |
| 5：eUICC | Simulator 完成已安装 Profile 列表、A→B→A、读回确认和持久化 |
| 6–7：Host VoWiFi 与 Mihomo 模型 | Simulator 完成 access-path 状态机、专用出口和 fail-closed |
| 8：可安装版本 | Debian bundle、systemd 服务、安装/卸载和 fresh-instance smoke |
| 9–11：管理完整性 | 管理员维护、模组/线路页面、Mihomo 管理和单向通知渠道 |
| 12–15：Simplus-owned Mihomo | 有界订阅转换、不可变工件、国家组、共享 DoH、TPROXY 与最小权限 supervisor |
| 16：首次管理后台迁移 | 从旧 Vue 双栈迁移到 React、Ant Design Pro Components 和 Umi Max |
| 17：External UI | 固定版本 Zashboard、Mihomo 托管和私有 controller secret |
| 18：真实 Host VoWiFi 纵切 | ML307A 类型化 SIM AKA、ePDG、Gm IPsec 和最小 IMS 注册 |
| 19：Web 管理的持续运行态 | per-Line 生命周期、keepalive、提前刷新、有界重连、恢复和脱敏 Web 状态 |
| 21：Host VoWiFi 短信 | 真实单段与 multipart 入站、Web/API 异步提交结果、单段与两段 UCS-2 自回环，以及自动重连后再次收发 |
| 22：已添加模组与能力适配层 | 动态发现与持久模组分离、IMEI 指纹稳定绑定，以及 ML307A 最小鉴权/RF 能力边界 |
| 23：持久线路层 | 管理员显式创建稳定 Line，运行时解析硬件目标，业务消费者不再依赖自动 Line |
| 24：线路与通信路径解耦 | Line 只表达稳定身份；RF、VoWiFi、出口和 transport 独立配置，新 Line 出口默认未配置 |
| 25：strongSwan 插件发布边界 | 同仓库独立 GPL 组件、锁定 Debian 输入、独立 `.deb`、对应源码和隔离 CI |
| 26：容器化生产部署 | 三镜像权限边界、Compose 生命周期、精确 USB/sysfs 映射、bridge 内 netd 和本地开发不容器化 |
| 27：显式前端运行时与后端权威通信 | Vite/React Router/直接 Ant Design/TanStack Query、生成客户端、游标分页与有界 SSE 失效流 |
| 28：收件人短信会话 | 跨 Line 收件人会话、持久未读水位、桌面双栏与手机主从工作区 |
| 29：短信首次持久化顺序 | messages v8 record sequence、SMS v2 cursor、历史修复和服务端顺序消费 |
| 30：飞书私聊一键绑定 | 最小权限一键创建应用、授权用户私聊、测试后加密持久化和仅本地解绑 |
| 31：QDC507 原生蜂窝短信 | 统一 typed adapter、fenced SIM/设备、`SM` persist-before-delete、受控入站/出站 HIL 与 production per-Line 装配 |

## Milestone 31：QDC507 原生蜂窝短信（已完成）

- [x] 接受 [`0026`](../../decisions/0026-qdc507-native-cellular-sms.md)，明确不使用 VoWiFi、
  不开放电话或通用蜂窝数据，并让型号差异只停留在 typed adapter/driver；
- [x] QDC507 稳定设备/SIM 身份、蜂窝状态、显式 RF、PDU-mode GSM7/UCS-2、共享 device gate、
  Agent fence、SIM `SM` 暂存、v2 SQLite recovery 和 per-Line transport/no-fallback 已实现；
- [x] 受控 HIL 已完成指定 SIM/批准 peer 的新入站 persist→PDU revalidate/delete→pending-zero，
  以及新出站 persist→modem-confirmed；历史 outcome-unknown operation 保持不变且未重发；
- [x] production Agent 必需 private state root，构造完整 store/adapter/router 后才声明 `sms-v1`；
  `simplusd` hardware 同时支持 Agent native 与可选 Host VoWiFi bundle，并按 Line 唯一选择；
- [x] `CLCC mode=1` 既存 data bearer 不再误判为语音阻断；语音/传真/未知仍 fail closed，SMS
  不创建、挂断或暴露数据 bearer，RF mutation 继续拒绝任何活动 bearer；
- [ ] 其他 SIM/运营商/收件人、长时间稳定性、真实电话、数字音频、
  RF 写入 HIL、通用蜂窝数据和 eUICC mutation 仍需独立设计与授权。

## Milestone 30：飞书私聊一键绑定（已完成）

- [x] 接受 [`0025`](../../decisions/0025-feishu-private-message-binding.md)，限定飞书中国版、
  授权用户私聊、单向出站、最小发送权限和仅本地解绑；
- [x] core v23 增加独立应用渠道表，三个敏感字段使用独立密文标签，旧 Webhook 表不
  重建；Down 只删除本地应用渠道；
- [x] 固定主机与路径的注册/消息客户端实现有界设备授权轮询、slow-down、拒绝、过期、
  Lark 拒绝、无重定向、响应上限和 test-before-persist；
- [x] OpenAPI、鉴权/CSRF/no-store HTTP 和 Vite/TanStack Query 页面完成 waiting/testing/
  terminal 状态、响应式目标显示、编辑与模式化删除确认；
- [x] Go、SQLite、HTTP、Vitest 与 Playwright 只使用合成 provider/transport；未创建真实
  飞书应用或发送真实消息。

## Milestone 29：短信首次持久化顺序（已完成）

- [x] 接受 [`0024`](../../decisions/0024-sms-record-sequence-ordering.md)，把短信业务时间与
  Simplus 首次持久化顺序分开；Calls 的 created-time keyset 保持不变；
- [x] messages schema v8 增加全局 AUTOINCREMENT record sequence，并按入站 `updatedAt`、
  出站 `createdAt` 回填 v7 历史；Up/Down 保留业务消息、unread marker、外键和水位；
- [x] 全局、remote-only、Line + remote、会话摘要与最近出站 Line 统一使用 sequence，
  replay、状态更新和删除不重排或复用序号；
- [x] SMS 响应改发 kind/version 隔离的 v2 sequence cursor；仍存在且 scope 一致的 v1
  boundary 可过渡映射，删除后的 v2 boundary 继续分页，Calls 明确只接受 v1；
- [x] Web 只反转拼接后的服务端 newest-first 页面，不再按 `createdAt/message ID` 推断；
  Vitest 与 synthetic desktop/mobile Playwright 覆盖业务时间倒退而本地后持久化的场景；
- [x] 全部验证使用临时 SQLite 与合成 fixture；未执行真实短信、RF、Host VoWiFi、模组
  写入或 HIL。

## Milestone 28：收件人短信会话（已完成）

- [x] 接受 [`0023`](../../decisions/0023-recipient-sms-conversations.md)，以 exact remote
  address 作为产品会话身份，跨 Line 合并历史，同时保留每条消息的实际 Line 与显式发送
  Line；不猜测本地号/国际号等价；
- [x] messages schema v7 增加 remote-address keyset 索引和 `AUTOINCREMENT` unread
  marker ledger；首次入站 message + marker 同 transaction，duplicate 不重复计数，删除
  级联，v6 旧历史升级后自然已读，Down/再 Up 保留业务短信；
- [x] 增加后端会话摘要分页、remote-only 历史与 snapshot read-through token；旧全局与
  Line + remote 查询保持兼容，Line-only fail closed，并覆盖同毫秒、跨 Line、并发新入站、
  重复/乱序 token、删除和 reopen；
- [x] OpenAPI、Go handler 与生成 Web Query 同步提供会话 list/read-state；鉴权 HTTP/SQLite
  始终权威，既有 `messages + sms.received` SSE 只失效 active query，不携带通信数据或未读；
- [x] 短信页改为桌面双栏和手机 list→detail→back，提供联系人名称/号码、摘要/角标、
  跨 Line 气泡、状态、历史 Line 回退、分页、显式 Line composer、字母地址只读、failed
  重新编辑、单条二次确认删除、临时新会话和联系人 CRUD；
- [x] 只有 detail 可见、页面前台且最新 HTTP snapshot 成功渲染后才提交其 opaque token；
  不自动重发、不 optimistic append、不在最近 Line 不可用时静默回退；
- [x] 以临时 SQLite、httptest、Vitest 与 synthetic desktop/mobile Playwright 验证迁移、
  分页、未读 race、HTTP contract、响应式主路径和无全局横向溢出；未执行真实短信、RF、
  Host VoWiFi、模组写入或 HIL。

## Milestone 27：显式前端运行时与后端权威通信（已完成）

- [x] 接受 [`0022`](../../decisions/0022-vite-react-query-web-runtime.md)，保留 React 19、
  Ant Design、单一前端栈、cookie/CSRF 和同源静态承载，同时 supersede ADR 0009 的
  Umi Max/Pro Components 决定；
- [x] 将构建与应用壳原子迁移到 Vite、React Router Declarative Mode、直接 `antd`、
  TanStack Query 和响应式桌面 Sider/手机 Drawer；完成所有页面迁移后删除 Umi、
  Pro Components、兼容桥和重复 UI/router/server-state 依赖；
- [x] 让 `@hey-api/openapi-ts` 从 OpenAPI 生成 Fetch SDK、TypeScript 类型、Zod
  schema 和 Query keys/options，由唯一手写 runtime 负责同源 cookie、CSRF、
  取消/超时、稳定错误与少量领域跨字段 guard；页面不直接 fetch 或复制公共 payload；
- [x] 当时为 Messages/Calls 实现 `(createdAt, stable ID)` opaque keyset pagination；
  Messages 的排序现已由 [`0024`](../../decisions/0024-sms-record-sequence-ordering.md) 改为
  首次持久化 sequence，Calls 仍保持原契约；
- [x] 增加同源鉴权、有界且不阻塞发布者的 SSE 失效/attention 流；HTTP 继续承载
  权威 snapshot 和 mutation，断线或丢失 hint 通过 resync + active query refetch
  收敛，只有新短信和来电产生明显页面内提示；
- [x] 迁移全部页面并覆盖桌面/手机的 loading、empty、error、partial、disabled、
  busy、无全局横向溢出和无意外 autofocus；未装配能力保持可见且显示原因；
- [x] 完成 Vitest/typecheck/build、后端与迁移测试、generated drift、Playwright
  desktop/mobile、完整 dependency/audit before-after 和文档/规范一致性检查；在这些
  验证完成前不把目标架构描述为已获得运行证据；
- [x] 以迁移前相同口径记录依赖与产物：完整 workspace tree 从约 1,578 收敛到 268，
  production tree 从约 1,486 收敛到 73；`web/dist` 仍为 29 个文件，字节数从
  3,004,106 降到 1,387,882（约减少 53.8%）；production/full audit 均为零 advisory；
- [x] 以一个匹配的 `simplusd` API + `web/dist` 版本交付，不维护 Umi/Pro fallback、
  双 API 或长期双通信层；本里程碑不执行真实短信、电话、RF、eUICC 或其他 HIL。

## Milestone 22：已添加模组与能力适配层

- [x] 持久化 `ManagedModem`，将动态发现候选和管理员配置分成两个真相源；
- [x] 提供只读候选扫描、已添加模组列表和显式添加 API；
- [x] 重做模组页，只展示已添加模组，并以“添加模组”单选表格展示未添加候选的相对 USB 地址、VID:PID、型号、脱敏序列标识、系统支持状态和能力；
- [x] 将已添加模组主表收敛为 `AT+CGMM` 实时型号、优先模块序列号并回退 USB Serial 的当前序列号、按需显示的 IMEI、在线状态、SIM 插入状态和射频开关；型号读取失败不回退 Adapter 名称，序列号仅作在线展示，IMEI 只实时读取并核对稳定指纹，不进入列表或持久存储；
- [x] 以 ML307A IMEI 的每实例 HMAC 指纹稳定绑定 `ManagedModem`，USB Serial 指纹作为辅助，sysfs 拓扑仅作运行时定位，并兼容一次性提升旧端口绑定；
- [x] 将 Host VoWiFi 的产品就绪条件与 RF 状态解耦，同时保留“真实证据仅覆盖 RF Off”的兼容性说明；
- [x] 只建立 ML307A `ATProbeAdapter`、`EquipmentIdentityAdapter`、`SIMPresenceAdapter`、`SIMIdentityAdapter`、`SIMAuthAdapter` 与 `RFControlAdapter`；设备身份、SIM 插入状态和 Line 绑定身份独立探测，插卡状态保持只读，上电 RF 策略在命令语义、读回和获准 HIL 前保持不可操作；
- [x] 将 Linux AT tty 生命周期与型号命令彻底分离：通用 transport 只做有界 I/O，标准只读计划由型号显式选择，ML307A 身份、SIM/IMS/APDU 与 RF 命令归属 adapter；
- [x] 移除 SIM/IMS 身份里的运营商 Home Domain 常量：完整 ISIM 优先，否则按 IMSI 与 EF_AD 的明确 MNC 长度派生并贯穿 SIP realm 校验；长度未知时 fail closed；一张无可用 ISIM 应用的真实 SIM 已完成该 fallback 的只读 HIL-0；
- [x] 下一纵切新增持久 Line，并把现有自动 Line 与业务绑定迁移过去。

## Milestone 23：持久线路层

- [x] 以随机业务 ID 持久化 `ManagedModem + SIM/Profile 身份 + 卡槽` 绑定和显示名称；
- [x] 提供已添加线路列表、当前可添加候选、显式创建和不改绑编辑 API；
- [x] 重做线路页，只有“添加模组 → 添加线路”后才出现可操作 Line；
- [x] 将短信、通话、Mihomo 国家出口和 Host VoWiFi 全部迁移到稳定 Line 目录；
- [x] 移除 Simulator access-path 的固定线路编号，使同一稳定 Line 契约覆盖模拟与真实后端；
- [x] 将临时 Agent Line 限制在运行时解析与 SIM preflight，Web/API 不公开硬件目标和身份指纹；
- [x] 验证 USB 端口变化保持绑定，模组离线、SIM 更换和身份冲突 fail closed；
- [x] 允许升级时只清理旧版 Host VoWiFi 网络清单，禁止旧清单重新启动。

## Milestone 24：添加线路与通信路径解耦

- [x] 删除 Line、Profile、setup、inventory 和公共 API 中的 access-mode/`rfSafety` 模型与修改入口，并移除重复保存出口状态的旧 Simulator access-path 与 Mihomo egress-profile API/表；
- [x] 数据库升级保留稳定 Line、SIM 绑定、国家出口和 VoWiFi 意图，并把旧 Host VoWiFi 的隐式直连物化为显式 `direct`；
- [x] 让候选接口覆盖所有已添加模组，返回实时型号、USB Serial、脱敏 SIM/Profile、能力和 `READY / MODEM_OFFLINE / SIM_ABSENT / SIM_UNAVAILABLE / ALREADY_ADDED / BINDING_CONFLICT`；创建时重新扫描，候选过期或身份变化即拒绝；
- [x] 添加线路只保存候选和名称，不修改 RF、Mihomo、Host VoWiFi 或任何通信 transport；绑定创建后不可改，本阶段不提供删除；
- [x] 新 Line 出口默认为 `unconfigured`，只接受显式 `direct` 或 `mihomo-country`；Host VoWiFi 准入只检查当前解析、`hostVoWifiAuth` 与明确可用出口，不读取或修改 RF；
- [x] 短信与电话移除接入方式分支，依据实际装配 transport、Line 能力和所需运行状态 fail closed；
- [x] 线路页改为 ProTable、候选单选表格和响应式配置抽屉，名称、出口与 VoWiFi 意图分别保存，未配置出口时禁用激活；
- [x] OpenAPI 和 Web 严格校验不返回身份指纹、运行时设备目标或设备路径，并覆盖过期候选、重复添加、SIM 更换、离线、端口变化、身份冲突与旧库迁移。

## Milestone 25：strongSwan 插件发布边界

- [x] 将 Simplus SIM AKA bridge 移入同仓库独立 GPL-2.0-or-later 组件，保持与根项目 PolyForm 许可边界；
- [x] 为 Debian 13/amd64 锁定精确 strongSwan source、运行 ABI `.deb`、下载地址与 SHA-256，不读取主机安装树作为隐式构建输入；
- [x] 在普通用户的临时 source tree/sysroot 中构建 Simplus bridge 与上游 `p-cscf`，产出由 `dpkg` 管理的 `simplus-strongswan-plugins` 包；
- [x] 同时生成包含全部锁定输入的对应源码归档、摘要和机器可读 manifest；netd 镜像只通过 `dpkg` 安装该包，不再复制裸 `.so`，也不再接受人工 source/build 路径；
- [x] 增加独立 CI 与包级验证，检查导出构造符、动态依赖、固定 runpath、文件权限、ABI 元数据、manifest 和对应源码完整性；
- [ ] 在新的 Debian 13/amd64 clean VM 上补充插件包构建/校验、netd 镜像安装与 Compose 生命周期 smoke；ARM64 和其他发行版仍需各自锁、对应架构构建及镜像证据。

## Milestone 26：容器化生产部署

- [x] 使用一个多阶段 Dockerfile 生成 `simplus-control`、`simplus-agent` 和
  `simplus-netd` 三个 production target；固定 Go/Node/Debian 基础镜像 digest，
  netd 镜像安装独立 strongSwan 插件包、对应 Debian runtime 和双摘要固定的 Mihomo
  core，tag release 附带校验过的 Mihomo GPL 源码；
- [x] 增加 `data-init / agent / netd / app / bootstrap` Compose 生命周期，持久数据
  固定在 `./data/core` 与 `./data/agent`，首次管理员密码只由幂等 bootstrap 输出；
- [x] 将 ML307A option 动态 ID 收入 Adapter registry。Agent 只有一个可写
  `option1/new_id`，没有容器网络，root entrypoint 注册后降到 UID 10002 并清空
  capability；app 固定 UID 10001 且没有 capability；
- [x] 让 netd 使用普通 Docker bridge，并以临时 netns/veth/nft TPROXY/XFRM probe
  验证容器 capability；per-Line 网络对象不进入宿主根 network namespace；
- [x] 增加 typed app/Agent/netd healthcheck、Compose 静态 contract、宿主准备/检查脚本
  和 GHCR amd64 tag workflow，并以校验过摘要的 standalone Compose 完成 config render；
  release 同时发布 strongSwan `.deb` 与对应源码；
- [x] 将正式安装接口收敛为确定性 `linux/amd64` Release 部署包：包内 Compose 写死严格
  版本 tag，`.env` 只保留两个端口和设备 GID；PR/手动 workflow 只验证构建，tag workflow
  按部署包/对应源码 → 三张 GHCR 镜像 → digest manifest 的顺序保持 Pre-release；
- [ ] 发布首个不可移动的 `v0.1.0` Pre-release，由仓库所有者把三张首次 GHCR package
  显式改为 public，并在未登录 registry 的环境核对匿名 pull、OCI metadata、digest
  manifest 和解包后的 Compose config/pull；该项不得执行 `up`、宿主准备或 HIL；
- [x] 保持日常开发、Simulator、Go/Web 测试和 CI 使用本机/runner 工具，不提供 dev
  image；保留受限 `simplus-agent-dev` 供本机 HIL；
- [x] 在当前 Debian 13/amd64 开发 VM 构建三个 production target，并以隔离空
  USB/sysfs 完成全栈 Compose smoke：typed health、固定 UID/capability、netd 临时
  netns/veth/nft TPROXY/XFRM preflight 与清理、首次登录、幂等 bootstrap 和保留数据
  重建均通过；该项不计为 clean-VM 或真实硬件 HIL；
- [ ] 在安装 Docker 的 clean Debian 13/amd64 VM 运行 `docker compose config`、三个
  镜像构建、全新初始化、升级、停止、卸载和权限/namespace smoke；
- [x] 在正式 Compose 切换后完成真实设备映射与预期模组候选的只读 HIL-0，不修改 RF
  或触发业务动作；
- [x] 经逐次授权完成当前开发 VM 容器中的 Mihomo 国家出口、Host VoWiFi 注册与
  单段自号码短信回环 HIL；过程没有请求 RF 写入，并独立于既有 systemd Runtime
  证据。Compose 现为唯一受支持的 production 部署方式，clean-VM 生命周期验收仍待完成。

## Milestone 20：公开源码准备

- [x] 定义公开产品文档与私有实验记录的边界；
- [x] 将原始 HIL、逐节点网络测试、完整现场 handoff 和旧私人归档迁出公开文档树；
- [x] 建立公开兼容性摘要、通用排障指南和发布隐私规范；
- [x] 对公开文档增加本机路径、私网地址、订阅/代理凭据和通信身份防泄漏检查；
- [x] 由仓库所有者选择并确认 PolyForm Noncommercial 1.0.0 非商业源码可用许可证，并记录单独许可材料边界；
- [x] 从脱敏工作树创建不包含现有私有历史的全新 Git 仓库；
- [x] 对全新工作树与完整新历史运行 secret scan，并人工复核首个发布树；
- [x] 经仓库所有者最终确认后创建公开远程并推送；
- [x] 通过 Milestone 27 删除 Umi/Pro 传递依赖链并复核 production/full audit；继续复核后续 Actions、issues 和 release assets。

## Milestone 21：Host VoWiFi 短信

- [x] 按 TS 24.341/24.011 实现 RP-DATA、RP-ACK、RP-ERROR 和 binary SIP MESSAGE 的有界编解码；
- [x] 通过固定 root-only 读取发现 SIM 已配置短信中心，并仅在可用时注册 `+g.3gpp.smsip`；
- [x] 在 per-Line worker 内实现受保护 SIP 发送、`In-Reply-To` 关联的异步提交报告、入站队列和 persist-before-RP-ACK；
- [x] 通过 `simplus-netd` 类型化 API 接入既有短信服务、历史页、幂等 operation 和后台入站同步；
- [x] 增加持久化 multipart 入站分片 spool、逐分片 ACK、歧义拒绝、重启恢复和过期清理；
- [x] 在明确授权后完成真实 multipart 入站的逐片持久化、delivery report、完整重组和 worker 队列清空，并把脱敏结论写入兼容性文档；
- [x] 完成真实 GSM7 字母型发送方单段入站、业务落库、RP-ACK 与队列清空 HIL；
- [x] 将发送结果未知建模为持久 `unconfirmed` 状态并在 Web 中单独显示，保持 operation 幂等且禁止自动重发；
- [x] 用 SIP/RP fixture 证明提交请求不等待 RP 最终报告：同一 multipart 操作逐段各提交一次并先保持 `unconfirmed`，后续匹配的 RP-ACK 经持久化同步后才成为 `sent`；
- [x] 修正标准 RP-ERROR cause-length 解析、普通 SMS-DELIVER-REPORT TPDU、REGISTER `P-Associated-URI` 身份选择、RP reference 生命周期和 RFC Call-ID 校验；
- [x] 短信页面通过有界 SSE 失效提示重新读取 HTTP 权威快照，避免后台已接收消息仍需手动刷新；
- [x] 完成一条受控单段 GSM7 服务请求的出站 RP 最终结果 HIL：SIP `accepted` 后取得关联 RP-ACK，且对应 multipart 业务回复完成持久化与重组；
- [x] 完成从公开 Web/API 发起的单段服务请求 HIL，并确认业务库由带 provider ID 的 `unconfirmed` 异步提升为 `sent`；
- [x] 完成普通号码的单段自号码回环 HIL：出站取得关联 RP-ACK，随后同一业务消息重新入站、持久化并确认；
- [x] 完成普通号码的两段 UCS-2 长短信自号码回环 HIL，并逐字符确认出站与重组后入站正文一致；
- [x] 完成自动重连后的短信行为 HIL：重新注册后再次取得出站关联 RP-ACK，并完成两段消息入站重组与确认。

## 后续产品工作

以下能力尚未完成，且不能因为 Simulator 或 Host VoWiFi 注册成功而宣称可用：

1. 真实模组原生蜂窝短信收发；
2. 真实呼入、外呼、DTMF 和媒体；
3. 真实 eUICC 已安装 Profile 切换；
4. SMS over IMS 的其他收件人互通、VoWiFi 通话和 RTP/RTCP；
5. 显式 IMS de-registration、IKE/CHILD rekey 与多日稳定性；
6. ARM64、其他发行版、签名包和供应链发布材料。
7. QDC507 的稳定设备身份读取与 `ManagedModem` 添加，以及与真实 `RFControlAdapter` 实现一致的能力证据；在专项只读/写入 HIL 前不得复制 ML307A 命令或降低稳定身份要求。
8. 将 ePDG FQDN、IKE responder identity 和远端选择从当前已验证运营商接入 profile 中解耦，并分别完成其他运营商 HIL；动态 IMS Home Domain 本身不能作为多运营商兼容证据。
9. 完成 Milestone 26 的 clean-VM Compose 生命周期验收；ARM64、rootless/userns、
   Podman、Docker Desktop 与 SELinux 发行版仍需独立设计和证据。
10. 在 `v0.1.0` 合并提交上完成 Pre-release/GHCR 首发和所有者 public visibility 门禁；
    若代码修复才可完成发布，保留原 tag 并使用新的 patch 版本。

每项真实副作用都需要独立设计、最小实现、明确授权和与风险相称的 HIL；Web/API 不得暴露任意 AT/QMI、设备路径、网络命令或配置路径。

## 非目标

- 公网 SaaS、多租户和远程入站控制；
- 通用代理、通用蜂窝数据网关或完整 eSIM RSP；
- 企业 PKI、企业审计、Web 自更新和备份恢复平台；
- 为未来假设场景预建通用硬件命令或安全编排框架。

## 验收方式

按风险选择最小有效验证：

1. 受影响包的针对性单元或集成测试；
2. Simulator 用户路径；
3. OpenAPI 生成一致性、Web typecheck 和 build；
4. 受影响二进制构建；
5. 公开资料检查与 secret scan；
6. 真实硬件只执行当前任务明确授权的 HIL。

公开仓库准备完成不等于 V1 的模组原生蜂窝短信和电话目标完成。
