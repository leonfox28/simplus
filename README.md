# Simplus

Simplus 是一个运行在 Linux 主机上的可信局域网通信控制后台，用统一的 Web 界面管理 4G/5G 模组、线路、短信、电话、可拔插 eUICC 已安装 Profile，以及 Host VoWiFi 的专用网络出口。

项目采用单管理员模型，不以公网 SaaS、多租户平台、通用代理网关或完整 eSIM RSP 平台为目标。

## 当前能力

- Go、React、Ant Design Pro 与 SQLite 管理后台；
- 安装时生成唯一管理员和随机初始密码；
- Simulator 中完整验证短信、电话、数字音频和 eUICC Profile 管理交互；
- QDC507 与 ML307A 的类型化硬件识别、固定端点角色和只读状态探测；
- Mihomo core、订阅、国家分组、共享 DoH、TPROXY 生命周期和 Zashboard 管理；
- ML307A Host VoWiFi 的 SIM AKA、ePDG、Gm IPsec、IMS 注册、保活、提前刷新和服务恢复；
- Debian bundle、三个受限 systemd 服务和局域网 Web 入口。

真实硬件的普通短信、电话、数字音频和 eUICC 切换仍未开放。默认 Agent 不提供任意 AT/QMI、设备路径或通用硬件写入口；真实副作用必须经过独立实现、验证和明确授权。

## 本地开发

```bash
make dev-toolchain
make bootstrap-dev
make doctor
make test
make dev-sim
```

默认开发入口为 `http://127.0.0.1:5173`，API 只监听 `127.0.0.1:8080`。需要从受信任局域网中的另一台设备访问时运行：

```bash
make dev-sim-lan
```

然后打开 `http://<host-lan-ip>:5173`。真实硬件和本机 systemd Agent 的说明见[开发工作流](docs/development.md)；`make dev-agent-deploy` 会请求 root 权限并重启本机开发 Agent，不属于普通构建步骤。

## 文档

- [文档地图](docs/README.md)
- [产品范围](docs/product.md)
- [架构与不变量](docs/architecture.md)
- [当前执行计划](docs/plans/active/mvp.md)
- [开发进度交接](docs/handoff.zh-CN.md)
- [兼容性与验证边界](docs/compatibility.md)
- [通用排障指南](docs/troubleshooting.md)
- [公开资料与隐私边界](docs/privacy-and-publication.md)
- [安装与卸载](docs/installation.md)

## 许可证

除另行标注的材料外，Simplus 原创部分按照 [PolyForm Noncommercial License 1.0.0](LICENSE) 提供：允许非商业使用、修改和分发，不授予商业使用权，也不要求用户仅因修改而公开源码。该模式属于“非商业源码可用”，不是 OSI 定义的开源软件。第三方和单独许可材料见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。

项目正在准备首次公开源码发布。公开仓库将从经过脱敏和扫描的全新 Git 历史创建，不直接公开现有私人开发历史。

> 当前首要目标平台是 Debian `linux/amd64`。其他发行版和 ARM64 在核心硬件业务路径完成后再决定支持级别。
