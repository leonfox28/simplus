# 切换本地生产容器并保留持久数据

## Goal

将当前本机运行的 Simplus 源码 Compose `:dev` 容器切换为 GitHub Release `v0.1.1`
部署包与 GHCR 版本化镜像；新容器使用停机时取得的完整持久数据副本，原数据与独立备份
不被迁移命令覆盖或删除，以便在验收窗口内可靠回滚。

## Background

- `v0.1.1` 是首个资产、三张 `linux/amd64` 镜像和 digest manifest 完整且可匿名拉取的
  Pre-release；不完整的 `v0.1.0` 不得使用。
- 只读盘点确认宿主为 Debian 13/amd64、rootful Docker，Docker Compose 版本满足要求；
  固定 `option1/new_id` 可用，ModemManager 与遗留 production units 均未运行，开发
  Agent 已停用。
- 当前 `simplus` 项目由源码 `compose.yaml` 启动，`app`、`agent`、`netd` 健康，
  `data-init` 与 `bootstrap` 已正常退出，但三张镜像仍为旧 revision 的 `:dev` tag。
- 实际持久 bind mount 是 `<repo>/data/core` 与 `<repo>/data/agent`；目录分别为
  UID 10001/10002、mode 0700，未发现 symlink，总状态约 51 MiB。当前根文件系统的空间
  足以同时保留原目录、生产副本和迁移备份。
- 数据库升级只保证向前迁移；因此不能让 `v0.1.1` 直接修改唯一一份旧数据后再假设旧
  `:dev` 镜像可以安全接管。

## Requirements

- 生产部署根固定为 `/opt/simplus`，包含校验过的 Release 受管文件、只含三项允许参数的
  `.env`，以及从停机快照恢复出的 `data/core` 和 `data/agent`。
- 迁移备份固定放在 root-only 的 `/var/backups/simplus/<cutover-id>`；原
  `<repo>/data` 不得被迁移命令覆盖或删除；安全回滚重新启动旧服务时允许它产生正常运行
  写入，此后的重试必须重新停机取快照。两者均不得进入 Git。
- 生产镜像只使用 `ghcr.io/leonfox28/simplus-{control,agent,netd}:v0.1.1`，不得使用
  本地 `build`、`:dev`、`latest` 或环境变量镜像 tag。
- 保留当前安装参数：HTTP 端口 8080、controller 端口 19090、ttyUSB 数字 GID 20。
- 下载、checksum/资产白名单、Compose 渲染、镜像 digest 和宿主检查必须在停机前完成；
  任一门禁失败时不得中断当前服务。
- 停机后使用保留 owner、mode、ACL/xattr 和稀疏文件语义的完整归档取得一致快照；验证
  archive、checksum 及恢复树后，才允许启动生产容器。不得只复制数据库单文件。
- 切换只使用普通 `docker compose down`，不得使用 `down -v`、删除 runtime volume、
  删除原数据或清理备份。
- 启动仅授权生产 Compose 的标准副作用：写入固定 module-load 配置、注册已审查的 USB
  serial ID、创建临时 Docker/netd 网络对象，并按已有持久状态恢复服务意图。不得执行
  人工 RF 更改、短信、电话、SIM/eUICC mutation、模组持久写入或新的 HIL 动作。
- 验收期间不进行用户写入；若生产服务未健康，先停止生产 Compose，再从未被迁移命令
  覆盖的源码数据启动旧 `:dev` Compose。生产副本已经接收新写入后不得静默回退。
- 全过程不得读取、打印、提交或发布数据库内容、管理员凭据、设备/SIM 身份、私有端点、
  网络拓扑或原始容器/HIL 日志。

## Acceptance Criteria

- [x] 停机前门禁确认 `v0.1.1` 归档 checksum、严格文件清单、VERSION、三张固定镜像、
      digest manifest、Compose config/pull 和宿主检查全部通过。
- [x] 已以私有权限生成完整迁移归档及 SHA-256，并验证归档可读；原 `<repo>/data` 未被
      覆盖或删除，最终生产副本来自最后一次停机后的最新状态。
- [x] `/opt/simplus/data` 从同一停机归档恢复，文件内容和元数据比较通过，且不存在 symlink。
- [x] 新 Compose 的三个业务容器均健康，两个 one-shot 容器成功退出；未输出 bootstrap
      或业务原始日志。
- [x] 运行容器实际使用 `v0.1.1` 镜像/digest，`core` 与 `agent` mount source 分别是
      `/opt/simplus/data/core` 与 `/opt/simplus/data/agent`。
- [x] 生产 Compose 二次渲染仍通过，现有管理员未被覆盖，旧源码 Compose 未运行且源码
      `compose.yaml`/`.env` 未被改写。
- [x] 原数据、私有备份、旧镜像标识和明确的回滚命令均保留；任务不执行清理。
- [x] Git 记录只包含脱敏任务/会话信息，不包含 Release 下载物、数据、备份或私有运行证据。

## Out of Scope

- 迁移遗留原生 `/var/lib/simplus`、systemd production 状态或其他主机的数据。
- 修改 Web/API、数据库 schema、容器权限、UID、挂载合同、宿主 Docker daemon 或防火墙。
- 真实短信、电话、RF/SIM/eUICC 操作、业务功能验收或新的 HIL/clean-VM 取证。
- 删除旧源码数据、迁移备份、旧镜像或 runtime volumes；这些只能在稳定运行后另行授权。
- 同盘迁移备份不作为磁盘故障或主机灾难恢复方案。
