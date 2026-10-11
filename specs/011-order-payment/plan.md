# Implementation Plan: Online Order Payment

**Branch**: `011-order-payment` | **Date**: 2026-10-11 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/011-order-payment/spec.md`

## Summary

Module 08 Payment, the last module of V1.0: a customer pays an order online through **PayOS**. The
customer starts a payment for an order awaiting payment; the shop creates a PayOS payment request for
the order's exact total and returns the checkout link/QR. PayOS notifies the shop through a signed
**webhook**; the shop verifies the signature, matches the order and amount, and applies the result
**exactly once** — the order becomes **paid** and the held goods become a sale. A confirmation that no
longer matches the order (edited, cancelled, expired, or a duplicate) is never applied to the wrong
content: it is recorded for the operator to reconcile, because V1.0 has **no refunds**. Cancelling an
unpaid order also cancels its PayOS payment request.

The integration is written with the **standard library** (`net/http`, `crypto/hmac`, `crypto/sha256`) —
**no new dependency** (ADR 015 §5 D4). This feature consumes the hooks feature `010` left for it:
`order_version` and `payment_expires_at`, and the paid transition (`PAYMENT_PENDING → PAID`) module 07
already delivers and tests.

## Technical Context

**Language/Version**: Go 1.26.0

**Primary Dependencies**: Standard library only — `net/http` for the PayOS client and the webhook,
`crypto/hmac` + `crypto/sha256` for the signatures, `encoding/json` — plus the foundation (chi router,
pgx, the shared response/error envelope, the shared `UnitOfWork`, the shared generic repository, the
audit writer). **No new dependency** (ADR 015 §5 D4).

**Storage**: PostgreSQL 16, one additive migration `migrations/00012_payment.sql` creating `payments`
and `payment_events`. Redis is not involved.

**Testing**: `testing` with `net/http/httptest`; an in-memory **fake PayOS provider** for the use cases;
`testcontainers-go` behind the `integration` tag for the storage, the apply-once behaviour, the
webhook→order→stock path, and concurrency. The signature algorithm is tested against the **known
vectors in the PayOS documentation** (create-link and webhook) so the stdlib implementation is proven,
not assumed.

**Target Platform**: Linux server (Docker) in production, local development on Windows via Docker
Desktop. The webhook endpoint must be reachable from PayOS: the VPS domain in production, a tunnel in
local development.

**Project Type**: web-service

**Performance Goals**: The operator's payment list is paginated under the project's convention. Creating
a payment request makes one outbound call to PayOS; a webhook makes one order read and one paid
transition.

**Constraints**: Money is **integer minor units plus an explicit currency** (VND); the amount equals the
order total with **no shipping fee** (ADR 015 §5). PayOS `orderCode` is a 53-bit integer, so the shop
maps a random integer code to an order id and keeps it unique. Applying a payment, changing the order's
state and turning the hold into a sale commit in **one transaction** (Constitution II). No refunds. The
paid transition is module 07's; this feature drives it once.

**Scale/Scope**: One migration (2 tables), a new module (`internal/modules/payment`), **two** new
cross-module contracts (`OrderPayment`, provided by module 07; `PaymentCancellation`, provided by module
08) with one adapter each, a PayOS client over the standard library, **four** endpoints (customer start
+ read, operator list, public webhook), and the documentation that moves with all of it.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-checked after Phase 1 design.*

- [x] **I. Modular Monolith & Clean Architecture**: A new module at `internal/modules/payment` with the
      four layers. `domain` imports only the standard library, `internal/share/access` and
      `github.com/google/uuid`. The payment reaches module 07 **only** through `internal/contracts`
      (`OrderPayment`), implemented by module 07's adapter at the composition root; module 07 reaches the
      payment **only** through `internal/contracts` (`PaymentCancellation`), implemented by module 08's
      adapter there.
- [x] **II. Transactional Integrity**: Applying a verified payment runs in one transaction with the
      order's paid transition and the stock sale; a webhook replay applies once (idempotency key
      persisted); money is integer minor units + currency; a provider error leaves no inconsistent
      intermediate state.
- [x] **III. State Machines & Invariants**: The payment status is an explicit set changed only through
      the module's transitions, tested both ways; the order's paid transition stays module 07's.
- [x] **IV. Test-First for Critical Logic**: The money amount match, the apply-once behaviour and the
      signature verification are critical and are written test-first; the guarantees a fake cannot
      prove (apply-once, the webhook→stock path, concurrency) are proven against real PostgreSQL and
      against the PayOS documentation's signature vectors.
- [x] **V. Security & Least Privilege**: The customer's order is the session's; the operator's list
      requires the administrator role; the webhook is public but **verifies the PayOS signature** before
      trusting anything; provider credentials come from the environment and are never committed.
- [x] **VI. Observability**: Applying a payment, cancelling a request and recording a reconciliation
      write an audit entry naming the payment and the order.
- [x] **VII. Simplicity (YAGNI)**: No refunds, no multi-currency, no recurring payments, no admin
      dashboard; the PayOS client is the smallest stdlib implementation and adds no dependency.
- [x] **VIII. API Documentation as a Contract**: The new endpoints and the new codes update
      `docs/api-reference.md`, this feature's OpenAPI contract and the module documents in the same
      change.
- [x] **Frontend Integration Guide**: This feature adds client-facing endpoints, so
      `specs/011-order-payment/frontend-guide.md` is a required deliverable, re-read and corrected in the
      final phase.
- [x] **Swagger annotations**: Every endpoint carries handler annotations and `docs/swagger/` is
      regenerated with `make swagger` in the same change, so `make swagger-check` passes. (The webhook
      handler is annotated too.)

**Post-design re-check**: all ten still hold. Nothing in Phase 0 or Phase 1 forced a violation.
Complexity Tracking records the deliberate choices below.

## Project Structure

### Documentation (this feature)

```text
specs/011-order-payment/
├── plan.md  ├── spec.md  ├── research.md  ├── data-model.md  ├── quickstart.md
├── contracts/{openapi.yaml, error-codes.md}
├── frontend-guide.md  ├── deferred.md
└── checklists/requirements.md

docs/decisions/019-payos-payment.md   # the ADR this feature records
```

### Source Code (repository root)

```text
internal/modules/payment/
├── domain/{constant,error,model,repository}          # payment states, errors, the Payment entity, the repo port
├── application/{dto,interface,implement,mapper}      # start/read/list use cases; PayOS provider port; apply-once
├── infrastructure/implement/{postgres,payos,auditor} # pgx adapter; the stdlib PayOS client; audit
└── presentation/{dto,http}                           # customer start/read, operator list, public webhook

internal/contracts/order.go                         # NEW: OrderPayment (provided by module 07)
internal/contracts/payment.go                       # NEW: PaymentCancellation (provided by module 08)
internal/modules/order/infrastructure/implement/payment/payment.go   # module 07's adapter (read + version-aware MarkPaid)
internal/modules/order/application/implement/lifecycle.go            # module 07 gains the nil-tolerant Cancellations port

migrations/00012_payment.sql                        # payments, payment_events
cmd/api/main.go                                     # mounts the module, wires both contracts, registers the webhook
docs/api-reference.md  docs/modules/08-payment.md  docs/modules.md
```

**On the route mount (chi)**: module 07 already mounts `/orders`. The payment's customer routes are a
sub-resource of an order, so the payment router is mounted **once** at `/orders/{orderId}/payment`
(chi supports a path param in a `Mount` pattern; the handler reads `orderId` with `chi.URLParam`), and
must not be mounted a second time at `/orders` — chi panics on a duplicate `Mount`. The operator's list
mounts at `/admin/payments` and the webhook at `/payments/payos/webhook`.

**Structure Decision**: A new module mirroring modules 03–07. Four things are specific to this feature
and recorded rather than assumed:

1. **PayOS over the standard library** (ADR 015 §5 D4): a small client in
   `infrastructure/implement/payos` signs create-link requests and verifies webhook signatures with
   `crypto/hmac`, so no dependency is added.
2. **Two new contracts across the module boundary.** `OrderPayment` (provided by module 07) lets the
   payment read an order's payment-relevant facts (owner, total, version, whether it is awaiting
   payment) and drive the version-aware paid transition. `PaymentCancellation` (provided by module 08)
   lets module 07 tell the payment module that an order was cancelled, so the provider link is cancelled
   too — the mirror direction, called after the order's transaction commits.
3. **`orderCode` is a random 53-bit integer mapped to the order id**, unique by an index, so a webhook
   can find the payment without trusting the client (ADR 015 §5 D4).
4. **Anomalies are recorded, not refunded.** A verified confirmation that cannot be applied becomes a
   reconciliation record for the operator; the order is untouched (V1.0 has no refunds, ADR 015 §5 D5).

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| A new `OrderPayment` contract against module 07, including a **version-aware** paid transition | The payment must not read the order's tables (Constitution I), and a confirmation for a superseded order version must not be applied — a re-confirmed order can be `PAYMENT_PENDING` again at a new version, so the state alone cannot detect staleness | Reading the order's tables directly — rejected by Constitution I. Detecting staleness by status alone — rejected: a re-confirmed order returns to the same status at a new version, so the old link would be wrongly accepted |
| A second contract `PaymentCancellation` against module 08, plus a nil-tolerant port on module 07's `OrderService` | FR-015 requires cancelling an unpaid order to cancel its provider link, and there is no event bus; the only constitutional way for one module to act on another's event is a contract with an adapter at the composition root | A decorator in `cmd/api` wrapping the order service — rejected: it would move ordering rules out of the use case. Emitting events to a bus — rejected by Constitution VII |
| The PayOS client is written with the **standard library** rather than the official SDK | ADR 015 §5 D4 fixes "no new dependency"; the needed surface is small (create/cancel link + webhook verify) | The official `payos-lib-golang` SDK — rejected by the recorded decision; would add a dependency for a few HTTP calls |
| `orderCode` is a random 53-bit integer with a unique index, mapped to the order id | PayOS identifies a payment by an integer `orderCode`, not by the shop's order UUID | Sending the order UUID as the code — impossible: PayOS requires an integer. Reusing a sequential id — rejected: it leaks volume and invites guessing |
| `payments` keeps its own status axis and a reconciliation state, separate from the order's single status | The order keeps one status axis (feature 009's clarification); the payment's life (pending/paid/cancelled/unmatched) is richer and module 08's own | Adding payment columns to `orders` — rejected: it would give the order two status axes and couple module 07 to the provider |
| `domain` imports `github.com/google/uuid` | `Payment` expresses its primary key as `uuid.UUID`; Constitution I names this the one allowed value-type import and requires the owning feature to record it | A payment-local identifier type — rejected: it duplicates the UUID concept for no behavioural gain |
