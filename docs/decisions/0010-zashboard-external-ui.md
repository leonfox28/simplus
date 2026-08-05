# 0010：由 Mihomo 直接托管 Zashboard

- 状态：Accepted
- 日期：2026-08-04
- 修订：controller 精确地址约束由 [`0015`](0015-zashboard-wildcard-controller.md) 替代

## 背景

管理员需要查看连接、节点组和实时流量等 Mihomo 原生控制信息。把这些能力重复实现为 Simplus 页面会制造第二套 controller 客户端，也会扩大业务后台职责。

## 决定

固定引入 Zashboard `v3.6.0` 的官方 `dist-no-fonts.zip` 静态产物，由 Mihomo 通过 `external-ui: ui` 直接托管。最初决定让 controller 只绑定安装器选定的可信私网地址和固定 `19090` 端口；[`0015`](0015-zashboard-wildcard-controller.md) 后续取消了安装时地址依赖。每个实例首次启动生成独立的 32-byte 随机 controller secret，以 `0600` 文件保存并写入每个新生成的私有订阅工件。

Simplus 的 Mihomo 页面只显示 Zashboard 版本、controller 地址、入口和可复制密码。Zashboard 不嵌入 Simplus 路由，也不复用管理员 Session。Core 安装升级仍归 Simplus 管理，Zashboard 入口不用于替代订阅转换、配置自检或运行生命周期管理。

## 后果

- Zashboard 没有独立进程或 systemd unit，其地址由 Mihomo controller 和浏览器当前主机共同确定；
- 切换订阅不改变 UI、controller 地址或密码；
- controller secret 会进入 `0600` 生成配置，不能记录到日志或诊断包；
- 更新 Zashboard 必须固定版本、官方资产、SHA-256 和 MIT License，不使用运行时自动下载；
- 直接 controller 管理可能修改节点组等 Mihomo 运行状态，但不能扩大为宿主通用代理或修改 Simplus 的 Line Binding。
