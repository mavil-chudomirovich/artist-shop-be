# Phase 0 Research: Inventory Tracking

All technical unknowns resolved here. No decision depends on an unanswered question.

## D1: What a stock level is, and where availability comes from

**Decision**: Each product has **one physical quantity**, held in its own row, and `available` is
**derived**: `physical − the quantities of its holds that are currently active`. A hold is a
first-class record with its own status and expiry, not a movement and not a flag on the level.

**Rationale**: FR-001 to FR-003 change the physical quantity; the clarification requires a hold;
FR-018 defines availability as physical minus active holds. The three are different facts about the
same product — what is on the shelf, what is set aside, and what is therefore sellable — and
collapsing any two of them loses a distinction the spec relies on. In particular, a hold leaving
availability unchanged when it is paid (FR-016) is only expressible if "physical" and "held" are
separate: paying moves one unit from "physical" to "gone" and the same unit out of "held", so
`physical − held` does not move at all, which is exactly why paying does not change what is
available.

**Alternatives considered**: A single number representing availability with holds folded in —
rejected: it cannot distinguish "sold" from "held", so a hold that is released could not be told
from a sale, and a physical damage could not be distinguished from a hold. Folding holds into
`physical` as negative movements — rejected for the same reason and because FR-004 states a hold is
not a physical change.

## D2: How "never negative" is enforced at the storage layer

**Decision**: The physical quantity lives in a `stock_levels` row per product, and every decrease is
a **conditional update**: `UPDATE stock_levels SET quantity = quantity - $1 WHERE product_id = $2
AND quantity >= $1 RETURNING quantity`. A decrease whose row is not returned is an oversell and is
refused; an increase and an absolute adjustment write the new value the same way. Hold creation locks
that same row (`SELECT ... FOR UPDATE`) before it computes availability, so two concurrent holds
cannot both set aside the same unit.

**Rationale**: FR-009 and FR-010 require the no-negative rule to hold at the storage layer and to
stand under concurrency. A conditional `UPDATE` is the storage's own atomic check: Postgres re-reads
the row under its write lock, so the `quantity >= $1` predicate is evaluated against the current
value and no application-level read-then-write race exists. It also means a refused oversell changes
nothing and writes no ledger row (US2 scenario 1), because the update simply matches no row.

**Alternatives considered**: Deriving the quantity by summing the ledger — rejected: with no row to
lock, the predicate would be a pre-check outside the write and two writers could both pass it. A
`CHECK (quantity >= 0)` alone — rejected: it refuses the write but does not let the use case tell an
oversell from a missing product, and it fires on a negative value that a conditional update never
produces in the first place. Application-level counting with no lock — rejected: it is the race
feature 003 was already bitten by, and Constitution II forbids it for stock.

**On the two non-negative rules, and where each is enforced**: the `quantity >= 0` and
`resulting_quantity >= 0` rules are properties of a single column, so they are storage `CHECK`s.
FR-009's second half — a physical decrease must not fall below the quantity currently held — is a
**relationship between two tables**, which no single-table constraint can express, so it is enforced
**atomically** by the same `SELECT ... FOR UPDATE` row lock the reserve path takes, inside the
transaction, rather than by a `CHECK`. Constitution II's "at the storage layer" is met fully under
this reading: the column rules are constraints, and the cross-table rule is made race-free by the row
lock, which is the strongest form a cross-table rule can take in this schema.

## D3: The order of one stock operation

**Decision**: One operation runs inside a single transaction (the `UnitOfWork` port) and, in order,
(1) locks or updates the `stock_levels` row and reads its previous availability, (2) inserts exactly
one `inventory_transactions` row carrying the signed delta and the resulting quantity, (3) computes
the new availability and, **only if it crossed zero**, asks product to change its sell state
(D10). A failure at any step rolls the whole thing back, so the level and the ledger cannot disagree
(FR-011).

**Rationale**: FR-011 requires the physical change, its ledger record and the availability
consequence to be one transaction. The reconciliation must run **inside** the transaction rather
than after it, because FR-026 requires a product never to be served as buyable with nothing in
stock: committing the stock first and warning product second leaves exactly that window.

**Alternatives considered**: Writing the ledger first and the level second — rejected: the
conditional update is what decides whether the operation is allowed, so it must run first. Recording
the reconciliation as a deferred event — rejected: no event system exists, and an asynchronous
reconciliation reintroduces the window FR-020 closes.

## D4: The availability contract, and the product-side adapter

**Decision**: A new interface in `internal/contracts/product.go`, consumed by inventory and
implemented by a product adapter:

```go
// ProductAvailability lets the inventory module signal a product's availability to
// the module that owns the sell state. The product module supplies the adapter at the
// composition root; the methods run in the caller's transaction and are idempotent.
type ProductAvailability interface {
    // MarkOutOfStock moves an on-sale product to out of stock. It is a no-op when the
    // product is already out of stock, and it leaves an announced or retired product
    // untouched. An unknown product is reported as ErrProductNotFound.
    MarkOutOfStock(ctx context.Context, productID uuid.UUID) error
    // MarkOnSale moves an out-of-stock product back on sale. It is a no-op when the
    // product is already on sale, and it leaves an announced or retired product
    // untouched. An unknown product is reported as ErrProductNotFound.
    MarkOnSale(ctx context.Context, productID uuid.UUID) error
}
```

Alongside it, a second, read-only interface answers the one product fact inventory cannot derive
itself — whether a product exists:

```go
// ProductLookup answers whether a product exists, so inventory can tell an unknown
// product (not-found) from one that has never been stocked (quantity zero) without
// reading module 04's table.
type ProductLookup interface {
    ProductExists(ctx context.Context, productID uuid.UUID) (bool, error)
}
```

Existence cannot be answered by the foreign key inventory owns: `stock_levels_product_fk` only
fires on an **insert**, so it tells the truth for a restock but says nothing for a read or a
conditional decrease, neither of which writes a row (this is the gap D12 corrects). The lookup is
the arrangement Constitution I prescribes for a question one module must ask another, and it keeps
inventory from ever reading `products`.

The product module implements **both** interfaces in
`internal/modules/product/infrastructure/implement/availability` over its own repository, by loading
the product and calling the entity's existing `SellOut`/`Restock` transitions for the signal and a
single existence read for the lookup. The signal is composed from a new, system-facing use case
(`product/application/implement/availability.go`) rather than the administrator `ChangeSellState`,
because the two have different actors: the administrator path requires and audits an administrator,
while the signal path has no human actor and audits with a nil actor. The lookup needs no use case:
it is a single read.

**Rationale**: Constitution I requires cross-module communication to go through a contract in
`internal/contracts`, with the **providing** module supplying the adapter at the composition root.
Here the provider is product — it owns the sell state — and the consumer is inventory, so the
dependency still runs one way (`inventory → product`), exactly as the roadmap lists. The contract
speaks in availability terms ("out of stock", "on sale"), not in raw state names, so inventory never
learns product's four-state machine; product maps the two signals onto the two edges feature 006
left ready (`SellOut`, `Restock`).

**On idempotence and tolerance**: the signal may arrive for a product already in the target state
(a restock that does not change availability, a retried operation), or for one that must not move at
all (announced, retired). Returning an error for those would fail a stock operation that is itself
correct, so a no-op is the contract's behaviour, and only "no such product" is an error. The
product-side tests cover one positive and one negative for each signal — the "negative" being a
terminal or announced product the signal must not move.

**Alternatives considered**: A generic `Transition(productID, toState)` — rejected: it would leak the
product's state names into inventory and let inventory name an illegal target. Inventory reading or
writing the `products` table — rejected: Constitution I forbids one module touching another's table.
Product subscribing to an inventory event — rejected: no event system exists and it would invert the
roadmap's dependency. Reusing the administrator `ChangeSellState` — rejected: it demands an
administrator actor and would forge one.

## D5: Exactly-once: the source reference and the per-order hold key

**Decision**: An `inventory_transactions` row may carry a nullable `source_reference` (the identity
of an outside event), enforced unique by a **partial unique index** where it is not null. A hold is
keyed by `(order_id, product_id)` with a **partial unique index** on active holds, so an order can
set aside a given product only once. Consuming a hold writes the `SALE` movement with the payment
event's identity as its source reference, so replaying the same payment changes stock once.

**Rationale**: FR-014 to FR-016 and FR-019 require at-most-once application, including under
concurrent arrival. A unique index is the only mechanism that refuses the second concurrent writer:
an application-level "have I seen this?" check is the same read-then-write race D2 refuses. The
unique key for a hold is the order, because FR-019 says so; the unique key for a movement is the
event identity, because FR-020 says so. They are different keys because they answer different
questions — "does this order already hold this product?" versus "have I applied this event?" — and a
retried payment must be idempotent even if the hold row has already been consumed.

**On the replay being a no-op rather than an error**: FR-021 requires a repeat to be accepted as
success, so the adapter recognises a unique-violation on the source reference as "already applied"
and the use case answers success without a second change. Deduplication is only as good as the
identity the caller supplies; the contract records this as module 07's obligation.

**Alternatives considered**: Keying movements by order alone — rejected: one order can have several
lines, so a per-line effect could not be deduplicated. A timestamp-based dedupe — rejected: two
genuine events can share a second. Relying on the hold's uniqueness for the sale too — rejected: the
payment event and the hold are resolved in different operations and the sale must be idempotent on
its own.

## D6: How a hold expires

**Decision**: A **periodic sweeper** started by the composition root calls an application use case
every interval (default **30 seconds**) that releases every hold whose `expires_at` has passed and
has not been resolved, reconciling the product's availability. Availability reads and hold
consumption also treat a hold as active only while `status = 'ACTIVE' AND expires_at > $now`, where
`$now` is the instant the injected `Clock` produced and the application passes **as a query
parameter** — never the database's own `now()` (D15) — so an expired hold that the sweeper has not
yet reached neither blocks availability nor can be consumed, and the sweeper and the availability
predicate agree on what "expired" means.

**Rationale**: FR-015 requires a hold to expire **by itself**, and the delivered system has no
mechanism to run something on a timer; the sweeper is the smallest one. It guarantees eventual
release even for a product nobody touches again — which is the case that matters, because a product
whose last unit is held and never released would otherwise be stuck out of stock forever. The second
half (reads and consumes excluding expired holds) is what makes the system correct **between**
sweeps: without it, a payment arriving one second after expiry would still find the hold active and
consume it, and a customer would be refused goods the shop had already freed.

**Alternatives considered**: Lazy-only release (release when availability is next computed) —
rejected: a product whose hold expires and that receives no further traffic never returns to sale,
so the sell state would be wrong indefinitely. Lazy-only with reconciliation on read — rejected:
reads would have to write, which turns a GET into a mutation and is a different contract. A database
job / `pg_cron` — rejected: it puts business logic outside the application and outside the tests.
Releasing synchronously at exactly fifteen minutes — rejected: nothing can wake at a stored moment
without a scheduler, which is the sweeper by another name.

## D7: What this feature exposes, and what it deliberately does not

**Decision**: The HTTP surface is **five administrator endpoints** (D8) and nothing else. The
reserve/release/consume/expire use cases are delivered and unit-tested directly and are exercised by
the sweeper, but they are **not** published as a cross-module contract yet, because their consumer —
Order (07) and Payment (08) — does not exist. Publishing a contract now would be building for a
caller that is not there (Constitution VII).

**Rationale**: The spec (Scope boundary, Assumptions) states the triggers belong to modules that do
not exist and that the capability is delivered and tested directly. A contract with no consumer
cannot be exercised end to end and would freeze an interface before the consumer's needs are known —
the exact condition module 03's `deferred.md` D4 named for not writing a contract early. The
obligation to publish it is recorded in `deferred.md` so it is not lost.

**Alternatives considered**: Publishing `InventoryReservation` now — rejected: YAGNI, and the shape
would be a guess about Order's needs. Exposing hold endpoints over HTTP now — rejected: holds are
internal to the checkout flow, there is no checkout, and an admin able to hold stock by hand is not
a requirement.

## D8: The routes

**Decision**:

| Method | Path | Who |
|---|---|---|
| GET | `/api/v1/admin/inventory/{productId}` | administrator |
| GET | `/api/v1/admin/inventory/{productId}/movements` | administrator |
| POST | `/api/v1/admin/inventory/{productId}/restock` | administrator |
| POST | `/api/v1/admin/inventory/{productId}/damage` | administrator |
| POST | `/api/v1/admin/inventory/{productId}/adjustment` | administrator |

**Rationale**: The resource is one product's stock and its movement history, addressed by the
product identifier an administrator already holds from the product list. The three writes are
**three endpoints, not one with a `kind` field**, for the same reason module 04 made the state change
its own endpoint: an adjustment sets an absolute counted quantity while restock and damage add and
subtract, so a single body would carry a member whose meaning depends on another member — a shape
that invites a client to send `quantity` with no kind or a negative restock, and forces the server to
reject shapes the contract itself should have made impossible. Three endpoints each document one
intent and one failure mode exactly.

The group is `/admin/inventory`, not `/admin/products/{id}/stock`, because inventory is its own
module: mounting a second router under `/admin/products` would collide with module 04's group and
couple the two modules' route namespaces. `/admin/inventory/{productId}` keeps the modules'
surfaces independent, exactly as `/admin/categories` and `/admin/products` are.

**Alternatives considered**: `PATCH /admin/inventory/{productId}` with a computed quantity —
rejected: it hides whether the client means "add", "remove" or "set", which are the three operations
the module document names. Nested under `/admin/products/{id}` — rejected above.

## D9: What a movement kind is, and what an adjustment means

**Decision**: Four kinds, stored as constrained text: `RESTOCK` (increase), `DAMAGE` (decrease),
`ADJUSTMENT` (set an absolute counted quantity), `SALE` (the decrease a paid hold becomes). Every
movement carries the **signed delta** and the **resulting quantity**. An adjustment is expressed as
its delta: the use case reads the current quantity under the row lock, computes `counted − current`,
and if the difference is zero the operation changes nothing and writes **no** movement (FR edge
case).

**Rationale**: FR-003 and the spec's edge case require an adjustment to an unchanged count to write
nothing, because nothing changed. Storing the delta rather than the absolute value makes the ledger
sum to the physical quantity (SC-001) under one rule for every kind, and makes `RESTOCK`/`DAMAGE`
and `ADJUSTMENT` the same row shape with a different kind — one scan, one reconciliation. `SALE` is
distinct from `DAMAGE` because the two are different events with different sources (a payment versus
a person) and the ledger is read by operators trying to explain a quantity.

**Alternatives considered**: Storing the absolute quantity instead of the delta — rejected: the
ledger would then need two reading rules (sum for restock/damage, last-wins for adjustment) and SC-001
would be a special case rather than a sum. Folding `SALE` into `DAMAGE` — rejected: it makes a sale
indistinguishable from breakage in the history an operator reads.

## D10: The reconciliation rule, stated once

**Decision**: After a stock change, the use case computes availability before and after and asks
product to change its state only when **availability crossed zero**:

| before | after | signal |
|---|---|---|
| > 0 | > 0 | none (a non-crossing change leaves the operator's marking alone) |
| > 0 | = 0 | `MarkOutOfStock` |
| = 0 | > 0 | `MarkOnSale` |
| = 0 | = 0 | none |

**Rationale**: FR-024 to FR-026 require the change on a crossing; FR-027 and FR-028 require announced
and retired products to be untouched and a non-crossing change to preserve the operator's deliberate
out-of-stock marking. Calling the signal on every change would flip a product the operator marked out
of stock back on sale the next time it was restocked without crossing zero, which FR-028 forbids.
The four-row table is the whole rule, and it is expressed once in the use case and covered by a test
per row.

**Alternatives considered**: Signalling on every change and relying on the product adapter's
idempotence — rejected: the adapter's no-op preserves the target state, not the operator's marking,
so a non-crossing restock would still move a manually-marked product back on sale. Deriving the
signal inside the product module — rejected: product cannot read inventory.

## D11: What happens to a product's stock when the product is removed

**Decision**: `stock_levels`, `inventory_transactions` and `stock_holds` all reference the product
with **`ON DELETE CASCADE`**. Removing a product removes its stock, its holds and its ledger rows.

**Rationale**: Feature 006 chose a hard delete for a product, and the spec's edge case states that a
removed product leaves stock management with the product and that preserving inventory history
beyond a product's life is not required for MVP — the audit trail keeps the fact of the removal. A
cascade is the same choice feature 006 made for a product's pictures, and it is the only one that
does not either block module 04's existing removal (a `RESTRICT`) or leave rows pointing at a
product that cannot be named (`SET NULL`, which the not-null columns refuse anyway).

**Alternatives considered**: `RESTRICT` so history is never lost — rejected: it would make module 04's
existing hard delete start failing for any product that ever had stock, a behavioural change module
04 did not ask for and the spec's edge case does not require. A loose reference with no foreign key
so ledger rows outlive the product — rejected as premature: feature 006 named module 07 as the owner
of the product-removal decision (soft delete or a snapshot on the order line), and this feature
should not pre-empt that with a second durable copy of history.

## D12: How a product that has never been stocked is handled

**Decision**: A product with no `stock_levels` row is understood as quantity **zero**. An increase
materialises the row (`INSERT ... ON CONFLICT (product_id) DO UPDATE SET quantity =
stock_levels.quantity + ...`); a decrease that finds no row is an oversell and is refused.
**Existence is answered by `ProductLookup` (D4), not by the foreign key**: the read path, the damage
path and the adjustment path resolve the product through the contract and answer not-found when it is
absent, because a read and a conditional decrease write no row and so never reach the foreign key.
The foreign key on `stock_levels` remains the storage guard that refuses a level row for a product
that does not exist, as a second line of defence for the one path that does write.

**Rationale**: Products are created by module 04 and this module has no hook on creation (and adding
one would couple the two modules), so a stock row must be able to appear lazily. Existence has to be
answerable on **every** path, not only the writing one: the read endpoint must not fabricate a zero
for a product that was never created (FR-007), and damage on an unknown product must answer not-found
rather than an oversell. A foreign key cannot do that, because it only fires on an insert; the
contract is what answers without either module reading the other's table.

**Alternatives considered**: Relying on the foreign key alone — rejected: it is silent for a read and
a decrease, the two paths that most need the answer (this is the gap the design review found).
Materialising a zero row on the read path so the foreign key fires — rejected: it turns a GET into a
write, and a read that mutates is a different and worse contract. Creating the level row from module
04 on product creation — rejected: it would make module 04 depend on inventory, inverting the
roadmap. Retro-listing every existing product into `stock_levels` in the migration — rejected: it
pre-creates rows the shop may never use, and a product added later would still need lazy creation, so
two paths would exist.

## D13: The route of the acting administrator, and the actor on the ledger

**Decision**: The acting administrator is taken from the session through the shared role guard and
the `ActorFromContext` helper, never from the request (FR-005). A manual movement stores that
actor's identifier on the ledger row; a `SALE` movement stores **no** actor, because no human caused
it, and carries the payment event's reference instead (D5).

**Rationale**: FR-005 and the project's standing rule require the actor to come from the session to
prevent cross-account access. The ledger's `actor_id` is a nullable, informative field — it answers
"who did this?" for the operator reading the history — and a system-caused sale genuinely has no
human actor, so `NULL` is truthful rather than a placeholder. The audit trail is separate and is
written for **both** paths (FR-006): a manual operation names the administrator, and a system event
names its source reference with a nil actor, so Constitution VI's "payment events are recorded" is
satisfied even though module 08 does not exist yet. A hold's reserve, release and expiry are
operational acts and are recorded on the hold row, not in the audit trail.

**Alternatives considered**: Storing a sentinel system actor — rejected: it invents an account that
does not exist and would render as a broken link in any admin UI. Dropping `actor_id` and relying on
audit alone — rejected: the history an operator reads must name the person who made the change, and
joining to the audit table per row is a worse read than one column.

## D14: The history is paginated and ordered

**Decision**: A product's movement history is paginated under the project's existing list
convention (a page and a page size defaulting to 1 and 20, the size permitted between 1 and 100) and
ordered by `created_at, id` ascending — oldest first — so the story of how a quantity came to be
reads in the order it happened.

**Rationale**: FR-008 requires the history in the order the changes happened and paginated. The
project's pagination convention already exists and is what every other list answers. `created_at`
alone can tie for two movements in the same transaction or the same millisecond, so `id` breaks the
tie so the order is stable across identical requests, exactly as module 04 orders its catalogue.

**Alternatives considered**: Newest-first — rejected: the spec asks for the order the changes
happened, and a reconciliation is read forwards. Unpaginated — rejected: a busy product's history
grows without bound.

## D15: The clock is injected

**Decision**: The use cases read the current time through a `Clock` port declared in
`application/interface`, defaulting to `time.Now` in the composition root. The instant the clock
produces is passed **as a parameter** to every storage predicate that decides expiry
(`expires_at > $now`) and to the sweep's selection; the database's own `now()` is never consulted
for a business decision.

**Rationale**: The hold window and the sweep are time-dependent, and a test that proves
"a hold is released after fifteen minutes" must not sleep for fifteen minutes. A one-method port is
the smallest seam that makes expiry deterministic; the domain still computes with `time.Time` values
it is handed, so the rule stays in the domain and only "what time is it" is injected. Passing the
instant into the SQL rather than letting the query call `now()` is what makes the injected clock
**authoritative**: otherwise the sweeper (driven by the injected clock) and the availability sum
(driven by the database clock) could disagree about whether a hold has expired, and the test in T042
could not observe the behaviour at all.

**Alternatives considered**: Passing `now` as a parameter from the handler — rejected: the sweeper
and the tests both call the use cases without an HTTP request, so the seam belongs on the use case,
not the transport. Letting SQL use `now()` and dropping the injected clock — rejected: expiry would
then be untestable without real waiting, and the sweeper's clock and the query's clock would be two
sources of truth. Sleeping in tests — rejected: a test suite that waits fifteen minutes will not be
run.

## Open questions carried into implementation

None. Every decision above is settled and each is testable.
