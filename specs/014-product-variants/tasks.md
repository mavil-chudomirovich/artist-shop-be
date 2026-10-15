---
description: "Task list for Product Variants (variant groups + options)"
---

# Tasks: Product Variants

**Input**: Design documents from `/specs/014-product-variants/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Included. Constitution IV mandates test-first for pricing/state/inventory, and the spec's
Success Criteria require automated tests for the merged list, option independence, storage-enforced
invariants, and the not-found consistency (SC-001–SC-006). No sell-state or money rule changes, so
the existing catalogue/cart/order/inventory suites must stay green unchanged (SC-005).

**Organization**: Tasks are grouped by user story. **US1 (customer picks & buys) and US2 (operator
maintains groups) are the core and ship together** — one without the other shows nothing; US3
(option independence) and US4 (group presence) are guards on the same read.

**Only module 04 (product) changes** — plus `migrations/00013`, docs and swagger. No change to
`internal/contracts/` or modules 05–08.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1–US4)
- Exact file paths are included in each task

## Path Conventions

- Single Go module; module code under `internal/modules/product/`; migrations under `migrations/`.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: The constants and sentinels the whole feature maps to

- [x] T001 [P] Add variant-group audit action constants in `internal/modules/product/domain/constant/audit.go` — `AuditVariantGroupCreated`, `AuditVariantGroupUpdated`, `AuditVariantGroupDeleted`, `AuditVariantGroupImageAdded`, `AuditVariantGroupImageRemoved`, `AuditVariantGroupImagePrimarySet`, `AuditProductVariantAttached`, `AuditProductVariantDetached` (values `PRODUCT_VARIANT_GROUP_*` / `PRODUCT_VARIANT_*`) — FR-014
- [x] T002 [P] Add the domain sentinels in `internal/modules/product/domain/error/errors.go` — `ErrVariantGroupNotFound`, `ErrVariantGroupSlugTaken`, `ErrVariantGroupHasOptions`, `ErrVariantLabelTaken`, `ErrProductAlreadyGrouped` — **and** the matching error-code constants in `internal/modules/product/domain/constant/codes.go` (`CodeVariantGroupNotFound`, `CodeVariantGroupSlugTaken`, `CodeVariantGroupHasOptions`, `CodeVariantLabelTaken`, `CodeProductAlreadyInVariantGroup`), mirroring `contracts/error-codes.md` one-for-one

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The migration, the domain entities/ports, the Postgres adapters and the wiring every story needs

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [x] T003 Write the migration in `migrations/00013_product_variants.sql` per `data-model.md`: create `product_variant_groups` and `product_variant_group_images` (with every constraint/index); `ALTER TABLE products ADD COLUMN variant_group_id uuid, ADD COLUMN variant_label text` + the FK (RESTRICT), the `(variant_group_id IS NULL) = (variant_label IS NULL)` check, the `NOT (is_set AND variant_group_id IS NOT NULL)` check, the partial unique index on `(variant_group_id, variant_label)` and the `variant_group_id` index; both `-- +goose Up` and `-- +goose Down`
- [x] T004 [P] Add the migration guard test in `migrations/product_variants_guard_test.go` mirroring `product_guard_test.go`: every constraint/index present Up and dropped Down; the FK is `ON DELETE RESTRICT`; the one-primary partial index; the slug-format regex; the set↔option check; the two new columns
- [x] T005 [P] Create the `VariantGroup` entity in `internal/modules/product/domain/model/variant_group.go` — fields per `data-model.md`, `NewVariantGroup(VariantGroupDraft, now)` and `Apply(VariantGroupEdit, now)`, validation reusing the module's existing name/slug/description rules (120/140/5000, slug pattern) plus a non-blank `option_name`
- [x] T006 [P] Write the domain test in `internal/modules/product/domain/model/variant_group_test.go` — valid create/edit; blank name/option_name, bad slug, over-bound values rejected with the field named (FR-009, FR-019, FR-020)
- [x] T007 Extend `Product` in `internal/modules/product/domain/model/product.go` — add `VariantGroupID *uuid.UUID` and `VariantLabel *string`; add them to `ProductDraft`/`ProductEdit`; validate "label required iff group set", trim, and refuse a set product becoming an option (and vice versa) naming the field (FR-010, FR-021, FR-024)
- [x] T008 [P] Extend `internal/modules/product/domain/model/product_test.go` — group+label attach/detach, label-without-group and group-without-label rejected, set↔option rejected, existing product rules unchanged
- [x] T009 Define the `VariantGroupRepository` port and its read views in `internal/modules/product/domain/repository/variant_group.go` (create/update/delete/find-by-id/find-by-slug/list-all/list images/add/remove/set-primary/count images/list options); extend `internal/modules/product/domain/repository/product.go` with `ListCatalogue` (merged page + total), `ListGroupOptions`, and a bulk option read for the detail selector — every method documented against its FR
- [x] T010 Implement the `VariantGroupRepository` adapter in `internal/modules/product/infrastructure/implement/postgres/variant_group.go` (projection + scan, Create/Update/Delete/Find*/List*/image ops; constraint-name classification for `product_variant_groups_slug_key`, `product_variant_groups_category_fk`, `products_variant_label_key`, `products_variant_group_fk`, `products_set_not_variant_ck`)
- [x] T011 Extend the product adapter `internal/modules/product/infrastructure/implement/postgres/product.go` — add the two columns to `productColumns` and `scanProduct`, write them in `Create`/`Update`, and implement `ListCatalogue` as the single CTE/UNION ALL of D4 (groups visible iff category visible AND EXISTS a visible option; standalone = `variant_group_id IS NULL`; group price = `MIN` visible-option price; group image = group primary image; order `position, created_at, id`; `COUNT(*)` over the same CTE) and `ListGroupOptions`
- [x] T012 [P] Add Postgres integration tests in `internal/modules/product/infrastructure/implement/postgres/product_variants_integration_test.go` — the merged list is one page with a correct total across both kinds; group price = lowest visible option; label uniqueness / one-group-per-product / set↔option enforced at storage; RESTRICT refusal on group delete; slug unique within each namespace
- [x] T013 Extend `internal/modules/product/application/interface/ports.go` — alias `VariantGroupRepository = repository.VariantGroupRepository` (so the composition root can name the new port and the build stays green). The group/catalogue methods are added to `ProductService` in the story that implements them (T022 for the public reads, T029 for the group surface), so the interface never declares a method the `Service` does not yet have; the module keeps one `Service` the handler consumes
- [x] T014 [P] Add the application DTOs in `internal/modules/product/application/dto/dto.go` — the public catalogue entry (with a `type` discriminator: `"product"` | `"variant-group"`), the public group detail + option, the admin group list/detail, and the group create/edit inputs; extend `CreateProductInput`/`UpdateProductInput` with tri-state attachment — `VariantGroupID *uuid.UUID` + `VariantGroupSet bool`, and the same pair for the label — so "omitted = keep current", "present and null = detach", "present and non-null = attach/move" are distinguishable (research D13)
- [x] T015 Extend `internal/modules/product/application/mapper/mapper.go` — map group entities/views and the option views to the new DTOs; keep the public/admin split so no administrator field leaks
- [x] T016 Wire the new repository and service in the composition root `cmd/api/main.go` (construct `postgres.NewVariantGroupRepository(db.Pool)` and pass it to the product service) and add the field to `Service` in `internal/modules/product/application/implement/catalogue.go`
- [x] T017 [P] Create the shared in-memory `VariantGroupRepository` fake in `internal/modules/product/application/implement/fakes_test.go` (new file; the existing use-case tests declare their own product fakes inside their test files)

**Checkpoint**: Schema, domain, adapters and wiring ready — the user stories can begin

---

## Phase 3: User Story 1 - A customer picks a variant and buys it (Priority: P1) 🎯 MVP

**Goal**: The catalogue shows a group once; opening it shows the shared content and its options; a chosen option is bought as an ordinary product.

**Independent Test**: Seed a group with two options (via the repository), `GET /products` lists the group once and not the options; `GET /product-groups/{slug}` returns both options with label/price/picture; `GET /products/{option-slug}` returns the selector; adding the option to the cart holds it at its own price.

### Tests for User Story 1

- [ ] T018 [P] [US1] Write failing unit tests for `ListCatalogue` in `internal/modules/product/application/implement/catalogue_test.go` — a group with a visible option appears once as `type: "variant-group"`, its options do not appear as separate entries; a standalone product appears as `type: "product"`
- [ ] T019 [P] [US1] Write failing unit tests for the group detail and the product-detail selector in `catalogue_test.go` — group detail returns shared content + ordered options; product detail of an option carries `variantGroup`; a group with no visible option answers not-found

### Implementation for User Story 1

- [ ] T020 [US1] Implement `ListCatalogue` in `internal/modules/product/application/implement/catalogue.go` (visibility set resolved once per request, passed to the repository so the predicate runs in SQL — FR-001, FR-006, FR-027)
- [ ] T021 [US1] Implement `GetPublicGroupBySlug` and extend `GetPublicBySlug` (attach the `variantGroup` selector when the product is an option) in `catalogue.go` (FR-002, FR-005, FR-007, FR-008)
- [ ] T022 [US1] Add/extend the handlers in `internal/modules/product/presentation/http/handler.go` — `ListPublic` returns the merged entry shape, a new `GetPublicGroupBySlug`, `GetPublicBySlug` gains the selector; each with Swagger annotations; add the matching methods to `ProductService` in `internal/modules/product/application/interface/ports.go`
- [ ] T023 [US1] Add the public route `GET /product-groups/{slug}` in `internal/modules/product/presentation/http/router.go`; add the HTTP request/response DTOs in `internal/modules/product/presentation/dto/dto.go`; map the new not-found in `internal/modules/product/presentation/http/errors.go`
- [ ] T024 [P] [US1] Write the integration test in `internal/modules/product/presentation/http/catalogue_variants_integration_test.go` (new file) — merged list, group detail, direct option link, a customer adding the option to the cart at its own price, and the FR-004 negative case: adding an off-sale option is refused (`409`) while its on-sale sibling still adds

**Checkpoint**: US1 works against the repository-seeded data; addable to cart with no cart/order change

---

## Phase 4: User Story 2 - An operator maintains variant groups (Priority: P1) 🎯 MVP

**Goal**: An administrator creates a group, attaches/detaches/labels options, edits and removes groups, and manages the group's picture gallery.

**Independent Test**: Create a group, attach two products as labelled options, see them on the public group, detach one, remove the group (after detaching), and confirm the products survive; a customer session is refused on every group route.

### Tests for User Story 2

- [ ] T025 [P] [US2] Write failing unit tests for the group use cases in `internal/modules/product/application/implement/groups_test.go` — create/edit/delete, slug collision, **create/edit with a category that does not exist refused naming the field (FR-023)**, delete-with-options refused, duplicate label refused, attach a product already grouped refused, group picture ceiling/primary, and **each mutation emitting its audit event (FR-014, SC-006)** (FR-009–FR-015, FR-021, FR-023)
- [ ] T026 [P] [US2] Extend the failing tests in `internal/modules/product/application/implement/maintenance_test.go` — product create/edit with `variantGroupId`+`variantLabel` attaches; clearing both detaches; attaching does not touch price/pictures/state/link; set↔option refused

### Implementation for User Story 2

- [ ] T027 [US2] Implement the group use cases in `internal/modules/product/application/implement/groups.go` — create/list/get/update/delete and the group picture ops (reusing the module's media + UnitOfWork picture pattern), each emitting its audit event naming the group and the acting administrator, with no PII (FR-009–FR-015, FR-018, SC-006)
- [ ] T028 [US2] Extend `internal/modules/product/application/implement/maintenance.go` — `CreateProduct`/`UpdateProduct` read `VariantGroupID`/`VariantLabel` and persist the attachment (no change to price/pictures/state/link); audit `PRODUCT_VARIANT_ATTACHED`/`DETACHED`
- [ ] T029 [US2] Add the group handlers in `internal/modules/product/presentation/http/handler.go` (list/create/get/update/delete + image add/remove/primary) with Swagger annotations; add the request/response DTOs in `internal/modules/product/presentation/dto/dto.go`; add the group methods to `ProductService` in `internal/modules/product/application/interface/ports.go`
- [ ] T030 [US2] Add the admin group routes in `internal/modules/product/presentation/http/router.go` (`/admin/product-groups` and `/{id}`, `/{id}/images`, `/{id}/images/{imageId}`, `/{id}/images/{imageId}/primary`) behind the ADMIN guard
- [ ] T031 [US2] Map the new group error codes to their HTTP responses in `internal/modules/product/presentation/http/errors.go` (`VARIANT_GROUP_NOT_FOUND` 404, `VARIANT_GROUP_SLUG_TAKEN` 409, `VARIANT_GROUP_HAS_OPTIONS` 409, `VARIANT_LABEL_TAKEN` 409, `PRODUCT_ALREADY_IN_VARIANT_GROUP` 409) and add the error-code test in `presentation/http/errors_test.go`
- [ ] T032 [P] [US2] Write the integration test in `internal/modules/product/presentation/http/product_groups_integration_test.go` (new file) — full admin group CRUD + attach/detach + images, the role refusal on a customer session, and an audit entry recorded per mutation (SC-006)

**Checkpoint**: US1 + US2 together deliver the whole feature against real HTTP

---

## Phase 5: User Story 3 - Each option is its own sellable unit (Priority: P2)

**Goal**: Options carry independent price, stock, sell state and pictures; nothing is derived from siblings.

**Independent Test**: Give two options different prices and stock; each shows and charges its own; a stock-out option is refused while the sibling is buyable.

### Tests for User Story 3

- [ ] T033 [P] [US3] Write the integration test in `internal/modules/product/infrastructure/implement/postgres/product_variants_integration_test.go` — two options with different prices/stock/images keep their own through the group read and the product read; the group card price is the lowest visible option's (FR-016, FR-017, FR-018; SC-003)

### Implementation for User Story 3

- [ ] T034 [US3] Implement/confirm in `internal/modules/product/infrastructure/implement/postgres/product.go` and `internal/modules/product/application/mapper/mapper.go` that each option's projection returns its **own** `price_amount`/`currency`, `is_preorder`, primary `image_url`, `slug` and (admin) `sell_state`/`position`/`name` — nothing inherited from the group or siblings; T033 is the test that proves it

**Checkpoint**: Option independence holds end-to-end; the cart/order/inventory suites are unchanged

---

## Phase 6: User Story 4 - A group's presence follows its choices (Priority: P3)

**Goal**: A group with no visible option is unreachable; it returns as soon as one option is visible.

**Independent Test**: Hide/remove every option → the group leaves the list and its link answers not-found; make one option visible → the group reappears once.

### Tests for User Story 4

- [ ] T035 [P] [US4] Write failing unit tests in `catalogue_test.go` — a group with zero visible options is absent from the list and answers the same not-found as an unknown slug (FR-006, FR-008)
- [ ] T036 [P] [US4] Write the integration test in `product_variants_integration_test.go` — hiding/removing all options removes the group (and the total drops by one); a grouped product never appears on the standalone side (FR-001, FR-006)

### Implementation for User Story 4

- [ ] T037 [US4] Implement/confirm in `internal/modules/product/infrastructure/implement/postgres/product.go` that the group-visibility predicate (`category_id = ANY(visible) AND EXISTS(<visible option>)`) and the `variant_group_id IS NULL` exclusion on the standalone side match `data-model.md` D7/D4; T036 is the test that proves the total drops by one when a group's last option leaves

**Checkpoint**: All four stories work independently; the list total is always correct

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Contract docs, ADR, swagger, the frontend hand-off and the full gate

- [ ] T038 [P] Update `docs/api-reference.md` §6 — the changed `GET /products` (the `type` discriminator), the new `GET /product-groups/{slug}`, the changed product detail (`variantGroup`), the new admin group endpoints, the changed product create/edit bodies, the new error codes, the summary table and the change log (Constitution VIII)
- [ ] T039 [P] Update `docs/modules/04-product.md` — move "biến thể một chiều" out of "Không (hoãn)" into delivered scope, and point to `specs/014-product-variants/deferred.md` for what stays deferred
- [ ] T040 [P] Write the ADR `docs/decisions/020-product-variants.md` — a group is its own table; an option is an ordinary product (so no cross-module contract changes); the merged catalogue list; the two slug namespaces; group-removal RESTRICT

### Swagger annotations *(bắt buộc — feature có endpoint)*

- [ ] T041 Ensure every new/changed handler in `internal/modules/product/presentation/http/handler.go` carries `@Summary`, `@Tags`, `@Param`, `@Success`, `@Failure`, `@Router`, then run `make swagger` to regenerate `docs/swagger/`; `make swagger-check` must pass (Constitution VIII)

### Frontend Integration Guide *(bắt buộc — hiến pháp §Governance; feature CÓ đổi bề mặt client)*

- [ ] T042 Write `specs/014-product-variants/frontend-guide.md` enumerating every affected endpoint with method and path, marked **new/changed**, with before → after for each changed one — at minimum: `GET /products` (items gain `type`; grouped products leave as their group), `GET /products/{slug}` (gains `variantGroup`), `GET /product-groups/{slug}` (new), `POST/PATCH /admin/products` (gain `variantGroupId`/`variantLabel`), the new `/admin/product-groups*` endpoints, and the new error codes
- [ ] T043 **Đồng bộ guide** after the source is stable: re-read `frontend-guide.md` and compare against the source **with counts** — every DTO JSON field, every enum value, every listed route, every HTTP status. Fix every drift; state any part that could not be checked and why

### Phát hiện ngoài phạm vi *(hiến pháp §Governance)*

- [ ] T044 Write `specs/014-product-variants/deferred.md` for any out-of-scope debt found while building (per-option availability on the group page, multi-dimension options, option reordering beyond `position`, a shared-slug registry if ever wanted) — what it is, why out of scope, what would unblock it. Do not leave unchecked tasks here

### Validation

- [ ] T045 Validate `specs/014-product-variants/quickstart.md` end-to-end (Scenarios A–G) against the running stack and record the outcome; mark the manual checks (e.g. the 404 consistency) as manual, not automated
- [ ] T046 [P] Correct module 03's now-stale assumption: the `classifyDeleteError` comment in `internal/modules/category/infrastructure/implement/postgres/category.go` ("products_category_fk is the only foreign key that points at categories") is no longer true once `product_variant_groups_category_fk` exists; update the comment (behaviour is unchanged — the SQLSTATE-based mapping already covers any category FK) and add the one-line note to `docs/modules/03-category.md`; run `make fmt-check` after (comment-only change)
- [ ] T047 Run the full gate `make check` (format + tidy + vet + lint + unit + integration + build + swagger-check); confirm the existing catalogue/cart/order/inventory suites pass unchanged (SC-005)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies
- **Foundational (Phase 2)**: depends on Setup; **blocks all user stories**
- **US1 (Phase 3)** and **US2 (Phase 4)**: after Foundational; the core pair — US1 reads what US2 writes
- **US3 (Phase 5)**: after US1 (it asserts on the reads US1 delivers)
- **US4 (Phase 6)**: after US1 (it guards the same list)
- **Polish (Phase 7)**: after all desired stories

### Within Each Story

- Tests are written and FAIL before implementation (Constitution IV)
- Domain/adapter before use case; use case before handler/route
- Core implementation before integration test

### Parallel Opportunities

- T001, T002 (Setup)
- T004, T005, T006, T008, T012, T014, T017 in Foundational (different files)
- All `[P]` test tasks within a story (T018/T019, T025/T026, T035/T036)
- T038, T039, T040 in Polish (different docs)

---

## Parallel Example: Foundational

```text
Task: T004 migration guard test in migrations/product_variants_guard_test.go
Task: T005 VariantGroup entity in internal/modules/product/domain/model/variant_group.go
Task: T006 domain test in internal/modules/product/domain/model/variant_group_test.go
Task: T014 application DTOs in internal/modules/product/application/dto/dto.go
Task: T017 variant-group fake repository for use-case tests
```

---

## Implementation Strategy

### MVP First (US1 + US2)

1. Complete Phase 1 and Phase 2
2. Complete US1 (public catalogue + group detail + selector) and US2 (admin groups + attach/detach)
3. **STOP and VALIDATE**: seed a group with options and run the Scenario A–B of `quickstart.md`
4. US3 and US4 harden independence and presence; Phase 7 closes docs, swagger, guide and the gate

### Notes

- [P] tasks = different files, no dependencies
- No change to `internal/contracts/`, modules 05–08, or any shared package
- Commit only when the dev explicitly authorizes it
