## Bug Analysis: Mihomo subscription refresh rejected and misreported

### 1. Root Cause Category

- **Category**: E - Implicit Assumption, compounded by B - Cross-Layer
  Contract and D - Test Coverage Gap.
- **Specific Cause**: The subscription downloader assumed the product-specific
  `Simplus` User-Agent was accepted by subscription providers. The affected
  provider rejected that identifier but accepted the minimal Mihomo client
  identifier. The application then returned raw, prose-based errors, HTTP
  classified them with string matching, and Web had no message for the stable
  refresh code, so one provider compatibility failure appeared as a generic
  management-service failure.
- **Evidence and confidence**: Initial hypotheses were service availability
  (45%), subscription/account rejection (35%), and request compatibility (20%).
  Healthy service state weakened the first; repeated bounded 403 evidence kept
  the latter two plausible; a user-authorized, status-only comparison in which
  only User-Agent changed made request compatibility the discriminating cause.
  Confidence is above 95% because the old identifier consistently failed while
  the new exact identifier consistently succeeded, without reading or retaining
  response content.

### 2. Why Fixes Failed (if applicable)

1. **Generic browser fallback**: It described the HTTP symptom as a management
   service problem and provided no clue that the subscription source rejected
   or could not supply usable content.
2. **String-based HTTP classification**: It recognized only selected current
   error prose, coupled layers to formatting, and encouraged wrapping raw HTTP
   errors that can contain a credential-bearing URL.
3. **Missing refresh fixtures**: Parser tests existed, but no synthetic service
   test asserted the outbound User-Agent, stage-specific failure state,
   artifact call, or complete error-string privacy.

No earlier source fix was deployed during this task; the investigation moved
from health evidence to a discriminating request comparison before editing.

### 3. Prevention Mechanisms

| Priority | Mechanism | Specific Action | Status |
| --- | --- | --- | --- |
| P0 | Architecture | Return `SubscriptionRefreshError` with only a stable stage code; map it with `errors.As` and log only that code. | DONE |
| P0 | Test coverage | Assert exact `User-Agent: mihomo`, success publication boundaries, each failure stage, persistence failure, and secret-free error/log strings with synthetic fixtures. | DONE |
| P0 | Browser contract | Map the existing public refresh code to source-validity guidance while retaining mutation settlement behavior. | DONE |
| P1 | Documentation | Record the exact fetch, SSRF/resource, persistence and error matrix in the backend application-boundary spec. | DONE |
| P1 | Review checklist | Add a credential-bearing external-fetch checklist to the cross-layer guide. | DONE |

### 4. Systematic Expansion

- **Similar Issues**: A focused scan found other external HTTP clients. The
  credential-bearing notification paths already reduce transport/provider
  failures to bounded sentinels; official Mihomo release downloads are a
  separate public-source contract. No unrelated source change is justified by
  this task.
- **Design Improvement**: Keep provider compatibility at one application-owned
  request constant and keep raw external errors inside the fetch boundary.
  Stage details cross layers only as stable codes.
- **Process Improvement**: For credential-bearing fetch failures, compare
  service health, bounded upstream status and one discriminating request
  attribute before changing behavior. Add an end-to-end error-flow trace and
  privacy assertions to the task plan.

### 5. Knowledge Capture

- [x] Added the seven-section executable contract to
  `.trellis/spec/core/backend/application-boundaries.md`.
- [x] Added the reusable external-fetch checklist to
  `.trellis/spec/guides/cross-layer-thinking-guide.md`.
- [x] Confirmed this repository has no `src/templates/markdown/spec/` mirror,
  so there is no generated template copy to synchronize.
- [x] Kept the analysis in this task and covered the root fix here; no separate
  issue or feature ticket is needed.
