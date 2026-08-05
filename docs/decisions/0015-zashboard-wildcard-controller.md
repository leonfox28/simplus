# 0015：Zashboard controller 跟随管理后台监听范围

- 状态：Accepted
- 日期：2026-08-05
- 修订：[`0010`](0010-zashboard-external-ui.md) 的精确地址约束

## 背景

Zashboard 是由 Mihomo `external-ui` 直接提供的静态页面，不是独立守护进程。旧实现仍把安装时探测到的私网地址通过 `simplusd.service` 传入配置生成器，导致 DHCP 地址变化后 controller 和后台按钮继续引用旧地址；为静态 UI 单独增加服务或网络发现机制都没有必要。

## 决定

Zashboard 继续没有独立进程或 systemd unit。`simplusd` 从自己的管理监听地址派生 controller：production 的 `0.0.0.0:8080` 对应 `0.0.0.0:19090`，loopback 开发环境仍对应 loopback controller。Mihomo 只有运行时才托管 UI 和 controller。

浏览器不能把 `0.0.0.0` 当作远程目的地址，因此 Web 在打开 Zashboard 时用当前页面的 hostname 替换 wildcard，同时把实例随机 controller secret 写入 Zashboard setup fragment。现有不可变订阅工件不原地修改；用户下一次选择、启动或重启 Mihomo 时，配置管理器使用本地保存的 raw YAML 和节点摘要生成新版本，先由当前 core 校验成功再切换 pointer，不自动重启正在运行的 core。

## 后果

- DHCP 地址变化不需要修改 systemd 或重新下载订阅；
- `19090/tcp` 会在 production 的所有 IPv4 接口监听，实例 secret 提供鉴权，但普通 HTTP 不提供传输机密性；部署者必须把 controller 限制在可信 LAN，不能暴露公网；
- Zashboard 仍不复用 Simplus 管理员 Session，直接 controller 能修改 Mihomo 运行选择，但不能修改 Simplus 的订阅、Line Binding 或宿主网络命令；
- 生成规则变化只在显式的选择、启动或重启边界应用，避免后台静默中断 Mihomo。
