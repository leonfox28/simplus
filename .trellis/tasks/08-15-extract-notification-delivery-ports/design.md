# Design: Typed legacy Webhook delivery boundary

## 1. Ownership and data flow

The repaired create/update path is:

```text
HTTP -> notification Service business validation
     -> WebhookPort.ValidateTarget(provider, supplied URL)
     -> Service encrypts normalized URL/signing secret
     -> Store persists the unchanged legacy channel row
```

The repaired delivery path is:

```text
Service selects enabled/event-matching channel
  -> decrypts existing Webhook URL and optional signing secret
  -> WebhookPort.Deliver(bounded typed request)
       -> concrete notificationwebhook adapter validates + signs + POSTs + parses
  -> typed outcome
  -> Service records success/network-failed/rejected business state
```

The adapter owns no Store, SecretCipher, channel iteration, event policy or
delivery-state persistence. The Service owns no raw HTTP/provider wire shape.

## 2. Application contract

Use application-owned values and one consumer port:

```go
type WebhookProvider string

const (
    WebhookProviderWeCom  WebhookProvider = "wecom"
    WebhookProviderFeishu WebhookProvider = "feishu"
)

type WebhookTarget struct {
    URL  string
    Hint string
}

type WebhookDeliveryRequest struct {
    Provider      WebhookProvider
    URL           string
    SigningSecret string
    Message       string
    Timestamp     int64
}

type WebhookDeliveryOutcome string

const (
    WebhookDelivered      WebhookDeliveryOutcome = "delivered"
    WebhookNetworkFailed  WebhookDeliveryOutcome = "network_failed"
    WebhookRejected       WebhookDeliveryOutcome = "rejected"
)

type WebhookDeliveryResult struct {
    Outcome WebhookDeliveryOutcome
}

type WebhookPort interface {
    ValidateTarget(WebhookProvider, string) (WebhookTarget, error)
    Deliver(context.Context, WebhookDeliveryRequest) (WebhookDeliveryResult, error)
}

type Dependencies struct {
    Store     Store
    Secrets   SecretCipher
    Webhooks  WebhookPort
}

func New(Dependencies) (*Service, error)
```

`ErrDependenciesInvalid` is the stable constructor sentinel. Store, Secrets
and Webhooks are mandatory, including typed-nil rejection. `time.Now` remains
the default clock seam; Feishu binding ports remain explicitly configured
after construction.

The exact exported names may be tightened during implementation, but the
single required dependency shape, bounded fields and typed outcomes are
contractual.

## 3. Concrete adapter contract

`internal/notificationwebhook.Client` implements the application port and owns
the production `http.Client`. Construction sets a 15-second timeout and refuses
redirects. A compile-time assertion proves the client implements the port.

`ValidateTarget` preserves current normalization and restrictions:

- HTTPS, no userinfo and no fragment;
- WeCom: `url.Hostname()` equals `qyapi.weixin.qq.com`, exact
  `/cgi-bin/webhook/send`, non-empty `key`, with existing extra query values
  preserved;
- Feishu: `open.feishu.cn`, non-empty suffix below
  `/open-apis/bot/v2/hook/`, and no query;
- explicit URL ports retain the legacy `url.Hostname()` validation semantics;
  this boundary refactor does not silently reject existing authenticated rows;
- normalized URL plus hostname-only public hint; no credential in the hint or
  error.

`Deliver` revalidates the provider/target and bounded inputs before any HTTP,
then preserves the current protocol:

- WeCom JSON: `msgtype=text`, `text.content=<message>`;
- Feishu JSON: `msg_type=text`, `content.text=<message>`, with optional decimal
  timestamp and existing HMAC-SHA256/base64 signature calculation;
- POST with the exact content type and user agent;
- read at most 64 KiB plus one byte;
- require HTTP 2xx and explicit numeric `errcode=0` for WeCom or `code=0` for
  Feishu. Missing/malformed/nonzero codes fail closed.

The adapter returns stable credential-safe errors. It never returns or wraps a
raw `url.Error`, provider response body or URL-bearing redirect error.

## 4. Outcome and persistence matrix

| Adapter result | Service persistence | Returned behavior |
| --- | --- | --- |
| `delivered`, nil | record `success / ""`; update returned view | success-record failure remains visible |
| `network_failed`, bounded error | best-effort `failed / DELIVERY_NETWORK_FAILED` | return bounded delivery error |
| `rejected`, bounded error | best-effort `failed / DELIVERY_REJECTED` | return bounded delivery error |
| zero/unknown outcome or contradictory result | no invented delivery status | stable invalid-result error |
| target validation, decryption or request preflight error | no delivery record | return bounded error; public mapping remains existing behavior |

Failure-state persistence errors remain secondary to the primary external
failure, matching current behavior. A successful external delivery followed by
status persistence failure remains an error and is never blindly retried by
this task.

## 5. Composition and package direction

`cmd/simplusd` constructs `notificationwebhook.NewClient()`, injects it through
`notification.Dependencies`, handles constructor failure, then configures the
existing Feishu registrar/messenger and realtime callback.

The concrete adapter imports the application-owned value contract solely to
implement it. It does not invoke Service methods or depend on storage. This is
the intentional dependency-inversion edge; `cmd` remains the only concrete
assembly owner.

## 6. Compatibility and privacy

- No OpenAPI, HTTP route/error code, domain Channel field, SQLite migration,
  ciphertext label, Web view or generated file changes.
- Existing legacy Webhook ciphertext decrypts and is delivered through the new
  adapter without migration. Create/update continue to persist normalized URLs
  and hostname-only hints.
- Feishu application binding and delivery continue through
  `FeishuRegistrar`/`FeishuMessenger`; only Service construction call sites
  change.
- Plaintext URL/signing material exists only for the current delivery call. It
  must not be logged, formatted into returned errors, stored in task evidence
  or asserted verbatim in failure output.

## 7. Rollback and safety

Rollback is a source-only revert. There is no data migration or runtime state
to undo. Automated checks use fake ports, injected transports and temporary
SQLite only; they do not contact official provider hosts or send a message.
