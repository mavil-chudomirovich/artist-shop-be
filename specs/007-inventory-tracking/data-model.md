# Phase 1 Data Model: Inventory Tracking

Three new tables. The decisions behind their shape are in `research.md`; this file records what is
stored, what is constrained, and what the read paths project.

The three are different facts about one product, and the spec needs all three (D1): `stock_levels`
is what is on the shelf, `stock_holds` is what has been set aside, and `inventory_transactions` is
how the quantity came to be. Availability — the only number a customer can take — is
`stock_levels.quantity` minus the active holds, and is never stored.

## Entity: Stock level

The physical count of one product, in its own row so it can be the lock the no-negative rule is
enforced on (D2).

### Stored columns

| Column | Type | Null | Meaning |
|---|---|---|---|
| `product_id` | uuid | no | Primary key and the one product this level belongs to. References `products(id)` with a **cascading** delete (D11) |
| `quantity` | bigint | no | The physical units on the shelf. A whole number, never negative |
| `updated_at` | timestamptz | no | Touched on every write |

### Constraints, and why each is at the storage layer

| Constraint | Kind | Requirement | Why not only in the application |
|---|---|---|---|
| `stock_levels_pkey` | primary key on `product_id` | D1 | One level per product; a second row for the same product is how two writers would disagree |
| `stock_levels_quantity_ck` | check: `quantity >= 0` | FR-009 | The last line of defence: even a code path that bypassed the conditional update cannot store a negative count |
| `stock_levels_product_fk` | foreign key → `products(id)` **ON DELETE CASCADE** | D11, D12 | A removed product's stock leaves with it; the foreign key is the storage guard that refuses a level row for a product that does not exist, as a second line of defence behind the `ProductLookup` contract, which is what answers existence on the read and decrease paths |

**A product with no row is understood as zero** (D12). The row is materialised lazily by the first
increase; that is why there is no backfill for the products already in the catalogue. **Existence is
answered by the `ProductLookup` contract, not by the row** (research D4, D12): the read, damage and
adjustment paths ask the product module whether the product exists and answer not-found when it does
not, because those paths write no row and so never reach the foreign key. The foreign key remains the
storage guard for the one path that does write a row.

## Entity: Movement (`inventory_transactions`)

The append-only ledger. It is the traceable history of every **physical** change; a hold is not one
and does not appear here (D1, D9).

### Stored columns

| Column | Type | Null | Meaning |
|---|---|---|---|
| `id` | uuid | no | Primary key |
| `product_id` | uuid | no | The product whose quantity changed. References `products(id)` with a **cascading** delete (D11) |
| `kind` | text | no | One of the four kinds below (D9) |
| `delta` | bigint | no | The **signed** change: positive for a restock, negative for damage and a sale, positive or negative for an adjustment |
| `resulting_quantity` | bigint | no | The physical quantity after this change, so the history is self-contained |
| `source_reference` | text | yes | The identity of the outside event that caused the change (a payment), for exactly-once application (D5). Null for a manual change |
| `actor_id` | uuid | yes | The administrator who made a manual change; null for a system-caused sale (D13) |
| `note` | text | yes | An optional free-text note a manual change carries, so a damage or a correction can say why. Null for a system-caused change |
| `created_at` | timestamptz | no | Set once. The history's order |

### Constraints

| Constraint | Kind | Requirement | Why not only in the application |
|---|---|---|---|
| `inventory_transactions_pkey` | primary key on `id` | — | A movement is addressable |
| `inventory_transactions_kind_ck` | check: `kind IN ('RESTOCK','DAMAGE','ADJUSTMENT','SALE')` | FR-004, D9 | An unlisted kind cannot be stored |
| `inventory_transactions_delta_ck` | check: `delta <> 0` | D9 | A change that changes nothing is not a change; the ledger holds no empty rows |
| `inventory_transactions_resulting_ck` | check: `resulting_quantity >= 0` | FR-009 | The recorded result can never describe a negative shelf |
| `inventory_transactions_source_key` | **partial** unique index on `(source_reference)` where it is not null | FR-020, FR-022 | The mechanism that makes an outside event apply at most once, including concurrently |
| `inventory_transactions_product_fk` | foreign key → `products(id)` **ON DELETE CASCADE** | D11 | A removed product takes its history with it, as feature 006 chose for its pictures |
| `inventory_transactions_history_idx` | btree on `(product_id, created_at, id)` | FR-008 | The history read, in order (D14) |

**On the reconciliation SC-001 asserts**: because every kind stores its signed delta and the level is
updated by exactly that delta in the same transaction, `SUM(delta)` over a product's movements equals
`stock_levels.quantity` at all times. A hold does not change either, so it cannot break the identity;
only the point a hold becomes a sale writes a movement and moves the level together (FR-016).

## Entity: Hold (`stock_holds`)

A quantity set aside for one order while it is being paid (the clarification). It is the difference
between what is on the shelf and what is available.

### Stored columns

| Column | Type | Null | Meaning |
|---|---|---|---|
| `id` | uuid | no | Primary key |
| `product_id` | uuid | no | The product set aside. References `products(id)` with a **cascading** delete (D11) |
| `order_id` | uuid | no | The order the hold belongs to. A loose reference: module 07 does not exist and inventory may not guess its table (D5) |
| `quantity` | bigint | no | How many units are set aside. A positive whole number |
| `status` | text | no | `ACTIVE`, `CONSUMED` or `RELEASED` |
| `expires_at` | timestamptz | no | When an unpaid hold returns its quantity. Computed in the application from the injected clock's instant plus the hold window (research D15), so the same time source creates the hold and judges it |
| `created_at` | timestamptz | no | Set once |
| `resolved_at` | timestamptz | yes | When the hold stopped being active — paid, cancelled or expired |

### Constraints

| Constraint | Kind | Requirement | Why not only in the application |
|---|---|---|---|
| `stock_holds_pkey` | primary key on `id` | — | A hold is addressable by the sweep and by consumption |
| `stock_holds_quantity_ck` | check: `quantity > 0` | FR-018 | Setting aside nothing is not a hold |
| `stock_holds_status_ck` | check: `status IN ('ACTIVE','CONSUMED','RELEASED')` | clarification | The three ways a hold ends, and no others, cannot be stored |
| `stock_holds_active_key` | **partial** unique index on `(order_id, product_id)` where `status = 'ACTIVE'` | FR-019 | An order cannot set aside the same product twice, even from two concurrent beginnings |
| `stock_holds_resolved_ck` | check: `(status = 'ACTIVE') = (resolved_at IS NULL)` | clarification | A resolved hold has a time and an active one does not; the two facts cannot disagree |
| `stock_holds_product_fk` | foreign key → `products(id)` **ON DELETE CASCADE** | D11 | A removed product's holds leave with it |
| `stock_holds_active_idx` | partial btree on `(product_id)` where `status = 'ACTIVE'` | FR-018 | The availability sum reads active holds per product |
| `stock_holds_sweep_idx` | btree on `(status, expires_at)` | FR-015 | The sweeper finds expired active holds without scanning resolved ones (D6) |

**What a hold's `order_id` deliberately is not**: a foreign key. Module 07 owns orders and does not
exist, so a foreign key would either point at a table inventory must not name or would need module 07
built first (which is out of order). The identifier is stored so the hold is meaningful when Order
arrives; the obligation to reconcile the reference belongs with that module's specification.

## The three events, and what each writes

| Event | Level | Ledger | Hold | Availability signal | Audit |
|---|---|---|---|---|---|
| restock (`RESTOCK`) | `+q` | one row, `delta = +q` | — | on a zero crossing (D10) | `INVENTORY_RESTOCKED` |
| damage (`DAMAGE`) | `−q`, refused if `quantity < q` | one row, `delta = −q` | — | on a zero crossing | `INVENTORY_DAMAGED` |
| adjustment (`ADJUSTMENT`) | set to the counted value | one row, `delta = counted − current`, **omitted when equal** | — | on a zero crossing | `INVENTORY_ADJUSTED` |
| reserve (payment begins) | unchanged | — | one `ACTIVE` hold, `expires_at = $now + 15m` from the injected clock; refused if it exceeds availability | on a zero crossing (availability falls) | — |
| consume (order paid) | `−held` | one row, `kind = SALE`, `delta = −held`, `source_reference = event` | that hold → `CONSUMED` | none (availability is unchanged) | `INVENTORY_SALE_APPLIED` (no human actor, naming the reference); plus `PRODUCT_STATE_CHANGED` on the product side when availability crosses |
| release (order cancelled) | unchanged | — | that hold → `RELEASED` | on a zero crossing (availability rises) | — |
| expire (sweeper) | unchanged | — | every overdue active hold → `RELEASED` | on a zero crossing per product | — |

**Reserving and releasing are not movements.** They change what is available and nothing on the
shelf, which is why the ledger does not hold them (FR-004) and why the reconciliation SC-001 asserts
ignores them.

**Every write on this table runs through the use cases' `UnitOfWork`**, so a level change, its
ledger row and any availability signal commit together or not at all (FR-011, D3).

## The availability rule, stated once

```
available(product) = stock_levels.quantity
                   − Σ stock_holds.quantity  WHERE product = product
                                              AND status  = 'ACTIVE'
                                              AND expires_at > $now
```

`$now` is the instant the injected `Clock` produced, passed as a query parameter (research D15); the
database's own `now()` is never consulted, so the sweeper and this sum agree on what "expired" means.

It is computed inside the transaction that changes any of its inputs, before and after, to decide
whether availability crossed zero and therefore whether to signal product (D10). It is never stored,
so it cannot drift from the rows it is derived from — the staleness a stored copy would introduce is
exactly the "copy that can drift" FR-013 refuses.

## What each read path projects

| Path | Reader | Columns | Requirement |
|---|---|---|---|
| stock read | administrator | `quantity`, the summed active hold quantity, and their difference as `available` | FR-007 |
| movement history | administrator | `id`, `product_id`, `kind`, `delta`, `resulting_quantity`, `source_reference`, `actor_id`, `note`, `created_at`, paginated | FR-008 |
| availability (internal) | a future cart/order use case | the same computed `available` | FR-013 |

The stock read answers three numbers — physical, held, available — because the spec's FR-007 names
all three and an operator reconciling a shelf needs to see what is set aside as well as what is on
it. The history read carries `resulting_quantity` so an operator can follow the count forward without
re-summing, and `source_reference` so a movement caused by an order can be traced to it.

## What this feature changes outside its own tables

| Change | Where | Why |
|---|---|---|
| Two new cross-module contracts, `ProductAvailability` and `ProductLookup` | `internal/contracts/product.go` | Inventory must signal a product's availability and must ask whether a product exists; product owns the sell state and the product rows, so both contracts are declared once and product supplies the one adapter (D4) |
| A product-side adapter and a system-facing use case | `internal/modules/product/...` | The contract's implementation, deliberately separate from the administrator path because the actor differs (D4) |
| A command-line wiring point and a sweeper start/stop | `cmd/api/main.go` | The module is mounted, the contract is wired, and the sweeper runs (D6) |

No column, index or constraint of an existing table is otherwise touched, and no data is backfilled:
a product with no stock row is understood as zero (D12).
