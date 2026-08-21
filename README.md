# Simplus

Simplus 是一个运行在 Linux 主机上的可信局域网通信控制后台，通过统一的 Web 界面管理
4G/5G 模组、线路、短信、电话、可拔插 eUICC 已安装 Profile，以及 Host VoWiFi 的专用
网络出口。

项目采用单管理员模型，不以公网 SaaS、多租户平台、通用代理网关或完整 eSIM RSP 平台
为目标。

> [!WARNING]
> Docker Compose 是 Simplus 唯一受支持的正式部署形态，但当前 `v0.1.1` 仍是
> Pre-release 部署候选，只适合受控评估。请勿用于关键通信或其他无法容忍服务中断、
> 数据丢失的场景；升级或重建前必须自行备份数据。

## 当前状态

当前部署运行形态已从宿主机原生进程迁移到 Docker Compose。仓库通过 `control`、
`agent` 和 `netd` 三个镜像划分管理面、硬件访问与网络权限；Compose 中的 `app` 服务
运行 control 镜像，并由 `data-init` 和 `bootstrap` 完成数据目录初始化及首次管理员创建。

容器部署已经在 Debian 13/amd64 开发宿主完成镜像与隔离 smoke，并以 `v0.1.1` Release
部署包和 GHCR 镜像完成有数据切换、回滚演练及最终健康、摘要、挂载验收。本次切换未执行
HIL；真实模组发现、Mihomo、Host VoWiFi 和单段自号码短信回环另有独立受控证据。这不是
clean VM 验证，完整安装、升级和卸载生命周期仍待验收，因此当前不是稳定发布版本。

## 当前能力

- Go、SQLite，以及由 Vite、React Router、直接 Ant Design 和 TanStack Query 组成的管理后台；
- 安装时生成唯一管理员和随机初始密码；
- Simulator 中完整验证短信、电话、数字音频和 eUICC Profile 管理交互；这些能力不会
  在 hardware backend 不可用时伪装成真实硬件成功；
- QDC507 与 ML307A 的类型化硬件识别、固定端点角色和只读状态探测；
- QDC507 经受控 HIL 验收并装配到 production Agent 的原生蜂窝短信收发；
- Mihomo core、订阅、国家分组、共享 DoH、TPROXY 生命周期和 Zashboard 管理；
- ML307A Host VoWiFi 的 SIM AKA、ePDG、Gm IPsec、IMS 注册、保活、提前刷新和服务恢复；
- Docker Compose 下相互隔离的 control、agent、netd 镜像，以及类型化健康检查和持久化
  数据目录。

除已验收的 QDC507 原生短信和 ML307A Host VoWiFi 短信外，真实硬件电话、数字音频、
eUICC 切换及其他蜂窝通信仍未开放。默认 Agent 不提供任意
AT/QMI、设备路径或通用硬件写入口；真实副作用必须经过独立实现、验证和明确授权。

## 5 分钟快速开始

当前支持边界为 Debian 13、Linux amd64、rootful Docker Engine 和 Docker Compose
2.24 或更新版本。Docker Desktop、Podman、ARM64、rootless Docker、user namespace
remap 及其他发行版尚未验证。Web 与 controller 只应开放给受信任局域网，不能直接暴露
到公网。

当前可安装候选是 `v0.1.1`；不要使用 `v0.1.0`、`latest` 或生产机本地构建的镜像。
生产安装不需要克隆源码，也不需要逐个下载 Release 中的源码包或其他发布材料。安装者
实际只需下载一个 `simplus-compose-*.tar.gz` 部署归档和它的一个 `.sha256` 校验文件；
三个 GHCR 镜像稍后由 `docker compose pull` 自动拉取。先确认宿主符合支持边界并取得
ttyUSB 设备的数字 GID：

```bash
test "$(dpkg --print-architecture)" = amd64
. /etc/os-release && test "$ID" = debian && test "$VERSION_ID" = 13
docker version
docker compose version
stat -c '%g' /dev/ttyUSB0
```

将 `/dev/ttyUSB0` 换成目标模组实际的 ttyUSB 节点。下载并校验部署包后，把内容安装到
持久目录 `/opt/simplus`，不要长期从 Downloads、`/tmp` 或其他临时解包目录运行：

```bash
version=v0.1.1
base="https://github.com/leonfox28/simplus/releases/download/$version"
curl -fLO "$base/simplus-compose-$version-linux-amd64.tar.gz" &&
  curl -fLO "$base/simplus-compose-$version-linux-amd64.tar.gz.sha256" &&
  sha256sum -c "simplus-compose-$version-linux-amd64.tar.gz.sha256" &&
  sudo mkdir -m 0755 /opt/simplus &&
  sudo chown "$(id -u):$(id -g)" /opt/simplus &&
  tar -xzf "simplus-compose-$version-linux-amd64.tar.gz" \
    -C /opt/simplus --strip-components=1 &&
  cd /opt/simplus &&
  cp .env.example .env
```

命令链会在下载、校验或创建目录失败时停止；如果 `/opt/simplus` 已存在，请按下方链接的
生命周期说明升级已有实例，不要把全新安装覆盖到该目录。

部署归档内的 `compose.yaml` 和 `.env.example` 是运行入口；宿主准备/检查脚本、简明说明、
版本信息、许可证和第三方 notices 随同归档提供，用于保持同版本的安全检查和合规材料，
不代表还要下载多套安装包。

启动前编辑 `.env` 中的 `SIMPLUS_HTTP_PORT`、`SIMPLUS_CONTROLLER_PORT` 和
`SIMPLUS_DEVICE_GID`。`SIMPLUS_DEVICE_GID` 必须与上面 `stat` 得到的数字一致；Debian
的 `dialout` 通常为 `20`，但应以实际设备为准。生产镜像版本由部署包固定，不要增加
镜像 tag 变量。

持久数据将写入 `/opt/simplus/data/core` 和 `/opt/simplus/data/agent`。这些目录包含
数据库、管理员状态和运行数据；请在升级或恢复前停止写入并完整备份 `/opt/simplus/data`。
确认数据位置后，审阅并执行宿主准备脚本，再渲染、拉取和启动：

```bash
sudo bash prepare-container-host.sh &&
  bash check-container-host.sh "$PWD" &&
  docker compose config --quiet &&
  docker compose pull &&
  docker compose up -d &&
  docker compose wait bootstrap &&
  docker compose ps &&
  docker compose logs bootstrap
```

`bootstrap` 只在全新实例首次启动时输出 `simplus_admin` 的一次性随机初始密码，请立即
保存并在首次登录后修改。已有实例重建时不会覆盖密码，也不会再次输出原密码。管理后台
默认位于 `http://<host-lan-ip>:8080`。

完整的升级、备份、回滚、权限、遗留数据和故障处理见
[生产部署与生命周期](docs/installation.md)，证据等级见
[兼容性与验证边界](docs/compatibility.md)。

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
- [生产部署与生命周期](docs/installation.md)

## 许可证

除另行标注的材料外，Simplus 原创部分按照
[PolyForm Noncommercial License 1.0.0](LICENSE) 提供：允许非商业使用、修改和分发，
不授予商业使用权，也不要求用户仅因修改而公开源码。该模式属于“非商业源码可用”，
不是 OSI 定义的开源软件。第三方和单独许可材料见
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
