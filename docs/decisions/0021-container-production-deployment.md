# 0021：生产部署迁移到 Docker Compose

- 状态：Accepted
- 日期：2026-08-06

## 实施状态（2026-08-14）

第 8 项约定的容器业务 HIL 条件已经满足，仓库已退出原生 Debian production bundle、
安装器、卸载器与型号专用驱动绑定脚本；Docker Compose 现为唯一受支持的 production
部署方式。本次退出不提升证据等级：clean Debian VM 的完整安装、升级、停止和卸载生命
周期仍待验收，不能沿用旧原生 systemd 的 Runtime 结论。遗留私有 `/var/lib` 状态不被
读取、迁移或删除；本机原生 Go/Node 开发、受限 `simplus-agent-dev` HIL 和 QDC507 原生
蜂窝短信含义均不受影响。

## 背景

原生 Debian bundle 会在宿主安装 Simplus 二进制、三个 systemd 服务、strongSwan
运行包、自有插件、iproute2、nftables 和静态 Web 资源。虽然进程权限已经分离，用户
仍需协调发行版包、插件 ABI、服务 unit、设备组和升级顺序。项目只面向一台用户管理
的 Linux 主机和可信 LAN，没有必要让这些 userspace 依赖散落在宿主。

硬件和 Host VoWiFi 不能成为普通无权限容器：Agent 需要动态出现的 USB tty，netd
需要网络 namespace、veth、nftables、TPROXY 和 XFRM。把整个应用放入单个
`privileged`/host-network 容器会破坏现有的硬件与网络权限边界，也会让 Web 进程
获得不必要能力。

开发机已经有仓库锁定的 Go、Node 和 pnpm 工具链。生产容器化不构成把日常编码、
Simulator 或单元测试搬进开发容器的理由。

## 决定

1. 首个容器目标只支持 Debian 13/amd64、Docker Compose 2.24+ 和没有 rootless/
   userns-remap 的 rootful Docker Engine。生产保持三个镜像：无 capability 的
   `simplus-control`、无网络的 `simplus-agent`，以及单独持有受限网络 capability
   的 `simplus-netd`；不得合并为 privileged 单体或使用 host network。
2. 宿主只负责 Docker、内核和开机加载 `option` 模块。管理员一次性写入
   `/etc/modules-load.d/simplus.conf`；Compose 只读映射 USB sysfs 扫描树及其
   `/sys/devices` 符号链接目标、映射 `/dev`，并只把 `option1/new_id` 作为精确 sysfs
   写点交给 Agent。动态 ID 来自 Adapter registry，
   目前仅有经过验证的 ML307A `2ecc:3012`，不能从 Compose 或 Web 输入任意 ID。
3. Agent entrypoint 只以 root 完成运行目录准备和 `new_id` 注册，随后清空 capability
   bounding set 并降到固定 UID 10002。业务进程固定 UID 10001。跨容器调用继续使用
   Unix socket、固定数字 UID 和 `SO_PEERCRED`；因此首版不支持 Docker userns。
4. netd 使用普通 Docker bridge。per-Line netns、veth、路由、nftables、TPROXY、
   strongSwan 和 XFRM 全部位于 netd 容器的网络 namespace；启动前用临时对象验证
   capability 和内核支持，失败即不进入健康状态。Web/API 容器不获得网络管理权限。
5. 持久数据使用部署目录的 `./data/core` 和 `./data/agent`，不复用或自动迁移原生
   `/var/lib` 数据。一次性 data-init 固定所有权、私有权限并安装随镜像发布的
   Zashboard，并只给全新实例安装镜像固定、摘要验证的 Mihomo core；已有 core 和
   选择不被覆盖。bootstrap 在 app 健康后幂等创建 `simplus_admin`，初始随机密码只在
   首次 bootstrap 容器日志出现。
6. 不替用户停止 ModemManager，也不向宿主写 udev 忽略规则。其他进程争用 tty 时
   Agent fail closed，并给出检查方向；避免容器隐式改变宿主其他蜂窝设备的管理策略。
7. 日常开发和 CI 的 Go/Web 验证继续直接使用本机或 GitHub runner 工具链。Docker
   只用于生产镜像构建、Compose contract、clean-VM smoke 和最终容器 HIL；受限的
   `simplus-agent-dev` systemd 工作流继续服务本机硬件开发。
8. 容器 HIL 通过前保留原生 production 安装器作为回退，但二者不得同时占用端口或
   模组。验收后 Compose 成为唯一正式部署方式，原生生产安装脚本退出默认发布；不
   删除本地开发 Agent 工作流。

## 后果

- 宿主无需安装 Simplus、Mihomo、strongSwan 或网络 userspace 包，但仍必须具备所需
  内核能力，并允许 rootful Docker 访问精确设备和 sysfs 写点；
- netd 仍是高权限组件，但权限和网络对象被限制在独立容器，app 与 Agent 不随之提权；
- 固定 UID、设备 major 和 Debian 运行 ABI 成为首版容器兼容性的一部分，新增架构、
  rootless/Podman、SELinux 或新设备类型都需要独立设计与证据；
- Compose 文件和镜像标签成为正式安装接口，业务 API、Line、模组、短信和 VoWiFi
  契约不因部署方式改变；
- netd 镜像中的 Mihomo 版本和压缩/展开摘要是受审查的发布输入；对应 GPL-3.0 源码
  归档必须在发布镜像前验证，并随相同 tag 的 GitHub Release 提供；
- 在 clean VM 和真实硬件完成容器验收前，只能声明实现与自动化 contract 证据，不能
  沿用原生 systemd 的 Runtime/HIL 结论。
