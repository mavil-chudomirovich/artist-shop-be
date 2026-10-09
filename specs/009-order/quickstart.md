# Quickstart: Order Checkout

Validation guide. Every scenario states the request, the expected answer, and how to tell a real pass
from a false one.

**How to read the outcomes**: `PASS` means the observed answer matches. `FAIL` means it does not.
A scenario that cannot run here is reported `BLOCKED` with the reason — never silently skipped.

**A note on the paid path**: an order reaches `PAID` only when module 08 Payment confirms a payment,
and module 08 does not exist yet. So the scenarios that need a **paid** order — ship, complete,
transfer — cannot be driven over HTTP here; they are covered by the automated tests, which call the
paid transition directly. The scenarios below say so where it applies.

## Prerequisites

- Dependencies running: `make up-tools` (PostgreSQL, Redis, Mailpit).
- Migration applied: `make migrate-up` — this feature adds `migrations/00009_order.sql`.
- API running: `make run`, or `go run ./cmd/api`.
- A signed-in customer token (`CUSTOMER`) with **at least one delivery address** and a **non-empty
  cart**, and a second customer token (`CUSTOMER2`), and an **administrator** token (`ADMIN`).
- A product on sale with stock (module 04 + 05), added to the cart (module 06).

Set `BASE=http://localhost:8080/api/v1`, `CUSTOMER=...`, `CUSTOMER2=...`, `ADMIN=...`, `P=<product id>`.

**On Windows, never send a JSON body inline.** Write the body to a file and send it with
`--data-binary @file`:

```bash
printf '{}' > /tmp/checkout.json
curl -s -X POST "$BASE/orders" -H "$CUSTOMER" -H 'Content-Type: application/json' \
  --data-binary @/tmp/checkout.json
```

## Scenario 1 — checkout needs something to order

| # | Setup | Request | Expected |
|---|---|---|---|
| 1a | an empty cart | `POST $BASE/orders` | `409 ORDER_CART_EMPTY`, and no order exists |
| 1b | a cart whose product is off sale or removed | `POST $BASE/orders` | `409 ORDER_ITEM_NOT_PURCHASABLE`, the item named in `error.details`, cart unchanged |
| 1c | a cart with more than is available | `POST $BASE/orders` | `409 ORDER_QUANTITY_EXCEEDS_AVAILABLE`, the available amount named |
| 1d | a cart added at one price, then the price changed | `POST $BASE/orders` | `409 ORDER_ITEM_PRICE_CHANGED`, nothing created |
| 1e | a customer with no delivery address | `POST $BASE/orders` | `409 ORDER_NO_ADDRESS` |

**1d is the check the cart system exists to reach**: a price the customer did not see is refused, not
silently charged.

## Scenario 2 — a customer checks out

| # | Action | Expected |
|---|---|---|
| 2a | `POST $BASE/orders` (empty body → default address) | `201`, the order carrying a line per cart item with the product's name, slug, unit price and currency, and the delivery address |
| 2b | `total.amount` | equals the sum of `quantity × unitPrice.amount` over the lines, exactly — the round trip proves no float crept in |
| 2c | `GET $BASE/admin/inventory/$P` (module 05) | the physical quantity is **unchanged** but the **held** quantity rose — the goods are held, not sold |
| 2d | `GET $BASE/cart` (module 06) | an empty cart — the cart was emptied |
| 2e | a product later removed from the catalogue, then `GET $BASE/orders/{id}` | the order is unchanged; its lines still show the snapshot |

## Scenario 3 — the customer's own orders

| # | Action | Expected |
|---|---|---|
| 3a | `GET $BASE/orders` with `CUSTOMER` | `200`, only the caller's orders, newest first, paginated |
| 3b | `GET $BASE/orders/{id}` with `CUSTOMER2` for the first customer's order | `404 ORDER_NOT_FOUND` — indistinguishable from an unknown order |
| 3c | `GET $BASE/orders` with no token | `401 UNAUTHENTICATED` |
| 3d | `POST $BASE/orders/{id}/cancel` with `CUSTOMER` on an unpaid order | `200`, the order is `CANCELLED` and the goods are available again (`availableQuantity` back) |
| 3e | `POST $BASE/orders/{id}/cancel` a second time | `409 ORDER_STATE_TRANSITION_INVALID`, naming `CANCELLED` |

## Scenario 4 — two checkouts of one cart at once

| # | Action | Expected |
|---|---|---|
| 4a | Two concurrent `POST $BASE/orders` for the same cart | Exactly one `201`; the other is `409 ORDER_CART_EMPTY`, and only one order exists |
| 4b | `SELECT count(*) FROM orders WHERE user_id = '...'` | `1` — one cart never makes two orders |

## Scenario 5 — the state machine's paid-dependent moves (verified by test)

An order cannot be paid over HTTP until module 08 exists, so these are proven by the automated tests
(direct use cases over real PostgreSQL), not here:

| # | Action | Expected |
|---|---|---|
| 5a | confirm payment on an awaiting-payment order | the order becomes `PAID` and the held quantity becomes a **sale** (physical stock falls, held clears), exactly once even if the confirmation repeats |
| 5b | `POST $BASE/admin/orders/{id}/ship` on an unpaid order | `409 ORDER_STATE_TRANSITION_INVALID`, naming `PENDING_PAYMENT` |
| 5c | on a paid order, ship then complete | each `200`, each audited |
| 5d | complete a completed order, or ship a cancelled one | `409`, naming the state |
| 5e | cancel a **paid** order (through the customer cancel route, or a direct use case — there is no administrator cancel route) | refused: a paid order is transferred, never cancelled |

**5a is the check that closes the loop**: the held goods become a sale exactly once.

## Scenario 6 — transfer a paid order

| # | Action | Expected |
|---|---|---|
| 6a | `POST $BASE/admin/orders/{id}/transfer` with `{"email":"<an existing account>"}` on a paid order | `200`, the order belongs to that account, lines/state/total unchanged |
| 6b | `GET $BASE/admin/inventory/$P` after the transfer | no product's physical or available stock changed |
| 6c | transfer to an email no account carries | `404 ORDER_TRANSFER_TARGET_NOT_FOUND` |
| 6d | transfer an unpaid order | `409 ORDER_NOT_TRANSFERABLE` |
| 6e | any admin order route with `CUSTOMER` | `403 FORBIDDEN` |

## Scenario 7 — storage-level guarantees (integration)

Asserted against real PostgreSQL, because a fake that enforces a check proves only that the fake was
written twice:

| # | Action | Expected |
|---|---|---|
| 7a | insert an order with a `status` outside the five | Refused by `orders_status_ck` |
| 7b | insert an order line with `quantity = 0` | Refused by `order_items_quantity_ck` |
| 7c | insert two lines for the same `(order_id, product_id)` | Refused by `order_items_order_product_key` |
| 7d | delete an order | its lines are gone with it |
| 7e | delete a product a line's informational `product_id` names | the line **survives** — there is no foreign key |
| 7f | let an unpaid order pass its window across the order and inventory sweeps | the goods free **exactly once**, and the order is `CANCELLED` |

## Automated gates

```bash
make lint          # format, tidy, vet, golangci-lint
make test          # unit, including the state machine and the whole checkout refusal set
make test-integration   # needs Docker; proves the storage guarantees, the concurrency case and the paid path
make swagger       # regenerate docs/swagger after the new handler annotations
```

If `make test-integration` reports success without executing — check for a skip — it is a false green
and does not count.

## What is deliberately not verified here

| Not verified | Why | Where it belongs |
|---|---|---|
| A real payment turning an order `PAID` over HTTP | Payment is module 08 and does not exist; the transition is proven by test | Module 08; recorded in `deferred.md` |
| Shipping fee and tracking | The customer pays shipping on delivery (ADR 015 §5); tracking is module 09 | Module 09 |
| Refunds, split orders, discount codes | Out of MVP scope | A later feature each |
