# Design — 统一线路手机号观测

## 1. Boundary and ownership

手机号属于当前 `Line` 绑定的 SIM/Profile 身份观测，不属于 Modem、短信 transport 或 Host VoWiFi 页面。维持固定依赖链：

```text
QDC507 SubscriberNumberAdapter (AT+CNUM)
  -> Agent SIMObservation.SubscriberNumber
  -> hardware.SubscriptionProfile.CellularPhoneNumber
  -> inventory.Line current profile
                                      \
                                       Line Service -> domain/line.View.PhoneNumbers
                                      /
VoWiFi supervisor status (IMS number) -> Line-defined PhoneNumberSource port
  -> OpenAPI ManagedLine.phoneNumbers -> Web Lines page
```

Web 不再 join `VoWiFiLineState.phoneNumber`。Line service 是唯一来源合并者。它定义 consumer-owned 的窄端口，接受 `Line ID -> optional IMS E.164` 当前快照；实现 adapter 可以读取 `vowifisupervisor.API.List`，但 Line package 不依赖 `application/vowifi` 或 `domain/vowifi`，避免循环依赖。

## 2. Cellular typed capability

新增 `SubscriberNumberAdapter`，只暴露：

```go
ReadSubscriberNumber(context.Context, attransport.Query) (string, error)
```

QDC507 固定调用一次 `AT+CNUM`。parser 使用有界 CSV 语义，只接受：

- transcript 精确为一条 `+CNUM` record 加 final `OK`；
- 三字段 `<alpha>,<number>,<type>`；alpha 可为空但必须是合法 bounded CSV；
- number 本身匹配 `^\+[1-9][0-9]{2,14}$`；
- type 精确为 `145`。

只有 `OK` 是正常 unavailable。重复/多条 record、额外 URC/echo、少/多字段、national/unknown type、缺 `+`、controls、overflow、错误/mixed/trailing terminal 或 query error 均返回 unavailable。上层不会基于 TON、PLMN、运营商或已知号码补全。

`executeATProbe` 只在 ready present SIM identity 已成功读取后调用本能力，并把成功结果放入同一 `SIMObservation`。`SIMProfileIdentity` 继续只负责稳定身份与归属运营商；号码保持独立能力，避免 identity 读取接口被可选 metadata 耦合。号码读取是 best effort；失败不降级完整 probe。Agent client validation 要求号码只有在 keyed SIM fingerprint、masked hint、present+ready 时出现，并验证 E.164。

## 3. Profile and Line propagation

独立 subscriber-number 结果随后进入：

- `agentapi.SIMObservation.SubscriberNumber`；
- `hardware.SubscriptionProfile.CellularPhoneNumber`；
- `inventory.SubscriptionProfile` 与当前 resolved Line；
- `domain/line.View.PhoneNumbers`。

号码不进入 `domain/line.Record` 或 SQLite。换卡会产生不同 profile fingerprint；缺卡、锁卡、identity failure 或 profile mismatch 时没有当前 profile，Line 返回空的蜂窝号码观测，不保留旧值。号码参与当前 topology revision，使硬件号码变化能出现在下一次 Line refetch；setup/hardware topology HTTP mapper、日志与 SSE payload 不输出号码，只有管理员鉴权的 `ManagedLine` mapper 显式输出。

Simulator profile 使用 synthetic `+E.164`，保证 UI 与公共契约可离线测试。

## 4. IMS source and merge

Line-defined port 返回与稳定业务 Line ID 关联的当前 IMS 号码。只有 supervisor `Status.PhoneNumber` 已经通过现有 IMS E.164 validator、对应当前 Line，且该 Managed Line 此刻仍解析到原 SIM/Profile 时才贡献 `ims` 观测；stopped/offline/empty、Line unavailable 或 list failure 不贡献号码，也不使整个 Line list 失败。此端口只用于 `List/Add/Update` 返回的展示 `View`，不得进入 `Topology`，因此短信、电话、出口和 VoWiFi 控制的 Line 解析不依赖 supervisor 号码状态。端口在 hardware composition 中注入，在 Simulator 中使用合成 source 或仅蜂窝 synthetic observation。

Line merge algorithm：

1. 收集合法 `cellular-sim` 与 `ims` `(number, source)`；
2. 按 exact E.164 number 分组；
3. sources 去重并按固定枚举顺序排序；
4. observations 按 number 排序；
5. 最多两项。

结果矩阵：

| Cellular | IMS | `ManagedLine.phoneNumbers` |
| --- | --- | --- |
| empty | empty | `[]` |
| A | empty | `[{A,[cellular-sim]}]` |
| empty | A | `[{A,[ims]}]` |
| A | A | `[{A,[cellular-sim,ims]}]` |
| A | B | `[{A,[cellular-sim]},{B,[ims]}]`（按号码排序） |

不选主号码，不把不同值视为 Line failure。

## 5. Public API and Web

OpenAPI-first 新增：

```yaml
PhoneNumberObservation:
  required: [number, sources]
  number: E.164
  sources: unique array, 1..2, enum [cellular-sim, ims]

ManagedLine.phoneNumbers:
  required array, maxItems: 2
```

从 `VoWiFiLineState` 删除 `phoneNumber`，同步 generated Go/TS/Zod。该接口是本地同版本前后端整体更新，不保留页面双读兼容路径；domain supervisor 内部字段可暂时保留作为 Line adapter 输入。

线路页表格/卡片只渲染 `ManagedLine.phoneNumbers`：

- 空：`未获取`；
- 每个号码独立显示；
- source 标签为 `蜂窝 SIM`、`IMS`；
- 同值时一个号码旁显示两个标签；
- 不同值时两行全部显示。

VoWiFi 状态查询继续只用于激活/在线/阶段/出口，不再提供号码给页面。

## 6. Privacy, operation, and rollback

- 号码是当前只读观测，不持久化，不进普通日志/错误/SSE/task evidence；测试只用 synthetic values。
- 生产 probe 新增固定 read-only `CNUM`，无 RF/SIM/SMS/data write；普通测试不能触碰设备。
- HIL-0 已确认当前 QDC507/SIM 返回唯一 type-145 E.164；真实值只在私有对话展示。
- 回滚是恢复旧镜像/提交；没有 schema migration 或持久数据回滚。
- 完整门禁通过后重建 `dev` 三镜像、原地 Compose update、核对 health/revision，并私下确认真实 Line API/UI observation；不打印号码到日志或 task。

## 7. Key risks

- `CNUM` 是 SIM phonebook 数据，可能为空或陈旧；因此保留来源并与 IMS 不同值同时展示。
- Line/VoWiFi 构造可能形成依赖环；通过 Line-owned narrow port + supervisor adapter 避免。
- Probe cache 会暂存号码于 Agent/simplusd 进程内存；这是当前 observation 所需，但不得落盘或进入 error/log。
- 删除公开 VoWiFi phoneNumber 会触及生成物与 UI fixtures；必须 OpenAPI-first 原子更新并运行 generated drift checks。
