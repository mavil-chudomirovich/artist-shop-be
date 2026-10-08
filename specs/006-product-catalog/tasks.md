---

description: "Task list for feature 006-product-catalog"
---

# Tasks: Product Catalogue

**Input**: Design documents from `specs/006-product-catalog/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Included. The constitution mandates test-first coverage for state transitions and
invariants, and this module's substance *is* invariants — the sell-state transition table, the
price rule, the slug rule, and two guarantees a fake cannot prove: the restricting foreign key and
the single-main-picture index. Each test task is written first and must fail before its
implementation task runs.

**Organization**: One phase per user story, in the priority order the spec gives. US1 and US2 are
both P1 and ship together; US3 through US6 each add a capability on top. The dependencies between
them are stated rather than hidden, because several of these stories cannot be exercised before
another one's endpoints exist.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1…US6)
- Include exact file paths in descriptions

## Path Conventions

Go web service inside a modular monolith, following `plan.md`:

- `internal/modules/product/` — the new module, four layers
- `internal/share/media/` — the image capability, moved here from module 02
- `internal/contracts/` — the cross-module interface this feature introduces
- `internal/modules/user/`, `internal/modules/category/` — touched, each in a small and stated way
- `migrations/` — the additive schema change
- `cmd/api/main.go` — composition
- `docs/` — documentation that must change with the code (Constitution VIII)

---

## Phase 1: Setup (Baseline)

**Purpose**: Establish a green baseline so a later failure is attributable to this feature. This
feature adds no project scaffolding — the module skeleton is Phase 2's work.

- [x] T001 Confirm the baseline is green and the migration number is free: run `make lint`, `make test` and `make test-integration` (all must exit 0, and the integration run must be proven to execute rather than skip), and confirm `migrations/` ends at `00005_category.sql` so the new migration is `00006`

**Checkpoint**: The tree is green and the next migration number is known.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The shared image capability, the cross-module contract, the three tables, the entity,
its rules, the ports and the adapters — everything all six user stories stand on.

**⚠️ CRITICAL**: No user story work begins until this phase is complete. US1 and US2 both need the repository (T020) and the storage guarantees (T022); every story needs the entity and its rules.

### Move the shared image capability (research D2)

- [x] T002 Move the media port and adapter into `internal/share/media/`: create `media.go` holding the `Store` interface and the `Reference` type, and `cloudinary.go` holding the adapter, both moved verbatim from `internal/modules/user/application/interface/ports.go` and `internal/modules/user/infrastructure/implement/media/cloudinary.go`. Behaviour MUST NOT change — same signatures, same multipart upload, same sanitised failure classification
- [x] T003 Move the adapter's unit tests to `internal/share/media/cloudinary_test.go` and update module 02 to consume the shared port: remove the old files, point `internal/modules/user/application/implement/profile.go` and `internal/modules/user/infrastructure/implement/media/` at `internal/share/media`, and rewire the composition in `cmd/api/main.go`. **Evidence the move is a move and not a rewrite: module 02's existing tests in `internal/modules/user/presentation/http/avatar_test.go` and `internal/modules/user/application/implement/avatar_test.go` must pass unchanged**
- [x] T004 [P] Update the imports in `internal/modules/user/application/interface/ports.go` so the media port is no longer declared there, and confirm `go build ./...` and `make lint` are clean. Nothing else in module 02 changes

### The cross-module contract (research D1)

- [x] T005 [P] Declare the visibility contract in `internal/contracts/category.go`: a `CategoryVisibility` interface with `VisibleCategoryIDs(ctx context.Context) ([]uuid.UUID, error)`, documented as the one question module 04 cannot answer itself and the reason it is a set rather than a per-identifier check (research D1)
- [x] T006 Provide the contract's adapter from module 03 in `internal/modules/category/presentation/` or `internal/modules/category/infrastructure/implement/`, whichever keeps module 03's layering intact, returning the identifiers of every category whose `is_visible` is true. Add its unit test
- [x] T007 [P] Add the compile-time assertion that the adapter satisfies `contracts.CategoryVisibility` **in the adapter's own package** — `var _ contracts.CategoryVisibility = (*Adapter)(nil)` beside the implementation — so the two cannot drift apart without the build failing. It MUST NOT live in `internal/contracts`: that package is dependency-free by design and must not import a module, which is why `internal/contracts/user.go` declares the interface and module 02 satisfies it rather than the other way round
- [x] T007b Extend the contract to carry the second question the public filter needs: rename `contracts.CategoryVisibility` to `contracts.CategoryQuery` and add `CategoryIDBySlug(ctx context.Context, slug string) (uuid.UUID, bool, error)` beside `VisibleCategoryIDs`, in `internal/contracts/category.go`; add the matching method to module 03's adapter in `internal/modules/category/infrastructure/implement/visibility/` with its unit test; update the compile-time assertion. **Added after T005–T007 landed**, because FR-006's `?category=<slug>` filter cannot be built from a set of identifiers alone and module 04 may not read module 03's table (research D1). The found-flag rather than a not-found error is deliberate: a hidden slug, an unknown slug and an empty category must all answer the same empty list, and a hidden slug is excluded by the visibility predicate anyway

### Schema

- [x] T008 Write `migrations/00006_product.sql` following `docs/development/migration.md`: the three tables `data-model.md` specifies with every column, the unique index on `products.normalized_slug` (FR-027), the normalised-shape and slug-format checks (FR-028), the three length checks (FR-029), the price, currency, sell-state and two pre-order checks (FR-030, FR-031, FR-039), **the restricting foreign key `products.category_id → categories(id) ON DELETE RESTRICT`** (FR-034, FR-035, research D15), the cascading foreign keys on `product_images` and `product_set_items`, the **partial unique index that keeps one main picture per product** (FR-016), the set-membership primary key and self-reference check (FR-038), the ordering and category indexes (FR-006, FR-009), `COMMENT ON` for the columns whose purpose is not obvious, and both `-- +goose Up` and `-- +goose Down` sections
- [x] T009 Add `migrations/product_guard_test.go` asserting the migration text carries every constraint **and** drops it in the down section — matching `migrations/category_guard_test.go`, because the runner rejects a one-way migration. It must assert the restricting foreign key is present, since that is the constraint the whole of US4 rests on, and that it carries **no stock or quantity column**, which is the negative half of FR-038 — this feature tracks no stock, and a guard is what keeps that from being quietly undone later

### Domain

- [x] T010 [P] Create the module skeleton under `internal/modules/product/` with the four layers from `plan.md` — `domain/{constant,error,model,repository}`, `application/{dto,interface,implement,mapper}`, `infrastructure/implement/{postgres,auditor}`, `presentation/{dto,http}` — each package carrying a `// Package …` comment (revive enforces this)
- [x] T011 [P] Define the four sell states and their names in `internal/modules/product/domain/constant/sellstate.go`, with the transition table as data (FR-021, research D8)
- [x] T012 [P] Define the machine codes in `internal/modules/product/domain/constant/codes.go` (`PRODUCT_NOT_FOUND`, `PRODUCT_SLUG_TAKEN`, `PRODUCT_STATE_TRANSITION_INVALID`, `PRODUCT_IMAGE_LIMIT_REACHED`, `PRODUCT_IMAGE_TYPE_UNSUPPORTED`, `PRODUCT_IMAGE_TOO_LARGE`, `PRODUCT_MEDIA_UNAVAILABLE`) and the audit actions in `internal/modules/product/domain/constant/audit.go` (`PRODUCT_CREATED`, `PRODUCT_UPDATED`, `PRODUCT_STATE_CHANGED`, `PRODUCT_IMAGE_ADDED`, `PRODUCT_IMAGE_REMOVED`, `PRODUCT_IMAGE_PRIMARY_SET`, `PRODUCT_DELETED`), each with a comment (FR-013)
- [x] T013 [P] Implement the product entity and its transitions in `internal/modules/product/domain/model/product.go`: create, edit, and the sell-state transitions — launch, sell out, restock, retire — refusing every move the table does not have an edge for and naming the current state (FR-021 to FR-025). Launching clears the pre-order label (FR-039). A caller MUST NOT be able to reach a state by assigning a field
- [x] T014 [P] Implement the money rule in `internal/modules/product/domain/model/price.go`: an integer amount in a currency's minor unit plus a three-uppercase-letter currency, refusing zero and negative amounts (FR-030, FR-031, research D3). No floating point anywhere in the path
- [x] T015 [P] Implement the slug and normalisation rules in `internal/modules/product/domain/model/slug.go` as pure functions, following module 03's rule exactly: the URL-safe shape (FR-028), the bounds counted in **characters** (FR-029), and the folding key used for uniqueness — trimmed and case-folded with Unicode-aware folding, **not** a locale-dependent one (research D4)
- [x] T016 [P] Implement the picture rules in `internal/modules/product/domain/model/picture.go`: the several-per-product ordering and single-main-picture invariant (FR-017), the ten-picture ceiling (FR-020), the primary-picture promotion rule (FR-016, research D7), and the upload validation — format by content signature and the 2 MB ceiling, reusing module 02's approach (FR-019)
- [x] T017 [P] Define the module's sentinel errors in `internal/modules/product/domain/error/errors.go` — a not-found, a slug collision, an invalid state transition carrying the current state, a picture-limit carrier, an unsupported-type carrier, an oversized carrier, a media-unavailable carrier, and an invalid-value carrier that names the offending field (FR-023, FR-027, FR-029, FR-031)
- [x] T018 Write the domain tests in `internal/modules/product/domain/model/{product_test.go,price_test.go,slug_test.go,picture_test.go}` **before** T013–T016 are finished, and confirm they fail: every legal sell-state transition moves the entity and every illegal one is refused naming the current state; `DISCONTINUED` has no outgoing edge; launching clears the pre-order label; the price refuses zero and negative and stores an amount exactly; the slug rule behaves as module 03's does including a Vietnamese uppercase letter; the picture ceiling refuses the eleventh

### Ports and adapters

- [x] T019 [P] Declare the repository contract in `internal/modules/product/domain/repository/product.go` — the reads both audiences need, the writes, the collision-aware create and update, the picture operations, and the set-membership operations. The reads take the **visible category identifiers** as a parameter so the visibility filter happens in SQL (research D1). Repositories MUST NOT open a transaction
- [x] T020 [P] Declare the application ports in `internal/modules/product/application/interface/ports.go`: the use-case surface for both audiences, plus what it depends on — the repository, the auditor, the media store, the visibility contract, and the `UnitOfWork` port the picture operations need (research D6)
- [x] T021 [P] Implement the actor port in `internal/modules/product/application/interface/actor.go`, taking the acting administrator from the context the session filled, exactly as module 03 does
- [x] T022 [P] Define the application DTOs in `internal/modules/product/application/dto/dto.go` — the use-case inputs and outputs, separate from the HTTP shapes
- [x] T023 [P] Implement the single mapper in `internal/modules/product/application/mapper/mapper.go`; it stays the only place a model becomes a DTO
- [x] T024 Implement the PostgreSQL adapter in `internal/modules/product/infrastructure/implement/postgres/product.go`, embedding the shared `Base` with an **explicit `Columns` projection**, binding every value as a parameter, recomputing `normalized_slug` on every insert and update, ordering reads by `position, created_at, id`, applying the **whole visibility predicate** — `(sell_state = 'ACTIVE' OR (is_preorder AND sell_state <> 'DISCONTINUED')) AND category_id = ANY($n)` — in both public reads' own queries (FR-002, FR-026, research D1), and distinguishing the unique-constraint violation so the use case can answer `PRODUCT_SLUG_TAKEN` (FR-027, FR-009, research D17). **The predicate was corrected in Phase 5**: this task first wrote `(sell_state = 'ACTIVE' OR is_preorder)`, which serves a retired product that still carries the pre-order label — a row retiring can produce, since retiring does not clear that label. `data-model.md` carries the same correction and the reason
- [x] T025 [P] Implement the audit adapter in `internal/modules/product/infrastructure/implement/auditor/auditor.go`, delegating to the foundation's shared audit writer rather than opening a second one (FR-013)
- [x] T026 Write `internal/modules/product/infrastructure/implement/postgres/product_integration_test.go` behind the `integration` tag, proving against real PostgreSQL what a fake cannot: the unique index rejects a duplicate normalised slug (FR-033); the shape, format and length checks reject a value that bypassed the application; the price and currency checks refuse a non-positive amount and a malformed currency; **the restricting foreign key refuses deleting a category that still has products** (FR-035, research D15); the partial unique index refuses a second main picture; the cascading deletes remove a product's pictures and membership rows; a product's identifier survives every update unchanged and no code path rewrites it (FR-015, SC-008); and page ordering is stable across two reads with a shared position (FR-009, SC-009). Confirm each test fails if its constraint is removed from the migration

**Checkpoint**: The shared capability, the contract, the tables, the entity, its rules, the ports and
the adapters all exist and the storage guarantees are proven. US1 and US2 can proceed independently.

---

## Phase 3: User Story 1 - A customer browses the shop and inspects a product (Priority: P1) 🎯 MVP

**Goal**: A visitor reads the products that are visible to them — on sale, or announced as a
pre-order, in a category the operator has left on display — narrows the list to one category, and
reaches a single product by its slug without ever learning that anything else exists.

**Independent Test**: Insert products directly through the repository — some on sale, some not, one
of the latter a pre-order, across a visible and a hidden category — then request the public list,
the filtered list and the public detail. Confirm exactly the visible ones appear with their price,
picture and availability, that the order is the operator's and stable, and that everything else
answers the same not-found. Delivers value on its own: `quickstart.md` scenarios 1, 2b, 2c, 3b,
4, 5b, 6a–6d, 13, 16e.

### Tests for User Story 1 ⚠️

> **NOTE: Write these tests FIRST, ensure they FAIL before implementing.**

- [x] T027 [P] [US1] Use-case tests in `internal/modules/product/application/implement/catalogue_test.go` over an in-memory repository and a fake visibility port: only visible products are returned (FR-002); the visible set reaches the repository so the filter is not applied in Go (research D1); a product in a hidden category is excluded and answers not-found (FR-003); an empty or fully hidden catalogue answers an empty result rather than an error (FR-007); the order is the configured one and identical across two calls (FR-009, SC-009)
- [x] T028 [P] [US1] HTTP tests in `internal/modules/product/presentation/http/catalogue_test.go`: the public list is reachable with no token (FR-001); each entry carries **exactly** `id`, `name`, `slug`, `price`, `imageUrl`, `isPreorder` — a price, a currency, a picture and its availability — and no `sellState`, no `position`, no `categoryId`, no `isSet` and no folding key (FR-004, FR-008, SC-002); the price round-trips as the exact integer sent (FR-030); `quickstart.md` scenario 4 is the shape assertion to encode
- [x] T029 [P] [US1] Router test in `internal/modules/product/presentation/http/router_test.go` asserting the two public paths resolve, that the detail route takes a slug rather than an identifier, and that the list accepts the optional `category` filter
- [x] T030 [US1] Category-filter tests in `internal/modules/product/presentation/http/catalogue_test.go`: filtering by a hidden category and by an unknown one both answer `200` with `data: []`, indistinguishable from each other, so the filter cannot be used to discover hidden categories (FR-006, `quickstart.md` 6d)

### Implementation for User Story 1

- [x] T031 [US1] Implement the public browse use cases in `internal/modules/product/application/implement/catalogue.go`, calling the visibility contract once per request and passing the set to the repository (FR-001 to FR-009)
- [x] T032 [US1] Implement the public HTTP shapes in `internal/modules/product/presentation/dto/dto.go` and the public handler, router and error mapping in `internal/modules/product/presentation/http/{handler.go,router.go,errors.go}` (FR-001, FR-003, FR-004, FR-005, FR-008)
- [x] T033 [US1] Mount the module's public route group in `cmd/api/main.go`, wiring the visibility contract, and confirm `go build ./...` succeeds
- [x] T034 [US1] Add the integration test in `internal/modules/product/presentation/http/catalogue_integration_test.go` behind the `integration` tag: with rows seeded through the repository and a real category, a product in a hidden category is absent from the list and from its own route, a `COMING_SOON` product and an `OUT_OF_STOCK` one are absent for their own reason (FR-002, FR-026), a pre-order is present, `meta.total` counts only what is visible, the page is not short when hidden rows exist (SC-001, `quickstart.md` 16e), and the hidden answer and the never-existed answer are identical apart from the request identifier (SC-003)

**Checkpoint**: US1 is fully functional and independently testable. This is a valid MVP
demonstration: the catalogue is browsable, which is the only part of the feature a customer
touches. It is only useful once US2 lands, because nobody can put anything in it yet — which is why
both are P1 and ship together.

---

## Phase 4: User Story 2 - An operator maintains the products (Priority: P1)

**Goal**: An administrator creates a product, corrects it, attaches and removes pictures and picks
the main one, and removes a product — with every change audited and every action refused to anyone
else.

**Independent Test**: Drive the administrator endpoints through their whole lifecycle, including
picture upload, removal and promotion, and confirm after each step what a customer would then see,
that an audit entry exists for each write, and that a customer token is refused.
`quickstart.md` scenarios 2, 3, 8, 9, 10, 14, 16.

### Tests for User Story 2 ⚠️

> **NOTE: Write these tests FIRST, ensure they FAIL before implementing.**

- [x] T035 [P] [US2] Use-case tests in `internal/modules/product/application/implement/maintenance_test.go`: create returns a product in `COMING_SOON` (FR-009); edit changes the fields it carries and leaves the rest (FR-011); an edit that re-sends the product's own slug succeeds (FR-027); remove deletes and leaves no trace (FR-012); creating or editing with an unknown category is refused naming the field (FR-032); every one of those writes records its audit action (FR-013, SC-006)
- [x] T036 [P] [US2] Picture use-case tests in `internal/modules/product/application/implement/pictures_test.go`: the first picture added becomes the main one (FR-016); removing the main one promotes the next by position (FR-020); the eleventh is refused **before** the media store is called, asserted by counting the fake's calls (FR-020, `quickstart.md` 10g); a picture that does not belong to the product answers not-found; the count check and the insert run inside the `UnitOfWork` transaction (research D6)
- [x] T037 [P] [US2] HTTP tests in `internal/modules/product/presentation/http/maintenance_test.go`: no token is `401` and a customer token is `403` on every maintenance route (FR-014); create is `201`; edit is `200`; remove is `204`; an unknown identifier is `404 PRODUCT_NOT_FOUND`; a malformed identifier is `400 VALIDATION_ERROR` with the field named; a duplicate slug is `409 PRODUCT_SLUG_TAKEN` naming `slug`; a removed product cannot be removed again and answers `404` rather than failing
- [x] T038 [P] [US2] Error-mapping tests in `internal/modules/product/presentation/http/errors_test.go` covering every code the module can answer with, including that a slug collision maps to `409`, an unsupported image type to `400 PRODUCT_IMAGE_TYPE_UNSUPPORTED`, an oversized upload to `413 PRODUCT_IMAGE_TOO_LARGE`, a media outage to `503 PRODUCT_MEDIA_UNAVAILABLE`, and every field-level rejection to `400` with `details[].field` populated
- [x] T039 [US2] Upload-validation tests in `internal/modules/product/presentation/http/maintenance_test.go` driving the multipart endpoint: a JPEG, a PNG and a WebP are accepted; a PDF, a GIF and a text file are refused by their **bytes** and not by a file name or a declared content type, and an oversized one is refused too — each before anything reaches the provider (FR-019, SC-007, `quickstart.md` 10e, 10f)

### Implementation for User Story 2

- [x] T040 [US2] Implement the maintenance use cases in `internal/modules/product/application/implement/maintenance.go`, taking the actor from the session and never from the request, and recording the audit action for each write (FR-010 to FR-014)
- [x] T041 [US2] Implement the picture use cases in `internal/modules/product/application/implement/pictures.go`: validate before the provider is called, keep the pictures ordered with exactly one main one while any exists (FR-017), store only the provider's reference and never the bytes (FR-018), run the count check and the insert inside the `UnitOfWork` transaction, promote on removal, and release the stored asset **after** the row is gone (FR-016 to FR-020, research D6, D14)
- [x] T042 [US2] Implement the administrator HTTP shapes in `internal/modules/product/presentation/dto/dto.go` so the administrator response carries `sellState`, `position`, `categoryId`, `isSet` and the timestamps the public shape must not, and implement the administrator handler, route group and error mapping in `internal/modules/product/presentation/http/{handler.go,router.go,errors.go}` with the administrator role guard (FR-010, FR-008, FR-014)
- [x] T043 [US2] Mount the module's administrator route group in `cmd/api/main.go`, constructing the repository, the auditor, the media store, the visibility contract and the `UnitOfWork`; confirm `go build ./...` succeeds
- [x] T044 [US2] Add the integration test in `internal/modules/product/presentation/http/maintenance_integration_test.go` behind the `integration` tag: create, edit, add pictures, promote one, remove one and remove the product against real PostgreSQL, confirming after each step what the public list then shows, that each write left its audit row, and that a product's pictures and membership rows are gone after the product is
- [x] T045 [US2] Add the picture-concurrency integration test in `internal/modules/product/presentation/http/maintenance_integration_test.go` behind the `integration` tag: two concurrent promotions leave exactly one main picture, and two concurrent uploads at the ceiling admit at most ten (research D6, `quickstart.md` 10j)

**Checkpoint**: US1 and US2 both work independently. The catalogue can be browsed and maintained.
This is the point at which the feature is genuinely usable.

---

## Phase 5: User Story 3 - The sell state can be trusted (Priority: P2)

**Goal**: An operator moves a product through its selling life and the system refuses a move that
makes no sense, telling them which state made it invalid — and a product is only buyable when it is
on sale.

**Independent Test**: Drive a product through every legal transition and confirm each succeeds and
changes what a customer sees, then attempt each illegal one and confirm it is refused naming the
current state and leaves the product untouched. `quickstart.md` scenarios 7.

**Dependency**: US3 needs US2's product endpoints and the state route. It is P2 for that reason and
is not a defect in the ordering.

### Tests for User Story 3 ⚠️

> **NOTE: Write these tests FIRST, ensure they FAIL before implementing.** T018 proves the
> transition table in the domain; these prove it through the endpoint a human uses.

- [x] T046 [P] [US3] Use-case tests in `internal/modules/product/application/implement/lifecycle_test.go`: `COMING_SOON → ACTIVE`; `ACTIVE ↔ OUT_OF_STOCK` in both directions; any of the three → `DISCONTINUED`; `DISCONTINUED → anything` refused naming `DISCONTINUED`; `DISCONTINUED → DISCONTINUED` refused; every transition records `PRODUCT_STATE_CHANGED`; and a refused transition leaves the product exactly as it was (FR-021 to FR-025, SC-004)
- [x] T047 [P] [US3] HTTP tests in `internal/modules/product/presentation/http/lifecycle_test.go`: a legal move is `200`; an illegal one is `409 PRODUCT_STATE_TRANSITION_INVALID` naming the current state; an unknown target state is `400 VALIDATION_ERROR` naming `to`; a customer token is `403`; an unknown product is `404`; and `sellState` sent to the edit endpoint is refused rather than accepted (research D11)
- [x] T048 [US3] Integration test in `internal/modules/product/presentation/http/lifecycle_integration_test.go` behind the `integration` tag: launching a product makes it appear in the public list, selling out removes it, restocking brings it back, retiring removes it for good, and the state column in the database matches the response after every step (FR-002, FR-026, SC-003)

### Implementation for User Story 3

- [x] T049 [US3] Implement the lifecycle use cases in `internal/modules/product/application/implement/lifecycle.go`, routing every change through the domain's transition functions and never writing the state directly (FR-022, research D8)
- [x] T050 [US3] Add the `POST /api/v1/admin/products/{id}/state` route and its request shape in `internal/modules/product/presentation/http/router.go` and `internal/modules/product/presentation/dto/dto.go`, and map the invalid-transition sentinel to `409` in `internal/modules/product/presentation/http/errors.go` (FR-023)
- [x] T051 [US3] Add the domain test in `internal/modules/product/domain/model/product_test.go` asserting the state a customer-facing response implies is enforced **at the point of reading** as well as writing — a product hidden by state is not served, whatever wrote the row (FR-026)

**Checkpoint**: All three stories are independently functional and the sell state is trustworthy.

---

## Phase 6: User Story 4 - A category that still has products cannot vanish (Priority: P2)

**Goal**: An operator tries to remove a category that products still belong to, and the system
refuses — from the storage layer, not from a check — so no product is ever left pointing at a
category that is gone. This is the half of feature 005's completion criterion that could not be
built until products existed.

**Independent Test**: Create a category and a product in it, attempt to remove the category, and
confirm it is refused with an answer the operator can act on while both survive; then remove the
product and confirm the category can be removed. Then attempt the delete directly against
PostgreSQL and confirm the database itself refuses. `quickstart.md` scenario 12.

**Dependency**: US4 needs US2's product create endpoint to have a product referencing the category,
and it changes module 03. It is P2 for that reason.

### Tests for User Story 4 ⚠️

> **NOTE: Write these tests FIRST, ensure they FAIL before implementing.** T026 already proves the
> foreign key at the storage layer; these prove the operator's answer and the wiring around it.

- [x] T052 [P] [US4] HTTP test in `internal/modules/category/presentation/http/maintenance_test.go` asserting that removing a category which still has products answers `409 CATEGORY_IN_USE` rather than `500`, and that the category and its products are both still there afterwards (FR-036, FR-037)
- [x] T053 [P] [US4] Error-mapping test in `internal/modules/category/presentation/http/errors_test.go` asserting the new sentinel maps to `409` with its code, alongside the existing `CATEGORY_*` mappings
- [x] T054 [US4] Integration test in `internal/modules/category/presentation/http/maintenance_integration_test.go` behind the `integration` tag: with a real product referencing the category, the delete is refused; after the product is removed the delete succeeds; and the refusal is produced by the storage layer, proven by asserting the error is a foreign-key violation before it is translated (FR-035, FR-036, SC-005)

### Implementation for User Story 4

- [x] T055 [US4] Add the `CATEGORY_IN_USE` code to `internal/modules/category/domain/constant/codes.go`, its sentinel to `internal/modules/category/domain/error/errors.go`, and its mapping to `internal/modules/category/presentation/http/errors.go` (FR-036)
- [x] T056 [US4] Translate the storage refusal in `internal/modules/category/infrastructure/implement/postgres/category.go`: classify the foreign-key violation by its SQLSTATE rather than by the database's message text, and report the new sentinel so the use case can answer it. Module 03 MUST NOT check for products itself — that would be reading module 04's table (research D15)
- [x] T057 [US4] Confirm the removal rule is now reported in `docs/modules/03-category.md` as met rather than carried forward, and that `specs/005-category-catalog/deferred.md` D1's closing condition is satisfied. Do not edit 005's spec — superseding it with the record here is what the constitution requires

**Checkpoint**: The debt feature 005 left is paid, and module 03's completion criterion is true.

---

## Phase 7: User Story 5 - Combo sets (Priority: P3)

**Goal**: An operator offers several products together as one purchasable set with a price they
set, and the products inside it stay listed individually.

**Independent Test**: Create a set from existing products, confirm it appears to customers as one
item with one price and one picture, that its members are still listed on their own, and that
removing the set leaves them untouched. `quickstart.md` scenario 11.

**Dependency**: US5 needs US2's create and edit endpoints. It is P3 because the shop sells
individual items without it.

### Tests for User Story 5 ⚠️

> **NOTE: Write these tests FIRST, ensure they FAIL before implementing.**

- [x] T058 [P] [US5] Use-case tests in `internal/modules/product/application/implement/maintenance_test.go`: creating a set records its members and keeps the operator's price rather than summing the members (FR-039, research D10); editing replaces the whole member list; a member identifier that does not exist is refused naming the field; and removing a set leaves its members alone (US5 scenario 3)
- [x] T059 [P] [US5] HTTP tests in `internal/modules/product/presentation/http/maintenance_test.go`: the administrator detail lists a set's members in order; the public detail lists **none** (research D18, `quickstart.md` 11c); and a self-referencing member is refused rather than producing a `500` (`quickstart.md` 11g)
- [x] T060 [US5] Integration test in `internal/modules/product/presentation/http/maintenance_integration_test.go` behind the `integration` tag: the membership rows are written and read in order, removing a set cascades its membership rows and leaves the members, and removing a member removes it from the set it was in

### Implementation for User Story 5

- [x] T061 [US5] Implement set-membership reads and writes in `internal/modules/product/infrastructure/implement/postgres/product.go` and wire them through `internal/modules/product/application/implement/maintenance.go`, inside the same transaction as the product write so a set is never created without its members (FR-038)
- [x] T062 [US5] Add `isSet` and `memberProductIds` to the create and update request shapes in `internal/modules/product/presentation/dto/dto.go` and the `members` field to the administrator detail shape, leaving the public shape without it (research D18)

**Checkpoint**: US5 is functional and the catalogue still sells individual items unchanged.

---

## Phase 8: User Story 6 - Pre-orders (Priority: P3)

**Goal**: An operator announces a product that is not yet available, and a customer can see it and
tell it is not buyable.

**Independent Test**: Create a product as a pre-order, confirm a customer sees it as an
announcement rather than as something buyable, and confirm it behaves as an ordinary product once
it goes on sale. `quickstart.md` scenario 5.

**Dependency**: US6 needs US1's public read to be visible in, and US3's launch transition to clear
the label. It is P3 because the catalogue works without it.

### Tests for User Story 6 ⚠️

> **NOTE: Write these tests FIRST, ensure they FAIL before implementing.**

- [x] T063 [P] [US6] Use-case tests in `internal/modules/product/application/implement/maintenance_test.go`: setting the pre-order label on a product that is not on sale succeeds; setting it on an `ACTIVE` product is refused naming `isPreorder`; an expected date without the label is refused; and launching a product clears the label (FR-040, `quickstart.md` 5e)
- [x] T064 [P] [US6] Visibility tests in `internal/modules/product/presentation/http/catalogue_test.go`: a pre-order is **present** in the public list with `isPreorder: true` while an ordinary unlaunched product is absent — the two cases side by side, because passing one and failing the other is exactly the contradiction the clarification resolved (FR-002, FR-039, `quickstart.md` 5b)
- [x] T065 [US6] Integration test in `internal/modules/product/presentation/http/catalogue_integration_test.go` behind the `integration` tag: a pre-order is visible and not buyable; launching it makes it buyable and clears the label; and the database's pre-order consistency check refuses a row that claims both (FR-039)

### Implementation for User Story 6

- [x] T066 [US6] Add the pre-order label and expected date to the create and update request shapes in `internal/modules/product/presentation/dto/dto.go`, to the visibility condition in `internal/modules/product/infrastructure/implement/postgres/product.go`, and to the label-clearing in `internal/modules/product/domain/model/product.go`'s launch transition (FR-002, FR-040)

**Checkpoint**: All six stories are independently functional.

---

## Phase 9: Polish & Cross-Cutting Concerns

**Purpose**: Documentation, the frontend hand-off, a decision record, and the close-out gates.

- [ ] T067 [P] Update `docs/api-reference.md` with the eleven endpoints in the existing six-part format (info table including its rate limit, request, response, errors, notes), the seven new codes in the error section, the summary table, a change-log row, the public-shape note that a hidden product and an unknown slug are indistinguishable, and — in module 03's section — the new `CATEGORY_IN_USE` refusal. Required by Constitution VIII in the same change as the code
- [ ] T068 [P] Write `specs/006-product-catalog/frontend-guide.md`: every endpoint this feature adds, marked new, with its method and path and the shape of each response, plus module 03's changed `DELETE` marked changed with behaviour before → after. Required by the constitution for any feature that changes a client-facing contract
- [ ] T069 [P] Update `docs/modules/04-product.md`: status, spec pointer, and the completion criteria this feature meets — stating plainly the two it does not (the automatic stock-driven state, which is module 05's) with a pointer to `deferred.md`. Marking a module complete without saying what was carried forward is how an unfinished rule becomes invisible
- [ ] T070 [P] Write the ADR at `docs/decisions/013-*.md` for the decisions a later reader will question: the cross-module visibility contract and why a join was refused, the shared media capability's new home, and hard-deleting a product while the module document says soft. Follow `docs/decisions/template.md`, write it in Vietnamese like the existing ADRs, add it to `docs/decisions/README.md`, and state the accepted costs. **Check the number against `docs/decisions/` at the time of writing** — 011 and 012 are taken
- [ ] T071 [P] Verify in `internal/modules/product/presentation/http/leak_test.go` that neither a product's values nor an audit entry can leak something they should not — in particular that the folding key never appears in a response, an audit entry or a log line, and that a media failure's log line carries no provider URL, credential or response body (FR-013, Constitution V, VI)
- [ ] T072 Check every new and changed file for UTF-8 without BOM and LF endings, per `.editorconfig`; PowerShell's `Out-File` and `WriteAllLines` write CRLF, so write files in a way that ends up correct. Report the method and the count
- [ ] T073 Run `make lint` and `make test` and clear every finding in the feature's scope
- [ ] T074 Run `quickstart.md` end to end — scenarios 1 through 16 — and confirm each passes. A container-backed check that silently skips is a **fail**, not a pass. Scenarios 10 and 11 need real media credentials; if they are absent, report them `BLOCKED` rather than passing them
- [ ] T075 Run `make check` — the close-out gate — once, and confirm `git status` shows only intended files before anything is staged
- [ ] T076 Re-read `specs/006-product-catalog/frontend-guide.md` against the implementation with counts — JSON fields, enum values, routes, HTTP statuses — and correct it. The constitution requires this re-read in the feature's final phase, and a comparison that produces no counts did not happen

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies
- **Foundational (Phase 2)**: Depends on Setup — **BLOCKS all six user stories**
- **US1 (Phase 3)**: Depends on Phase 2 only
- **US2 (Phase 4)**: Depends on Phase 2 only. Independent of US1: they share the tables and the adapter, not each other's code
- **US3 (Phase 5)**: Depends on US2 — its independent test drives the state endpoint
- **US4 (Phase 6)**: Depends on US2 — it needs a product referencing a category — and changes module 03
- **US5 (Phase 7)**: Depends on US2 — it extends the create and edit use cases
- **US6 (Phase 8)**: Depends on US1 and US3 — a pre-order is only meaningful once the public read exists and a product can be launched
- **Polish (Phase 9)**: Depends on all six stories

### Within Each Phase

- Tests are written and must FAIL before the implementation they cover
- The entity and its rules precede the adapter; the adapter precedes the use cases; the use cases precede the handlers
- T002–T004 (the media move) must land before T043 wires the media store, and they are deliberately first because a half-moved capability would break module 02's tests for the rest of the feature
- T006 must follow T005 — the contract has to exist before an adapter can satisfy it
- T008 must precede T026, since there is nothing to constrain until the migration exists
- T018 is deliberately written before T013–T016 land, because a rule test that passes before the rule exists is testing nothing
- T067–T070 are independent files and can be written together

### Critical Path

T001 → T008 → T013 → T014 → T015 → T024 → T031 → T032 → T033 → T040 → T041 → T042 → T043 → T049 → T074 → T075

The entity (T013) and its rules (T014, T015) sit on the path because every use case depends on them.
The media move (T002) and the contract (T005) branch off early and rejoin at T043.

US4, US5 and US6 sit at the end of the chain: US4 verifies a storage guarantee through module 03's
endpoint, US5 extends US2's write path, and US6 needs both the public read and the launch
transition. That is the honest order, not an accident of numbering.

---

## Parallel Opportunities

- T005, T008 and T010–T017 are largely independent files once the skeleton exists; T006 depends on T005
- T002–T004 are one unit and should not be split across agents
- Within US1, T027, T028 and T029 are three different test files
- Within US2, T035–T039 are five different concerns over three test files; T035 and T036 are different files and can run together
- Within US3, T046 and T047 are two different test files
- Within US4, T052 and T053 are two different test files in module 03
- All of Phase 9's documentation tasks (T067–T071) are independent of each other

---

## Parallel Example: User Story 1

```bash
# All three test files, before any implementation:
Task: "Use-case tests in application/implement/catalogue_test.go"
Task: "HTTP tests in presentation/http/catalogue_test.go"
Task: "Router test in presentation/http/router_test.go"

# Then the implementation, in order — each depends on the one before:
Task: "Implement the public browse use cases in application/implement/catalogue.go"
Task: "Implement the public handler, router and error mapping"
Task: "Mount the public route group in cmd/api/main.go"
Task: "Add the public integration test"
```

---

## Implementation Strategy

### MVP First (User Story 1 + User Story 2)

Both P1 stories are the MVP. US1 alone is browsable but empty, so the smallest genuinely useful
increment is US1 + US2.

1. Phase 1: confirm the baseline
2. Phase 2: the media move, the contract, the migration, the entity, its rules, the adapter
3. Phase 3 + 4: US1 and US2
4. **STOP and VALIDATE**: create a category and a product, attach pictures, put it on sale, browse
   it as a customer, hide the category and confirm the product vanishes
5. Deploy or demo if ready

### Incremental Delivery

1. Phase 1 + 2 → the tables, the entity and the storage guarantees exist and are proven
2. US1 → the catalogue is browsable → validate independently
3. US2 → the catalogue is maintainable → validate independently → the feature is usable
4. US3 → the sell state is trustworthy → validate independently
5. US4 → module 03's completion criterion becomes true → validate independently
6. US5 → combo sets → validate independently
7. US6 → pre-orders → validate independently
8. Phase 9 → documentation, the frontend hand-off and the decision record, then the close-out gate

### Parallel Team Strategy

With two developers:

1. Both complete Phase 1 and 2 together — T002–T004 (the media move) is the one piece that touches
   another module and should not be split; T024's adapter is the other contested file
2. Then split: **Developer A takes US1** (the public read path), **Developer B takes US2** (the
   write path, including pictures). They touch different files: US1 owns the catalogue use case and
   the public handler, US2 owns the maintenance and picture use cases, the administrator handler
   and the composition
3. `presentation/http/router.go`, `presentation/dto/dto.go` and `errors.go` are touched by both —
   sequence those three files rather than editing them in parallel
4. US3 follows US2; US4 follows US2 and touches module 03; US5 follows US2; US6 follows US1 + US3

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] labels map each task to a user story
- US1 is independent of US2; US3, US4, US5 and US6 are not, and that is stated rather than hidden
- Verify tests fail before implementing. A test that passes before its implementation exists is
  testing nothing, and T018, T026 and T046 are the ones most likely to be written that way by
  accident
- Commit after each phase, not after each task
- The same-file overlaps are `presentation/http/router.go`, `presentation/dto/dto.go` and
  `errors.go` (US1/US2/US3), and `application/implement/maintenance_test.go`
  (US2/US5/US6) — sequence them
- Avoid: vague tasks, same-file conflicts, and letting US4 be reduced to an application-level
  check — T054 must prove the **storage layer** refuses, or the feature has not paid 005's debt
