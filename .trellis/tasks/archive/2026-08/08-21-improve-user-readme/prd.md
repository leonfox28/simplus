# 优化用户 README 与部署说明

## Goal

让首次接触 Simplus 的用户能够快速判断项目是否适合自己，并在受支持宿主上从版本化
Release 安全安装到持久部署目录、找到首次登录入口、理解数据/升级边界；同时以正确证据
等级记录已完成的本机生产形态切换，不夸大为 clean-VM Runtime。

## Background

- 根 `README.md` 的技术事实、内部链接和 `v0.1.1` Release 命令当前有效，
  `make check-docs` 已通过。
- 当前入口把快速安装、`v0.1.0` 失败史、GHCR visibility 和发布工程细节混在一起；
  用户按现有命令直接在解压目录启动时，持久 `./data` 可能留在下载/临时目录。
- 顶部“请勿用于生产”与后文“唯一受支持的生产部署方式”容易被理解为冲突；准确含义是
  Docker Compose 是唯一正式部署形态，但 `v0.1.1` 仍是只适合受控评估的 Pre-release
  部署候选。
- 开头列出电话、数字音频和 eUICC 管理目标，但真实硬件能力尚未全部开放；README 必须
  区分 Simulator/Fixture 与已经进入 production Agent 的能力。
- 本机已使用校验过的 `v0.1.1` Release/GHCR 生产形态完成现有数据的停机快照、恢复、
  回滚演练、最终切换以及 health/digest/mount 验收。该结论可公开脱敏记录，但宿主不是
  clean VM，不能据此提升 clean-VM 生命周期或稳定版本声明。

## Requirements

- 根 README 使用渐进披露：简短说明项目、成熟度、支持边界和当前能力，然后提供一个
  可复制的快速开始；发布失败史、package visibility 和完整生命周期留给规范文档。
- 快速开始必须先让用户校验 Release 归档，再安装到明确的持久目录
  `/opt/simplus`，避免从下载目录长期运行；数据路径在 `compose up` 之前说明。
- 安装命令只使用公开 `v0.1.1` 部署包和写死版本的 GHCR 镜像，不使用 Git checkout、
  本地 build、`latest` 或生产镜像 tag 环境变量。
- 安装入口明确普通用户只下载一个版本化部署归档和一个 SHA-256 文件；说明归档内
  Compose/环境模板是运行核心，其余脚本、版本和许可材料是同版本安全与合规附件，避免
  把 Release 的源码、包和 digest 资产误解为多套必装归档。
- 用户能直接检查 Debian 13/amd64、Docker/Compose 版本和 ttyUSB 数字 GID；仍明确
  rootful/no-userns、可信 LAN 和不能暴露公网的边界。
- 首次登录说明包含 `bootstrap` 的一次性随机密码语义、默认 Web 地址，以及已有实例不会
  重新输出/覆盖密码的含义；不得把真实凭据或日志写入文档。
- 根 README 只保留最小数据安全提醒，并链接 canonical `docs/installation.md` 获取升级、
  备份、回滚、权限和故障处理；文档链接名改为“生产部署与生命周期”。
- `docs/installation.md` 成为持久部署目录与升级命令的唯一完整来源；
  `packaging/container/README.md` 与其保持一致，但源修改只影响未来 Release，不能改写已
  发布的不可移动 `v0.1.1` 资产。
- `docs/compatibility.md` 与 `docs/handoff.zh-CN.md` 仅增加脱敏结论：开发宿主已完成
  `v0.1.1` Release 生产形态的有数据切换和回滚/验收；clean-VM Runtime 仍待完成。
- 不新增产品能力、部署平台、硬件授权、稳定性承诺、架构决定或截图。

## Acceptance Criteria

- [x] 新用户从根 README 前两屏能回答：项目用途、当前成熟度、支持宿主、当前可安装版本、
      真实已开放能力和关键未验证边界。
- [x] 快速开始从下载校验到 `/opt/simplus` 启动形成连续命令，启动前明确三项 `.env`
      参数、数据目录和备份责任，不从临时解压目录长期运行。
- [x] `README.md` 不再展开 `v0.1.0` 失败时间线或 package visibility 操作细节，而是链接
      canonical 安装/兼容性文档。
- [x] 根 README、安装文档和部署包 README 的命令、版本、路径、镜像/数据边界一致，且
      不暗示修改已发布的 `v0.1.1` Release 资产。
- [x] 三处安装入口都明确只需一个部署归档和一个校验文件，GHCR 镜像由 Compose 拉取，
      归档内辅助文件不构成额外下载项。
- [x] 本机验证结论明确限定为开发宿主上的生产形态切换，不声称 clean-VM Runtime、稳定版
      或其他硬件/运营商兼容性。
- [x] 所有本地 Markdown 链接有效；`make check-docs`、`make check-container-files`、
      `go test ./internal/containercontract` 和 `git diff --check` 通过。
- [x] 公开文本不包含真实主机路径、地址、用户名、数据、凭据、设备/SIM 身份、日志或
      排障时间线。

## Out of Scope

- 重新发布、替换或补写 `v0.1.1` Release/GHCR 资产。
- 把当前宿主声明为 clean VM，或把部署候选提升为稳定生产版本。
- 修改安装脚本、Compose、镜像、运行时数据、权限、端口、数据库或容器状态。
- 添加截图、真实日志、现场拓扑或逐次部署记录。
