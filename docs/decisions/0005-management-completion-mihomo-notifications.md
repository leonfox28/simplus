# 0005：完成非真实通信管理面、Mihomo 与通知渠道

## 状态

已接受，2026-08-04。

## 背景

V1 已完成真实模组只读连接和 Simulator 业务路径，但日常管理面仍不完整：管理员不能改密，模组与线路页信息不足，Mihomo 只有模拟的 running/stopped/failed 开关，没有 core、订阅或节点管理；企业微信与飞书此前被排除。

用户明确要求暂停 HTTPS，继续完成除真实线路短信/通话与真实 Host VoWiFi 激活之外的功能，并把 Mihomo core/订阅管理及企业微信、飞书通知纳入当前目标。

## 决策

1. HTTPS 暂停，不作为当前完成条件。
2. 真实 QDC507/ML307A 继续只读；真实 SMS、Call、RF、数据连接、eUICC 切换和 Host VoWiFi 激活均不执行。
3. Mihomo 管理纳入当前范围：
   - 只从 MetaCubeX 官方 GitHub release 获取 stable metadata 和匹配当前 Linux 架构的资产；
   - 下载到 staging，执行大小上限、SHA-256、gzip 解包、普通文件检查和 `mihomo -v` probe 后原子激活；
   - 禁用 Mihomo 自更新，不把它注册为宿主通用代理；
   - 管理订阅 URL、刷新状态、节点摘要和 Host VoWiFi 专用 Profile；凭据不回显，不记录订阅正文；
   - core、订阅或节点不可用时 `mihomo-required` 保持离线，不回退 direct。
4. 企业微信与飞书只作为出站通知渠道：
   - 支持配置、禁用、测试消息、投递状态和错误摘要；
   - V1 不接收入站命令，不提供机器人远程控制；
   - secret 只接受写入/替换，不通过 API 或 Web 回显。
5. 管理后台补齐管理员改密、模组/线路配置、明确的加载/空/失败状态和移动端布局。

## 非目标

- 下载或运行第三方 Mihomo 分叉、Alpha/预发布或任意用户上传的可执行文件；
- 通用系统代理、LAN 代理端口、热点、NAT 或蜂窝数据网关；
- 真实 VoWiFi/ePDG/IMS 激活；
- 企业微信/飞书入站事件、审批、通讯录同步或远程短信/电话命令；
- 当前阶段的内建 HTTPS。

## 依据

- Mihomo 官方发布页：<https://github.com/MetaCubeX/mihomo/releases>
- Mihomo 官方文档：<https://wiki.metacubex.one/>
