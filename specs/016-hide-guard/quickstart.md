# Quickstart — Hide Guard with Pending Orders (016)

Validation guide proving the feature end to end. It does not restate the contract (see
[contracts/error-codes.md](./contracts/error-codes.md)) or the schema (see
[data-model.md](./data-model.md)); it says how to run and what to expect.

## Prerequisites

- Local stack up (see `docs/getting-started.md` / `docs/docker.md`); `make migrate` runs migrations
  **including `00016_product_visibility_flag.sql`**.
- An ADMIN account and token; a visible category, a product with two variants, and a customer account
  (see `docs/development/api-testing.md` for the four-checks convention).

## Automated gates

| Command | Proves |
|---|---|
| `make lint` | format + vet + golangci-lint clean |
| `make test` | unit tests: guard refuse/no-change, cleanup scoping, no-op system signal |
| `make test-integration` | the guard and cleanup against PostgreSQL (needs Docker) |
| `make swagger-check` | must pass (run `make swagger` when the new `@Failure` annotations of T025 are added) |

## Scenario A — hide a clean category (spec US1, FR-004/FR-007)

1. With no pending order on the category's variants, turn the category's display off through the admin
   category update endpoint.
2. Expect success; the category leaves the public catalogue; every cart holding its variants is empty
   of those lines (other lines untouched).

## Scenario B — hide a blocked category (spec US1, FR-001/FR-004)

1. Place a variant of the category in a PENDING order (via checkout) and another variant in a
   PAYMENT_PENDING order (confirm it so it holds goods).
2. Attempt the hide → expect `409 CATEGORY_HAS_PENDING_ORDERS` naming both orders; re-read the
   category (unchanged) and the carts (untouched, holds intact).
3. Move both orders out of the pending states; attempt again → success as in Scenario A.

## Scenario C — variant moves, both directions (spec US2, FR-005)

1. With no pending order, move a variant away from on-sale → success; the variant leaves every cart.
2. Place it in a PENDING order, move it back on sale, then attempt to hide it again → `409
   VARIANT_HAS_PENDING_ORDERS`, variant byte-for-byte unchanged.
3. Force available stock to zero while a pending order covers the variant → the system signal leaves
   the on-sale state in place and records the blocked event; the stock change itself still commits.

## Scenario D — product flag (spec US3, FR-006)

1. Turn a product's visibility flag off with no pending order on any variant → the product leaves the
   catalogue (browse, filter and direct link) and its variants leave every cart.
2. With a pending order on one variant → `409 PRODUCT_HAS_PENDING_ORDERS`, nothing changes.
3. Turn the flag back on → never refused; removed cart lines do not return.

## Scenario E — bulk scale spot-check (spec SC-004, FR-008)

Seed a category holding hundreds of variants with lines spread across carts; hide it and observe the
guard plus cleanup complete as bounded bulk statements (no per-variant round trips in logs/traces).

## Out of scope (do not test here)

Per the spec: hard-delete paths, customer notifications for cleaned carts, checkout/order-flow
changes, inventory policy changes, and multi-dimension options.
