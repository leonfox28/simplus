# 飞书绑定 expiry 上限复发修复：技术设计

## 1. Root cause

第一轮补丁正确加入 `expires_in` 字段，却继续沿用初版 `expiresIn > 900` 的本地拒绝条件。当前飞书中国 Begin 成功响应是 3600 秒，因此在 device code、authority、HTTP status 等校验全部通过后，仍同步返回 `ErrFeishuProviderResultInvalid`。

截图中的顶部 mutation Alert + 卡片缓存 idle 与该路径完全一致。脱敏结构探针进一步排除了网络、provider error、device code、URL authority、最终 URL 长度及 Poll pending 语义。

## 2. Minimal code change

只修改 `internal/application/notification/feishu.go` 的 expiry 归一化及 `binding_test.go` fixture/边界测试：

- 增加 `feishuRegistrationLifetimeLimit = 24 * time.Hour`。
- `normalizeFeishuRegistrationLifetime(currentSeconds, legacySeconds, intervalSeconds) (time.Duration, error)` 负责：
  - 两个正值不同 → invalid；
  - current 正值优先，否则 legacy 正值；
  - 两者均非正 → 600 秒；
  - 用秒数与 `limit/time.Second` 比较，避免乘法溢出；
  - duration 小于 poll interval 或超过 24 小时 → invalid。
- `Begin` 用返回 duration 直接计算 `ExpiresAt`。

不改 device code、URL、HTTP decode、Poll、messenger、状态机、API、Web 或存储。

## 3. Test contract

- 现场结构合成 fixture 使用 514+ 字节特殊字符 synthetic code、`open.feishu.cn` 和 `expires_in=3600`，通过 service `StartFeishuBinding` 断言 waiting + 一小时期限。
- table test 覆盖 current 3600、legacy 60、matching、default 600、24 小时边界、24 小时+1 秒、conflict、短于 interval 和极大整数。
- 原有 exact authority、opaque code、HTTP 400 device-flow、token/message strict success 和 expiry fence 测试保持。
- 聚焦 race/HTTP 后运行全量门禁；无 OpenAPI/生成源变更，但仍运行 `make verify-generated` 检查漂移。

## 4. Observability and privacy

不记录 expiry 探针的临时 code/URL/响应。`expires_in=3600`、HTTP status、稳定 `authorization_pending` 和 authority 是非身份结构事实，可写入任务根因材料。公共错误仍为稳定 `FEISHU_BINDING_RESULT_INVALID`，不新增 provider 正文回显。

## 5. Deployment and live acceptance

用户批准实施后，仍需在提交阶段单独确认提交分组。用户已经要求修复现场，但本次最终规划摘要后需新批准才能实施；部署授权将随该批准一并确认。部署只从当前提交重建 `dev` 镜像并原地 `docker compose up -d`，保留 `./data`。

代理只验证镜像摘要、版本、health 和认证。用户随后点击“绑定飞书私聊”；出现 URL 后由用户决定是否打开和完成真实授权。
