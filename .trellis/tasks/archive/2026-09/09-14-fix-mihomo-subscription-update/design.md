# Design: Mihomo subscription refresh compatibility

## 1. Current failure path

```text
Mihomo page “更新”
  -> POST /api/v1/mihomo/subscriptions/{id}/refresh
  -> SubscriptionService.Refresh
  -> HTTPS GET with User-Agent: Simplus
  -> provider HTTP 403
  -> raw Go HTTP error / string-based HTTP classification
  -> 502 MIHOMO_SUBSCRIPTION_REFRESH_FAILED
  -> generic Web fallback text
```

The running services and existing selected artifact remain healthy. A privacy-safe live comparison proved that only the client identifier changes the response class: `Simplus` receives 403, while the minimal `mihomo` identifier receives 200.

## 2. Request compatibility contract

Define one package constant for the subscription download user agent and set it to `mihomo`. Keep the existing `Accept` header, custom transport, public-address resolution check, HTTPS-only redirect validation, three-redirect limit, 30-second client timeout, 5 MiB body limit and node/config validation unchanged.

The change is provider-neutral: there is no hostname branch, credential transformation, retry loop or fallback request. One administrator action still results in one application refresh attempt.

## 3. Typed and privacy-safe refresh failure

Add an application-owned `SubscriptionRefreshError` carrying only the stable refresh status code already persisted on the subscription, such as `SUBSCRIPTION_FETCH_FAILED`, `SUBSCRIPTION_PARSE_FAILED` or `SUBSCRIPTION_CONFIG_VALIDATION_FAILED`.

Its `Error()` value is bounded and derived only from that stable code. It must not wrap the original `http.Client` error, URL, provider body, node content, command output or filesystem path. `refreshFailed` first records the existing failed status; if that persistence operation itself fails, the storage error is returned instead of falsely reporting a successfully recorded refresh failure.

The HTTP mapper uses `errors.As` for `SubscriptionRefreshError` and returns the existing HTTP 502 / `MIHOMO_SUBSCRIPTION_REFRESH_FAILED` response. It logs only the stable refresh code. Invalid/not-found and unknown persistence mappings remain 400/404 and 500 respectively. No OpenAPI or generated source changes are required.

## 4. Browser behavior

Add a dedicated message for `MIHOMO_SUBSCRIPTION_REFRESH_FAILED` in the existing bounded `codeMessages` map. The message tells the administrator that the subscription source rejected the request or returned unusable content and recommends checking validity before retrying. It does not display the upstream status, URL, token, host, response body or raw backend detail.

TanStack Query mutation behavior stays unchanged: no automatic retry, row-scoped busy state is cleared in `onSettled`, existing subscription snapshots remain authoritative, and a successful mutation invalidates the generated Mihomo query keys.

## 5. Test boundaries

- Application tests use a synthetic public HTTPS URL plus an injected `RoundTripper`; no network is opened. They assert the exact User-Agent, success persistence/artifact calls, 403 and transport failure state, stage-specific failure codes, and absence of synthetic secret markers from returned errors.
- HTTP tests invoke the error mapper with a typed refresh error and an ordinary storage error, asserting the existing 502/500 response contract and privacy-safe logging.
- Web tests construct an `ApiClientError` with `MIHOMO_SUBSCRIPTION_REFRESH_FAILED` and assert the dedicated Chinese message. The existing Mihomo page test continues to protect URL-hint-only rendering.

## 6. Compatibility, rollout and rollback

There is no schema migration, data rewrite, generated API change, core update, process restart protocol change or hardware action. Existing last-known-good subscription artifacts and running Mihomo state are untouched by the source change.

Rollout is the normal application/Web release. Rollback is a source-only revert. A provider may still return 403 for an expired token, account restriction or allowlist; in that case the corrected UI reports a subscription-source failure without attempting to bypass provider policy.
