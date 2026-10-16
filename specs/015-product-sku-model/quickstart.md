# Quickstart — Product SKU Model (015)

A run/validation guide proving the restructure end-to-end. It does not restate the contract (see
[contracts/openapi.yaml](./contracts/openapi.yaml)) or the schema (see [data-model.md](./data-model.md)).

## Prerequisites

- Local stack up (see `docs/getting-started.md` / `docs/docker.md`); `make migrate` runs migrations
  **including `00014_product_sku_model.sql`** (which also backfills existing products).
- An ADMIN account + token (`docs/development/api-testing.md`), and a visible category.
- **Note:** the integration suite needs Docker; where it is unavailable, run the gates in CI/Linux.

## Automated gates

| Command | Proves |
|---|---|
| `make lint` | format + vet + golangci-lint clean |
| `make test` | unit tests: variant model, per-variant price fallback, sell-state transitions, ≥1-variant rule, code/SKU uniqueness |
| `make test-integration` | the migration applies and the repoint works against PostgreSQL (merged catalogue, per-variant stock, cart/order by variant) |
| `make swagger` then `make swagger-check` | every new/changed/removed endpoint is annotated and `docs/swagger/` matches |
| `make check` | the whole close-out gate before push |

## Scenario A — catalogue shows one entry and buys the chosen variant (spec US1, SC-001, SC-002)

1. Create a product with two variants (Scenario E) that differ in price, stock and image.
2. Move one variant to `ACTIVE`: `POST /api/v1/admin/products/{id}/variants/{variantId}/state` with
   `{ "to": "ACTIVE" }` — a new variant starts `COMING_SOON`, so without this the product is not yet on
   the catalogue (FR-004).
3. `GET /api/v1/products` — the product appears **once** (not once per variant); the card price is the
   **lowest** on-sale variant price; the card image is the **default (first)** variant's image.
4. `POST /api/v1/cart/items` with that variant's `variantId` and a quantity — the line is that variant
   at its own price.
5. `POST /api/v1/cart/items` with **no** `variantId` → `400 VALIDATION_ERROR` (a product cannot be
   added without choosing a variant).

## Scenario B — product detail lists variants with status (spec US1.2, FR-003/FR-005)

`GET /api/v1/products/{slug}` returns the product's shared content and its on-sale / out-of-stock
variants, each with `name`, `price`, `sellState`, `inStock`, ordered by display order; the default
variant is the first. A `COMING_SOON` or `DISCONTINUED` variant is not returned. No `sku` is exposed.

## Scenario C — variants are independent (spec US3, SC-003, SC-004)

1. Give two variants different prices and different stock.
2. Sell one out via the cart/checkout flow — only that variant becomes `OUT_OF_STOCK` / unbuyable; the
   sibling stays on sale.
3. A variant whose `price` is null charges the product's **default price**; a variant with a price
   overrides it.

## Scenario D — product presence follows its on-sale variants (spec Clarifications)

1. Take every variant out of stock (or `DISCONTINUED`) — the product leaves `GET /api/v1/products`.
2. Put one variant back on sale — the product reappears once.
3. An out-of-stock variant of a shown product still appears on the detail, marked out of stock.

## Scenario E — admin manages a product and its variants (spec US2, FR-009–FR-013)

1. `POST /api/v1/admin/products` with product fields **and `variants` (≥1)** → `201`.
2. `POST .../variants` adds a variant; `PATCH .../variants/{variantId}` edits name/sku/price/order;
   the sell state changes through `POST .../variants/{variantId}/state` (transitions keep their own
   endpoint); the image through `POST`/`DELETE .../variants/{variantId}/image`;
   `DELETE .../variants/{variantId}` removes one.
3. Duplicate `code` → `409 PRODUCT_CODE_TAKEN`; duplicate `sku` → `409 VARIANT_SKU_TAKEN`.
4. Removing the product's **last** variant → `409 VARIANT_LAST_REMOVAL`.
5. A customer token on any of these routes → `403 FORBIDDEN`, and the attempt is audited.

## Scenario F — existing products migrated (spec US4, SC-005)

1. Apply `00014`. Each pre-existing product has a `code` equal to its old slug and exactly one default
   variant (`sku = code`, price = the old product price, sell state = the old product state).
2. The migrated product appears and sells as before; its variant can be edited/replaced.

## Scenario G — storage-level invariants (spec FR-023, SC-006)

Attempt these directly against storage: a duplicate `lower(btrim(code))`, a duplicate
`lower(btrim(sku))`, a non-positive variant price, an unknown sell state — each must be refused by the
database, not only by application code. Inventory tables must be keyed by `variant_id`; no `product_id`
column remains in `stock_levels`/`inventory_transactions`/`stock_holds`/`cart_items`.

## Scenario H — an order snapshots the code and SKU (spec FR-019/FR-020, SC-003)

Check out a cart holding two variants of one product → the order has **two** lines, each with
`product_name`, `product_code`, `variant_name`, `sku`, `unit_price`, `quantity`. The customer's order
view shows "product name + variant name".

## Out of scope (do not test here)

Combo set, pre-order, multi-dimension options (colour × size) — each recorded as deferred/superseded.
