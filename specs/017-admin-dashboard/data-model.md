# Data Model — Admin Dashboard Overview (017)

No new tables and no new admin-owned data store. One additive change to the order line
carries the category snapshot; everything else is a read projection over data the
existing modules already own.

## Entity changes

### Order line category snapshot (new columns)

`migrations/00017_order_item_category_snapshot.sql`:

| Column | Type | Null | Meaning |
|---|---|---|---|
| `order_items.category_id` | `uuid` | yes | The category the product belonged to when the line was bought. Loose reference; no foreign key because a category may be deleted after the sale. |
| `order_items.category_name` | `text` | yes | The category name at the same instant, so a later rename cannot change historical revenue labels. |

Constraints:

- `order_items_category_ck CHECK ((category_id IS NULL) = (category_name IS NULL))` —
  a half-snapshot is not a state the dashboard can interpret.
- No FK to `categories`: the line is a snapshot and must outlive the category.
- No new index: the category breakdown joins `order_items` by `order_id` (covered by
  `order_items_ordering_idx (order_id, position, id)`) and groups the current month's
  lines; a separate category index is not justified at MVP volume (YAGNI).

Backfill (in the same migration, marked `-- data-migration:`):

```sql
UPDATE order_items oi
SET category_id = p.category_id,
    category_name = c.name
FROM product_variants v
JOIN products p ON p.id = v.product_id
JOIN categories c ON c.id = p.category_id
WHERE oi.variant_id = v.id
  AND oi.category_id IS NULL;
```

- Lines whose variant/product was deleted before this feature stay null. They are
  reported as the `"Khác"` bucket, not dropped.
- The backfill uses the **current** category because no historical category exists for
  those lines; new lines snapshot at sale. This limitation is recorded in `deferred.md`.
- Down: drop the check constraint, then both columns. The snapshot is derived data and
  needs no reverse backfill.

### Order line model

`internal/modules/order/domain/model.OrderLine` gains:

```text
Category *CategorySnapshot   // nil for a legacy line whose variant/category no longer resolves
```

with `CategorySnapshot{ID uuid.UUID; Name string}`. New lines always carry it; the
database permits null only for legacy lines. The line insert and the edit's line
replacement write `category_id`/`category_name` in the same statement as the other
snapshot columns, inside the existing checkout/edit transaction. No new statement and no
new transaction boundary. A zero `CategoryID` from a variant fact (a misconfigured or
test-only provider; production facts always populate it for a visible variant) maps to
no snapshot rather than storing the nil UUID as a real category.

### Product variant facts

`contracts.VariantSummary`, `domain/repository.VariantFacts` and the
`FindFactsByIDs` projection gain:

- `CategoryID uuid.UUID`
- `CategoryName string`

The facts query already joins `products` and `categories` for the visibility predicate;
it projects `p.category_id` and `c.name` from that join. The catalog adapter maps them;
the order checkout/edit copies them onto the line. This is the pre-existing cross-module
join recorded in `deferred.md`; admin does not add a second one.

## Read models (no storage)

### Order KPIs (order module)

`contracts.OrderKPIs`:

| Field | Type | Source |
|---|---|---|
| `CurrentRevenue` | `Money` | `sum(total_amount)` for `PAID`/`SHIPPED`/`COMPLETED`, `VND`, current window |
| `LastRevenue` | `Money` | same, previous-month same-period window |
| `CurrentOrders` | `int64` | `count(*)` created in current window, all states |
| `LastOrders` | `int64` | `count(*)` created in previous window, all states |
| `TotalOrders` | `int64` | `count(*)` all-time, all states |
| `PendingConfirmation` | `int64` | `count(*) WHERE status = 'PENDING'`, all-time |

Answered by one conditional-aggregate query over `orders`. The adapter sets
`Currency: "VND"`; the SQL filters the same currency for revenue sums.

### Daily revenue (order module)

`contracts.DailyRevenue`: `Date string` (`YYYY-MM-DD` in the shop timezone) and
`Amount Money`.

```sql
SELECT to_char(o.created_at AT TIME ZONE $3, 'YYYY-MM-DD') AS day,
       sum(o.total_amount)::bigint AS amount
FROM orders o
WHERE o.status IN ('PAID','SHIPPED','COMPLETED')
  AND o.currency = 'VND'
  AND o.created_at >= $1 AND o.created_at < $2
GROUP BY day
ORDER BY day;
```

The admin application zero-fills the days the query does not return. `$3` is the IANA
shop timezone. The window is half-open `[from, to)`; today's `to` is the request instant.

### Category revenue (order module)

`contracts.CategoryRevenue`: `CategoryID *uuid.UUID`, `CategoryName string`,
`Amount Money`.

```sql
SELECT oi.category_id,
       COALESCE(oi.category_name, '') AS category_name,
       sum(oi.quantity * oi.unit_price_amount)::bigint AS amount
FROM orders o
JOIN order_items oi ON oi.order_id = o.id
WHERE o.status IN ('PAID','SHIPPED','COMPLETED')
  AND o.currency = 'VND'
  AND o.created_at >= $1 AND o.created_at < $2
GROUP BY oi.category_id, oi.category_name
ORDER BY amount DESC, category_name ASC, oi.category_id ASC;
```

Line totals are the same numbers that make up `orders.total_amount`, so the category sum
(including the null-category bucket) reconciles with `CurrentRevenue`. A null
`category_id` is the `"Khác"` bucket; the admin mapper names it, not the SQL. The final
`oi.category_id` tie-break is beyond the spec's amount/name order and exists only to make
the result deterministic when two rows share an amount and a name.

### Recent orders (order module)

`contracts.RecentOrder`:

| Field | Type | Source |
|---|---|---|
| `ID` | `uuid.UUID` | `orders.id` |
| `CustomerID` | `uuid.UUID` | `orders.user_id` |
| `Total` | `Money` | `orders.total_amount` + `currency` |
| `Status` | `string` | `orders.status` |
| `CreatedAt` | `time.Time` | `orders.created_at` |
| `FirstLineName` | `string` | first `order_items.name` by position |
| `FirstLineVariantName` | `string` | first `order_items.variant_name` |
| `FirstLineQuantity` | `int64` | first `order_items.quantity` |
| `LineCount` | `int64` | `count(*)` over the order's lines |
| `TotalQuantity` | `int64` | `sum(quantity)` over the order's lines |

Newest `created_at DESC, id DESC`, `LIMIT 5`. Two `LEFT JOIN LATERAL` subqueries (first
line; counts) keep it one bounded statement. Any order state is included. The admin
application formats `itemSummary` from these fields and resolves customers separately;
it trims the joined parts and avoids a double space when a legacy line has an empty
`variant_name`.

### Customer KPIs and summaries (user module)

`contracts.CustomerKPIs`: `TotalUsers int64` (active `CUSTOMER` all-time),
`NewUsersThisWeek int64` (active `CUSTOMER` created in the current Monday-to-now window).
The user module's `DirectoryRepository.KPIs(week)` answers both in one conditional-
aggregate query over `users`; the service maps the result to the contract DTO.

`contracts.CustomerSummary`: `ID uuid.UUID`, `DisplayName string` (empty when unset),
`Email string`. Bulk read by id:

```sql
SELECT id, COALESCE(display_name, ''), email
FROM users
WHERE id = ANY($1)
ORDER BY id;
```

No role/status filter: a disabled account still labels its old order. Missing ids are
omitted; the admin maps them to `customer: null`. No audit is written per customer; the
dashboard's one audit event covers the read.

### Low-stock variants (inventory module)

`contracts.LowStockVariant`: `VariantID uuid.UUID`, `Available int64`.

```sql
SELECT l.variant_id,
       GREATEST(l.quantity - COALESCE(h.held, 0), 0)::bigint AS available
FROM stock_levels l
LEFT JOIN (
    SELECT variant_id, sum(quantity) AS held
    FROM stock_holds
    WHERE status = 'ACTIVE' AND expires_at > $2
    GROUP BY variant_id
) h ON h.variant_id = l.variant_id
WHERE GREATEST(l.quantity - COALESCE(h.held, 0), 0) < $1
ORDER BY l.variant_id;
```

- `$1` is the threshold, `$2` is the inventory module's injected clock instant.
- A variant with no `stock_levels` row is absent (not counted).
- The query does not filter product visibility; hidden products keep real stock and may
  still need restocking during the hide.
- `DISCONTINUED` is filtered by the product contract, not by inventory.

### Discontinued variant ids (product module)

`contracts.ProductVariantStates.DiscontinuedVariantIDs(ids)`:

```sql
SELECT id
FROM product_variants
WHERE id = ANY($1) AND sell_state = 'DISCONTINUED'
ORDER BY id;
```

Empty input answers an empty slice without a query. The admin removes these ids from the
low-stock candidates before counting.

## Admin read models (no storage)

`internal/modules/admin/domain/model`:

- `Overview` — KPI groups, revenue trend, category breakdown, recent orders.
- `RevenueKPI` — `Current`, `Last` (`Money`), `GrowthPercent *float64`.
- `OrderKPI` — `Total`, `ThisMonth`, `LastMonth`, `PendingConfirmation`, `GrowthPercent
  *float64`.
- `CustomerKPI` — `TotalUsers`, `NewUsersThisWeek`.
- `InventoryKPI` — `LowStockCount`, `Threshold`.
- `RevenuePoint` — `Date string` (`YYYY-MM-DD`), `Amount Money`.
- `CategoryShare` — `CategoryID *uuid.UUID`, `CategoryName string`, `Amount Money`,
  `Percentage float64`.
- `RecentOrder` — `ID`, `Customer *CustomerSummary`, `ItemSummary`, `ItemCount`,
  `ItemQuantity`, `Total`, `Status`, `CreatedAt`.
- `Money` — `Amount int64`, `Currency string`.
- Window helpers: `CurrentMonth`, `PreviousMonthSamePeriod`, `CurrentWeek`,
  `TrailingDays` (pure functions over `time.Time`/`*time.Location`).

The admin domain imports only stdlib and `github.com/google/uuid`, per Constitution I.

## Validation rules (from the spec)

- `days` is 7 or 30; default 7. Any other value is a 400 `VALIDATION_ERROR` naming
  `days`.
- `lowStockThreshold` is 0..10000; default 5. Any other value is a 400
  `VALIDATION_ERROR` naming `lowStockThreshold`.
- Revenue statuses are exactly `PAID`, `SHIPPED`, `COMPLETED`; currency is `VND`.
- Growth is null when the previous value is zero; otherwise rounded to 2 decimals.
- Percentage is rounded to 1 decimal; the list is only categories with amount > 0; the
  sum may differ from 100 by rounding.
- Trend has exactly `days` entries, oldest first, zero-filled, with today's bucket ending
  at the request instant.
- Recent orders are at most 5, newest first; `itemCount` is the number of lines and
  `itemQuantity` the sum of quantities.
- Every money value is an integer amount plus currency; no float is stored or returned
  as money.
- The low-stock count excludes variants with no stock row and `DISCONTINUED` variants;
  active holds count only while `ACTIVE` and unexpired.

## What does not change

- No order/payment/inventory state machine change.
- No new table, no materialised view, no cache.
- No change to the order total calculation; the line totals already sum to the order
  total.
- No change to the existing customer lookup or its per-customer audit.
- No change to the existing product visibility predicate; the new category fields come
  from the join that predicate already uses.
