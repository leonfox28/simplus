# 0001：把 V1 重置为局域网短信与电话工具

- 状态：Accepted，部分范围由 [`0002`](0002-restore-vowifi-mihomo-euicc.md) 修订
- 日期：2026-08-03
- 影响：产品范围、架构优先级、文档结构和后续开发顺序

## 背景

> 2026-08-03 后续决定恢复 Host VoWiFi、窄化 Mihomo 出口和可拔插 eUICC 已安装 Profile 管理。下文保留第一次范围重置时的原始决定；冲突处以 `0002` 为准。

旧规划把单机局域网工具扩展成了同时处理 Host VoWiFi、代理网络、企业连接器、PKI、签名升级、诊断、审计、eUICC 和多平台发行的综合系统。规范文件增长到数千行，但核心短信和电话纵切仍未开始。

用户重新确认的真实目标是：在可信局域网内，通过 Linux 主机控制 4G/5G 模组收发短信和接打电话。

文档组织参考 OpenAI 的 [Harness Engineering](https://openai.com/zh-Hans-CN/index/harness-engineering/)：仓库作为记录系统，入口是一张地图；计划是一等工件；通过少量可执行不变量建立反馈回路；定期清理过期规则，而不是继续扩充一本巨型手册。

## 决策

### 保留

| 能力 | 原因 |
| --- | --- |
| 一个管理员登录 | 避免局域网误操作，成本低 |
| 非 root Agent 与 Unix socket | 隔离 Web 进程和硬件权限 |
| 固定类型的模组动作 | 防止 UI 直接执行任意 AT/QMI |
| 每 Modem 串行 worker | tty/模组本来就是串行资源 |
| operation ID 与超时后观察 | 避免明显重复短信/拨号 |
| 入站短信 persist-before-delete | 防止真实业务数据丢失 |
| 紧急号码拒绝 | 项目不能承担紧急通信职责 |
| HIL 明确授权 | 短信、电话和 RF 有费用与外部副作用 |
| 普通目录/数据库权限 | 合理的低成本默认值 |

### 停止作为 MVP 门槛

| 旧设计 | 新决定 |
| --- | --- |
| root 单次 bootstrap、完整 setup 状态机 | 已有代码保留；不继续增加恢复矩阵、重新认证和 root reset 门槛 |
| 内建 local CA、候选证书 probe、自动轮换 | 交给外部反向代理/VPN；MVP 可在可信 LAN 使用 HTTP |
| 多层 ResourceGroup lease、generation/fencing、双重 durable ledger | 不扩展为 SMS/Call 前置平台；改用每 Modem 串行 worker 和最小 operation 状态 |
| hash-chain 审计、180 天/1 GiB、WORM 语义 | 改为普通操作日志；不作为首版门槛 |
| 五库 dataset identity、inode/mount pinning、复杂 anti-symlink 策略 | 已有代码暂留；不继续扩展，MVP 后评估合库 |
| StorageProtection、ClockHealth、LUKS/swap 展示 | 移出 MVP |
| 签名 metadata、emergency checkpoint、自动回滚、SBOM/provenance | 移出首个可用版本；先提供可重复构建与手工安装 |
| 加密诊断包、不可逆伪名、age recipient | 移出 MVP；先提供本地普通日志 |
| 多浏览器 PhoneConsoleLease 与 15/60 秒接管 | 改为单一活动控制页面；复杂接管以后再说 |
| 强制双语完整度、Playwright、72 小时 soak、多发行版认证 | 不作为首个可用版本门槛 |

### 从产品范围移除

- Host VoWiFi、Host IMS、strongSwan/ePDG；
- Mihomo、netns/TUN/XFRM 和代理出口；
- 普通蜂窝数据路由；
- eUICC Profile 管理和复杂 PIN/PUK credential vault；
- 企业微信、飞书、Telegram、Webhook 和第三方 service token；
- Backup、RecoverySet、WebDAV；
- Web 内升级与企业发行平台。

以上不是“永远禁止”，而是没有真实用户需求前不投入。

## 对既有代码的处理

本决策不要求立即删除已经完成的 setup/auth、动态拓扑、ResourceGroup lease 或 Agent outcome ledger。一次大规模回滚会再次消耗时间在基础设施上。

后续原则：

1. 先用现有基础完成短信和电话；
2. 不再让新业务依赖更多通用安全层；
3. 旧机制真正阻碍纵切时再删除；
4. 删除时用针对性测试证明用户路径没有回退；
5. 私人历史规划保存在仓库外记录系统，不随公开源码发布，也不再是实现依据。

## 结果

- 产品范围变小，交付顺序由业务纵切驱动；
- 安全从“覆盖所有假设威胁”改为“覆盖可信 LAN 中的误操作、硬件独占、费用和数据丢失”；
- 文档从单个巨型规划改为地图、产品说明、架构地图、活跃计划、决策和证据；
- 当前下一目标改为单模组 SMS 完整纵切，不再先实现通用 daemon command platform。
