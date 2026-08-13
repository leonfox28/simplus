# 0026：QDC507 原生蜂窝短信生产装配

- 状态：Accepted / Implemented
- 日期：2026-08-13

## 背景

QDC507 的型号 adapter、PDU-mode driver、SIM `SM` 暂存、SQLite 恢复账本、设备/SIM
fence 和应用层 per-Line transport 已分别完成。受控 HIL 已在同一指定 SIM 与获批 peer
上完成一条入站 persist→revalidate/delete→pending-zero，以及一条新的出站
persist→modem-confirmed；原先 outcome-unknown 的历史操作仍保持不变。

产品需要把这条已经验收的纵切接入普通 hardware runtime，同时保持 Host VoWiFi、电话和
通用蜂窝数据边界不变。

## 决定

1. production `simplus-agent` 必须接收绝对、非根的私有 `--state-root`，在启动时打开固定
   QDC507 SMS v2 SQLite 状态，构造 tty transport、PDU driver、完整 SMS adapter、runtime
   registry、共享 device gate 和 typed SMS backend。任一依赖失败均退出，不能退化为无恢复发送。
2. Agent 的普通 managed handler 只有在完整 backend 成功注入时才声明 `sms-v1` 并注册固定
   List/Read/Send/Acknowledge 路由；不增加任意 AT、QMI、设备路径、电话或数据 API。HTTP
   write timeout 必须覆盖完整 multipart dispatch 与结果持久化预算。
3. QDC507 只有 production composite 把 `sms-control` 提升为 observed；安全的
   `DefaultRegistry()` 仍保留未装配 QDC507 与 ML307A，不能仅因 fixture 或型号描述符声明 SMS。
4. `simplusd` hardware backend 始终构造 Agent native SMS bundle；配置 Host VoWiFi supervisor
   时再独立构造 Host VoWiFi bundle。每条 Line 依据能力必须且只能匹配一个 bundle；选中 transport
   不可用时不得 fallback。
5. SMS send 的新鲜 probe 必须拒绝语音、alternate voice/fax 和无法分类的 `CLCC` 状态。已经
   存在的 `CLCC mode=1` data bearer 可与 SMS 共存，但 Simplus 不创建、挂断、配置或暴露该 bearer，
   也不因此获得通用蜂窝数据能力。RF 控制仍对任何活动 call/bearer fail closed。
6. 入站仍固定使用 SIM `SM` 作为暂存区；应用数据库先持久化，随后才逐段复核和删除 SIM PDU。
   Agent v2 recovery 数据库是安全恢复账本，不是第二份用户消息历史。

## 后果

- container、开发 unit 和 Debian unit 都传入固定私有 state root；既有 Agent data volume/
  `StateDirectory` 提供 mode 0700 owner 边界，无新增 capability、网络 attachment 或 sysfs 写面；
- 当前 HIL 只证明指定 QDC507、SIM、运营商和批准 peer 的蜂窝 SMS；电话、通用蜂窝数据、其他
  SIM/运营商和长期稳定性仍未验证；
- 原 outcome-unknown operation 不因后续确认成功而被重发、删除或改写；
- 自动化覆盖 production handler feature gate、state-root fail-closed、registry evidence、per-Line
  no-fallback、call-mode 分类、持久化/恢复和部署文件一致性；普通验证不执行真实短信。
