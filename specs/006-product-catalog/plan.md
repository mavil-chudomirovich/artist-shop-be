# Implementation Plan: Product Catalogue

**Branch**: `006-product-catalog` | **Date**: 2026-10-07 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/006-product-catalog/spec.md`

## Summary

Module 04 Product: an operator keeps the shop's products — physical items, combo sets and
pre-orders — with their pictures, prices, categories and selling life; a visitor reads the ones
that are on offer. It is the first module in phase 1 and the thing the cart, the order and the
inventory all stand on.

Two things make this module more than a repeat of module 03. First, it **pays two debts feature
005 deliberately left here**: the reference from a product to its category, with the restricting
delete that finally makes "a category with products cannot be removed" true at the storage layer,
and the "products by category" listing. Second, it is the **first module to need a capability that
another module already owns** — image storage — so this feature is where that capability moves to
the shared area the constitution requires, rather than becoming a second copy.

## Technical Context

**Language/Version**: Go 1.26.0

**Primary Dependencies**: Standard library plus what the foundation already provides — the chi
router, pgx, the shared response/error envelope, the shared audit writer and the shared generic
repository. **No new dependency.** `go.mod` and `go.sum` stay untouched; the image bytes are
posted to the media provider with `net/http` and `mime/multipart`, exactly as the existing media
adapter already does.

**Storage**: PostgreSQL 16, one additive migration `migrations/00006_product.sql` creating three
tables and the foreign key module 03 could not create. Redis is not involved: nothing here is
cached, rate-limited per module, or short-lived.

**Testing**: `testing` with `net/http/httptest` for the handler and router layers; in-memory fakes
for the use cases; `testcontainers-go` behind the `integration` build tag for the adapter —
including the two guarantees a fake cannot prove: the restricting delete that blocks removing a
category with products, and the single-primary-picture rule.

**Target Platform**: Linux server (Docker) in production, local development on Windows via
Docker Desktop.

**Project Type**: web-service

**Performance Goals**: Both public reads answer inside the platform's existing response target.
The catalogue is a shop's own inventory — hundreds to low thousands of products, not millions — so
the goal is the platform's standing one rather than a new number invented for this module.

**Constraints**: Prices are **integer minor units plus an explicit currency**; floating point is
never used for money. A product belongs to **exactly one** category. Pictures are stored by the
media provider and only a reference is kept. Writes are administrator-only; both public reads are
unauthenticated. Both lists are paginated per the project's list convention. No dedicated module
rate limit. **No stock quantity** is stored: the sell state is an operator's act until module 05
Inventory exists.

**Scale/Scope**: Three new tables, one foreign key added to a table module 03 owns, eleven
endpoints (two public, nine administrator), one new module, one refactor of an existing
capability into `internal/share/`, and the documentation that must move with all of it.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-checked after Phase 1 design.*

- [x] **I. Modular Monolith & Clean Architecture**: A new module at `internal/modules/product`
      with the four layers the project uses (`domain`, `application`, `infrastructure`,
      `presentation`), mirroring module 03. `domain` imports only the standard library,
      `internal/share/access` and `github.com/google/uuid`; `application` depends on `domain` and
      its own ports; `infrastructure` implements those ports; `presentation` translates HTTP.
      Two consequences are specific to this feature and both are recorded rather than assumed:
      the image capability **moves** to `internal/share/media` because two modules now need it
      (Complexity Tracking), and product visibility needs a fact the category module owns, which
      is reached through a new `internal/contracts` interface rather than by reading another
      module's table (research D1).
- [x] **II. Transactional Integrity**: Prices are integer minor units with an explicit currency,
      never floats (FR-030). No inventory is touched, so the `inventory_transactions` rule does
      not apply here — and that is a consequence of the deliberate decision to keep stock out of
      this module (spec FR-038). One operation does span more than one write: adding or removing a
      picture changes the picture rows **and** the product's primary-picture choice, so it runs
      inside the `UnitOfWork` port, which is also what makes the ten-picture ceiling race-free
      (research D6).
- [x] **III. State Machines & Invariants**: The sell state is an explicit enumeration with the
      allowed transitions defined in the domain and refused everywhere else (FR-021 to FR-025);
      the transition set was settled by clarification rather than guessed, because this principle
      is exactly why a default nobody confirmed is not good enough. Uniqueness of the slug is a
      catalogue-wide invariant, so it is enforced at the storage layer (FR-033), as module 03
      does.
- [x] **IV. Test-First for Critical Logic**: The critical logic here is the sell-state transitions
      (one positive and one negative per transition), the price rule, the slug rule and the
      storage-level guarantees. Each is written as a failing test first. The two guarantees a fake
      cannot prove — the restricting delete and the single-primary rule — are proven against real
      PostgreSQL.
- [x] **V. Security & Least Privilege**: Writes are refused unless the caller holds the
      administrator role, and the role comes from the session, never from the request. Uploads
      are validated by content signature, bounded in size and bounded in count before a byte
      reaches the provider (FR-019, FR-020). The public surface reveals nothing about hidden
      products or hidden categories: both answer the same not-found an unused identifier answers
      (FR-003, FR-006). No new credential, no new secret.
- [x] **VI. Observability**: Every create, edit, picture change, state change and removal writes
      an audit entry naming the product and the administrator (FR-013), reusing the shared audit
      writer. The public reads are not audited: they are unauthenticated browses, and recording
      every visitor would create a privacy problem the module does not need to solve.
- [x] **VII. Simplicity (YAGNI)**: No variants, no multi-language content, no stock, no reorder
      endpoint, no second source of truth for the catalogue. A combo set is a product with a price
      the operator sets rather than a priced graph, because that is the smaller model and the one
      the operator asked for. Every one of these is a decision recorded in `research.md`, not an
      omission.
- [x] **VIII. API Documentation as a Contract**: The eleven endpoints, the new error codes and the
      two changes to module 03's own behaviour (the restricted delete and its new code) update
      `docs/api-reference.md`, this feature's OpenAPI contract and the module documents in the
      same change as the code, never after it.
- [x] **Frontend Integration Guide**: This feature adds client-facing endpoints and DTOs, so
      `specs/006-product-catalog/frontend-guide.md` is a required deliverable, enumerating each
      endpoint as new and giving the shape of each response. It is re-read and corrected against
      the source in the final phase and after any convergence pass.

**Post-design re-check**: all nine still hold. Nothing in Phase 0 or Phase 1 forced a violation.
Complexity Tracking records the two deliberate design choices below; neither breaks a principle,
and each is recorded because it is the kind of choice a later reader would otherwise question.

## Project Structure

### Documentation (this feature)

```text
specs/006-product-catalog/
├── plan.md              # This file
├── spec.md              # Feature specification
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/
│   ├── openapi.yaml     # The eleven endpoints
│   └── error-codes.md   # Every code this module can answer with
├── frontend-guide.md    # Required by the constitution: the client-facing hand-off
├── deferred.md          # What this feature deliberately does not carry
└── checklists/
    └── requirements.md  # Spec quality checklist
```

### Source Code (repository root)

```text
internal/modules/product/
├── domain/
│   ├── constant/
│   │   ├── audit.go          # the audit actions this module records
│   │   ├── codes.go          # the PRODUCT_* machine codes
│   │   └── sellstate.go      # the four sell states and their names
│   ├── error/errors.go       # the module's business sentinel errors
│   ├── model/
│   │   ├── product.go        # the entity, its transitions and its invariants
│   │   ├── price.go          # integer minor units plus currency
│   │   ├── picture.go        # a stored reference, its position and its primary flag
│   │   └── slug.go           # the slug shape, the bounds and the folding key
│   └── repository/product.go # the persistence contract, no transaction inside it
├── application/
│   ├── dto/dto.go            # use-case input and output types
│   ├── interface/
│   │   ├── actor.go          # the acting administrator, from the session
│   │   └── ports.go          # the use-case surface, the media port, the visibility port
│   ├── implement/
│   │   ├── catalogue.go      # the public browse use cases
│   │   ├── maintenance.go    # the administrator product use cases
│   │   ├── pictures.go       # adding, removing and promoting a picture
│   │   └── lifecycle.go      # the sell-state transitions
│   └── mapper/mapper.go      # the single place model becomes dto
├── infrastructure/implement/
│   ├── postgres/product.go   # the adapter, explicit Columns projection
│   └── auditor/auditor.go    # the audit adapter over the shared writer
└── presentation/
    ├── dto/dto.go            # the three HTTP shapes: public, administrator, picture
    └── http/
        ├── handler.go        # request to use case, use case to response
        ├── router.go         # the two route groups
        └── errors.go         # sentinel to status, code and field detail

internal/share/media/         # MOVED here from module 02, so two modules share one capability
├── media.go                  # the port: Store and Reference
└── cloudinary.go             # the adapter, unchanged in behaviour

internal/contracts/category.go # NEW: the visibility question module 04 asks module 03

migrations/00006_product.sql   # three tables and the restricting foreign key
cmd/api/main.go                # mounts the module and wires the two new ports
docs/api-reference.md          # the eleven endpoints, plus module 03's new refusal
docs/modules/04-product.md     # status, spec pointer, completion criteria
docs/modules/03-category.md    # the completion criterion that is now enforceable
```

**Structure Decision**: A new module directory mirroring module 03, because module 03 is the most
recently completed module and therefore the cheapest pattern to follow and the easiest to review
against. Three things break the mirror deliberately:

1. **The image capability moves to `internal/share/media`.** Module 02 owns it today and module 04
   needs it. The constitution requires code shared by more than one module to live in the shared
   area, and the alternative — module 04 importing module 02's infrastructure — would couple two
   business modules directly. The move is a refactor of module 02 with its tests, not a rewrite.
2. **A cross-module contract appears in `internal/contracts/category.go`.** Product visibility
   depends on the category's display state, which module 03 owns, so this is the first genuine
   consumer of another module and the first entry in that package with a real implementer. It is
   created now because a consumer exists now, which is exactly the condition module 03's
   `deferred.md` D4 named.
3. **Module 03 changes in two small ways**, both forced by this feature and both additive: the
   restricting foreign key lives in module 04's migration but constrains module 03's deletes, and
   module 03's delete path must translate the resulting storage refusal into an answer the
   operator can act on (FR-036).

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| The media **port** lives in `internal/share/media`, not in a module's `application/interface` | Constitution I says external-service ports live in `application/interface`, and separately says code shared by more than one module lives in `internal/share/`. Once module 04 needs image storage these two rules meet, and only one arrangement satisfies both: one shared port, one shared adapter | Declaring an identical `MediaStore` interface in each module's `application/interface` and sharing only the adapter is more literally compliant with the port rule, and was rejected: two structurally identical interfaces with one implementation is the drift the project already refuses elsewhere (contract purity), and the second declaration would exist only to satisfy a rule about a port that is not the module's own. Recorded here because it is a deliberate reading of two principles that overlap |
| The public product list filters on a value the category module owns | FR-002 makes a product visible only if its category is visible, and FR-006 requires the filter to hold for a paginated list. Filtering in Go after the page is fetched would return short pages and a wrong `total`, so the filter has to be in the query | A join straight onto `categories.is_visible` is the obvious implementation and was rejected: Constitution I forbids one module reading another's tables, and a join would make module 03's schema part of module 04's query. The contract in research D1 answers the same question through the interface the constitution prescribes, and keeps the filter in SQL where pagination needs it |
| Module 03's delete path gains a new failure it did not have | The restricting foreign key this feature adds is what makes module 03's completion criterion true, and a storage refusal that surfaced as an unexplained server failure would satisfy FR-035 while failing FR-036 | Checking for products inside module 03 before deleting was rejected: it would mean module 03 reading module 04's table, which is the same violation from the other direction. Letting the database refuse and translating the refusal is both the smaller change and the one that cannot be bypassed |
