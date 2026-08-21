# 实施结果

## 文档变更

- 根 README 采用渐进披露，并提供从 `v0.1.1` 校验到 `/opt/simplus` 启动的连续入口。
- 全新安装命令在下载、校验、目标目录创建或宿主/Compose 检查失败时停止；已有
  `/opt/simplus` 不会被全新安装覆盖，也不再递归改写目录属主。部署包 README 直接把
  已校验归档解压到新建目标，不会从可能复用的解包目录复制额外文件。
- 安装文档成为持久目录、数据、备份、升级、回滚和遗留状态边界的完整来源。
- 未来部署包的 README 与上述路径、三项环境变量、首次密码和数据安全语义保持一致；
  已发布的 `v0.1.1` 资产未被修改。
- 三处安装入口明确普通用户只下载一个版本化 `.tar.gz` 和一个 SHA-256 文件；三个镜像
  由 Compose 拉取，归档内其余脚本、版本和许可文件用于同版本安全检查与合规，不是额外
  安装包。启动命令等待 `bootstrap` 成功退出后才显示状态和首次凭据日志。
- 兼容性与交接只记录开发宿主上的脱敏生产形态切换结论，未提升 clean-VM Runtime。
- 现有容器发布规范同步记录全新安装拒绝已有部署根、非递归属主修改和失败即停合同。

## 验证

- `make check-docs`
- `make check-container-files`
- `go test -count=1 ./internal/containercontract`
- `make lint`
- `umask 0022 && CI=true make test`
- `git diff --check`

以上检查均通过。完整测试包含 Go、19 个 Vitest 文件/70 项测试、Web typecheck、worktree
manifest 与 Simulator supervisor。未修改脚本、Compose、Release 资产、容器或运行时数据。
