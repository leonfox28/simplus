# Extract notification delivery ports

## Goal

Resolve audit finding V-05 by keeping notification channel state and delivery
outcome policy in the application service while moving legacy Webhook endpoint
validation, provider signing/payload construction, HTTP execution and provider
response parsing behind one explicit bounded port and a concrete adapter
assembled in `cmd/simplusd`.

## Background and Evidence

- The layer audit classified V-05 as a confirmed Medium violation. At the
  audited call chain, `internal/application/notification/service.go:48-62`
  constructs a redirect-blocking `*http.Client`, while the active Webhook path
  at `service.go:203-263` decrypts credentials, builds provider protocol,
  performs raw HTTP, parses the response and updates delivery state.
- `internal/application/notification/feishu.go:60-80` and
  `binding.go:58-66` provide the intended local precedent: application state
  consumes fixed-purpose typed provider ports while `cmd/simplusd` assembles
  the concrete client.
- Existing enterprise WeChat and Feishu bot Webhooks are outbound-only. They
  support configuration, enable/disable, explicit tests, event filtering and
  delivery status; secrets are write-only and must not enter public responses,
  SSE, ordinary logs or errors (`docs/decisions/0005-*`).
- Feishu private-message application channels are a separate delivery mode.
  Their binding/test-before-persist behavior and concrete client remain intact,
  and legacy Webhook rows/credentials must remain compatible
  (`docs/decisions/0025-*`).

## Requirements

- R1. `internal/application/notification` owns a narrow `WebhookPort` plus
  bounded provider, target, delivery-request, result and outcome values. The
  port exposes only target validation and text delivery for the two supported
  providers; it must not accept caller-selected headers, methods, bodies,
  redirects or arbitrary HTTP options.
- R2. Move exact Webhook authority/path validation, URL normalization,
  WeCom/Feishu payload and signature generation, redirect refusal, timeout,
  headers, HTTP execution, response-size limiting and explicit provider-success
  parsing into a concrete `internal/notificationwebhook` adapter. The adapter
  must revalidate every delivery target and fail closed.
- R3. Keep provider/name/event business validation, secret labels and
  encryption/decryption, channel persistence, event filtering, per-channel
  iteration, delivery-state transitions and best-effort failure recording in
  the application service.
- R4. Replace the hidden Webhook HTTP default with an explicit notification
  dependency contract. `notification.New` must require Store, SecretCipher and
  WebhookPort dependencies, reject nil and typed-nil inputs with a stable
  configuration error, and never return a partially usable Service.
- R5. `cmd/simplusd` constructs the concrete Webhook adapter, injects it into
  the application service and handles constructor failure by logging a bounded
  configuration error, closing stores and exiting non-zero. Feishu registrar
  and messenger configuration remains the existing separate explicit step.
- R6. Preserve delivery outcome semantics: network/redirect/transport failure
  records `failed / DELIVERY_NETWORK_FAILED`; non-2xx, read/size, malformed or
  explicit provider rejection records `failed / DELIVERY_REJECTED`; success
  requires HTTP 2xx plus an explicit provider zero code and then persists
  `success`. Pre-dispatch validation/decryption errors do not invent a delivery
  record, failure-recording errors remain secondary, and success-recording
  errors remain visible.
- R7. Adapter errors must be bounded and credential-safe. They may classify
  invalid request, network failure or provider rejection, but must not contain
  a Webhook URL/key, signing secret, raw provider body or redirect target.
- R8. Preserve the existing supported protocol shapes: 15-second timeout;
  redirects refused; `Content-Type: application/json`; `User-Agent: Simplus`;
  64 KiB response limit; WeCom official hostname/send path with a non-empty
  `key`; Feishu official hostname and bot-hook path with no query; optional
  Feishu signing secret up to 512 bytes; messages remain bounded to 4000 runes.
  Preserve the legacy parser's explicit-port handling and WeCom extra-query
  handling rather than tightening accepted stored credentials in this refactor.
- R9. Preserve OpenAPI/Web behavior, HTTP error mapping, domain/storage rows,
  encryption labels/ciphertexts, migrations, event kinds, Feishu application
  binding and generated outputs. This task introduces no provider or delivery
  feature.
- R10. Move synthetic HTTP/provider assertions to the concrete adapter and use
  an application fake port to prove validation, credential handoff, event
  filtering, outcome-to-persistence mapping and constructor failures. Tests
  must send no external notification.

## Acceptance Criteria

- [ ] AC1. The notification Service contains no `http.Client`, raw Webhook
  request/response handling, provider JSON/signature function or official
  endpoint/path validation; those behaviors exist only in the concrete adapter.
- [ ] AC2. The application exposes one bounded typed Webhook port and an
  error-returning constructor that rejects every missing/typed-nil mandatory
  dependency.
- [ ] AC3. `cmd/simplusd` explicitly constructs and injects the Webhook adapter,
  handles application configuration failure and preserves the existing Feishu
  client composition.
- [ ] AC4. Application fake-port tests prove target validation, decrypted
  credential/message/timestamp handoff, event filtering, success, network
  failure, rejection, invalid adapter result and persistence ordering/mapping.
- [ ] AC5. Adapter tests prove both provider payloads/signature, exact allowed
  target shapes, per-delivery revalidation, headers, redirect/transport
  classification, bounded response parsing and credential-safe errors using
  synthetic transports only.
- [ ] AC6. Existing application integration, HTTP, storage and Feishu tests
  preserve public responses, encrypted data, legacy rows and application
  binding semantics without API/schema/generated changes.
- [ ] AC7. Focused race tests, broad safe Go tests, formatting, lint, ownership
  scans, task validation and `git diff --check` pass without external delivery,
  services, Compose, private-state access, HIL or hardware/network actions.

## Out of Scope

- Adding providers, generic Webhooks, arbitrary HTTP configuration, retries,
  queues, delivery scheduling, inbound events, remote control or new UI/API
  fields.
- Moving or redesigning the Feishu registration/private-message client beyond
  caller changes required by the new Service constructor.
- Database migrations, stored-secret re-encryption, Webhook credential format
  changes, live provider probing or real test notifications.

## Risks and Deferred Items

- The concrete adapter will implement application-owned request/result types;
  this is deliberate dependency inversion, not permission for the adapter to
  call application behavior. The boundary must be documented and protected by
  focused import/ownership scans.
- Existing authenticated ciphertext is the storage truth. This task does not
  rotate or revalidate stored channels in bulk; per-delivery adapter validation
  prevents an invalid stored target from becoming arbitrary HTTP.
