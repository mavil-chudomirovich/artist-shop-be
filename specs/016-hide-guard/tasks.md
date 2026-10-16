---

description: "Task list for Hide Guard with Pending Orders"
---

# Tasks: Hide Guard with Pending Orders

**Input**: Design documents from `/specs/016-hide-guard/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: Included. Every guarded transition needs one positive test (refusal names blocking orders, target byte-for-byte unchanged) and one negative test (clean hide succeeds and carts are cleaned), plus the system-initiated no-op path. Tests are written FIRST and must FAIL before implementation.

**Organization**: Tasks are grouped by user story. All three stories are P1.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to ([US1], [US2], [US3])
- Exact file paths are included in each task

## Path Conventions

- Single Go module; business modules under `internal/modules/<module>/{domain,application,infrastructure,presentation}`; cross-module contracts in `internal/contracts/`; migrations under `migrations/` as `NNNNN_<module>_<description>.sql` (next: `00016`).

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Error vocabulary shared by every story

- [x] T001 Add the three 409 codes and their domain sentinels in `internal/modules/category/domain/constant/codes.go`, `internal/modules/category/domain/error/errors.go`, `internal/modules/product/domain/constant/codes.go` and `internal/modules/product/domain/error/errors.go` (`CATEGORY_HAS_PENDING_ORDERS`, `PRODUCT_HAS_PENDING_ORDERS`, `VARIANT_HAS_PENDING_ORDERS`)
- [x] T002 [P] Cover hide-guard audit with the existing actions in `internal/modules/category/domain/constant/audit.go` and `internal/modules/product/domain/constant/audit.go` (add a hide/state-change action only where none exists, reuse otherwise and record the choice here); assert in tests that every hide emits its action, that the blocked system signal is recorded with a system actor, and that the hide event metadata carries the removed cart-line count

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Schema, cross-module contracts, and repository capabilities every story depends on

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [x] T003 Create `migrations/00016_product_visibility_flag.sql` adding `products.is_visible boolean NOT NULL DEFAULT true` (Down drops the column), with a guard test beside it following the existing `migrations/*_guard_test.go` pattern
- [x] T004 [P] Extend `internal/contracts/order.go` with the pending-orders query (`PendingOrdersForVariants(variantIDs)` returning blocking order IDs in PENDING/PAYMENT_PENDING), `internal/contracts/product.go` with the variants-by-categories read (`VariantIDsByCategories`), and `internal/contracts/cart.go` with the bulk cleanup (`RemoveVariantsFromAllCarts` returning the removed line count)
- [x] T005 Implement the pending-orders query for `internal/contracts/order.go` over `order_items` joined to `orders` in `internal/modules/order/infrastructure/implement/postgres/order.go`, plus the `FindPendingOrdersByVariants` repository method in `internal/modules/order/domain/repository/order.go`
- [x] T006 [P] Add the `VariantIDsByCategories` repository method in `internal/modules/product/domain/repository/product.go` and its single-query implementation in `internal/modules/product/infrastructure/implement/postgres/product.go`
- [x] T007 [P] Add the bulk cart-line removal by variant list in `internal/modules/cart/domain/repository/cart.go` (`DeleteLinesByVariants`) and its single-statement implementation in `internal/modules/cart/infrastructure/implement/postgres/cart.go`
- [x] T008 [P] Add the product visibility flag to the `Product` entity in `internal/modules/product/domain/model/product.go` (default visible, validated like other product fields) with its unit test alongside

**Checkpoint**: Foundation ready - user story implementation can now begin in parallel

---

## Phase 3: User Story 1 - Admin hides a category with pending orders (Priority: P1) 🎯 MVP

**Goal**: Hiding a category is refused with blocking order IDs when covered, otherwise hides and cleans carts.

**Independent Test**: Seed a category with a variant, place it in a PENDING order, attempt hide → refusal names the order and nothing changes; resolve the order → hide succeeds and cart lines are gone.

### Tests for User Story 1

> **NOTE: Write these tests FIRST, ensure they FAIL before implementation**

- [x] T009 [P] [US1] Unit test for the guarded `UpdateCategory` in `internal/modules/category/application/implement/maintenance_test.go` (refuse-with-orders vs hide-and-clean; plus a passing case seeded with PAID/SHIPPED/COMPLETED/CANCELLED orders only; assert the hide emits its audit action)
- [x] T010 [P] [US1] Integration test for the category hide journey in `internal/modules/category/presentation/http/maintenance_integration_test.go`, including a hundreds-of-variants category asserting the guard and cleanup stay bounded (fixed small statement count, no per-variant round trips — SC-004)

### Implementation for User Story 1

- [x] T011 [US1] Guard `UpdateCategory` in `internal/modules/category/application/implement/maintenance.go`: resolve variants via the product contract, query pending orders, refuse or hide-and-clean-carts inside one transaction with audit (add the `Tx`/contract dependencies to the Service struct and wire them in `cmd/api/main.go`)
- [x] T012 [US1] Map the 409 with order identifiers in `internal/modules/category/presentation/http/errors.go` (handler passthrough in `internal/modules/category/presentation/http/handler.go` only if the current mapping cannot carry it)

**Checkpoint**: At this point, User Story 1 should be fully functional and testable independently

---

## Phase 4: User Story 2 - Admin hides a variant with pending orders (Priority: P1)

**Goal**: Any move of a variant away from on-sale is guarded (operator or system); moves to on-sale never are.

**Independent Test**: Variant in PAYMENT_PENDING order → hide refused; resolve → hide succeeds and variant leaves all carts; move back on-sale never refused; system stock-out transition covered by a pending order leaves state unchanged with the event recorded.

### Tests for User Story 2

- [x] T013 [P] [US2] Unit tests for the guarded `ChangeVariantState` in a new `internal/modules/product/application/implement/variants_test.go` (operator moves checked incl. ACTIVE→OUT_OF_STOCK and ACTIVE→DISCONTINUED; move-to-active never checked; COMING_SOON→DISCONTINUED passes unguarded even with pending orders; plus a passing case with only non-pending orders; assert the hide emits its audit action and the cleanup count travels in event metadata)
- [x] T014 [P] [US2] Integration test for the variant hide journey in `internal/modules/product/presentation/http/variant_maintenance_integration_test.go`

### Implementation for User Story 2

- [x] T015 [US2] Guard `ChangeVariantState` in `internal/modules/product/application/implement/variants.go` for every move away from on-sale, with cart cleanup in the same transaction (wire the order/cart contract dependencies through the Service struct and `cmd/api/main.go`)
- [x] T016 [US2] Guard the system-initiated path: route the inventory reconcile signal through the same guarded transition so a blocked stock-out is a recorded no-op that never fails the stock write (`internal/modules/inventory/application/implement/reconcile.go` caller unchanged; product-side signal handling in `internal/modules/product/infrastructure/implement/availability/availability.go`; extend `internal/modules/inventory/application/implement/reconcile_test.go` alongside)
- [x] T017 [US2] Map the 409 with order identifiers in `internal/modules/product/presentation/http/errors.go`

**Checkpoint**: At this point, User Stories 1 AND 2 should both work independently

---

## Phase 5: User Story 3 - Admin hides a product with its own flag (Priority: P1)

**Goal**: Product flag off removes the product from the catalogue and its variants from every cart, guarded by pending orders.

**Independent Test**: Product with two variants, one in a PENDING order → hide refused; resolve → hide succeeds, product leaves catalogue, both variants leave carts.

### Tests for User Story 3

- [x] T018 [P] [US3] Unit tests for the guarded product-flag edit in `internal/modules/product/application/implement/maintenance_test.go` (plus a passing case with only non-pending orders; assert the hide emits its audit action)
- [x] T019 [P] [US3] Integration test for catalogue exclusion in `internal/modules/product/presentation/http/catalogue_sku_integration_test.go` (hidden product absent from browse, filter and direct link)

### Implementation for User Story 3

- [x] T020 [US3] Guard the product visibility-flag edit in `internal/modules/product/application/implement/maintenance.go` (check all of the product's variants, refuse or hide-and-clean in one transaction)
- [x] T021 [US3] Exclude flag-off products from the public catalogue read in `internal/modules/product/infrastructure/implement/postgres/product.go` (extend the existing visibility predicate with the flag term)

**Checkpoint**: All user stories should now be independently functional

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Contract docs, gates, and leftover decisions

- [x] T022 [P] Document the three new 409 codes and their order-ID detail in `docs/api-reference.md` with one entry per affected admin endpoint — `PATCH /api/v1/admin/categories/{categoryId}` (UpdateCategory), `POST /api/v1/admin/products/{id}/variants/{variantId}/state` (ChangeVariantState), `PATCH /api/v1/admin/products/{id}` (product flag edit) — stating when each refusal fires (Constitution VIII: error catalogue stays current)
- [x] T023 **Không đổi bề mặt client**: this feature adds no route, changes no existing route path or method, and changes no client-facing DTO or event payload — no new/changed/removed endpoint rows and no `frontend-guide.md` changes are required. (The new 409 codes on existing admin endpoints are documented in `docs/api-reference.md` by T022.)
- [x] T024 Run the full gate `make check` (format + tidy + vet + lint + unit + integration + build + swagger-check) — green on the committed tree including the card-image order fix

### Swagger annotations *(bắt buộc nếu feature có endpoint)*

**Giữ đúng MỘT trong hai nhánh dưới — nhánh còn lại thuộc về T025 ở trên, không lặp ID.**

- [x] T025 **Có endpoint thêm/đổi/xoá** về mặt annotation: thêm `@Failure` cho 3 mã 409 mới trên mọi handler liên quan (`@Summary`, `@Tags`, `@Param`, `@Success`, `@Failure`, `@Router`) rồi chạy `make swagger`; `make swagger-check` (trong `make check`) phải xanh. Thêm mã lỗi mà không thêm annotation là chưa xong (Constitution VIII).

### Phát hiện ngoài phạm vi *(hiến pháp §Governance)*

- [x] T026 **Ghi vào `specs/016-hide-guard/deferred.md`**, KHÔNG để nguyên `- [ ]` ở file này.
  Ô trống nghĩa là *việc của feature này chưa làm*; việc ngoài phạm vi thì không
  phải. Ghi rõ **là gì**, **vì sao ngoài phạm vi**, và **điều gì sẽ gỡ được** nó.
  Nếu phát hiện làm mất hiệu lực bằng chứng của một task đã tick `[X]`, phải nêu
  rõ số task đó — nếu không thì `[X]` sẽ bị đọc là không điều kiện

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories
- **User Stories (Phase 3+)**: All depend on Foundational phase completion
  - User stories can then proceed in parallel (if staffed)
  - Or sequentially in priority order (P1 → P1 → P1)
- **Polish (Final Phase)**: Depends on all desired user stories being complete

### User Story Dependencies

- **User Story 1 (P1)**: Can start after Foundational (Phase 2) - No dependencies on other stories
- **User Story 2 (P1)**: Can start after Foundational (Phase 2) - No dependencies on other stories; shares the pending-orders contract and cart cleanup with US1
- **User Story 3 (P1)**: Can start after Foundational (Phase 2) - Needs the product flag (T008) plus US1/US2's guard pattern

### Within Each User Story

- Tests (if included) MUST be written and FAIL before implementation
- Models before services
- Services before endpoints
- Core implementation before integration
- Story complete before moving to next priority

### Parallel Opportunities

- T001 and T002 (different modules' constant/error files)
- T004, T006, T007, T008 in Foundational (contracts, repos, model — different files)
- T009 with T010; T013 with T014; T018 with T019 (test pairs per story)
- T022 with T024 (docs vs gate); US1/US2/US3 can proceed in parallel after Foundational

---

## Parallel Example: User Story 1

```bash
# Launch both tests for User Story 1 together:
Task: "Unit test for guarded UpdateCategory in internal/modules/category/application/implement/maintenance_test.go"
Task: "Integration test for category hide journey in internal/modules/category/presentation/http/maintenance_integration_test.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL - blocks all stories)
3. Complete Phase 3: User Story 1
4. **STOP and VALIDATE**: Test User Story 1 independently
5. Deploy/demo if ready

### Incremental Delivery

1. Complete Setup + Foundational → Foundation ready
2. Add User Story 1 → Test independently → Deploy/Demo (MVP!)
3. Add User Story 2 → Test independently → Deploy/Demo
4. Add User Story 3 → Test independently → Deploy/Demo
5. Each story adds value without breaking previous stories

### Parallel Team Strategy

With multiple developers:

1. Team completes Setup + Foundational together
2. Once Foundational is done:
   - Developer A: User Story 1
   - Developer B: User Story 2
   - Developer C: User Story 3
3. Stories complete and integrate independently

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Each user story should be independently completable and testable
- Verify tests fail before implementing
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
- Avoid: vague tasks, same file conflicts, cross-story dependencies that break independence

---

## Phase 7: Convergence

**Purpose**: Ghi nhận khoảng trống mà đợt rà hội tụ phát hiện khi so code đã giao
với spec/plan/research/data-model/quickstart/tasks. Chỉ APPEND ở đây; không
viết lại, đánh lại số, sắp xếp lại hay xoá task cũ; không chạm spec.md hay
plan.md.

- [x] T027 **Bổ sung `specs/016-hide-guard/frontend-guide.md` cho thay đổi DTO
  `isVisible`**. Hiến pháp §Governance bắt buộc guide khi feature đổi DTO
  client-facing; code đã thêm member `isVisible` vào `UpdateProductRequest`
  (request, tuỳ chọn) và `AdminProductResponse`/`AdminProductDetailResponse`
  (response) — chính `docs/api-reference.md` (changelog 2026-10-16) cũng ghi
  "DTO chỉ thêm member `isVisible`" — nên premise "changes no client-facing
  DTO" trong plan (Constitution Check) và T023 không còn đúng với code đã giao.
  Guide phải liệt kê cả 3 endpoint chịu ảnh hưởng
  (`PATCH /api/v1/admin/categories/{categoryId}`,
  `PATCH /api/v1/admin/products/{id}`,
  `POST /api/v1/admin/products/{id}/variants/{variantId}/state`: route/method
  không đổi, mỗi endpoint thêm một mã 409 mới), đánh dấu changed, nêu before →
  after cho từng DTO đổi, và khớp `docs/api-reference.md`; đợt rà sync phải có
  đếm (số field DTO, số route, số status) theo quy tắc frontend guide sync.





