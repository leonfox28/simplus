# V-05 planning research

## Confirmed current call chain

- `internal/application/notification/service.go:48-62` stores a concrete
  `*http.Client`; `New(store, secrets)` creates the 15-second,
  redirect-refusing client without an explicit dependency.
- `service.go:75-167` combines application settings/encryption with exact
  provider URL/path knowledge. Create persists the normalized URL ciphertext
  and hostname hint; Update preserves existing ciphertext when replacement
  input is empty.
- `service.go:181-220` owns test/event filtering/channel dispatch.
- `service.go:224-263` decrypts the legacy Webhook URL/signing secret, builds
  and sends raw HTTP, interprets the provider result and records delivery
  status in one method.
- `service.go:294-355` owns WeCom/Feishu payloads, Feishu signing, explicit
  success-code parsing and official authority/path validation.
- `cmd/simplusd/main.go:218-223` constructs the Service without a Webhook
  adapter, then correctly constructs/injects `FeishuClient` through the
  registrar/messenger ports.
- `internal/application/notification/feishu.go:60-80` and
  `binding.go:58-65` are the existing explicit provider-port precedent.

## Exact behavior to preserve

- Providers are `wecom` and `feishu`; Webhook messages are outbound-only.
- URL rules: HTTPS/no userinfo/no fragment; WeCom official `Hostname()`/exact
  send path/non-empty `key`; Feishu official `Hostname()`/non-empty bot-hook
  suffix/no query. The existing parser accepts explicit ports and extra WeCom
  query values; this refactor preserves that compatibility. Public views expose
  hostname only.
- Feishu signing is optional, limited to 512 bytes, and uses decimal Unix time
  plus the existing HMAC-SHA256/base64 formula.
- POST headers are `Content-Type: application/json` and `User-Agent: Simplus`.
  Client timeout is 15 seconds and redirects are refused.
- Responses are bounded to 64 KiB and require HTTP 2xx plus an explicit
  numeric provider zero code (`errcode` or `code`).
- Transport/redirect failure records `DELIVERY_NETWORK_FAILED`; provider,
  body, status and read rejection record `DELIVERY_REJECTED`; both failure
  record writes are best effort. Successful delivery requires successful
  state persistence. Pre-dispatch decrypt/build/request failures do not record
  an attempt.
- Secret labels, ciphertexts, legacy table, public HTTP errors and Feishu app
  binding/application delivery remain unchanged.

## Test evidence and gaps

- `service_test.go:64-99` injects a raw RoundTripper into the application
  Service and asserts both provider payload families. These assertions belong
  at the concrete adapter after the split.
- `service_test.go:101-127` proves event filtering and arbitrary-host rejection
  but does not exercise network/rejection/persistence outcome mapping.
- Binding tests share an HTTP test helper for the separate Feishu client; that
  helper must remain available when Webhook tests move.
- `integration_test.go` proves Webhook/signing material is encrypted in SQLite
  and omitted from views. It needs only explicit adapter construction; no
  external delivery is necessary.

## Planning conclusions

- Secret labels and decryption remain application-owned; the adapter receives
  plaintext only in one bounded in-memory request.
- Exact target validation moves with provider protocol and is invoked both at
  create/update and again immediately before delivery.
- A typed outcome keeps persistence/status policy in the Service without
  requiring it to inspect transport errors.
- The adapter implements application-owned types as a deliberate dependency
  inversion. It does not import storage or call application behavior.
- Raw `http.Client.Do` errors may contain the credential-bearing URL. The new
  adapter must replace them with bounded stable errors instead of wrapping or
  logging raw transport/provider data.

## Historical context

`trellis mem search/extract` found the original layer-audit review session and
confirmed V-05 remained a Medium typed-port bypass. No additional product
decision beyond the committed audit report was recovered; part of the stored
inter-agent payload was encrypted and unavailable, so current source, tests,
ADRs and the checked-in audit remain authoritative.
