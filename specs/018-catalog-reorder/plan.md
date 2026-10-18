# Implementation Plan: Catalog Reorder (Drag & Drop)

**Branch**: `018-catalog-reorder` | **Date**: 2026-10-18 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/018-catalog-reorder/spec.md`

## Summary

Let an administrator reorder categories (one catalogue-wide order), products (one
catalogue-wide order) and a product's variants (ordered within the product) by drag & drop,
saving the whole new order in one request. Three new admin endpoints accept the complete,
ordered set of identifiers and apply it with a single bulk `UPDATE ... FROM UNNEST(...)`
statement (no per-row round trips). The `position` field becomes optional on **create**:
omitting it places the new record at the front of its list (`MIN(position) - 1`), while an
explicit value is still honoured. An edit keeps the record's current order (the update DTO is
already optional and unchanged). Admin lists gain an `all` selector so the whole list can be
fetched for reordering, bounded by a documented safety maximum. No schema migration is needed —
the existing `position` columns and ordering indexes are reused. Request-shape, duplicate-ID,
foreign-ID and incomplete-set conditions are refused with dedicated codes, and every reorder
records one bulk audit event.

## Technical Context

**Language/Version**: Go (per repo toolchain; no new version decision)

**Primary Dependencies**: existing module layout only — no new library, no new external service

**Storage**: PostgreSQL; **no migration** — `categories.position`, `products.position`,
`product_variants.position` and their ordering indexes already exist
(`categories_ordering_idx`, `products_ordering_idx`, `product_variants_product_idx`)

**Testing**: `go test` unit suites per module plus integration suites (`make test-integration`,
needs Docker)

**Target Platform**: Linux server (VPS) via Docker Compose; unchanged

**Project Type**: web-service (modular monolith, REST under `/api/v1`)

**Performance Goals**: each reorder is one bounded statement regardless of list size (spec
SC-005); the auto-front create is one read plus one insert (no full-table shift); the complete
list fetch is bounded by a documented safety maximum

**Constraints**: `domain` imports only stdlib + `share/access` + `github.com/google/uuid`;
repositories never open transactions (the existing `UnitOfWork` port owns the boundary);
no cross-module table reads

**Scale/Scope**: 3 new admin endpoints; 3 new bulk writes; 6 touched use cases (three reorders
plus auto-front on the three creates); `position` becomes `*int` (optional) on the create DTOs;
one `all` query parameter on two admin lists; 4 new error codes; docs + swagger regenerated

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- [x] **Modular Monolith & Clean Architecture**: all work lands in the existing `category` and
      `product` four-layer modules; new repository methods are declared in `domain/repository`
      and implemented in `infrastructure`; no new module, no cross-module table read; product's
      variant scoping uses its own `VariantRepository`.
- [x] **Transactional Integrity**: reorder and the auto-front create each run in a single
      transaction through the `UnitOfWork` port; no money/stock rule changes; the category
      service reuses the `Tx` port already wired for the hide guard (no new wiring).
- [x] **State Machines & Invariants**: no lifecycle/state-machine change; the variant transition
      table is untouched. The domain invariant "a reorder carries the complete set of siblings"
      is enforced in the use case.
- [x] **Test-First Critical Logic**: the ordering writes, the complete-set/duplicate/foreign
      refusals, the auto-front placement and the guest/customer/admin boundary of the three new
      routes get failing tests first (unit + integration).
- [x] **Security & Least Privilege**: all three reorder routes sit behind the existing ADMIN
      guard; the actor is taken from the session, never the body; request arrays are validated
      (non-empty, UUID, no duplicates) so no unbounded or malformed write is accepted.
- [x] **Observability**: every reorder records one audit event (actor, affected scope, count)
      with no PII; structured logs carry the correlation ID via the existing middleware.
- [x] **Simplicity (YAGNI)**: bulk SQL instead of per-row writes; `min(position)-1` instead of a
      full shift; no versioning/concurrency machinery (last-write-wins, accepted in spec
      Clarifications); no new index or table. Deviations: none — Complexity Tracking stays empty.
- [x] **Frontend Integration Guide**: this feature adds three endpoints and changes two create
      DTOs' contract (position now optional) and two admin lists (new `all` selector), so it
      ships `specs/018-catalog-reorder/frontend-guide.md` with before → after for each change.
- [x] **Swagger annotations**: the three new handlers carry `@Summary/@Tags/@Param/@Success/
      @Failure/@Router`, the touched handlers keep theirs, and `docs/swagger/` is regenerated
      with `make swagger` in the same change so `make swagger-check` passes.

## Project Structure

### Documentation (this feature)

```text
specs/018-catalog-reorder/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── frontend-guide.md    # Phase 1 close-out artifact (Constitution §Governance)
├── contracts/           # Phase 1 output (/speckit.plan command)
│   ├── error-codes.md
│   └── openapi.yaml
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
internal/modules/category/
├── domain/constant/codes.go                       # + CATEGORY_REORDER_DUPLICATE_ID
├── domain/constant/audit.go                       # + AuditCategoryReordered
├── domain/error/errors.go                         # + ErrCategoryReorderDuplicateID
├── domain/repository/category.go                  # + Reorder, ListIDs, NextFrontPosition
├── infrastructure/implement/postgres/category.go  # + implementations (UNNEST / MIN / ids)
├── application/dto/dto.go                          # CreateCategoryInput.Position *int; + ReorderCategoriesInput, ReorderResult
├── application/interface/ports.go                  # + ReorderCategories
├── application/implement/maintenance.go            # auto-front create + ReorderCategories use case
├── presentation/dto/dto.go                         # CreateCategoryRequest.Position *int; + ReorderCategoriesRequest, ReorderResponse
├── presentation/http/handler.go                    # + ReorderCategories handler; `all` on ListAdmin
├── presentation/http/errors.go                     # + map CATEGORY_REORDER_DUPLICATE_ID (400)
└── presentation/http/router.go                     # + PUT /reorder

internal/modules/product/
├── domain/constant/codes.go                       # + PRODUCT_REORDER_DUPLICATE_ID, VARIANT_REORDER_DUPLICATE_ID, VARIANT_BELONGS_TO_OTHER_PRODUCT
├── domain/constant/audit.go                        # + AuditProductReordered, AuditProductVariantsReordered
├── domain/error/errors.go                          # + ErrProductReorderDuplicateID, ErrVariantReorderDuplicateID, ErrVariantBelongsToOtherProduct
├── domain/repository/product.go                    # + Reorder, ListIDs, NextFrontPosition
├── domain/repository/variant.go                    # + Reorder, ListIDsByProduct, NextFrontPosition
├── infrastructure/implement/postgres/product.go    # + implementations (UNNEST / MIN / ids)
├── infrastructure/implement/postgres/variant.go    # + implementations (UNNEST scoped by product / MIN / ids)
├── application/dto/dto.go                           # create Position *int; + ReorderProductsInput, ReorderVariantsInput, ReorderResult
├── application/interface/ports.go                   # + ReorderProducts, ReorderVariants
├── application/implement/maintenance.go             # CreateProduct auto-front + ReorderProducts
├── application/implement/variants.go                # AddVariant auto-front + ReorderVariants
├── presentation/dto/dto.go                          # create Position *int; + ReorderProductsRequest, ReorderVariantsRequest, ReorderResponse
├── presentation/http/handler.go                     # + ReorderProducts, ReorderVariants handlers; `all` on ListAdmin
├── presentation/http/errors.go                      # + map the three new codes (400)
└── presentation/http/router.go                      # + PUT /reorder, + PUT /{id}/variants/reorder

cmd/api/main.go                                      # unchanged — Tx already wired for the hide guard (T006 confirms)

docs/
├── api-reference.md                                 # + three endpoints, `all` selector, optional position, new codes
├── modules.md                                       # ordering semantics note if it describes positions
└── swagger/                                         # regenerated with `make swagger`
```

**Structure Decision**: single-project modular monolith is kept as-is. All work lands in the
existing `category` and `product` modules plus their composition (`cmd/api/main.go`), following
the exact layout above. No new module, no new top-level directory, no migration.

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

No violations. No entries — reorder uses one bulk statement per list, auto-front uses one read
plus one insert, transactions stay in `application` via the existing UnitOfWork port, and no new
infrastructure, table or index is introduced.

## Phase 0 / Phase 1 Artifacts

- **research.md** — decisions D1–D12, each with rationale and rejected alternatives (bulk SQL
  shape, validation order, auto-front placement, ordering scope, complete-set rule, DTO
  optionality, error codes, audit, full-list fetch cap, concurrency, no-migration, default
  variant effect).
- **data-model.md** — the reused `position` columns and ordering indexes, the "smallest is
  default" rule, the new bounded reads/writes, and the validation rules from the spec.
- **contracts/openapi.yaml** — the three new endpoints and the changed request shapes.
- **contracts/error-codes.md** — the four new codes, their HTTP status and reused codes.
- **quickstart.md** — end-to-end validation scenarios mapped to the spec's acceptance scenarios.
- **frontend-guide.md** — every affected endpoint (new/changed) with before → after for the
  frontend hand-off.
