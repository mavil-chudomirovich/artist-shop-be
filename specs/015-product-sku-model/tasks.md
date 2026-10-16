---
description: "Task list for Product SKU Model (parent product + variants)"
---

# Tasks: Product SKU Model

**Input**: Design documents from `/specs/015-product-sku-model/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Included. The constitution mandates test-first for pricing, inventory and state transitions
(positive + negative), and the spec's Success Criteria require automated tests (catalogue one-entry,
variant independence, storage-level uniqueness, order snapshots, migration). The restructure changes
pricing/inventory keys, so the existing product/cart/order/inventory suites are **updated to the variant
key**, not dropped.

**Organization**: Tasks are grouped by user story. **US1 and US2 are the core and ship together.** The
Foundational phase is unusually large: because the unit of sale moves to the variant, the repo does not
compile until the contracts and the four modules are re-keyed, so Phase 2 is the spine both stories
stand on.

**Blast radius**: modules 04 (product), 05 (inventory), 06 (cart), 07 (order) + `internal/contracts` + 1
migration. Modules 01/02/03/08 and the shared foundation are unchanged.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: US1–US4 (user stories); setup/foundational/polish phases have no story label
- Exact file paths in every task

## Path Conventions

- Single Go module; modules under `internal/modules/<module>/`; contracts in `internal/contracts/`;
  migrations under `migrations/`.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: the domain constants, codes and sentinels the whole feature maps to

- [x] T001 [P] Add the variant audit actions and the new code constants in `internal/modules/product/domain/constant/audit.go` and `internal/modules/product/domain/constant/codes.go` (`PRODUCT_VARIANT_ADDED`, `PRODUCT_VARIANT_UPDATED`, `PRODUCT_VARIANT_REMOVED`; `PRODUCT_CODE_TAKEN`, `VARIANT_SKU_TAKEN`, `VARIANT_NOT_FOUND`, `VARIANT_LAST_REMOVAL`, `VARIANT_STATE_TRANSITION_INVALID`). 014's group code constants are left in place here and removed with 014's code in T026 (so the build never breaks)
- [x] T002 [P] Add the variant sentinels in `internal/modules/product/domain/error/errors.go` (`ErrProductCodeTaken`, `ErrVariantSKUTaken`, `ErrVariantNotFound`, `ErrVariantLastRemoval`, `ErrVariantStateTransitionInvalid`). 014's group sentinels are left in place here and removed with 014's code in T026

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: migrate the schema, model the variant, re-key the contracts and the four modules so the repository compiles and the existing (updated) suites pass on the new model

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

### Schema and domain

- [x] T003 Write `migrations/00014_product_sku_model.sql` per `data-model.md` D11: create `product_variants`; add `products.code` (backfill = slug, then NOT NULL + unique on `lower(btrim(code))`); backfill one default variant per product; repoint `stock_levels`/`inventory_transactions`/`stock_holds`/`cart_items` to `variant_id`; extend `order_items` (variant_id + product_name/product_code/variant_name/sku) with the new natural key `(order_id, product_code, sku)`; drop 014's objects and the product flags (`sell_state`, `is_preorder`, `preorder_expected_at`, `is_set`) with `DROP ... IF EXISTS`; full `-- +goose Up` and `-- +goose Down`
- [x] T004 [P] Add the migration guard test `migrations/product_sku_model_guard_test.go` (every table/column/constraint/index created in Up and dropped in Down; the variant SKU unique index; the code unique index; the sell-state check; the inventory/cart/order re-key)
- [x] T005 [P] Create the `Variant` entity and its sell-state transitions in `internal/modules/product/domain/model/variant.go` (`NewVariant`, `Apply`, `Launch`/`SellOut`/`Restock`/`Retire`, effective price = `price_amount` or the product's default, validation of name/sku/position)
- [x] T006 [P] Write the variant domain test `internal/modules/product/domain/model/variant_test.go` (effective-price fallback; every transition positive + negative; retired terminal; SKU required)
- [x] T007 Update `internal/modules/product/domain/model/product.go`: add `Code`; remove `SellState`/`IsPreorder`/`PreorderExpectedAt`/`IsSet` and 014's `VariantGroupID`/`VariantLabel`; a `ProductDraft` carries ≥1 variant (or creation is refused)
- [x] T008 [P] Update `internal/modules/product/domain/model/product_test.go` for the new product shape
- [x] T009 [P] Define the `VariantRepository` port and the variant-aware product read views in `internal/modules/product/domain/repository/variant.go` and `internal/modules/product/domain/repository/product.go` (list-by-product ordered, create/update/delete, count; catalogue entry with lowest price + default image; detail with visible variants; variant facts by ids)

### Persistence adapters

- [x] T010 Implement the `VariantRepository` adapter in `internal/modules/product/infrastructure/implement/postgres/variant.go` (projection + scan, CRUD, the SKU-unique classification, the ordered list)
- [x] T011 Update `internal/modules/product/infrastructure/implement/postgres/product.go`: `code` column + unique classification; remove state/flags; `ListCatalogue` returns one entry per product with the lowest effective variant price and the default-variant image; the detail read returns the product's content + its visible variants
- [x] T012 [P] Update/extend the Postgres integration tests `internal/modules/product/infrastructure/implement/postgres/*_integration_test.go` for the one-entry catalogue, per-variant data and storage invariants
- [x] T013 [P] Add the migration/backfill integration test `internal/modules/product/infrastructure/implement/postgres/product_sku_model_migration_integration_test.go`: after `00014`, every product has a code and one default variant; the repoint maps variant_ids correctly

### Cross-module contracts

- [x] T014 Replace `ProductCatalog` with a variant-facts contract and change `ProductLookup`, and make feature 007's availability **signal** variant-keyed, in `internal/contracts/product.go` (`Variants(variantIDs) []VariantSummary` with `{VariantID, ProductID, ProductName, ProductCode, VariantName, SKU, OnSale, Price}`; a variant-existence lookup; `MarkVariantOutOfStock`/`MarkVariantOnSale(variantID)`)
- [x] T015 [P] Re-key `internal/contracts/inventory.go` to variant ids (`AvailableQuantity(variantIDs)`, `Reserve`/`Release`/`ApplySale` take `variantID`)
- [x] T016 [P] Update `internal/contracts/cart.go`: `CartLine` carries the chosen `VariantID` and the snapshots (`ProductID`, `ProductName`, `ProductCode`, `VariantName`, `SKU`, `Quantity`, `UnitPrice`)
- [x] T017 Rebuild the product→contract **read** adapter `internal/modules/product/infrastructure/implement/catalog/catalog.go` on variant facts
- [x] T018 Rebuild the product→contract **availability-signal** adapter `internal/modules/product/infrastructure/implement/availability/availability.go` on the variant-keyed `MarkVariantOutOfStock`/`MarkVariantOnSale` (the auto `ACTIVE ↔ OUT_OF_STOCK` edge inventory calls — FR-015, research D4/D6)

### Inventory module re-key

- [x] T019 [P] Re-key `internal/modules/inventory/domain/{model,repository}` and `infrastructure/implement/postgres/inventory.go` to `variant_id` (stock row per variant, ledger, holds)
- [x] T020 Re-key the inventory use cases `internal/modules/inventory/application/implement/{stock,holds,reconcile,events,history}.go` and their DTOs to the variant id
- [x] T021 Update the inventory availability/reservation adapters `internal/modules/inventory/infrastructure/implement/{availability,reservation}/*.go` to the variant-keyed contract

### Cart module re-key

- [x] T022 Re-key `internal/modules/cart/{domain,infrastructure}` (line identity = variant id; `cart_items.variant_id`) and the cart DTOs/mapper
- [x] T023 Re-key `internal/modules/cart/application/implement/cart.go` to the variant-facts contract: `Add` takes a `variantId`, reads the variant's on-sale state and effective price, and the view shows product name + variant name + availability

### Order module re-key

- [x] T024 Extend `internal/modules/order/domain/model` `OrderLine` with the snapshots (`VariantID`, `ProductName`, `ProductCode`, `VariantName`, `SKU`, `UnitPrice`, `Quantity`) and update `infrastructure/implement/postgres/order.go`
- [x] T025 Re-key `internal/modules/order/application/implement/checkout.go` (and the order views) to the variant-facts contract: one line per (product code, SKU), re-checking per variant that it is on sale, that enough is available, and that the captured price still matches the variant's current effective price (a changed price refuses the whole checkout, the order module's existing rule — FR-021)

### Remove feature 014 and wire up

- [x] T026 Remove feature 014's grouping code and routes: delete the variant-group model/port/adapter/DTOs/handlers, the `/product-groups` and `/admin/product-groups*` routes, and their tests; **and remove 014's now-unused group code constants (T001) and sentinels (T002)**; keep `product_images`; and remove the product-level sell-state endpoint `POST /admin/products/{id}/state` (the product has no sell state; the variant state endpoint replaces it). `00013_product_variants.sql` is kept as a committed historical migration and its guard test stays; `00014` drops 014's objects with `DROP ... IF EXISTS` (so a DB that ran `00013` and one that did not both reach the new model)
- [x] T027 Wire the whole composition root `cmd/api/main.go` to the variant model (variant repository, changed catalog/availability/inventory/cart/order adapters)
- [x] T028 [P] Update every affected test fake/stub across `product`, `inventory`, `cart`, `order` so the repository builds and the existing suites compile against the new contracts

**Checkpoint**: the repository builds on the variant model and the existing (updated) unit suites pass

---

## Phase 3: User Story 1 - A customer picks a variant and buys it (Priority: P1) 🎯 MVP

**Goal**: the catalogue shows one entry per product (lowest price, default-variant image); the detail lists variants with price and stock status; adding to the cart requires a variant.

**Independent Test**: seed a product with two variants; `GET /products` shows one entry with the lowest price; `GET /products/{slug}` lists both variants with status; add an in-stock variant to the cart; adding a sold-out variant is refused; adding without a variant is refused.

### Tests for User Story 1

- [x] T029 [P] [US1] Write failing unit tests in `internal/modules/product/application/implement/catalogue_test.go`: one catalogue entry per product with the lowest effective variant price and the default-variant image; a product hidden when no variant is on sale
- [x] T030 [P] [US1] Write failing unit tests for the product detail (visible variants with price + stock status; default = first) in `catalogue_test.go`
- [x] T031 [P] [US1] Write failing unit tests in `internal/modules/cart/application/implement/cart_test.go`: adding by variant captures the variant's effective price; adding a not-on-sale/short variant is refused; adding without a variant is refused

### Implementation for User Story 1

- [x] T032 [US1] Implement the catalogue read in `internal/modules/product/application/implement/catalogue.go` (one entry per product; category-visible and an on-sale variant required)
- [x] T033 [US1] Implement the product detail read (product content + gallery + visible variants, each joined with its availability from the inventory read) in `catalogue.go`
- [x] T034 [US1] Finish the cart add/change/remove/read by variant in `internal/modules/cart/application/implement/cart.go` and its mapper (product name + variant name + availability)
- [x] T035 [US1] Add/annotate the handlers and routes in `internal/modules/product/presentation/http/{handler,router,errors}.go` and `internal/modules/cart/presentation/http/*`; presentation DTOs for the catalogue entry, product detail + variants, and the cart line; map the new codes
- [x] T036 [P] [US1] Write the integration test `internal/modules/product/presentation/http/catalogue_sku_integration_test.go` and `internal/modules/cart/presentation/http/cart_sku_integration_test.go` (one-entry catalogue, detail with status, add-by-variant, the three refusals), plus: a hidden or unknown **product** and a hidden or unknown **variant** each answer the same not-found (FR-008), and a checkout produces one order line per (product code, SKU) carrying the snapshot (SC-003)

**Checkpoint**: US1 works end-to-end against real handlers

---

## Phase 4: User Story 2 - An operator maintains a product and its variants (Priority: P1) 🎯 MVP

**Goal**: admin creates a product with ≥1 variant, manages variants (add/edit/remove), and every mutation is guarded and audited.

**Independent Test**: create a product with one variant; add a second; edit name/sku/price/image/order/state; remove one; removing the last is refused; a duplicate code or SKU is refused; a customer token is refused on every route.

### Tests for User Story 2

- [x] T037 [P] [US2] Write failing unit tests in `internal/modules/product/application/implement/maintenance_test.go`: create requires ≥1 variant; adding/editing/removing a variant; duplicate code (`ErrProductCodeTaken`); duplicate SKU (`ErrVariantSKUTaken`); removing the last variant (`ErrVariantLastRemoval`); the variant sell-state transition refused naming the state; each mutation audited
- [x] T038 [P] [US2] Write failing handler tests for the admin product/variant routes and the role refusal in `internal/modules/product/presentation/http/maintenance_test.go`

### Implementation for User Story 2

- [x] T039 [US2] Implement the admin product use cases in `internal/modules/product/application/implement/maintenance.go` (create with ≥1 variant; update; delete cascading variants; audit `PRODUCT_CREATED/UPDATED/DELETED`); the product gallery keeps using the catalogue's existing product-picture routes (unchanged)
- [x] T040 [US2] Implement the variant use cases (add/edit/remove, `variant.go` or `variants.go` in `application/implement`) with the last-removal and unique-SKU rules, the sell-state transition, the variant image upload/delete through the media capability, and the variant state transition, auditing `PRODUCT_VARIANT_*`
- [x] T041 [US2] Add the variant handlers/routes (`POST/PATCH/DELETE /admin/products/{id}/variants...`, `POST/DELETE /admin/products/{id}/variants/{variantId}/image`, `POST /admin/products/{id}/variants/{variantId}/state`) and update the product handlers in `internal/modules/product/presentation/http/{handler,router,errors}.go`; **remove** the product-level state route/handler/annotation; add/refresh the admin product+variant DTOs and Swagger annotations
- [x] T042 [US2] Map the new codes in `presentation/http/errors.go` (`PRODUCT_CODE_TAKEN`, `VARIANT_SKU_TAKEN`, `VARIANT_NOT_FOUND`, `VARIANT_LAST_REMOVAL`, `VARIANT_STATE_TRANSITION_INVALID`) and cover them in `errors_test.go`
- [x] T043 [P] [US2] Write the integration test `internal/modules/product/presentation/http/variant_maintenance_integration_test.go` (full admin flow, duplicate refusals, last-removal, role refusal, audit entry per mutation)

**Checkpoint**: US1 + US2 deliver the restructured catalogue and admin surface

---

## Phase 5: User Story 3 - Each variant is its own stock and selling unit (Priority: P2)

**Goal**: per-variant stock and sell state; the stock-driven transition; independence.

**Independent Test**: two variants with different stock/state; sell one out; only it changes; the effective price falls back to the product's default when the variant price is null.

### Tests for User Story 3

- [ ] T044 [P] [US3] Write the integration test `internal/modules/inventory/infrastructure/implement/postgres/inventory_variant_integration_test.go`: per-variant stock, the hold/sale/release per variant, and the automatic `ACTIVE ↔ OUT_OF_STOCK` transition on the variant as availability crosses zero
- [ ] T045 [P] [US3] Write the integration test proving variant price/state/picture independence and the null-price fallback (`internal/modules/product/infrastructure/implement/postgres/product_sku_model_integration_test.go`)

### Implementation for User Story 3

- [ ] T046 [US3] Confirm/adjust the per-variant availability read path and the variant-facts projection (`internal/modules/inventory/...` and `.../product/...`) so nothing is inherited from a sibling; T044/T045 are the proof

**Checkpoint**: variant independence holds end-to-end

---

## Phase 6: User Story 4 - Existing products are migrated (Priority: P2)

**Goal**: every pre-existing product gains a code and one default variant and keeps selling.

### Tests for User Story 4

- [ ] T047 [P] [US4] Extend the migration integration test (`T013`) to assert every seeded legacy product has `code` = slug and exactly one default variant, and that a migrated product still checks out
- [ ] T048 [US4] Update the affected existing integration tests (catalogue, cart, order, inventory) that seeded products directly, so they seed a product **and its variant** after `00014`

**Checkpoint**: migrated data behaves as before

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: contract docs, ADR supersede, swagger, the frontend hand-off and the full gate

- [ ] T049 [P] Update `docs/api-reference.md` for module 04 (and the cart/order sections, **and module 05 inventory whose `/admin/inventory/*` routes are now keyed by `variantId`**): the one-entry catalogue, the product detail with variants, the admin product+variant routes, the cart-by-variant routes, the new/removed error codes, the summary table and the change log; **remove** feature 014's product-group endpoints **and feature 006's product-level state endpoint** (Constitution VIII)
- [ ] T050 [P] Update `docs/modules/04-product.md` (product/variant model), `docs/modules/05-inventory.md` (per-variant stock), `docs/modules/06-cart.md` and `docs/modules/07-order.md` (variant + snapshots)
- [ ] T051 [P] Write `docs/decisions/021-product-sku-model.md` superseding `020-product-variants.md`, and add the superseding note to `specs/014-product-variants/`

### Swagger annotations *(bắt buộc — feature có endpoint)*

- [ ] T052 Ensure every new/changed handler carries `@Summary`/`@Tags`/`@Param`/`@Success`/`@Failure`/`@Router` and that 014's removed paths are gone; run `make swagger` to regenerate `docs/swagger/`; `make swagger-check` must pass (Constitution VIII)

### Frontend Integration Guide *(bắt buộc — hiến pháp §Governance; feature CÓ đổi bề mặt client)*

- [ ] T053 Write `specs/015-product-sku-model/frontend-guide.md` enumerating every affected endpoint with method + path and **before → after** — at minimum: `GET /products` (one entry per product; lowest price; default image; no `type`), `GET /products/{slug}` (gains `variants[]`), `POST/PATCH/DELETE /admin/products/{id}/variants*` (new), `POST /api/v1/admin/products` (requires `variants`), `GET /api/v1/cart` and `POST /api/v1/cart/items` / `PATCH` / `DELETE /api/v1/cart/items/{variantId}` (keyed by `variantId`), the order line `variantName` (customer view) with `productCode`/`sku` stored but not shown; and mark feature 014's `/product-groups*` routes **removed**, the product-level `POST /admin/products/{id}/state` **removed** (replaced by the variant state endpoint), and the inventory admin routes now keyed by `variantId`
- [ ] T054 **Đồng bộ guide** after the source is stable: re-read `frontend-guide.md` and compare against the source **with counts** — every `json:"…"` field of the feature's DTOs, every enum value, every route (gin `:param` → `{param}`), every HTTP status. Fix every mismatch; state any part that could not be checked and why

### Phát hiện ngoài phạm vi *(hiến pháp §Governance)*

- [ ] T055 Write `specs/015-product-sku-model/deferred.md` for any out-of-scope debt found (e.g. the product→inventory availability edge, multi-dimension options carried from 014, any migration-window issue). Do not leave unchecked tasks here

### Validation

- [ ] T056 Validate `specs/015-product-sku-model/quickstart.md` end-to-end (Scenarios A–H) against a running stack and record the outcome; mark manual checks as manual
- [ ] T057 Run the full gate `make check` (format + tidy + vet + lint + unit + integration + build + swagger-check); note that `test-integration`/`test-race` need Docker/gcc and must run where available

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies
- **Foundational (Phase 2)**: depends on Setup; **blocks every user story** (the repo does not compile until the contracts and four modules are re-keyed)
- **US1 (Phase 3)** and **US2 (Phase 4)**: after Foundational; the core pair
- **US3 (Phase 5)**: after Foundational (asserts on the re-keyed inventory)
- **US4 (Phase 6)**: after Foundational (asserts on the migration/backfill)
- **Polish (Phase 7)**: after all desired stories

### Within Each Story

- Tests are written and FAIL before implementation (Constitution IV)
- Domain → adapters → use cases → handlers → integration

### Parallel Opportunities

- T001/T002 (Setup)
- Foundational: T004–T006, T008, T009, T012–T016, T019, T028 (different files)
- Within a story, the `[P]` test tasks
- Polish: T049, T050, T051

**Serialization**: migration number (`00014`) and `tasks.md` are single-writer; the contract files
(`internal/contracts/*`) must change before the modules that consume them.

---

## Parallel Example: Foundational (contracts)

```text
Task: T014 contracts/product.go — variant facts + variant lookup
Task: T015 contracts/inventory.go — variant-keyed availability/reservation
Task: T016 contracts/cart.go — CartLine with variant + snapshots
```

---

## Implementation Strategy

### MVP First (US1 + US2)

1. Complete Phase 1 and the large Phase 2 (the repo compiles on the variant model)
2. Complete US1 (public catalogue + detail + cart-by-variant) and US2 (admin product + variants)
3. Validate Scenarios A–E of `quickstart.md`
4. US3/US4 harden independence and the migration; Phase 7 closes docs/swagger/guide/gate

### Notes

- [P] tasks = different files, no dependencies
- No change to modules 01/02/03/08 or `internal/share`
- The migration (`00014`) both backfills and **drops** feature 014 — confirm the supersede direction before implementing
- Commit only when the dev explicitly authorizes it
