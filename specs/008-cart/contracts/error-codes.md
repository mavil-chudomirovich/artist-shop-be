# Contract: Error codes — Cart module

Authoritative for what this module can answer with. `specs/008-cart/contracts/openapi.yaml`
describes where each appears; this file records what each means and, equally, which situations
deliberately reuse a shared or another module's code rather than inventing one.

## Codes this module adds

| Code | HTTP | Meaning | Retryable |
|---|---|---|---|
| `CART_PRODUCT_NOT_PURCHASABLE` | 409 | The product exists but is not on sale — it is announced, out of stock or retired — so it cannot be added or kept as a line. Nothing is changed | No — the product must go on sale first |
| `CART_QUANTITY_EXCEEDS_AVAILABLE` | 409 | The requested quantity, or the quantity an add would produce, is greater than the amount currently available. Nothing is changed | No — reduce the quantity |

### Why these two are conflicts and not validation failures

The quantity is a well-formed positive whole number and the product is a real product; what makes the
request impossible is the state of the product or the shelf, not the shape of the request. The
operator's next step is different from fixing a value — wait for the product to go on sale, or reduce
the quantity — so the two are conflicts, the same reasoning modules 03 and 04 used for their own
state and reference refusals. It also keeps "the shelf said no" distinguishable from "the request was
wrong".

### Where the product and the available quantity are reported

A refusal names what the customer must change, in the shared `error.details` array:

- `CART_PRODUCT_NOT_PURCHASABLE` sets `details[].field = "productId"` with an `issue` naming the
  state ("is not on sale"), so the customer knows which product failed.
- `CART_QUANTITY_EXCEEDS_AVAILABLE` sets `details[].field = "quantity"` with an `issue` stating the
  currently available amount, so the customer can reduce the line to it.

## Codes deliberately reused

| Situation | Code | Why not a module code |
|---|---|---|
| Adding, changing or removing a line for a product that does not exist, or was removed | `PRODUCT_NOT_FOUND` (404) | The product is module 04's resource and the code is already this service's answer to "no such product". A second, near-identical `CART_PRODUCT_NOT_FOUND` would make a client branch on two codes for one condition |
| Changing or removing a line the customer does not hold | `PRODUCT_NOT_FOUND` (404) | A line that is not there and a product that is not there are deliberately one answer, so the route never confirms what is in another customer's cart |
| The quantity is missing, zero, negative, or not a whole number | `VALIDATION_ERROR` (400) with `details[].field` | The value is invalid on its own, which is what the shared code means, and the field detail is what the customer acts on. The project established this shape in module 02 |
| The product identifier is not a UUID | `VALIDATION_ERROR` (400) with `details[].field` | A malformed path segment is request shape, not cart state |
| The request body does not parse, or carries an unknown field | `MALFORMED_REQUEST` (400) | The shared decoder owns this, established in module 01 |
| No token, or an expired one | `UNAUTHENTICATED` (401) | The foundation owns session validity for every module |
| Something unexpected failed | `INTERNAL_ERROR` (500) | Never describe an internal failure in cart terms |
| Too many requests | `RATE_LIMITED` (429) with `Retry-After` | The shared limiter answers this, not the module |

## Situations that deliberately have no code

| Situation | Why there is no code |
|---|---|
| A line's product went off sale or ran short after being added | This is not an error: the read reports it on the line itself (the `buyable` flag and, when short, `availableQuantity`), so the customer can fix it before checkout. It becomes a refusal only when they try to add or change |
| The cart is empty | An empty cart is the normal state, not an error; the response is an empty list (FR-004) |
| A customer reads another customer's cart | There is no route that names a cart, so there is nothing to attempt: the owner is the session. A request without a session is `UNAUTHENTICATED` |
| The price changed since a line was added | The cart shows the captured price; the checkout re-checks it (FR-008). The cart answers no code for a change it is not the place to refuse |
