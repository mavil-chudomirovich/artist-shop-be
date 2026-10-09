# Quickstart: Inventory Tracking

Validation guide. Every scenario states the request, the expected answer, and how to tell a real
pass from a false one.

**How to read the outcomes**: `PASS` means the observed answer matches. `FAIL` means it does not.
A scenario that cannot run here is reported `BLOCKED` with the reason — never silently skipped,
because a container-backed check that skips is the failure mode this project has already paid for
once.

## Prerequisites

- Dependencies running: `make up-tools` (PostgreSQL, Redis, Mailpit).
- Migration applied: `make migrate-up` — this feature adds `migrations/00007_inventory.sql`.
- API running: `make run`, or `go run ./cmd/api`.
- An administrator token from `ADMIN_EMAIL` / `ADMIN_PASSWORD` in `.env`, obtained through
  `POST /api/v1/auth/login`, and a customer token for the role checks.
- **A product exists.** Create one through module 04 and keep its `id` as `$P`; put it on sale
  (`POST /api/v1/admin/products/$P/state` with `{"to":"ACTIVE"}`) for the crossing scenarios.

Set `BASE=http://localhost:8080/api/v1`, `ADMIN="Authorization: Bearer <admin token>"`,
`CUSTOMER="Authorization: Bearer <customer token>"` and `P=<product id>` once.

**On Windows, never send a JSON body inline.** PowerShell rewrites embedded quotes and the request
arrives malformed, which looks like a server defect and is not one. Write the body to a file and send
it with `--data-binary @file`.

```bash
cat > /tmp/q.json <<'JSON'
{ "quantity": 10 }
JSON
curl -s -X POST "$BASE/admin/inventory/$P/restock" -H "$ADMIN" \
  -H 'Content-Type: application/json' --data-binary @/tmp/q.json
```

## Scenario 1 — a product never stocked is zero, not a not-found

| # | Request | Expected |
|---|---|---|
| 1a | `GET $BASE/admin/inventory/$P` | `200`, `physicalQuantity: 0`, `heldQuantity: 0`, `availableQuantity: 0` |
| 1b | `GET $BASE/admin/inventory/<an unused uuid>` | `404 PRODUCT_NOT_FOUND` |

**1a is the check that catches a "row must exist" mistake**: a product added by module 04 has no
stock row yet, and reading it is the normal state, not an error.

## Scenario 2 — restock raises the shelf and writes one ledger row

```bash
curl -s -X POST "$BASE/admin/inventory/$P/restock" -H "$ADMIN" \
  -H 'Content-Type: application/json' --data-binary @/tmp/q.json   # {"quantity": 10}
```

| # | Expected |
|---|---|
| 2a | `200`, `physicalQuantity: 10`, `availableQuantity: 10` |
| 2b | `GET $BASE/admin/inventory/$P/movements` contains exactly one entry: `kind: "RESTOCK"`, `delta: 10`, `resultingQuantity: 10`, `actorId` is the administrator |
| 2c | `SELECT SUM(delta) FROM inventory_transactions WHERE product_id = '$P'` equals `SELECT quantity FROM stock_levels WHERE product_id = '$P'` — the reconciliation SC-001 asserts |

## Scenario 3 — the shelf never goes below zero

| # | Body | Expected |
|---|---|---|
| 3a | `{"quantity": 10}` to `/damage` on ten units | `200`, `physicalQuantity: 0` — zero is reachable |
| 3b | `{"quantity": 1}` to `/damage` on zero units | `409 INVENTORY_INSUFFICIENT_STOCK`, and `physicalQuantity` is still `0` |
| 3c | After 3b, `GET .../movements` | No new entry — a refused change writes nothing |
| 3d | `{"quantity": 0}` to `/restock` | `400 VALIDATION_ERROR`, `details[].field` naming `quantity` |
| 3e | `{"quantity": -1}` to `/restock` | `400`, the field named |
| 3f | `{"quantity": 1.5}` to `/restock` | `400`, the field named — a quantity is a whole number |

**3b and 3c together are the point**: a refusal is a real refusal, not a negative number that then
exists.

## Scenario 4 — an adjustment records only the difference, and a no-op records nothing

| # | Action | Expected |
|---|---|---|
| 4a | Restock to `10`, then `{"quantity": 7}` to `/adjustment` | `200`, `physicalQuantity: 7`; the ledger's newest entry is `kind: "ADJUSTMENT"`, `delta: -3`, `resultingQuantity: 7` |
| 4b | `{"quantity": 7}` again (the stored value) | `200`, `physicalQuantity: 7`, and **no new movement** — nothing changed |
| 4c | `{"quantity": 0}` | `200`, `physicalQuantity: 0` — zero is a valid count |
| 4d | `{"quantity": -1}` | `400 VALIDATION_ERROR`, the field named |

## Scenario 5 — availability is physical minus what is held

This is verified by the automated tests rather than by curl, because holding stock has no HTTP
surface (D7). The test drives the hold use case directly and then reads the stock:

| # | Action | Expected |
|---|---|---|
| 5a | Restock to `5`, then begin a payment for `2` (hold use case) | `GET $BASE/admin/inventory/$P` answers `physicalQuantity: 5`, `heldQuantity: 2`, `availableQuantity: 3` |
| 5b | Another attempt to hold `4` | Refused — only `3` is available, so a hold of `4` exceeds it |
| 5c | The first hold is cancelled (release use case) | `heldQuantity: 0`, `availableQuantity: 5`, and physical is still `5` |

**5a is the check that a hold is not a sale**: the shelf still holds five, but only three are on
offer.

## Scenario 6 — availability drives the product's sell state

The product was put on sale in the prerequisites, so it is `ACTIVE`.

| # | Action | Expected |
|---|---|---|
| 6a | Restock to `1`, put the product on sale, then damage `1` | `GET $BASE/admin/products/$P` answers `sellState: "OUT_OF_STOCK"` — availability reached zero |
| 6b | Restock `1` | `GET $BASE/admin/products/$P` answers `sellState: "ACTIVE"` — availability left zero |
| 6c | Hold the last available unit (hold use case) | `GET $BASE/admin/products/$P` answers `sellState: "OUT_OF_STOCK"` — a hold makes it unavailable |
| 6d | Let the hold expire (sweeper, or injected clock in the test) | `sellState: "ACTIVE"` again |
| 6e | While the product is `OUT_OF_STOCK`, restock by `5` **without** crossing zero is impossible to arrange here; instead mark it manually `OUT_OF_STOCK` while stock is positive, then restock without crossing | `sellState` is **unchanged** — a non-crossing change preserves the operator's marking (FR-028) |

**6a and 6b together close obligation D1**: the sell state changes by itself, in both directions,
with no operator act.

## Scenario 7 — announced and retired products are never moved by stock

| # | Action | Expected |
|---|---|---|
| 7a | Take a product still in `COMING_SOON`, damage it to zero | `sellState` stays `COMING_SOON` |
| 7b | Retire a product (`to: "DISCONTINUED"`), then restock it | `200`; `sellState` stays `DISCONTINUED` — a retired product never returns to sale |
| 7c | Damage it below its held quantity while it holds stock | `409 INVENTORY_INSUFFICIENT_STOCK` — the shelf never falls below what is promised |

## Scenario 8 — exactly-once application of an outside event

Verified by the automated tests directly (no HTTP surface). Applying the same payment event twice,
including concurrently:

| # | Action | Expected |
|---|---|---|
| 8a | Apply a sale of `2` with source reference `evt-1`, then apply `evt-1` again | The physical quantity falls exactly once; exactly one `SALE` movement carries `evt-1`; the repeat is accepted as success |
| 8b | Apply the same event twice at the same instant (concurrent) | Exactly one takes effect; the other is a no-op, not an error and not a double-decrease |
| 8c | `SELECT count(*) FROM inventory_transactions WHERE source_reference = 'evt-1'` | `1` — the storage layer, not the application, is what refused the second |

## Scenario 9 — the history reads in order and paginates

| # | Request | Expected |
|---|---|---|
| 9a | Make several changes, then `GET .../movements` | Every change appears once, oldest first, each with `kind`, `delta`, `resultingQuantity`, `createdAt` |
| 9b | `?page=1&pageSize=2` with five movements | `200`, two entries, `meta.total` is `5` |
| 9c | `?pageSize=101` | `400 VALIDATION_ERROR`, `details[].field` naming `pageSize` |
| 9d | A product with no movements | `200`, `data: []`, not an error |

## Scenario 10 — role enforcement

| # | Request | Expected |
|---|---|---|
| 10a | `GET $BASE/admin/inventory/$P` with no token | `401 UNAUTHENTICATED` |
| 10b | `POST $BASE/admin/inventory/$P/restock` with the customer token | `403 FORBIDDEN` |
| 10c | `POST $BASE/admin/inventory/$P/damage` with the customer token | `403 FORBIDDEN` |
| 10d | `POST $BASE/admin/inventory/$P/adjustment` with the customer token | `403 FORBIDDEN` |

## Scenario 11 — storage-level guarantees (integration)

Asserted against real PostgreSQL, because a fake repository that enforces a check constraint or a
unique index proves only that the fake was written twice:

| # | Action | Expected |
|---|---|---|
| 11a | Two concurrent damages that would together exceed the stock | Exactly one succeeds; `SELECT quantity FROM stock_levels` is never negative |
| 11b | Insert two movements with the same non-null `source_reference` | The second is refused by the unique index |
| 11c | Insert two `ACTIVE` holds for the same `(order_id, product_id)` | The second is refused by the partial unique index |
| 11d | `DELETE FROM products WHERE id = '$P'` | The product's `stock_levels`, `inventory_transactions` and `stock_holds` rows are gone with it |
| 11e | Insert a `stock_levels` row for a product that does not exist | Refused by the foreign key |

**11a is the check a non-atomic implementation fails**: an application-level count lets both writers
read the same quantity and both proceed.

## Automated gates

```bash
make lint          # format, tidy, vet, golangci-lint
make test          # unit, including the hold lifecycle with an injected clock
make test-integration   # needs Docker; proves the storage constraints and concurrency
```

If `make test-integration` reports success without executing — check for a skip — it is a false
green and does not count.

## What is deliberately not verified here

| Not verified | Why | Where it belongs |
|---|---|---|
| A paid order decreasing stock end to end, and a cancelled order releasing a hold | Order (07) and Payment (08) do not exist, so no event can be produced | Module 07 / 08; recorded in `deferred.md` |
| Holding stock over HTTP | Holds are internal to the checkout flow; no checkout exists | A later feature, when the order flow is specified |
| A low-stock warning | Deliberately out of scope (spec FR-029) | A later feature, if wanted |
