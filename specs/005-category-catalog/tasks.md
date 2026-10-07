---

description: "Task list for feature 005-category-catalog"
---

# Tasks: Category Catalogue

**Input**: Design documents from `specs/005-category-catalog/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: Included. The constitution mandates test-first coverage for state transitions and
invariants, and this module's substance *is* invariants — catalogue-wide uniqueness, the slug
shape, the bounds, and the display transitions. Each test task is written first and must fail
before its implementation task runs.

**Organization**: One phase per user story. US1 needs nothing US2 builds — its rows can be
inserted through the repository — so it is genuinely independent. US3 cannot be exercised
before US2's write endpoints exist, and that dependency is stated rather than hidden.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

Go web service inside a modular monolith, following `plan.md`:

- `internal/modules/category/` — the new module, four layers
- `migrations/` — the additive schema change
- `cmd/api/main.go` — composition
- `docs/` — documentation that must change with the code (Constitution VIII)

---

## Phase 1: Setup (Baseline)

**Purpose**: Establish a green baseline so a later failure is attributable to this feature. This
feature adds no project scaffolding — the module skeleton is Phase 2's first task.

- [x] T001 Confirm the baseline is green and the migration number is free: run `make lint`, `make test` and `make test-integration` (all must exit 0, and the integration run must be proven to execute rather than skip), and confirm `migrations/` ends at `00004_user.sql` so the new migration is `00005`

**Checkpoint**: The tree is green and the next migration number is known.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The table, the entity, its rules, the contracts and the adapter — everything both
user stories stand on.

**⚠️ CRITICAL**: No user story work begins until the foundational phase is complete — through T016, not merely T014. US2 needs the auditor adapter (T015) as much as the repository, and T016 is what proves the storage guarantees every story rests on.

- [x] T002 Write `migrations/00005_category.sql` following `docs/development/migration.md`: the `categories` table with the ten columns `data-model.md` specifies (`id`, `name`, `normalized_name`, `slug`, `normalized_slug`, `description`, `position`, `is_visible`, `created_at`, `updated_at`), the two unique indexes on the normalised columns (FR-016, FR-017, FR-021), the two normalised-shape checks and the slug-format check (FR-018), the three length checks (FR-019), the ordering index on `(position, created_at, id)` (FR-003), `COMMENT ON` for the columns whose purpose is not obvious, and both `-- +goose Up` and `-- +goose Down` sections. The primary key is what satisfies FR-015 and FR-023: it is unique, generated once, never rewritten and never reused, so a reference a product stores can never come to mean a different category
- [x] T003 Add `migrations/category_guard_test.go` asserting the migration text carries every constraint **and** drops it in the down section — matching the pattern of the existing `migrations/user_profile_guard_test.go`, because the runner rejects a one-way migration
- [x] T004 [P] Create the module skeleton under `internal/modules/category/` with the four layers from `plan.md` — `domain/{constant,error,model,repository}`, `application/{dto,interface,implement,mapper}`, `infrastructure/implement/{postgres,auditor}`, `presentation/http` — each package carrying a `// Package …` comment (revive enforces this)
- [x] T005 [P] Implement the category entity and its transitions in `internal/modules/category/domain/model/category.go`: create, edit, hide, show, and the invariant that a caller cannot reach an inconsistent state by assigning a field (FR-008, FR-010, FR-011, FR-012, FR-022). The entity knows nothing about HTTP, storage or the database
- [x] T006 [P] Implement the slug and normalisation rules in `internal/modules/category/domain/model/slug.go` as pure functions: the URL-safe shape rule (FR-018), the length bounds counted in **characters** (FR-019), and the folding key used for uniqueness — trimmed and case-folded with Unicode-aware folding, **not** a locale-dependent one (research D3)
- [x] T007 [P] Define the module's sentinel errors in `internal/modules/category/domain/error/errors.go` — a not-found, a name collision, a slug collision, and an invalid-value carrier that names the offending field (FR-005, FR-018, FR-019, FR-020)
- [x] T008 [P] Define the machine codes in `internal/modules/category/domain/constant/codes.go` (`CATEGORY_NOT_FOUND`, `CATEGORY_NAME_TAKEN`, `CATEGORY_SLUG_TAKEN`) and the audit actions in `internal/modules/category/domain/constant/audit.go` (`CATEGORY_CREATED`, `CATEGORY_UPDATED`, `CATEGORY_HIDDEN`, `CATEGORY_SHOWN`, `CATEGORY_DELETED`), each with a comment (FR-013)
- [x] T009 Write the domain tests in `internal/modules/category/domain/model/category_test.go` and `slug_test.go` **before** T005–T006 are finished, and confirm they fail: the slug shape accepts `tranh-son-dau` and refuses accents, spaces, a leading hyphen, a trailing hyphen and a double hyphen; the bounds accept exactly 120 characters of Vietnamese and refuse 121; folding treats `Tranh` and `tranh` as the same key **including a Vietnamese uppercase letter**; every transition moves the entity as `data-model.md` describes
- [x] T010 [P] Declare the repository contract in `internal/modules/category/domain/repository/category.go` — the reads the two audiences need, the writes, and the collision-aware create and update. Repositories MUST NOT open a transaction
- [x] T011 [P] Declare the application ports in `internal/modules/category/application/interface/ports.go`: the use-case surface for both audiences, plus what it depends on (the repository, the auditor)
- [x] T012 [P] Define the application DTOs in `internal/modules/category/application/dto/dto.go` — the use-case inputs and outputs, separate from the HTTP shapes
- [x] T013 [P] Implement the single mapper in `internal/modules/category/application/mapper/mapper.go`; it stays the only place a model becomes a DTO
- [x] T014 Implement the PostgreSQL adapter in `internal/modules/category/infrastructure/implement/postgres/category.go`, embedding the shared `Base` with an **explicit `Columns` projection** (the shared base now refuses an empty projection), binding every value as a parameter, writing the normalised columns on every insert and update, and ordering reads by `position, created_at, id` (FR-003, FR-021)
- [x] T015 [P] Implement the audit adapter in `internal/modules/category/infrastructure/implement/auditor/auditor.go`, delegating to the foundation's shared audit writer rather than opening a second one (FR-013)
- [x] T016 Write `internal/modules/category/infrastructure/implement/postgres/category_integration_test.go` behind the `integration` tag, proving against real PostgreSQL what a fake cannot: the two unique indexes reject a duplicate normalised name and a duplicate normalised slug (FR-021); the shape checks reject an un-normalised value a future writer might store; the slug-format and length checks reject a value that bypassed the application; and page ordering is stable across two reads with a shared position (FR-003, SC-002). Also assert that a category's identifier survives every update unchanged and that no code path writes it (FR-015, FR-023, SC-006). Confirm each test fails if its constraint is removed from the migration

**Checkpoint**: The table, the entity, its rules, the adapter and the storage guarantees all exist
and are proven. US1, US2 and US3 can proceed — US1 and US2 independently, US3 after US2.

---

## Phase 3: User Story 1 - A customer browses the catalogue (Priority: P1) 🎯 MVP

**Goal**: A visitor reads the categories on display, in the operator's order, and reaches one by
its slug without ever learning that a withheld category exists.

**Independent Test**: Insert categories directly through the repository — some on display, some
withheld — then request the public list and the public detail. Confirm the visible ones appear in
the configured order with exactly four fields each, that a withheld one is absent, and that
fetching it answers byte-for-byte what an unused slug answers. Delivers value on its own:
`quickstart.md` scenarios 1, 3, 4, 9.

### Tests for User Story 1 ⚠️

> **NOTE: Write these tests FIRST, ensure they FAIL before implementing.**

- [x] T017 [P] [US1] Use-case tests in `internal/modules/category/application/implement/catalogue_test.go` over an in-memory repository: only on-display categories are returned (FR-002); the order is the configured one and is identical across two calls (FR-003); an empty catalogue answers an empty result rather than an error (FR-006); reading a withheld, removed or unknown slug answers the same not-found (FR-005, SC-004)
- [x] T018 [P] [US1] HTTP tests in `internal/modules/category/presentation/http/catalogue_test.go`: the public list is reachable with no token (FR-001, FR-014); each entry carries **exactly** `id`, `name`, `slug`, `description` and no `isVisible`, no `position` and no folding key (FR-004, FR-007); `quickstart.md` scenario 3 is the shape assertion to encode
- [x] T019 [P] [US1] Router test in `internal/modules/category/presentation/http/router_test.go` asserting the two public paths resolve and that the detail route takes a slug, not an identifier

### Implementation for User Story 1

- [x] T020 [US1] Implement the public browse use cases in `internal/modules/category/application/implement/catalogue.go`
- [x] T021 [US1] Implement the public HTTP shapes, handler and router in `internal/modules/category/presentation/http/{handler.go,router.go,errors.go}` (FR-001, FR-004, FR-005, FR-007)
- [x] T022 [US1] Add the integration test in `internal/modules/category/presentation/http/catalogue_integration_test.go` behind the `integration` tag: with rows seeded through the repository, a withheld category is absent from the list, `meta.total` counts only what is visible, the order is stable across two requests, and the withheld-slug answer and the unknown-slug answer are identical apart from the request identifier (FR-002, FR-003, FR-005, SC-001, SC-002, SC-004)

**Checkpoint**: US1 is fully functional and independently testable. This is a valid MVP: the
catalogue is browsable, which is the only part of the feature a customer touches.

---

## Phase 4: User Story 2 - An operator maintains the catalogue (Priority: P1)

**Goal**: An administrator creates a category, corrects it, takes it off display and puts it
back, and removes one — with every change audited and every action refused to anyone else.

**Independent Test**: Drive the administrator endpoints through their whole lifecycle and confirm
after each step what a customer would then see, that an audit entry exists for each write, and
that a customer token is refused. `quickstart.md` scenarios 2, 4d, 10, 11.

### Tests for User Story 2 ⚠️

> **NOTE: Write these tests FIRST, ensure they FAIL before implementing.**

- [x] T023 [P] [US2] Use-case tests in `internal/modules/category/application/implement/maintenance_test.go`: create returns a category on display (FR-008); edit changes name, slug, description and position (FR-010); hide and show move the display state without destroying anything (FR-011); remove deletes and leaves the customer-facing catalogue with no trace of it, including no gap in the ordering (FR-012, FR-024); every one of those writes records its audit action (FR-013, SC-005)
- [x] T024 [P] [US2] HTTP tests in `internal/modules/category/presentation/http/maintenance_test.go`: no token is `401` and a customer token is `403` on every maintenance route (FR-014); create is `201`; edit is `200`; remove is `204`; an unknown identifier is `404 CATEGORY_NOT_FOUND`; a malformed identifier is `400 VALIDATION_ERROR` with the field named; a removed category cannot be removed again and answers `404` rather than failing
- [x] T025 [P] [US2] Error-mapping tests in `internal/modules/category/presentation/http/errors_test.go` covering every code the module can answer with, including that a collision maps to `409` with `details[].field` naming `name` or `slug` (FR-020), and that an invalid value maps to `400` naming the field (FR-018, FR-019)

### Implementation for User Story 2

- [x] T026 [US2] Implement the maintenance use cases in `internal/modules/category/application/implement/maintenance.go`, taking the actor from the session and never from the request, and recording the audit action for each write (FR-013, FR-014)
- [x] T027 [US2] Implement the error mapping in `internal/modules/category/presentation/http/errors.go`: the module sentinels to their statuses and codes, with `details[].field` populated for every field-level rejection (FR-020)
- [x] T028 [US2] Implement the administrator handler and route group in `internal/modules/category/presentation/http/{handler.go,router.go}` — the `/api/v1/admin/categories` group with the administrator role guard, refusing a denial through the foundation's existing hook so the denial is audited as module 01 already audits its own (FR-014)
- [x] T029 [US2] Define the administrator HTTP shapes in `internal/modules/category/presentation/dto/dto.go` so the administrator response carries `isVisible`, `position` and the timestamps the public shape must not (FR-009, FR-007), and map them in `internal/modules/category/presentation/http/handler.go`
- [x] T030 [US2] Mount the module in `cmd/api/main.go` beside the existing three, constructing the repository, the auditor and the two route groups; confirm `go build ./...` succeeds
- [x] T031 [US2] Add the integration test in `internal/modules/category/presentation/http/maintenance_integration_test.go` behind the `integration` tag: create, edit, hide, show and remove against real PostgreSQL, confirming after each step what the public list then shows, that each write left its audit row, and that the hidden category is still readable by the administrator while invisible to a customer

**Checkpoint**: US1 and US2 both work independently. The catalogue can be browsed and maintained.

---

## Phase 5: User Story 3 - The catalogue cannot be made ambiguous (Priority: P2)

**Goal**: An operator cannot create or rename a category into a name or slug another category
already holds, and the refusal names the field — including when two operators act at the same
instant.

**Independent Test**: Attempt collisions through the endpoints — same name, name differing only
in case, name differing only by surrounding whitespace, Vietnamese letters differing only in
case, and the same slug — and confirm each is refused with the field named while the original is
untouched. Then fire the same creation concurrently and confirm exactly one wins.
`quickstart.md` scenarios 5, 6, 8, 12.

**Dependency**: US3 needs US2's write endpoints. It is marked P2 for that reason and is not a
defect in the ordering.

### Tests for User Story 3 ⚠️

> **NOTE: Write these tests FIRST, ensure they FAIL before implementing.** T015 proves the
> storage constraint directly; these prove it through the endpoints a human uses.

- [x] T032 [P] [US3] Collision tests in `internal/modules/category/presentation/http/collision_test.go`: a duplicate name is `409 CATEGORY_NAME_TAKEN`; a name differing only in **letter case** is refused; a name differing only in **surrounding whitespace** is refused; a duplicate slug is `409 CATEGORY_SLUG_TAKEN`; and in every case the field is named and the existing category is unchanged (FR-016, FR-017, FR-020, SC-003)
- [x] T033 [P] [US3] Vietnamese folding test in `internal/modules/category/domain/model/slug_test.go` asserting a name is refused when it differs from an existing one only by the case of a Vietnamese letter — the case a locale-driven fold passes and Unicode folding catches (FR-016, research D3). An ASCII-only test is not sufficient and must not be counted as this task
- [x] T034 [P] [US3] Self-edit test in `internal/modules/category/presentation/http/collision_test.go`: re-sending a category's own name and slug, alone and together with other changes, succeeds — a category is never a duplicate of itself, including when the slug is unchanged, which is the case a naive unique check gets wrong (FR-022, SC-006)
- [x] T035 [US3] Concurrency test behind the `integration` tag, in `internal/modules/category/presentation/http/collision_integration_test.go`: fire several creations of the same name against real PostgreSQL, assert exactly one answers `201` while the rest answer `409`, that **no answer is `500`**, and that the stored count is one (FR-020, FR-021). A duplicate-key violation surfacing as an internal error is a fail

### Implementation for User Story 3

- [x] T036 [US3] Make the adapter in `internal/modules/category/infrastructure/implement/postgres/category.go` distinguish the two unique-constraint violations and report which field collided, so the use case can answer the two different codes; the classification must not depend on the database's error text beyond the constraint name (FR-020)

**Checkpoint**: All three stories are independently functional and the catalogue's invariants hold
under concurrency.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Documentation, a decision record, and the close-out gates.

- [x] T037 [P] Update `docs/api-reference.md` with the seven endpoints in the existing six-part format (info table including its rate limit, request, response, errors, notes), the three new codes in the error section, the summary table, a change-log row, and the public-shape note that a withheld category is indistinguishable from an unused one. Required by Constitution VIII in the same change as the code
- [x] T038 [P] Update `docs/modules/03-category.md`: status, spec pointer, the completion criteria that this feature meets, and — explicitly — the two that it cannot meet yet (products by category, and removal blocked while products reference the category) with a pointer to `deferred.md`. Marking a module complete without saying what was carried forward is how an unfinished rule becomes invisible
- [x] T039 [P] Write the ADR at `docs/decisions/012-*.md` for the two decisions a later reader will question: folding in the application rather than the database, and two response shapes over one table. Follow `docs/decisions/template.md`, write it in Vietnamese like the existing ADRs, add it to `docs/decisions/README.md`, and state the accepted costs. **The number is 012, not 011:** 011 is taken by an unrelated, uncommitted Swagger decision that was set aside to run this feature, and two files claiming 011 would collide the moment it is restored
- [x] T040 [P] Verify in `internal/share/logging/redact_test.go` or the module's own test that neither a category's values nor an audit entry can leak something they should not — in particular that the folding keys never appear in a response, an audit entry or a log line
- [x] T041 Check every new and changed file for UTF-8 without BOM and LF endings, per `.editorconfig`; PowerShell's `Out-File` and `WriteAllLines` write CRLF, so write files in a way that ends up correct. Report the method and the count
- [x] T042 Run `make lint` and `make test` and clear every finding in the feature's scope
- [x] T043 Run `quickstart.md` end to end — scenarios 1 through 13 — and confirm each passes. A container-backed check that silently skips is a **fail**, not a pass
- [x] T044 Run `make check` — the close-out gate — once, and confirm `git status` shows only intended files before anything is staged

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies
- **Foundational (Phase 2)**: Depends on Setup — **BLOCKS all user stories**
- **US1 (Phase 3)**: Depends on Phase 2 only
- **US2 (Phase 4)**: Depends on Phase 2 only. Independent of US1: they share the table and the adapter, not each other's code
- **US3 (Phase 5)**: Depends on Phase 4 — **its independent test drives the maintenance endpoints**, so it cannot run before they exist
- **Polish (Phase 6)**: Depends on all three stories

### Within Each Phase

- Tests are written and must FAIL before the implementation they cover
- The entity and its rules precede the adapter; the adapter precedes the use cases; the use cases precede the handlers
- T015 must follow T002 — there is nothing to constrain until the migration exists
- T009 is deliberately written before T005 and T006 land, because a rule test that passes before the rule exists is testing nothing

### Critical Path

T001 → T002 → T005 → T006 → T014 → T020 → T021 → T022 → T026 → T028 → T030 → T036 → T043 → T044

The entity (T005) and its rules (T006) sit on the path because every use case depends on them;
an earlier draft of this line omitted them and read as though the adapter could be built on
nothing.

US3 sits at the end of the chain because it verifies the invariants through US2's endpoints.
That is the honest order, not an accident of numbering.

---

## Parallel Opportunities

- T004–T008 and T010–T013 are independent files and can be written together once the skeleton exists
- T015 is independent of T014 — the auditor adapter does not touch the repository
- Within US1, T017, T018 and T019 are three different test files
- Within US2, T023, T024 and T025 are three different test files
- Within US3, T032, T033 and T034 are three different files; T035 is a fourth and can run beside them
- All of Phase 6's documentation tasks are independent of each other

---

## Parallel Example: User Story 1

```bash
# All three test files, before any implementation:
Task: "Use-case tests in application/implement/catalogue_test.go"
Task: "HTTP tests in presentation/http/catalogue_test.go"
Task: "Router test in presentation/http/router_test.go"

# Then the implementation, in order — each depends on the one before:
Task: "Implement the public browse use cases in application/implement/catalogue.go"
Task: "Implement the public handler and router"
Task: "Add the public integration test"
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phase 1: confirm the baseline
2. Phase 2: the migration, the entity, its rules, the adapter — nothing customer-facing yet
3. Phase 3: US1
4. **STOP and VALIDATE**: seed a few categories through the repository, then browse them and
   confirm a withheld one is invisible and indistinguishable from one that never existed
5. Deploy or demo if ready

This is a genuinely shippable increment: the catalogue becomes readable. It is only useful once
US2 lands, because nobody can put anything in it yet — which is why both are P1 and ship
together, and why the MVP checkpoint is a demonstration rather than a release.

### Incremental Delivery

1. Phase 1 + 2 → the table and the entity exist and their invariants are proven
2. US1 → the catalogue is browsable → validate independently
3. US2 → the catalogue is maintainable → validate independently → the feature is usable
4. US3 → the invariants are proven through the endpoints a human uses, including under
   concurrency → validate independently
5. Phase 6 → documentation and the decision record, then the close-out gate

### Parallel Team Strategy

With two developers:

1. Both complete Phase 1 and 2 together — T014's adapter is the only contested file
2. Then split: **Developer A takes US1** (the public read path), **Developer B takes US2** (the
   write path). They touch different files: US1 owns the public handler and the catalogue use
   case, US2 owns the maintenance use case, the admin handler and the composition
3. `presentation/http/router.go` and `errors.go` are touched by both — sequence those two files
   rather than editing them in parallel
4. US3 follows US2, since it exercises US2's endpoints

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] labels map each task to a user story
- US1 is independent of US2; US3 is not, and that is stated rather than hidden
- Verify tests fail before implementing. A test that passes before its implementation exists is
  testing nothing, and T009 and T032–T035 are the ones most likely to be written that way by
  accident
- Commit after each phase, not after each task
- The two same-file overlaps are `presentation/http/router.go` and `presentation/http/errors.go`,
  shared by US1 and US2 — sequence them
- Avoid: vague tasks, same-file conflicts, and letting US3's invariant tests be reduced to an
  ASCII case that passes without proving the fold
