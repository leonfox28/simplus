# Debian V1 安装与卸载

## 支持边界

目标是单台 Debian Linux 主机、单管理员、可信 LAN。production Agent 固定为真实硬件只读及窄化 SIM-AKA owner：它只枚举 USB、执行白名单查询，并只为获准的 ML307A Host VoWiFi worker 提供类型化 AKA；不注册 RF、SMS、Call、eUICC mutation 或蜂窝数据连接路由。真实 Host VoWiFi 仅覆盖已验证的 ePDG/IMS 注册和保活，不代表真实短信、电话或媒体已经可用。

## 构建 bundle

在仓库工具链已经安装的普通用户会话运行：

```bash
scripts/release/build-debian-bundle.sh "$PWD/.dev/release/debian"
```

产物包含 `simplusd`、`simplus-agent`、受限 Mihomo/Host VoWiFi supervisor `simplus-netd`、固定 strongSwan 插件、Zashboard、生产 Web 资源以及安装/卸载脚本。

## 安装

ModemManager 与 Simplus 不应同时占用控制端点。需要使用 Simplus 的真实只读探测时，由管理员先明确停止并禁用 ModemManager；安装脚本不会替用户停止它。

```bash
sudo .dev/release/debian/install-debian.sh
systemctl status simplus-agent.service simplus-netd.service simplusd.service
curl http://<host-lan-ip>:8080/api/v1/system/health
```

全新实例安装结束时，CLI 会一次性显示随机生成的管理员凭据：用户名固定为 `simplus_admin`，密码为每实例独立的 32 字符随机值。数据库只保存密码哈希；请保存输出并在首次登录后更换密码。升级已有实例只显示“administrator already configured”，不会覆盖或再次输出凭据。

安装器默认从 IPv4 默认路由选择主机的私网地址并只监听该地址的 `8080` 端口。若需要指定另一张可信 LAN/VPN 网卡，安装时使用 `sudo SIMPLUS_LISTEN_HOST=<host-lan-ip> ./install-debian.sh`。应用拒绝 `0.0.0.0`、公网 IP、域名 Host 头和非私网地址，避免意外监听所有或公网接口。

首次访问直接使用安装器输出的账户登录，再确认本机存储目录即可进入后台。普通 HTTP 仅适用于可信局域网；可信 HTTPS 在“系统设置”维护，浏览器麦克风需要 HTTPS secure context。systemd unit 中的 `simplus-agent` 没有任何启用写操作的 flag。

`simplusd` 没有网络管理 capability。root `simplus-netd.service` 是 Mihomo 和 Host VoWiFi 网络状态的唯一 owner；两者通过 `/run/simplus-netd/mihomo.sock` 的固定 lifecycle 协议通信。激活真实 Line 时，`simplus-netd` 只按内部派生计划创建该 Line 的 namespace、veth、策略路由、nftables、strongSwan 和 XFRM，不接受 Web 提交底层路径或命令；停用和进程退出会清理这些临时对象。

## 故障恢复

- Agent 看不到模组：检查 ModemManager 是否仍在占用端点、USB 枚举和 `dialout` 权限；不要用任意 AT 命令绕过 Agent。
- Agent socket 创建失败：确认 unit 以 `User=simplus-agent`、`Group=simplus` 创建 RuntimeDirectory，不能通过增加 `CAP_CHOWN` 绕过组配置。
- Agent socket 只允许 root 维护工具和 `simplus` 服务 UID；不要加入普通交互用户或改成 `0666`。
- ML307A 未物化真实 Line：确认 `2ecc:3012` 的 Interface `02` 已由安装器绑定、SIM 为 READY 且 RF 保持 Off；不要手工打开 tty 绕过 Agent。
- Host VoWiFi 无法激活：先在线路页检查接入方式、当前订阅、国家出口和 Mihomo 状态；`mihomo-country` 不可用时不会回退 direct。
- Hardware 页面业务按钮不可用：这是 V1 read-only policy，不得通过 Simulator fallback 伪装成功。
- 数据位于 `/var/lib/simplus`；普通卸载保留该目录。

## 卸载

```bash
sudo .dev/release/debian/uninstall-debian.sh
```

确认不再需要数据后才显式执行：

```bash
sudo .dev/release/debian/uninstall-debian.sh --purge-data
```
