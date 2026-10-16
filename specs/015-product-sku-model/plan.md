# Implementation Plan: Product SKU Model

**Branch**: `015-product-sku-model` | **Date**: 2026-10-16 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/015-product-sku-model/spec.md`

## Summary

Restructure the catalogue from "each choice is its own product + a grouping table" (feature 014) into
a **parent product with child variants (SKUs)**: a product carries the shared content (name, code,
default price, category, description, slug, gallery) and owns one or more variants; a variant carries
its own name, SKU, optional price, image, order and **its own sell state**. Sales, stock and orders
move to the **variant** as the unit of sale: inventory is keyed by variant, the cart holds a chosen
variant, and an order line snapshots the product code and SKU it was sold under.

This is a **cross-module restructure**: it changes module 04 (product), module 05 (inventory), module
06 (cart), module 07 (order) and the cross-module contracts in `internal/contracts`, plus one
migration with backfill. Feature 014's grouping structures, routes and docs are removed.

## Technical Context

**Language/Version**: Go 1.26 (module `github.com/mavil-chudomirovich/artist-shop-be`)

**Primary Dependencies**: `go-chi/chi`, `jackc/pgx/v5`, `pressly/goose` (embedded migrations),
`google/uuid`, `swaggo` (Swagger from annotations), `redis` (shared cache — used by cart/session, not
changed here)

**Storage**: PostgreSQL; Cloudinary for media. A product's gallery reuses `product_images`; each
variant's single image is stored as a media reference on the variant row (only a reference, never
bytes).

**Testing**: `go test` for unit; the Postgres integration harness in
`internal/share/testsupport/postgres.go` for repository/integration tests; run through `make test` and
`make test-integration` (AGENTS.md §3 gate). **Note:** the integration harness needs Docker;
it is unavailable on the current Windows host, so integration tests will be written and compiled but
must be executed where Docker exists.

**Target Platform**: Linux server (Docker image, VPS); REST API under `/api/v1`

**Project Type**: web-service (Go modular monolith, four layers per module)

**Performance Goals**: keep the module rule that a list read is one query per page (no N+1). The
public catalogue (one entry per product with its lowest price and default image) is one statement; the
cart and checkout keep their existing bulk-read shape, now keyed by variant.

**Constraints**: `domain` imports only stdlib + `share/access` + `google/uuid`; cross-module reads go
through `internal/contracts`; money is integer minor units + currency; important data constraints live
at the storage layer; application owns transactions; every endpoint carries Swagger annotations and
`docs/swagger/` is regenerated (`make swagger`).

**Scale/Scope**: the whole catalogue and its downstream (cart/order/inventory) change shape; ~4
modules, 4 contracts, 1 migration (with backfill). No new external dependency, no new module.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- [x] **Modular Monolith & Clean Architecture**: the variant is a product-domain entity, so the
      `product_variants` model/port/adapter live in module 04. Inventory, cart and order keep their
      layers; every cross-module read/write moves through `internal/contracts`, whose shape is updated
      deliberately. No module reads another's tables.
- [x] **Transactional Integrity**: moving sell state with stock runs in one transaction and writes an
      `inventory_transactions` record (as today, now per variant); checkout/reservation stay
      all-or-nothing. Money stays integer minor units + currency (a variant's optional price inherits
      the product's currency).
- [x] **State Machines & Invariants**: the variant sell state is an explicit transition table with
      positive and negative tests (spec FR-015, reused from feature 006/014); the product has no state
      machine of its own. New invariants (product code unique, SKU unique, ≥1 variant) are explicit and
      tested.
- [x] **Test-First Critical Logic**: pricing (variant effective price), inventory (per-variant stock)
      and the sell-state transitions are written test-first; the existing pricing/inventory/order tests
      are updated to the variant key rather than dropped.
- [x] **Security & Least Privilege**: every product/variant admin route is behind the ADMIN guard; the
      acting account comes from the session; mutations are audited. Customer-owned resources (cart) are
      still addressed by the session's owner.
- [x] **Observability**: variant and product mutations emit audit events through module 04's auditor;
      structured logs and correlation IDs are inherited; no PII added.
- [x] **Simplicity (YAGNI)**: no option matrix; a variant is a flat child with a single image; no new
      infrastructure. The one genuinely large thing is the migration/backfill and the contract change
      — recorded in Complexity Tracking.
- [x] **Frontend Integration Guide**: this feature changes client-facing contracts extensively (the
      catalogue response, the product detail, cart add/read, order lines; and it **removes** feature
      014's group routes), so `specs/015-product-sku-model/frontend-guide.md` MUST ship with before →
      after for each change, and feature 014's guide is superseded.
- [x] **Swagger annotations**: every new/changed/removed handler is annotated and `docs/swagger/` is
      regenerated (`make swagger`); `make swagger-check` must pass.

## Project Structure

### Documentation (this feature)

```text
specs/015-product-sku-model/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/
│   ├── openapi.yaml     # Phase 1 output — new/changed/removed HTTP contract
│   └── error-codes.md   # Phase 1 output
├── frontend-guide.md    # close-out artifact
├── deferred.md          # out-of-scope findings
└── tasks.md             # Phase 2 output (/speckit.tasks)
```

### Source Code (repository root)

Four modules and the contracts change.

```text
internal/contracts/
├── product.go     # ProductCatalog → variant facts; ProductLookup → variant existence
├── inventory.go   # availability + reservation keyed by variant
├── cart.go        # CartLine carries the chosen variant + snapshots
└── (order.go)     # unchanged; order still keyed by order id

internal/modules/product/
├── domain/model/          # + Variant entity + sell-state machine; Product loses state/flags, gains code
├── domain/repository/     # + VariantRepository; product reads become variant-aware
├── domain/constant|error/ # + variant codes/sentinels; drop preorder/set codes
├── application/           # variant use cases; product create forces ≥1 variant; catalogue reads
├── infrastructure/implement/postgres/  # + variant adapter; product adapter updated
├── infrastructure/implement/catalog|availability/  # adapters rebuilt on variant facts
└── presentation/http|dto/ # product + variant routes; remove group routes

internal/modules/inventory/     # stock/holds/transactions repointed to variant; availability/reservation
internal/modules/cart/          # line keyed by variant; add/read/checkout consult variant facts
internal/modules/order/         # order line snapshots product code + sku; checkout keyed by variant

migrations/
├── 00013_product_variants.sql        # (existing, feature 014 — superseded)
└── 00014_product_sku_model.sql       # NEW: create variants, add product code, backfill, drop 014

docs/
├── api-reference.md, modules/04-product.md, modules/05-inventory.md, modules/06-cart.md, modules/07-order.md
├── decisions/021-product-sku-model.md  # new ADR superseding 020
└── swagger/                          # regenerated
```

**Structure Decision**: the restructure is contained to modules 04–07 plus `internal/contracts`; no new
module, no new shared package, no new external dependency. The variant is a product-domain entity
because "a product offered in several SKUs" is a product concept, and every downstream module already
reasons in terms of a sellable unit, which now becomes the variant.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| Cross-module contract change: `ProductCatalog`, `ProductLookup`, `InventoryAvailability`, `InventoryReservation` and `CartLine` are re-keyed from product to variant | The unit of sale becomes the variant (spec Clarifications: stock per variant, cart/order per variant), so every contract that carried a product id must carry the variant's identity and facts instead | Keeping the contracts on product ids would force the cart/order to hold a product and separately guess the variant, duplicating identity and losing the SKU on the order line — the exact defect the spec removes |
| A data migration with backfill that also **drops** feature 014's structures and the product sell-state/preorder/set columns | The spec replaces 014; existing products must gain a code and one default variant before the old columns can be removed | An additive-only migration would leave two variant mechanisms and a product-level sell state that contradicts the per-variant state — the model the spec rejects |
| The product module now serves a per-variant availability read to the public product page | FR-003/FR-005 require showing each variant's stock status; inventory owns stock, so the product surface needs a read across the boundary | Reading inventory's table directly is forbidden (Constitution I); computing availability on the product side would duplicate stock |
| Removed endpoints: feature 014's `/product-groups` and `/admin/product-groups` routes, **and feature 006's product-level `POST /admin/products/{id}/state`** | 014's grouping is superseded, and the product no longer has a sell state (it moved to the variant), so its state endpoint is replaced by the variant state endpoint | Keeping the product state endpoint would serve a product field that no longer exists, and leaving its doc/annotation would violate Constitution VIII |
| `google/uuid` in `domain` (the `Variant` entity and its product key) | Constitution I requires the UUID exception for primary keys to be recorded; the new domain entity carries UUID ids like the existing ones | Inventing a second ID type per module would duplicate the concept with no gain (the recorded reason the exception exists) |

## Phase 0 / Phase 1 Artifacts

- **research.md** — decisions D1–D14 (product/variant model, columns kept/added/dropped, variant
  image, per-variant sell state + auto-transition, inventory repoint, cart re-key, order snapshot,
  availability read, migration/backfill order, 014 removal, contract changes, unchanged areas).
- **data-model.md** — `product_variants` table + `products` column changes + inventory/cart/order table
  changes + the migration/backfill sequence + constraints/indexes.
- **contracts/openapi.yaml** — the new/changed/removed HTTP contract.
- **contracts/error-codes.md** — new/changed module error codes.
- **quickstart.md** — end-to-end validation scenarios mapped to the spec's acceptance scenarios.
