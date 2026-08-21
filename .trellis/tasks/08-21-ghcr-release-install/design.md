# Design: GHCR release installation

## Architecture

仓库根 `compose.yaml` 继续作为权限/服务拓扑的唯一源码模板，但镜像 tag 改为必须显式提供的开发模板变量，避免源码 checkout 默认伪装成某个已发布生产版本。Release 构建器只替换这一个受控占位符，生成写死 `vX.Y.Z` 的发行 Compose；端口和设备 GID 仍由用户 `.env` 提供。

生产数据继续相对发行 Compose 位于 `./data/core` 和 `./data/agent`。部署包更新只覆盖受管 Compose、脚本、示例和文档，不包含或删除 `.env`、`data/` 或运行 volume。

## Deployment bundle contract

新增 `scripts/release/build-container-release-bundle.sh`，接口为：

```text
build-container-release-bundle.sh <vX.Y.Z> <40-hex-commit> <source-date-epoch> <output-directory>
```

脚本必须：

1. 严格验证 tag、commit、epoch 和既有输出目录；
2. 在私有临时目录创建 `simplus-compose-<tag>-linux-amd64/`；
3. 将源码 Compose 的唯一 image-tag 占位符替换为字面版本 tag，并断言五个服务只引用预期三张 GHCR 镜像；
4. 复制 `.env.example`、两条宿主脚本、bundle README、VERSION、根许可证和 notices，固定脚本为 `0755`、其余为 `0644`；
5. 使用排序条目、固定 mtime、numeric owner/group 0 和 `gzip -n` 生成确定性归档；
6. 在输出目录写入归档及只引用其 basename 的 `.sha256` 文件。

bundle README 仅给出 SHA 校验后的显式命令，不提供 `curl | sh` 或隐式 sudo。`.env.example` 不含秘密或镜像版本，升级时用户保留自己的 `.env`。

## GitHub Actions data flow

1. `metadata/compose-contract` 对所有触发器运行；tag ref 必须匹配 `^v[0-9]+\.[0-9]+\.[0-9]+$`。
2. PR/手动触发用合成 CI metadata 构建部署包和三个 target，`push: false`。
3. tag 触发先构建 strongSwan/Mihomo 对应源码和部署包，并上传临时 Actions artifacts。
4. 发布任务创建或更新同 tag GitHub **Pre-release**，上传部署包、checksum 和全部对应源码；版本化资产存在时只接受内容相同的幂等重跑，不静默覆盖不同内容。
5. 镜像矩阵在来源资产发布成功后登录 GHCR。同 ref workflow 串行执行；每个 target 若已
   存在同 tag，只在 commit、`linux/amd64` 和 OCI 标签完全匹配时复用其 digest；若不存在，
   先用 BuildKit 按 digest 推送无 tag 内容，再次确认版本 tag 不存在后用 `imagetools create`
   提升，并核对提升后的 digest 与 staged digest 完全相同。每个矩阵项把最终 digest 写成
   小型 artifact。
6. 汇总任务验证三个 target/digest 唯一且完整，生成稳定排序的 `simplus-images-<tag>.json` 并上传到同一 Pre-release。

`packages: write` 只授予推送镜像的 job，`contents: write` 只授予发布 Release 资产的 job。PR 来源永不获得发布路径。保留现有 action commit pin、BuildKit cache、SBOM 和 provenance。

## First release and visibility

代码经 PR #5 合并后，给合并提交创建了不可移动的 `v0.1.0`。部署/来源资产成功进入
Pre-release，但两个 registry manifest HEAD 调用错误使用 `curl --request HEAD`，三个
矩阵项均在任何镜像构建或推送前以 exit 18 失败，digest manifest 未生成。该 tag 和
Pre-release 保持不完整，不重跑或补写；修复合并后用新 tag `v0.1.1` 完成首发。

GitHub 首次创建的个人 GHCR package 默认 private，workflow 无法替代所有者的不可逆
visibility 决策；`v0.1.1` 三个镜像完成后由所有者逐个改为 public，再从未登录 registry
的环境执行 inspect/pull 验证。

基础设施瞬时错误可对同一 tag/commit 重跑。此次失败需要改代码，所以不移动或复用
`v0.1.0`，而是在 main 修复后使用 `v0.1.1`。首发保持 Pre-release，clean-VM 验收由
独立任务完成。

同 ref concurrency 消除 workflow 自身重跑竞争；发布期间其他主体不得持有或使用
`packages: write` 改写同 tag。GHCR 在本流程中没有 compare-and-set 接口，因此版本 tag
不可移动性还依赖该写权限治理，发布后继续以 digest manifest 和匿名 inspect 核对。

## Compatibility and failure boundaries

- 不改变 Compose 服务、权限、UID、socket 或持久数据布局，现有容器合同测试仍权威。
- 发布 bundle 引用版本 tag；digest JSON 只用于审计和发布后核对，不改变运行引用。
- 新 bundle 可替换旧受管文件并重建容器，但数据库只自动向前迁移。降级必须另外确认 schema 兼容或恢复备份，本文不新增 downgrade 接口。
- Release 发布/镜像 pull 验证不授权 `docker compose up`、宿主内核修改或真实硬件访问。
