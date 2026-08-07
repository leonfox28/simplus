# Linux 本地开发工作流

Simplus 直接在 Linux Git checkout 中开发、构建和测试。Mac 或其他电脑可以作为浏览器、SSH 终端或 Git 客户端，但仓库不依赖 rsync 镜像或自定义 SSH wrapper。

生产部署迁移到 Docker Compose 不改变这一点：日常 Go/Web 编译、生成校验、测试、
Simulator 和前端热更新都直接使用开发机工具链，不构建或进入开发容器。Docker 只在
生产镜像打包、Compose contract、clean-VM smoke 和最终容器 HIL 时使用。

## 1. 初始化工具链

进入仓库根目录：

```bash
cd <repo>
git status --short --branch
make dev-toolchain
make bootstrap-dev
make doctor
```

`make dev-toolchain` 根据仓库锁定文件把 Go、Node、Corepack/pnpm 安装到被 Git 忽略的 `.tools/`。依赖、构建缓存和运行数据都保存在被忽略目录中，不应提交。

常用验证：

```bash
make verify-modules
make verify-generated
make check-format
make lint
make test
make security
make build
make build-linux
```

按 `AGENTS.md` 选择与风险相称的检查；小改动不要求机械运行所有目标。

Web 的目标运行时由 [`0022`](decisions/0022-vite-react-query-web-runtime.md) 固定为
Vite + React Router Declarative Mode + 直接 `antd` + TanStack Query。常用的窄化
前端检查是：

```bash
corepack pnpm --dir web test
corepack pnpm --dir web typecheck
corepack pnpm --dir web build
corepack pnpm --dir web e2e
```

`build` 输出 `web/dist`，由 production `simplusd` 与匹配的 API 一起承载；`e2e`
使用本地脱敏 fixture，不是硬件或真实通信测试。OpenAPI 或生成 client 发生变化时还要
运行 `make verify-generated`。旧 Umi/Pro 输入已经删除；不要重新引入第二套路由、UI
或服务端状态栈。

## 2. Simulator

```bash
make dev-sim
```

默认入口：

```text
Web: http://127.0.0.1:5173
API: http://127.0.0.1:8080
Data: <repo>/.dev/data
```

从同一受信任 LAN 的另一台设备访问时显式运行：

```bash
make dev-sim-lan
```

浏览器打开 `http://<host-lan-ip>:5173`。只有 Vite Web dev server 对 LAN 监听，API
继续使用 loopback，并由开发服务器代理同源 `/api`，包括有界 SSE 失效流。该普通
HTTP 入口只用于可信开发网络。

全新开发数据库需要初始化时，在运行服务的主机终端执行：

```bash
sudo "$PWD/.dev/bin/simplusctl-dev" bootstrap-url \
  --socket "$HOME/.simplus-dev/data/run/simplusd-control.sock" \
  --base-url http://<host-lan-ip>:5173
```

URL 中的授权 code 为单次使用，兑换后由页面立即从地址栏移除。不要把 URL、管理员密码、Cookie 或开发数据库写进问题、文档或测试 fixture。

## 3. 真实硬件 HIL-0

首次安装或明确更新开发 Agent 时运行：

```bash
make dev-agent-deploy
```

该目标会请求本机 `sudo`、更新 root-owned 开发 Agent、重启服务并执行固定脱敏探测。ModemManager 正占用端点时部署 fail closed，不会替管理员停止它。

部署后可以执行：

```bash
make dev-hardware-probe
make dev-hardware
make dev-hardware-lan
```

硬件 backend 与 Simulator 使用不同数据目录，服务或协议不兼容时不会回退 Simulator。production Agent 不提供任意命令、短信、电话、eUICC mutation 或蜂窝数据入口；ML307A 运行时 RF 控制只接受布尔目标状态并要求读回确认。

## 4. strongSwan 插件发布包

普通 Go/Web 开发和 Simulator 不需要 strongSwan 源码。只有发布插件包时，在
Debian 13/amd64 普通用户会话执行：

```bash
make build-strongswan-plugins-deb
make test-strongswan-plugins-package
```

构建器按 [`0020`](decisions/0020-strongswan-plugin-package.md) 使用仓库锁定的
Debian source 与运行 ABI 输入，在临时目录和 sysroot 中构建，不写 `/usr`、不要求
root，也不读取主机已安装库作为链接输入。首次构建需要网络下载，之后使用被 Git
忽略的 `.dev/cache/strongswan-plugins`；所有下载都先校验 SHA-256。输出位于
`.dev/packages/strongswan-plugins`，包含 Debian 包、对应源码归档、摘要和 manifest。
不要提交这些生成物，也不要把任意 source/build 路径重新引入 release 接口。

## 5. 生产容器构建

容器文件的本地静态检查不要求 Docker：

```bash
make check-container-files
go test ./internal/containercontract
```

开发机安装 Docker 后，可以构建三个生产目标并渲染 Compose：

```bash
make container-build CONTAINER_IMAGE_TAG=dev
make container-config CONTAINER_IMAGE_TAG=dev
```

该过程生成 `simplus-control`、`simplus-agent` 和 `simplus-netd`，不会生成 dev image。
Compose 真实启动会访问 host USB/sysfs 并为 netd 创建网络对象，必须先按
[`installation.md`](installation.md) 完成宿主准备，并停止本机 `simplus-agent-dev`
及原生 production 服务。单纯构建或 `docker compose config` 不执行 RF、SIM AKA、
VoWiFi、短信或电话动作。

基础镜像使用仓库固定的 Go/Node/Debian tag 与 manifest digest。正式 tag workflow
只发布 `linux/amd64` GHCR 镜像，并把 strongSwan 插件 Debian 包、对应源码及镜像内
固定 Mihomo 的校验后 GPL 源码附加到同一 GitHub Release；普通 CI 仍由 runner 上的
原生 Go/Node 工具执行。

## 6. 受控 Host VoWiFi HIL

Host VoWiFi HIL 使用真实 SIM AKA 和网络连接，不是日常测试。现有 runner 只按已经验收的 RF Off 基线运行：要求唯一目标模组、SIM READY、RF Off、无活动呼叫且已经取得明确授权。RF On 共存验证必须另行设计和授权，不能从产品解耦直接推断兼容。

先构建固定 helper 和协议测试：

```bash
make build-vowifi-hil
bash scripts/dev/test-simplus-simaka-c.sh
```

正式 bundle 只安装经过锁定输入构建和验证的 `simplus-strongswan-plugins` 包；HIL
主机不现场编译插件，也不接受手工 source/build tree。一次性 runner 只接受内部固定
路径和类型化输入；节点配置必须为 root-owned mode `0600`，不得打印或提交。

正式安装后的日常路径不运行一次性 runner。管理员在线路页为具备 Host VoWiFi 鉴权能力的 Line 明确保存 `direct` 或 Mihomo 国家出口，再使用“激活 VoWiFi”与“停用 VoWiFi”。新 Line 的未配置出口不能激活；服务只恢复此前明确保存的激活意图。

出现连接、注册或刷新故障时按 [`troubleshooting.md`](troubleshooting.md) 的稳定错误码和复查顺序定位。公开问题中只能提供脱敏状态，不能附加真实订阅、节点、内网拓扑、SIM 身份、SIP 鉴权头或原始日志。

## 7. 数据与发布边界

- `.dev/`、`.tools/`、数据库、日志、录音、抓包和本机配置都不得提交；
- 原始 HIL 与逐次排错记录应进入仓库外的私有记录系统；
- 公开兼容性结论只更新 [`compatibility.md`](compatibility.md)；
- 准备公开提交前运行 `make check-docs`，并遵守 [`privacy-and-publication.md`](privacy-and-publication.md)；
- 正式安装和升级使用 release bundle，不从开发目录复制运行文件。
