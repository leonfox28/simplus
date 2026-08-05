# 0006：由 Simplus 生成并验证 Mihomo 专用配置

## 状态

已接受，2026-08-04。

## 背景

订阅提供的是面向通用 Clash 客户端的完整 YAML，可能包含普通桌面代理使用的 DNS、规则、监听器和策略组。Simplus 只需要其中的代理节点，并需要让多个 Host VoWiFi Line 在单一 Mihomo 实例中使用独立出口。用户同时明确要求订阅完整 URL 明文保存，并要求任何生成配置解析或自检失败时都不得启动 Mihomo。

## 决策

1. 订阅 URL 在 core SQLite 中明文保存并通过管理 API 完整显示；数据库、配置文件和目录权限仍分别保持 `0600`/`0700`，URL 不写入普通日志。
2. 订阅刷新使用有界下载和 YAML 解析，只接受受支持的 `proxies` 节点；忽略订阅提供的 `listeners`、`tun`、`dns`、`rules`、controller、providers 和文件路径等系统配置。
3. Simplus 从节点名称和可覆盖的用户选择生成国家分类；出口 Profile 可选择固定节点或国家组。国家组仅包含当前订阅中该国家的节点，缺失时 fail closed。
4. Simplus 独占生成 DNS、TPROXY listener、按 Line 源网段路由、策略组和末尾 `MATCH,REJECT`。每份激活订阅额外生成唯一 `🌐 DNS` url-test 组，包含该订阅全部有效节点；共享 `redir-host` DNS 使用 1.1.1.1/8.8.8.8 DoH，开启 `respect-rules`，并在末尾拒绝规则前把两个 DoH IP 导向该组。代理节点域名另用不经过该组的 `proxy-server-nameserver` 解决循环依赖。真实 namespace、veth、nftables 和 VoWiFi 激活不在本切片执行。
5. 每次生成先写入私有 staging 文件，再调用已安装的同一 Mihomo binary 执行配置自检。只有自检成功才原子发布为 active 配置。
6. 没有 active 配置、active 配置未通过当前 binary 自检、或最近候选自检失败时，生命周期管理器必须拒绝启动和重载。不得回退到候选配置、订阅原始配置、Direct 或通用代理模式。
7. 配置生成与发布不等于启动。启动 Mihomo、设置透明代理和激活真实 Host VoWiFi 仍需要后续决策与用户明确授权。

## 参考实现

- Sub-Store：<https://github.com/sub-store-org/Sub-Store>
- subconverter：<https://github.com/tindy2013/subconverter>
- Clash Verge Rev：<https://github.com/Clash-Verge-rev/clash-verge-rev>
- Mihomo listener：<https://wiki.metacubex.one/en/config/inbound/listeners/>
- Mihomo TPROXY：<https://wiki.metacubex.one/en/config/inbound/listeners/tproxy/>
