# Quickstart: Shopping Cart

Validation guide. Every scenario states the request, the expected answer, and how to tell a real pass
from a false one.

**How to read the outcomes**: `PASS` means the observed answer matches. `FAIL` means it does not.
A scenario that cannot run here is reported `BLOCKED` with the reason — never silently skipped.

## Prerequisites

- Dependencies running: `make up-tools` (PostgreSQL, Redis, Mailpit).
- Migration applied: `make migrate-up` — this feature adds `migrations/00008_cart.sql`.
- API running: `make run`, or `go run ./cmd/api`.
- A signed-in customer token from `POST /api/v1/auth/login`, kept as `CUSTOMER`; a second customer
  token for the ownership checks.
- **A product exists and is on sale with stock.** Create it through module 04, put it on sale
  (`POST /api/v1/admin/products/$P/state` with `{"to":"ACTIVE"}`), and restock it through module 05
  (`POST /api/v1/admin/inventory/$P/restock` with `{"quantity":10}`).

Set `BASE=http://localhost:8080/api/v1`, `CUSTOMER="Authorization: Bearer <customer token>"`,
`CUSTOMER2="Authorization: Bearer <second customer token>"` and `P=<product id>` once.

**On Windows, never send a JSON body inline.** PowerShell rewrites embedded quotes and the request
arrives malformed. Write the body to a file and send it with `--data-binary @file`:

```bash
cat > /tmp/add.json <<'JSON'
{ "productId": "00000000-0000-0000-0000-000000000000", "quantity": 2 }
JSON
curl -s -X POST "$BASE/cart/items" -H "$CUSTOMER" -H 'Content-Type: application/json' \
  --data-binary @/tmp/add.json
```

## Scenario 1 — an empty cart answers an empty cart

| # | Request | Expected |
|---|---|---|
| 1a | `GET $BASE/cart` | `200`, `data.lines: []`, `data.subtotal: null` |
| 1b | A brand-new customer with no cart | The same empty answer, not a not-found and not a created row |

## Scenario 2 — a customer adds a product

```bash
curl -s -X POST "$BASE/cart/items" -H "$CUSTOMER" -H 'Content-Type: application/json' \
  --data-binary @/tmp/add.json    # {"productId":"$P","quantity":2}
```

| # | Expected |
|---|---|
| 2a | `200`, `data.lines` has exactly one line: `productId = $P`, `quantity = 2`, `unitPrice.amount` equal to the product's current price |
| 2b | `unitPrice` is the product's price **at add time**; `lineTotal.amount = quantity × unitPrice.amount`; `subtotal.amount` equals the line total |
| 2c | `buyable: true` and no `availableQuantity` while the line is fully available |

**2b is the check that catches float arithmetic**: a total carried through a `float` would round, and
the exact integer round-trip is what proves it did not.

## Scenario 3 — adding the same product raises the line

| # | Action | Expected |
|---|---|---|
| 3a | `POST $BASE/cart/items` for `$P` with `{"quantity":1}` again | `200`, **still one line** for `$P`, now `quantity = 3` |
| 3b | `GET $BASE/cart` | `data.lines` length is `1` — a product appears once |

## Scenario 4 — changing and removing a line

| # | Action | Expected |
|---|---|---|
| 4a | `PATCH $BASE/cart/items/$P` with `{"quantity":5}` | `200`, the line shows `quantity: 5`, subtotal updated |
| 4b | `DELETE $BASE/cart/items/$P` | `204`, and `GET $BASE/cart` shows an empty cart |
| 4c | `DELETE $BASE/cart/items/$P` again | `404 PRODUCT_NOT_FOUND` — the line is gone |

## Scenario 5 — the cart refuses what cannot be bought

| # | Setup | Request | Expected |
|---|---|---|---|
| 5a | a product not on sale (e.g. `COMING_SOON`) | `POST $BASE/cart/items` | `409 CART_PRODUCT_NOT_PURCHASABLE`, and the cart is unchanged |
| 5b | a retired product (`DISCONTINUED`) | `POST $BASE/cart/items` | `409 CART_PRODUCT_NOT_PURCHASABLE` |
| 5c | a product with 3 available | `POST` with `{"quantity":5}` | `409 CART_QUANTITY_EXCEEDS_AVAILABLE`, cart unchanged |
| 5d | a product with 3 available | `POST` with `{"quantity":3}` | `200` — the whole available amount is reachable |
| 5e | an unknown product id | `POST $BASE/cart/items` | `404 PRODUCT_NOT_FOUND` |
| 5f | any | `POST` with `{"quantity":0}` | `400 VALIDATION_ERROR`, `details[].field` naming `quantity` |
| 5g | any | `POST` with `{"quantity":-1}` | `400`, the field named |
| 5h | any | `POST` with `{"quantity":1.5}` | `400`, the field named — a quantity is a whole number |

## Scenario 6 — the view re-checks the lines

| # | Action | Expected |
|---|---|---|
| 6a | Add `$P` at quantity 2, then damage the stock so 1 remains, then `GET $BASE/cart` | The line is `buyable: false` and carries `availableQuantity: 1`, with the **original** `unitPrice` |
| 6b | Add `$P`, then take it off sale, then `GET` | The line is `buyable: false` with **no** `availableQuantity` |
| 6c | Reduce the line to the available quantity (`PATCH`) | `200`, `buyable: true` again |
| 6d | Any re-read | The line's `quantity` and `unitPrice` are never changed by a read |

**6a is the clarification's check**: a short line tells the customer the number to reduce to, and the
price shown stays the snapshot.

## Scenario 7 — a removed product's line survives

| # | Action | Expected |
|---|---|---|
| 7a | Add `$P`, then `DELETE $BASE/admin/products/$P` through module 04 | `204` |
| 7b | `GET $BASE/cart` | The line is still present, `name: null` and `slug: null`, `buyable: false`, no `availableQuantity` |
| 7c | `DELETE $BASE/cart/items/$P` | `204` — the customer can clear it |

## Scenario 8 — a cart is the customer's own

| # | Action | Expected |
|---|---|---|
| 8a | `GET $BASE/cart` with no token | `401 UNAUTHENTICATED` |
| 8b | `GET $BASE/cart` with `CUSTOMER2` | Its own cart, not the first customer's |
| 8c | `PATCH $BASE/cart/items/$P` with `CUSTOMER2` for a product only the first customer holds | `404 PRODUCT_NOT_FOUND` — the route never confirms another cart's contents |
| 8d | `DELETE $BASE/cart/items/$P` with `CUSTOMER2` likewise | `404` |

## Scenario 9 — storage-level guarantees (integration)

Asserted against real PostgreSQL, because a fake that enforces a unique index proves only that the
fake was written twice:

| # | Action | Expected |
|---|---|---|
| 9a | Insert two `carts` for the same `user_id` | The second is refused by `carts_user_key` |
| 9b | Insert two `cart_items` for the same `(cart_id, product_id)` directly | The second is refused by `cart_items_cart_product_key` |
| 9c | Two concurrent adds of the same product to one cart | One line whose quantity is the sum, never two lines, and never above what is available |
| 9d | Two concurrent adds to one cart that together exceed availability | The line never ends above what is available |
| 9e | `INSERT INTO cart_items (cart_id, product_id, quantity)` with `quantity = 0` | Refused by `cart_items_quantity_ck` |
| 9f | Delete a product that a cart line references | The line survives — there is no foreign key on `product_id` |

## Automated gates

```bash
make lint          # format, tidy, vet, golangci-lint
make test          # unit, including the subtotal and the buyable decision
make test-integration   # needs Docker; proves the storage guarantees and the concurrency case
make swagger       # regenerate docs/swagger after the new handler annotations
```

`make swagger-check` (part of `make check`) must pass: every endpoint this feature adds carries its
annotations and the generated spec is committed with it.

If `make test-integration` reports success without executing — check for a skip — it is a false green
and does not count.

## What is deliberately not verified here

| Not verified | Why | Where it belongs |
|---|---|---|
| The checkout turning a cart into an order | Order is module 07 and does not exist | Module 07; recorded in `deferred.md` |
| Holding stock while a customer pays | Holding is module 05's capability, invoked at payment, not by the cart | Module 07 |
| Discounts, upsell, a guest cart, a saved cart | The module document defers them | A later feature each, when a need is agreed |
