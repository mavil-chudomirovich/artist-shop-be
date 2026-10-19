# Phase 0 Research: Online Order Payment

All technical unknowns resolved here. The PayOS facts are taken from the official API reference
(`payos.vn/docs/api`) and the signature guide (`payos.vn/docs/tich-hop-webhook/kiem-tra-du-lieu-voi-signature`).

## D1: The provider, its base URL and its endpoints

**Decision**: The provider is **PayOS** (ADR 015 §5). The shop calls the PayOS Merchant API at
`https://api-merchant.payos.vn` with the headers `x-client-id` and `x-api-key` (from the environment),
using three operations:

| Operation | Method + path | Use |
|---|---|---|
| Create a payment link | `POST /v2/payment-requests` | start a payment for an order |
| Get a payment link | `GET /v2/payment-requests/{id}` | **reserved** for the operator's manual reconciliation; not called by the module in this feature |
| Cancel a payment link | `POST /v2/payment-requests/{id}/cancel` | cancel when the order is cancelled |

The webhook the shop receives is a `POST` to its own public URL, and PayOS registers it with
`POST /confirm-webhook` (a one-time setup step).

**Rationale**: These are the operations the spec needs (start, cancel, and a reserved read for the
operator's manual reconciliation). The base URL and the `x-client-id`/`x-api-key` auth are what the
reference documents.

**Alternatives considered**: The official Go SDK — rejected by ADR 015 §5 D4 (no new dependency). A
separate sandbox base URL — the reference documents **only** production; sandbox testing is done with a
test payment channel against the same API, so the base URL is one value.

## D2: The create-link request and its signature

**Decision**: The create-link body carries `orderCode`, `amount`, `description`, `cancelUrl`,
`returnUrl` (and optionally buyer/item fields, unused here), plus a `signature`. The signature is
`HMAC_SHA256` with the channel's **checksum key** over the fields **sorted alphabetically**, formatted
`key1=value1&key2=value2...`:

```
amount=$amount&cancelUrl=$cancelUrl&description=$description&orderCode=$orderCode&returnUrl=$returnUrl
```

`amount` and `orderCode` are integers; `description` is short (the reference notes a 9-character limit
for non-PayOS-linked bank accounts), so it carries a stable, short reference.

**Supplement (2026-10-19 — provider-side expiry)**: the create-link body also carries an optional
`expiredAt` (Unix seconds, Int32). It is **not** part of the signature above: the documented signature
covers only the five fields. The shop sends `expiredAt = payment_expires_at` (the order's own deadline
from `OrderPaymentTarget`), so the PayOS link expires with the shop-side 60-minute window instead of
living on after the sweeper cancels the order. A zero deadline omits the field, keeping the previous
behaviour.

**Rationale**: This is the algorithm the reference states for `create-payment-link`.

**Alternatives considered**: Signing the whole body — rejected: the reference fixes the exact field set
and ordering. Omitting `description` — rejected: it is required and is what the payer sees.

## D3: The webhook and how it is verified

**Decision**: PayOS posts a JSON body `{code, desc, success, data:{...}, signature}`. The shop verifies
`signature` = `HMAC_SHA256(checksumKey, data_string)` where `data_string` is the `data` object's fields
**sorted alphabetically**, `key1=value1&key2=value2...`, with null/undefined rendered as an empty string
and nested arrays' objects sorted. The verification is done with the **standard library** and is tested
against the **documented sample vector**: with the documentation's sample checksum key
`1a54716c8f0efb2744fb28b6e38b25da7f67a925d98bc1c18bd8faaecadd7675` and the sample `data`
(`orderCode=123, amount=3000, description=VQRIO123, accountNumber=12345678, reference=TF230204212323,
transactionDateTime=2023-02-04 18:25:00, currency=VND, paymentLinkId=124c33293c43417ab7879e14c8d9eb18,
code=00, desc=Thành công, and the empty counter/virtual account fields`), the signature is
`412e915d2871504ed31be63c8f62a149a4410d34c4c42affc9006ef9917eaa03` — the value the documentation's own
sample code computes. A body whose signature does not match is **refused** and nothing is applied.

**Rationale**: This is the algorithm the reference states for the payment webhook. Verifying against the
documented vector makes the stdlib implementation provable rather than assumed.

**Alternatives considered**: Trusting the body because it comes from a "known" IP — rejected: the
signature is the authentication. Reading the fields before verifying — rejected: verification must
happen on the exact received values.

## D4: `orderCode` — a random 53-bit integer mapped to the order

**Decision**: Each payment request gets a random **53-bit positive integer** `order_code`, unique by an
index, stored on the payment row beside the `order_id`. The webhook carries `orderCode`, so the shop
finds the payment by `order_code` without trusting anything from the client.

**Rationale**: PayOS identifies a payment by an integer `orderCode` and its amount; the shop's order id
is a UUID. A random code avoids leaking volume and, with a unique index, makes a collision retryable.
This mirrors ADR 015 §5 D4 ("số ngẫu nhiên int53 + thử lại khi trùng, chốt bằng unique index").

**Alternatives considered**: A sequential code — rejected: it leaks order volume and invites guessing.
Sending the order UUID — impossible: PayOS requires an integer.

**On one live payment per order**: a **partial unique index** on `payments (order_id) WHERE status =
'PENDING'` enforces FR-005 at the storage layer, so two concurrent starts converge on one live payment
rather than each creating a link (which would let the customer pay twice). The losing start re-reads and
reuses the winner (research D7's orphan-link window still applies to the link it created).

## D5: The `OrderPayment` contract against module 07

**Decision**: A new interface in `internal/contracts/order.go`, provided by module 07:

```go
type OrderPayment interface {
    // PaymentTarget returns the facts a payment needs about an order: its owner,
    // its total, its content version, and whether it is awaiting payment. A
    // missing order answers a sentinel not-found.
    PaymentTarget(ctx context.Context, orderID uuid.UUID) (OrderPaymentTarget, error)
    // MarkPaid drives the order's paid transition. An order already paid (or
    // beyond) is a no-op success, exactly as module 07's delivered transition
    // already behaves, so a retried provider callback cannot double-apply.
    // An order awaiting the artist, cancelled or expired — or one whose version
    // is not expectedVersion — is refused with a distinct error, so a
    // confirmation for superseded content is never applied (FR-009, FR-012).
    MarkPaid(ctx context.Context, orderID uuid.UUID, sourceReference string, expectedVersion int64) error
}

// OrderPaymentTarget carries only what a payment needs, in contract-local types:
// the package imports no module, exactly as contracts.ProductSummary carries
// OnSale rather than module 04's state type (docs/system-design/contract-purity.md).
type OrderPaymentTarget struct {
    UserID           uuid.UUID
    Amount           int64
    Currency         string
    Version          int64
    PaymentExpiresAt time.Time
    // AwaitingPayment reports whether the order is awaiting payment. It is a
    // flag, not module 07's status value, so no enum type crosses the boundary.
    AwaitingPayment bool
}
```

**Rationale**: Constitution I forbids the payment reading the order's tables. The version-aware
`MarkPaid` closes the gap the state alone cannot: a customer may edit a `PAYMENT_PENDING` order (which
returns it to `PENDING`) and the artist may re-confirm it, so the order is `PAYMENT_PENDING` again at a
**new version** — only a version match tells the shop the link still belongs to the current content.
"Awaiting payment" is carried as a **flag**, not as module 07's status type, so the contract package
imports no module (`internal/contracts/doc.go`, `docs/system-design/contract-purity.md`), exactly as
`contracts.ProductSummary` carries `OnSale` instead of module 04's state.

**On an already-paid order**: module 07's delivered transition is a **no-op success** for an order
already `PAID` (or beyond), which is what makes a replayed provider callback harmless at the order
layer. That behaviour is **kept**: the version check only adds a refusal for an order whose content
changed. Removing the no-op would break feature 010's settled, tested idempotency for no gain, because
the webhook already short-circuits on a `PAID` payment (D7).

**Alternatives considered**: Reading the order's tables — rejected by Constitution I. A status-only
check — rejected: it accepts a superseded link after a re-confirmation. A payment event bus — rejected:
over-engineered for one synchronous call.

## D6: The payment status machine

**Decision**: A payment has its own states, changed only through the module's transitions. The set is
deliberately small, because every state in it is reachable:

```
PENDING ──apply──> PAID            (terminal)
   │
   ├──> CANCELLED                 (the order was cancelled / the link was cancelled)
   └──> UNMATCHED                 (a verified confirmation could not be applied)
```

A duplicate confirmation on a `PAID` payment is a no-op. `UNMATCHED` is the reconciliation state the
spec's anomalies need (D8).

**Why there are no `CREATED`, `FAILED` or `EXPIRED` states**: creating a link and going live is **one**
atomic step inside the payment's own transaction, so there is no observable "created but not yet live"
state, and when the provider refuses, **nothing is written** — the customer simply gets
`PAYMENT_PROVIDER_ERROR`, exactly as `error-codes.md` already states. Expiry is the order's own path
(module 07's sweep cancelling the order) and that cancellation cancels the payment, so `EXPIRED` would
be a second name for `CANCELLED`; `FAILED` would be a state the module never persists. Dropping
unreachable states keeps the set honest (Constitution III, VII).

**Rationale**: The payment's life is richer than the order's single axis and is module 08's own
(feature 009's clarification).

**Alternatives considered**: Reusing the order's states — rejected: it would couple the two modules'
lifecycles. A boolean "paid" — rejected: it cannot express cancelled or unmatched. Keeping
`CREATED`/`FAILED`/`EXPIRED` — rejected above: none of them is reachable.

## D7: Applying a payment exactly once, and the orphan-link window

**Decision**: The webhook handler, after verifying the signature and finding the payment by `order_code`:
1. If the payment is already `PAID`, it is a **no-op success** (a replay).
2. It checks the confirmation's **amount** equals the payment's stored amount; a mismatch is recorded as
   `UNMATCHED` and not applied.
3. It calls `OrderPayment.MarkPaid(orderID, sourceReference, payment.version)`. `sourceReference` is the
   payment's own stable reference (e.g. `payos:<order_code>`), so module 05's per-line sale is
   idempotent. A version/state refusal is recorded as `UNMATCHED`.
4. On success it marks the payment `PAID` and stores the provider `reference`, all in **one
   transaction** with the order's paid transition.

**The provider is called before the row is written, so the payment's own transaction stays short.** The
window between a successful create-link response and the payment row committing is therefore real: if
that write fails, a live link exists at PayOS with no payment row, and a later webhook for it is
**acknowledged with `2xx` and records nothing** (there is no row to attach a record to), with a warning
logged. The operator reconciles it out of band — query the link with the reserved
`GET /v2/payment-requests/{id}` (D1), then write the row and apply by hand. Full automatic recovery is
deferred (`specs/011-order-payment/deferred.md`).

**Rationale**: FR-009/FR-010 and Constitution II. The idempotency has two layers: the payment's own
`PAID` short-circuit, and module 05's per-line `source_reference`. The transaction makes the payment
record and the order's state agree or not at all. Keeping the provider call outside the order's
transaction is what makes a slow or failing provider harmless to the order.

**Alternatives considered**: Applying in the request that created the link — impossible: only the
webhook knows the payment happened. Writing the row first and calling the provider inside the
transaction — rejected: it would hold a database transaction open across an HTTP call, and a rollback
would leave a live link with no row in exactly the same way. Relying only on module 05's idempotency —
rejected: the payment record must also not double-apply. Auto-recovering the orphan link from the
webhook — deferred: without a payment row there is no stored version or amount to check safely.


## D8: Anomalies are recorded, never refunded

**Decision**: A verified confirmation that cannot be applied — a wrong amount, an order whose version
changed, or an order no longer awaiting payment — becomes an `UNMATCHED` payment record (with the
reason), and the order is left untouched. No refund is attempted (ADR 015 §5 D5). The operator reviews
`UNMATCHED` payments and resolves them outside the system.

**Rationale**: FR-012/FR-013 and the clarification. The shop keeps a record for reconciliation without
silently corrupting the order.

**Alternatives considered**: Auto-refunding — rejected by D5 (no refunds at V1.0). Silently dropping the
confirmation — rejected: the operator would never know money arrived.

## D9: Cancelling an unpaid order cancels its PayOS link — through a contract

**Decision**: A new interface in `internal/contracts/payment.go`, provided by **module 08** and consumed
by **module 07** — the mirror of `OrderPayment`:

```go
// PaymentCancellation lets the order module tell the payment module that an order
// was cancelled, so the provider payment request it created is cancelled too. A
// payment module adapter is supplied at the composition root.
type PaymentCancellation interface {
    // CancelForOrder cancels the live payment of a cancelled order. It is
    // best-effort: a nil adapter is a no-op, and a provider failure is logged by
    // the payment module and never fails the order's cancellation.
    CancelForOrder(ctx context.Context, orderID uuid.UUID)
}
```

Module 07's `OrderService` gains an optional, nil-tolerant `Cancellations appinterface.PaymentCancellation`
port. `Cancel` — the one use case both the customer's `CancelMine` and the expiry sweep drive — calls
`CancelForOrder` **after the transaction commits**, exactly as module 07 already calls its own `Notifier`
after a commit (feature 010). Module 08 supplies the adapter at the composition root.

**Rationale**: FR-015/FR-016. There is no event bus in this project, so the only constitutional way for
one module to act on another's event is a contract with an adapter at the composition root. Putting the
call on `Cancel` (rather than on `CancelMine` and on the sweeper separately) keeps **one** call site, so
the expiry path is covered by the same port. After-commit keeps a slow provider out of the order's
transaction, and best-effort keeps a provider failure from blocking a cancellation.

**Alternatives considered**: Calling the provider inside the order's transaction — rejected: a network
call must not sit in a database transaction, and a failure would roll back the order's cancellation.
A decorator wrapping the order service at the composition root — rejected: it would put ordering logic
(which orders, which transitions) in `cmd/api`; the port keeps the rule in the use case. Emitting events
to a bus — rejected by Constitution VII (no machinery without measured need).

## D10: The webhook endpoint

**Decision**: `POST /api/v1/payments/payos/webhook` — **public** (no session), verifies the signature,
applies or records, and answers `2xx` quickly so PayOS does not retry a handled event. A body that does
not verify answers an error and applies nothing.

**Rationale**: FR-019. PayOS retries until it gets a `2xx`, so a handled event must answer success.

**Alternatives considered**: Requiring a session — impossible: PayOS is not a user. Doing slow work
before answering — rejected: it invites retries and timeouts.

## D11: Configuration

**Decision**: New environment settings, read by `internal/share/config`:
`PAYOS_CLIENT_ID`, `PAYOS_API_KEY`, `PAYOS_CHECKSUM_KEY`, `PAYOS_BASE_URL` (default
`https://api-merchant.payos.vn`), `PAYMENT_RETURN_URL`, `PAYMENT_CANCEL_URL` (the frontend URLs PayOS
redirects to). `.env.example` gains empty placeholders; the real values live in `.env` (git-ignored).

**Rationale**: FR-020 and Constitution V — no secret is committed. The return/cancel URLs are the
frontend's and vary by environment.

**Alternatives considered**: Hardcoding the base URL — rejected: the environment setting keeps it
overridable. Committing the keys — forbidden.

## D12: The endpoints and their shapes

**Decision**:

| Method | Path | Who |
|---|---|---|
| POST | `/api/v1/orders/{orderId}/payment` | signed-in customer (start/reuse) |
| GET | `/api/v1/orders/{orderId}/payment` | signed-in customer (read) |
| GET | `/api/v1/admin/payments` | administrator (list + reconciliation) |
| POST | `/api/v1/payments/payos/webhook` | public (PayOS) |

Starting a payment returns the payment's status and, when live, the `checkoutUrl` and `qrCode`. A repeat
start **reuses** the live payment (the clarification) and returns the same link.

**Rationale**: The clarification chose dedicated payment endpoints (not folded into the order). The
paths sit under the order the payment belongs to for the customer, and under `/admin` for the operator.

**Alternatives considered**: A top-level `POST /payments` — rejected: the payment always belongs to an
order, so the order's path is clearer. Folding a `payment` field into the order response — rejected by
the clarification.

## D13: Money

**Decision**: Amounts are integer minor units (VND) with the currency `VND`; the payment amount equals
the order total, with no shipping fee. The amount is compared exactly between the payment and the
confirmation.

**Rationale**: FR-004 and ADR 015 §5. An exact integer comparison is the safe match.

**Alternatives considered**: A tolerance — rejected: there is no rounding, so an exact match is correct.

## D14: What is tested, and how

**Decision**: Unit tests cover the payment state machine (both ways per edge), the create-link signature
and the webhook signature (against the PayOS documentation vectors), the start/reuse behaviour, the
amount match, the apply-once short-circuit, and the anomaly→`UNMATCHED` path, over a fake provider and
module 07. HTTP tests cover the session/role guards, the shapes and the refusals. Integration tests
prove against real PostgreSQL what a fake cannot: the webhook→order→stock path applies once, a replay
does nothing, a stale version is not applied, and two concurrent confirmations apply once.

**Rationale**: The amount, the apply-once behaviour and the signature are critical logic (Constitution
IV) and are written test-first; the storage and concurrency rules need the database.

**Alternatives considered**: Fakes for the signature — rejected: the documented vectors prove the real
algorithm. Calling the real PayOS sandbox in tests — rejected: tests must not depend on a live provider;
a fake stands in and the signature vectors prove the crypto.

## D15: Operator reconciliation

**Decision**: The operator list `GET /admin/payments` returns every payment, filterable by status, so
`status=UNMATCHED` is the reconciliation queue. It is review-only: there is no "apply manually" action
in V1.0 (the clarification).

**Rationale**: FR-018 and the clarification. A filter on the existing list is the smallest way to serve
the queue.

**Alternatives considered**: A dedicated `/admin/payments/unmatched` endpoint — rejected: it duplicates
the list for one filter. A manual-apply action — rejected by the clarification.

## Open questions carried into implementation

None. Every decision above is settled and each is testable.
