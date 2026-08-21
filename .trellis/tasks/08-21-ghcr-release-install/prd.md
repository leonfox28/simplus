# GHCR release installation

## Goal

让 Debian 13/amd64 管理员通过版本化 GitHub Release 部署包安装 Simplus，直接匿名拉取由 GitHub Actions 构建并发布到 GHCR 的三个生产镜像，不再在生产机克隆源码或本地构建镜像。

## Background

- Docker Compose 已是唯一受支持的生产部署方式，现有 `control`、`agent`、`netd` 三镜像及 UID、capability、mount、socket 和网络隔离契约必须保持不变。
- `compose.yaml` 已引用 `ghcr.io/leonfox28/simplus-{control,agent,netd}`，现有 tag workflow 具备构建/推送骨架，但远端尚无 Git tag、GitHub Release 或已验证的公开 GHCR 版本。
- 当前安装文档仍要求在源码 checkout 中取得 Compose/宿主脚本，并保留首发前本地构建回退；这不是目标生产安装接口。
- clean Debian VM 的完整安装、升级、停止和卸载生命周期仍未验收；首发不得提升为稳定 Runtime 结论。

## Release attempt outcome

PR #5 在全部检查成功后以 `d7aaa34` 合并，并创建了不可移动的 `v0.1.0`。部署包和对应
源码资产成功发布；随后三个镜像 job 因 registry HEAD 探测的 curl 语义错误，在任何
镜像构建/推送前失败，digest manifest 未生成。按既定失败处理，不移动、重跑或补写
`v0.1.0`；代码修复后以 `v0.1.1` 完成剩余验收。

## Requirements

### R1 — Release bundle installation

- 生产安装只依赖 Docker Engine、Compose、下载/校验/解包工具及版本化部署包，不依赖 Git、Go、Node、pnpm、Make 或本地 `docker build`。
- 每个版本资产名固定为 `simplus-compose-<tag>-linux-amd64.tar.gz` 及同名 `.sha256`；
  `v0.1.0` 已生成并保留，最终候选改用对应的 `v0.1.1` 资产。
- 解包目录只包含：写死同版本镜像 tag 的 `compose.yaml`、`.env.example`、宿主准备和检查脚本、简明安装说明、`VERSION`、`LICENSE`、`THIRD_PARTY_NOTICES.md`。
- `.env.example` 只暴露 `SIMPLUS_HTTP_PORT`、`SIMPLUS_CONTROLLER_PORT`、`SIMPLUS_DEVICE_GID`；生产用户不通过环境变量切换镜像版本。

### R2 — Versioned GHCR publication

- 只允许严格 `vX.Y.Z` Git tag 发布 `linux/amd64` 镜像；PR 和 `workflow_dispatch` 只构建验证，不推送。
- 不发布或引用 `latest`、`main` 或分支滚动标签。
- 正式引用为 `ghcr.io/leonfox28/simplus-{control,agent,netd}:<tag>`；`v0.1.0` 是失败关闭的
  首次尝试，首个完整候选改为 `v0.1.1`。
- 继续保留 OCI source/version/revision/license 标签、SBOM、provenance 和按 target 分离的缓存。

### R3 — Release ordering and evidence

- strongSwan 包/对应源码、Mihomo 对应源码和部署包必须先作为同 tag GitHub Pre-release 资产公开，随后才允许推送镜像。
- 三镜像成功后发布 `simplus-images-<tag>.json`，记录版本、Git commit、`linux/amd64`
  平台、完整 tag reference 和实际 `sha256` digest；本任务的完整清单应为
  `simplus-images-v0.1.1.json`。
- 任一来源、资产或镜像步骤失败时不得把 GitHub Release 标记为稳定可用。
- GitHub 首次生成的三个 private package 由仓库所有者显式改为 public；完成前不得宣告匿名安装可用。

### R4 — Documentation and lifecycle

- README 和 `docs/installation.md` 以 Release 下载、SHA-256 校验、解包、宿主检查、`docker compose pull/up` 为唯一生产安装路径。
- `make container-build` 和参数化源码 Compose 只保留在 `docs/development.md` 作为开发验证入口。
- 升级通过用新版本部署包替换受管文件并 pull/up；不得承诺仅切回旧镜像即可安全降级数据库。
- 更新 ADR 0021、相关 Trellis infra/docs 规范和必要的公开状态摘要，明确 `v0.1.0` 是
  不完整 Pre-release、`v0.1.1` 仍只是部署候选。

### R5 — Safety and publication

- 不改变业务 API、数据库 schema、Compose 服务拓扑、固定 UID、capability、mount、端口、socket 鉴权或硬件能力边界。
- 本任务不运行宿主准备、`docker compose up`、真实设备部署、RF/SIM/短信/电话/VoWiFi 操作或 HIL。
- 发布包采用明确文件白名单和确定性归档，不能包含源码、`.git`、`.dev`、`.tools`、`.env`、运行数据、日志、凭据或私人证据。
- 发布前按项目公开规范完成文档检查、启用 redaction 的 secret scan，以及许可证和最终资产清单人工复核。

## Acceptance Criteria

- [x] `make container-release-bundle RELEASE_TAG=v0.1.0` 生成规定名称、内容、权限、确定性 tar.gz 和有效 SHA-256 文件。
- [x] 发布包中的 Compose 只含三张 `:v0.1.0` GHCR 镜像、没有 `build`/`latest`，并可在解包目录用 `.env` 成功渲染。
- [x] `v0.1.1` 替代部署包也通过 checksum、解包和 Compose 渲染合同。
- [x] 合同测试拒绝非法 tag、额外/私有文件、错误权限、非确定性输出、未固定镜像或缺失许可证。
- [x] PR Actions 成功构建三个 target 但不推送；只有严格版本 tag 进入来源资产 → Pre-release → GHCR → image manifest 流程。
- [x] `make check-container-files`、`go test ./internal/containercontract`、`make check-docs`、`make lint`、`make test`、`make security` 和 `git diff --check` 通过。
- [x] 功能分支通过 PR #5 合并；`v0.1.0` workflow 成功生成 Pre-release/部署/源码资产，并在任何镜像推送前失败关闭，旧 tag 保持不可移动且不完整。
- [ ] 修复分支通过 PR/CI 合并后，新合并提交的 `v0.1.1` workflow 成功生成完整 Pre-release、部署资产、源码资产、三个 GHCR 镜像和 digest manifest。
- [ ] 三个 package 经所有者改为 public 后，无 GHCR 登录也能 inspect/pull `v0.1.1`，且 digest/OCI 元数据与 `simplus-images-v0.1.1.json` 一致。
- [ ] 发布后仅执行解包、Compose config 和 pull 验证；不启动服务或接触真实硬件。

## Out of Scope

- clean-VM 生命周期验收或把发布提升为稳定版；
- ARM64、其他发行版、Docker Desktop、Podman、rootless/userns 或 SELinux 支持；
- `latest`/main/nightly 发布通道、镜像签名平台或自动更新系统；
- 自动备份、数据库 downgrade、旧原生数据迁移或真实部署/HIL。
