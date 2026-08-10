# 0024：以首次持久化序号排列短信

- 状态：Accepted / Implemented
- 日期：2026-08-11
- 局部取代：[`0022`](0022-vite-react-query-web-runtime.md) 第 6 条和
  [`0023`](0023-recipient-sms-conversations.md) 第 2 条中以
  `(createdAt, message ID)` 排列短信历史与会话的决定

## 背景

出站短信的 `createdAt` 是 Simplus 在发送副作用前记录的本机时间；入站短信的
`createdAt` 则是模组或运营商提供的业务时间，可能只有整秒精度、时钟有偏差或经过延迟
同步。两者都应继续作为业务时间展示，但不能可靠表示 Simplus 先持久化了哪条记录。
随机 message ID 只能稳定打破相同时间，不会恢复真实的本地观察顺序。

发送一条短信后再收到内容回传时，入站业务时间可能早于已经持久化的出站记录。按业务
时间排序会令入站气泡出现在出站上方，并让历史、会话摘要与浏览器各自重复同一错误推断。

## 决定

1. messages schema v8 为每条首次成功插入的短信分配全局
   `record_sequence INTEGER PRIMARY KEY AUTOINCREMENT`。出站在 transport 副作用前取得
   序号；入站 message 与 unread marker 在同一 transaction 中取得序号。replay、状态
   更新和删除不改变或复用序号。
2. 全局、remote-only、Line + remote 历史、会话最后消息、会话顺序和最近出站 Line
   全部按 `record_sequence DESC` 查询。`createdAt`、`updatedAt`、`sentAt` 的公共含义和
   UI 展示不变，公共 JSON 不暴露内部序号。
3. v8 为 v7 历史按可恢复的首次本地持久化代理回填：入站使用 `updatedAt`，出站使用
   `createdAt`，再以既有 `(createdAt, message ID)` 确定性打破同毫秒平局。Down 回到 v7
   时保留全部业务消息、unread marker 及其 AUTOINCREMENT 水位。
4. SMS 新页面使用带 SMS kind 的 v2 opaque cursor，编码正整数 record sequence 和稳定
   message ID，并以严格 `<` keyset 继续分页。boundary 消息删除后 sequence 仍足以继续。
   部署前的 v1 `(createdAt, message ID)` cursor 只在 boundary 行仍存在、时间和 filter
   一致时映射到 sequence；所有新响应只产生 v2。
5. Calls 继续使用原有 v1 `(createdAt, call ID)` cursor，并拒绝 SMS v2。浏览器不解析
   cursor，也不再按业务时间或 ID 排序；它只拼接服务端 newest-first 页面，再反转副本为
   oldest-first 气泡顺序。SSE 仍只触发 HTTP 权威快照刷新。

## 后果

- 短信页面顺序表达 Simplus 首次成功持久化的稳定先后；运营商业务时间仍可能看似倒退，
  但不会再驱动气泡、分页或摘要重排；
- v8 主表从 `WITHOUT ROWID` 业务 ID 主键改为 AUTOINCREMENT 整数主键，message ID 保持
  UNIQUE 公共业务身份和 unread 外键目标；
- 无法从 v7 字段恢复的同毫秒历史只保证确定性回填，未来记录不再依赖时钟精度；
- 本决定不修改会话 identity、Line、联系人、未读 token、transport、发送状态机或 SSE
  payload，也不授权真实短信、RF、模组写入或 HIL。
