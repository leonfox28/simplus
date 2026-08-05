# 0007：每订阅不可变工件、仅选择切换与独立运行态

## 状态

已接受，2026-08-04。

## 背景

把所有订阅节点临时拼装为一个配置，会混淆订阅来源、Line 和 Mihomo 生命周期。用户明确要求同一时间只使用一个订阅：创建或更新时保存原始 YAML 并提前生成成品配置；切换订阅只用当前 core 复验和选择，不立即重启。

## 决策

1. 每个订阅保存不可变版本工件：原始 `raw.yaml`、Simplus 生成的 `generated.yaml` 和包含摘要、core 版本、国家统计的 `metadata.json`；私有目录/文件权限为 `0700`/`0600`。
2. 原始订阅只贡献 `proxies`。生成配置只包含该订阅实际扫描出的国家；每个国家是一个不带订阅名的 `url-test`/`select` 组和稳定国家 TPROXY 入口。
3. 创建、URL 更新和刷新均在 staging 完成下载、解析、生成及当前 core `-t` 自检。成功后写入不可变版本并原子更新订阅 `current.json`；失败保留上一版本。
4. 全局分别持久化 `selected_subscription_id` 与 `running_subscription_id`。选择操作复验订阅成品并只更新 selected，不管理进程；已运行进程继续使用 running，界面显示待重启应用。
5. Start 使用 selected；Restart 在停止当前进程前复验 selected；Stop 只管理由 Simplus 启动且 manifest 与 `/proc` 身份一致的进程。当前订阅不能被停用或删除。
6. 生产部署和自动验收不启动 Mihomo。真实 Line namespace/veth/nftables、VoWiFi 和通信激活仍需后续明确授权。

## 取代关系

本决策取代 `0006` 中“从全部出口 Profile 临时生成单一 active 配置”的部分；`0006` 的订阅输入隔离、候选自检、原子发布和失败禁启动边界继续有效。
