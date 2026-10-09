# Phase 0 Research: Order Checkout

All technical unknowns resolved here. No decision depends on an unanswered question.

## D1: How checkout reads and empties the cart

**Decision**: A new interface in `internal/contracts/cart.go`, provided by module 06 and consumed by
module 07 through the composition root:

```go
// CartCheckout lets the order module read a customer's cart to turn it into an order,
// and empty it once the order exists. A cart module adapter is supplied at the
// composition root.
type CartCheckout interface {
    // CartLines returns the caller's cart lines for checkout: one entry per product,
    // in a stable order. A customer with no cart returns an empty slice.
    CartLines(ctx context.Context, userID uuid.UUID) ([]CartLine, error)
    // ClearCart empties the caller's cart, inside whatever transaction the context
    // carries.
    ClearCart(ctx context.Context, userID uuid.UUID) error
}

// CartLine is one line of a cart at the moment of checkout, as the order module needs
// it: the product, the quantity, and the price the customer was shown.
type CartLine struct {
    ProductID        uuid.UUID
    Quantity         int64
    UnitPriceAmount  int64
    Currency         string
}
```

The cart adapter (`internal/modules/cart/infrastructure/implement/checkout/`) answers over the cart's
own repository; the cart repository gains a bulk `ClearLines(ctx, cartID)` so emptying is one
statement, run in the caller's transaction.

**Rationale**: The order owns `orders` and may not read the cart's tables; the cart owns the cart and
supplies the lines. The cart's snapshot price travels so the order can detect a price that changed
since the customer saw it (D2). Clearing runs through the contract because the cart is the cart
module's to empty, and it must join the order's transaction so an order and an emptied cart commit
together.

**Alternatives considered**: The order reading `cart_items` — rejected by Constitution I. The cart
creating the order — rejected: `orders` is the order module's. A richer contract returning the
product's facts too — rejected: it would put the order's re-check inside the cart and duplicate it in
a second place (D2).

## D2: How checkout re-checks a line

**Decision**: Within the checkout transaction, the order reads each line's product through
`ProductCatalog` (module 04) and its availability through `InventoryAvailability` (module 05), both
**bulk**, and refuses the whole checkout when any line is off sale, removed, priced differently from
the cart's snapshot, or asked for above what is available.

**Rationale**: FR-004 and FR-005 require the re-check, and the cart feature settled that a changed
price is **refused**, not silently re-priced (feature 008's Clarifications). The two contracts already
exist and are bulk, so the order does not re-implement the cart's read logic beyond the one comparison
only the order makes (the snapshot price against the product's current price).

**Alternatives considered**: Trusting the cart's snapshot — rejected: prices move between the cart and
checkout. A per-line read — rejected: N calls where one bulk call answers all lines.

## D3: A line is a snapshot, not a reference

**Decision**: `order_items` stores the product's **name, slug, unit price and currency as text/values**
and the quantity, plus an **informational `product_id` with no foreign key**. The line's displayed data
is never read from the product.

**Rationale**: This resolves the decision features 006 and 007 left to this module (`deferred.md` D3 in
both): an order must not depend on a product that module 04 may hard-delete. The informational
identifier is kept because the module document lists a product field on a line and the plan records
what was bought, but it points at nothing — no foreign key, so a removed product cannot cascade into or
block the order.

**Alternatives considered**: A foreign key with `ON DELETE CASCADE` — rejected: it would delete order
lines when a product is removed. `RESTRICT` — rejected: it would block module 04's removal. No
identifier at all — rejected: it loses which product was bought, which the module document lists.

## D4: How the goods are held, sold and released

**Decision**: A new interface in `internal/contracts/inventory.go`, provided by module 05 and consumed
by module 07, wrapping module 05's delivered reservation use cases, **per order and product**:

```go
// InventoryReservation consumes the reservation capability module 05 delivered: an
// inventory module adapter is supplied at the composition root. Each method joins the
// caller's transaction.
type InventoryReservation interface {
    Reserve(ctx context.Context, orderID, productID uuid.UUID, quantity int64) error
    Release(ctx context.Context, orderID, productID uuid.UUID) error
    ApplySale(ctx context.Context, orderID, productID uuid.UUID, sourceReference string) error
    // HoldWindow returns the fixed window module 05 sets goods aside for an order
    // (its HoldTTL). The order derives its own expires_at from it, so the window has
    // a single owner and the order never repeats the fifteen minutes (FR-016).
    HoldWindow() time.Duration
}
```

Checkout calls `Reserve` for each line; cancelling and expiry call `Release`; the paid transition
calls `ApplySale`.

**Rationale**: Feature 007's `deferred.md` **D1** named exactly this: the reservation contract module
05 withheld until the order flow exists. The capability is already delivered and tested directly
(`Service.Reserve`/`Release`/`ApplySale`); this feature **publishes the contract** and supplies the
adapter, which is the condition D1 recorded. The window and expiry stay module 05's — the order reads
the window through `HoldWindow()` rather than repeating it (FR-016) — so there is one place that decides
when a hold is over. Every call joins the order's transaction, so a hold
and the order that caused it commit together.

**Alternatives considered**: The order re-implementing holding — rejected: it would duplicate module
05's tested capability and split the answer to "when is a hold over". A contract with a richer shape
per order — rejected: module 05's use cases are per `(order, product)`, and matching them keeps one
place for the rule.

## D5: The source reference of a sale, per line

**Decision**: The paid transition takes the payment event's identity from module 08 and derives a
**per-line** source reference for `ApplySale` — the event identity combined with the product
identifier — so each line's sale movement carries a distinct reference.

**Rationale**: Module 05's `inventory_transactions.source_reference` is **globally unique** (a partial
unique index), and a paid order turns **N** lines into **N** movements. One reference for the whole
order would collide on the second line, so the order composes a distinct reference per line; replaying
the same payment event is then idempotent **per line**, which is what FR-014 requires. This obligation
on module 08 — to supply a stable event identity — is the same one feature 007 recorded.

**Alternatives considered**: Passing the raw event identity for every line — rejected: the unique
index refuses the second line. A reference derived only from the order — rejected: a retried event for
a different order could collide.

## D6: An unpaid order expires itself

**Decision**: An order stores `expires_at` (its creation plus the window `InventoryReservation.HoldWindow()`
returns — module 05's `HoldTTL` — so the value has one owner and is not repeated here). A background
sweeper — the project's second, mirroring module 05's — cancels every order that is still awaiting
payment past its `expires_at` and calls `Release` for its lines, idempotently.

**Rationale**: FR-012 requires an unpaid order to **cancel itself**, not merely for its hold to be
freed; only the order can move its own state. Module 05's hold expiry frees the goods, but an order
left "awaiting payment" would be payable with nothing to sell, so the order needs its own deadline and
sweep. Both sweeps release the same hold, and module 05's `Release` is idempotent (a second release is
a no-op success), so the two cannot double-free.

**Alternatives considered**: Relying on module 05's sweep alone — rejected: it leaves the order
payable with no goods. Releasing synchronously at the deadline — rejected: nothing can wake at a
stored moment without a scheduler, which is the sweeper.

## D7: The delivery address is a snapshot from the user module

**Decision**: The order reads the customer's addresses through the **existing**
`contracts.CustomerLookupService` (module 02), picks the address the customer named, or the default
when none is named, and writes its fields onto the order as a snapshot. A customer with no address is
refused.

**Rationale**: FR-003 and the address clarification (Session 2026-10-09) require this; the contract
already answers a customer's addresses, ordered default-first, so no new contract is needed and the
address is not read from the order's own tables later.

**Alternatives considered**: A new address contract — rejected: the existing one already returns what
is needed. Storing only an address identifier — rejected: a later edit to the address would change a
placed order, which FR-003 forbids.

## D8: Transfer resolves the recipient by email, through auth

**Decision**: A new interface in `internal/contracts/account.go`, provided by module 01:

```go
// ErrAccountNotFound is returned when no account carries the requested email; the
// provider translates its own domain error into this sentinel, as CustomerLookupService
// does with ErrCustomerNotFound.
var ErrAccountNotFound = errors.New("account not found")

// AccountLookup resolves an account by its email, so the order module can name a
// transfer recipient without reading the auth module's table. An auth module adapter
// is supplied at the composition root.
type AccountLookup interface {
    // UserIDByEmail returns the identifier of the account whose email matches, or
    // ErrAccountNotFound when none does.
    UserIDByEmail(ctx context.Context, email string) (uuid.UUID, error)
}
```

**Rationale**: The transfer clarification names the recipient by **email**, and email is the auth
module's fact (`contracts.Customer.Email` says so). The order may not read auth's table, so it asks
through a contract; auth supplies the adapter at the composition root, over its own repository's
existing email lookup.

**Alternatives considered**: Naming the recipient by identifier and dropping email — rejected by the
clarification. Reading auth's `accounts` table — rejected by Constitution I. Resolving the email in
module 02 — rejected: module 02 does not own the email.

## D9: The order state machine

**Decision**: Five states stored as a constrained text column, named in the domain:

```
PENDING_PAYMENT ──pay──► PAID ──ship──► SHIPPED ──complete──► COMPLETED
        │
        └──cancel (customer or expiry)──► CANCELLED   (terminal)
```

Exactly the edges FR-009 lists. `PAID`, `SHIPPED` and `COMPLETED` have no edge back; `CANCELLED` is
terminal. Every move is a method on the entity; the state is never assigned.

**Rationale**: Constitution III requires the transition set to be explicit and tested both ways, and
the state was settled by the V1.0 decisions (ADR 015 §5). A direct write of the column is refused by a
storage check, so the transition table cannot be bypassed.

**Alternatives considered**: A separate `EXPIRED` state distinct from `CANCELLED` — rejected: the
V1.0 decisions name one cancelled state, and whether the customer or the clock cancelled it is a
timestamp, not a state. A separate payment-status axis — rejected by the clarification.

## D10: The routes

**Decision**:

| Method | Path | Who |
|---|---|---|
| POST | `/api/v1/orders` | signed-in customer |
| GET | `/api/v1/orders` | signed-in customer |
| GET | `/api/v1/orders/{orderId}` | signed-in customer |
| POST | `/api/v1/orders/{orderId}/cancel` | signed-in customer |
| GET | `/api/v1/admin/orders` | administrator |
| GET | `/api/v1/admin/orders/{orderId}` | administrator |
| POST | `/api/v1/admin/orders/{orderId}/ship` | administrator |
| POST | `/api/v1/admin/orders/{orderId}/complete` | administrator |
| POST | `/api/v1/admin/orders/{orderId}/transfer` | administrator |

The customer surface is at the root (`/orders`), owned by the session; the operator surface is under
`/admin`, addressable by identifier. Checkout is `POST /orders` — the act of creating one from the
cart. The state moves are their own endpoints (`cancel`, `ship`, `complete`) rather than a `status`
field, because a move is a transition that can be refused, exactly as module 04 made the sell state
its own endpoint.

**There is no `pay` endpoint**: the paid transition is module 08's to drive, and a customer paying is
the payment flow, not the order flow.

**Alternatives considered**: `/users/me/orders` — rejected: "me" is the session, so the path would
repeat the token. One `PATCH /orders/{id}` with a status — rejected: a transition is not a field edit,
and folding it in would let one request change a field and a lifecycle at once. An admin `pay`
endpoint — rejected: no truthful source of a payment exists until module 08.

## D11: Money, and the order total

**Decision**: Each line stores the snapshot in integer minor units plus the currency
(`unit_price_amount bigint`, `currency char(3)`), and the order stores its committed total
(`total_amount bigint`, `currency char(3)`) and the quantity per line. The total equals the sum of
`quantity × unit_price_amount` and is never a float.

**Rationale**: Constitution II and FR-008. Storing the total commits it to the snapshot, so a later
read does not re-add lines that could (if a bug existed) differ, and the operator's list carries the
total without a join.

**Alternatives considered**: Deriving the total on every read — acceptable, but it would recompute a
committed figure; storing one figure at checkout is simpler to read and to reason about. A decimal
type — unnecessary for single-currency integer minor units.

## D12: The owner, and the transfer

**Decision**: The order's owner comes from the session for every customer route; no route carries an
owner identifier. `orders.user_id` is a **loose reference with no foreign key**, because an order must
outlive an account. A transfer changes `user_id` to the resolved recipient and nothing else —
the state, the lines and the totals are untouched — after the administrator role guard and an
existence check on the recipient.

**Rationale**: FR-020 and the transfer clarification. The loose reference mirrors the cart's product
reference: an order is a record of a sale that must survive the account it names. The recipient's
existence is checked at the source of truth (the auth lookup), which is what makes "transfer to a
non-existent account" refusable without a foreign key.

**Alternatives considered**: A foreign key with `ON DELETE CASCADE` — rejected: it would delete a
customer's orders when the account is removed, losing what was sold. `RESTRICT` — rejected: it would
block removing an account. A customer-initiated transfer — rejected by the clarification.

## D13: The paid transition, delivered but unexposed

**Decision**: The `PENDING_PAYMENT → PAID` transition is a use case (`MarkPaid(orderID, sourceRef)`)
that moves the order and calls `ApplySale` for each line, **inside one transaction**. It is delivered
and tested directly — the unit and integration suites take an order from awaiting payment to paid and
assert the hold became a sale — but it has **no HTTP endpoint**: module 08 Payment is where a real
payment confirms it.

**Rationale**: FR-014 requires the sale to be produced exactly once; the transition is the order's,
but its trigger (a confirmed payment) is module 08's, which does not exist. This is the same
arrangement feature 007 used when it delivered the sale capability with no producer, and feature 006
when it left the sell state to 007. The obligation on module 08 — to call `MarkPaid` with a stable
event identity — is recorded in `deferred.md`.

**Alternatives considered**: An admin `pay` endpoint so orders can be paid now — rejected: nothing
truthful confirms a payment, and a manual "mark paid" with no money behind it would record a sale the
shop did not make. Deferring the transition entirely — rejected: it is the order's own state, and the
hold becoming a sale is its behaviour to prove.

## D14: Pagination and ordering of the lists

**Decision**: Both the customer's and the administrator's order lists are paginated under the
project's existing convention and ordered newest first (creation time, then identifier).

**Rationale**: An order list grows without bound, so it takes the ordinary rule. Newest-first is what
both a customer checking recent purchases and an operator working a queue expect.

**Alternatives considered**: Unpaginated — rejected: the list grows forever.

## D15: What is tested, and how

**Decision**: Unit tests cover the state machine (one positive and one negative per edge), the
checkout refusals (empty cart, off sale, price changed, over available, no address), the total, and
the hold/consume/release calls, over in-memory fakes of every contract. HTTP tests cover the session
and role guards, ownership, the shapes and the refusals. Integration tests prove against real
PostgreSQL what a fake cannot: the state-check column, two concurrent checkouts of one cart producing
exactly one order, a hold freeing exactly once across the order and inventory sweeps, and an order
surviving its product's removal.

**Rationale**: The state transitions, the total and the hold are Constitution-IV critical logic and
are written test-first. The storage-level rules can only be proven against the database. A real
end-to-end checkout — cart → order → hold — runs once, against real PostgreSQL, as the feature's one
real path.

**Alternatives considered**: Fakes for the storage guarantees — rejected: they prove the fake, not the
schema.

## Open questions carried into implementation

None. Every decision above is settled and each is testable.
