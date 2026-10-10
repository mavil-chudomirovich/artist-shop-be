# Quickstart: Order Confirmation & Editing

Validation guide. Every scenario states the request, the expected answer, and how to tell a real pass
from a false one.

**How to read the outcomes**: `PASS` means the observed answer matches. `FAIL` means it does not. A
scenario that cannot run here is reported `BLOCKED` with the reason — never silently skipped.

**A note on the paid path**: an order reaches `PAID` only when module 08 Payment confirms a payment, and
module 08 does not exist yet. The scenarios that need a **paid** order — ship, complete, transfer — are
covered by the automated tests, which call the paid transition directly.

## Prerequisites

- Dependencies running: `make up-tools`.
- Migration applied: `make migrate-up` — this feature adds `migrations/00011_order_confirmation.sql`.
- API running: `make run`, or `go run ./cmd/api`.
- A signed-in customer token (`CUSTOMER`) with at least one delivery address and a **non-empty cart**,
  a second customer token (`CUSTOMER2`), and an **administrator** token (`ADMIN`).
- A product on sale with stock (module 04 + 05), added to the cart (module 06).
- An SMTP sink (Mailpit, via `make up-tools`) if you want to read the notification emails.

Set `BASE=http://localhost:8080/api/v1`, `CUSTOMER=...`, `CUSTOMER2=...`, `ADMIN=...`, `P=<product id>`.

**On Windows, never send a JSON body inline.** Write the body to a file and send it with
`--data-binary @file`.

## Scenario 1 — checkout creates an order awaiting the artist, holding nothing

| # | Action | Expected |
|---|---|---|
| 1a | `POST $BASE/orders` (empty body → default address) | `201`, `status: "PENDING"`, a snapshot line per cart item and the delivery address |
| 1b | `GET $BASE/admin/inventory/$P` (module 05) | the available quantity is **unchanged** — nothing was held |
| 1c | `GET $BASE/cart` (module 06) | an empty cart — the cart was emptied |
| 1d | the artist's mailbox (Mailpit) | an email saying an order needs confirmation |

**1b is the check the feature exists to reach**: an order that waits for the artist does not lock stock.

## Scenario 2 — the artist confirms, and the goods are held

| # | Action | Expected |
|---|---|---|
| 2a | `POST $BASE/admin/orders/{id}/confirm` | `200`, `status: "PAYMENT_PENDING"` |
| 2b | `GET $BASE/admin/inventory/$P` | available **fell**, physical **unchanged** — the goods are held |
| 2c | the customer's mailbox | an email that the order was confirmed |
| 2d | `POST $BASE/admin/orders/{id}/confirm` again | `409 ORDER_STATE_TRANSITION_INVALID`, naming `PAYMENT_PENDING` |

## Scenario 3 — confirmation is all-or-nothing

| # | Action | Expected |
|---|---|---|
| 3a | an order with two lines where one is short at confirmation | `409 ORDER_QUANTITY_EXCEEDS_AVAILABLE`, the item named with the available amount, and **no** line held |
| 3b | `GET $BASE/admin/orders/{id}` | `status: "PENDING"` — the order was not confirmed |

## Scenario 4 — a customer edits an order before paying

| # | Action | Expected |
|---|---|---|
| 4a | `PUT $BASE/orders/{id}` with a new line set while `PENDING` | `200`, lines replaced, total recomputed, `status: "PENDING"`, nothing held |
| 4b | `PUT $BASE/orders/{id}` with `{"lines":[]}` | `409 ORDER_EMPTY` |
| 4c | `PUT $BASE/orders/{id}` with a line whose product is off sale | `409 ORDER_ITEM_NOT_PURCHASABLE`, the item named |
| 4d | `PUT $BASE/orders/{id}` on a `PAYMENT_PENDING` order | `200`, `status: "PENDING"` again, and `GET $BASE/admin/inventory/$P` shows the hold **released** |
| 4e | `PUT $BASE/orders/{id}` on a paid order | `409 ORDER_NOT_EDITABLE` |
| 4f | `PUT $BASE/orders/{id}` with another customer's order id | `404 ORDER_NOT_FOUND` |
| 4g | `PUT $BASE/orders/{id}` with an `addressId` that is not the caller's | `400 VALIDATION_ERROR`, `details[].field = "addressId"` |

**4d is the check that matters**: changing an accepted order returns it to awaiting confirmation and
frees the goods, exactly once.

## Scenario 5 — the artist declines, and the queue is FIFO

| # | Action | Expected |
|---|---|---|
| 5a | `POST $BASE/admin/orders/{id}/reject` on a `PENDING` order | `200`, `status: "CANCELLED"`, nothing was held |
| 5b | `POST $BASE/admin/orders/{id}/reject` on a `PAYMENT_PENDING` order | `409 ORDER_STATE_TRANSITION_INVALID` |
| 5c | `GET $BASE/admin/orders?status=PENDING&sort=oldest` | only awaiting-confirmation orders, oldest first (FIFO) |

## Scenario 6 — the customer cancels before paying

| # | Action | Expected |
|---|---|---|
| 6a | `POST $BASE/orders/{id}/cancel` on a `PENDING` order | `200`, `status: "CANCELLED"`, no stock effect |
| 6b | `POST $BASE/orders/{id}/cancel` on a `PAYMENT_PENDING` order | `200`, `status: "CANCELLED"`, and the hold released |
| 6c | `POST $BASE/orders/{id}/cancel` a second time | `409 ORDER_STATE_TRANSITION_INVALID` |

## Scenario 7 — expiry, and the paid path (verified by test)

An order cannot be paid over HTTP until module 08 exists, and the expiry clock cannot be wound by hand,
so these are proven by the automated tests (direct use cases over real PostgreSQL), not here:

| # | Action | Expected |
|---|---|---|
| 7a | an awaiting-payment order past its 60-minute window | becomes `CANCELLED`, goods released **exactly once**, even across both sweeps |
| 7b | an order awaiting the artist left for a long time | still `PENDING` — it never expires by time |
| 7c | two confirmations competing for the last unit | exactly one confirms; the other is refused; no oversell |
| 7d | confirming twice, or paying twice | the sale is applied once |
| 7e | ship then complete a paid order | each succeeds and is audited |

## Scenario 8 — storage-level guarantees (integration)

| # | Action | Expected |
|---|---|---|
| 8a | insert an order with a status outside the six | refused by `orders_status_ck` |
| 8b | insert an order with `order_version = 0` | refused by `orders_version_ck` |
| 8c | delete an order | its lines and its edit history are gone with it |
| 8d | accept an edit | an `order_edit_history` row records before/after, the actor and the new version |

## Automated gates

```bash
make lint              # format, tidy, vet, golangci-lint
make test              # unit, including the state machine and the edit/confirm refusals
make test-integration  # needs Docker; proves the storage guarantees, the concurrency and the paid path
make swagger           # regenerate docs/swagger after the handler annotations
```

If `make test-integration` reports success without executing — check for a skip — it is a false green.

## What is deliberately not verified here

| Not verified | Why | Where it belongs |
|---|---|---|
| A real payment turning an order `PAID` over HTTP | Payment is module 08 and does not exist | Module 08; recorded in `deferred.md` |
| Cancelling a provider payment session when an order is edited | PayOS integration is module 08; this feature only bumps the order version | Module 08 |
| In-app notifications | Module 12 is unbuilt; this feature sends email only | Module 12 |
