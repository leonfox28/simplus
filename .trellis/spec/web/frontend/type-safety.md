# Type Safety

> Compile-time and runtime contracts at the browser/API boundary.

## Compiler Contract

`web/tsconfig.json` uses strict TypeScript, ES2022, the React JSX transform,
bundler module resolution, and `@/*`. The required check is:

```bash
corepack pnpm --dir web typecheck
```

Handwritten application code does not use `any`, blanket suppression, or broad
casts to bypass a network boundary. Catch values remain `unknown` until
normalized.

## Generated Contract

`api/openapi.yaml` is the source. Hey API generates the following under
`web/src/api/generated/`:

- `types.gen.ts`: request/response/discriminant types;
- `zod.gen.ts`: request, response, and definition schemas;
- `sdk.gen.ts`: Fetch SDK with request and response validators;
- `@tanstack/react-query.gen.ts`: query keys/options, infinite options, and
  mutation options;
- client/core runtime support.

Import generated types with `type` and generated operations/options by name.
Do not create a second interface matching an OpenAPI response or manually edit
generated output.

The generated client invokes Zod validators for their validation side effect.
It intentionally continues to return decoded JSON rather than Zod-transformed
values; this avoids converting OpenAPI `int64` numbers to `bigint` while the
generated TypeScript response contract is `number`.

## Runtime Boundary

`configureApiClient()` installs one generated client interceptor.
`runtimeFetch()` is the sole Fetch implementation and enforces:

- same-origin credentials and base URL;
- `Accept: application/json`;
- CSRF header on mutating business APIs, excluding login/setup;
- endpoint-aware bounded deadlines;
- caller abort propagation;
- stable `ApiClientError` kinds/codes for timeout, abort, network, HTTP,
  invalid request, and invalid success response;
- session-expiry notification on HTTP 401.

Successful JSON must pass the generated response validator. Invalid request
Zod errors become non-retryable `REQUEST_INVALID`; malformed successful
responses become non-retryable `API_RESPONSE_INVALID`. Pages render
`displayApiError(error)`, never raw transport exception text.

## Domain Validation Beyond OpenAPI Shape

Keep relational/cross-reference checks that OpenAPI cannot express in
`web/src/api/hardwareSchema.ts`. Current topology validation proves exact
private-safe fields, unique IDs, valid references, generations, capability
subsets, SIM/Profile relations, and resource-group consistency.

`decodeRealtimeEvent` adds exact-key and unique-topic checks around the
generated Zod schema. Do not place those rules in page code.

## Type Placement

- Generated API models stay in `api/generated`.
- Runtime/network error types stay in `api/errors.ts`.
- Cross-field public response checks stay in `api/hardwareSchema.ts` or a
  similarly narrow API module.
- Form values and view-only row types stay near the page.
- Reusable feature transforms/types stay in `calls/`, `messages/`, or
  `mihomo/` with focused tests.
- Use indexed access, `Pick`, and generated discriminants instead of copying
  fields. Use `Record<GeneratedUnion, ...>` or exhaustive switches for status
  presentation.

## Validation Matrix

| Boundary failure | Browser result | Retry |
| --- | --- | --- |
| Request does not satisfy generated Zod schema | `REQUEST_INVALID` | No |
| Network failure | `NETWORK_UNAVAILABLE` | Query only, bounded |
| Client deadline | `API_TIMEOUT` | Query only when marked retryable |
| Caller abort | `REQUEST_ABORTED` | No |
| HTTP `ApiError` | Preserve bounded code/status/reference | Per server flag for queries |
| HTTP 401 | Normalize error and notify session expiry | No mutation retry |
| 2xx body fails response validation | `API_RESPONSE_INVALID` | No |
| Hardware shape passes primitives but breaks references | Reject as invalid response | No |

## Good / Base / Bad Cases

- Good: add a field in OpenAPI, regenerate Go/Web, consume its generated type,
  and add response/request regression tests.
- Base: a page defines `type DialValues` for its form, then passes the values to
  a generated mutation whose Zod validator enforces the API contract.
- Bad: cast `await response.json()` to `ManagedLine[]` or duplicate an API
  response interface in a page.

## Tests Required

- Runtime tests for CSRF exemptions/inclusion, same-origin credentials,
  deadlines, abort, transport failure, invalid request, invalid response, and
  401 session expiry.
- Generator drift after every OpenAPI/config change.
- Hardware schema tests for exact keys, private-field rejection, duplicate IDs,
  broken references, and valid synthetic topology.
- Typecheck plus focused page tests for new discriminant/state mappings.

## Wrong vs Correct

```ts
// Wrong
const lines = await fetch('/api/v1/lines').then((response) => response.json()) as ManagedLine[]

// Correct
const query = useQuery(listManagedLinesOptions())
const lines = query.data?.lines ?? []
```

## Avoid

- Hand-editing generated files or exporting parallel API response types.
- Treating TypeScript casts or HTTP 2xx as runtime proof.
- Widening generated enums to `string`.
- Returning raw browser/server exception messages to the UI.
- Using response-transform output whose runtime type disagrees with generated
  TypeScript types.
