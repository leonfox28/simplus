# 0027：Line 统一拥有当前手机号观测

- 状态：Accepted / Implemented
- 日期：2026-08-13
- 修订：[`0012`](0012-web-managed-vowifi-runtime.md) 中由公共 VoWiFi 状态直接返回
  IMS 手机号的部分

## 背景

线路页曾只从 Host VoWiFi 运行状态读取 IMS 注册返回的手机号。因此没有激活 Host
VoWiFi 的原生蜂窝 Line 即使当前 SIM 能明确返回本机号码，页面仍会显示未获取；公共
页面还需要把 Line 身份与 VoWiFi 运行状态拼接起来。手机号由此错误地成为一种通信
路径的附属字段，而不是当前 Line 所绑定 SIM/Profile 的观测。

QDC507 已有固定只读 subscriber-number 查询的 HIL-0 证据。IMS 注册仍可能返回另一
个明确授权的国际号码；两种来源都可能为空、相同或不同，且都不适合作为持久配置或
稳定 SIM 身份。

## 决定

1. 手机号是 Line 的可选当前观测，不是持久 Line 配置、稳定 SIM 身份、首选通信路径
   或 VoWiFi 公共状态。管理员鉴权的 `ManagedLine.phoneNumbers` 是唯一公共所有者。
2. 蜂窝来源只能由具有当前证据的型号 adapter 实现固定只读能力。本阶段仅 QDC507
   执行一次固定 `AT+CNUM`；只接受唯一、显式 `+E.164`、type 145 且以 `OK` 结束的
   记录。空、失败、歧义或畸形结果均为不可用，不能令其他完整探测失败。
3. 蜂窝号码只附着到同一次探测中 present、ready 且身份已确认的 SIM/Profile。它经
   Agent typed observation、hardware Profile 和 inventory 传递，但不改变 SIM 指纹；
   缺卡、锁卡、换卡、身份失败、绑定冲突或 Line 无法解析时不贡献号码。
4. IMS 号码仍只在 worker 在线且其明确 IMS 身份可规范化为 `+E.164` 时存在。Line
   应用定义按稳定 Line ID 查询的窄端口，并在当前 Line 仍解析到原 SIM/Profile 时把
   它作为 `ims` 来源合并。查询是 best effort，只用于 Line 的列表和 mutation 展示；
   短信、电话、出口与 VoWiFi 控制使用的 `Topology` 不读取该来源。
5. Line 按号码精确去重，来源固定排序为 `cellular-sim`、`ims`，观测按号码排序。
   同值时返回一项和两个来源；不同值时全部返回，不猜测主号码或权威来源。
6. 公共 `VoWiFiLineState` 删除手机号，线路页只渲染 `ManagedLine.phoneNumbers`，并在
   桌面表格和手机卡片中显示所有不同值及来源。VoWiFi 页面继续只拥有激活、阶段、
   在线与出口状态。
7. 号码不写 SQLite，不进入普通日志、错误、SSE、setup/hardware 响应、任务证据或
   公开兼容性原始记录。测试只使用明确的合成 E.164 值；真实 HIL 值和 transcript
   继续保存在仓库外的私有记录系统。

## 后果

- Web 与其他上层只理解 Line 号码观测和来源枚举，不理解 QDC507、`CNUM`、IMS
  `P-Associated-URI` 或 VoWiFi worker 实现；
- 未启用 Host VoWiFi 的受支持蜂窝 Line 也能显示当前 SIM 明确返回的号码；
- IMS 与蜂窝结果不一致时保留事实而不阻断 Line，SIM 或运行状态变化后不会从数据库
  恢复旧号码；
- 未来型号只有取得独立证据并实现同一最小 adapter seam 后才能贡献蜂窝号码，不能在
  Line、OpenAPI 或 Web 增加型号分支。
