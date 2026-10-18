# Quickstart: Order Delivery & Completion

Validation guide. Every scenario states the request, the expected answer, and how to tell a real pass
from a false one.

**How to read the outcomes**: `PASS` means the observed answer matches. `FAIL` means it does not. A
scenario that cannot run here is reported `BLOCKED` with the reason — never silently skipped.

**A note on the paid path**: an order reaches `PAID` only when module 08 Payment confirms a payment, and
module 08 has no HTTP surface. The scenarios that need a **paid** order — dispatch, delivery — are
covered by the automated tests, which call the paid transition directly, and can also be driven against a
row set to `PAID` in a dev database.

## Prerequisites

- Dependencies running: `make up-tools`.
- Migration applied: `make migrate-up` — this feature adds `migrations/00018_order_delivery.sql`.
- API running: `make run`, or `go run ./cmd/api`.
- `MEDIA_*` (Cloudinary) configured if you want real photo uploads; without them the media store is
  unavailable and an upload answers `INTERNAL_ERROR` (the order is left unchanged).
- A signed-in customer token (`CUSTOMER`) with a **paid** order (`PAID`), a second customer token
  (`CUSTOMER2`), and an **administrator** token (`ADMIN`).
- An SMTP sink (Mailpit, via `make up-tools`) if you want to read the notification emails.

Set `BASE=http://localhost:8080/api/v1`, `CUSTOMER=...`, `CUSTOMER2=...`, `ADMIN=...`, `O=<order id>`.

**On Windows, never send a JSON body inline.** Write the body to a file and send it with
`--data-binary @file`. For multipart uploads, pass photos with `-F "photos=@photo.jpg"`.

## Scenario 1 — the artist dispatches a paid order with photos

| # | Action | Expected |
|---|---|---|
| 1a | `POST $BASE/admin/orders/$O/ship -F photos=@a.jpg -F photos=@b.jpg` as ADMIN | `200`, `status: "SHIPPING"`, and the two photos appear in `deliveryPhotos` with `stage: "SHIPPING"` |
| 1b | `GET $BASE/orders/$O` as CUSTOMER | the order detail shows the same photos and no `deliveredAt` |
| 1c | `POST $BASE/admin/orders/$O/ship` again | `409 ORDER_STATE_TRANSITION_INVALID`, naming `SHIPPING` |
| 1d | `POST $BASE/admin/orders/$O/ship -F photos=@a.jpg` (6 files) | `409 ORDER_IMAGE_LIMIT_REACHED` |
| 1e | `POST $BASE/admin/orders/$O/ship -F photos=@note.txt` | `400 ORDER_IMAGE_TYPE_UNSUPPORTED` |
| 1f | the customer's mailbox | an email that the order is in transit |

**1a is the check the feature adds**: a paid order now stops at "in transit" with evidence, instead of
jumping to a delivered state.

## Scenario 2 — the artist marks the order delivered

| # | Action | Expected |
|---|---|---|
| 2a | `POST $BASE/admin/orders/$O/deliver -F photos=@c.jpg` as ADMIN | `200`, `status: "DELIVERED"`, `deliveredAt` set, and the photo appears with `stage: "DELIVERED"` |
| 2b | `POST $BASE/admin/orders/$O/deliver` on an order that is already `DELIVERED` | `409 ORDER_STATE_TRANSITION_INVALID`, naming `DELIVERED` |
| 2c | `POST $BASE/admin/orders/$O/deliver` on an order still `PAID` (skip dispatch) | `409 ORDER_STATE_TRANSITION_INVALID`, naming `PAID` |
| 2d | the customer's mailbox | an email that the order was delivered |

## Scenario 3 — the customer confirms receipt

| # | Action | Expected |
|---|---|---|
| 3a | `POST $BASE/orders/$O/complete` as CUSTOMER | `200`, `status: "COMPLETED"`, `completedAt` set |
| 3b | `GET $BASE/orders/$O` as CUSTOMER2 (another customer) | `404 ORDER_NOT_FOUND` |
| 3c | `POST $BASE/orders/$O/complete` on an order still `SHIPPING` | `409 ORDER_STATE_TRANSITION_INVALID`, naming `SHIPPING` |
| 3d | the customer's mailbox | an email that the order is complete |

## Scenario 4 — feedback goes to the artist, once

| # | Action | Expected |
|---|---|---|
| 4a | `POST $BASE/orders/$O/feedback` `{"content":"Thank you!"}` as CUSTOMER on a `DELIVERED` order | `200`, the order detail now carries `feedback.content` |
| 4b | `POST $BASE/orders/$O/feedback` a second time | `409 ORDER_FEEDBACK_ALREADY_SENT` |
| 4c | `POST $BASE/orders/$O/feedback` on an order still `SHIPPING` | `409 ORDER_FEEDBACK_NOT_ALLOWED`, naming the state |
| 4d | `POST $BASE/orders/$O/feedback` `{"content":""}` | `400 VALIDATION_ERROR`, `details[].field = "content"` |
| 4e | the artist's mailbox | an email that feedback arrived |

## Scenario 5 — the operator and role guards (automatic)

| # | Action | Expected |
|---|---|---|
| 5a | `POST $BASE/admin/orders/$O/ship` as CUSTOMER | `403 FORBIDDEN` |
| 5b | `POST $BASE/orders/$O/complete` with no token | `401 UNAUTHENTICATED` |
| 5c | `POST $BASE/admin/orders/$O/complete` | `404`/`405` — the route was removed |

## Scenario 6 — automatic completion, the rename, and the storage guarantees (integration)

The five-day clock cannot be wound by hand and the paid path is module 08's, so these are proven by the
automated tests (direct use cases over real PostgreSQL), not here:

| # | Action | Expected |
|---|---|---|
| 6a | a `DELIVERED` order past its five-day window | becomes `COMPLETED` **exactly once**, with no actor |
| 6b | a `SHIPPING` order left for a long time | stays `SHIPPING` — it never completes by time |
| 6c | the customer confirms at the same moment the sweep completes | `COMPLETED` once; the second act is refused/skipped, never a double |
| 6d | a database holding `SHIPPED` rows, after the migration | those orders read `SHIPPING`, none lost |
| 6e | insert an order with a status outside the seven | refused by `orders_status_ck` |
| 6f | a six-photo dispatch | refused with `ORDER_IMAGE_LIMIT_REACHED` before the media store is called; a second dispatch of the same order is refused naming `SHIPPING` (the edge fires once) |
| 6g | delete an order | its `order_delivery_photos` and `order_feedback` rows are gone with it |
| 6h | insert a second feedback row for one order | refused by `order_feedback_order_key` |

## Automated gates

```bash
make lint              # format, tidy, vet, golangci-lint
make test              # unit, including the seven-state machine and the completion/feedback refusals
make test-integration  # needs Docker; proves the storage guarantees, the rename backfill, the races
make swagger           # regenerate docs/swagger after the handler annotations
```

If `make test-integration` reports success without executing — check for a skip — it is a false green.

## What is deliberately not verified here

| Not verified | Why | Where it belongs |
|---|---|---|
| A real payment turning an order `PAID` over HTTP | Payment is module 08 and has no HTTP surface | Module 08; recorded in `deferred.md` |
| A delivery that fails or is refused | No failed/returned state in this feature | `deferred.md`; a later feature |
| Restricting or extending transfer across `SHIPPING`/`DELIVERED` | Deferred decision | `deferred.md` |
| Shipping fees and carrier tracking | Module 09 proper | Module 09 |
| Product reviews with ratings | Module 13 (V2.0) | Module 13 |
| In-app notifications | Module 12 is unbuilt; this feature sends email only | Module 12 |
