# Docker Compose 部署与原生过渡

## 当前状态

Simplus 的目标生产形态是 Docker Compose，并保持 `simplusd`、`simplus-agent`、
`simplus-netd` 三个权限边界。容器定义、镜像构建、宿主准备、数据初始化、typed
healthcheck 和发布 workflow 已进入仓库；开发 VM 已完成真实硬件容器业务 HIL，但
clean Debian VM 生命周期尚未验收，因此当前仍属于可审查的部署候选。原生 systemd
安装路径在 clean-VM 验收前继续保留，
但两套运行态不得同时占用模组或管理端口。

三个 production target 和隔离无硬件 Compose 已在 Debian 13/amd64 开发 VM 完成
smoke；该证据验证镜像、权限、netd preflight、bootstrap、Web 登录和数据保留重建，
不等于 clean-VM 或真实模组验收，也没有执行 RF、VoWiFi、短信或电话动作。
随后在同一开发 VM 的正式 Compose 切换中，已完成预期模组发现、Mihomo 国家出口、
Host VoWiFi 注册和单段自号码短信回环 HIL；该证据没有请求 RF 写入，且不能外推为
其他收件人互通、电话或 clean-VM 生命周期证据。

容器部署创建全新实例，数据位于 Compose 文件旁的 `./data`，不会读取或迁移原生
`/var/lib/simplus`。需要保留旧数据时先留在原生安装中，不要手工复制数据库或运行
目录到容器。

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

## 一次性宿主准备

宿主必须在每次开机时加载 `option`，但不在宿主写动态 USB ID。仓库提供的脚本只创建
模块加载配置并执行本次 `modprobe`：

```bash
sudo bash scripts/release/prepare-container-host.sh
bash scripts/release/check-container-host.sh "$PWD"
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
会 fail closed。

## 启动全新实例

正式部署使用一个已经发布的固定 tag。仓库模板默认 `v0.1.0`；其他版本通过环境变量
覆盖，不要使用 `latest`。在首个 tag 尚未发布的开发验收阶段，先执行
`make container-build CONTAINER_IMAGE_TAG=dev`，并在以下命令中使用 `dev`；这不等于
正式发布证据。

```bash
export SIMPLUS_IMAGE_TAG=v0.1.0
export SIMPLUS_HTTP_PORT=8080
export SIMPLUS_CONTROLLER_PORT=19090
export SIMPLUS_DEVICE_GID=20
docker compose pull
docker compose up -d
docker compose ps
docker compose logs bootstrap
```

`SIMPLUS_DEVICE_GID` 必须等于宿主 ttyUSB 节点所属组的数字 GID；Debian 默认
`dialout` 为 20。如果原生 production 服务或开发 Agent 已运行，应先明确停止它们，
再启动 Compose；安装流程不会替用户操作宿主服务。

`data-init` 创建并固定：

```text
./data/core   -> /var/lib/simplus
./data/agent  -> /var/lib/simplus-agent
```

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
docker compose ps
docker compose logs agent netd app
```

更新到另一个已发布固定 tag：

```bash
export SIMPLUS_IMAGE_TAG=<published-version>
docker compose pull
docker compose up -d
```

停止并删除容器、bridge 和临时运行对象，同时保留 bind-mounted 数据：

```bash
docker compose down
```

`agent-runtime` 和 `netd-runtime` 只保存 Unix socket 与临时状态，不是备份数据。只有
确认不再需要实例时才删除 `./data`；不要把数据库、管理员凭据、订阅或运行日志提交
到仓库。

## 权限模型

- `app` 以 UID/GID 10001 运行，没有 Linux capability；
- `agent` 没有网络，root entrypoint 只注册 option ID 和准备目录，随后降到 UID 10002
  并清空 capability bounding set；
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
- Agent 不健康：检查 ttyUSB 的 GID、device cgroup、ModemManager 占用和 USB 枚举；
- netd preflight 失败：检查 rootful Docker、AppArmor 设置和宿主内核的 veth、nft
  TPROXY、XFRM 支持；不要改为 privileged/host network 掩盖问题；
- bootstrap 没有密码：先看 `docker compose ps`，只有 app 健康且数据库中尚无管理员时
  才会生成；
- Mihomo/VoWiFi 不可用：先确认预装或后台升级的 core 有效，并选择有效订阅/出口，再按
  [`troubleshooting.md`](troubleshooting.md) 的稳定状态检查。

## 原生安装过渡

容器 HIL 验收前，现有 Debian bundle 仍可作为回退：

```bash
scripts/release/build-debian-bundle.sh "$PWD/.dev/release/debian"
sudo .dev/release/debian/install-debian.sh
```

它安装三个 systemd 服务和由 `dpkg` 管理的 `simplus-strongswan-plugins`。该路径不会
与容器共享数据；切换运行方式前必须先停止另一套 Agent/netd/app。容器验收完成后，
原生 production 安装器将退出默认发布，本机开发用的受限 `simplus-agent-dev` 不受影响。
