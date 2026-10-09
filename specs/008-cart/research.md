# Phase 0 Research: Shopping Cart

All technical unknowns resolved here. No decision depends on an unanswered question.

## D1: How the cart reaches a product's name, on-sale state and price

**Decision**: A new interface in `internal/contracts/product.go`, provided by module 04 and consumed
by module 06 through the composition root, with a **bulk** read so a cart view is not one call per
line:

```go
// ProductCatalog answers the facts about products that the cart needs and cannot read
// itself. A product module adapter is supplied at the composition root.
type ProductCatalog interface {
    // Products returns the facts of every requested product that exists. An
    // identifier no product carries is absent from the result rather than an
    // error, so the cart can tell a removed product from an available one.
    Products(ctx context.Context, productIDs []uuid.UUID) ([]ProductSummary, error)
}

// ProductSummary is the contract DTO for one product. OnSale is a fact about
// whether the product may be sold, deliberately not module 04's four-state enum.
type ProductSummary struct {
    ID      uuid.UUID
    Name    string
    Slug    string
    OnSale  bool
    Price   ProductPrice
}

// ProductPrice is money as an integer amount in the currency's minor unit plus the
// currency, mirroring the shape the product module stores.
type ProductPrice struct {
    Amount   int64
    Currency string
}
```

**Rationale**: The cart needs a product's price and name to **add** a line (the price is captured,
the name is shown), and its on-sale state on both add and read. `ProductLookup` answers only whether
a product exists, and `ProductAvailability` is a write signal, so neither carries these facts; a
third interface is the smallest shape that does, and it is declared once in the dependency-free
`internal/contracts` package with a DTO rather than module 04's domain model
(`docs/system-design/contract-purity.md`). The read is **bulk** because the cart view needs the facts
of every line at once (research D5): one call for the whole cart, not one per line, so the cart's cost
does not grow with the number of lines.

**On the `OnSale` boolean rather than the sell-state enum**: the cart's question is "may this be
sold?", not "which of the four states is it in". Exposing the enum would put module 04's state
machine inside module 06 and make every future state a change to a consumer; a boolean answers the
cart's question and no more.

**Alternatives considered**: Adding a `Products` method to `ProductLookup` — rejected: it widens a
dependency module 05 already holds (its existence fake would need the method) for a fact module 05
does not use, and it mixes a one-bit question with a richer read. Returning module 04's `Product`
model — rejected by contract purity. A per-product call — rejected: it makes the cart read N calls.
Returning a sell-state string — rejected: it leaks the state machine.

## D2: How the cart reaches what is available

**Decision**: A new interface in a new `internal/contracts/inventory.go`, provided by module 05 and
consumed by module 06:

```go
// InventoryAvailability answers how many units of a product a customer can take,
// which is the product's physical stock minus what active holds have set aside. An
// inventory module (05) adapter is supplied at the composition root.
type InventoryAvailability interface {
    // AvailableQuantity returns one entry per requested identifier, in the same
    // order. A product with no stock row answers zero.
    AvailableQuantity(ctx context.Context, productIDs []uuid.UUID) ([]Availability, error)
}

// Availability is the contracted available quantity of one product.
type Availability struct {
    ProductID uuid.UUID
    Available int64
}
```

**Rationale**: "Available" is physical stock minus active holds, a fact only module 05 owns; the
cart may not read `stock_levels` or `stock_holds` (Constitution I), so a contract is the prescribed
arrangement. It is **bulk** for the same reason as D1: the cart view asks for every line's
availability at once, and the adapter answers the whole set in one indexed query (`= ANY(...)`) rather
than one query per product, so the database cost is fixed too. It is a **new availability read**; the
**reservation** contract module 05's `deferred.md` D1 deliberately withheld until the order flow
exists is a different contract — publishing this read now follows the same rule (a consumer exists),
but it neither uses nor closes D1, which stays open for module 07.

**On the clock staying inside module 05**: "available" depends on which holds have expired, and
module 05 judges expiry with its injected clock. The contract takes no instant — the adapter computes
availability at the moment it is asked, using the module's own clock — so the expiry rule keeps one
owner and a caller cannot pass a time that disagrees with the sweeper (module 05's research D15).

**Alternatives considered**: A per-product method — rejected: N calls for a cart read. Reading
module 05's tables from the cart — rejected by Constitution I. Passing the caller's instant — rejected:
it would make two clocks disagree about expiry.

## D3: The two tables, and a line that survives its product

**Decision**: Two tables. `carts` holds one row per account (`user_id` **unique**); `cart_items` holds
one row per product in a cart (`(cart_id, product_id)` **unique**) with the quantity and the captured
price. `cart_items.product_id` is a **loose reference — no foreign key** to `products`.

**Rationale**: FR-001 needs one cart per account, so a unique index on `user_id` is the only thing
that refuses a second cart under a race. FR-002 needs one line per product, so `(cart_id, product_id)`
is the natural key. The loose reference is what lets a removed product's line **survive** so the cart
can report it as no longer available (FR-012); a cascading key would silently delete the line and a
restricting key would block removing a product, which module 04 chose to allow.

**Alternatives considered**: A foreign key with `ON DELETE CASCADE` — rejected: it would silently
change the cart, the failure mode FR-012 exists to prevent. `ON DELETE RESTRICT` — rejected: it would
stop module 04 removing a product, a behaviour it deliberately allows. Storing a product snapshot on
the line — rejected: more than a cart needs, and mirroring an order line's snapshot belongs to module
07.

## D4: What a line captures, and what it shows

**Decision**: A line stores the **unit price** it was added at (`unit_price_amount` + `currency`) and
its quantity. The product's **name and slug are read live** on every view and are absent when the
product is gone; the **price is the stored snapshot** and is never re-read. Adding more of a product
already held raises its quantity and **keeps the price captured at the first add**, so the line's
price does not move under the customer; the checkout re-checks the price anyway, so a stale snapshot
is never charged.

**Rationale**: FR-008 makes the displayed price the one captured when the product was added, so a
price change mid-shop does not move the total under the customer; the checkout is where the price is
re-checked. The name is not a price: showing the current name keeps a rename correct, and a gone
product simply has no name, which is honest. Snapshotting the name would make it stale on rename for
no benefit the cart needs.

**Alternatives considered**: Showing the live price — rejected by FR-008. Snapshotting the name —
rejected: it goes stale on rename and duplicates a fact the read already fetches. Storing no price and
recomputing at checkout — rejected: the cart total would move as prices change.

## D5: A view re-checks each line

**Decision**: Reading the cart re-checks every line against the current product facts (D1) and
availability (D2), and reports, per line, whether it can be bought as it stands and — when it is short
— the currently available quantity. It never re-checks, and never shows, the current price.

**Rationale**: The clarification (spec, Session 2026-10-09) settles this: FR-012 requires a line that
can no longer be bought to be reported rather than silently dropped, and telling the customer the new
limit lets them fix it before checkout. The two reads are bulk (D1, D2), so the view costs a fixed two
cross-module calls regardless of how many lines it holds.

**Alternatives considered**: Not re-checking on read, enforcing only on write and at checkout
(the module document's "at operation time / checkout" phrasing) — rejected by the clarification: the
customer would meet the refusal at checkout without knowing why. Re-checking the price too — rejected:
FR-008 makes the checkout the price re-check point.

## D6: The subtotal

**Decision**: The line total is `quantity × unit_price_amount`, and the subtotal is the sum of the
line totals, computed in Go as `int64` minor units. Money is never a float and never rounded.

**Rationale**: Constitution II requires integer minor units and no floating point; FR-009 requires
the subtotal to equal the sum exactly. `int64` minor units of a sum of a small number of lines cannot
overflow in practice, and the same arithmetic is what the checkout will copy.

**Alternatives considered**: A decimal type — unnecessary: the MVP is single-currency integer minor
units with no per-unit fractions, so no intermediate rounding is possible. Computing in SQL —
rejected: the display shape and the rule belong in the domain, and the repository stays a store.

## D7: The routes

**Decision**:

| Method | Path | Who |
|---|---|---|
| GET | `/api/v1/cart` | signed-in customer |
| POST | `/api/v1/cart/items` | signed-in customer |
| PATCH | `/api/v1/cart/items/{productId}` | signed-in customer |
| DELETE | `/api/v1/cart/items/{productId}` | signed-in customer |

**Rationale**: A customer has exactly one cart, so the cart is a **singleton resource** addressed at
`/cart` with no identifier — there is no cart id to guess and none to pass, which is the shape of the
ownership rule (FR-010). A line is addressed by its **product identifier**, because a cart holds at
most one line per product (D3), so the product is the line's key; a separate line id would be a second
key for something already unique. Changing a quantity is a `PATCH` of the line and removing it is a
`DELETE` of the line, which is what each act is.

**Alternatives considered**: `/users/me/cart` — rejected: "me" is the session, so the path would
repeat what the token already says and invite the impression that a cart id could be passed. A
`PATCH /cart` with a whole cart body — rejected: the acts are per line, and replacing the whole cart
would lose the per-line refusals. A line id — rejected: a second key for a product already unique per
cart.

## D8: Creating the cart

**Decision**: A cart row is created lazily on the customer's first `POST`; a `GET` before any add
answers an empty cart (`200`, empty lines) **without** creating a row. The row lock a write needs is
taken on the cart row once it exists; the very first add creates it under an insert that a concurrent
first add is refused by the unique index, then retried as a find.

**Rationale**: FR-004 requires an empty cart to answer an empty cart, not an error, and there is no
reason to write a row for a customer who only looks. Lazy creation keeps the table to accounts that
actually added something, and the unique index makes "create once" race-safe.

**Alternatives considered**: Creating a cart at first read — rejected: it writes on a read and
materialises rows nobody uses. Creating a cart when the account is created — rejected: it would make
module 06 depend on module 01's account creation.

## D9: Add and change under concurrency

**Decision**: An add or a quantity change runs inside a `UnitOfWork` transaction that **locks the
cart's row** (`SELECT … FOR UPDATE`), then reads availability (D2), then writes the line (an upsert
that sums the quantity for an existing line, or the new quantity for a change). Two writes to the same
cart therefore serialise, and a line can never be left holding more than what was available when its
write ran.

**Rationale**: SC-002 requires a cart never to hold a quantity above availability **including when two
updates arrive at once**, and the availability check is a cross-module read the storage cannot fold
into a single conditional statement, so the lock is what makes the check-then-write safe. The lock is
per cart, not per product, so it does not contend across customers.

**Alternatives considered**: Relying on the `(cart_id, product_id)` unique index alone — rejected: it
refuses a duplicate line but says nothing about the quantity the two writers compute. A conditional
`UPDATE … WHERE quantity + n <= available` — rejected: `available` is not in `cart_items` and cannot
be a column without duplicating a fact module 05 owns. Carrying availability into the cart's table —
rejected: a copy of another module's fact that would drift.

## D10: How a line says it cannot be bought

**Decision**: Every line in the cart response carries a boolean `buyable` (the product is on sale and
at least the line's quantity is available). When a line is not buyable because the product is **on
sale but short** — availability below the line's quantity, including zero — the line also carries the
current `availableQuantity`. When a line is not buyable because the product is **off sale or removed**,
it carries no `availableQuantity`. A line is never removed and its quantity is never changed by a
read.

**Rationale**: The clarification (Session 2026-10-09) settles this shape. A short line gives the
customer the number they need to reduce to; an off-sale or gone line has no number to give, only a
truth — that it cannot be bought.

**Alternatives considered**: A single "unavailable" boolean with no quantity — rejected: the customer
would not know how far to reduce. Silently capping the quantity — rejected: it changes the customer's
cart without telling them. Reporting the current price beside the snapshot — rejected: FR-008 shows
the captured price and the checkout re-checks.

## D11: No pagination

**Decision**: The cart is answered whole, with no page window.

**Rationale**: A cart is a single bounded resource — at most one line per product the customer chose
to add — and the customer reads the whole thing to decide to check out. Pagination would add a page
selector to an object whose size is the number of distinct products in it, and the project paginates
growing lists, not a fixed-size singleton.

**Alternatives considered**: Paginating the lines — rejected: it would hide part of the basket the
customer is looking at, for no bound the object actually has.

## D12: The cart does not reserve stock

**Decision**: The cart checks availability but never holds it. Holding stock is module 05's job and
happens when a customer begins paying; the checkout (module 07) re-checks availability before an
order is created.

**Rationale**: FR-011 and the inventory feature's Clarifications settle this. A cart is not a
promise, so adding to a cart must not reduce availability for anyone else; the short window between
adding and paying is covered by the availability re-check at checkout, not by a hold.

**Alternatives considered**: Holding stock on add — rejected: it would let a customer deny stock to
others indefinitely with no order, and it is module 05's capability to hold, invoked at payment.

## D13: Composition and the owner

**Decision**: `cmd/api/main.go` builds module 04's `ProductCatalog` adapter and module 05's
`InventoryAvailability` adapter and hands them to the cart's service. The acting customer is taken
from the context the session filled, exactly as the other modules do, and no route carries a cart,
account or owner identifier.

**Rationale**: Constitution I requires the providing module to supply the adapter at the composition
root; the session is the only source of the acting account (FR-010), which is what makes cross-account
access impossible by construction.

**Alternatives considered**: Passing the owner in the body or query — rejected: the one way to
introduce cross-account access. Letting the cart import either module's infrastructure — rejected by
Constitution I.

## D14: The codes and the refusals

**Decision**: The cart reuses `PRODUCT_NOT_FOUND` (404) for adding a product that is unknown or removed,
and adds two codes: `CART_PRODUCT_NOT_PURCHASABLE` (409) for a product that exists but is not on sale,
and `CART_QUANTITY_EXCEEDS_AVAILABLE` (409) for a quantity above what is available. A quantity that is
not a positive whole number is the shared `VALIDATION_ERROR` (400) with the field named.

**Rationale**: An unknown product is the same situation module 04 already answers with
`PRODUCT_NOT_FOUND`, so a second, near-identical code would make a client branch on two codes for one
condition. "Not on sale" and "more than available" are states of the product and the shelf, not
malformed values, so they are conflicts the operator can act on — the same reasoning module 03 and
module 04 used. A malformed quantity is a value rule, which is what `VALIDATION_ERROR` means.

**Alternatives considered**: One code for both conflicts — rejected: the two lead to different
actions (wait for the product to go on sale vs reduce the quantity), so the client would have to read
the message. A cart-specific not-found code — rejected: it would duplicate module 04's.

## D15: What is tested, and how

**Decision**: Unit tests cover the subtotal, the quantity rules, the "can it be bought" decision and
the empty cart, over an in-memory repository and fakes of the two contracts. HTTP tests cover the
session requirement, cross-account refusal, the refusals and the response shape. Integration tests,
behind the `integration` tag, prove against real PostgreSQL what a fake cannot: the one-cart-per-
account and one-line-per-product unique indexes, a cart line surviving its product's removal, and two
concurrent adds to one cart never leaving a line above availability.

**Rationale**: The money rule and the invariants are Constitution-IV critical logic and are written
test-first. The storage-level rules can only be proven against the database — a fake that enforces a
unique index proves only that the fake was written twice. The concurrency case is the one the row
lock (D9) exists for.

**Alternatives considered**: Proving the unique indexes with a fake — rejected: it proves the fake,
not the schema. Skipping the concurrency test — rejected: it is one of the cart's stated success
criteria.

## Open questions carried into implementation

None. Every decision above is settled and each is testable.
