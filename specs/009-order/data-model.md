# Phase 1 Data Model: Order Checkout

Two new tables. The decisions behind their shape are in `research.md`; this file records what is
stored, what is constrained, and what the read paths project.

## Entity: Order

A purchase a customer has committed to: who owns it, where it goes, what it costs, and where it is in
its life.

### Stored columns

| Column | Type | Null | Meaning |
|---|---|---|---|
| `id` | uuid | no | Primary key. Stable for the order's life; the operator and the customer address it by this |
| `user_id` | uuid | no | The account that owns the order. A **loose reference, no foreign key** (research D12), so an order outlives the account it names; a transfer changes it |
| `status` | text | no | One of the five states (research D9). Changed only through the domain transitions |
| `total_amount` | bigint | no | The committed total in the currency's minor unit: the sum of `quantity × unit_price_amount` over the lines (FR-008, research D11) |
| `currency` | char(3) | no | The currency the total is in; three uppercase letters |
| `recipient_name` | text | no | The delivery name, captured from the chosen address (FR-003) |
| `recipient_phone` | text | no | The delivery phone, captured from the chosen address |
| `province_code` | text | no | The first-level unit code, captured at checkout |
| `province_name` | text | no | The first-level unit name, captured at checkout |
| `ward_code` | text | no | The second-level unit code, captured at checkout |
| `ward_name` | text | no | The second-level unit name, captured at checkout |
| `street_address` | text | no | The free-text street, captured at checkout |
| `expires_at` | timestamptz | no | When an unpaid order cancels itself (created plus module 05's hold window; research D6) |
| `created_at` | timestamptz | no | Set once; the order was placed now |
| `updated_at` | timestamptz | no | Touched on every write |

### Constraints, and why each is at the storage layer

| Constraint | Kind | Requirement | Why not only in the application |
|---|---|---|---|
| `orders_pkey` | primary key on `id` | — | An order is addressable and lockable for a transition |
| `orders_status_ck` | check: `status IN ('PENDING_PAYMENT','PAID','SHIPPED','COMPLETED','CANCELLED')` | FR-009 | An unlisted state cannot be stored, so the domain's transition table cannot be bypassed by writing a value |
| `orders_currency_ck` | check: `currency ~ '^[A-Z]{3}$'` | FR-008 | The currency is an unambiguous three-letter code |
| `orders_total_ck` | check: `total_amount > 0` | FR-008 | An order of at least one positive-price line has a positive total; a non-positive total is refused where it is stored |
| `orders_owner_idx` | btree on `(user_id, created_at, id)` | FR-018 | The customer's list is one owner's orders, newest first |
| `orders_admin_idx` | btree on `(created_at, id)` | FR-021 | The operator's list is every order, newest first |
| `orders_expiry_idx` | btree on `(status, expires_at)` | FR-012 | The sweeper finds unpaid orders past their deadline without scanning every order |

**On the missing foreign key to `users`**: `user_id` is a loose reference. An order is a record of a
sale; removing the account it names must not delete or block the order (research D12). That a transfer
recipient exists is checked at the source of truth — the auth module — not by a key here.

## Entity: Order line

One item of an order. It holds a **snapshot** of the product as it was at checkout, so it never
depends on the product still existing (FR-002, research D3).

### Stored columns

| Column | Type | Null | Meaning |
|---|---|---|---|
| `id` | uuid | no | Primary key |
| `order_id` | uuid | no | The order it belongs to. References `orders(id)` with a **cascading** delete |
| `product_id` | uuid | no | The product bought, as an **informational** reference with **no foreign key** (research D3). It is recorded, never read to resolve the product |
| `name` | text | no | The product's name at checkout |
| `slug` | text | no | The product's link segment at checkout |
| `unit_price_amount` | bigint | no | The unit price at checkout, in the currency's minor unit |
| `currency` | char(3) | no | The currency the unit price is in |
| `quantity` | bigint | no | How many, at least 1 |
| `position` | integer | no | The order the lines are listed in |
| `created_at` | timestamptz | no | Set once |

### Constraints

| Constraint | Kind | Requirement | Why not only in the application |
|---|---|---|---|
| `order_items_pkey` | primary key on `id` | — | A line is addressable |
| `order_items_order_fk` | foreign key → `orders(id)` **ON DELETE CASCADE** | FR-001 | A removed order takes its lines with it |
| `order_items_order_product_key` | unique index on `(order_id, product_id)` | FR-002 | A cart holds one line per product, so an order does too; the pair is the natural key |
| `order_items_quantity_ck` | check: `quantity >= 1` | FR-002 | A line of zero or fewer is not a line |
| `order_items_unit_price_ck` | check: `unit_price_amount > 0` | FR-008 | A snapshot price is positive |
| `order_items_currency_ck` | check: `currency ~ '^[A-Z]{3}$'` | FR-008 | The currency is unambiguous |
| `order_items_ordering_idx` | btree on `(order_id, position, id)` | FR-018, FR-021 | The lines are read in order for every order read |

**On the snapshot**: `name`, `slug`, `unit_price_amount` and `currency` are values copied at checkout;
they are never joined back to a product. `product_id` is stored for the plan's record of what was
bought, but the line's shown data is the snapshot, so a product's later rename, re-price or removal
changes nothing on the order.

## Transitions

```
PENDING_PAYMENT ──pay──► PAID ──ship──► SHIPPED ──complete──► COMPLETED
        │
        └──cancel (customer or expiry)──► CANCELLED   (no way out)
```

| Transition | From | To | Refuses when | Trigger | Audit |
|---|---|---|---|---|---|
| pay | `PENDING_PAYMENT` | `PAID` | the order is not awaiting payment | module 08, through `MarkPaid` (research D13) | — |
| ship | `PAID` | `SHIPPED` | the order is not paid | administrator | `ORDER_SHIPPED` |
| complete | `SHIPPED` | `COMPLETED` | the order is not shipped | administrator | `ORDER_COMPLETED` |
| cancel | `PENDING_PAYMENT` | `CANCELLED` | the order is not awaiting payment | the customer, or the expiry sweep | — (a customer act) |
| transfer | *(state unchanged)* | *(state unchanged)* | the order is not paid | administrator | `ORDER_TRANSFERRED` |

**A refused transition names the current state** (FR-011) and leaves the order exactly as it was.
**Transfer is not a transition**: it changes the owner only and leaves the state, lines and totals
untouched (FR-024, research D12).

**Pay, cancel, expiry and every admin move run inside a `UnitOfWork` transaction** that also calls
module 05 (`ApplySale` on pay, `Release` on cancel and expiry), so the order state and the stock
effect commit together or not at all.

## What each read path projects

| Path | Reader | Columns | Requirement |
|---|---|---|---|
| customer list | signed-in customer | `id`, `status`, `total_amount`, `currency`, `created_at`, and the line count | FR-018 |
| customer detail | signed-in customer | every order column, and every line in order | FR-018 |
| administrator list | administrator | the same as the customer list, plus the owner | FR-021 |
| administrator detail | administrator | every order column and every line | FR-021 |

Both lists are one owner's / every order's, newest first (`created_at`, then `id`), paginated under the
project's convention (research D14).

## What this feature changes outside its own tables

| Change | Where | Why |
|---|---|---|
| A new cross-module contract `CartCheckout` | `internal/contracts/cart.go` | The order reads and clears the cart through the cart module (research D1) |
| A new cross-module contract `InventoryReservation` | `internal/contracts/inventory.go` | The order holds, consumes and releases goods through module 05 — the contract module 05 withheld until this consumer existed (research D4) |
| A new cross-module contract `AccountLookup` | `internal/contracts/account.go` | A transfer is named by email, which the auth module owns (research D8) |
| A cart adapter, an inventory adapter and an auth adapter | `internal/modules/{cart,inventory,auth}/infrastructure/implement/{checkout,reservation,account}/` | Each providing module implements its contract over its own repository, supplied at the composition root |
| A bulk `ClearLines` on the cart repository | `internal/modules/cart/domain/repository/cart.go` and its adapter | Emptying the cart is one statement inside the order's transaction (research D1) |
| A command-line wiring point and a sweeper start/stop | `cmd/api/main.go` | Mounts the module, wires the contracts, starts the expiry sweeper (research D6) |

No column, index or constraint of an existing table is touched, and no data is backfilled.
