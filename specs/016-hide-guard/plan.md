# Implementation Plan: Hide Guard with Pending Orders

**Branch**: `016-hide-guard` | **Date**: 2026-10-16 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/016-hide-guard/spec.md`

## Summary

Block hiding a category, product, or variant while a PENDING or PAYMENT_PENDING order contains it,
and remove the hidden variants from every cart when hiding succeeds. The check runs inside the same
 atomic unit of work as the hide through two new cross-module contracts (order answers which pending
 orders hold given variants; product answers which variants belong to given categories; cart exposes a
 bulk line-removal), plus a new `is_visible` flag on products. No new HTTP endpoint is added; three new
 409 codes name the blocking orders. The guard never applies to hard delete: removing a variant,
 product, or category outright keeps its existing rules unchanged.

## Technical Context

**Language/Version**: Go (per repo toolchain; no version decision needed)

**Primary Dependencies**: existing module layout only — no new libraries, no new services

**Storage**: PostgreSQL; one additive migration (`migrations/00016_product_visibility_flag.sql`);
Down reverses it

**Testing**: `go test` unit suites per module plus integration suites (`make test-integration`,
needs Docker)

**Target Platform**: Linux server (VPS) via Docker Compose; unchanged

**Project Type**: web-service (modular monolith, REST under `/api/v1`)

**Performance Goals**: the pending-order check and the cart cleanup are each one bounded bulk
statement regardless of variant count (spec SC-004); no per-variant round trips

**Constraints**: `domain` imports only stdlib + `share/access` + `github.com/google/uuid`;
repositories never open transactions; cross-module calls go through `internal/contracts` only

**Scale/Scope**: one migration, two contract extensions, one new contract method on cart, guarded
writes in three use cases, one new catalogue predicate term; category hiding of hundreds of variants
stays bounded

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- [x] **Modular Monolith & Clean Architecture**: new cross-module reads go through `internal/contracts`
      (`order.go`, `product.go`, `cart.go`); providing modules supply adapters at the composition root;
      four layers and the dependency rule are untouched.
- [x] **Transactional Integrity**: check-then-hide and cart cleanup run inside the hiding use case's
      existing UnitOfWork transaction; no money or stock rule changes.
- [x] **State Machines & Invariants**: no state machine changes; the variant transition table is reused
      as-is, with the guard applied to moves away from on-sale only.
- [x] **Test-First Critical Logic**: refusal/no-change and cleanup-count tests per story, including the
      system-initiated stock-out path.
- [x] **Security & Least Privilege**: hide actions stay behind the existing ADMIN guard; refusal detail
      (order IDs) is admin-surface only; no secrets committed.
- [x] **Observability**: hide mutations keep their existing audit actions; the blocked system signal and
  the cart-cleanup count are recorded, with correlation IDs and no PII.
- [x] **Simplicity (YAGNI)**: no new service, queue, or notification; bulk SQL instead of new machinery;
      deviations: none, Complexity Tracking stays empty (see below).
- [x] **Frontend Integration Guide**: no endpoint added, removed or changed and no DTO/event change, so
      per the template the feature states that in `tasks.md`; the three new 409 codes are documented in
      `docs/api-reference.md`.
- [x] **Swagger annotations**: no handler signature or route changes; `make swagger-check` must still pass
      unchanged; any `@Failure` gap found while mapping the new 409s is fixed in the same change.

## Project Structure

### Documentation (this feature)

```text
specs/016-hide-guard/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
internal/contracts/
├── order.go                 # + PendingOrdersForVariants (new interface)
├── product.go               # + VariantIDsByCategories (new interface)
└── cart.go                  # + RemoveVariantsFromAllCarts (new interface)

internal/modules/order/
├── domain/repository/order.go                    # + FindPendingOrdersByVariants
└── infrastructure/implement/postgres/order.go    # + bulk query implementation

internal/modules/product/
├── domain/model/product.go                       # + IsVisible field + validation
├── domain/constant/codes.go                      # + PRODUCT_HAS_PENDING_ORDERS, VARIANT_HAS_PENDING_ORDERS
├── domain/error/errors.go                        # + sentinels for the two refusals
├── domain/repository/product.go                  # + VariantIDsByCategories
├── application/implement/variants.go             # guard moves away from on-sale
├── application/implement/maintenance.go         # guard the flag edit
├── application/implement/catalogue.go            # (no change: predicate lives in SQL)
├── infrastructure/implement/postgres/product.go  # + is_visible predicate term + variant-ID query
├── infrastructure/implement/availability/availability.go  # funnel MarkVariantOutOfStock through the guarded transition (MarkVariantOnSale never checked)
└── presentation/http/errors.go                   # map the two new 409s

internal/modules/category/
├── domain/constant/codes.go                      # + CATEGORY_HAS_PENDING_ORDERS
├── domain/error/errors.go                        # + sentinel for the refusal
├── application/implement/maintenance.go         # + Tx/UnitOfWork + guard + cleanup call
└── presentation/http/errors.go                   # map the new 409

internal/modules/cart/
├── domain/repository/cart.go                     # + DeleteLinesByVariants
├── infrastructure/implement/postgres/cart.go     # + bulk DELETE implementation
└── presentation/http/errors.go                   # (no change: cleanup has no new refusal)

internal/modules/inventory/
└── application/implement/reconcile.go            # unchanged caller; blocked signal is a no-op + event

migrations/
└── 00016_product_visibility_flag.sql             # + products.is_visible, Down reverses it

docs/
├── api-reference.md                              # + three 409 codes on the affected admin endpoints
└── swagger/                                      # regenerated only if an annotation gap is found
```

**Structure Decision**: single-project modular monolith is kept as-is; all work lands in the existing
four-layer modules plus `internal/contracts`, following the exact layout above. No new module, no new
top-level directory, no `internal/platform` path.

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

No violations. No entries — every cross-module read uses an `internal/contracts` interface with a
provider-side adapter, transactions stay in `application` via the existing UnitOfWork port, and no new
infrastructure is introduced.

## Phase 0 / Phase 1 Artifacts

- **research.md** — decisions D1–D8, each with rationale and rejected alternatives (contract placement,
  bulk-query shape, guard placement and atomicity, cleanup ownership and audit, system-signal no-op,
  flag column and index choice, error shape, audit reuse).
- **data-model.md** — the product flag column, the unchanged tables it touches, the two bulk reads plus
  the bulk delete, validation rules from requirements, and why no other schema change is needed.
- **quickstart.md** — end-to-end validation scenarios mapped to the spec's acceptance scenarios.
- **contracts/error-codes.md** — the three new 409 codes and the refusal shape; no openapi.yaml changes
  (no endpoint added, changed or removed).
