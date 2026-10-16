# Contract: Error codes — Product SKU Model (015)

Authoritative for the catalogue/cart surface after the restructure. This feature **adds** variant codes
and **removes** feature 014's grouping codes.

## Codes this feature adds

| Code | HTTP | Meaning | Retryable |
|---|---|---|---|
| `PRODUCT_CODE_TAKEN` | 409 | Another product already uses that product code (compared ignoring case and surrounding whitespace) | No — change the code |
| `VARIANT_SKU_TAKEN` | 409 | Another variant already uses that SKU (compared ignoring case and surrounding whitespace) | No — change the SKU |
| `VARIANT_NOT_FOUND` | 404 | No variant carries that identifier for the product (an unknown variant id). A variant that exists but is not buyable is **not** this code — on a catalogue read it is simply not returned, and on a cart add it is the cart's not-purchasable refusal | No |
| `VARIANT_LAST_REMOVAL` | 409 | The variant is the product's last one; a product must keep at least one variant | No — add another variant first |
| `VARIANT_STATE_TRANSITION_INVALID` | 409 | The requested sell-state change is not one the variant's current state allows; the message names the current state | No — move it through a legal state first |

## Codes kept (meaning adjusted)

| Code | HTTP | Change |
|---|---|---|
| `PRODUCT_NOT_FOUND` | 404 | Still covers an unknown or hidden product; a product now hidden because **none of its variants is on sale** joins its hidden cases. The public product detail and slug still answer it |
| `CART_*`, `ORDER_*`, `VALIDATION_ERROR`, `MALFORMED_REQUEST`, `UNAUTHENTICATED`, `FORBIDDEN`, `RATE_LIMITED`, `INTERNAL_ERROR` | as today | Unchanged; reused by the cart/order modules, whose routes are only re-keyed to a variant id |

## Codes this feature removes (feature 014)

`VARIANT_GROUP_NOT_FOUND`, `VARIANT_GROUP_SLUG_TAKEN`, `VARIANT_GROUP_HAS_OPTIONS`,
`VARIANT_LABEL_TAKEN`, `PRODUCT_ALREADY_IN_VARIANT_GROUP` — their endpoints no longer exist
(`/product-groups/*`, `/admin/product-groups/*`). A client that handled them handles the new codes
above.

**`PRODUCT_STATE_TRANSITION_INVALID` (feature 006)** is also removed: the product has no sell state any
more, so `POST /admin/products/{id}/state` is gone and `VARIANT_STATE_TRANSITION_INVALID` replaces it on
the variant state endpoint.

## Why the code/SKU collisions are conflicts and not validation failures

A duplicate product code or SKU is a well-formed value that the current data does not allow — the same
shape as module 04's existing `PRODUCT_SLUG_TAKEN`. A `400` would say the request was malformed when
what is wrong is the value's uniqueness.

## Why the last-variant removal is a conflict

Removing the only variant would leave a product that cannot satisfy "at least one variant" or "default
= first variant". The request is understandable; the product's state forbids it.

## Codes deliberately reused

| Situation | Code | Why not a new code |
|---|---|---|
| A required field blank, over bound, a price not positive, a code/SKU malformed, an unknown category, a variant with no name | `VALIDATION_ERROR` (400) with `details[].field` | The value is invalid on its own; the field detail is the established shape |
| Path identifier not a UUID | `VALIDATION_ERROR` (400) | Malformed path segment is request shape |
| Body does not parse, unknown field | `MALFORMED_REQUEST` (400) | The shared decoder owns this |
| No token / expired | `UNAUTHENTICATED` (401) | The foundation owns session validity |
| A customer calls an admin route | `FORBIDDEN` (403) | Role-based; the foundation established it |
| Page/page size out of range | `VALIDATION_ERROR` (400) | Pagination is a foundation concern |
| Something unexpected failed | `INTERNAL_ERROR` (500) | Never describe an internal failure in catalogue terms |
| Too many requests | `RATE_LIMITED` (429) with `Retry-After` | The shared limiter answers this |
| Adding a variant not on sale / short of stock | `CART_PRODUCT_NOT_PURCHASABLE` / `CART_QUANTITY_EXCEEDS_AVAILABLE` | The cart module's existing codes, now keyed by variant; their `details[].field` becomes `variantId` |
| Checkout refuses because a line's captured price no longer matches the variant's current price | the order module's existing item-price-changed refusal (feature 009) | The checkout's re-check rule is the order module's; re-keying it to a variant does not change its code |

## What is not here

No code exists for "a product has no variant": creation requires ≥1 and the last removal is refused, so
the state cannot arise. No code exists for "a variant's price is missing": a null price is valid and
falls back to the product's default. Feature 014's `PRODUCT_IMAGE_*` codes remain (the product gallery
is unchanged); a variant has one image and reuses the same media rules.
