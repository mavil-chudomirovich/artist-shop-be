# Contract: Error codes — Catalog Reorder (Drag & Drop)

Authoritative for what the three reorder actions can answer with. The shared envelope, error
catalogue conventions and the per-module codes below live in `docs/api-reference.md`, which
remains the authoritative cross-module reference (Constitution VIII).

## Codes this feature adds

| Code | HTTP | Meaning | Retryable |
|---|---|---|---|
| `CATEGORY_REORDER_DUPLICATE_ID` | 400 | The `categoryIds` array names the same category more than once | No — send each id once |
| `PRODUCT_REORDER_DUPLICATE_ID` | 400 | The `productIds` array names the same product more than once | No — send each id once |
| `VARIANT_REORDER_DUPLICATE_ID` | 400 | The `variantIds` array names the same variant more than once | No — send each id once |
| `VARIANT_BELONGS_TO_OTHER_PRODUCT` | 400 | A `variantId` in the reorder does not belong to the product on the path | No — order only that product's variants |

Each carries a field-level detail naming the offending member (`categoryIds`, `productIds`,
`variantIds`) so a client can point at the array it sent.

### Why duplicates and foreign ids are validation failures (400) not conflicts (409)

The request body is malformed for the operation — it repeats an identifier, or names a record
outside the addressed scope — and the operator's next step is to fix the array they sent. There is
no stored conflict to resolve (unlike the pending-orders 409s of feature 016). 400 with a distinct
code is the correct answer, and the distinct code lets the client tell a duplicate from a foreign
id without parsing a message.

## Codes deliberately reused

| Situation | Code | Why not a new code |
|---|---|---|
| Empty reorder array | `VALIDATION_ERROR` (400) | Nothing to apply; a request-shape problem |
| A reorder omits a sibling record | `VALIDATION_ERROR` (400) | The array is incomplete; a request-shape problem |
| A reorder names an unknown category | `CATEGORY_NOT_FOUND` (404) | The addressed record simply does not exist |
| A reorder names an unknown product | `PRODUCT_NOT_FOUND` (404) | The addressed record simply does not exist |
| A variant reorder for an unknown product | `PRODUCT_NOT_FOUND` (404) | The addressed parent does not exist |
| A malformed identifier, page window or `all` value | the shared request-shape errors | Unchanged request validation |
| A non-admin caller | the shared authorization refusal | Unchanged role enforcement |
| A complete-list fetch beyond the safety maximum | `VALIDATION_ERROR` (400) | Request-shape problem; no new code |

There is deliberately **no** `PRODUCT_BELONGS_TO_OTHER_CATEGORY` code: product ordering is
catalogue-wide (spec FR-016), so a product never belongs to a different category for ordering
purposes.
