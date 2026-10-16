# Contract: Error codes — Hide Guard with Pending Orders

Authoritative for what the three hide actions can answer with. The shared envelope, error catalogue
conventions, and the per-module codes below live in `docs/api-reference.md`, which remains the
authoritative cross-module reference (Constitution VIII).

## Codes this feature adds

| Code | HTTP | Meaning | Retryable |
|---|---|---|---|
| `CATEGORY_HAS_PENDING_ORDERS` | 409 | Hiding the category was refused: at least one PENDING or PAYMENT_PENDING order holds one of its variants. Detail carries the blocking order IDs | No — resolve or wait out the listed orders first |
| `PRODUCT_HAS_PENDING_ORDERS` | 409 | Turning the product's visibility flag off was refused for the same reason | No — resolve or wait out the listed orders first |
| `VARIANT_HAS_PENDING_ORDERS` | 409 | Moving the variant away from on-sale was refused for the same reason | No — resolve or wait out the listed orders first |

### Why the refusals are conflicts and not validation failures

The request is well-formed and understandable; the current data forbids it. That is a different
situation from a malformed value, and the operator's next step is different — clear the blocking
orders, rather than fix the value they sent.

### Why the order identifiers travel in the refusal

The spec requires the refusal to name the blocking orders so an administrator can act on it. All
three hide actions sit behind the ADMIN guard, so customer-facing information leak is not a concern.

## Codes deliberately reused

| Situation | Code | Why not a new code |
|---|---|---|
| A hidden, retired, removed or unknown item is read | the existing not-found of that surface | The item is simply not there for that audience |
| A malformed identifier or body | the shared request-shape errors | Unchanged request validation |
| A non-admin caller | the shared authorization refusal | Unchanged role enforcement |
| A blocked system stock-out signal | no error at all | By design it is a recorded no-op, never a refusal (the stock write must not fail) |
