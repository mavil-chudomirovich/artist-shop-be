# Phase 1 Data Model: Online Order Payment

Two new tables. The decisions behind the shape are in `research.md`; this file records what is stored,
what is constrained, and what the read paths project. Nothing in `orders` or `order_items` changes: the
payment reads the order through a contract and writes its own tables.

## Entity: Payment

One payment for one order. It is module 08's own record of what the customer agreed to pay, what
the provider said, and where that leaves both.

### Stored columns

| Column | Type | Null | Meaning |
|---|---|---|---|
| `id` | uuid | no | Primary key |
| `order_id` | uuid | no | The order this payment belongs to. A **loose reference, no foreign key** (as `orders.user_id`, `009` research D12), exactly as `orders.user_id`: the payment and the order live in different modules, so no cross-module key is possible |
| `user_id` | uuid | no | The account that started the payment, copied at creation so the operator can see who paid without a join. A loose reference |
| `order_code` | bigint | no | PayOS's `orderCode`: a **random 53-bit positive integer**, unique (research D4) |
| `amount` | bigint | no | The amount requested, in the currency's minor unit |
| `currency` | char(3) | no | Three uppercase letters; `VND` in MVP |
| `description` | text | no | The short description sent to PayOS (the payer sees it) |
| `order_version` | bigint | no | The order's content version **at the time the payment was created** (research D5, D7). A confirmation is applied only when this still matches |
| `status` | text | no | One of the four payment states (research D6). Changed only through the module's transitions |
| `provider` | text | no | The provider name; `payos` |
| `provider_reference` | text | yes | PayOS's own transaction reference once it confirms. **NULL until confirmed** |
| `payment_link_id` | text | yes | PayOS's `paymentLinkId`, the handle used to read and cancel the link |
| `checkout_url` | text | yes | The PayOS checkout URL the customer opens |
| `qr_code` | text | yes | The VietQR payload PayOS returns, for QR rendering |
| `unmatched_reason` | text | yes | Why a verified confirmation could not be applied (research D8). NULL unless `UNMATCHED` |
| `created_at` | timestamptz | no | Set once |
| `updated_at` | timestamptz | no | Touched on every write |

### Constraints, and why each is at the storage layer

| Constraint | Kind | Requirement | Why not only in the application |
|---|---|---|---|
| `payments_pkey` | primary key on `id` | — | A payment is addressable |
| `payments_order_code_key` | unique on `order_code` | FR-022, research D4 | A collision must be retried by the module, not silently stored twice; PayOS identifies by this code |
| `payments_live_order_key` | partial unique index on `order_id` WHERE `status = 'PENDING'` | FR-005 | At most one **live** payment per order: two concurrent starts converge on one, so a customer cannot end up with two live links (and pay twice). Terminal rows are not constrained |
| `payments_currency_ck` | check: `currency ~ '^[A-Z]{3}$'` | FR-004 | The currency is unambiguous |
| `payments_amount_ck` | check: `amount > 0` | FR-004, research D13 | A payment is for a positive amount |
| `payments_order_version_ck` | check: `order_version >= 1` | FR-012 | The version is the order's positive counter |
| `payments_status_ck` | check: `status IN ('PENDING','PAID','CANCELLED','UNMATCHED')` | FR-011 | An unlisted state cannot be stored, so the transition table cannot be bypassed |
| `payments_unmatched_ck` | check: `(status = 'UNMATCHED') = (unmatched_reason IS NOT NULL)` | FR-012, FR-013 | A payment is `UNMATCHED` **iff** it carries a reason: the operator's queue needs the reason, and no other state may carry one |
| `payments_provider_reference_key` | unique partial index on `provider_reference` WHERE it is NOT NULL | FR-009, research D7 | The provider reference is the idempotency key: two rows must never claim the same transaction |
| `payments_order_idx` | btree on `(order_id, created_at, id)` | FR-017 | The customer reads their order's payments |
| `payments_status_idx` | btree on `(status, created_at, id)` | FR-018 | The operator's reconciliation queue filters by status |

**On the missing foreign keys**: `order_id` and `user_id` are loose references for the same reason
`orders.user_id` is (`009` research D12). There is no key from `orders` to `payments` either: the modules are
separate, and the order's cancellation is asked of module 08 through the contract, not persisted here.

**On `provider_reference` being nullable**: it is set only when the provider confirms. Until then the
payment holds no transaction identity, and the unique index is partial so it does not reject several
rows with no reference.

## Entity: Payment event (new)

One row per decision the module made about a payment: created, applied, cancelled, or recorded as
unmatched. It is the module's audit trail (FR-022, FR-010) and makes "apply once" provable after the
fact.

### Stored columns

| Column | Type | Null | Meaning |
|---|---|---|---|
| `id` | uuid | no | Primary key |
| `payment_id` | uuid | no | The payment this event belongs to. FK → `payments(id)` **ON DELETE CASCADE** |
| `action` | text | no | The module's own audit action: `PAYMENT_CREATED`, `PAYMENT_APPLIED`, `PAYMENT_CANCELLED`, `PAYMENT_UNMATCHED` |
| `actor_id` | uuid | yes | The acting account, when one acted (the customer creating it, the operator reconciling). NULL for the provider's webhook |
| `detail` | text | yes | A short reason or the provider reference, for the audit trail |
| `created_at` | timestamptz | no | Set once |

### Constraints

| Constraint | Kind | Requirement | Why |
|---|---|---|---|
| `payment_events_pkey` | primary key on `id` | FR-022 | An event is addressable |
| `payment_events_payment_fk` | FK → `payments(id)` ON DELETE CASCADE | FR-022 | A removed payment takes its history with it |
| `payment_events_action_ck` | check: `action IN ('PAYMENT_CREATED','PAYMENT_APPLIED','PAYMENT_CANCELLED','PAYMENT_UNMATCHED')` | FR-022 | A written event always carries a canonical action |
| `payment_events_payment_idx` | btree on `(payment_id, created_at)` | FR-022 | The history is read in order |

**On the actor being nullable**: the webhook is a provider act with no acting account, so the actor is
NULL there, which means "PayOS said so": the acting account is absent only for the provider webhook (FR-019).

## Transitions

```
PENDING ──apply──> PAID            (terminal)
   │
   ├──> CANCELLED                 (the order was cancelled / the link was cancelled)
   └──> UNMATCHED                 (a verified confirmation could not be applied)
```

| Transition | From | To | Refuses when | Trigger | Stock effect |
|---|---|---|---|---|---|
| create | (none) | `PENDING` | the order is not awaiting payment, or the provider refuses — in which case **nothing is written** | the customer, through the provider | none |
| apply | `PENDING` | `PAID` | the amount differs, the order's version differs, or the order is no longer awaiting payment | the provider's webhook | the order's paid transition → module 05's sale |
| cancel | `PENDING` | `CANCELLED` | the payment is not live | the order's cancellation (through the new contract, research D9) | none here (module 07 releases the hold) |
| record | any | `UNMATCHED` | (it is a record, not a move) | a verified confirmation that cannot be applied | **none** (the order is untouched, research D8) |
**An `UNMATCHED` payment is not undoable** and the order is never moved by it (FR-012, FR-013).

**Applying, recording and the order's paid transition run inside one `UnitOfWork`** that also calls
module 05, so the payment row, the order's state and the stock effect commit together or not at all
(Constitution II, research D7). Cancelling a PayOS link deliberately runs **outside** the order's
transaction (research D9).

## What each read path projects

| Path | Reader | Columns | Requirement |
|---|---|---|---|
| customer read | signed-in customer | the payment's status, amount, and the link (`checkoutUrl`, `qrCode`) | FR-017 |
| operator list | administrator | every payment with its order and owner, newest first, filterable by status | FR-018 |

Both are paginated under the project's convention; the operator list's `status=UNMATCHED` is the
reconciliation queue (research D15).

## What this feature changes outside its own tables

| Change | Where | Why |
|---|---|---|
| A new cross-module contract `OrderPayment` | `internal/contracts/order.go` | The payment may not read the order's tables (research D5) |
| A second cross-module contract `PaymentCancellation` | `internal/contracts/payment.go` | The order may not call the provider; module 08 owns it (research D9) |
| Module 07 adapters for both contracts | `internal/modules/order/infrastructure/implement/payment/*.go` | Module 07 owns the order: it answers the order's payment facts and drives the version-aware paid transition. Its `OrderService` also gains the nil-tolerant `Cancellations` port and calls it after a successful cancel (research D9) |
| A version-aware paid transition in module 07 | `internal/modules/order/application/implement/lifecycle.go` | A re-confirmed order is `PAYMENT_PENDING` again at a new version, so only a version match detects a superseded link (research D5) |
| A command-line wiring point and the webhook route | `cmd/api/main.go` | Mounts the module, wires the contract and the PayOS client |

No column, index or constraint of `orders` or `order_items` is touched, and no data is backfilled.
