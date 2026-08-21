# Simplus

Simplus 是一个运行在 Linux 主机上的可信局域网通信控制后台，通过统一的 Web 界面管理
4G/5G 模组、线路、短信、电话、可拔插 eUICC 已安装 Profile，以及 Host VoWiFi 的专用
网络出口。

项目采用单管理员模型，不以公网 SaaS、多租户平台、通用代理网关或完整 eSIM RSP 平台
为目标。

> [!WARNING]
> 本项目仍处于快速开发阶段。功能、接口、数据结构和部署方式可能随时迭代，且不保证
> 现有功能的可用性、稳定性或向后兼容性。请勿将当前版本用于生产环境、关键通信或其他
> 无法容忍服务中断和数据丢失的场景；升级或重建前请自行备份数据。

## 当前状态

当前部署运行形态已从宿主机原生进程迁移到 Docker Compose。仓库通过 `control`、
`agent` 和 `netd` 三个镜像划分管理面、硬件访问与网络权限；Compose 中的 `app` 服务
运行 control 镜像，并由 `data-init` 和 `bootstrap` 完成数据目录初始化及首次管理员创建。

容器部署已经在 Debian 13/amd64 开发虚拟机完成镜像构建、隔离 smoke、真实模组发现、
Mihomo、Host VoWiFi 和单段自号码短信回环验证；clean Debian 虚拟机上的完整安装、升级
与卸载生命周期仍待验收。因此 Docker Compose 是唯一受支持的生产部署方式，但当前仍
属于部署候选，而不是稳定发布版本。

## 当前能力

- Go、SQLite，以及由 Vite、React Router、直接 Ant Design 和 TanStack Query 组成的管理后台；
- 安装时生成唯一管理员和随机初始密码；
- Simulator 中完整验证短信、电话、数字音频和 eUICC Profile 管理交互；
- QDC507 与 ML307A 的类型化硬件识别、固定端点角色和只读状态探测；
- QDC507 经受控 HIL 验收并装配到 production Agent 的原生蜂窝短信收发；
- Mihomo core、订阅、国家分组、共享 DoH、TPROXY 生命周期和 Zashboard 管理；
- ML307A Host VoWiFi 的 SIM AKA、ePDG、Gm IPsec、IMS 注册、保活、提前刷新和服务恢复；
- Docker Compose 下相互隔离的 control、agent、netd 镜像，以及类型化健康检查和持久化
  数据目录。

除已验收的 QDC507 原生短信和 ML307A Host VoWiFi 短信外，真实硬件电话、数字音频、
eUICC 切换及其他蜂窝通信仍未开放。默认 Agent 不提供任意
AT/QMI、设备路径或通用硬件写入口；真实副作用必须经过独立实现、验证和明确授权。

## Docker Compose 部署

当前支持边界为 Debian 13、Linux amd64、rootful Docker Engine 和 Docker Compose
2.24 或更新版本。Docker Desktop、Podman、ARM64、rootless Docker、user namespace
remap 及其他发行版尚未验证。Web 与 controller 只应开放给受信任局域网，不能直接暴露
到公网。

生产安装不需要克隆源码或在宿主构建镜像。首个计划发布的部署候选为 GitHub
Pre-release `v0.1.0`；发布资产和三个公开 GHCR package 完整后，从该 Release 下载
版本化部署包及校验文件：

```bash
version=v0.1.0
base="https://github.com/leonfox28/simplus/releases/download/$version"
curl -fLO "$base/simplus-compose-$version-linux-amd64.tar.gz"
curl -fLO "$base/simplus-compose-$version-linux-amd64.tar.gz.sha256"
sha256sum -c "simplus-compose-$version-linux-amd64.tar.gz.sha256"
tar -xzf "simplus-compose-$version-linux-amd64.tar.gz"
cd "simplus-compose-$version-linux-amd64"
cp .env.example .env
```

审阅脚本和 `.env` 后，完成一次性宿主准备、只读检查并拉取部署包写死版本的三个 GHCR
镜像；不要使用 `latest`，也不要在 `.env` 中增加镜像 tag：

```bash
sudo bash prepare-container-host.sh
bash check-container-host.sh "$PWD"
docker compose config --quiet
docker compose pull
docker compose up -d
docker compose ps
docker compose logs bootstrap
```

`SIMPLUS_DEVICE_GID` 必须与宿主 ttyUSB 设备所属组的数字 GID 一致；Debian 的 `dialout`
通常为 `20`。`bootstrap` 只在全新实例首次启动时输出 `simplus_admin` 的随机密码，请立即
保存并在首次登录后修改。管理后台默认位于 `http://<host-lan-ip>:8080`。

持久数据保存在 Compose 文件旁的 `./data/core` 和 `./data/agent`。`docker compose down`
会删除容器和临时运行对象，但保留这些目录；容器部署不会自动读取或迁移旧原生安装的
`/var/lib/simplus` 数据。

三个 GHCR package 首次发布后需由仓库所有者显式改为 public，之后匿名 `pull` 才是有效
安装证据。发布包、对应源码和 `simplus-images-v0.1.0.json` digest 清单均保留在同一
Pre-release；任一资产缺失时不要回退到生产机本地构建。

完整的宿主要求、权限模型、生命周期、更新方式和故障处理见
[Docker Compose 生产部署](docs/installation.md)。

## 本地开发

日常 Go/Web 开发、测试、Simulator 和前端热更新仍直接使用本机工具链，不需要进入开发
容器：

```bash
make dev-toolchain
make bootstrap-dev
make doctor
make test
make dev-sim
```

开发入口为 `http://127.0.0.1:5173`，API 只监听 `127.0.0.1:8080`。`make dev-sim` 仅用于
开发和模拟验证，不是正式部署方式。真实硬件、本机受限开发 Agent 和局域网调试说明见
[Linux 本地开发工作流](docs/development.md)。

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

除另行标注的材料外，Simplus 原创部分按照
[PolyForm Noncommercial License 1.0.0](LICENSE) 提供：允许非商业使用、修改和分发，
不授予商业使用权，也不要求用户仅因修改而公开源码。该模式属于“非商业源码可用”，
不是 OSI 定义的开源软件。第三方和单独许可材料见
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
