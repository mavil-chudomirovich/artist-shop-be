# Quickstart — Admin Dashboard Overview (017)

Validation guide proving the feature end to end. It does not restate the contract (see
[contracts/openapi.yaml](./contracts/openapi.yaml) and
[contracts/error-codes.md](./contracts/error-codes.md)) or the schema (see
[data-model.md](./data-model.md)); it says how to run and what to expect.

## Prerequisites

- Local stack up (see `docs/getting-started.md` / `docs/docker.md`).
- Migrations applied: `make migrate-up` — this includes
  `migrations/00017_order_item_category_snapshot.sql`.
- API running: `make run` (or `go run ./cmd/api`).
- `SHOP_TIMEZONE` unset (defaults to `Asia/Ho_Chi_Minh`) or set explicitly.
- An ADMIN access token and a CUSTOMER access token.
- Seed data with:
  - orders in PENDING, PAYMENT_PENDING, PAID, SHIPPED, COMPLETED and CANCELLED;
  - a product/variant in a category, and at least one order line sold before a category
    change;
  - active customers, one pending and one disabled account;
  - a variant with a stock record and an active hold, a `DISCONTINUED` variant and a
    variant with no stock record.

Set `BASE=http://localhost:8080/api/v1`, `ADMIN=...`, `CUSTOMER=...`.

## Automated gates

| Command | Proves |
|---|---|
| `make lint` | format, tidy, vet, golangci-lint |
| `make test` | window/growth/rounding/zero-fill, mapper, handler authorization and validation |
| `make test-integration` | the order/customer/inventory/product reads and the migration against PostgreSQL (needs Docker) |
| T066 performance case (10k orders / 1k variants) | the SC-006 2-second target; the DB-backed admin integration case records the measured duration |
| `make swagger` | regenerates `docs/swagger/` from the handler annotations |
| `make swagger-check` | the generated spec matches the annotations |
| `make check` | the close-out gate before push |

## Scenario A — default dashboard (spec US1)

```bash
curl -s -H "Authorization: Bearer $ADMIN" "$BASE/admin/dashboard/overview"
```

- `200`; the envelope has `data` and `meta`.
- `data.kpis` has all four groups.
- `data.revenueTrend` has exactly 7 entries, oldest first, each with `date`, `dayLabel`
  (`T2`..`CN`), and a `{amount, currency}` object.
- `data.categoryBreakdown` is sorted by amount descending and each `amount` is an object.
- `data.recentOrders` has at most 5 entries, newest first.

## Scenario B — 30-day trend (spec US1)

```bash
curl -s -H "Authorization: Bearer $ADMIN" \
  "$BASE/admin/dashboard/overview?days=30&lowStockThreshold=3"
```

- `revenueTrend` has exactly 30 entries.
- `dayLabel` is `DD/MM`.
- `kpis.inventory.threshold` is `3`.
- The month KPIs are unchanged by `days`.

## Scenario C — access control (spec US6)

| Request | Expected |
|---|---|
| No `Authorization` header | `401 UNAUTHENTICATED` |
| `Authorization: Bearer $CUSTOMER` | `403 FORBIDDEN`; the foundation records `AUTH_PRIVILEGE_DENIED` |

## Scenario D — validation (spec US6)

| Request | Expected |
|---|---|
| `?days=14` | `400 VALIDATION_ERROR`, `details[].field = "days"` |
| `?days=abc` | `400 VALIDATION_ERROR`, `details[].field = "days"` |
| `?lowStockThreshold=-1` | `400 VALIDATION_ERROR`, `details[].field = "lowStockThreshold"` |
| `?lowStockThreshold=10001` | `400 VALIDATION_ERROR`, `details[].field = "lowStockThreshold"` |
| `?lowStockThreshold=0` | `200`; the threshold is echoed as `0` |

## Scenario E — revenue statuses and fair month windows (spec US2)

1. Put one order in each of the six states in the current month, with known totals.
2. Call the endpoint.
3. Expected: `currentMonthAmount` is the sum of only PAID, SHIPPED and COMPLETED; the
   other three states contribute nothing.
4. Compare `lastMonthAmount` with the same elapsed period (not the whole previous month).
   If the previous period had no revenue, `growthPercent` is `null`.
5. Place an order at 23:30 UTC on the last day of a month and confirm it is counted in
   the next local day/month according to `Asia/Ho_Chi_Minh`.

A direct status update in a disposable local database is acceptable for this read-only
check; do not use it against a shared environment.

## Scenario F — category snapshot (spec US4)

1. Sell a variant in category A; confirm the line appears in A.
2. Change the product's category to B in the admin product screen.
3. Call the endpoint; the old line's revenue stays in A, not B.
4. Delete a product that has a sold line (or simulate a legacy line with null category),
   then call the endpoint; its revenue remains in the total and appears under
   `categoryId: null`, `categoryName: "Khác"`.
5. Confirm the sum of `categoryBreakdown` amounts equals `kpis.revenue.currentMonthAmount`.

## Scenario G — low stock (spec US3)

1. Seed a variant with `stock_levels.quantity = 5` and an active hold of 2 for a
   PAYMENT_PENDING order; call with `lowStockThreshold=5` — available is 3, so it is
   counted.
2. Expire the hold (or move its `expires_at` into the past) — available becomes 5, so it
   is no longer below the threshold.
3. Mark another variant `DISCONTINUED` with low stock — it is not counted.
4. Leave a variant with no stock record — it is not counted.
5. Confirm `kpis.inventory.lowStockCount` matches the expected set and
   `kpis.inventory.threshold` echoes the request.

## Scenario H — recent orders (spec US5)

1. Create more than 5 orders, including one for an account that is then deleted.
2. Call the endpoint.
3. Expected: only the 5 newest, newest first; the deleted account's order has
   `customer: null`; the other customer objects have `id`, `displayName`, `email`.
4. For a multi-line order, `itemSummary` is the first line's product/variant plus
   `(x<quantity>)` and ` +N` for the remaining lines; `itemCount` is the line count and
   `itemQuantity` is the sum of quantities.
5. `status` is one of the six raw BE values; no translation is done by the API.

## Scenario I — audit (spec FR-030, SC-009)

After a successful call, allow the shared async audit writer a moment to drain, then
query the audit trail in a disposable environment:

```sql
SELECT action, actor_id, target_type, target_id, outcome, metadata
FROM audit_logs
WHERE action = 'ADMIN_DASHBOARD_VIEWED'
ORDER BY occurred_at DESC
LIMIT 5;
```

- Exactly one row per successful request.
- Metadata has `days`, `lowStockThreshold`, `recentOrderCount`.
- No customer name, email, order line or other PII is in the row.
- A 403 from a CUSTOMER session records `AUTH_PRIVILEGE_DENIED`, not this action.

## Scenario J — migration and rollback

- `make migrate-up` applies `00017` and the backfill; existing order lines whose
  variant/product exists gain the category snapshot.
- `make migrate-down` drops the check and the two columns; `make migrate-up` re-applies
  them.
- The migration's integration test proves the backfill, the both-or-none constraint and
  the rollback.

## What is deliberately not verified here

| Not verified | Why | Where it belongs |
|---|---|---|
| A real PayOS payment that produces a PAID order | Needs live provider credentials and a public webhook | Existing payment quickstart; use a direct status update for this dashboard check |
| A production-scale data set beyond the spec's 10k-order/1k-variant volume | `T066` covers the SC-006 fixture; larger volumes are not measured | A later load test if the shop outgrows the MVP volume |
| Cache/pre-aggregation behaviour | Explicitly out of scope | A later feature if measured need appears |
| Multi-currency aggregation | MVP is VND-only | A later currency feature |
