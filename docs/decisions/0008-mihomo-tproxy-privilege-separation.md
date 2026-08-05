# 0008：Mihomo TPROXY 权限分离

## 状态

已接受，2026-08-04。

## 背景

Mihomo 的普通用户进程可以加载生成配置和提供 controller，但 Linux TPROXY listener 设置透明套接字需要网络管理能力。首次 production 启动证明：core 和 88 个代理记录正常，37 个国家 listener 却全部以 `operation not permitted` 失败。把 `CAP_NET_ADMIN` 直接授予承载 Web/API、数据库和订阅输入的 `simplusd` 会不必要地扩大攻击面。

## 决策

1. 保留 TPROXY：Host VoWiFi 需要透明 TCP/UDP 接入；SOCKS/HTTP 要求客户端主动支持代理，REDIRECT 又不能覆盖 UDP。
2. `simplusd` 保持无网络 capability，只通过 `0600` Unix socket 调用 `simplus-netd` 的固定 Mihomo supervisor 协议。
3. 当前 `simplus-netd` 只接受 status/start/stop；start 只接受固定订阅 ID、已安装版本目录中的 `mihomo` 和该订阅不可变版本目录中的 `generated.yaml`。协议不接受 shell、附加参数、设备名、路由或 nftables 表达式。
4. 只有 `simplus-netd.service` 及其 Mihomo 子进程取得 `CAP_NET_ADMIN`/`CAP_NET_RAW`，并同时启用 systemd filesystem、device、namespace、kernel 和 address-family 收窄。
5. supervisor 校验进程身份并拥有 PID manifest。启动后若新日志出现 listener bind error，即终止候选并报告失败；不能仅凭 PID 存活报告成功。
6. 本切片只解决 TPROXY listener 权限和生命周期隔离，不创建 namespace、veth、policy route 或 nftables，不把真实 Line/VoWiFi 流量导入 Mihomo。

## 后果

- 安装包新增常驻 `simplus-netd.service`；它与 `simplusd` 使用同一非登录服务 UID，以访问私有 Mihomo 工件，但只有 netd 获得网络 capability。
- netd 重启会由 systemd cgroup 收束其 Mihomo 子进程；下一次状态读取会把数据库 running 状态与 supervisor 事实对齐。
- 后续 Line 网络对象必须另行设计 typed plan，不得借当前 start 请求扩展为任意宿主网络命令。
