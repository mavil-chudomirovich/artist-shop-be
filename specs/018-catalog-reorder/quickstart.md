# Quickstart — Catalog Reorder (Drag & Drop) (018)

Validation guide proving the feature end to end. It does not restate the contract (see
[contracts/openapi.yaml](./contracts/openapi.yaml) and
[contracts/error-codes.md](./contracts/error-codes.md)) or the schema (see
[data-model.md](./data-model.md)); it says how to run and what to expect.

## Prerequisites

- Local stack up (see `docs/getting-started.md` / `docs/docker.md`); `make migrate` runs the
  existing migrations — this feature adds **no** migration.
- An ADMIN account and token; a visible category, a product with at least three variants, and a
  second category/product so cross-scope cases can be exercised (see
  `docs/development/api-testing.md` for the four-checks convention).

## Automated gates

| Command | Proves |
|---|---|
| `make lint` | format + vet + golangci-lint clean |
| `make test` | unit: reorder validation (empty/duplicate/incomplete/foreign), auto-front placement, order applied |
| `make test-integration` | the bulk reorder and complete-set checks against PostgreSQL (needs Docker) |
| `make swagger-check` | passes after `make swagger` regenerates the three new handlers |

## Scenario A — reorder categories (spec US1, FR-001/FR-004/FR-013)

1. `GET /api/v1/admin/categories?all=true` → the complete ordered list; note the ids in order.
2. `PUT /api/v1/admin/categories/reorder` with the same ids in a new order → `200`,
   `data.updatedCount` equals the list size.
3. `GET /api/v1/admin/categories?all=true` and `GET /api/v1/categories` → both reflect the new
   order.

## Scenario B — reorder a product's variants (spec US1, FR-003/FR-014)

1. Put a variant that is not currently first at the front via
   `PUT /api/v1/admin/products/{id}/variants/reorder`.
2. `GET /api/v1/products/{code}` → the variants are listed in the new order and the new first
   variant is the default. On a product with no primary gallery picture, its image is the card
   fallback image.
3. Confirm the card **price** did not change unless the lowest on-sale variant price itself
   changed (price is the minimum across on-sale variants, not the first variant's price).

## Scenario C — refusals leave the order untouched (spec FR-005–FR-008, SC-003)

For each, read the list before and after to confirm nothing changed:

1. Empty array → `400 VALIDATION_ERROR`.
2. A repeated id → `400 CATEGORY_REORDER_DUPLICATE_ID` (or the product/variant duplicate code).
3. An array missing one sibling → `400 VALIDATION_ERROR`.
4. An unknown id → `404 CATEGORY_NOT_FOUND` (or `PRODUCT_NOT_FOUND`).
5. A variant id that belongs to a different product →
   `400 VARIANT_BELONGS_TO_OTHER_PRODUCT`.

## Scenario D — new records appear first (spec US2, FR-011/FR-012)

1. `POST /api/v1/admin/categories` without `position` → the new category is first in
   `GET /api/v1/admin/categories?all=true`.
2. `POST /api/v1/admin/products/{id}/variants` without `position` → the new variant is first and
   becomes the default.
3. `POST /api/v1/admin/categories` **with** `"position": 50` → that explicit value is honoured.
4. Create a product with two variants, both without `position` → they take positions in payload
   order (first payload variant is the default).

## Scenario E — whole-list fetch and its bound (spec US3, FR-015)

1. `GET /api/v1/admin/products?all=true` → every product in one ordered response.
2. Seed more than the safety maximum (1000) products → `all=true` answers `400 VALIDATION_ERROR`,
   while a normal paged request still works.

## Out of scope (do not test here)

Per the spec: per-category product ordering, optimistic-concurrency/version conflict detection,
and customer notifications. Variant/category/product visibility rules are unchanged — only
ordering is affected.
