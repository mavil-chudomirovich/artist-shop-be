# Implementation Plan: Admin Dashboard Overview

**Branch**: `017-admin-dashboard` | **Date**: 2026-10-17 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/017-admin-dashboard/spec.md`

## Summary

Add one ADMIN-only endpoint, `GET /api/v1/admin/dashboard/overview`, that returns the
operator's whole dashboard in one response: revenue/order/customer/inventory KPIs, a
7- or 30-day revenue series, a current-month category breakdown and the five newest
orders. The endpoint is a read-only orchestrator in a new `internal/modules/admin`
module; it owns no data and reads the owning modules only through new contracts in
`internal/contracts/dashboard.go`.

The one schema change is a category snapshot on each order line
(`migrations/00017_order_item_category_snapshot.sql`), so historical category revenue
does not move when a product changes category or is deleted. The order module supplies
the revenue/order reads, the user module supplies customer counts and summaries, the
inventory module supplies the low-stock candidates and the product module identifies
`DISCONTINUED` variants. The shop timezone is configurable (`SHOP_TIMEZONE`, default
`Asia/Ho_Chi_Minh`). Money is always an integer minor-unit amount plus currency.

## Technical Context

**Language/Version**: Go 1.26 (the repository toolchain; no version decision needed)

**Primary Dependencies**: existing module layout only — `go-chi/chi/v5`, `pgx/v5`,
`log/slog`, `github.com/google/uuid`; no new library and no new service

**Storage**: PostgreSQL; one additive migration
(`migrations/00017_order_item_category_snapshot.sql`); Down reverses it; no new table,
view or materialised read model

**Testing**: `go test` unit suites per module, `make test-integration` against
PostgreSQL (build tag `integration`, needs Docker), plus HTTP handler tests

**Target Platform**: Linux server (VPS) via Docker Compose; the runtime image already
installs `tzdata`, so `time.LoadLocation` works

**Project Type**: web-service — modular monolith, REST under `/api/v1`, shared response
envelope and error catalogue

**Performance Goals**: the dashboard uses a small, bounded set of reads (one order KPI
query, one trend query, one category query, one recent-order query, one customer KPI
query, one customer-summary query, one low-stock query, one discontinued-id query); no
read grows with the number of orders, variants or customers (spec SC-006, FR-032); target
under 2 seconds at 10,000 orders / 1,000 variants

**Constraints**: `domain` imports only stdlib + `share/access` + `github.com/google/uuid`;
this feature adds no new cross-module table read (the pre-existing product→categories
join is recorded as debt, not copied into admin); money is integer minor units +
currency; storage timestamps stay UTC; all calendar boundaries use the shop timezone; the
endpoint is read-only and opens no transaction; the admin module owns no persistent data

**Scale/Scope**: one new module, one new endpoint, one additive migration, four new
read contracts plus one field pair on the existing variant-facts contract, one
configuration value, an API-reference update, a generated Swagger update and a frontend
guide; no change to the order/payment/inventory state machines

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- [x] **Modular Monolith & Clean Architecture**: the new `admin` module is four layers;
      it owns no data and its application depends on `internal/contracts/dashboard.go`
      only. The owning modules supply adapters at the composition root. `domain` imports
      only stdlib + `github.com/google/uuid`. The admin module's `domain/repository` and
      `domain/error` are package documents because it has no storage or business error;
      this is the one new exception introduced by this feature. The pre-existing
      product→`categories` join is recorded as a separate debt row in Complexity Tracking.
- [x] **Transactional Integrity**: the dashboard is read-only and opens no transaction.
      The only write this feature touches is the category snapshot inside the existing
      checkout/edit insert, which already runs in the order use case's transaction. No
      money or stock rule changes.
- [x] **State Machines & Invariants**: no state machine changes. The endpoint reads the
      six order states and the existing inventory/order invariants; no transition is
      added or bypassed.
- [x] **Test-First Critical Logic**: window calculations (month/week/trailing days),
      growth nullable/rounding, percentage rounding, zero-fill, category "Khác" bucket,
      discontinued filtering, item-summary formatting and customer-null mapping are
      written test-first. The order state machine tests are untouched.
- [x] **Security & Least Privilege**: the route is behind `RequireRole(...,
      access.RoleAdmin)`; guest 401, customer 403. The response exposes only the recent
      order's customer id, display name and email, and the audit event carries no PII.
      No secret or configuration value is committed.
- [x] **Observability**: one `ADMIN_DASHBOARD_VIEWED` audit event per successful view
      with `days`, `lowStockThreshold` and `recentOrderCount`, through the existing
      shared writer; structured logs keep the correlation id; no PII in logs or audit.
- [x] **Simplicity (YAGNI)**: no cache, queue, materialised view, microservice or new
      dependency; a handful of bounded reads and pure in-memory assembly. No
      unjustified complexity beyond the documented admin-module exception.
- [x] **Frontend Integration Guide**: the new endpoint and changed money/field shapes
      ship `specs/017-admin-dashboard/frontend-guide.md` (new endpoint; every money field
      is an object; orders fields; nullable growth/customer; itemCount/itemQuantity;
      six-status mapping).
- [x] **Swagger annotations**: the new handler carries `@Summary`, `@Tags`, `@Param`,
      `@Success`, `@Failure` and `@Router`, and `docs/swagger/` is regenerated with
      `make swagger`; `make swagger-check` must pass.

**Re-check after Phase 1 design**: the design below keeps every box checked. The new
exception is the read-only admin module's empty repository/error packages, recorded in
Complexity Tracking with its rationale; the pre-existing product→`categories` join is
also recorded there as inherited debt.

## Project Structure

### Documentation (this feature)

```text
specs/017-admin-dashboard/
├── plan.md              # This file
├── spec.md              # Feature specification
├── research.md          # Phase 0 decisions
├── data-model.md        # Phase 1 data model and reads
├── quickstart.md        # Phase 1 validation guide
├── contracts/
│   ├── openapi.yaml     # Machine-readable endpoint contract
│   └── error-codes.md   # Reused codes and audit action
├── frontend-guide.md    # Close-out hand-off (created by tasks)
├── deferred.md          # Out-of-scope findings (created by tasks)
└── tasks.md             # Phase 2 output (/speckit.tasks — not created here)
```

### Source Code (repository root)

```text
internal/contracts/
├── dashboard.go              # NEW: Money, TimeWindow, OrderStatistics,
│                             #      CustomerStatistics, InventoryStatistics,
│                             #      ProductVariantStates + read DTOs
└── product.go                # VariantSummary + CategoryID/CategoryName

internal/modules/admin/       # NEW read-only module
├── domain/
│   ├── model/                # Overview/KPI/trend/category/recent read models,
│   │                         #   Money, window helpers, growth/percentage helpers
│   ├── constant/             # defaults, allowed days, threshold bounds,
│   │                         #   recent limit, ADMIN_DASHBOARD_VIEWED action
│   ├── error/doc.go          # no business error; validation is request-level
│   └── repository/doc.go     # no storage; reads are contract ports
├── application/
│   ├── interface/            # contract aliases, Clock, Auditor, Config/Actor
│   ├── dto/                  # OverviewInput/Output and section DTOs
│   ├── mapper/               # domain → application DTO, itemSummary, day labels
│   └── implement/            # Overview orchestration, growth/percentage, zero-fill
├── infrastructure/
│   └── implement/auditor/    # adapter over the shared audit emitter
└── presentation/
    ├── dto/                  # HTTP response/query DTOs
    └── http/                 # handler + router + Swagger annotations

internal/modules/order/
├── domain/model/order.go                    # + category snapshot on OrderLine
├── domain/model/dashboard.go                # NEW KPI/trend/category/recent read models
├── domain/repository/order.go               # + KPI/trend/category/recent reads
├── application/implement/checkout.go        # + copy category onto the new line
├── application/implement/edit.go            # + copy category onto replaced lines
├── infrastructure/implement/postgres/order.go # + category insert/scan + four reads
└── infrastructure/implement/statistics/statistics.go # NEW contract adapter

internal/modules/product/
├── domain/repository/variant.go             # VariantFacts + category + DiscontinuedIDs
├── infrastructure/implement/postgres/variant.go # projection + discontinued ids
├── infrastructure/implement/catalog/catalog.go  # map category fields
└── infrastructure/implement/variantstate/variantstate.go # NEW contract adapter

internal/modules/user/
├── domain/model/directory.go                # NEW CustomerKPIs/CustomerSummary read models
├── domain/repository/directory.go           # NEW narrow customer read port
├── infrastructure/implement/postgres/directory.go # NEW counts/summaries queries
├── application/implement/profile.go         # + Directory dependency on Service
└── application/implement/customer_stats.go  # NEW CustomerStatistics methods

internal/modules/inventory/
├── domain/model/stock.go                    # + LowStockVariant read model
├── domain/repository/inventory.go           # + LowStock read
├── infrastructure/implement/postgres/inventory.go # + low-stock query
└── infrastructure/implement/statistics/statistics.go # NEW contract adapter

internal/share/config/
├── config.go                                # + SHOP_TIMEZONE, validated for the API
└── config_test.go                           # + default/valid/invalid timezone tests

cmd/api/main.go                              # + admin module wiring and mount

migrations/
├── 00017_order_item_category_snapshot.sql   # + category columns/backfill/check
└── admin_dashboard_guard_test.go          # + up/down assertions for 00017

docs/
├── api-reference.md                         # + endpoint, params, response, errors
├── configuration.md                         # + SHOP_TIMEZONE row/default/validation
├── modules.md                               # + module 16 status/roadmap
├── modules/16-admin.md                      # + Spec Kit status update
├── decisions/022-admin-dashboard-read-model.md # NEW ADR + README row
└── swagger/                                 # regenerated by make swagger

.env.example                                 # + SHOP_TIMEZONE (English comment)
```

**Structure Decision**: keep the single-project modular monolith. The new capability is
a fourth-layer module under `internal/modules/admin`; all data reads stay in the module
that owns the table behind `internal/contracts/dashboard.go`. No new top-level directory
and no second service.

## Design Decisions (Phase 0/1)

- **Admin is an orchestrator, not a data owner**: the application takes contract ports,
  computes growth/percentages/zero-fill and assembles the response. It never opens a
  transaction because it never writes. It reads the injected clock **once** per request
  and uses that same instant for every window, the trend's "today", and the audit, so the
  sections cannot disagree about "now" within one response.
- **Coarse-grained read contracts**: `OrderStatistics.KPIs` answers all order/revenue
  KPIs in one conditional-aggregate query; `CustomerStatistics.KPIs` answers both
  customer counts in one query. The other sections are one query each. This keeps the
  dashboard bounded and avoids a per-metric contract explosion.
- **Category snapshot**: nullable `category_id`/`category_name` on `order_items`, both
  or neither, no FK; backfill from the current product/category where resolvable;
  checkout/edit writes the current pair; unresolved lines become `"Khác"`.
- **Shop timezone**: `SHOP_TIMEZONE` default `Asia/Ho_Chi_Minh`; the API validates it at
  startup; windows are computed with `time.Date` in that location; storage is UTC; the
  trend query buckets with `to_char(created_at AT TIME ZONE $tz, 'YYYY-MM-DD')`.
- **Revenue**: `PAID`/`SHIPPED`/`COMPLETED`, `VND`, attributed by `created_at`; current
  month is MTD, previous month is the same elapsed period clamped to that month's end.
- **Low stock**: inventory computes `max(0, quantity - active unexpired holds)` over
  variants with a stock record; product answers the `DISCONTINUED` subset; admin removes
  that subset before counting.
- **Recent orders**: fixed limit 5; structural first-line/count fields in the contract;
  `itemSummary` assembled in the admin mapper; customer ids resolved in one bulk read;
  a missing account yields `customer: null`.
- **Audit**: one `ADMIN_DASHBOARD_VIEWED` event, no PII, through the shared emitter.
- **Errors**: shared `VALIDATION_ERROR`/`UNAUTHENTICATED`/`FORBIDDEN`/`RATE_LIMITED`/
  `INTERNAL_ERROR` only; no new code. Query validation names `days` or
  `lowStockThreshold` in `details[].field`.
- **Money is a neutral contract type**: `contracts.Money` is used by the order read
  contract; the customer and inventory contracts carry no money. `contracts.ProductPrice`
  stays product-specific (research D5); the duplicated shape is deliberate.
- **US1's month window is a placeholder**: the first-cut current/previous calendar
  window in T027 is replaced by US2's MTD/clamp helper (T037) before close-out; US1 is
  a demoable MVP, not the final revenue definition.
- **SC-006/FR-032 are verified**: task T066 asserts the fixed contract-call count and
  runs a 10k-order/1k-variant performance case, recording the measured result against
  the 2-second target.

## Go Read Contracts (`internal/contracts/dashboard.go`)

The exact surface the admin application depends on. DTOs are pure data; no module
internals cross the boundary.

```go
type TimeWindow struct {
    From time.Time // inclusive
    To   time.Time // exclusive
}

type Money struct {
    Amount   int64
    Currency string
}

type OrderKPIs struct {
    CurrentRevenue       Money
    LastRevenue          Money
    CurrentOrders        int64
    LastOrders           int64
    TotalOrders          int64
    PendingConfirmation  int64
}

type DailyRevenue struct {
    Date   string // YYYY-MM-DD in the shop timezone
    Amount Money
}

type CategoryRevenue struct {
    CategoryID   *uuid.UUID // nil = unresolved/legacy
    CategoryName string
    Amount       Money
}

type RecentOrder struct {
    ID                   uuid.UUID
    CustomerID           uuid.UUID
    Total                Money
    Status               string
    CreatedAt            time.Time
    FirstLineName        string
    FirstLineVariantName string
    FirstLineQuantity    int64
    LineCount            int64
    TotalQuantity        int64
}

type OrderStatistics interface {
    KPIs(ctx context.Context, current, last TimeWindow) (OrderKPIs, error)
    DailyRevenue(ctx context.Context, window TimeWindow, timeZone string) ([]DailyRevenue, error)
    CategoryRevenue(ctx context.Context, window TimeWindow) ([]CategoryRevenue, error)
    RecentOrders(ctx context.Context, limit int) ([]RecentOrder, error)
}

type CustomerKPIs struct {
    TotalUsers       int64
    NewUsersThisWeek int64
}

type CustomerSummary struct {
    ID          uuid.UUID
    DisplayName string
    Email       string
}

type CustomerStatistics interface {
    KPIs(ctx context.Context, week TimeWindow) (CustomerKPIs, error)
    CustomerSummaries(ctx context.Context, ids []uuid.UUID) ([]CustomerSummary, error)
}

type LowStockVariant struct {
    VariantID uuid.UUID
    Available int64
}

type InventoryStatistics interface {
    LowStockVariants(ctx context.Context, threshold int64) ([]LowStockVariant, error)
}

type ProductVariantStates interface {
    DiscontinuedVariantIDs(ctx context.Context, ids []uuid.UUID) ([]uuid.UUID, error)
}
```

`contracts.VariantSummary` gains `CategoryID uuid.UUID` and `CategoryName string`; the
existing `ProductVariants` method is unchanged. The order statistics adapter lives in
the order module, the customer implementation on the user service, the inventory adapter
in the inventory module and the variant-state adapter in the product module; each asserts
its contract at compile time.

## Delivery Slices

1. **Foundation**: add `SHOP_TIMEZONE` and its validation; add
   `internal/contracts/dashboard.go`; add the category fields to `VariantSummary`;
   create migration 00017 and the guard test.
2. **Order read path**: category snapshot on `OrderLine`, insert/scan/projection
   changes, checkout/edit copy, and the order repository's four read queries plus the
   `statistics` adapter.
3. **Other read paths**: user directory counts/summaries and `CustomerStatistics`;
   inventory low-stock read and adapter; product discontinued-id read and adapter.
4. **Admin module**: domain read models/windows/metrics; application ports, DTOs,
   mapper and orchestration; auditor adapter; HTTP handler, router and Swagger
   annotations.
5. **Wiring**: construct all adapters in `cmd/api/main.go` and mount
   `/admin/dashboard`; add the route to the shared router.
6. **Close-out**: `docs/api-reference.md`, `docs/configuration.md`, `docs/modules.md`,
   `docs/modules/16-admin.md`, `docs/decisions/022-admin-dashboard-read-model.md` (plus
   the `docs/decisions/README.md` row), `frontend-guide.md`, `deferred.md`, `make swagger`,
   the full gate, and a frontend-guide sync check against the source.

## Risks and Mitigations

| Risk | Mitigation |
|---|---|
| Legacy lines cannot resolve a category after a product/variant deletion | Explicit `"Khác"` bucket; total still reconciles; limitation in `deferred.md`. |
| A product's category changed after an old sale | New lines are snapshotted at sale; the legacy backfill can only use the current category, which is recorded. |
| A misconfigured `SHOP_TIMEZONE` shifts month/day boundaries | Validate with `time.LoadLocation` at API startup; the runtime image ships `tzdata`; tests cover default and invalid values. |
| The endpoint becomes a fan-out of many queries | Coarse-grained `KPIs` contracts, one bounded query per section, a test that the read count does not grow with rows, and a 2-second target at 10k orders. |
| PII in the audit trail or logs | The audit metadata is only `days`/`lowStockThreshold`/`recentOrderCount`; the handler logs the path, not the body; tests assert the metadata shape. |
| Existing product `FindFactsByIDs` already joins `categories` across modules | Do not copy it into admin; extend the existing projection only; record the debt in `deferred.md`. |
| FE still expects bare numbers in chart arrays | `frontend-guide.md` enumerates the money-object and field changes; the contract is the source of truth. |
| Internal `VariantSummary` change breaks existing fakes/tests | Update every fake/test in the same slice; the compile error is the guard. |

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

| Violation | Why Needed | Simpler Alternative Rejected Because |
|---|---|---|
| The `admin` module has no `domain/repository` interface and no `domain/error` sentinel (packages contain a document only). | Module 16 Admin owns no table and defines no business transition; it is a read-only orchestrator over other modules' published contracts. | Giving admin its own tables or a duplicate read model would create a second source of truth and violate the module's stated scope; inventing a repository interface with no storage would be dead code. |
| The product module's `FindFactsByIDs` already reads `categories` across the module boundary; this feature extends that projection with `category_id`/`category_name`. | The order line needs the category snapshot at checkout; the existing facts read already joins categories for the visibility predicate, so extending it avoids a second category lookup per checkout. | Fixing the boundary would require a `CategoryQuery` extension and a product read refactor, which is a separate feature; the debt is recorded in `deferred.md`. |

One pre-existing cross-module read is extended and recorded above; no new violation is
introduced by this feature. The admin module adds no cross-module SQL.

## Phase 0 / Phase 1 Artifacts

- **research.md** — D1–D14: contract-based admin orchestration, category snapshot,
  revenue recognition, timezone/windows, money shape, coarse-grained contracts, order
  reads, customer reads without per-customer audit, low-stock split, recent-order
  composition, audit, read consistency, product category facts, and documentation.
- **data-model.md** — the two order-line columns, backfill, read projections for order/
  customer/inventory/product, admin read models, validation rules and what does not
  change.
- **contracts/openapi.yaml** — the endpoint, parameters, response schemas, nullable
  growth/customer/category id, errors and examples.
- **contracts/error-codes.md** — shared codes only and the `ADMIN_DASHBOARD_VIEWED`
  audit action.
- **quickstart.md** — runnable validation scenarios mapped to the spec's acceptance
  scenarios and the automated gates.
