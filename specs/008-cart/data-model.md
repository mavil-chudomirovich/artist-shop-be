# Phase 1 Data Model: Shopping Cart

Two new tables. The decisions behind their shape are in `research.md`; this file records what is
stored, what is constrained, and what the read path projects.

## Entity: Cart

The one working basket of a signed-in customer.

### Stored columns

| Column | Type | Null | Meaning |
|---|---|---|---|
| `id` | uuid | no | Primary key. Generated once; never exposed to the client (the cart is addressed as `/cart`, not by id) |
| `user_id` | uuid | no | The one account the cart belongs to. References `users(id)` with a **cascading** delete, so an account's cart leaves with it |
| `created_at` | timestamptz | no | Set once |
| `updated_at` | timestamptz | no | Touched on every write |

### Constraints, and why each is at the storage layer

| Constraint | Kind | Requirement | Why not only in the application |
|---|---|---|---|
| `carts_pkey` | primary key on `id` | — | A cart is addressable and lockable for a write |
| `carts_user_key` | unique index on `user_id` | FR-001 | "One cart per account" is a uniqueness property; two concurrent first adds both pass an application check and both insert, and only the database can refuse the second |
| `carts_user_fk` | foreign key → `users(id)` **ON DELETE CASCADE** | FR-001 | An account's cart belongs to the account; removing the account removes its cart |

**A customer with no cart row is understood as an empty cart** (research D8): `GET /cart` answers an
empty cart without creating a row, and the row appears on the first add.

## Entity: Cart item

One product the customer intends to buy, with the quantity they chose and the price they were shown
when they added it.

### Stored columns

| Column | Type | Null | Meaning |
|---|---|---|---|
| `id` | uuid | no | Primary key; the line's own identity, not exposed to the client |
| `cart_id` | uuid | no | The cart it belongs to. References `carts(id)` with a **cascading** delete |
| `product_id` | uuid | no | The product. A **loose reference — no foreign key** (research D3), so the line survives the product's removal and can be reported as no longer available (FR-012) |
| `quantity` | bigint | no | How many the customer chose. A whole number, at least 1 |
| `unit_price_amount` | bigint | no | The price in the currency's minor unit, **captured when the product was added** (FR-008, research D4). Never re-read |
| `currency` | char(3) | no | The currency the captured amount is denominated in; three uppercase letters |
| `created_at` | timestamptz | no | Set once |
| `updated_at` | timestamptz | no | Touched on every write |

### Constraints, and why each is at the storage layer

| Constraint | Kind | Requirement | Why not only in the application |
|---|---|---|---|
| `cart_items_pkey` | primary key on `id` | — | A line is addressable for a change or removal |
| `cart_items_cart_product_key` | unique index on `(cart_id, product_id)` | FR-002 | "One line per product" is a uniqueness property; two concurrent adds both pass an application check and both insert, and the database is what refuses the duplicate — the add is an **upsert** on this key, so the second sums the quantity instead of failing |
| `cart_items_cart_fk` | foreign key → `carts(id)` **ON DELETE CASCADE** | FR-001 | A removed cart takes its lines with it |
| `cart_items_quantity_ck` | check: `quantity >= 1` | FR-006 | A line holding zero or fewer is not a line; a customer empties a line by removing it, and the database refuses a stored zero |
| `cart_items_unit_price_ck` | check: `unit_price_amount > 0` | FR-009 | A captured price is positive, as module 04's prices are; a zero or negative line cannot be stored |
| `cart_items_currency_ck` | check: `currency ~ '^[A-Z]{3}$'` | FR-009 | The captured currency is an unambiguous three-letter code |

**On the loose product reference**: `product_id` carries **no** foreign key. This is the deliberate
asymmetry with an order line (research D3): removing a product must leave the customer's line in place
so the cart can tell them it is no longer available, which a cascading key would silently delete and a
restricting key would block. The product's absence is answered on read through `ProductCatalog` (D1).

**On the single currency per line**: the captured currency travels with each line's amount, as the
constitution requires money to carry its currency. The MVP shop is single-currency (spec, Assumptions),
so every line in one cart shares a currency, the cart does no conversion, and a mismatch never
arises. **An empty cart carries no line and therefore no money**, so its `subtotal` is **absent
(null)** rather than a zero in a currency the shop would have to invent (spec, Assumptions).

## What the read path projects

| Path | Reader | Columns | Also fetched | Requirement |
|---|---|---|---|---|
| cart view | signed-in customer | `product_id`, `quantity`, `unit_price_amount`, `currency` | the live `name`/`slug`/on-sale state of each product (via `ProductCatalog`) and the available quantity of each product (via `InventoryAvailability`) | FR-004, FR-012 |

The view derives, per line, the line total (`quantity × unit_price_amount`) and the `buyable` flag,
and derives the subtotal as the sum of the line totals (research D6). It reports, per line, the
current available quantity **only when the line is on sale but short** (research D10); it re-checks
neither the product's availability beyond that nor the price (FR-008). The product's name and slug are
live and absent when the product is gone; the price shown is always the stored snapshot.

## The write paths, and what each touches

| Act | Writes | Refuses when |
|---|---|---|
| add | creates the cart if absent; **upserts** the line — a new line captures the product's current price and currency, and a product already held has its quantity summed while **keeping the price captured when it was first added** (so the line's price does not move under the customer; the checkout re-checks it) | the product does not exist or is not on sale (`PRODUCT_NOT_FOUND` / `CART_PRODUCT_NOT_PURCHASABLE`), the summed quantity would exceed what is available (`CART_QUANTITY_EXCEEDS_AVAILABLE`), or the quantity is not a positive whole number (`VALIDATION_ERROR`) |
| change quantity | sets the line's quantity | the product is not on sale, the requested quantity exceeds what is available, the quantity is not a positive whole number, or the line does not exist (`PRODUCT_NOT_FOUND`, treated as no such line in this cart) |
| remove | deletes the line | the product is not a line of this cart (`PRODUCT_NOT_FOUND`, 404) — the same not-found an unknown product answers, so the route never confirms another cart's contents |

Every write runs inside the `UnitOfWork` transaction that locks the cart's row (research D9), so
`carts.updated_at`, the line, and the availability decision commit together or not at all.

## What this feature changes outside its own tables

| Change | Where | Why |
|---|---|---|
| A new cross-module contract `ProductCatalog` | `internal/contracts/product.go` | The cart needs a product's name, on-sale state and price, which only module 04 owns (research D1) |
| A new cross-module contract `InventoryAvailability` | `internal/contracts/inventory.go` | The cart needs what is available, which only module 05 owns; it is a new **availability read**, published because a consumer exists (research D2). It is not the **reservation** contract module 05's `deferred.md` D1 keeps open |
| A product-side adapter | `internal/modules/product/infrastructure/implement/catalog/` | Implements `ProductCatalog` over module 04's own repository, supplied at the composition root |
| An inventory-side adapter | `internal/modules/inventory/infrastructure/implement/availability/` | Implements `InventoryAvailability` over module 05's own repository, computing `Level − ActiveHeld` at the module's clock |
| A bulk read on each providing repository | `internal/modules/product/.../postgres/product.go`, `internal/modules/inventory/.../postgres/inventory.go` | Each adapter answers the whole requested set in one indexed query (`= ANY(...)`), so a cart view is not one query per line (research D1, D2) |
| A command-line wiring point | `cmd/api/main.go` | Mounts the module and wires both contracts |

No column, index or constraint of an existing table is touched, and no data is backfilled.
