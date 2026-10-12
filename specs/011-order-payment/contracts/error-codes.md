# Contract: Error codes — Online order payment

Authoritative for what this module can answer with. `contracts/openapi.yaml` describes where each
appears; this file records what each means and which situations deliberately reuse a shared code.

## Codes this module adds

| Code | HTTP | Meaning | Retryable |
|---|---|---|---|
| `PAYMENT_ORDER_NOT_FOUND` | 404 | No order carries the identifier, or it belongs to another customer. The route never confirms another customer's order | No |
| `PAYMENT_ORDER_NOT_PAYABLE` | 409 | The order is not awaiting payment — already paid, cancelled, or still awaiting the artist's confirmation. A payment can only start for an order awaiting payment | No — re-read the order |
| `PAYMENT_NOT_FOUND` | 404 | (Customer read) the caller's order has no payment yet. The **webhook no longer uses this code**: a verified event for an unknown order code is acknowledged with **2xx** and records nothing (see below) | No |
| `PAYMENT_ALREADY_LIVE` | 409 | A live payment exists for this order but its link can no longer be returned (cancelled or expired at the provider), so a new one cannot be created while the stale row stands | No — reconcile |
| `PAYMENT_PROVIDER_ERROR` | 422 | The provider refused to create the link or was unreachable. **Nothing was created, and the order is unchanged** | Yes — after a moment |
| `PAYMENT_WEBHOOK_INVALID_SIGNATURE` | 401 | The webhook's signature does not verify, so the event is not from the provider. Nothing is applied | No — never retry unverified |

### Why these codes are not application errors

Each names a situation the customer or operator must act on differently: the order is not payable, the
provider is unavailable, the webhook is not authentic, or the payment is unknown. Only
`PAYMENT_PROVIDER_ERROR` is retryable; a refusal that has no payment to attach to (an unverified
webhook, or an unknown order code) records nothing and only logs.

### On the webhook's refusals

An unverified webhook answers `PAYMENT_WEBHOOK_INVALID_SIGNATURE` (401) and applies nothing — it is not
a client error but a failed authenticity check, so it is this module's code rather than the shared
`UNAUTHENTICATED` (which is about a session). A verified webhook for an unknown order code is
**acknowledged with 2xx** and **records nothing** (there is no payment row to attach a record to); it
never invents a payment, and only a warning is logged for the operator (research D7). This is required
so PayOS accepts the webhook URL when it registers it, and so the provider stops retrying an event it
cannot use.

## Codes deliberately reused

| Situation | Code | Why not a new code |
|---|---|---|
| A webhook body that does not parse, or carries an unknown field | `MALFORMED_REQUEST` (400) | The shared decoder owns this |
| `status` outside its permitted values, or `page`/`pageSize` outside range | `VALIDATION_ERROR` (400) | Pagination/filter validation is a foundation concern |
| No token, or an expired one | `UNAUTHENTICATED` (401) | The foundation owns session validity |
| A customer's session reaches the operator's payment list | `FORBIDDEN` (403) | Role-based; the foundation established the code |
| Something unexpected failed | `INTERNAL_ERROR` (500) | Never describe an internal failure in payment terms |
| Too many requests | `RATE_LIMITED` (429) with `Retry-After` | The shared limiter answers this |

**On the webhook and rate limiting**: the webhook is public and retried by the provider, so the shared
request limit applies to the route but a handled event answers `2xx` quickly (research D10), so the
provider stops retrying instead of being throttled.

## Situations that deliberately have no code

| Situation | Why there is no code |
|---|---|
| A verified confirmation whose amount differs from the payment | Not an error answer: the payment becomes `UNMATCHED` (409 would be wrong — the confirmation is valid, just not applicable) and the webhook still answers `2xx`, because refusing it would make the provider retry forever |
| A verified confirmation for an order that was edited, cancelled or expired | Recorded as `UNMATCHED` and answered `2xx` for the same reason |
| A verified confirmation for an unknown order code (e.g. an orphaned link, or PayOS's registration sample) | It is not an error answer: the webhook is acknowledged with `2xx` so the provider stops retrying, nothing is applied and nothing is recorded (no payment row exists), and a warning is logged for the operator (research D7) |
| The same confirmation arriving twice | The second one is a no-op success: the payment is already `PAID`, so nothing applies and the webhook answers `2xx` |
| The provider refusing to cancel a link when the order was cancelled | Not an error answer: the order's cancellation succeeded (module 07) and the failure is logged for the operator (research D9) |
| Refunding a payment | There are no refunds in V1.0 (ADR 015 §5 D5); the operator resolves a stranded payment outside the system |
