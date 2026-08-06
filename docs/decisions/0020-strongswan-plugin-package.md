# 0020：strongSwan 插件独立构建与 Debian 包

- 状态：Accepted
- 日期：2026-08-06

## 背景

Host VoWiFi 使用 Debian 的 `charon-systemd`、`libcharon`、`libsimaka` 与
`eap-aka`，并额外需要 Simplus 的 `simaka_card_t` bridge 和 strongSwan
上游 `p-cscf` 插件。strongSwan 没有为 `libcharon`/`libsimaka` 私有插件
接口提供完整、稳定的发行版开发 SDK，因此旧 release 脚本要求开发者手工
提供 strongSwan source 与 configured build tree，再把两个裸 `.so` 复制进
bundle。该流程无法独立证明源码、生成的 `config.h` 与链接运行库来自同一
Debian revision，也把只有发布构建才需要的源码边界泄漏给了普通开发者。

插件只服务 Simplus Agent 的固定 Unix Socket 协议和 IMS APN IDr 行为；当前
不存在独立消费者。立即拆成另一个 Git 仓库会引入跨仓库协议、版本和发布同步，
并不能消除 strongSwan 私有 ABI 的编译时源码需求。

## 决定

1. Simplus 自有插件作为同仓库内独立 GPL-2.0-or-later 组件保存在
   `components/strongswan-simplus-simaka`，不继承根目录 PolyForm 许可；
   strongSwan 上游 `p-cscf` 源码不复制进 Git，而从锁定的 Debian source
   package 构建。
2. `packaging/strongswan-plugins` 维护每个受支持发行版/架构的独立输入锁。
   锁必须记录 Debian source、生成 ABI 所需的运行库 `.deb`、HTTPS 来源和
   SHA-256；当前唯一目标是 Debian 13/amd64。新增架构或发行版必须新建锁并
   取得原生 CI/安装证据，不能复用 amd64 产物或猜测 ABI。
3. 构建以普通用户运行：所有输入先校验摘要，Debian source 解到临时目录，
   运行库解到临时 sysroot，再由匹配 source 生成 `config.h` 并链接两个插件。
   构建不得安装系统包、写 `/usr`、要求 root，或读取主机已有 strongSwan
   头文件/库作为隐式输入。
4. 发布产物为 `simplus-strongswan-plugins.deb`、包含全部锁定输入的对应源码
   归档、摘要文件和机器可读 manifest。bundle 安装器只按 manifest 校验并由
   `dpkg` 安装；卸载器由 `dpkg` 删除，旧版裸文件只保留一次性兼容清理。
5. 二进制记录精确构建 source revision，但依赖范围限制在同一 strongSwan
   上游 ABI 系列，而不锁死一个 Debian security revision，避免阻止安全更新。
   新 security revision 仍必须更新输入锁并通过包构建/兼容性 CI，才能作为
   新发布的已审查构建输入。
6. CI 单独构建该 Debian 包，检查导出构造符、动态依赖、固定 runpath、包内容、
   权限、manifest 与对应源码完整性。普通 Go/Web 测试和运行安装不需要
   strongSwan source；真实 ePDG、SIM AKA、RF、短信和电话不属于包 CI。
7. 运行时架构保持不变：charon 仍通过极薄 C plugin 调用外部 Go Agent，
   `simplus-netd` 仍通过 VICI 和固定 worker 生命周期控制 strongSwan。不得为
   消除构建依赖而把 Go runtime 嵌入 charon，或把 AT/APDU 逻辑移入插件。

## 后果

- release 构建不再接受人工 `SIMPLUS_STRONGSWAN_SOURCE/BUILD` 路径，输入来源和
  摘要成为可评审代码；
- 用户只安装架构匹配的 `.deb`，不需要源码、编译器或 configured build tree；
- 同一仓库可以原子修改 Agent 协议、插件与集成测试，同时保持不同许可证边界；
- 对应源码随二进制产物一起生成，p-cscf 与私有 ABI 的来源可复现；
- 以后若 socket backend 成为通用 strongSwan 能力并有独立消费者，可再拆仓库
  或提交上游；当前不为这一假设增加跨仓库发布系统。
