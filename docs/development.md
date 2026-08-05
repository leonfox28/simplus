# Linux 本地开发工作流

Simplus 直接在 Linux Git checkout 中开发、构建和测试。Mac 或其他电脑可以作为浏览器、SSH 终端或 Git 客户端，但仓库不依赖 rsync 镜像或自定义 SSH wrapper。

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

浏览器打开 `http://<host-lan-ip>:5173`。只有 Web dev server 对 LAN 监听，API 继续使用 loopback，并由开发服务器代理。该普通 HTTP 入口只用于可信开发网络。

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

硬件 backend 与 Simulator 使用不同数据目录，服务或协议不兼容时不会回退 Simulator。production Agent 不提供任意命令、RF、短信、电话、eUICC mutation 或蜂窝数据入口。

## 4. 受控 Host VoWiFi HIL

Host VoWiFi HIL 使用真实 SIM AKA 和网络连接，不是日常测试。只有唯一目标模组、SIM READY、RF Off、无活动呼叫且已经取得明确授权时才能运行。

先构建固定 helper 和协议测试：

```bash
make build-vowifi-hil
bash scripts/dev/test-simplus-simaka-c.sh
```

自定义 strongSwan 插件必须针对主机实际安装的版本与 build tree 构建。一次性 runner 只接受内部固定路径和类型化输入；节点配置必须为 root-owned mode `0600`，不得打印或提交。

正式安装后的日常路径不运行一次性 runner。管理员在线路页保存 Host VoWiFi 接入方式和 `direct` 或 Mihomo 国家出口，再使用“激活 VoWiFi”与“停用 VoWiFi”。服务只恢复此前明确保存的激活意图。

出现连接、注册或刷新故障时按 [`troubleshooting.md`](troubleshooting.md) 的稳定错误码和复查顺序定位。公开问题中只能提供脱敏状态，不能附加真实订阅、节点、内网拓扑、SIM 身份、SIP 鉴权头或原始日志。

## 5. 数据与发布边界

- `.dev/`、`.tools/`、数据库、日志、录音、抓包和本机配置都不得提交；
- 原始 HIL 与逐次排错记录应进入仓库外的私有记录系统；
- 公开兼容性结论只更新 [`compatibility.md`](compatibility.md)；
- 准备公开提交前运行 `make check-docs`，并遵守 [`privacy-and-publication.md`](privacy-and-publication.md)；
- 正式安装和升级使用 release bundle，不从开发目录复制运行文件。
