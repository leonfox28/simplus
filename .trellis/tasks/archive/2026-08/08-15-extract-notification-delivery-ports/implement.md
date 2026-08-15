# Implementation Plan

## Phase A — Define the application-owned boundary

- [x] Add bounded provider/target/request/result/outcome values and the
  `WebhookPort` consumer interface.
- [x] Replace `notification.New(store, secrets)` with one validated dependency
  constructor and stable missing/typed-nil configuration error.
- [x] Refactor create/update to delegate exact target validation and refactor
  delivery to translate typed outcomes into the unchanged persistence policy.
- [x] Remove Webhook HTTP, signing, JSON and provider-response code from the
  Service while retaining secret labels, encryption/decryption and state
  ownership.

## Phase B — Implement the concrete provider adapter

- [x] Add `internal/notificationwebhook` with the redirect-blocking 15-second
  client, exact target rules, both payload/signature shapes, bounded response
  parser and credential-safe stable errors.
- [x] Revalidate every delivery request and return typed delivered/network/
  rejected outcomes without exposing URLs, keys, secrets or provider bodies.
- [x] Move/expand synthetic transport tests to cover exact allowed/rejected
  targets, payloads, headers, redirects, transport/read/size/status/JSON/code
  failures and per-delivery revalidation.

## Phase C — Compose and update callers

- [x] Construct the concrete adapter in `cmd/simplusd`, inject it through the
  dependency object and handle constructor failure before later assembly.
- [x] Update notification service, binding, integration and any HTTP callers to
  the explicit constructor without changing Feishu configuration behavior.
- [x] Add application fake-port tests for constructor validation, target
  normalization handoff, decrypted bounded request contents, event filtering,
  all typed outcomes, invalid adapter results and persistence failures.

## Phase D — Independent verification and contract capture

- [x] Run focused notification/adapter/cmd/http/storage tests, then focused race
  coverage and supported broad Go tests/vet/lint.
- [x] Run ownership/privacy scans proving the Service has no raw Webhook HTTP
  or provider protocol and adapter errors cannot include credential material.
- [x] Confirm no OpenAPI, migration or generated drift; validate task context,
  formatting and `git diff --check`.
- [x] Complete an independent Trellis check and update backend
  boundary/storage/quality specs with the verified contract.
- [x] Commit the completed task as `04e1770`; archive and record it through the
  Trellis finish-work flow.

## Validation Commands

Safe commands may include:

```bash
go test ./internal/application/notification ./internal/notificationwebhook ./cmd/simplusd ./internal/api/httpapi ./internal/storage/sqlite
go test -race ./internal/application/notification ./internal/notificationwebhook ./cmd/simplusd
go test ./cmd/... ./internal/...
make check-format
make lint
make verify-generated
make check-docs
python3 ./.trellis/scripts/task.py validate .trellis/tasks/08-15-extract-notification-delivery-ports
git diff --check
```

## Prohibited Validation

Do not contact provider endpoints, send test notifications, start services or
Compose, read private runtime data, or run hardware discovery, HIL, SMS/calls,
RF, SIM/eUICC or other device/network actions.

## Implementation Validation Notes

- The independent check fixed one adapter consistency issue: `Deliver` now
  executes the normalized `WebhookTarget.URL` returned by its per-delivery
  validation instead of revalidating one string and passing the original to
  `net/http`. A synthetic regression covers whitespace normalization without
  external delivery.
- Focused compatibility checks passed for notification, Webhook adapter,
  `cmd/simplusd`, HTTP API and SQLite packages.
- Uncached focused race passed with
  `go test -count=1 -race ./internal/application/notification ./internal/notificationwebhook ./cmd/simplusd`.
- Uncached broad safe Go scope passed with
  `go test -count=1 ./cmd/... ./internal/...`.
- Repository gates: `make check-format`, `make lint`, `make verify-generated`, and `make check-docs`.
- Explicit `go vet ./cmd/... ./internal/...` also passed.
- Ownership/privacy scans found no raw Webhook HTTP, provider JSON/signing, or
  official target rules in `service.go`; the adapter has no storage dependency
  and its parse/request/network/rejection paths return exact stable errors
  without wrapping URLs, transport errors, redirect targets or provider bodies.
- The follow-up spec check synchronized the seven-section legacy Webhook
  scenario and backend index/directory/storage/testing guidance with the exact
  names, bounds, normalized execution target, Feishu signature, v1
  ciphertext/hint preservation, outcome matrix and package direction.
- `python3 ./.trellis/scripts/task.py validate .trellis/tasks/08-15-extract-notification-delivery-ports` and `git diff --check` passed.
- No provider endpoint, service, Compose, private runtime state, hardware, HIL, RF, SIM/eUICC, SMS, or call action was used.
