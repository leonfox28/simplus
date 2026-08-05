# 0013：公开代码采用 PolyForm Noncommercial 1.0.0

- 状态：Accepted
- 日期：2026-08-05

## 背景

Simplus 将公开产品代码、架构和开发进度，但项目依赖本地通信硬件，仓库所有者不授权商业使用，也不要求非商业用户公开其私有修改。AGPL 等强 copyleft 许可证允许商业使用并要求特定场景下提供源码，不符合这两个目标。

## 决定

除文件或目录中另有许可证声明的材料外，Simplus 原创部分使用 [`PolyForm Noncommercial License 1.0.0`](../../LICENSE)。该许可允许非商业用途的使用、修改和分发，不要求用户仅因修改而公开源码，也不转让许可方对原创代码的版权。

项目对外统一称为“非商业源码可用”或 “noncommercial source-available”，不称为 OSI 认可的开源软件。仓库所有者保留向其他主体授予独立许可证的权利。

## 后果

- 商业使用者必须事先取得仓库所有者的单独授权；
- 分发原版或修改版时必须附带 PolyForm 条款及项目提供的 Required Notice（如有）；
- 外部贡献者仍拥有其贡献的版权，除非另有书面协议；
- Zashboard、strongSwan 插件和其他另行标注的材料继续使用各自许可证，详见 [`THIRD_PARTY_NOTICES.md`](../../THIRD_PARTY_NOTICES.md)；
- 若未来改变商业授权、贡献者协议或许可证范围，需要另行记录决定，不能静默改写既有发布物所获得的许可。
