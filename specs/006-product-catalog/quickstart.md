# Quickstart: Product Catalogue

Validation guide. Every scenario states the request, the expected answer, and how to tell a real
pass from a false one.

**How to read the outcomes**: `PASS` means the observed answer matches. `FAIL` means it does not.
A scenario that cannot run here is reported `BLOCKED` with the reason — never silently skipped,
because a container-backed check that skips is the failure mode this project has already paid
for once.

## Prerequisites

- Dependencies running: `make up-tools` (PostgreSQL, Redis, Mailpit).
- Migration applied: `make migrate-up` — this feature adds `migrations/00006_product.sql`.
- API running: `make run`, or `go run ./cmd/api`.
- Media credentials present: `MEDIA_CLOUD_NAME`, `MEDIA_API_KEY`, `MEDIA_API_SECRET`. Without
  them every upload answers `503 PRODUCT_MEDIA_UNAVAILABLE` and scenarios 10 and 11 are `BLOCKED`
  rather than failed — that behaviour is itself scenario 12.
- An administrator token from `ADMIN_EMAIL` / `ADMIN_PASSWORD` in `.env`, obtained through
  `POST /api/v1/auth/login`, and a customer token for the role checks.
- **A category exists.** Products reference one, so create one through module 03 first and keep
  its `id` as `$CAT`.

**On Windows, never send a JSON body inline.** PowerShell rewrites embedded quotes and the
request arrives malformed, which looks like a server defect and is not one. Write the body to a
file and send it with `--data-binary @file`:

```bash
cat > /tmp/body.json <<'JSON'
{
  "name": "Acrylic stand Aki",
  "slug": "acrylic-stand-aki",
  "description": "Acrylic stand 15cm",
  "price": { "amount": 120000, "currency": "VND" },
  "categoryId": "00000000-0000-0000-0000-000000000000",
  "position": 10
}
JSON
curl -s -X POST "$BASE/admin/products" -H "$ADMIN" -H 'Content-Type: application/json' \
  --data-binary @/tmp/body.json
```

Set `BASE=http://localhost:8080/api/v1`, `ADMIN="Authorization: Bearer <admin token>"`,
`CUSTOMER="Authorization: Bearer <customer token>"` and `CAT=<category id>` once.

## Scenario 1 — an empty catalogue answers an empty list

```bash
curl -s "$BASE/products"
```

| # | Expected |
|---|---|
| 1a | `200`, `data` is `[]`, `meta.total` is `0` |
| 1b | No error, and no `null` where a list belongs |

**A `500` here is a fail**: an empty catalogue is the normal starting state, not an error.

## Scenario 2 — the operator creates a product, and it is not yet on sale

| # | Action | Expected |
|---|---|---|
| 2a | `POST $BASE/admin/products` with the body above | `201`, carrying `id`, `slug`, `price.amount: 120000`, `sellState: "COMING_SOON"`, `isSet: false`, `isPreorder: false` |
| 2b | `GET $BASE/products` | **Does not contain it** — a product announced but not on sale and not a pre-order is hidden (FR-002) |
| 2c | `GET $BASE/products/acrylic-stand-aki` | `404 PRODUCT_NOT_FOUND` |
| 2d | `GET $BASE/admin/products` | Contains it — the operator sees what a customer cannot |

**2b and 2c together are the check**: the product exists and the operator can see it, and no
customer-facing route reveals it.

## Scenario 3 — putting it on sale makes it visible

```bash
cat > /tmp/state.json <<'JSON'
{ "to": "ACTIVE" }
JSON
curl -s -o /dev/null -w '%{http_code}\n' -X POST "$BASE/admin/products/$ID/state" \
  -H "$ADMIN" -H 'Content-Type: application/json' --data-binary @/tmp/state.json
```

| # | Expected |
|---|---|
| 3a | `200`, `sellState: "ACTIVE"` |
| 3b | `GET $BASE/products` now contains it |
| 3c | `GET $BASE/products/acrylic-stand-aki` answers `200` with `description` and `images` |

## Scenario 4 — the public shape carries exactly what a customer needs

```bash
curl -s "$BASE/products" | python3 -m json.tool
```

| # | Expected |
|---|---|
| 4a | Each entry has `id`, `name`, `slug`, `price` (`amount` + `currency`), `imageUrl`, `isPreorder` |
| 4b | **No `sellState`, no `position`, no `categoryId`, no `isSet`.** Their presence is a fail: those are how the operator manages the catalogue, not what a customer reads (FR-008) |
| 4c | No `normalizedSlug` or any folding key anywhere in any response |
| 4d | `price.amount` is the integer that was sent, not a rounded or stringified value |

## Scenario 5 — a pre-order is visible but not buyable

```bash
cat > /tmp/pre.json <<'JSON'
{ "isPreorder": true, "preorderExpectedAt": "2026-12-01" }
JSON
curl -s -o /dev/null -w '%{http_code}\n' -X PATCH "$BASE/admin/products/$ID2" \
  -H "$ADMIN" -H 'Content-Type: application/json' --data-binary @/tmp/pre.json
```

`$ID2` is a second product still in `COMING_SOON`.

| # | Expected |
|---|---|
| 5a | `200`, `isPreorder: true` |
| 5b | `GET $BASE/products` **contains it**, with `isPreorder: true` — a pre-order is the one unlaunched product a customer sees (FR-002) |
| 5c | The public shape carries no date: availability is expressed by `isPreorder` alone |
| 5d | Launching it (`to: "ACTIVE"`) clears `isPreorder`, and the response shows `false` (FR-039) |
| 5e | Setting `isPreorder: true` on an `ACTIVE` product answers `400 VALIDATION_ERROR` naming `isPreorder` — a product that is on sale is not "coming soon" |

## Scenario 6 — hiding a category hides the products inside it

```bash
curl -s -o /dev/null -w '%{http_code}\n' -X PATCH "$BASE/admin/categories/$CAT" \
  -H "$ADMIN" -H 'Content-Type: application/json' --data-binary @/tmp/hide.json   # {"isVisible": false}
```

| # | Expected |
|---|---|
| 6a | `GET $BASE/products` no longer contains the products in that category, and `meta.total` dropped accordingly |
| 6b | `GET $BASE/products/acrylic-stand-aki` answers `404 PRODUCT_NOT_FOUND` |
| 6c | The product's own row is unchanged: `GET $BASE/admin/products/$ID` still shows `sellState: "ACTIVE"` (FR-002) |
| 6d | `GET $BASE/products?category=<hidden slug>` answers `200` with `data: []`, **not** a `404` — the filter reveals nothing about hidden categories (FR-006) |
| 6e | Showing the category again brings the products back exactly as they were |

**6c is the check that matters**: hiding a category must not edit the products. An implementation
that cascades the hide onto the product rows passes 6a and fails 6c.

## Scenario 7 — the sell state machine refuses what it should

| # | From | Request | Expected |
|---|---|---|---|
| 7a | `COMING_SOON` | `to: "ACTIVE"` | `200` |
| 7b | `ACTIVE` | `to: "OUT_OF_STOCK"` | `200` |
| 7c | `OUT_OF_STOCK` | `to: "ACTIVE"` | `200` — the restock edge |
| 7d | `OUT_OF_STOCK` | `to: "COMING_SOON"` | `409 PRODUCT_STATE_TRANSITION_INVALID`, naming `OUT_OF_STOCK` |
| 7e | `ACTIVE` | `to: "DISCONTINUED"` | `200` |
| 7f | `DISCONTINUED` | `to: "ACTIVE"` | `409`, naming `DISCONTINUED` |
| 7g | `DISCONTINUED` | `to: "DISCONTINUED"` | `409` |
| 7h | any | `to: "ON_SALE"` | `400 VALIDATION_ERROR`, naming `to` |
| 7i | after 7d | `GET $BASE/admin/products/$ID` | Still `OUT_OF_STOCK` — a refused transition changes nothing |

**7d and 7i together are the point**: an invalid transition is refused *and* leaves the product
untouched. A handler that writes the state and then validates would pass 7d's status and fail 7i.

## Scenario 8 — the slug rule and the bounds

| # | Body | Expected |
|---|---|---|
| 8a | `{"slug": "Acrylic Stand"}` | `400 VALIDATION_ERROR`, `details[].field` naming `slug` |
| 8b | `{"slug": "acrylic--stand"}` | `400`, double hyphen refused |
| 8c | `{"slug": "acrylic-"}` | `400`, trailing hyphen refused |
| 8d | `{"slug": "acrylic-stand"}` on the product that already has it | `200` — a product is never a duplicate of itself |
| 8e | `{"slug": "acrylic-stand"}` on a **different** product | `409 PRODUCT_SLUG_TAKEN`, `details[].field` naming `slug` |
| 8f | `{"slug": "ACRYLIC-STAND"}` on a different product | `409` — the comparison ignores letter case |
| 8g | a `name` of 120 Vietnamese characters | `201`/`200` — accepted at the bound |
| 8h | a `name` of 121 characters | `400`, the field named |
| 8i | a `description` of 5000 characters | `200` |
| 8j | a `description` of 5001 characters | `400` |
| 8k | two products with the **same name** | Both accepted — a product name is not unique (D5) |

**8g is the check a byte-counted bound fails.** 120 Vietnamese characters occupy about 360 bytes;
a bound counting bytes would refuse it at roughly a third of the allowance, and every ASCII test
would still pass.

## Scenario 9 — money is an integer, and a non-positive price is refused

| # | Body | Expected |
|---|---|---|
| 9a | `{"price": {"amount": 0, "currency": "VND"}}` | `400`, `details[].field` naming `price` |
| 9b | `{"price": {"amount": -1, "currency": "VND"}}` | `400`, the field named |
| 9c | `{"price": {"amount": 999999999999, "currency": "VND"}}` | `200`, and `GET` returns exactly `999999999999` |
| 9d | `{"price": {"amount": 100, "currency": "vnd"}}` | `400` — the currency is three uppercase letters |
| 9e | `SELECT price_amount, currency FROM products WHERE id = '...'` | `bigint`, the exact integer |

**9c is the check that catches a float in the path**: a JSON number carried through a `float64`
would come back rounded, and the round trip is what proves it did not.

## Scenario 10 — pictures

| # | Action | Expected |
|---|---|---|
| 10a | `POST $BASE/admin/products/$ID/images` with a JPEG | `201`, the product carries one picture, and it is `isPrimary: true` — the first one added becomes the main one |
| 10b | Add a second picture | `201`, `isPrimary: false` for the new one; the first stays primary |
| 10c | `POST .../images/$IMG2/primary` | `200`, `$IMG2` is now primary and the first is not |
| 10d | `DELETE .../images/$IMG2` | `204`, and the first picture is primary again — removing the main one promotes the next by position |
| 10e | `POST .../images` with a PDF | `400 PRODUCT_IMAGE_TYPE_UNSUPPORTED` — decided by the bytes, not by the file name |
| 10f | `POST .../images` with a 3 MB JPEG | `413 PRODUCT_IMAGE_TOO_LARGE` |
| 10g | Add pictures until ten exist, then add an eleventh | `409 PRODUCT_IMAGE_LIMIT_REACHED`, and **no asset was uploaded** — check the provider's usage did not move |
| 10h | `DELETE .../images/<an image id belonging to another product>` | `404 PRODUCT_NOT_FOUND` |
| 10i | `SELECT count(*) FROM product_images WHERE product_id = '...' AND is_primary` | `1`, never `0` while pictures exist and never `2` |
| 10j | Two concurrent `POST .../images/$IMG/primary` calls | Both `200` (or one `200` and one no-op), and 10i still holds |

**10g and 10i are the two checks a naive implementation fails**: an eleventh upload that reaches
the provider has already cost money and left an orphan, and two concurrent promotions leave a
product with two main pictures.

## Scenario 11 — a combo set

```bash
cat > /tmp/set.json <<'JSON'
{
  "name": "Combo Aki",
  "slug": "combo-aki",
  "price": { "amount": 300000, "currency": "VND" },
  "categoryId": "<category id>",
  "position": 20,
  "isSet": true,
  "memberProductIds": ["<product A id>", "<product B id>"]
}
JSON
```

| # | Expected |
|---|---|
| 11a | `201`, `isSet: true`, `price.amount: 300000` — the operator's price, **not** the sum of the members |
| 11b | `GET $BASE/admin/products/$SET` lists both members |
| 11c | `GET $BASE/products/combo-aki` carries **no** member list — the public shape does not enumerate a set's contents (D18) |
| 11d | `GET $BASE/products` still lists each member individually (US5 scenario 2) |
| 11e | `DELETE $BASE/admin/products/$SET` | `204`, and both members still exist |
| 11f | `POST` a set whose `memberProductIds` contains an unknown identifier | `400 VALIDATION_ERROR`, the field named |
| 11g | `POST` a set whose `memberProductIds` contains its own id | `400`, or `201` — a self-member is refused by the storage check, so it must not be `500` |

## Scenario 12 — a category with products cannot be removed

This is the debt feature 005 left, and this scenario is what closes it.

| # | Action | Expected |
|---|---|---|
| 12a | `DELETE $BASE/admin/categories/$CAT` while a product references it | `409 CATEGORY_IN_USE`, **not** `500` and not `204` |
| 12b | The category and its products are both still there | `GET $BASE/admin/categories/$CAT` answers `200` |
| 12c | `DELETE FROM categories WHERE id = '...'` run directly against PostgreSQL | The database itself refuses with a foreign-key violation — this is the check that the guarantee is in storage and not only in code |
| 12d | Move every product out of the category, then delete it | `204` |

**12c is the point of FR-035.** An application-level check would pass 12a and fail 12c; the
restricting reference is what makes the rule hold against any writer.

## Scenario 13 — the order never moves

| # | Action | Expected |
|---|---|---|
| 13a | Create four products with positions `0`, `10`, `10`, `20` | `201` each |
| 13b | `GET $BASE/products` twice and compare | Identical, including the two sharing a position |
| 13c | Add a fifth at position `10` | The three at `10` keep a stable relative order; `0` and `20` do not move |
| 13d | Rename the product at position `0` | The order does not change — the tie-break is not the name |
| 13e | `GET $BASE/products?category=<slug>` | Same relative order as the unfiltered list |

## Scenario 14 — role enforcement

| # | Request | Expected |
|---|---|---|
| 14a | `GET $BASE/admin/products` with no token | `401 UNAUTHENTICATED` |
| 14b | `POST $BASE/admin/products` with the customer token | `403 FORBIDDEN` |
| 14c | `POST $BASE/admin/products/$ID/state` with the customer token | `403 FORBIDDEN` |
| 14d | `POST $BASE/admin/products/$ID/images` with the customer token | `403 FORBIDDEN` |
| 14e | `GET $BASE/products` with no token | `200` — the public surface needs no token |

## Scenario 15 — the media provider being unavailable is a retryable answer

Unset `MEDIA_CLOUD_NAME` (or point the adapter at an unreachable host), restart the API, and:

| # | Expected |
|---|---|
| 15a | `POST .../images` answers `503 PRODUCT_MEDIA_UNAVAILABLE` |
| 15b | The product is unchanged — no picture row, and no half-written state |
| 15c | The API log carries the sanitised classification and **no** provider URL, credential or response body |

## Scenario 16 — pagination

| # | Request | Expected |
|---|---|---|
| 16a | `?page=1&pageSize=2` with five products | `200`, two entries, `meta.total` is `5` |
| 16b | `?page=3&pageSize=2` | The fifth product |
| 16c | `?pageSize=101` | `400 VALIDATION_ERROR`, `details[].field` naming `pageSize` |
| 16d | `?page=0` | `400`, the field named |
| 16e | A page of products where some are hidden by their category | `meta.total` counts only the visible ones, and the page is not short |

**16e is the check that catches a filter applied after the query**: dropping hidden rows in Go
returns a short page and a `total` that counts rows the caller never received.

## Automated gates

```bash
make lint          # format, tidy, vet, golangci-lint
make test          # unit
make test-integration   # needs Docker; proves the storage constraints
```

The storage-level guarantees are asserted against real PostgreSQL, because a fake repository that
enforces a unique index or a foreign key proves only that the fake was written twice: the slug
uniqueness, the price rule, the partial unique index that keeps one main picture, and — most of
all — the **restricting foreign key** that scenario 12c exercises.

If `make test-integration` reports success without executing — check for a skip — it is a false
green and does not count.

## What is deliberately not verified here

| Not verified | Why | Where it belongs |
|---|---|---|
| The sell state changing by itself when stock runs out | No stock is tracked; `OUT_OF_STOCK` is an operator's act (FR-038) | Module 05 Inventory; recorded in `deferred.md` |
| A set's contents shown to a customer | The specification does not ask the public shape to enumerate them (D18) | A later feature, if wanted; recorded in `deferred.md` |
| An order keeping a reference to a removed product | No order exists yet, so a removed product has no reader | Module 07 Order; recorded in `deferred.md` |
