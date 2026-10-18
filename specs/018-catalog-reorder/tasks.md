---

description: "Task list for Catalog Reorder (Drag & Drop)"
---

# Tasks: Catalog Reorder (Drag & Drop)

**Input**: Design documents from `/specs/018-catalog-reorder/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/ (openapi.yaml, error-codes.md), quickstart.md

**Tests**: Included. The codebase tests every mutation, and the constitution's test-first rule applies to the ordering write and the refusal paths (each refusal leaves the stored order unchanged). Tests are written FIRST and must FAIL before implementation.

**Organization**: Tasks are grouped by user story. US1 (P1) is the reorder endpoints; US2 (P2) is auto-front placement on create; US3 (P3) is the complete-list fetch. The three stories share the same modules but touch different files/members, so a phase is serialized against the previous one.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to ([US1], [US2], [US3])
- Exact file paths are included in each task

## Path Conventions

- Single Go module; business modules under `internal/modules/<module>/{domain,application,infrastructure,presentation}`; composition root `cmd/api/main.go`. **No migration** â€” `categories.position`, `products.position`, `product_variants.position` and their ordering indexes already exist (research D12).

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Error and audit vocabulary shared by every story

- [X] T001 Add the reorder error codes and their domain sentinels: `CATEGORY_REORDER_DUPLICATE_ID` in `internal/modules/category/domain/constant/codes.go` + `ErrCategoryReorderDuplicateID` in `internal/modules/category/domain/error/errors.go`; `PRODUCT_REORDER_DUPLICATE_ID`, `VARIANT_REORDER_DUPLICATE_ID`, `VARIANT_BELONGS_TO_OTHER_PRODUCT` in `internal/modules/product/domain/constant/codes.go` + their sentinels in `internal/modules/product/domain/error/errors.go`
- [X] T002 [P] Add the reorder audit actions: `AuditCategoryReordered` in `internal/modules/category/domain/constant/audit.go`; `AuditProductReordered`, `AuditProductVariantsReordered` in `internal/modules/product/domain/constant/audit.go`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The bounded storage reads/writes every reorder story depends on

**âš ï¸ CRITICAL**: No user story work can begin until this phase is complete

- [X] T003 [P] Add the category ordering reads/writes: `Reorder(ctx, ids) (int64, error)`, `ListIDs(ctx) ([]uuid.UUID, error)`, `NextFrontPosition(ctx) (int, error)` on the port in `internal/modules/category/domain/repository/category.go` and their single-statement implementations (UNNEST / `SELECT id` / `COALESCE(MIN(position)-1,0)`) in `internal/modules/category/infrastructure/implement/postgres/category.go`
- [X] T004 [P] Add the product ordering reads/writes: `Reorder`, `ListIDs`, `NextFrontPosition` on the port in `internal/modules/product/domain/repository/product.go` and their implementations in `internal/modules/product/infrastructure/implement/postgres/product.go` (UNNEST over `products`)
- [X] T005 [P] Add the variant ordering reads/writes: `Reorder(ctx, productID, ids) (int64, error)`, `ListIDsByProduct(ctx, productID) ([]uuid.UUID, error)`, `NextFrontPosition(ctx, productID) (int, error)` on the port in `internal/modules/product/domain/repository/variant.go` and their implementations in `internal/modules/product/infrastructure/implement/postgres/variant.go` (UNNEST scoped by `product_id`)
- [X] T006 Confirm the category service's existing `Tx` port (already wired in `cmd/api/main.go` for the hide guard) is used by the auto-front create and the reorder; add no new wiring (the product service already carries `Tx`)

**Checkpoint**: Foundation ready - user story implementation can now begin

---

## Phase 3: User Story 1 - Reorder catalogue records by drag & drop (Priority: P1) ðŸŽ¯ MVP

**Goal**: Categories, products and a product's variants can each be reordered by submitting the complete, ordered id set to a dedicated endpoint, applied in one action.

**Independent Test**: Seed an ordered list; submit a full new order; the stored order equals the submitted order and the storefront renders it. Every refusal (empty/duplicate/incomplete/unknown/foreign) is rejected and the stored order is unchanged.

### Tests for User Story 1

> **NOTE: Write these tests FIRST, ensure they FAIL before implementation**

- [X] T007 [P] [US1] Unit test for `ReorderCategories` in `internal/modules/category/application/implement/maintenance_test.go`: applies the order; rejects empty (`VALIDATION_ERROR`), duplicate (dedicated code), incomplete (`VALIDATION_ERROR`) and unknown (`CATEGORY_NOT_FOUND`); asserts the audit event
- [X] T008 [P] [US1] Unit test for `ReorderProducts` and `ReorderVariants` in `internal/modules/product/application/implement/maintenance_test.go` and `internal/modules/product/application/implement/variants_test.go`: applies the order; rejects empty/duplicate/incomplete/unknown; variant not belonging to the product answers `VARIANT_BELONGS_TO_OTHER_PRODUCT`; unknown parent answers `PRODUCT_NOT_FOUND`; asserts the audit events; asserts the first variant after reorder is the default variant (FR-014)
- [X] T009 [P] [US1] Integration test for the three reorder endpoints against PostgreSQL in `internal/modules/category/presentation/http/maintenance_integration_test.go` and `internal/modules/product/presentation/http/variant_maintenance_integration_test.go` (or a new `reorder_integration_test.go` per module), proving the order is persisted with one bulk statement and the storefront order reflects it, and that the first variant after reorder is the default variant (FR-014)
- [X] T027 [P] [US1] Extend the administrator auth-boundary test `TestEveryMaintenanceRouteRefusesNoTokenAndCustomerToken` in `internal/modules/category/presentation/http/maintenance_test.go` with the new `PUT /admin/categories/reorder` route (401 without a token, 403 with a customer token, denial recorded via the OnDenied hook) â€” Constitution Â§IV
- [X] T028 [P] [US1] Extend the product auth-boundary coverage in `internal/modules/product/presentation/http/product_test.go` (e.g. a shared route table beside `TestACustomerSessionIsRefusedOnTheVariantRoutes`) to cover `PUT /admin/products/reorder` and `PUT /admin/products/{id}/variants/reorder` for both 401 (no token) and 403 (customer token) â€” Constitution Â§IV

### Implementation for User Story 1

- [X] T010 [P] [US1] Category reorder end to end: `ReorderCategoriesInput` + `ReorderResult` in `internal/modules/category/application/dto/dto.go`; `ReorderCategories` on `internal/modules/category/application/interface/ports.go`; the use case (validate duplicate/completeness/existence via `ListIDs`, then `Reorder`, one audit event with the target per research D9) in `internal/modules/category/application/implement/maintenance.go`; `ReorderCategoriesRequest` + `ReorderResponse` in `internal/modules/category/presentation/dto/dto.go` (the application DTO `ReorderResult` maps to the wire `ReorderResponse`); handler + `@Router /admin/categories/reorder [put]` annotations in `internal/modules/category/presentation/http/handler.go`; `PUT /reorder` in `internal/modules/category/presentation/http/router.go`; map `CATEGORY_REORDER_DUPLICATE_ID` (400) in `internal/modules/category/presentation/http/errors.go`
- [X] T011 [P] [US1] Product reorder end to end: `ReorderProductsInput` + `ReorderResult` in `internal/modules/product/application/dto/dto.go`; `ReorderProducts` on `internal/modules/product/application/interface/ports.go`; the use case (one audit event with the target per research D9) in `internal/modules/product/application/implement/maintenance.go`; `ReorderProductsRequest` + `ReorderResponse` in `internal/modules/product/presentation/dto/dto.go` (the application DTO `ReorderResult` maps to the wire `ReorderResponse`); handler + `@Router /admin/products/reorder [put]` annotations in `internal/modules/product/presentation/http/handler.go`; `PUT /reorder` in `internal/modules/product/presentation/http/router.go`; map `PRODUCT_REORDER_DUPLICATE_ID` in `internal/modules/product/presentation/http/errors.go`
- [X] T012 [US1] Variant reorder end to end: `ReorderVariantsInput` in `internal/modules/product/application/dto/dto.go`; `ReorderVariants` on `internal/modules/product/application/interface/ports.go`; the use case (parent existence via `Products.Exists`, then `ListIDsByProduct` foreign/incomplete check, then `Reorder`, one audit event targeting the parent product per research D9) in `internal/modules/product/application/implement/variants.go`; `ReorderVariantsRequest` in `internal/modules/product/presentation/dto/dto.go`; handler + `@Router /admin/products/{id}/variants/reorder [put]` annotations in `internal/modules/product/presentation/http/handler.go`; `PUT /{id}/variants/reorder` in `internal/modules/product/presentation/http/router.go`; map `VARIANT_REORDER_DUPLICATE_ID` and `VARIANT_BELONGS_TO_OTHER_PRODUCT` in `internal/modules/product/presentation/http/errors.go`

**Checkpoint**: US1 fully functional and testable independently

---

## Phase 4: User Story 2 - New records appear first without manual ordering (Priority: P2)

**Goal**: A record created without a `position` appears at the front of its list; an explicit `position` is still honoured.

**Independent Test**: Create a record with no `position` and confirm it is first in its list; create one with an explicit `position` and confirm that value is stored.

### Tests for User Story 2

- [X] T013 [P] [US2] Unit test for auto-front placement in `internal/modules/category/application/implement/maintenance_test.go` and `internal/modules/product/application/implement/maintenance_test.go` + `internal/modules/product/application/implement/variants_test.go`: an omitted position yields a position smaller than every existing sibling; an explicit position is honoured; a batch product create assigns payload order to omitted variants; an edit that omits position keeps the record's current order

### Implementation for User Story 2

- [X] T014 [P] [US2] Category create auto-front: change `Position` to `*int` on `CreateCategoryRequest` (`internal/modules/category/presentation/dto/dto.go`) and `CreateCategoryInput` (`internal/modules/category/application/dto/dto.go`); resolve nil to `NextFrontPosition` inside the `Tx` transaction in `CreateCategory` (`internal/modules/category/application/implement/maintenance.go`); pass `req.Position` through the handler (`internal/modules/category/presentation/http/handler.go`)
- [X] T015 [P] [US2] Product and variant create/add auto-front: change `Position` to `*int` on `CreateProductRequest`, `CreateVariantRequest` (`internal/modules/product/presentation/dto/dto.go`) and their inputs (`internal/modules/product/application/dto/dto.go`); resolve the product's nil position to `NextFrontPosition` and omitted batch-variant positions to payload index in `CreateProduct`, and the added variant's nil position to `NextFrontPosition` in `AddVariant` (`internal/modules/product/application/implement/maintenance.go`, `internal/modules/product/application/implement/variants.go`); pass `req.Position` through the handler (`internal/modules/product/presentation/http/handler.go`)
- [X] T016 [US2] Update existing tests and seed that construct the create DTOs with an `int` `Position` to the new `*int` shape across `internal/modules/category/...`, `internal/modules/product/...` and `cmd/seed/` so the suite compiles

**Checkpoint**: US1 and US2 both work independently

---

## Phase 5: User Story 3 - Reorder across the whole list (Priority: P3)

**Goal**: An administrator can fetch the complete ordered list (beyond one page) to reorder it as a whole, bounded by a documented safety maximum.

**Independent Test**: With more records than one page, `?all=true` returns the whole ordered list; a list over the maximum is refused with a clear error while normal paging still works.

### Tests for User Story 3

- [X] T017 [P] [US3] Test the `all` selector and its bound: handler test asserting `all=true` skips paging and a size over the cap answers `VALIDATION_ERROR`, in `internal/modules/category/presentation/http/maintenance_test.go` and `internal/modules/product/presentation/http/product_test.go` (plus an integration case in the module integration suites)

### Implementation for User Story 3

- [X] T018 [P] [US3] Category `all` selector: add `All` to `ListAdminInput` (`internal/modules/category/application/dto/dto.go`); in `ListAdmin` (`internal/modules/category/application/implement/maintenance.go`) declare `const maxAdminListAll = 1000` and, when `All`, reuse the existing paged repository read with `pageSize = maxAdminListAll+1`, returning a field error on `all` when `total` exceeds the cap (no repository-interface change); parse `all` in `presentation/http/handler.go` and add its `@Param` annotation
- [X] T019 [P] [US3] Product `all` selector: add `All` to `ListAdminInput` (`internal/modules/product/application/dto/dto.go`); in `ListAdmin` (`internal/modules/product/application/implement/maintenance.go`) declare `const maxAdminListAll = 1000` and, when `All`, reuse the existing paged repository read with `pageSize = maxAdminListAll+1`, returning a field error on `all` when `total` exceeds the cap (no repository-interface change); parse `all` in `presentation/http/handler.go` and add its `@Param` annotation

**Checkpoint**: All three stories independently functional

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Contract docs, gates, and the feature hand-off

- [X] T020 [P] Update `docs/api-reference.md`: the three new reorder endpoints (method, path, auth, body, `updatedCount` response, error codes), the `all` selector on the two admin lists with its cap, the now-optional `position` on the create bodies (before â†’ after), and the four new error codes in the catalogue
- [X] T021 [P] Update `docs/modules.md` (and any ordering note it carries) to describe the drag & drop reorder and auto-front behaviour
- [X] T022 Run `make swagger` to regenerate `docs/swagger/` for the added/changed handlers, then confirm `make swagger-check` passes
- [X] T023 Run `make check` once at close-out (format + tidy + vet + lint + unit + race + integration + build + swagger-check) on the committed tree

### Frontend Integration Guide *(báº¯t buá»™c â€” hiáº¿n phÃ¡p Â§Governance)*

**CÃ³ Ä‘á»•i bá» máº·t client**: 3 route thÃªm má»›i vÃ  5 route Ä‘á»•i (xem `specs/018-catalog-reorder/frontend-guide.md`).

- [X] T024 **Äá»“ng bá»™** `specs/018-catalog-reorder/frontend-guide.md` á»Ÿ phase cuá»‘i SAU khi mÃ£ nguá»“n á»•n Ä‘á»‹nh: Ä‘á»c láº¡i guide vÃ  Ä‘á»‘i chiáº¿u vá»›i mÃ£ nguá»“n, **cÃ³ Ä‘áº¿m sá»‘** â€” má»i tháº» `json` cá»§a cÃ¡c DTO mÃ  feature cháº¡m, má»i giÃ¡ trá»‹ enum guide nÃªu, má»i route guide liá»‡t kÃª (Ä‘á»‘i chiáº¿u `@Router`), má»i HTTP status guide kháº³ng Ä‘á»‹nh. Sá»­a má»i chá»— lá»‡ch. Náº¿u khÃ´ng Ä‘á»‘i chiáº¿u Ä‘Æ°á»£c má»™t pháº§n, ghi rÃµ pháº§n nÃ o chÆ°a kiá»ƒm vÃ  vÃ¬ sao.

### Swagger annotations *(báº¯t buá»™c â€” feature cÃ³ endpoint thÃªm/Ä‘á»•i)*

- [X] T025 **CÃ³ endpoint thÃªm/Ä‘á»•i/xoÃ¡** â†’ thÃªm annotation Swagger cho **má»i** handler liÃªn quan (`@Summary`, `@Tags`, `@Param`, `@Success`, `@Failure`, `@Router`) rá»“i cháº¡y `make swagger`; `make swagger-check` (trong `make check`) pháº£i xanh. Endpoint khÃ´ng annotation coi nhÆ° chÆ°a xong (Constitution VIII).

### PhÃ¡t hiá»‡n ngoÃ i pháº¡m vi *(hiáº¿n phÃ¡p Â§Governance)*

- [X] T026 **Ghi vÃ o `specs/018-catalog-reorder/deferred.md`**, KHÃ”NG Ä‘á»ƒ nguyÃªn `- [ ]` á»Ÿ file nÃ y. Ã” trá»‘ng nghÄ©a lÃ  *viá»‡c cá»§a feature nÃ y chÆ°a lÃ m*; viá»‡c ngoÃ i pháº¡m vi thÃ¬ khÃ´ng pháº£i. Ghi rÃµ **lÃ  gÃ¬**, **vÃ¬ sao ngoÃ i pháº¡m vi**, vÃ  **Ä‘iá»u gÃ¬ sáº½ gá»¡ Ä‘Æ°á»£c** nÃ³. Náº¿u phÃ¡t hiá»‡n lÃ m máº¥t hiá»‡u lá»±c báº±ng chá»©ng cá»§a má»™t task Ä‘Ã£ tick `[X]`, pháº£i nÃªu rÃµ sá»‘ task Ä‘Ã³.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup - BLOCKS all user stories
- **User Stories (Phase 3â€“5)**: Each depends on Foundational. They are serialized (Phases 3 â†’ 4 â†’ 5) because US2 and US3 edit the same DTO/handler files US1 creates.
- **Polish (Phase 6)**: Depends on all desired user stories being complete

### User Story Dependencies

- **US1 (P1)**: Starts after Foundational. No dependency on US2/US3.
- **US2 (P2)**: Starts after Foundational. Independent of US1 in behaviour; edits `application/dto/dto.go` and `presentation/dto/dto.go` after US1, so it runs after Phase 3.
- **US3 (P3)**: Starts after Foundational. Independent; edits the admin-list handlers after US1/US2, so it runs last.

### Within Each User Story

- Tests MUST be written and FAIL before implementation
- Repository/DTO shapes before use cases
- Use cases before handlers/routes
- Core implementation before integration

### Parallel Opportunities

- T002 with T001; T003/T004/T005 in Foundational (different files)
- T007, T008, T009, T027, T028 (US1 tests across modules)
- T010 and T011 (different modules); T012 after T011 (same product files)
- T013 with nothing in-phase until T014/T015 (different modules) can start
- T018 and T019 (different modules)
- T020 and T021 (docs)

---

## Parallel Example: User Story 1

```bash
# Launch the US1 test tasks together:
Task: "Unit test ReorderCategories in internal/modules/category/application/implement/maintenance_test.go"
Task: "Unit test ReorderProducts/ReorderVariants in internal/modules/product/application/implement/maintenance_test.go + variants_test.go"
Task: "Integration test for the reorder endpoints in the two modules' http integration suites"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL - blocks all stories)
3. Complete Phase 3: User Story 1
4. **STOP and VALIDATE**: reorder each list independently against PostgreSQL
5. Deploy/demo if ready

### Incremental Delivery

1. Setup + Foundational â†’ foundation ready
2. US1 â†’ reorder works (MVP)
3. US2 â†’ new records appear first
4. US3 â†’ whole-list fetch for large catalogues
5. Polish â†’ docs, swagger, gates, frontend guide

---

## Notes

- [P] tasks = different files, no dependencies
- No migration: the `position` columns and ordering indexes already exist (research D12)
- Reorder is one bulk statement per list; auto-front is one read + one insert (research D1, D3)
- Each user story is independently testable; verify tests fail before implementing
- Commit after each phase, once the phase gate is green

---

## Phase 7: Convergence

- [X] T029 [US1] Run the bulk reorder write of all three use cases inside the existing `UnitOfWork` transaction (the same nil-tolerant `Tx` pattern `createCategoryAtFront`/`CreateProduct` already use, so a construction without the port still resolves directly): `ReorderCategories` in `internal/modules/category/application/implement/maintenance.go`, and `ReorderProducts` in `internal/modules/product/application/implement/maintenance.go` plus `ReorderVariants` in `internal/modules/product/application/implement/variants.go`, so that the defensive `updated != int64(len(ids))` refusal (which today returns after the single-statement `UPDATE` has already autocommitted) rolls the write back instead of leaving a partially applied order — matching plan.md ("reorder and the auto-front create each run in a single transaction through the `UnitOfWork` port"), T006 and FR-004 (partial)
