# Docker Compose 生产部署

## 当前状态

Docker Compose 是 Simplus 唯一受支持的生产部署方式，并保持 `simplusd`、`simplus-agent`、
`simplus-netd` 三个权限边界。生产安装接口是 GitHub Pre-release 中的版本化部署包和
GHCR 镜像，不要求源码 checkout、Git、Go、Node、pnpm、Make 或本地 `docker build`。
容器定义、镜像构建、宿主准备、数据初始化、typed healthcheck 和发布 workflow 已进入
仓库；开发 VM 已完成真实硬件容器业务 HIL，但 clean Debian VM 生命周期尚未验收，
因此当前仍属于可审查的部署候选，而不是稳定发布版本。

不可移动的 `v0.1.0` 已保留为不完整 Pre-release：部署包及对应源码资产发布成功，GHCR
manifest 探测随后在任何镜像构建/推送前失败，且没有生成 digest manifest。不要安装或
补齐该版本；修复后的首个完整候选使用新 tag `v0.1.1`。

`v0.1.1` 的八个 Release 资产、三张 `linux/amd64` 镜像和 digest manifest 已完整发布；
未登录 GHCR 的空凭据环境已完成镜像 inspect、OCI metadata/digest 核对及解包后的
Compose config/pull。该证据只验证发布与匿名分发，没有执行 `compose up`、宿主准备或
HIL，也不替代 clean-VM 生命周期验收。

三个 production target 和隔离无硬件 Compose 已在 Debian 13/amd64 开发宿主完成
smoke；该证据验证镜像、权限、netd preflight、bootstrap、Web 登录和数据保留重建，
不等于 clean-VM 或真实模组验收，也没有执行 RF、VoWiFi、短信或电话动作。
随后在开发宿主上使用校验过的 `v0.1.1` Release 部署包和 GHCR 镜像完成了既有数据的
停机快照、恢复、回滚演练、正式切换，以及容器健康、镜像摘要和数据挂载验收。本次切换
未执行 HIL；另有独立受控证据覆盖预期模组发现、Mihomo 国家出口、Host VoWiFi 注册和
单段自号码短信回环。独立 HIL 没有请求 RF 写入；部署切换和 HIL 都不能外推为 clean-VM
生命周期、其他收件人互通或电话证据。

容器部署默认创建全新实例，数据位于部署根旁的 `./data`，不会读取或迁移原生
`/var/lib/simplus`。本文推荐把部署根固定为 `/opt/simplus`，对应数据为
`/opt/simplus/data`。需要保留其他安装的数据时，应先停止写入并使用单独审查、可回滚的
迁移方案；不要在运行中手工复制数据库或运行目录，仓库不提供自动迁移或恢复推断。

## 支持边界

- Debian 13、Linux amd64；
- rootful Docker Engine 和 Docker Compose 2.24 或更新版本；
- 未启用 rootless 模式和 user namespace remap；
- 宿主内核提供 network namespace、veth、nftables TPROXY、XFRM 和 USB serial
  `option` 驱动；
- 当前动态 tty 设备权限只覆盖已经验证的 ML307A；
- Docker Desktop、Podman、ARM64、SELinux 发行版和其他设备 major 尚未验证。

Docker Engine 的安装按 [Docker 官方 Debian 指南](https://docs.docker.com/engine/install/debian/)
完成；Simplus 不替用户安装或修改 Docker daemon。

## 下载部署包

每个严格 `vX.Y.Z` tag 对应一个 `linux/amd64` 部署包。下面以替代部署候选 `v0.1.1`
为例；普通安装只需下载一个 `.tar.gz` 部署归档和它的一个 `.sha256` 校验文件。Release
页面中的 strongSwan/Mihomo 对应源码、包与镜像 digest manifest 用于发布审计、许可和
可追溯性，不是需要逐个下载或解压的安装包；三个运行镜像由后续 `docker compose pull`
直接从 GHCR 拉取。版本变量必须同时决定下载目录和文件名，不要改为滚动的 `latest`：

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

归档内的 `compose.yaml` 和 `.env.example` 是运行核心：前者写死同版本 GHCR tag，后者
只提供三项安装参数。两条宿主准备/检查脚本、简明说明、版本/commit 信息、根许可证和
第三方 notices 一并随包提供，以保持同版本的安全门禁、来源标识和合规材料；它们不是
额外下载项。校验必须在解包前完成；不要使用 `curl | sh`。命令链会在下载、校验或创建
目录失败时停止；`/opt/simplus` 应专用于这一实例，已有目录必须按“生命周期”一节升级，
不能用全新安装覆盖。命令只修改新建目录本身的属主。编辑 `.env` 时只设置
`SIMPLUS_HTTP_PORT`、
`SIMPLUS_CONTROLLER_PORT` 和 `SIMPLUS_DEVICE_GID`，生产安装不接受镜像版本变量。

tag workflow 会先把部署包、strongSwan 包/对应源码和 Mihomo 对应源码发布到同一
GitHub Pre-release，随后才推送三张镜像。`v0.1.1` 三个 package 已可匿名访问；未来首次
生成的新 package 仍须逐一验证，若保持 private，必须由仓库所有者改为 public，且该
visibility 变更不能再恢复为 private。在匿名 pull 完成前，候选不能宣称可匿名安装。
Release 中的
`simplus-images-v0.1.1.json` 记录实际 digest，供发布后审计，Compose 仍引用不可移动
的版本 tag。

## 一次性宿主准备

宿主必须在每次开机时加载 `option`，但不在宿主写动态 USB ID。部署包中的脚本只创建
模块加载配置并执行本次 `modprobe`：

```bash
sudo bash prepare-container-host.sh &&
  bash check-container-host.sh "$PWD"
```

等价的持久配置是：

```text
/etc/modules-load.d/simplus.conf
option
```

Agent 容器启动后从型号 Adapter registry 取得白名单 ID，并只写映射进容器的
`/sys/bus/usb-serial/drivers/option1/new_id`。USB bus 列表及其指向 `/sys/devices` 的
目标树均只读映射，保证动态设备链接可以解析；Compose 或 Web 不能提供任意 VID/PID。

宿主检查会拒绝 rootless/userns、过旧 Compose、缺失的 `new_id` 和符号链接数据目录。
发现 ModemManager 运行时只给出警告，不会停止或改写它；若它占用目标串口，Agent
会 fail closed。检查还会拒绝仍在运行或已启用的遗留 Simplus systemd 服务或本机开发
Agent，避免它们在当前或下次启动时与 Compose 争用模组、端口或运行对象。

## 启动全新实例

部署包的 Compose 已固定为同一 Release tag，不通过环境变量覆盖版本。确认 `.env` 中
端口和设备 GID 后先渲染并拉取；若 GHCR 尚未公开，应停止并等待仓库所有者完成可见性
门禁，不要在生产机本地构建替代镜像。

```bash
docker compose config --quiet &&
  docker compose pull &&
  docker compose up -d &&
  docker compose wait bootstrap &&
  docker compose ps &&
  docker compose logs bootstrap
```

`SIMPLUS_DEVICE_GID` 必须等于宿主 ttyUSB 节点所属组的数字 GID。用
`stat -c '%g' /dev/ttyUSB0` 查询并将设备路径换成目标模组实际节点；Debian 默认
`dialout` 通常为 20，但应以查询结果为准。如果遗留 production 服务或开发 Agent 已
运行或启用，应先明确停止并禁用它们，再启动 Compose；宿主准备和检查脚本不会替用户
操作其他服务。

`data-init` 创建并固定：

```text
/opt/simplus/data/core   -> /var/lib/simplus
/opt/simplus/data/agent  -> /var/lib/simplus-agent
```

Compose 实际使用相对于其文件的 `./data/core` 和 `./data/agent`；上面的绝对路径来自本文
推荐的部署根。首次 `compose up` 前应确认该位置位于持久存储，并确定实例外、访问受限的
备份位置。

Agent 在这个私有目录下固定使用 `qdc507-sms/` 保存 QDC507 v2 SMS recovery ledger；container
entrypoint 通过 `--state-root /var/lib/simplus-agent/qdc507-sms` 传入。缺失/不安全目录、
schema 不兼容或 store/adapter 构造失败会阻止 Agent 启动，不能退化为不带恢复状态的
短信发送。该目录与 WAL/SHM 都是私有运行数据，不应复制进仓库。

全新数据目录还会获得 netd 镜像中固定并校验过摘要的 Mihomo core。后续重建不会覆盖
当前 core；管理员仍可在后台按既有的官方 release 校验流程升级。镜像所含 GPL-3.0
许可证位于 `/usr/share/doc/simplus/mihomo-LICENSE`，相同 Simplus tag 的 GitHub
Release 附带校验过的对应 Mihomo 源码归档。

`bootstrap` 在 app 健康后创建唯一管理员。首次日志显示用户名 `simplus_admin` 和随机
密码；后续重建只显示凭据未变化，不会覆盖或重新输出密码。保存密码并在首次登录后
修改。管理后台通过 `http://<host-lan-ip>:8080` 访问；Mihomo 运行后 Zashboard 使用
同一主机的 `19090` controller 和实例独立 secret。

production Web/API 与 controller 都监听所有 IPv4 接口。项目仍只面向可信 LAN；必须
用路由或宿主防火墙限制两个端口，不能因为有登录或 controller secret 就暴露公网。

## 生命周期

查看状态与日志：

```bash
cd /opt/simplus
docker compose ps
docker compose logs agent netd app
```

升级前停止业务写入并完整备份 `/opt/simplus/data` 到部署根之外的受限位置；备份必须
包含 `core`、`agent` 及 SQLite 的 WAL/SHM 文件，并在继续前验证可以读取。下载并校验
新版部署包，在临时目录解包；保留当前部署根的 `.env` 和 `data/`，只用新版归档中的
八个受管文件替换当前版本，然后拉取并重建容器：

```bash
new_bundle="/path/to/verified/simplus-compose-vX.Y.Z-linux-amd64"
(
  set -eu
  cd /opt/simplus
  for file in compose.yaml .env.example prepare-container-host.sh check-container-host.sh README.md VERSION LICENSE THIRD_PARTY_NOTICES.md; do
    install -m 0644 "$new_bundle/$file" "$file"
  done
  chmod 0755 prepare-container-host.sh check-container-host.sh
  bash check-container-host.sh "$PWD"
  docker compose config --quiet
  docker compose pull
  docker compose up -d
  docker compose wait bootstrap
)
```

上面的子 shell 会在任一步失败时停止；路径必须指向已经通过 SHA-256 校验的新归档，脚本
的最终执行权限以 `chmod` 恢复。
数据库迁移只保证向前进行。仅把 Compose 或镜像 tag 切回旧版本不构成安全降级；只有已
确认旧版本 schema 兼容，或恢复了升级前备份时，才可另行制定回退步骤。

停止并删除容器、bridge 和临时运行对象，同时保留 bind-mounted 数据：

```bash
docker compose down
```

`agent-runtime` 和 `netd-runtime` 只保存 Unix socket 与临时状态，不是备份数据。只有
确认不再需要实例且备份满足保留要求时才删除 `/opt/simplus/data`；不要把数据库、管理员
凭据、订阅或运行日志提交到仓库。

## 权限模型

- `app` 以 UID/GID 10001 运行，没有 Linux capability；
- `agent` 没有网络，root entrypoint 只注册 option ID 和准备目录，随后降到 UID 10002
  并清空 capability bounding set；QDC507 蜂窝短信由模组/SIM 承载，不给 Agent 容器增加宿主网络；
- `netd` 是唯一网络 owner，使用普通 Docker bridge 和固定 capability，不使用
  `privileged` 或 host network；
- per-Line netns、veth、nftables、TPROXY、strongSwan 与 XFRM 位于 netd 容器网络
  namespace，不应出现在宿主根 namespace；
- app、Agent 与 netd 继续通过共享 Unix socket、固定 UID 和 `SO_PEERCRED` 鉴权。

netd 启动时创建临时 namespace/veth/nft/XFRM 探针并立即清理。探针失败会阻止 netd
健康，app 和 bootstrap 也不会继续启动。该 preflight 不执行 RF、SIM AKA、VoWiFi、
短信或电话操作。

## 常见启动失败

- `option1/new_id` 不存在：重新运行宿主准备脚本，并确认当前内核提供 option 模块；
- GHCR 返回 denied/not found：确认该版本 Release 资产完整，并等待仓库所有者把三个
  package 全部改为 public；不要改用 `latest` 或生产机本地构建；
- Agent 不健康：检查 ttyUSB 的 GID、device cgroup、ModemManager 占用和 USB 枚举；
- netd preflight 失败：检查 rootful Docker、AppArmor 设置和宿主内核的 veth、nft
  TPROXY、XFRM 支持；不要改为 privileged/host network 掩盖问题；
- bootstrap 没有密码：先看 `docker compose ps`，只有 app 健康且数据库中尚无管理员时
  才会生成；
- Mihomo/VoWiFi 不可用：先确认预装或后台升级的 core 有效，并选择有效订阅/出口，再按
  [`troubleshooting.md`](troubleshooting.md) 的稳定状态检查。

## 遗留原生状态边界

仓库不再提供原生 Debian production bundle、安装器或卸载器。遗留 `/var/lib/simplus`
等私有状态不会被 Compose 读取、迁移或删除；需要恢复时必须先取得明确授权，并使用独立、
经过审查的恢复方案，不能把旧数据库或运行目录直接复制到 `./data`。

`check-container-host.sh` 仍会拒绝正在运行或已启用的遗留 `simplus-ml307a-bind`、
`simplus-agent`、`simplus-netd`、`simplusd` systemd 服务以及 `simplus-agent-dev`。这项
fail-closed 检查用于避免当前或重启后的资源争用，不表示原生 production 仍受支持。本机
Linux 开发和受限的 `simplus-agent-dev` HIL 工作流继续由
[`development.md`](development.md) 维护。
