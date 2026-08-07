# Type Safety

> Compile-time and runtime contracts at the browser/API boundary.

## Compiler Contract

`web/tsconfig.json` uses TypeScript 5.7 with `strict: true`, ES2022, the React JSX
transform, bundler module resolution, and the `@/*` source alias. The package
script supplies `--noEmit`; the normal check is:

```bash
corepack pnpm --dir web typecheck
```

There is no `any`-based application convention and no `@ts-ignore` or
`@ts-expect-error` in current handwritten frontend source. API boundary helpers
accept external data as `unknown`, and `catch` blocks in the larger pages narrow
their caught values explicitly.

## OpenAPI Is the Static Source of Truth

`web/src/api/schema.d.ts` is generated from `api/openapi.yaml`. Most shared API
types in the client directly alias generated schemas:

```ts
// web/src/api/client.ts
import type { components } from './schema'

export type ManagedModem = components['schemas']['ManagedModem']
export type ManagedLine = components['schemas']['ManagedLine']
export type SendSMSRequest = components['schemas']['SendSMSRequest']
```

`web/src/api/hardwareSchema.ts` follows the same pattern for topology types.
Regenerate the declaration with `corepack pnpm --dir web generate:api`; do not
edit `schema.d.ts` by hand. The root `make verify-generated` target checks that
generated contracts are current and that generation does not change unrelated
worktree content.

There are two current exceptions in `api/client.ts`: the handwritten
`MihomoDashboardStatus` duplicates a generated schema, and `SMSHistory` repeats
the shape of `SMSMessageListResponse` after validating it. They are existing
exceptions, not a general pattern; do not add another parallel response type.

## Static Types Do Not Validate Network Data

The transport helper in `web/src/api/client.ts` deliberately returns
`Promise<unknown>`. Every exported operation that returns successful JSON
validates it before returning typed data:

```ts
// web/src/api/client.ts
export async function getSystemHealth(signal?: AbortSignal): Promise<HealthResponse> {
  const health = await requestJSON(
    '/api/v1/system/health',
    signal,
    'HEALTH_NETWORK_UNAVAILABLE',
    'HEALTH_RESPONSE_INVALID',
    'HEALTH',
  )
  if (!isHealthResponse(health)) throw new Error('HEALTH_RESPONSE_INVALID')
  return health
}
```

Runtime guards check more than primitive shapes:

- `hasExactKeys` rejects omitted and extra fields throughout topology validation
  in `api/hardwareSchema.ts`; `api/client.ts` uses it for Line/VoWiFi shapes and
  selected mutation requests, while other client guards validate required
  fields without rejecting every extra response key.
- Domain guards constrain IDs, string lengths, enum values, dates, array sizes,
  and dependent fields. `isVoWiFiLineState`, `isManagedModem`, `isSMSMessage`,
  and `isCall` are representative.
- `isHardwareTopologyResponse` validates unique IDs, generations, references,
  capability subsets, SIM/Profile relations, and resource-group consistency.
- Domain mutations with constrained IDs or cross-field invariants validate
  inputs before dispatch. Tests in `api/client.test.ts` assert that invalid
  Line, egress, SMS, and Call requests do not call `fetch`. Simpler auth/setup
  bodies currently rely on their generated request types and server validation.

The project uses hand-written type guards rather than Zod, Yup, or another
runtime schema library. Keep new validation at the API module boundary and use
stable uppercase error codes like the existing `*_RESPONSE_INVALID` and
`*_REQUEST_INVALID` values.

## Type Placement and Derivation

- Export shared request/response types from `api/client.ts`; page modules import
  them with `type` specifiers.
- Keep view-only types next to their page: `CountryOption` and `LineRow` in
  `Lines.tsx`, and typed ModalForm value objects in `Mihomo.tsx`.
- Keep feature types beside feature logic: `SMSConversation` in
  `messages/conversations.ts` and `SimulatorMediaState` in
  `calls/simulatorMedia.ts`.
- Derive subsets instead of copying fields. `SIMPresenceTag`-style components
  use indexed access (`ManagedModem['simPresence']`), message presentation uses
  `Pick<SMSMessage, 'status' | 'errorCode'>`, and test fixture helpers accept
  `Partial<SMSMessage>`.
- Use `readonly` when a helper promises not to mutate input. The signature
  `sortSMSMessagesForDisplay(messages: readonly SMSMessage[])` is backed by a
  test that verifies it returns a copied array.

## Exhaustive Domain Mappings

Use a generated union as the key type for presentation maps so additions fail
type-check until the UI handles them:

```ts
// web/src/pages/Lines.tsx
const lineStateLabels: Record<ManagedLine['state'], string> = {
  ready: '就绪',
  'modem-offline': '模组离线',
  'sim-unavailable': 'SIM / Profile 不可用',
}
```

`candidateReasonLabels`, `readinessLabels`, and `voWiFiStateLabels` in
`Lines.tsx` follow this shape. `messages/status.ts` uses an exhaustive switch
over `SMSMessage['status']`. `capabilityLabels` in `Modems.tsx` is different: its
`Array<[CapabilityKey, string]>` type constrains each selected key, but the list
currently shows only a subset of the capability object and is not exhaustive.

## Assertions and Narrowing

Assertions are localized to code performing runtime checks, or to tests adapting
a framework/mock type. Examples are `Partial<T>` candidates in `api/client.ts`,
the topology double assertion after exact object/array/member checks and before
cross-reference validation in `api/hardwareSchema.ts`, and `RequestInit`
inspection in `api/client.test.ts`.
Negative tests use `as never` specifically to prove that runtime validation
rejects a value TypeScript would normally prevent.

Do not use a broad assertion to make an unvalidated response convenient.
Prefer a predicate (`value is T`), discriminant check, `instanceof Error`, or
an `unknown` intermediate. Page error helpers in `Lines.tsx` and `Modems.tsx`
demonstrate caught-value narrowing.

## Current Form Typing

Typing is mixed at form boundaries. Complex modal forms in `Mihomo.tsx` use
`ModalForm<CreateSubscriptionValues>` and `ModalForm<EditSubscriptionValues>`;
several compact `ProForm` pages let the library infer a loose values object.
Do not claim all existing forms are generically typed. When changing a complex
form, follow the typed Mihomo example instead of introducing a duplicate API
response interface.

## Avoid

- Do not hand-edit `schema.d.ts` or add another duplicate of a generated OpenAPI
  response type; the dashboard status and SMS history aliases above are current
  exceptions.
- Do not treat a successful HTTP status or a TypeScript cast as runtime proof.
- Do not add `any`, blanket suppressions, or double assertions outside a
  boundary that has established the needed runtime shape.
- Do not widen API discriminants to unconstrained `string`; keep label maps,
  actions, and state transitions keyed by the generated unions.
- Do not bypass request validation or send fields outside the exact API
  contract; `api/client.test.ts` includes regression cases for legacy fields.
