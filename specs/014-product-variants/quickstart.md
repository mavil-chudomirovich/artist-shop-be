# Quickstart — Product Variants (014)

A run/validation guide proving the feature end-to-end. It does not restate the contract (see
[contracts/openapi.yaml](./contracts/openapi.yaml)) or the schema (see
[data-model.md](./data-model.md)); it says how to run and what to expect.

## Prerequisites

- Local stack up: `make env` once, then the dev stack with PostgreSQL (see `docs/getting-started.md`
  / `docs/docker.md`). Migrations run with `make migrate` (or the `migrate` compose service).
- An ADMIN account and an access token (see `docs/development/api-testing.md`).
- At least one visible category and one product to attach as an option
  (`POST /api/v1/admin/categories`, `POST /api/v1/admin/products`).

## Automated gates

| Command | Proves |
|---|---|
| `make lint` | format + vet + golangci-lint clean |
| `make test` | unit tests, including the new domain invariants and use cases |
| `make test-integration` | the migration applies; the merged list, group reads and constraints work against PostgreSQL |
| `make swagger` then `make swagger-check` | every new/changed endpoint's annotations match the regenerated `docs/swagger/` |
| `make check` | the whole close-out gate before push/PR |

## Scenario A — a group appears once and buys the chosen option (spec US1, SC-001, SC-002)

1. Create a group (ADMIN):
   `POST /api/v1/admin/product-groups` with `{ name, slug, categoryId, position, optionName: "Card", description }`.
   Expect `201` with a stable `id`.
2. Attach two products as options (ADMIN), each a `PATCH /api/v1/admin/products/{id}` with
   `{ "variantGroupId": "<group id>", "variantLabel": "Flins" }` (and `"Dottore"` for the other).
   Expect `200`; the products' price/pictures/state are unchanged.
3. `GET /api/v1/products` — the group appears **once** as an entry with `type: "variant-group"`; the
   two options do **not** appear as separate entries.
4. `GET /api/v1/product-groups/{group-slug}` — returns the group's shared content, its gallery, and
   two options each with `label`, `price`, `imageUrl`.
5. Add an option to the cart as a customer (`POST /api/v1/cart/items` with that option's product id)
   and confirm the cart line is that option at its own price.

## Scenario B — an option's link resolves with the selector (spec US1.5, FR-007)

`GET /api/v1/products/{option-slug}` returns the product detail **plus** `variantGroup` (the group's
shared content and both sibling options), so a shared link to one option lets the customer switch.

## Scenario C — options are independent (spec US3, SC-003)

1. Give the two options different prices (`PATCH /api/v1/admin/products/{id}` `price`).
2. `GET /api/v1/product-groups/{slug}` shows each option's own price; the group's list price is the
   lowest visible option's price (spec Q3).
3. Stock one option low, buy it out via the normal cart/checkout flow, and confirm only that option
   becomes unbuyable while the sibling stays buyable.

## Scenario D — group presence follows its options (spec US4, FR-006)

1. Take every option off sale (`POST /api/v1/admin/products/{id}/state` → `DISCONTINUED` or
   `OUT_OF_STOCK`) or detach them.
2. `GET /api/v1/products` no longer lists the group; `GET /api/v1/product-groups/{slug}` answers
   `404 VARIANT_GROUP_NOT_FOUND` (the same answer an unknown slug gives).
3. Make one option on sale again; the group reappears once.

## Scenario E — invariants are enforced at storage (spec FR-021, FR-022, FR-024, SC-004)

Run these and expect refusals:

- `PATCH` a second option to the label an existing option already has → `409 VARIANT_LABEL_TAKEN`.
- Attach a product that is already an option of another group → `409 PRODUCT_ALREADY_IN_VARIANT_GROUP`.
- Attach a product that `isSet: true`, or set `isSet: true` on an option → `400 VALIDATION_ERROR`
  with the field named.
- `DELETE /api/v1/admin/product-groups/{id}` while options remain → `409 VARIANT_GROUP_HAS_OPTIONS`,
  and the options are untouched.
- `DELETE /api/v1/admin/categories/{categoryId}` while a group references it → `409 CATEGORY_IN_USE`.

The migration also enforces these at the database (partial unique index, check constraints,
RESTRICT FK); an integration test attempts the violation directly against storage.

## Scenario F — nothing existing changed (spec FR-026, SC-005)

- A product with no group behaves exactly as before in the public list, detail, cart, order and
  inventory.
- `make test` and `make test-integration` — the existing catalogue, cart, order and inventory suites
  pass unchanged.

## Scenario G — slug namespaces are separate (spec Clarification Q1, FR-019)

A group slug and a product slug with the same value both resolve, each on its own route
(`/product-groups/{slug}` vs `/products/{slug}`). Two groups with the same slug are refused
(`409 VARIANT_GROUP_SLUG_TAKEN`); two products with the same slug are refused as today.

## Out of scope (do not test here)

Per-option availability shown on the group page; multi-dimension options (colour × size); option
reordering beyond a product's existing `position`. Each is recorded as deferred.
