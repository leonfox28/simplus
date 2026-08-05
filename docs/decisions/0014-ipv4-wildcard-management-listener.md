# 0014：production 管理后台使用 IPv4 通配监听

- 状态：Accepted
- 日期：2026-08-05

## 背景

旧安装器把安装时从默认路由探测到的私网地址直接写入 `simplusd.service`。在 DHCP 主机上，该地址可能在开机早期尚未配置，也可能在之后的租约中改变；前一种情况会让 bind 失败，配合 systemd 的快速重启触发启动限流，后一种情况会让服务持续依赖旧地址。要求所有可信 LAN 部署固定地址会增加不必要的安装和恢复负担。

## 决定

production `simplusd` 固定监听 IPv4 wildcard `0.0.0.0:8080`。应用配置只额外接受这个明确的 IPv4 wildcard；开发默认继续使用 `127.0.0.1:8080`，公网 IP、域名和 IPv6 wildcard 仍被拒绝。管理员登录、Session、CSRF、typed API 和 ready gate 不因监听方式改变。

安装器探测的私网地址不再用于主 Web/API bind，只用于显示浏览器访问 URL。Mihomo controller 后续也按 [`0015`](0015-zashboard-wildcard-controller.md) 从管理后台监听范围派生，不再保存安装时地址。`simplusd.service` 保留 `network-online.target` 顺序提示，并设置 `RestartSec=5s`，使其他瞬时依赖失败不会在短时间内耗尽 systemd 启动额度。

## 后果

- DHCP 地址未就绪或以后变化不会阻止主后台监听，用户使用主机当前地址访问即可；
- Web/API 会同时监听主机所有 IPv4 接口，包括以后出现的 VPN 或公网接口；部署者必须用网络边界和主机防火墙确保 `8080/tcp` 只对可信 LAN 开放；
- Mihomo controller 的后续 wildcard 决策、密码与浏览器 URL 处理见 [`0015`](0015-zashboard-wildcard-controller.md)；
- 本决策不增加公网部署、自动证书或接口发现能力。
