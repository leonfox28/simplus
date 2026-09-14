# Implementation plan

## 1. Application refresh contract

- [x] Add the `mihomo` subscription User-Agent constant and use it for every subscription fetch.
- [x] Add the bounded `SubscriptionRefreshError` type and stable code access.
- [x] Route fetch, response-size, parse and artifact build failures through the typed error without wrapping sensitive causes.
- [x] Return a storage error if persisting the failed refresh state fails.
- [x] Add synthetic success, 403/transport privacy, parse and artifact failure tests in `internal/application/mihomo`.

Validation:

```bash
go test -count=1 ./internal/application/mihomo
go test -race -count=1 ./internal/application/mihomo
```

## 2. HTTP boundary

- [x] Replace Mihomo refresh error-string inspection with `errors.As` against the application type.
- [x] Log only the bounded refresh code and retain the existing 502 / `MIHOMO_SUBSCRIPTION_REFRESH_FAILED` response.
- [x] Add focused handler/error-mapper tests for typed refresh and ordinary persistence errors, including log privacy.

Validation:

```bash
go test -count=1 ./internal/api/httpapi
go test -race -count=1 ./internal/api/httpapi
```

## 3. Web error guidance

- [x] Add the dedicated `MIHOMO_SUBSCRIPTION_REFRESH_FAILED` message to `web/src/api/errors.ts`.
- [x] Add a focused visible-message test using only a synthetic `ApiClientError`.

Validation:

```bash
corepack pnpm --dir web test
corepack pnpm --dir web typecheck
corepack pnpm --dir web build
```

## 4. Integrated quality gate

- [x] Run format and generated-drift checks.
- [x] Run supported lint and broad test targets in proportion to the cross-layer change.
- [x] Confirm no private URL, token, provider body, raw log or temporary database entered the worktree.
- [x] Update the backend/API specification with the newly established User-Agent and typed-error privacy contract.

Validation:

```bash
make check-format
make verify-generated
make lint
make test
git diff --check
git status --short
```

## Risk and rollback points

- Keep the request change to one constant; do not add provider-specific branches or retries.
- Do not change the OpenAPI contract unless implementation proves the existing 502 response cannot express the requirement.
- Do not run a production refresh, restart Mihomo, change Line egress, access hardware or perform HIL validation.
- Roll back all source changes together; there is no data migration or runtime rollback step.
