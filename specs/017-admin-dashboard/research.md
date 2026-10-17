# Research — Admin Dashboard Overview (017)

Phase 0 decisions. Each records what was chosen, why, and the alternatives rejected.
The spec's `Clarifications` (category snapshot, MTD comparison, money objects, low-stock
scope, fixed recent limit, one audit event) are inputs here, not open questions.

## D1 — A new read-only admin module orchestrates published read contracts

**Decision**: add `internal/modules/admin` with the four layers. It owns no tables and
no repository interfaces. Its application layer depends on narrow read contracts in
`internal/contracts/dashboard.go`; the owning modules supply adapters at the composition
root. `domain/repository` and `domain/error` contain a package document only, because the
module owns no storage and defines no business error.

**Rationale**: module 16 Admin is the operator's workspace and has no data of its own
(`docs/modules/16-admin.md`). Constitution I forbids one module reading another's tables
directly. A contract-based read model keeps the boundary and makes the dashboard's
dependencies explicit. The `domain/repository` exception is recorded in Complexity
Tracking because the constitution's repository rule assumes a module owns data.

**Alternatives rejected**: adding a dashboard SQL query over every module's tables
(Constitution I violation); putting the endpoint inside order/payment (wrong ownership,
and it would still need customer/inventory/category data); giving admin its own
materialised tables (duplicated source of truth, stale data, out of MVP scope).

## D2 — Category is snapshotted on each order line

**Decision**: migration `00017_order_item_category_snapshot.sql` adds nullable
`order_items.category_id uuid` and `order_items.category_name text`, with a check that
both are null or both non-null. There is no foreign key. Checkout and every line
replacement write the current category from the product-variant facts. Existing lines
are backfilled from the current product/category where the variant still exists; lines
whose variant/product was deleted stay null and are reported as "Khác".

**Rationale**: `order_items` is already a snapshot of what was sold (name, code, variant,
SKU, price); category is the missing dimension. A live join would move historical revenue
when a product changes category and would drop revenue when a product is deleted, making
the dashboard's category total disagree with its revenue KPI. Nulls are required because
a deleted product cannot be resolved retrospectively; the two-field check keeps a
half-snapshot out of storage.

**Alternatives rejected**: live join `order_items → product_variants → products →
categories` (historical drift and lost revenue); storing only `category_id` (a later
rename changes historical labels); backfilling with a placeholder category (invents a
sale that never happened).

## D3 — Revenue is recognised from completed order states at order creation time

**Decision**: revenue includes orders in `PAID`, `SHIPPED` or `COMPLETED`, currency
`VND`, and is attributed by `orders.created_at`. `PENDING`, `PAYMENT_PENDING` and
`CANCELLED` are excluded.

**Rationale**: those three are the states in which the shop has actually earned the sale;
an unpaid order is not revenue and a cancelled one never was. The order table has no
`paid_at`, and module 08 owns payment events, so `created_at` is the only timestamp the
dashboard can use without reading another module's tables. The difference is small for a
shop whose payment window is 60 minutes and is recorded as a deferred improvement.

**Alternatives rejected**: `PAYMENT_PENDING` (money not received); `updated_at` (a later
edit/transfer would move revenue between periods); payment-event time (correct but
requires a payment read contract and a wider feature; deferred).

## D4 — Time windows use the shop timezone, MTD comparison and trailing days

**Decision**: add `SHOP_TIMEZONE` to configuration, default `Asia/Ho_Chi_Minh`, validated
with `time.LoadLocation` for the API. All calendar boundaries and labels use it; storage
stays UTC.

- Current month: `[00:00 ngày 1 tháng hiện tại, now)`.
- Previous month: `[00:00 ngày 1 tháng trước, min(first-day-of-previous-month + elapsed,
  first-day-of-current-month))`, where `elapsed = now - current month start`.
- Week: `[00:00 Thứ Hai tuần hiện tại, now)`.
- Trend: the last `days` calendar days ending today, inclusive. Today's bucket ends at
  `now`; earlier buckets are full days.
- `dayLabel`: `T2`..`T7`, `CN` for 7 days; `DD/MM` for 30 days.

**Rationale**: a dashboard opened on the 17th must not compare a 17-day month with a full
30-day month; MTD-versus-same-period is the standard fair comparison. The shop operates in
Vietnam while the database stores UTC, so using the server/Docker timezone (UTC) would
shift late-evening orders into the wrong day/month. `Asia/Ho_Chi_Minh` has no DST, but the
implementation uses a real location for any configured zone. `time.LoadLocation` works in
the runtime image because the Dockerfile installs `tzdata`.

**Alternatives rejected**: UTC boundaries (wrong local day/month); full previous month
(unfair early in the month); rolling 7×24 hours for "this week" (does not match the
meaning of a week); hard-coded timezone constant (cannot be corrected by deployment
configuration).

## D5 — Money is an integer minor-unit amount plus currency at every layer

**Decision**: add `contracts.Money{Amount int64; Currency string}` and an admin
`model.Money` value object. Every monetary response field, including trend points and
category shares, is `{amount, currency}`. Revenue SQL filters `currency = 'VND'` and the
adapter returns `VND`.

**Rationale**: Constitution II forbids floats for money; the existing order DTO already
uses `{amount, currency}`. A payload with bare integers for chart series would create two
money shapes in one response, so the spec requires one shape everywhere. MVP is VND-only,
so a `VND` filter keeps a future multi-currency order from silently corrupting a sum.

**Alternatives rejected**: bare integers for chart series (two shapes, a client can
misread minor units); float/decimal in JSON (forbidden); summing mixed currencies
(mathematically wrong).

**On the `contracts.ProductPrice` shape**: `ProductPrice` is a product-contract type
for an effective price and belongs to the product module's vocabulary. The dashboard's
money crosses the order read contract (the customer and inventory contracts carry no
money), so reusing a product-named type would misname the concept and couple that
consumer to product. `Money` is a neutral contract value type; the duplicated shape is
deliberate, and the two types have different owners and meanings.

## D6 — Read contracts are coarse-grained per dashboard section

**Decision**: `contracts/dashboard.go` defines `TimeWindow`, `Money`,
`OrderStatistics` (`KPIs`, `DailyRevenue`, `CategoryRevenue`, `RecentOrders`),
`CustomerStatistics` (`KPIs`, `CustomerSummaries`), `InventoryStatistics`
(`LowStockVariants`) and `ProductVariantStates` (`DiscontinuedVariantIDs`). Each adapter
answers its section in one bounded query where possible; the admin application composes
the sections.

**Rationale**: a per-metric contract would make one dashboard request perform more than a
dozen round trips. The dashboard is a read model; a section-shaped contract is the right
granularity, keeps each query owned by the module that owns the data, and still lets the
admin application compute growth, percentages and zero-fill.

**Alternatives rejected**: importing each module's repository types (breaks contract
purity); a single `DashboardRepository` owned by admin (would read every table); per-metric
calls (unnecessary round trips and more fake methods).

## D7 — Order statistics are read from order-owned tables

**Decision**: the order module supplies a `statistics` adapter over new repository reads:

- `KPIs(current, last)` in one conditional-aggregate query returns current/last revenue,
  current/last order counts, all-time order count and all-time PENDING count.
- `DailyRevenue(window, timeZone)` groups completed revenue by local date with
  `to_char(created_at AT TIME ZONE $tz, 'YYYY-MM-DD')`.
- `CategoryRevenue(window)` groups line totals by the snapshot category pair.
- `RecentOrders(limit)` reads the newest orders with a lateral first-line projection and
  a lateral count/quantity projection.

**Rationale**: only module 07 may read `orders`/`order_items`. One conditional-aggregate
query answers all order/revenue KPIs and avoids a fan-out of count queries. Category
totals use line totals, which are the same numbers that make up `orders.total_amount`, so
the category breakdown reconciles with the revenue KPI. The lateral reads are bounded by
`LIMIT 5` and the per-order line index.

**Alternatives rejected**: computing KPIs in Go after loading rows (unbounded); separate
queries per metric (more round trips); using `orders.updated_at` for revenue (moves
historical revenue); joining product/category live (D2).

## D8 — Customer data is read by the user module without a per-customer audit

**Decision**: the user module (02) implements `contracts.CustomerStatistics` directly,
as it already implements `CustomerLookupService`. A new narrow repository over the
`users` table answers:
- `KPIs(week)` in one query: active `CUSTOMER` count and active `CUSTOMER` created in the
  week;
- `CustomerSummaries(ids)`: id, display name and email for the requested ids, regardless
  of current status (so a disabled account still labels its old order).

No per-customer audit is written. The dashboard's single `ADMIN_DASHBOARD_VIEWED` event
covers the read; it carries no PII.

**Rationale**: the user module already reads the auth-owned `email` and `role` columns
for its read-only contract. It is the natural provider of the customer read model, and
it already owns the display-name column. Reusing `LookupCustomer` would perform one
audited lookup per recent order and fill the audit trail with five PII reads on every
dashboard refresh. The dashboard's own audit gives the operator traceability without that
noise.

**Alternatives rejected**: auth module as provider (splits the existing customer read
contract across two modules and still reads `display_name`); per-order
`LookupCustomer` (N+1 plus audit spam); no audit at all (inconsistent with the
privacy-sensitive read).

## D9 — Low stock is computed by inventory and filtered by product state

**Decision**: inventory supplies `LowStockVariants(threshold)` over its own tables using
its injected clock; the query returns variants with a stock row and `available <
threshold`, where `available = max(0, quantity - sum(active, unexpired holds))`. The
product module supplies `DiscontinuedVariantIDs(ids)`, and the admin application removes
those ids before counting. Variants with no stock row are not candidates; hidden products
are still counted because their stock is real and may need restocking.

**Rationale**: only inventory may read `stock_levels`/`stock_holds`, and only product may
decide `DISCONTINUED`. Splitting the two facts keeps each module's ownership and avoids a
cross-module join. Filtering in the admin application is bounded by the low-stock set,
which is expected to be small; the alternative is a product read for every stock row.

**Alternatives rejected**: inventory joining `product_variants` (Constitution I
violation); admin joining both tables (the same violation in a new place); returning every
variant and filtering in Go (unbounded); counting `DISCONTINUED` (the spec excludes it).

## D10 — Recent orders are fixed to five, with a bulk customer read

**Decision**: `RecentOrders(5)` returns id, customer id, total, status, created_at, the
first line's product/variant name and quantity, line count and total quantity. The admin
application collects the customer ids and calls `CustomerSummaries` once. A missing
account yields `customer: null`. `itemSummary` is assembled in the admin mapper from the
first line: `<name> <variantName> (x<quantity>)`, plus ` +N` when there are N more lines.
`itemCount` = line count; `itemQuantity` = sum of quantities.

**Rationale**: five rows fit the dashboard card and keep the customer lookup bounded.
`itemCount` keeps the project-wide meaning (number of lines, already used by
`OrderSummary`); `itemQuantity` is the number the FE's `(x2)` sample implies. The
summary is presentation formatting, so it lives in the admin mapper, not in the order
query.

**Alternatives rejected**: a `limit` query param (the spec fixed it; extra API surface);
one `LookupCustomer` per order (N+1 and audit noise); returning the whole first line
(over-fetching); making `itemCount` mean units (breaks the existing contract).

## D11 — One audit event per successful dashboard view, no PII

**Decision**: emit one `ADMIN_DASHBOARD_VIEWED` event with outcome `SUCCESS`, actor from
the session, target type `dashboard`, target id `overview`, and metadata
`{days, lowStockThreshold, recentOrderCount}`. The admin module has its own auditor
adapter over the shared audit emitter. The event never contains customer names, emails
or order contents.

**Rationale**: the dashboard exposes aggregate revenue and recent customer contact data.
The user module's per-customer read already treats contact data as privacy-sensitive, but
auditing every recent order would produce five rows per refresh. One event records the
admin's access without PII or noise. The shared writer keeps the existing retry/drop
semantics (Constitution VI).

**Alternatives rejected**: no audit (weakens the privacy trace); per-customer audit
(noise, unnecessary PII-adjacent volume); audit with customer ids/emails (PII in the
trail, which the project avoids).

## D12 — The dashboard is a read-only snapshot with no transaction or cache

**Decision**: the service performs bounded reads sequentially and assembles one response.
It opens no transaction and writes no cache. All sections use the same shop timezone and
the same status definitions. A data change between two reads may make the response a
"nearly consistent" snapshot; this is acceptable for an operational dashboard.

**Rationale**: the endpoint has no writes, so Constitution II's transaction rule does not
apply. A repeatable-read transaction spanning modules would need a cross-module transaction
boundary the architecture does not have. Caching would hide the freshness the operator
expects; pre-aggregation is explicitly out of scope (spec). Sequential reads are simple
and bounded; if performance demands, parallel reads or a cache can be a later feature.

**Alternatives rejected**: a cross-module read transaction (no shared UnitOfWork across
providers); cache (stale dashboard); pre-aggregated tables (new infrastructure, YAGNI).

**Verification of SC-006/FR-032**: task T066 asserts the 8 contract-method calls across
the four read interfaces are fixed regardless of fixture size, and runs a DB-backed
integration case with 10,000 orders and 1,000 variants against the 2-second target.

## D13 — Product variant facts gain category fields through the existing read

**Decision**: add `CategoryID uuid.UUID` and `CategoryName string` to
`contracts.VariantSummary`, `domain/repository.VariantFacts` and the existing
`FindFactsByIDs` projection (`p.category_id`, `c.name`). The catalog adapter maps them.
The order checkout/edit copies them onto the order line.

**Rationale**: the variant-facts read already joins `products` and `categories` to apply
the visibility predicate, so adding two projected columns is the smallest change and keeps
one read for the cart/order. This is a pre-existing cross-module join in the product
module; the feature does not copy it into admin and records it in `deferred.md` rather
than widening the debt.

**Alternatives rejected**: a second `CategoryQuery` call per checkout (an extra round trip
and a second source for the same fact); duplicating category in the cart line (the cart
does not need it); a new order-side lookup after checkout (the order would need a second
transaction/read).

## D14 — Contracts, docs and generated API stay in the same change

**Decision**: add the endpoint to `docs/api-reference.md`; write
`specs/017-admin-dashboard/contracts/openapi.yaml` and
`specs/017-admin-dashboard/contracts/error-codes.md`; add the feature's
`frontend-guide.md`; annotate the handler and run `make swagger`; run
`make swagger-check` as part of the gate.

**Rationale**: Constitution VIII and the governance rule make the API reference, the
machine-readable contract and the generated Swagger part of the definition of done. The
frontend currently uses mock data and must be told about the money-object and field
changes.

**Alternatives rejected**: updating Swagger only (the authoritative reference would
drift); a hand-written contract only (the generated spec would drift); deferring the
frontend guide (the FE would integrate against the wrong shape).
