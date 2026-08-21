# Implementation: GHCR release installation

## 1. Bundle source and contracts

- [x] 将源码 Compose 的 image tag 改为显式开发模板输入，同时保持五服务和全部权限合同不变。
- [x] 添加 bundle README、`.env.example` 与确定性 `build-container-release-bundle.sh`。
- [x] 添加 `container-release-bundle` Make target，并把新 shell 文件纳入静态语法检查。
- [x] 扩展 `internal/containercontract`：严格 tag/commit/epoch、归档文件白名单/模式、双次构建字节一致、checksum、字面 GHCR tag、无 build/latest/private state。

## 2. Release workflow

- [x] 重构 containers workflow metadata 和 trigger gate：仅严格版本 tag 可 push，PR/manual 只 build。
- [x] 将部署 bundle 并入对应源码资产阶段，以 GitHub Pre-release 先公开来源/安装资产。
- [x] 在三 target push 后收集 digest，生成并上传 `simplus-images-<tag>.json`。
- [x] 保持 action SHA pin、最小 job 权限、linux/amd64、SBOM/provenance、缓存和 source-before-image gate；同 tag 重跑复用匹配 digest，不移动既有版本 tag。
- [x] 更新 workflow path filters 覆盖 bundle source、builder、tests 和文档所有者。

## 3. Documentation and project contracts

- [x] README/installation 改为 Release 下载、checksum、解包、`.env`、宿主检查、pull/up；删除生产本地构建回退。
- [x] development 保留 `make container-build`/参数化 Compose，明确只作开发验证。
- [x] 更新 ADR 0021、公开状态/活跃计划中与 GHCR 首发相关的准确口径，不提升 clean-VM 证据。
- [x] 更新 core infra/docs Trellis spec，记录 bundle 作为正式安装接口、tag-only/pre-release 和 public visibility 首发门禁。

## 4. Verification and PR

- [x] 运行 focused bundle/container tests、`make container-config CONTAINER_IMAGE_TAG=dev` 和 Actions lint。
- [x] 运行 `make check-container-files`、`go test ./internal/containercontract`、`make check-docs`、`make lint`、`make test`、`make security`、`git diff --check`。
- [x] 对工作树、历史和生成 bundle 运行启用 redaction 的独立 secret scan；人工复核资产清单/许可证且不保存扫描输出。
- [ ] Trellis check 通过后更新规范、提交分支、推送并创建以 `main` 为 base 的 PR；等待 CI 成功后合并。

## 5. v0.1.0 Pre-release

- [ ] 对合并提交创建/推送不可移动的 `v0.1.0`，等待 containers workflow 完成。
- [ ] 验证 Pre-release 部署包/checksum、strongSwan/Mihomo 来源资产、三个 GHCR tag 和 digest manifest。
- [ ] 由仓库所有者把三个首次 package 改为 public；在无 GHCR 登录环境 inspect/pull 并核对 OCI metadata/digest。
- [ ] 解包到临时目录，运行 checksum、`docker compose config --quiet` 和 `docker compose pull`；不得执行 up、宿主准备或 HIL。
- [ ] 若需代码修复，不移动 v0.1.0；在 main 修复后发布 v0.1.1。

## Rollback points

- 发布 tag 前：PR 可安全修订；不产生 GHCR/Release 外部状态。
- Release 资产发布后：保持 Pre-release，并修复 workflow；不要覆盖内容不同的同名资产。
- tag 镜像发布后：tag/commit 不移动；瞬时失败只重跑，代码缺陷使用新 patch tag。
- 不以旧 Compose 覆盖已经向前迁移的数据作为通用回滚方式。

## Verification notes

- 使用官方 checksum 校验的 Gitleaks v8.28.0，并先以合成 canary 验证探测有效；工作树、
  完整历史、部署包、二进制包和 Simplus 包装源码均 clean。对应源码中的命中只位于固定
  strongSwan/Mihomo 上游公开测试/示例夹具，已按路径和固定来源人工复核；未保存报告。
- 本地忽略目录最初仍是 Go 1.26.5；`make doctor` 识别出与仓库锁定的 1.26.6 不一致，
  用校验和固定的 `make dev-toolchain` 刷新后，`make security` 报告可达漏洞 0、pnpm 漏洞 0。
- `SIMPLUS_DEB_VERSION=0.1.0-1` 的 strongSwan 包、对应源码、manifest 和 checksum 已完成
  本地隔离构建与校验；未执行宿主准备、Compose 启动或任何 HIL。
