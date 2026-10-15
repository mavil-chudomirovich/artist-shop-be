# Implementation Plan: Product Variants

**Branch**: `014-product-variants` | **Date**: 2026-10-14 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/014-product-variants/spec.md`

## Summary

A product can be offered in several interchangeable choices — a group ("Card Genshin") with options
("Flins", "Dottore"), so a customer picks a choice and buys it, exactly as a shoe is picked by colour.
The feature adds a **variant group** as a first-class catalogue entity in the **product module (04)**,
and links existing products to a group as its **options**. Each option stays a full, independent
product — its own price, stock, sell state, pictures and link — so the cart, order, inventory,
payment and every cross-module contract (`internal/contracts/`) are **unchanged**: a customer still
adds a `productId`, and that id simply happens to be an option.

Technical approach: two new tables (`product_variant_groups`, `product_variant_group_images`) plus
two nullable columns on `products` (`variant_group_id`, `variant_label`); a merged public catalogue
list that unions groups and standalone products in one SQL statement so pagination and the total stay
correct; a group detail route and a group selector added to the product detail; a new administrator
surface for group CRUD and images, with attach/detach done through the existing product create/edit
payload. Nothing outside module 04 changes.

## Technical Context

**Language/Version**: Go 1.26 (module `github.com/mavil-chudomirovich/artist-shop-be`)

**Primary Dependencies**: `go-chi/chi` (routing), `jackc/pgx/v5` (PostgreSQL), `pressly/goose`
(migrations, embedded via `migrations/embed.go`), `google/uuid`, `swaggo` (Swagger generated from
handler annotations), `redis` (shared cache — not used by this feature)

**Storage**: PostgreSQL for catalogue data; Cloudinary for pictures (the group gallery reuses the
existing shared media capability exactly as a product's pictures do — only a reference is stored)

**Testing**: `go test` for unit; the Postgres integration harness in
`internal/share/testsupport/postgres.go` for repository/integration tests; run through `make test`
and `make test-integration` (AGENTS.md §3 gate: lint + test + test-integration)

**Target Platform**: Linux server (Docker image, VPS); REST API under `/api/v1`

**Project Type**: web-service (Go modular monolith backend, four layers per module)

**Performance Goals**: no special target; the hard rule this module already holds is that the public
list is **one query per page** (no N+1) with the whole visibility predicate in SQL. The merged list
must preserve that — one statement, not one query per entry or per group

**Constraints**: `domain` imports only stdlib + `share/access` + `google/uuid`; application owns
transaction boundaries; no module reads another module's tables; money is integer minor units +
currency; important data constraints live at the storage layer; every new endpoint carries Swagger
annotations and `docs/swagger/` is regenerated (`make swagger`)

**Scale/Scope**: a small artist shop's catalogue; ~1 migration, one module touched, no new
infrastructure, no new cross-module contract

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- [x] **Modular Monolith & Clean Architecture**: the group and its options are product-domain
      concepts, so everything lives in `internal/modules/product/{domain,application,infrastructure,presentation}`.
      The new entity follows the module's existing shape (model + repository port in `domain`,
      use cases in `application/implement`, adapter in `infrastructure/implement/postgres`, routes in
      `presentation/http`). No layer violation; `domain` gains only `google/uuid` (already allowed,
      Constitution I).
- [x] **Transactional Integrity**: attaching an option writes the product's `variant_group_id` and
      `variant_label` (one row, one statement); creating a group writes the group row, and images are
      written through the module's existing `UnitOfWork`. No money/inventory operation changes, so no
      new `inventory_transactions` obligation arises. Money on an option is the ordinary product price
      (integer minor units + currency) — unchanged.
- [x] **State Machines & Invariants**: the group has **no** state machine (no sell state, per
      Clarifications); options keep the existing `Product` sell-state machine untouched. The new
      invariants (label unique within a group, one group per product, set↔option exclusivity) are
      enforced at the storage layer and covered by positive/negative tests.
- [x] **Test-First Critical Logic**: the new invariants and the merged-list visibility predicate are
      written test-first; the existing sell-state and pricing tests are unchanged and must keep
      passing.
- [x] **Security & Least Privilege**: every group route is behind the ADMIN guard exactly as the
      existing `/admin/products` group; the acting administrator comes from the session, never the
      body; group/option mutations are audited. No secret handling changes.
- [x] **Observability**: group mutations emit audit events through the module's existing auditor;
      structured logs and correlation IDs are inherited from the foundation; no PII added.
- [x] **Simplicity (YAGNI)**: single choice dimension; no option matrix; no cross-module read; no
      microservice/WebSocket. The one place worth recording is **Complexity Tracking** below (a
      merged-list SQL union).
- [x] **Frontend Integration Guide**: this feature **changes client-facing contracts** (the public
      catalogue list and product detail gain fields; new group routes), so
      `specs/014-product-variants/frontend-guide.md` MUST ship, enumerating each endpoint with
      before → after. It is a close-out artifact and is listed as a task.
- [x] **Swagger annotations**: every new/changed handler carries `@Summary`/`@Tags`/`@Param`/
      `@Success`/`@Failure`/`@Router`, and `docs/swagger/` is regenerated with `make swagger` in the
      same change (`make swagger-check` must pass). Listed as close-out tasks.

## Project Structure

### Documentation (this feature)

```text
specs/014-product-variants/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/
│   ├── openapi.yaml     # Phase 1 output — new/changed HTTP contract
│   └── error-codes.md   # Phase 1 output — new module error codes
├── frontend-guide.md    # close-out artifact (Constitution §Governance)
├── deferred.md          # out-of-scope findings (created if any arise)
└── tasks.md             # Phase 2 output (/speckit.tasks — NOT created here)
```

### Source Code (repository root)

Only **module 04 (product)** changes behaviour. Existing files are extended; new files sit beside
them. Module 03 (category) gets a **comment-only** correction: the new
`product_variant_groups_category_fk` means its `classifyDeleteError` comment ("products_category_fk
is the only foreign key that points at categories") is no longer true, though the SQLSTATE-based
mapping still works unchanged — no behaviour change in module 03.

```text
internal/modules/product/
├── domain/
│   ├── model/
│   │   ├── product.go                # +VariantGroupID, +VariantLabel, +validation
│   │   └── variant_group.go          # NEW: VariantGroup entity + draft/edit + validation
│   ├── constant/
│   │   └── audit.go                  # + group audit actions
│   ├── error/
│   │   └── errors.go                 # + group/label/attachment sentinels
│   └── repository/
│       └── variant_group.go          # NEW: VariantGroupRepository port + read views
├── application/
│   ├── interface/
│   │   └── ports.go                  # + VariantGroupRepository alias; + group/catalogue methods on ProductService
│   ├── dto/
│   │   └── dto.go                    # + group in/out; public list entry gains a `type` discriminator
│   ├── mapper/
│   │   └── mapper.go                 # + group mappings; product detail gains the selector
│   └── implement/
│       ├── catalogue.go              # ListCatalogue (merged), group + option detail reads
│       ├── groups.go                 # NEW: group create/edit/delete/read use cases
│       └── maintenance.go            # product create/edit accept group attachment
├── infrastructure/implement/postgres/
│   ├── product.go                    # +variant columns; +ListCatalogue; +option read by group
│   └── variant_group.go              # NEW: VariantGroupRepository adapter
└── presentation/
    ├── http/
    │   ├── handler.go                # + group handlers; list/detail changes; annotations
    │   ├── router.go                 # + public group route; + admin group routes
    │   └── errors.go                 # + new code mappings
    └── dto/
        └── dto.go                    # + group request/response shapes

migrations/
└── 00013_product_variants.sql        # NEW (products ALTER + two tables + constraints)

docs/
├── api-reference.md                  # module 04 section: new/changed endpoints
├── modules/04-product.md             # scope note: one-dimension variants now delivered
└── swagger/                          # regenerated by `make swagger`
```

**Structure Decision**: single module (04 product). No new module; no `internal/contracts` change; no
`internal/share` change; no module 03 **behaviour** change (a comment/doc note only). The group is a
product-domain entity because "a product offered in several choices" is a product concept, not a new
capability. The group and catalogue use cases are methods on the module's single `Service` (no sibling
service), so the handler keeps consuming one `ProductService`.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| Public catalogue list reads two tables in one union (a CTE/UNION ALL over `product_variant_groups` and `products`) | The list must show one entry per group **and** one per standalone product in a single ordered, paginated result with a correct total (FR-001, FR-006, FR-027; SC-002). Merging two independently-paginated queries in Go cannot produce a correct `total` or a stable cross-kind order | "Two endpoints and let the client merge" returns two pages the client cannot interleave and paginate correctly, and violates the module's one-query-per-page rule; "materialise a view" is heavier than the union needs and adds a schema object to migrate |

No other principle is bent: the merged read is still one SQL statement with the whole predicate in
SQL, which is the pattern module 04 already uses.

## Phase 0 / Phase 1 Artifacts

- **research.md** — decisions D1–D14, each with rationale and rejected alternatives (representation,
  storage constraints, merged list, option ordering, slug namespaces, visibility predicate, group
  images, admin surface, set↔option exclusivity, group removal).
- **data-model.md** — the two new tables, the two new `products` columns, every constraint and index,
  the read projections, and the lifecycle/cleanup rules.
- **contracts/openapi.yaml** — the new and changed HTTP contract (public list, group detail, product
  detail selector, admin group CRUD + images).
- **contracts/error-codes.md** — the new module error codes and the codes deliberately reused.
- **quickstart.md** — end-to-end validation scenarios mapped to the spec's acceptance scenarios.
