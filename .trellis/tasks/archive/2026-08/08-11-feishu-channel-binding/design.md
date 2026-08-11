# 飞书通知渠道一键绑定：技术设计

## 1. 设计目标

在现有单向通知系统中增加飞书应用私聊渠道。管理员从通知页发起绑定，Simplus 返回飞书官方短期验证 URL；管理员授权后，Simplus 自动取得最小权限应用凭据，向授权用户发送一次测试消息，并只在测试成功后持久化可用渠道。

设计继续保持以下边界：单管理员可信 LAN、HTTP/SQLite 权威、无公网回调、无入站消息、无群聊、凭据不回显、现有 Webhook 渠道兼容。

## 2. 总体数据流

```text
Notifications page
  -> POST /notification-channel-bindings/feishu (admin + CSRF)
  -> notification binding service
       -> Feishu device-flow begin (fixed accounts.feishu.cn endpoint)
       <- verification URL + opaque device code + expiry
  <- waiting state + verification URL (Cache-Control: no-store)

Administrator opens Feishu URL and approves
  -> background bounded poll
  <- App ID + App Secret + authorizing open_id
  -> validate Feishu tenant and bounded credential shapes
  -> obtain tenant token + send one test message to open_id
  -> encrypt App ID/App Secret/open_id with instance key
  -> insert feishu_app_notification_channels row with delivery=success
  -> publish notifications invalidation
  <- status becomes succeeded; Web refetches channel list
```

The verification URL and opaque device code are transient workflow state. They never become channel data. A formal channel is created only after authorization and the test delivery both succeed.

## 3. Application boundaries

Keep the feature under `internal/application/notification` and extend the existing consumer-owned ports:

- `Store` gains explicit read/write/delete/delivery operations for the Feishu-app channel variant while continuing to expose a unified channel view.
- A narrow `FeishuRegistrar` owns only device-flow begin/poll/cancel behavior.
- A narrow `FeishuMessenger` owns only sending one bounded text message to an `open_id` using App ID/App Secret.
- The notification service owns the binding state machine, default channel configuration, encryption labels, persistence ordering and stable errors.
- `httpapi.Server` maps the application state/errors to OpenAPI and continues to require administrator authentication/CSRF.
- `cmd/simplusd` injects the process lifetime context and a bounded notification-change callback; the application package does not import HTTP API or realtime packages.

The external client is project-owned and fixed-purpose. It does not expose arbitrary Feishu paths, permissions, callbacks, request bodies or tokens to Web/API callers.

## 4. Binding state machine

Only one binding attempt may be active per Simplus instance. Sequential successful bindings remain allowed and produce independent channels/apps.

```text
idle/terminal --start--> waiting --authorization--> testing --test+persist--> succeeded
                              |                         |
                              +-- denied/expired ------+--> failed/expired
                              +-- cancel -----------------> cancelled
                              +-- process exit ------------> ephemeral state lost
```

States exposed publicly are `idle`, `waiting`, `testing`, `succeeded`, `failed`, `expired`, and `cancelled`.

- `POST` replaces an idle or terminal state. Starting while `waiting` or `testing` returns a stable conflict.
- `DELETE` is idempotent while idle/terminal and cancels only a `waiting` attempt. Once testing begins, cancellation is rejected so the service has one clear persistence outcome.
- The server holds a generation token under a mutex. A stale/cancelled goroutine must re-check ownership before testing or persisting.
- The URL is retained only while `waiting` and cleared on every terminal/testing transition. Device code and returned credentials are never included in public state.
- Process restart cancels all pending work and yields `idle`; no partial database row exists. An external app may already have been created if the process died after authorization, which is surfaced as a documented residual risk rather than guessed/recovered.

Stable terminal error codes are bounded and credential-free, including denial, expiry, unsupported Lark tenant, invalid provider result, provider/network failure, test-delivery failure and persistence failure. Raw response bodies and provider error descriptions remain internal and are not logged when they may contain identifiers.

## 5. Feishu protocol contract

The registrar follows the official RFC 8628-style one-click application flow:

- exact HTTPS account host `accounts.feishu.cn`;
- fixed registration endpoint and `PersonalAgent` archetype;
- `createOnly=true` so an existing app can never be selected or modified;
- prefilled application name/description identifying it as a Simplus notification app;
- minimal base template (`preset=false`);
- exactly one tenant permission: `im:message:send_as_bot`;
- no user permissions, events, callbacks, WebSocket or webhook configuration;
- returned verification URL must be HTTPS, have the exact approved host, contain no userinfo/fragment, and remain within the public length bound;
- poll interval, `slow_down`, denial, expiry and context cancellation follow the provider result, all within the provider expiry and process context.

The messenger uses only:

1. the fixed tenant-access-token endpoint at `open.feishu.cn`;
2. the fixed message-create endpoint with `receive_id_type=open_id` and `msg_type=text`.

Both clients reject redirects, use explicit timeouts, cap response bodies, validate required fields and numeric success codes, and never cache tokens beyond a single delivery. This ensures a deleted channel has no reusable long-lived provider token retained by Simplus.

MVP rejects `tenant_brand=lark`; Lark international endpoints and domains remain out of scope.

## 6. Persistent model and migration

Append core migration v23 with a separate `feishu_app_notification_channels` table. Do not rebuild or reinterpret the v12 `notification_channels` Webhook table.

The new table contains:

- random `channel_...` ID and display name;
- independently encrypted App ID, App Secret and recipient `open_id` ciphertexts;
- enabled flag and JSON event-kind set;
- last delivery time/status/error code;
- created/updated timestamps.

SQLite constraints bound IDs, display name, ciphertext sizes, event JSON, states and error-code length. Provider and target kind are implicit constants of this table. Cross-table ID collision is rejected by the store/service even though the random 128-bit ID already makes it negligible.

The domain channel gains a discriminant `DeliveryMode` (`webhook` or `feishu_app`). Existing Webhook fields remain valid only for `webhook`; encrypted application fields remain valid only for `feishu_app`. Service methods use exhaustive switches rather than placeholder Webhook values.

List/read/delete/delivery-status store operations merge or target both tables. The public list remains one ordered channel collection. Down migration drops only the new table and restores core schema version 22; legacy Webhook data is untouched. Because old binaries cannot represent app channels, rollback across v23 requires the explicit Down migration and loses local app bindings, while the corresponding Feishu-side apps remain for manual cleanup.

Encryption labels are versioned and field-specific, for example:

```text
notification-channel:v2:<channel-id>:feishu-app-id
notification-channel:v2:<channel-id>:feishu-app-secret
notification-channel:v2:<channel-id>:feishu-recipient-open-id
```

Deletion is logical SQLite deletion under the project's existing storage contract; it stops future use and removes the active rows but does not claim forensic erasure from WAL, free pages or backups.

## 7. Public HTTP contract

Extend `NotificationChannel` compatibly with required discriminants:

- `deliveryMode`: `webhook | feishu_app`;
- `targetType`: `webhook | authorized_user`.

No App ID, App Secret, access token, `open_id`, device code or verification URL appears in ordinary channel list/read responses. For compatibility, existing Webhook fields stay present; app channels use the bounded provider endpoint hint while the Web renders `targetType` instead of labeling it a Webhook.

Add the singular resource `/api/v1/notification-channel-bindings/feishu`:

- `GET`: return current transient state; administrator authentication; no CSRF;
- `POST`: begin a new attempt; administrator authentication + CSRF;
- `DELETE`: cancel a waiting attempt; administrator authentication + CSRF.

The response always has a state plus bounded `verificationUrl`, `expiresAt`, `channelId`, and `errorCode` fields, using empty values where not applicable to match current OpenAPI conventions. All responses containing or describing a binding carry `Cache-Control: no-store`.

Existing `PUT /notification-channels/{id}` remains the settings mutation for both modes. For `feishu_app`, empty `webhookUrl`/`signingSecret` preserve credentials and non-empty credential fields are rejected; display name, enabled state and event kinds can change without reauthorization. Provider/delivery mode cannot change in place.

## 8. Web experience

The Notifications page gains a primary “绑定飞书私聊” section above the existing manual Webhook form.

- One click starts the flow; no name/event form is shown.
- `waiting` renders the exact short-lived URL in an admin-only, copyable control, an external-link button, and its expiry.
- TanStack Query polls the transient status only while `waiting` or `testing`, stops in terminal states, and does not run in the background tab. This is a bounded workflow poll, not a second persistent state store.
- Success invalidates the generated notification-channel list query and shows the new default channel.
- Failure/denial/expiry/cancel display stable localized text and allow generating a new URL.
- Manual enterprise-WeChat/Feishu Webhook creation remains available as an advanced/fallback section.
- Channel rows/cards display “授权用户私聊” for app mode and the existing host hint for Webhook mode.
- Add an edit modal for display name and event kinds, reusing the generated update mutation for either mode.
- Deleting an app channel uses a mode-specific confirmation explaining that only the Simplus binding is removed and the Feishu-side app remains.

The wide table and compact card expose the same status, target, events and actions. The page never writes the verification URL or credentials to browser persistent storage.

## 9. Compatibility and documentation

- Existing v12 Webhook rows, API mutations, test delivery, enable/disable and delete behavior remain unchanged.
- No existing user is migrated or forced to rebind.
- Add ADR 0025 for the durable one-click/private-DM/minimal-permission/local-only-unbind decision.
- Update `docs/product.md`, `docs/architecture.md`, the active MVP plan, handoff and stable troubleshooting codes without publishing any real account, URL, token or screenshot.
- No hardware, SMS, call, RF, modem-persistent action or HIL is involved.

## 10. Failure and rollback considerations

- Authorization or test failure creates no local channel. The externally created app may remain and is explicitly documented.
- A successful test followed by a database failure may deliver one test message without creating a local channel; the state reports persistence failure and never claims success.
- No automatic retry is performed for the test message or normal notification mutation. Explicit future tests create a new provider message.
- Provider outages do not affect existing Webhook channels or other Simplus functions.
- Rolling back the binary requires core v23 Down first. This removes only local app-channel rows; administrators must manually handle Feishu apps if desired.
