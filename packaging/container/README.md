# Simplus Docker Compose 部署包

本目录是 Simplus 的版本化 Debian 13/amd64 部署候选。它只使用已经写入
`compose.yaml` 的同版本 GHCR 镜像，不需要 Git、Go、Node、pnpm、Make 或本地镜像构建。
完整的支持边界和故障说明见
[项目安装文档](https://github.com/leonfox28/simplus/blob/main/docs/installation.md)。

## 安装

在下载目录先校验尚未解包的归档（将 `<version>` 替换为本 Release 版本）：

```bash
sha256sum -c "simplus-compose-<version>-linux-amd64.tar.gz.sha256"
tar -xzf "simplus-compose-<version>-linux-amd64.tar.gz"
cd "simplus-compose-<version>-linux-amd64"
cp .env.example .env
```

检查并按需修改 `.env` 中的 HTTP 端口、controller 端口和 ttyUSB 数字 GID。生产镜像
版本由本部署包固定，`.env` 不提供镜像 tag 开关。

宿主准备会修改模块加载配置并执行 `modprobe option`，请先审阅脚本，再显式运行：

```bash
sudo bash prepare-container-host.sh
bash check-container-host.sh "$PWD"
docker compose config --quiet
docker compose pull
docker compose up -d
docker compose ps
docker compose logs bootstrap
```

`bootstrap` 只在全新实例首次启动时输出随机管理员密码。立即保存并在首次登录后修改。
Web 和 controller 只能暴露给可信局域网。

持久数据位于本目录的 `./data/core` 和 `./data/agent`。升级前备份 `./data`，再下载和
校验新版部署包、保留自己的 `.env` 与 `data/`，使用新版受管文件执行 `pull` 和 `up -d`。
数据库只保证向前迁移；切回旧镜像不构成安全降级，除非已确认 schema 兼容或恢复了备份。

停止服务并保留数据：

```bash
docker compose down
```
