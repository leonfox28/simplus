# 0023：按收件人合并短信会话并持久化未读状态

- 状态：Accepted / Implemented
- 日期：2026-08-10
- 局部取代：[`0022`](0022-vite-react-query-web-runtime.md) 第 6 条中短信会话必须同时以
  Line 与 remote address 过滤的决定

## 背景

短信历史原先把 Line 与 remote address 共同作为会话身份。管理员通过不同 Line 与同一
对端通信时会看到多个会话，且页面只能从已加载的消息局部分组，无法可靠列出更早历史中
才出现的对端。新短信 attention 只存在于内存 SSE 中，也不能在刷新、浏览器重开或服务
重启后提供可信未读数量。

消息的创建时间只有毫秒精度，稳定消息 ID 又是随机值。消息分页使用的
`(createdAt, message ID)` 元组因此不能表达同一毫秒内的未读到达顺序，也不能作为并发
到信时的安全已读水位。

## 决定

1. 产品会话身份改为持久消息中的 exact `remoteAddress`，不包含 Line。同一对端跨 Line
   合并；每条消息仍保留实际 Line，发送仍要求管理员显式选择一个当前可发送 Line。
   缺少国家上下文时不猜测本地号与国际号等价，联系人也只影响显示名称。
2. 后端提供按最后消息 `(createdAt, message ID)` 稳定倒序的会话摘要分页。摘要包含完整
   最后消息、未读数和最近一次出站 Line；当前会话历史允许只用 remote address 跨 Line
   查询。既有无过滤与 Line + remote address 查询保持兼容，Line-only 继续拒绝。
3. messages SQLite v7 在入站消息首次持久化的同一 transaction 中创建一条带
   `AUTOINCREMENT` 序号的 unread marker。计数从 marker 派生；重复入站不重复创建，
   删除短信通过外键级联删除 marker。
4. remote-only 最新页返回基于同一读取 snapshot 的 opaque read-through token。token
   只编码版本、单调 unread 序号与 boundary message ID，不含号码或正文。标记已读只删除
   同一 remote address 上不晚于该序号的 marker；较晚并发入站总有更大序号，不会被旧
   token 清除。重复或乱序请求幂等，boundary 已删除时要求客户端刷新。
5. v7 migration 创建空 unread ledger，升级前历史自然初始化为已读；未读只从新 schema
   生效后的首次入站持久化开始。Down migration 删除 ledger 与 remote-only 索引，但保留
   全部业务短信。
6. 桌面 Web 使用收件人列表与会话双栏，窄屏使用 list→detail→back。只有 detail 实际
   可见、document visible 且 remote-only 最新页成功渲染后，浏览器才原样提交该 snapshot
   的 token。HTTP/SQLite 继续是唯一权威，现有 `messages` SSE topic 仍只触发 active query
   重新读取，不增加号码、正文、未读数或水位字段。

## 后果

- 一个现实号码以不同存储形式出现时仍可能形成多个会话；在有可靠规范化决策前保持
  exact identity 比猜错身份更安全；
- 未读状态与消息同库原子维护，可跨刷新和重启恢复，并能承受同毫秒消息、并发到信、
  多标签页乱序已读与删除；
- 最近 Line 不可用或已删除时 composer 保留原选择并 fail closed，不能把其他 Line
  伪装为默认发送身份；
- 本决定不修改 transport、编码、operation 幂等、发送状态机或 SSE payload，也不授权
  真实短信、RF、模组持久写或 HIL。
