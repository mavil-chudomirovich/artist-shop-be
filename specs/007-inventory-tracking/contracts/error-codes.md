# Contract: Error codes — Inventory module

Authoritative for what this module can answer with. `specs/007-inventory-tracking/contracts/openapi.yaml`
describes where each appears; this file records what each means and, equally, which situations
deliberately reuse a shared or another module's code rather than inventing one.

## Codes this module adds

| Code | HTTP | Meaning | Retryable |
|---|---|---|---|
| `INVENTORY_INSUFFICIENT_STOCK` | 409 | The change would take the product's physical quantity below zero, or below the quantity currently held for orders being paid. Nothing is changed | No — restock, wait for the holds to resolve, or correct the count upwards |

### Why the insufficient-stock refusal is a conflict and not a validation failure

The quantity is a well-formed positive whole number and the request is understandable; what makes it
impossible is the current state of the shelf — there are not that many units. That is a different
situation from a malformed value, and the operator's next step is different: bring more stock in, or
wait for the holds to be paid, expired or cancelled, rather than fix the number they sent. This is
the same reasoning module 03 and module 04 used for their state and reference refusals, and it is
what keeps "the shelf said no" distinguishable from "the request was wrong".

## Codes deliberately reused

| Situation | Code | Why not a module code |
|---|---|---|
| The product identifier names no product, or the product was removed | `PRODUCT_NOT_FOUND` (404) | The product is module 04's resource and the code is already this module's answer to "no such product" on the administrator surface. A second, near-identical `INVENTORY_PRODUCT_NOT_FOUND` would make a client branch on two codes for one condition |
| The quantity is missing, zero where a positive amount is required, negative, or not a whole number | `VALIDATION_ERROR` (400) with `details[].field` | The value is invalid on its own, which is what the shared code means, and the field detail is what the operator acts on. The project established this shape in module 02 and module 04 uses it for its own value rules |
| The path identifier is not a UUID | `VALIDATION_ERROR` (400) with `details[].field` | A malformed path segment is request shape, not stock state |
| The request body does not parse, or carries an unknown field | `MALFORMED_REQUEST` (400) | The shared decoder owns this, established in module 01 |
| No token, or an expired one | `UNAUTHENTICATED` (401) | The foundation owns session validity for every module |
| A customer calls an inventory route | `FORBIDDEN` (403) | The rule is role-based and the foundation established the code |
| Page or page size outside its permitted range | `VALIDATION_ERROR` (400) | Pagination validation is a foundation concern |
| Something unexpected failed | `INTERNAL_ERROR` (500) | Never describe an internal failure in stock terms |
| Too many requests | `RATE_LIMITED` (429) with `Retry-After` | The shared limiter answers this, not the module |

## Situations that deliberately have no code

| Situation | Why there is no code |
|---|---|
| An outside event has already been applied, or a hold cannot be found to consume | There is no HTTP surface for holding stock. The application accepts a repeat as a **success** (FR-021), because a retrying caller must stop retrying rather than be told it failed |
| A request to hold more than is available | There is no HTTP surface for holding stock; the refusal is an application-level outcome the future order/payment flow branches on |
| A hold has expired | Expiry is not an error: the quantity simply returns to availability, and a payment that arrives after it is a matter for the order flow |
| A product has no stock row | It is understood as quantity **zero**, so reading it answers `0`, not a not-found (D12). There is nothing exceptional to report |
| The stock is below some threshold | This feature deliberately has no low-stock threshold (spec FR-029); the situation has no answer because it is not modelled |
