# Simplus Docker Compose 部署包

本目录是 Simplus 的版本化 Debian 13/amd64 部署候选。它只使用已经写入
`compose.yaml` 的同版本 GHCR 镜像，不需要 Git、Go、Node、pnpm、Make 或本地镜像构建。
安装者只需从 Release 下载包含本目录的一个 `.tar.gz` 归档及其一个 `.sha256` 校验文件；
无需逐个下载源码包、digest manifest 或镜像归档，三个镜像由 `docker compose pull`
直接从 GHCR 拉取。
完整的支持边界和故障说明见
[项目安装文档](https://github.com/leonfox28/simplus/blob/main/docs/installation.md)。

## 安装

在下载目录先校验尚未解包的归档（将 `<version>` 替换为本 Release 版本）：

```bash
sha256sum -c "simplus-compose-<version>-linux-amd64.tar.gz.sha256" &&
  sudo mkdir -m 0755 /opt/simplus &&
  sudo chown "$(id -u):$(id -g)" /opt/simplus &&
  tar -xzf "simplus-compose-<version>-linux-amd64.tar.gz" \
    -C /opt/simplus --strip-components=1 &&
  cd /opt/simplus &&
  cp .env.example .env
```

命令链会在校验、解包或创建目录失败时停止。`/opt/simplus` 必须是这一实例专用的新目录；
已有目录必须按项目安装文档升级，不能用全新安装覆盖。检查并按需修改 `.env` 中的 HTTP
端口、controller 端口和 ttyUSB 数字 GID。可用
`stat -c '%g' /dev/ttyUSB0` 查询 GID，并将设备路径换成目标模组实际节点。生产镜像版本
由本部署包固定，`.env` 不提供镜像 tag 开关。

`compose.yaml` 和 `.env.example` 是运行核心；宿主准备/检查脚本、README、VERSION、
LICENSE 和 THIRD_PARTY_NOTICES 随同归档提供，以保持同版本的安全检查、来源标识和
合规材料，不是额外安装包。

持久数据将写入 `/opt/simplus/data/core` 和 `/opt/simplus/data/agent`。首次启动前确认
该目录位于持久存储，并确定实例外、访问受限的备份位置。

宿主准备会修改模块加载配置并执行 `modprobe option`，请先审阅脚本，再显式运行：

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

`bootstrap` 只在全新实例首次启动时输出随机管理员密码。立即保存并在首次登录后修改。
已有实例重建时不会覆盖密码或再次输出原密码。Web 和 controller 只能暴露给可信局域网。

升级前停止写入并完整备份 `/opt/simplus/data`，再下载和校验新版部署包、保留自己的
`.env` 与 `data/`，使用新版受管文件执行 `pull` 和 `up -d`。数据库只保证向前迁移；
切回旧镜像不构成安全降级，除非已确认 schema 兼容或恢复了备份。

停止服务并保留数据：

```bash
docker compose down
```

本文件的源码修改只会进入未来生成的部署包，不会改写已经发布且不可移动的 Release 资产。
