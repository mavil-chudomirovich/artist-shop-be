# Data Model — Hide Guard with Pending Orders (016)

One new column; no new tables. The two bulk reads and the bulk delete below are the only new storage
access, each a single bounded statement.

## Entity changes

### Product visibility flag (new column)

`products.is_visible boolean NOT NULL DEFAULT true` via `migrations/00016_product_visibility_flag.sql`
(Down: drop the column). Validation: boolean, no other constraint; admin edits flip it like any
product field. The public catalogue predicate becomes: category visible **and** product flag on
**and** at least one on-sale variant — the new term sits beside the existing ones in
`internal/modules/product/infrastructure/implement/postgres/product.go`.

No other table changes. `categories.is_visible`, `product_variants.sell_state`, `orders.status`,
`order_items.variant_id`, `cart_items.variant_id` are all reused as-is.

## Reads and writes (all new, all bounded)

### Pending orders for variants (new, order module)

`order_items JOIN orders`: `SELECT DISTINCT oi.order_id FROM order_items oi JOIN orders o ON
o.id = oi.order_id WHERE oi.variant_id = ANY($1) AND o.status IN ('PENDING','PAYMENT_PENDING')`.
New repository method on order's `domain/repository/order.go`, implemented in its postgres adapter.
One statement however many variants are passed.

### Variants of categories (new, product module)

`SELECT id FROM product_variants WHERE product_id IN (SELECT id FROM products WHERE category_id =
ANY($1))`. New repository method on product's `domain/repository/product.go`, implemented in its
postgres adapter. Flat ID set out; no product internals cross the boundary.

### Cart cleanup (new, cart module)

`DELETE FROM cart_items WHERE variant_id = ANY($1)` returning the removed count. New repository
method on cart's `domain/repository/cart.go`, implemented in its postgres adapter, called only from
inside the hiding use case's transaction.

### Blocked system signal (no-op)

A blocked automatic stock-out leaves the variant's on-sale state untouched while its physical stock
change still commits (with its `inventory_transactions` record, per Constitution II). Checkout stays
safe because it re-checks both sell state and available stock before an order is created.

## Validation rules (from the spec)

- Pending = PENDING or PAYMENT_PENDING, nothing else (FR-001); enforced by the status filter in the
  bulk query, not by caller discipline.
- Refusal names blocking order IDs on the admin surface only (FR-004/FR-005/FR-006).
- A product flag edit is an ordinary validated product edit plus the guard (FR-006).
- Cart cleanup removes exactly the lines holding the hidden variants, in the same transaction as the
  hide (FR-007); an empty cart afterwards is valid.
- State-machine rules for variants are unchanged; only the guard is added around moves away from
  on-sale (FR-005).
