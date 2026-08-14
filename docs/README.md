# Simplus 文档地图

本文是项目公开知识库入口。产品规范、架构、开发进度和通用运维知识保存在仓库中；真实部署现场、订阅、网络拓扑、原始 HIL 日志和逐次排错时间线保存在仓库外的私有记录系统中。

## 当前记录系统

| 文档 | 作用 | 状态 |
| --- | --- | --- |
| [`product.md`](product.md) | 产品目标、MVP 范围、非目标和安全边界 | 规范性 |
| [`architecture.md`](architecture.md) | 当前进程、数据流、核心不变量和技术边界 | 规范性 |
| [`plans/active/mvp.md`](plans/active/mvp.md) | 唯一活跃执行计划和下一步 | 规范性 |
| [`handoff.zh-CN.md`](handoff.zh-CN.md) | 脱敏后的当前实现进度和接手提示 | 现场摘要 |
| [`development.md`](development.md) | 通用 Linux 本地开发、测试和受控 HIL 命令 | 操作指南 |
| [`installation.md`](installation.md) | Docker Compose 生产部署、宿主准备和生命周期 | 操作指南 |
| [`compatibility.md`](compatibility.md) | 公开兼容性结论和证据等级 | 参考证据 |
| [`troubleshooting.md`](troubleshooting.md) | 不包含私人现场信息的稳定错误码和复查顺序 | 操作指南 |
| [`privacy-and-publication.md`](privacy-and-publication.md) | 公开/私有记录边界和发布前检查 | 规范性 |
| [`decisions/`](decisions/) | 影响产品范围或架构的重要决策 | 决策记录 |

## 按任务阅读

- 改产品范围：先读 `product.md`，并新增或更新 decision；
- 改进程、领域或数据流：读 `architecture.md`；
- 开始功能实现：读 `plans/active/mvp.md` 和 `handoff.zh-CN.md`；
- 启动、测试或连接真实硬件：读 `development.md`；
- 判断某项能力是否真实验证：读 `compatibility.md`；
- 排查正式运行态：读 `troubleshooting.md`；
- 准备提交、发布或公开仓库：读 `privacy-and-publication.md`。

## 文档原则

本结构参考 OpenAI 的 [Harness Engineering](https://openai.com/zh-Hans-CN/index/harness-engineering/) 实践：仓库是公开记录系统，入口提供地图而不是百科全书，复杂任务使用版本化执行计划，重要不变量尽量由测试和工具强制执行。

1. 当前范围只有一个来源：`product.md`；
2. 同一时刻只有一个活跃计划；
3. `handoff` 只记录公开可披露的当前实现摘要；
4. 原始 HIL、逐节点结果、真实拓扑和旧私人规划不进入公开仓库；
5. 代码事实与文档冲突时，先核实代码和测试，再修正文档；
6. 每个纵切优先交付可运行、可观察、可验证的用户路径；
7. 新抽象必须解决当前纵切中的真实问题，不能只为假设中的未来平台预建。

`make check-docs` 会机械验证必需入口、本地链接、唯一活跃计划和公开资料隐私规则。CI 运行同一检查。
