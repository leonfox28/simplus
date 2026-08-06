# Debian V1 安装与卸载

## 支持边界

目标是单台 Debian Linux 主机、单管理员、可信 LAN。production Agent 只枚举 USB、执行白名单查询，提供窄化的 SIM 鉴权能力，以及经过确认后才执行的 ML307A 运行时 RF 开关；不注册 SMS、Call、eUICC mutation 或蜂窝数据连接路由。真实 Host VoWiFi 仅覆盖已验证的 ePDG/IMS 注册和保活，不代表真实短信、电话或媒体已经可用。

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

production Web/API 固定监听 `0.0.0.0:8080`，Mihomo 启动时其带密码 controller 使用相同监听范围的 `0.0.0.0:19090`，两者都不依赖安装时的 DHCP 地址。Zashboard 没有独立服务或 unit，而是由 Mihomo `external-ui` 托管；后台按钮会把 wildcard 自动替换为浏览器当前访问的主机。安装器只尝试从默认路由取得一个地址用于打印 URL，未取得也不阻止安装；`SIMPLUS_LISTEN_HOST=<host-lan-ip>` 仅用于覆盖这个提示地址。`simplusd` 发生瞬时启动失败时每 5 秒重试，不会因网络尚未就绪而迅速触发 systemd 启动限流。

`0.0.0.0` 会覆盖主机当前和以后出现的所有 IPv4 接口，包括 VPN 或误接入的公网接口。Simplus 仍只面向可信 LAN，管理员必须用路由和主机防火墙限制 `8080/tcp` 与 `19090/tcp`。controller 密码提供鉴权但不为普通 HTTP 加密，不能把通配监听理解为可安全暴露公网。开发命令默认仍只监听 `127.0.0.1`。

首次访问直接使用安装器输出的账户登录，再确认本机存储目录即可进入后台。普通 HTTP 仅适用于可信局域网；可信 HTTPS 在“系统设置”维护，浏览器麦克风需要 HTTPS secure context。systemd unit 不通过 flag 开放任意硬件写入；唯一的 ML307A RF 路径是编译期固定的类型化接口。

`simplusd` 没有网络管理 capability。root `simplus-netd.service` 是 Mihomo 和 Host VoWiFi 网络状态的唯一 owner；两者通过 `/run/simplus-netd/mihomo.sock` 的固定 lifecycle 协议通信。激活真实 Line 时，`simplus-netd` 只按内部派生计划创建该 Line 的 namespace、veth、策略路由、nftables、strongSwan 和 XFRM，不接受 Web 提交底层路径或命令；停用和进程退出会清理这些临时对象。

## 故障恢复

- Agent 看不到模组：检查 ModemManager 是否仍在占用端点、USB 枚举和 `dialout` 权限；不要用任意 AT 命令绕过 Agent。
- Agent socket 创建失败：确认 unit 以 `User=simplus-agent`、`Group=simplus` 创建 RuntimeDirectory，不能通过增加 `CAP_CHOWN` 绕过组配置。
- Agent socket 只允许 root 维护工具和 `simplus` 服务 UID；不要加入普通交互用户或改成 `0666`。
- ML307A 未出现在候选列表：确认 `2ecc:3012` 的 Interface `02` 已由安装器绑定且 SIM 为 READY；RF 是独立模组状态，不影响候选发现或添加。不要手工打开 tty 绕过 Agent。
- Host VoWiFi 无法激活：先在线路页确认该 Line 具备鉴权能力、出口已明确配置，并检查当前订阅、国家出口和 Mihomo 状态；`mihomo-country` 不可用时不会回退 direct。
- Hardware 页面业务按钮不可用：先确认模组在线且声明对应的证据化能力；不支持的能力不得通过 Simulator fallback 伪装成功。
- 数据位于 `/var/lib/simplus`；普通卸载保留该目录。

## 卸载

```bash
sudo .dev/release/debian/uninstall-debian.sh
```

确认不再需要数据后才显式执行：

```bash
sudo .dev/release/debian/uninstall-debian.sh --purge-data
```
