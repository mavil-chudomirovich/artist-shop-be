# Phase 0 Research: Order Confirmation & Editing

All technical unknowns resolved here. No decision depends on an unanswered question.

## D1: The order's new state machine, and the rename of `009`'s

**Decision**: The order has **six** states: `PENDING` (awaiting the artist's confirmation),
`PAYMENT_PENDING` (confirmed, awaiting payment), `PAID`, `SHIPPED`, `COMPLETED`, `CANCELLED`. The
allowed edges are exactly:

```
PENDING ──confirm──► PAYMENT_PENDING ──pay──► PAID ──ship──► SHIPPED ──complete──► COMPLETED
   │  ▲                     │
   │  └── edit ─────────────┘        (from PAYMENT_PENDING only; the order returns to PENDING)
   ├──reject──► CANCELLED
   ├──cancel (customer)──► CANCELLED
   PAYMENT_PENDING ──cancel (customer) / expire──► CANCELLED
```

`009`'s single `PENDING_PAYMENT` is **renamed** `PAYMENT_PENDING`, and a new `PENDING` is added.
`CONFIRMED` (named in `docs/modules/ke-hoach-don-hang.md`) is **folded into** `PAYMENT_PENDING` — the
artist's confirmation is the same act as opening the payment window — and `REJECTED`/`EXPIRED` **reuse**
`CANCELLED`, exactly as the plan document allows.

**Rationale**: The feature exists to insert a confirmation step between "the customer ordered" and
"the customer pays"; one state cannot express both. Folding `CONFIRMED` into `PAYMENT_PENDING` keeps the
state set minimal (Constitution VII): a state that is always entered and left by the same act adds
nothing. Nothing is deployed, so the breaking rename costs nothing and the enum stays truthful.

**Alternatives considered**: Keeping `CONFIRMED` separate from `PAYMENT_PENDING` — rejected: they would
always occur together, so a second state is dead weight. Adding `REJECTED`/`EXPIRED` states — rejected:
whether the artist declined or the clock expired is an event, not a state, and `CANCELLED` already means
"this order will not be fulfilled". Superseding via a new ADR (018) rather than editing ADR 017 —
required: decisions are not edited after the fact.

## D2: Checkout creates an order that holds nothing

**Decision**: Checkout still reads the cart, re-checks every line, snapshots the lines and the address,
creates the order and empties the cart — but the order is created in **`PENDING`** and **no hold is
taken**; the order carries **no deadline**.

**Rationale**: The plan's core rule is "do not hold stock while waiting for the artist". Holding at
checkout would lock stock for an unbounded time (the artist may confirm slowly) and make an edit
expensive. The re-check stays at checkout so the snapshot the customer sees is truthful.

**Alternatives considered**: Holding at checkout with a long window — rejected by the plan document
(§7) and by the design decision "chỉ giữ hàng khi artist xác nhận". Skipping the re-check at checkout —
rejected: a price the customer did not see must never be charged (feature 008's rule, carried forward).

## D3: Confirmation holds the whole order, all-or-nothing

**Decision**: Confirmation runs in one transaction that locks the order, drives the domain
`PENDING → PAYMENT_PENDING` transition, and calls `InventoryReservation.Reserve` for **every** line; if
any line cannot be held, the use case returns the shortage error and the transaction rolls back, so
**no** line stays held and the order stays `PENDING`.

**Rationale**: FR-005 requires all-or-nothing. The foundation's `WithTx` is transaction-aware since
`009` (it reuses the transaction already in the context), so module 05's `Reserve` joins the order's
transaction and a failure on line N rolls back lines 1..N-1. This is the anti-oversell guarantee
(FR-024): two confirmations competing for the last unit serialise on module 05's level row lock, and at
most one commits.

**Alternatives considered**: Reserving outside the order transaction and compensating on failure —
rejected: compensation is more code and can itself fail; one transaction is the simpler, correct answer.
Pre-checking availability and then reserving — rejected: the check and the reserve must be one atomic
step, which is exactly what running both under the order's lock and module 05's level lock gives.

## D4: The payment window is 60 minutes, from confirmation

**Decision**: On successful confirmation the order stores `payment_expires_at = now + 60 minutes` and
`confirmed_at = now`. `009`'s `expires_at` column is **renamed** `payment_expires_at` and becomes
**nullable** (it is `NULL` while the order awaits the artist). The order still derives the window from
`InventoryReservation.HoldWindow()`, whose value module 05 raises to 60 minutes (D13).

**Rationale**: FR-006/FR-008 and the plan document (§6.2). The window starts when the goods are
actually held, so it is meaningful. Renaming the column keeps it truthful: it is the *payment*
deadline, and an order awaiting the artist has none.

**Alternatives considered**: Keeping the name `expires_at` nullable — rejected: an `expires_at` that is
sometimes the payment deadline and sometimes absent is ambiguous to the next reader. Reusing `009`'s
15-minute window — rejected by the plan document, which sets 60 minutes.

## D5: Only an order awaiting payment expires; one awaiting the artist does not

**Decision**: The expiry sweeper selects only `PAYMENT_PENDING` orders whose `payment_expires_at` has
passed and cancels them (releasing the hold exactly once). An order in `PENDING` is **never** selected:
it has no deadline and cannot expire by time.

**Rationale**: FR-008/FR-009. Since `PENDING` holds nothing, an order waiting for the artist holds no
goods and needs no deadline; the artist works the queue (FIFO, D11) at their pace. The sweep is
idempotent against module 05's own release, exactly as `009`'s.

**Alternatives considered**: A separate artist deadline on `PENDING` — rejected by the user's decision
(no hold means no deadline needed). A separate `EXPIRED` state — rejected: reuse `CANCELLED` (D1).

## D6: Editing is a whole-order replacement

**Decision**: `PUT /api/v1/orders/{orderId}` replaces the order's **lines** (add/remove/change
quantities) and optionally its **delivery address**, in one transaction, under the order's row lock.
The address is named by `addressId` from the customer's saved addresses, exactly as checkout; an
omitted `addressId` **keeps** the current address. Editing to **zero lines** is refused. Every accepted
edit bumps `order_version` and writes an `order_edit_history` row (before/after content, actor, time).

**Rationale**: FR-012/FR-015/FR-017 and the user's choice of whole-order replacement. Reusing
`addressId` keeps one address model (checkout's) rather than inventing free-text editing. Refusing an
empty order keeps the existing invariant that an order has at least one line and a positive total. Each
line's snapshot (name, slug, unit price, currency) is **re-taken from the product's current values**,
because the customer is choosing the items again: a price that changed since the order was placed is
**not** refused, unlike checkout, where the cart held a price the customer had already seen.

**Alternatives considered**: Per-line endpoints — rejected by the user (more endpoints, more races).
Free-text address editing — rejected: it would fork the address model from checkout. Allowing an empty
order — rejected: it would break the `total_amount > 0` invariant and mean nothing to the artist.

## D7: Editing an awaiting-payment order returns it to awaiting confirmation

**Decision**: Editing an order in `PAYMENT_PENDING` (before a confirmed payment) **releases** its
current hold line by line (idempotent `Release`), **clears** `confirmed_at` and `payment_expires_at`,
bumps the version, and moves it back to `PENDING`. The old payment attempt is invalidated by the
**version bump**; actually cancelling a provider payment session is module 08's (deferred). Editing an
order in `PENDING` stays `PENDING` and touches no stock.

**Rationale**: FR-013/FR-014 and the plan document §5.2/§5.3: a changed order must be re-confirmed, and
its old hold must not linger. The version is the seam module 08 uses to reject a late callback for the
old content (the plan document §6.3), which this feature cannot fully implement without module 08.

**Alternatives considered**: Editing without releasing the hold — rejected: it would leave goods held
for content that no longer exists. Moving to a distinct `CONFIRMED`-less state — not applicable
(`CONFIRMED` is folded, D1).

## D8: Declining reuses cancellation

**Decision**: `POST /api/v1/admin/orders/{orderId}/reject` moves a `PENDING` order to `CANCELLED`; no
goods were held, so nothing is released. The move is audited (`ORDER_REJECTED`).

**Rationale**: FR-018. A declined order is one that will not be fulfilled — the same meaning
`CANCELLED` already carries. No new state, no stock effect.

**Alternatives considered**: A `REJECTED` state — rejected (D1). Storing a rejection reason — deferred:
the spec does not require it; a later admin/review feature can add it.

## D9: Notifications — artist on confirmation-needed, customer on status change

**Decision**: A new `Notifier` port in the order's `application/interface` is implemented by
`infrastructure/implement/notifier`, which sends through a small new `internal/share/mailer`
(`net/smtp`, `LogSender` fallback). The **artist** (the shop's configured `ADMIN_EMAIL`) is emailed when
an order **needs confirmation** (on checkout, and on any edit that leaves the order awaiting
confirmation). The **customer** is emailed on every **status change**. Sending is **best-effort**:
it runs after the transaction commits, and a failure is logged, never propagated (FR-021).

**Rationale**: FR-019/FR-020/FR-021 and the user's decision. Module 12 (Notification) is unbuilt and
module 01's sender is module-internal, so a minimal shared mailer is the smallest way to send email
without a new module or a boundary violation. Sending after commit and ignoring errors keeps email from
failing a business operation.

**Alternatives considered**: Building module 12 now — rejected (scope, Constitution VII). Reusing
module 01's `EmailSender` — rejected: it is module-internal. A generic `contracts` notification port —
rejected: there is no provider module to implement it; a module-local port plus an adapter is the
honest shape. In-app notifications — deferred to module 12.

## D10: The order version

**Decision**: `orders.order_version` starts at 1 and is **incremented** on every accepted **edit** and
on **confirmation**. It is stored on the order and used by module 08 to reject a payment callback that
belongs to an older version; it is **not** exposed in the client responses (it is an internal seam).

**Rationale**: FR-017 and the user's decision; the plan document §5.3/§6.3 requires versioning so a
late callback for edited content is not applied to the new content. Incrementing on confirmation marks
the exact content that was accepted and opened for payment.

**Alternatives considered**: Incrementing on every status change — rejected: it produces versions
unrelated to content, which is what module 08 must match. No version at all — rejected: module 08 would
have no way to tell old content from new.

## D11: The operator's confirmation queue is FIFO

**Decision**: The administrator list (`GET /api/v1/admin/orders`) gains optional `status` and `sort`
query parameters; `status=PENDING&sort=oldest` returns the confirmation queue oldest-first (FIFO by
creation time). The default ordering stays newest-first.

**Rationale**: The user asked for a confirmation screen sorted by order time (FIFO). Filtering and
ordering the existing list is the smallest way to serve it, and it reuses the list the operator already
has rather than adding a near-duplicate endpoint.

**Alternatives considered**: A dedicated `/admin/orders/pending` endpoint — rejected: it duplicates the
list projection and its pagination for one fixed filter. Ordering all admin lists oldest-first —
rejected: newest-first is the right default for a general list.

## D12: Error codes

**Decision**: Two new codes — `ORDER_NOT_EDITABLE` (409, editing an order that is not awaiting the
artist or payment) and `ORDER_EMPTY` (409, an edit that would leave the order with no lines). Reused:
`ORDER_STATE_TRANSITION_INVALID` (confirm/reject on the wrong state, naming the state),
`ORDER_QUANTITY_EXCEEDS_AVAILABLE` (a line that cannot be held at confirmation, or asks for more than is
available at edit, naming the item), `ORDER_ITEM_NOT_PURCHASABLE` and `ORDER_ITEM_PRICE_CHANGED` (an
edited line that is off sale or re-priced), `ORDER_NOT_FOUND`, `VALIDATION_ERROR` (a foreign/unknown
`addressId` or a malformed line), `MALFORMED_REQUEST`, `UNAUTHENTICATED`, `FORBIDDEN`, `RATE_LIMITED`,
`INTERNAL_ERROR`.

**Rationale**: A refusal the customer acts on differently (not editable, empty) earns its own code;
the rest are the same situations `009` already named, so reusing them avoids a second vocabulary.

**Alternatives considered**: A code per refusal — rejected: `ORDER_ITEM_NOT_PURCHASABLE` already means
"this line cannot be bought", whether at checkout or edit.

## D13: Module 05's hold window becomes 60 minutes

**Decision**: `internal/modules/inventory/domain/constant/hold.go`'s `HoldTTL` changes from 15 to 60
minutes, and module 05's tests that assert the old window are updated.

**Rationale**: FR-008's 60-minute payment window is meaningless if the hold lapses after 15 minutes.
The window has one owner — module 05's constant — and the order reads it through `HoldWindow()`, so
raising it is the single change that keeps them in step (research D4).

**Alternatives considered**: A per-order TTL passed to `Reserve` — rejected by YAGNI: one shop-wide
window suffices and the plan document fixes it at 60 minutes.

## D14: Edit history

**Decision**: A new `order_edit_history` table stores one row per accepted edit: `order_id`,
`version` (the version the edit produced), `actor_id`, `before` and `after` JSONB snapshots of the
lines and address, and `created_at`. It is written in the same transaction as the edit.

**Rationale**: FR-017 and the plan document §3 (item 12). JSONB snapshots capture "what changed" without
a normalized history schema that would only ever be read whole.

**Alternatives considered**: A normalized before/after line table — rejected: it duplicates
`order_items` for a record read whole, never queried by field.

## D15: What is tested, and how

**Decision**: Unit tests cover the state machine (one positive and one negative per edge), the checkout
(no hold), the confirmation (hold, all-or-nothing refusal), the edit (PENDING stays, PAYMENT_PENDING
releases and returns, refusals), the version bump, and the notifications (over a fake notifier). HTTP
tests cover the session and role guards, ownership, the shapes, the new codes, and the FIFO list
parameters. Integration tests prove against real PostgreSQL what a fake cannot: the state check, two
confirmations competing for the last unit (at most one wins), a hold freed exactly once on edit and on
expiry, and the edit-history row.

**Rationale**: The state transitions, the total and the hold are Constitution-IV critical logic and are
written test-first. The storage-level and concurrency rules can only be proven against the database.

**Alternatives considered**: Fakes for the storage guarantees — rejected: they prove the fake, not the
schema.

## Open questions carried into implementation

None. Every decision above is settled and each is testable.
