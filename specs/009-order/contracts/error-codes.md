# Contract: Error codes — Order module

Authoritative for what this module can answer with. `specs/009-order/contracts/openapi.yaml`
describes where each appears; this file records what each means and which situations deliberately
reuse a shared code rather than inventing one.

## Codes this module adds

| Code | HTTP | Meaning | Retryable |
|---|---|---|---|
| `ORDER_NOT_FOUND` | 404 | No order carries that identifier, or it belongs to another customer. The two are one answer, so the route never confirms another customer's order | No |
| `ORDER_CART_EMPTY` | 409 | Checkout was asked for an empty cart. An order is never created from nothing | No — add something first |
| `ORDER_ITEM_NOT_PURCHASABLE` | 409 | A cart line's product is off sale, out of stock or removed, so it cannot be ordered. The item is named; nothing is created | No — remove or replace the item |
| `ORDER_ITEM_PRICE_CHANGED` | 409 | A cart line's product is priced differently from the price the customer was shown. Nothing is created; the customer reviews the cart | No — review the cart |
| `ORDER_QUANTITY_EXCEEDS_AVAILABLE` | 409 | A line asks for more than is available, or the goods could not be held. The item is named; nothing is created | No — reduce the quantity |
| `ORDER_NO_ADDRESS` | 409 | The customer has no delivery address, so an order has nowhere to go. Nothing is created | No — add an address first |
| `ORDER_STATE_TRANSITION_INVALID` | 409 | The requested move is not one the order's current state allows — cancelling a paid order, shipping an unpaid one. The message names the current state | No — the order must be moved through a legal state first |
| `ORDER_NOT_TRANSFERABLE` | 409 | The order is not paid, so it cannot be transferred. A transfer replaces cancelling a paid order and applies only to a paid order | No |
| `ORDER_TRANSFER_TARGET_NOT_FOUND` | 404 | No account carries the email the transfer names. Nothing is changed | No — check the email |

### Why these are conflicts and not validation failures

Each of the 409s is a well-formed request the order or the shelf cannot satisfy at that moment — an
empty cart, an item off sale, a price that moved, a quantity beyond stock, a state that does not allow
the move. Each leads to a different next step (add something, remove an item, review the cart, reduce
a quantity, move the order through a legal state), so they are conflicts the customer or operator can
act on, not malformed values. This is the same reasoning modules 03, 04, 05 and 06 used.

### Where the offending item is named

`ORDER_ITEM_NOT_PURCHASABLE`, `ORDER_ITEM_PRICE_CHANGED` and `ORDER_QUANTITY_EXCEEDS_AVAILABLE` set
`error.details[].field = "productId"` with an `issue` naming the product and, for the quantity code,
the available amount, so the customer knows what to change.

## Codes deliberately reused

| Situation | Code | Why not a module code |
|---|---|---|
| `addressId` is not a UUID, or names an address that is not the customer's | `VALIDATION_ERROR` (400) with `details[].field = "addressId"` | A malformed or foreign identifier is request shape, and naming the field is what the customer acts on |
| The transfer `email` is missing or not an email | `VALIDATION_ERROR` (400) with `details[].field = "email"` | Same: a value rule, with the field named |
| The request body does not parse, or carries an unknown field | `MALFORMED_REQUEST` (400) | The shared decoder owns this, established in module 01 |
| No token, or an expired one | `UNAUTHENTICATED` (401) | The foundation owns session validity |
| A customer's session reaches an administrator route | `FORBIDDEN` (403) | The rule is role-based and the foundation established the code |
| Page or page size outside its permitted range | `VALIDATION_ERROR` (400) | Pagination validation is a foundation concern |
| Something unexpected failed | `INTERNAL_ERROR` (500) | Never describe an internal failure in order terms |
| Too many requests | `RATE_LIMITED` (429) with `Retry-After` | The shared limiter answers this, not the module |

## Situations that deliberately have no code

| Situation | Why there is no code |
|---|---|
| A payment confirms an order | There is no HTTP surface for it: a real payment is module 08's, and the paid transition is driven by it, not by a request here |
| An unpaid order passes its window and cancels itself | It is not an error: the order moves to cancelled by itself, which the customer sees on the order, not as a failed request |
| An order's line's product is later removed | It is not an error: the line is a snapshot and reads fine |
| A paid order is transferred twice to the same account | The transfer is idempotent in effect (the owner is already the recipient); no code is owed |
