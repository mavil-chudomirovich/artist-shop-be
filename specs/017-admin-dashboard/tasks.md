---

description: "Task list for Admin Dashboard Overview (module 16)"
---

# Tasks: Admin Dashboard Overview

**Input**: Design documents from `specs/017-admin-dashboard/`

**Prerequisites**: `plan.md`, `spec.md`, `research.md`, `data-model.md`, `contracts/openapi.yaml`, `contracts/error-codes.md`, `quickstart.md`

**Tests**: Included. The plan's Constitution Check requires test-first coverage for the
shared time-window/growth/rounding/zero-fill logic, the inventory low-stock calculation,
the category "Khác" bucket, and recent-order formatting. The order state machine, order
totals and pricing are read-only and unchanged. Write the tests FIRST and confirm they
FAIL before implementation.

**Organization**: Tasks are grouped by user story. US1 is the MVP endpoint; US2, US3 and
US6 harden the shared metrics and access rules; US4 and US5 complete the category and
recent-order sections. Setup and Foundational phases block all stories.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: Which user story this task belongs to (`[US1]`..`[US6]`)
- Exact repository-relative file paths are in every task

## Path Conventions

- New module: `internal/modules/admin/{domain,application,infrastructure,presentation}`
- Cross-module contracts: `internal/contracts/`
- Providing modules: `internal/modules/{order,product,user,inventory}/`
- Config: `internal/share/config/`
- Migrations: `migrations/`
- Composition root: `cmd/api/main.go`
- Feature docs: `specs/017-admin-dashboard/`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Module skeleton, read contracts, timezone configuration and the category
snapshot migration.

- [X] T001 [P] Create the `internal/modules/admin/` skeleton with a `doc.go` in each package: `internal/modules/admin/domain/{model,constant,error,repository}`, `internal/modules/admin/application/{interface,implement,dto,mapper}`, `internal/modules/admin/infrastructure/implement/auditor`, and `internal/modules/admin/presentation/{http,dto}`
- [X] T002 [P] Create `internal/contracts/dashboard.go` with `Money`, `TimeWindow`, `OrderKPIs`, `DailyRevenue`, `CategoryRevenue`, `RecentOrder`, `CustomerKPIs`, `CustomerSummary`, `LowStockVariant`, and the interfaces `OrderStatistics`, `CustomerStatistics`, `InventoryStatistics`, `ProductVariantStates` exactly as specified in `plan.md` §Go Read Contracts
- [X] T003 [P] Extend `contracts.VariantSummary` in `internal/contracts/product.go` with `CategoryID uuid.UUID` and `CategoryName string`; document that the facts read always populates them for a visible variant
- [X] T004 [P] Add `SHOP_TIMEZONE` to `internal/share/config/config.go` (default `Asia/Ho_Chi_Minh`, validated with `time.LoadLocation` in `ValidateForAPI`), add default/valid/invalid cases to `internal/share/config/config_test.go`, and add the commented placeholder to `.env.example`
- [X] T005 [P] Create `migrations/00017_order_item_category_snapshot.sql` (nullable `category_id`/`category_name`, the both-or-none check, the backfill update with a `-- data-migration:` note, Down drops the check then the columns) and `migrations/admin_dashboard_guard_test.go` asserting the Up/Down statements and the check

**Checkpoint**: The contracts, configuration and schema foundation exist.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Every cross-module read the dashboard needs, the category snapshot write
path, the admin ports and the audit adapter.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [X] T006 [P] Add `Category *CategorySnapshot` (`ID uuid.UUID`, `Name string`) to `model.OrderLine` in `internal/modules/order/domain/model/order.go`; document that nil is a legacy line whose category could not be resolved
- [X] T007 Update `internal/modules/order/infrastructure/implement/postgres/order.go`: add `category_id`/`category_name` to `lineColumns`, `scanLine`, the `Create` line insert and the `ReplaceLines` line insert; map a nil snapshot to SQL NULL and a zero ID to nil (never store the nil UUID)
- [X] T008 Update `internal/modules/order/application/implement/checkout.go` and `edit.go` to copy the category pair from `contracts.VariantSummary` onto each new line; update `checkout_test.go`/`edit_test.go` fakes and assertions for the new fields and the snapshot. Depends on T009, which is what populates the category fields on the facts; running T008 before T009 copies a zero pair and the nil-snapshot fallback
- [X] T009 [P] Extend `domainrepo.VariantFacts` in `internal/modules/product/domain/repository/variant.go`, the `variantFactsColumns`/`FindFactsByIDs` query in `internal/modules/product/infrastructure/implement/postgres/variant.go`, and the mapping in `internal/modules/product/infrastructure/implement/catalog/catalog.go` with `CategoryID`/`CategoryName`; update `catalog_test.go`
- [X] T010 Add `DiscontinuedIDs(ctx, ids)` to `internal/modules/product/domain/repository/variant.go` and its single query in `internal/modules/product/infrastructure/implement/postgres/variant.go`; add the `contracts.ProductVariantStates` adapter in `internal/modules/product/infrastructure/implement/variantstate/variantstate.go` with a compile-time assertion and `variantstate_test.go`
- [X] T011 [P] Add the order statistics read models in `internal/modules/order/domain/model/dashboard.go` (`OrderKPIs`, `DailyRevenue`, `CategoryRevenue`, `RecentOrder`) and the four read methods to `internal/modules/order/domain/repository/order.go`
- [X] T012 Implement the four bounded reads in `internal/modules/order/infrastructure/implement/postgres/order.go`: conditional-aggregate `KPIs`, `DailyRevenue` with `AT TIME ZONE`, `CategoryRevenue` grouped by the snapshot, and `RecentOrders` with two `LEFT JOIN LATERAL` projections; cast `sum(...)` results to `bigint`
- [X] T013 Add the `contracts.OrderStatistics` adapter in `internal/modules/order/infrastructure/implement/statistics/statistics.go` (compile-time assertion, domain-to-contract mapping, `VND` currency) with `statistics_test.go` using a fake repository
- [X] T014 [P] Add `CustomerKPIs` and `CustomerSummary` to `internal/modules/user/domain/model/directory.go` and the narrow `DirectoryRepository` port to `internal/modules/user/domain/repository/directory.go` (`KPIs(ctx, week)`, `SummariesByIDs(ctx, ids)`)
- [X] T015 Implement `internal/modules/user/infrastructure/implement/postgres/directory.go` with one conditional-aggregate customer KPI query and one bulk summary query; add `directory_integration_test.go` proving active/pending/disabled filtering and the week boundary
- [X] T016 Add `Directory` to the `Service` struct in `internal/modules/user/application/implement/profile.go` and implement `contracts.CustomerStatistics` in `internal/modules/user/application/implement/customer_stats.go` (no per-customer audit); add `customer_stats_test.go`
- [X] T017 [P] Add `LowStockVariant` to `internal/modules/inventory/domain/model/stock.go` and `LowStock(ctx, threshold, now)` to `internal/modules/inventory/domain/repository/inventory.go`
- [X] T018 Implement the low-stock query in `internal/modules/inventory/infrastructure/implement/postgres/inventory.go` (`GREATEST(level - held, 0)`, active unexpired holds, stock-row-only) and the `contracts.InventoryStatistics` adapter in `internal/modules/inventory/infrastructure/implement/statistics/statistics.go` using the injected clock; add `statistics_test.go` and the integration case in `statistics_integration_test.go`
- [X] T019 [P] Create `internal/modules/admin/domain/constant/constant.go` with `DefaultDays`, `AllowedDays`, `DefaultLowStockThreshold`, `MinLowStockThreshold`, `MaxLowStockThreshold`, `RecentOrderLimit`, `AuditDashboardViewed`, `TargetTypeDashboard`, `TargetIDOverview = "overview"`
- [X] T020 [P] Create `internal/modules/admin/application/interface/ports.go`, `actor.go` and `config.go`: aliases for the four contracts, `Clock`, `Auditor`, `Config{Location *time.Location}`, and the `Actor`/`WithActor`/`ActorFromContext` helpers
- [X] T021 Create the admin auditor adapter in `internal/modules/admin/infrastructure/implement/auditor/auditor.go` (shared `audit.Emitter`, compile-time assertion) and `doc.go`; the service records `action=ADMIN_DASHBOARD_VIEWED`, `outcome=SUCCESS`, `target_type=dashboard`, `target_id=overview`, metadata `{days, lowStockThreshold, recentOrderCount}`

**Checkpoint**: Foundation ready — the admin endpoint can be assembled from the read
contracts.

---

## Phase 3: User Story 1 - Admin sees the whole shop in one load (Priority: P1) 🎯 MVP

**Goal**: A single ADMIN request returns all four KPI groups, the trend, the category
breakdown and the five newest orders.

**Independent Test**: Seed orders in several states, customers, stock and categories;
call the endpoint as ADMIN; verify the envelope, all four KPI groups, the trend length,
the category array and at most five recent orders in one response; empty data still
returns a valid zero/empty response.

### Tests for User Story 1

> **NOTE: Write these tests FIRST; they must FAIL before implementation.**

- [ ] T022 [P] [US1] Add `internal/modules/admin/application/implement/overview_test.go` with fake contract ports, a fixed clock/location and a fake auditor: the full response shape, money objects, empty data, exactly one `ADMIN_DASHBOARD_VIEWED` event whose metadata has no PII, and a read-only assertion (the application depends only on read ports; no write/UnitOfWork port exists and none is called); the exact growth/percentage/category/customer/recent rules are covered by the US2–US5 tests
- [ ] T023 [P] [US1] Add `internal/modules/admin/presentation/http/handler_test.go` for the ADMIN 200 path, the `{data, meta}` envelope and the JSON field names

### Implementation for User Story 1

- [ ] T024 [US1] Add the admin domain read models in `internal/modules/admin/domain/model/overview.go` (`Overview`, `RevenueKPI`, `OrderKPI`, `CustomerKPI`, `InventoryKPI`, `RevenuePoint`, `CategoryShare`, `RecentOrder`, `Money`, `CustomerSummary`)
- [ ] T025 [US1] Add the application DTOs in `internal/modules/admin/application/dto/overview.go`
- [ ] T026 [US1] Add the domain-to-application mapper in `internal/modules/admin/application/mapper/overview.go`
- [ ] T027 [US1] Add the orchestration in `internal/modules/admin/application/implement/overview.go`: read the clock once, call all four contracts, assemble the four KPI groups, and record the one audit event. Use a basic current/previous calendar-month window here and leave the exact MTD/clamp growth, percentage, customer-summary and item-summary rules to US2–US5. This window is a deliberate placeholder: T037 MUST replace it before the feature is closed out; US1 is a demoable MVP, not the final revenue definition
- [ ] T028 [P] [US1] Add the HTTP response DTOs in `internal/modules/admin/presentation/dto/overview.go` (`MoneyResponse`, nullable `growthPercent`, nullable `customer`, nullable `categoryId`, `itemCount`/`itemQuantity`)
- [ ] T029 [US1] Add the handler and full Swagger annotations (`@Summary`, `@Description`, `@Tags`, `@Param`, `@Success`, `@Failure`, `@Router`) in `internal/modules/admin/presentation/http/handler.go`
- [ ] T030 [US1] Add the router in `internal/modules/admin/presentation/http/router.go` with `RequireRole(hooks, string(access.RoleAdmin))` and `GET /overview`
- [ ] T031 [US1] Wire the order/user/inventory/product statistics adapters, the admin service, auditor, handler and router in `cmd/api/main.go`; load `cfg.ShopTimezone` with `time.LoadLocation` and mount `/admin/dashboard`

**Checkpoint**: The MVP endpoint responds with every section; US2–US6 now harden the
sections.

---

## Phase 4: User Story 2 - Revenue and orders use fair windows and the shop timezone (Priority: P1)

**Goal**: Month-to-date vs the same period of the previous month, a trailing 7/30-day
trend, a Monday week, growth nullable on a zero base, and correct local day labels.

**Independent Test**: Seed orders across six states and around UTC/local midnight and
month boundaries; verify only PAID/SHIPPED/COMPLETED contribute revenue, the previous
window has the same elapsed length, growth is `null` when the base is zero, and the trend
has exactly `days` zero-filled local dates.

### Tests for User Story 2

- [ ] T032 [P] [US2] Add `internal/modules/admin/domain/model/window_test.go`: current-month start, MTD previous window, clamp at the previous month end, Monday week start, trailing 7/30 days, and a leap-year/31-day boundary
- [ ] T033 [P] [US2] Add `internal/modules/admin/domain/model/metrics_test.go`: growth null on zero base, 2-decimal rounding, negative growth, 1-decimal percentage, zero-revenue denominator
- [ ] T034 [P] [US2] Add `internal/modules/order/infrastructure/implement/statistics/statistics_integration_test.go`: revenue statuses/currency/windows, a local-midnight boundary, and `KPIs` totals against seeded orders

### Implementation for User Story 2

- [ ] T035 [P] [US2] Add `internal/modules/admin/domain/model/window.go` (`CurrentMonth`, `PreviousMonthSamePeriod`, `CurrentWeek`, `TrailingDays`)
- [ ] T036 [P] [US2] Add `internal/modules/admin/domain/model/metrics.go` (growth, percentage, rounding)
- [ ] T037 [US2] Update `internal/modules/admin/application/implement/overview.go` to use the windows/metrics and to zero-fill the trend from the domain date list
- [ ] T038 [US2] Update `internal/modules/admin/application/mapper/overview.go` for `YYYY-MM-DD`, `T2`..`CN` (7) and `DD/MM` (30)
- [ ] T039 [US2] Extend `internal/modules/admin/application/implement/overview_test.go` with MTD/clamp, null growth, zero-fill and a timezone-boundary case

**Checkpoint**: The revenue/order KPIs and trend are timezone-correct and fair.

---

## Phase 5: User Story 3 - Inventory warnings and customer health are accurate (Priority: P1)

**Goal**: Low-stock counts use available stock (physical minus active unexpired holds),
exclude `DISCONTINUED`, and ignore variants never stocked; customer counts include only
active CUSTOMER accounts and active new accounts this week.

**Independent Test**: Seed a held variant, an expired hold, a `DISCONTINUED` variant and
a never-stocked variant, plus active/pending/disabled customers; verify the low-stock
count and both customer counts.

### Tests for User Story 3

- [ ] T040 [P] [US3] Add the low-stock integration cases to `internal/modules/inventory/infrastructure/implement/statistics/statistics_integration_test.go`: active hold lowers available, expired hold does not, no stock row is absent, threshold is exclusive
- [ ] T041 [P] [US3] Add the customer KPI integration cases to `internal/modules/user/infrastructure/implement/postgres/directory_integration_test.go`: active only, Monday-to-now week, pending/disabled excluded
- [ ] T042 [US3] Extend `internal/modules/admin/application/implement/overview_test.go`: the admin filters `DISCONTINUED` ids out of the low-stock candidates and maps both customer KPIs

### Implementation for User Story 3

- [ ] T043 [US3] Update `internal/modules/admin/application/implement/overview.go` to call `CustomerStatistics.KPIs`, `InventoryStatistics.LowStockVariants` and `ProductVariantStates.DiscontinuedVariantIDs`, remove the discontinued ids, and set `lowStockCount`/customer KPIs

**Checkpoint**: Inventory and customer sections are accurate independently.

---

## Phase 6: User Story 6 - Only ADMIN can view; invalid parameters are rejected clearly (Priority: P1)

**Goal**: Guest 401, CUSTOMER 403 (with the foundation's denial audit), invalid `days` or
`lowStockThreshold` 400 with the exact field name.

**Independent Test**: Call the route with no token, a CUSTOMER token and an ADMIN token;
call as ADMIN with `days=14`, `days=abc`, `lowStockThreshold=-1`, `lowStockThreshold=10001`
and `lowStockThreshold=0`; verify statuses and `details[].field`.

### Tests for User Story 6

- [ ] T044 [P] [US6] Add `internal/modules/admin/presentation/http/router_test.go` with fake `AuthHooks`: guest 401, CUSTOMER 403 and `OnDenied` called, ADMIN 200, and the four invalid-parameter 400 cases naming `days`/`lowStockThreshold`; assert or explicitly note that the 429 `RATE_LIMITED` behaviour is inherited from the shared `/api/v1` limiter (the admin router does not install a second one)

### Implementation for User Story 6

- [ ] T045 [US6] Add the query parsing/validation helpers and constants to `internal/modules/admin/presentation/http/handler.go`: default 7/5, `days` in {7,30}, threshold 0..10000, `httpx.NewWithDetails(CodeValidation, Detail{Field, Issue})`
- [ ] T046 [US6] Verify the router in `internal/modules/admin/presentation/http/router.go` calls the foundation's `OnDenied` for a CUSTOMER and fix the route if the test exposes a gap

**Checkpoint**: The endpoint is access-controlled and rejects bad input precisely.

---

## Phase 7: User Story 4 - Category breakdown keeps history (Priority: P2)

**Goal**: Current-month revenue is grouped by the category snapshotted at sale; a changed
or deleted product does not move or drop old revenue; unresolved lines become "Khác".

**Independent Test**: Sell a variant in category A, move the product to B, and verify the
old revenue stays in A; create a legacy/null-category line and verify it appears as
"Khác"; sum the breakdown and compare it with `currentMonthAmount`.

### Tests for User Story 4

- [ ] T047 [US4] Add the category cases to `internal/modules/order/infrastructure/implement/statistics/statistics_integration_test.go`: snapshot persists at checkout/edit, a later category change does not move old revenue, and a null snapshot is returned as a null id
- [ ] T048 [P] [US4] Add `internal/modules/admin/application/mapper/overview_test.go`: null id -> "Khác", percentage 1-decimal, amount-desc/name-asc ordering
- [ ] T049 [US4] Extend `internal/modules/admin/application/implement/overview_test.go`: the category section uses the KPI current-month revenue as the denominator and the unknown bucket is counted

### Implementation for User Story 4

- [ ] T050 [US4] Update `internal/modules/admin/application/implement/overview.go`: map `CategoryRevenue`, name null ids "Khác", compute percentage, sort and filter `amount > 0`
- [ ] T051 [US4] Update `internal/modules/admin/application/mapper/overview.go` for the category share fields and the "Khác" label
- [ ] T052 [P] [US4] Add `migrations/admin_dashboard_migration_integration_test.go`: the 00017 backfill fills resolvable lines, the both-or-none check refuses a half snapshot, and Down/Up is reversible

**Checkpoint**: Historical category revenue is stable and reconciled.

---

## Phase 8: User Story 5 - Recent orders carry context without leaking extra data (Priority: P2)

**Goal**: The five newest orders include a customer summary (or `null`), a first-line
summary, line count/total quantity, total, status and creation time.

**Independent Test**: Create more than five orders, including one for a then-deleted
account and one multi-line order; verify only five newest, `customer: null` for the
deleted account, the `+N` summary, and the two counts.

### Tests for User Story 5

- [ ] T053 [US5] Add the recent-order cases to `internal/modules/order/infrastructure/implement/statistics/statistics_integration_test.go`: five newest by created_at/id, first line by position, line count and total quantity, deleted-account customer id retained
- [ ] T054 [US5] Add recent-order cases to `internal/modules/admin/application/mapper/overview_test.go`: `itemSummary` formatting, `+N`, missing customer -> nil, `itemCount` vs `itemQuantity`
- [ ] T055 [US5] Extend `internal/modules/admin/application/implement/overview_test.go`: one bulk `CustomerSummaries` call, missing id -> nullable customer, max five orders

### Implementation for User Story 5

- [ ] T056 [US5] Update `internal/modules/admin/application/implement/overview.go` to request five recent orders, deduplicate customer ids, call `CustomerSummaries` once, and map missing accounts to nil
- [ ] T057 [US5] Update `internal/modules/admin/application/mapper/overview.go` to build `itemSummary` (`<name> <variantName> (x<quantity>)` plus ` +N`) and map the counts; trim the joined parts and avoid a double space when `variantName` is empty (legacy tombstone)

**Checkpoint**: Recent orders are operationally useful and PII stays within the
ADMIN-only response.

---

## Phase 9: Polish & Cross-Cutting Concerns

**Purpose**: Documentation, generated API contract, frontend hand-off, deferred debt and
the close-out gates.

### Documentation and contracts

- [ ] T058 [P] Add the endpoint section to `docs/api-reference.md`: method/path, ADMIN requirement, `days`/`lowStockThreshold`, response example, the shared error codes, timezone/revenue/VND notes and the `ADMIN_DASHBOARD_VIEWED` audit note
- [ ] T059 [P] Add the `SHOP_TIMEZONE` row/default/validation rule to `docs/configuration.md`
- [ ] T060 [P] Update `docs/modules/16-admin.md` with the feature's Spec Kit status and the delivered overview endpoint; update `docs/modules.md` with module 16's status/roadmap; write `docs/decisions/022-admin-dashboard-read-model.md` (read-only admin module over published read contracts, category snapshot on order lines, shop timezone/MTD comparison) and add its row to `docs/decisions/README.md`

### Frontend Integration Guide *(bắt buộc — hiến pháp §Governance)*

- [ ] T061 [P] **Có đổi bề mặt client** → write `specs/017-admin-dashboard/frontend-guide.md`: the new endpoint; money objects everywhere; `orders` fields `totalOrders`/`thisMonthOrders`/`lastMonthOrders`; nullable `growthPercent`/`customer`/`categoryId`; `itemCount`/`itemQuantity`; the six BE statuses and their FE labels; `dayLabel` 7 vs 30; `days`/`lowStockThreshold`
- [ ] T062 **Đồng bộ guide** after the source stabilises: re-read `specs/017-admin-dashboard/frontend-guide.md` and compare it with the handler/DTOs/route with counts (every JSON field, every enum value, every route, every HTTP status); fix every mismatch or record the unverified part and why

### Swagger annotations *(bắt buộc nếu feature có endpoint)*

- [ ] T063 **Có endpoint thêm**: ensure the handler in `internal/modules/admin/presentation/http/handler.go` carries every Swagger annotation, run `make swagger`, and confirm `make swagger-check` (in `make check`) is green; the generated `docs/swagger/` must be committed

### Phát hiện ngoài phạm vi *(hiến pháp §Governance)*

- [ ] T064 [P] **Ghi vào `specs/017-admin-dashboard/deferred.md`**, KHÔNG để nguyên `- [ ]` ở file này. Ghi rõ: revenue attributed by `created_at` (a later `paid_at`/payment-event feature), legacy category gaps, the pre-existing product→`categories` join debt, index/caching if volume grows, and multi-currency support. Nếu một phát hiện làm mất hiệu lực bằng chứng của một task đã tick `[X]`, nêu rõ số task đó.

### Close-out gates

- [ ] T065 Run the `quickstart.md` scenarios against a disposable local stack (Scenarios A–J), record PASS/FAIL for each, and fix any implementation/doc drift; a scenario that cannot run is reported BLOCKED with the reason
- [ ] T066 Add the SC-006/FR-032 verification: (a) a bounded-call assertion in `internal/modules/admin/application/implement/overview_test.go` (the 8 contract-method calls across the four read interfaces are fixed regardless of fixture size); (b) a DB-backed integration test in `internal/modules/admin/presentation/http/overview_integration_test.go` (build tag `integration`) that wires the real order/user/inventory/product adapters and admin service, seeds 10,000 orders and 1,000 variants, and measures the full overview against the 2-second target. It shares `overview_test.go`, so it runs after T049/T055. Record the measured duration; if the environment cannot run the 10k fixture, report the check as BLOCKED with the reason — do not claim SC-006 verified
- [ ] T067 Run `make lint` and `make test`; fix every failure on the feature tree
- [ ] T068 Run `make test-integration` (Docker required); prove the order/customer/inventory/product reads and the 00017 backfill/rollback; a run that reports success without executing a test is a false green and must be investigated
- [ ] T069 Run `make check` (format + tidy + vet + lint + unit + integration + build + swagger-check) as the close-out gate; it must be green before the feature is reported done

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies; T001–T005 can start immediately.
- **Foundational (Phase 2)**: Depends on Setup; blocks all user stories.
- **US1 (Phase 3)**: Depends on Foundational; the MVP endpoint.
- **US2 (Phase 4)**, **US3 (Phase 5)**, **US6 (Phase 6)**: depend on Foundational and US1.
- **US4 (Phase 7)**, **US5 (Phase 8)**: depend on Foundational and US1; they share the
  category/recent adapters built in Foundational.
- **Polish (Phase 9)**: depends on all selected stories.

### User Story Dependencies

- **US1 (P1)**: no story dependency after Foundational.
- **US2 (P1)**: depends on US1's orchestrator/mapper; owns the window/metrics logic.
- **US3 (P1)**: depends on US1's orchestrator; owns the customer/inventory composition.
- **US6 (P1)**: depends on US1's handler/router; owns query validation and the boundary tests.
- **US4 (P2)**: depends on US1's orchestrator/mapper and the snapshot read adapter; owns the category section.
- **US5 (P2)**: depends on US1's orchestrator/mapper and the recent-order read adapter; owns the recent section.

T037, T043, T050 and T056 all edit `internal/modules/admin/application/implement/overview.go`
and T038, T051, T057 all edit `.../mapper/overview.go`. Run those story
phases sequentially or coordinate the shared-file edits. Test tasks may run in parallel
only where they touch different files: T032/T033/T034, T040/T041, T044, T048 and T052 are
independent; the app, mapper and order-statistics test files are shared, so T042, T047,
T049, T053, T054 and T055 must wait for the earlier task that created the file.

### Within Each User Story

- Tests MUST be written and must FAIL before implementation.
- Domain models before application DTO/mapper/implement.
- Application before presentation.
- Handler/router before wiring.
- A story is complete only after its tests and its checkpoint pass.

### Parallel Opportunities

- Phase 1: T001–T005 are all different files and can run in parallel.
- Foundational: T006, T009, T011, T014, T017, T019 and T020 are independent files; T007
  depends on T006; T008 depends on T006/T003/T009; T010 depends on T009; T012 depends on
  T011/T007; T013 depends on T012; T015/T016 depend on T014; T018 depends on T017; T021
  depends on T020.
- US1: T022/T023 tests; T024/T025/T028 models/DTOs.
- US2: T032/T033/T034 tests; T035/T036 files.
- US3: T040/T041 tests (different files); T042 shares `overview_test.go` with US1/US2 and runs after them.
- US6: T044 test (its own `router_test.go`).
- US4: T048 (mapper test) and T052 (migration integration) are independent; T047 shares the order-statistics test file with T034; T049 shares `overview_test.go`.
- US5: T053 shares the order-statistics test file; T054 shares the mapper test; T055 shares `overview_test.go`.
- Polish: T058–T061 docs/frontend guide and T064 deferred are parallel across different files; T062 sync depends on the source, T063 (Swagger) depends on T029, T065 quickstart depends on the feature, T066 performance verification depends on the shared `overview_test.go` and the real adapters, and T067–T069 gates are sequential.

---

## Parallel Example: User Story 1

```bash
# Write the failing tests first:
Task: "Application overview test in internal/modules/admin/application/implement/overview_test.go"
Task: "HTTP 200 handler test in internal/modules/admin/presentation/http/handler_test.go"

# Then the independent model/DTO files:
Task: "Admin domain read models in internal/modules/admin/domain/model/overview.go"
Task: "Application DTOs in internal/modules/admin/application/dto/overview.go"
Task: "HTTP response DTOs in internal/modules/admin/presentation/dto/overview.go"
```

## Parallel Example: Foundational read adapters

```bash
Task: "Order statistics read models in internal/modules/order/domain/model/dashboard.go"
Task: "User directory model/port in internal/modules/user/domain/model/directory.go and domain/repository/directory.go"
Task: "Inventory low-stock model/port in internal/modules/inventory/domain/model/stock.go and domain/repository/inventory.go"
Task: "Admin constants in internal/modules/admin/domain/constant/constant.go"
Task: "Admin application ports in internal/modules/admin/application/interface/ports.go"
```

---

## Implementation Strategy

### MVP First (US1 only)

1. Complete Phase 1 (Setup).
2. Complete Phase 2 (Foundational).
3. Complete Phase 3 (US1).
4. **STOP and VALIDATE**: call the endpoint as ADMIN, confirm all sections and the empty
   response; this is the demoable MVP.
5. Deploy/demo if ready.

### Incremental Delivery

1. Setup + Foundational → the read contracts and snapshot are ready.
2. US1 → the endpoint returns all sections (MVP).
3. US2 → fair timezone-aware revenue/order KPIs and a zero-filled trend.
4. US3 → accurate low-stock and customer KPIs.
5. US6 → strict access and parameter validation.
6. US4 → historical category breakdown.
7. US5 → recent-order context and nullable customer.
8. Polish → docs, frontend guide, Swagger, deferred debt and `make check`.

### Definition of Done

- The endpoint is ADMIN-only, returns the complete response, and every acceptance scenario
  in the spec passes.
- All money fields are `{amount, currency}`; no float is used for money.
- The trend has exactly `days` entries; category totals reconcile with current-month
  revenue; low stock respects holds and excludes `DISCONTINUED`.
- `docs/api-reference.md`, `contracts/openapi.yaml`, `frontend-guide.md` and
  `docs/swagger/` agree with the source.
- `make check` is green.

