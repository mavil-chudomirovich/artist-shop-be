# Research — Catalog Reorder (Drag & Drop) (018)

Phase 0 decisions. Each records what was chosen, why, and the alternatives rejected. The spec's
`Clarifications` (last write wins; a documented safety maximum on the full-list fetch) and the
requirements-phase decisions (global product/category order; complete-set required; auto-front
placement) are inputs here, not open questions.

## D1 — A reorder is one bulk statement per list (UNNEST), never a loop

**Decision**: apply a reorder with a single statement:

```sql
UPDATE <table> AS t
SET position = pos.new_pos, updated_at = now()
FROM UNNEST($1::uuid[], $2::int[]) AS pos(id, new_pos)
WHERE t.id = pos.id
  [AND t.product_id = $3]   -- variants only
```

The `new_pos` array is the 0-based index of each id in the submitted order. Categories and
products update the whole (single) table; variants update only rows of the addressed product.

**Rationale**: one statement, zero per-row round trips, bounded by the request size — exactly the
spec's SC-005 ("no per-record round trips"). The `WHERE` on `product_id` is what stops a foreign
variant id from touching another product's rows.

**Alternatives rejected**: a loop of single-row `UPDATE`s (N round trips, lock churn); delete the
rows and reinsert (loses identities, timestamps and breaks foreign keys).

## D2 — Validation order: Go duplicate check → sibling-id read → bulk write

**Decision**: in the use case, in this order: (1) reject an empty array as `VALIDATION_ERROR`;
(2) detect duplicate ids in Go and answer the dedicated duplicate code; (3) read the sibling id
set (`ListIDs` / `ListIDsByProduct`) and require the request set to equal it, answering
`VALIDATION_ERROR` when a sibling is missing and the not-found / belongs-to-other-product code for
an unknown or foreign id; (4) run the bulk `UPDATE` and require `RowsAffected == len(ids)` as a
defensive invariant only.

**Rationale**: `RowsAffected` alone cannot tell a duplicate from a missing/foreign id, and
PostgreSQL silently collapses duplicate join matches to one updated row, so it would report the
wrong code. Explicit checks (2)–(3) produce the correct code per failure (spec FR-005–FR-008),
and (4) catches a race the pre-checks missed.

**Alternatives rejected**: relying on `RowsAffected < len(ids)` for every refusal (wrong code,
ambiguous); one big CTE that validates and updates (harder to map to distinct codes, harder to
unit test).

## D3 — Auto-front placement is `MIN(position) - 1`, not a full shift

**Decision**: when the create request omits `position`, the new record's position is
`COALESCE(MIN(position) - 1, 0)` over its sibling scope (all categories; all products; the parent
product's variants).

**Rationale**: one bounded read plus the insert — O(1) in the number of existing records, no
mass row locks. Because reorder resets positions to `0..n-1`, the smallest value stays small.
A first record in an empty list gets `0`.

**Alternatives rejected**: shifting every sibling by `+1` then inserting at `0` (O(n) writes and
locks on every create, and two concurrent creates can both land on `0`); fractional/lexicographic
ordering keys (new machinery for no measured need).

## D4 — Ordering scope: global for categories and products, per-product for variants

**Decision**: categories have one catalogue-wide order; products have one catalogue-wide order;
variants are ordered within their parent product. The products reorder endpoint therefore carries
only `productIds`, with no `categoryId`.

**Rationale**: the storage is a single `position` column with a global ordering index
(`categories_ordering_idx`, `products_ordering_idx`); a per-category product order would need a
new column or table and would conflict with the existing catalogue order (spec FR-016).

**Alternatives rejected**: per-category product ordering (rejected during requirements: schema
change and it breaks the current single catalogue order).

## D5 — A reorder must carry the complete set of siblings

**Decision**: the request must name every existing sibling. Missing sibling → `VALIDATION_ERROR`;
duplicate id → dedicated duplicate code; unknown id → not-found; a variant of another product →
`VARIANT_BELONGS_TO_OTHER_PRODUCT`. Any refusal leaves the stored order untouched.

**Rationale**: an unsent record keeps its old position and can interleave unpredictably with the
new order; requiring the complete set makes the saved order equal the submitted order and makes a
paginated fragment impossible to submit (spec FR-005–FR-008, edge cases).

**Alternatives rejected**: allowing partial reorders (ill-defined, unpredictable ordering).

## D6 — `position` becomes optional on create (backward compatible)

**Decision**: `Position` changes from `int` to `*int` on the category/product/variant create
request DTOs and their application inputs. A nil value means auto-front; a supplied value is
honoured. The update DTOs already use `*int` and are unchanged. The admin interface stops showing
the field, but the API keeps accepting it.

**Rationale**: the JSON decoder rejects unknown fields, so removing the field would be a breaking
change for existing clients and would ripple through many tests and the seed. Keeping it optional
satisfies "no manual order input" at the UI without breaking the contract (spec FR-011, FR-012).

**Alternatives rejected**: removing the field entirely (breaking contract + broad test/seed churn).

## D7 — Batch variant positions follow payload order

**Decision**: in `CreateProduct`, a variant whose position is omitted takes its index in the
payload (`0,1,2…`), so the first payload variant is the default. `AddVariant` (single variant)
uses `MIN(position) - 1` so the added variant appears first.

**Rationale**: a batch create has no existing siblings; index assignment makes "first = default"
deterministic instead of leaving every variant at `0` and tie-breaking by `created_at`/`id`
(which would be effectively random).

**Alternatives rejected**: assigning `0` to every omitted batch variant (nondeterministic default
variant); computing `MIN - 1` per inserted variant (all still `0`, same problem).

## D8 — Error codes and statuses

**Decision**: new codes `CATEGORY_REORDER_DUPLICATE_ID`, `PRODUCT_REORDER_DUPLICATE_ID`,
`VARIANT_REORDER_DUPLICATE_ID`, `VARIANT_BELONGS_TO_OTHER_PRODUCT` — all HTTP 400. Reuse the
existing `CATEGORY_NOT_FOUND` / `PRODUCT_NOT_FOUND` (404) and the shared `VALIDATION_ERROR` (400)
for empty or incomplete arrays. No `PRODUCT_BELONGS_TO_OTHER_CATEGORY` code, because product
ordering is global.

**Rationale**: a duplicate or a foreign id is a request-shape problem the client can fix, so 400
with a distinct machine-readable code is right and lets the client point at the offending row. An
unknown id is a not-found on the addressed resource.

**Alternatives rejected**: 409 for duplicates (there is no stored conflict — the body itself is
malformed); one generic code (the client could not tell duplicate from foreign).

## D9 — Audit: one bulk event per reorder

**Decision**: each reorder emits exactly one audit event through the existing auditor
(`AuditCategoryReordered`, `AuditProductReordered`, `AuditProductVariantsReordered`), carrying the
actor and metadata `{count, ids}`. The event's target names the ordered scope: a category reorder
uses `targetType="category"` with an empty `targetID`; a product reorder uses
`targetType="product"` with an empty `targetID`; a variant reorder uses `targetType="product"`
with `targetID` = the parent product's id. The affected ids travel in the metadata, not in the
target, because a bulk reorder has no single target row. No per-record event.

**Rationale**: Constitution VI requires the mutation and the actor recorded, not one event per
row; one event keeps audit volume proportional to the action and matches the "one write" goal.

**Alternatives rejected**: one event per record (defeats the single-write intent); no audit
(Constitution VI violation).

## D10 — Full-list fetch for reorder: `all=true` with a safety maximum

**Decision**: `GET /admin/categories` and `GET /admin/products` accept `all=true`, returning the
full ordered list. A documented maximum (`maxAdminListAll = 1000`) bounds the result; a request
that would exceed it is refused with a validation error. The reorder endpoints validate
completeness against the repository's sibling-id read, never against a page.

**Rationale**: drag & drop across the whole list needs the whole list; the bound protects against
an unbounded response (spec FR-015, Clarifications). If a catalogue ever exceeds the maximum,
whole-list reorder is unavailable until the cap is revisited — an accepted, documented limit.

**Alternatives rejected**: no cap (unbounded response/query); leaving pagination as the only fetch
(a page fragment cannot be reordered without corrupting the order).

## D11 — Concurrency: last-write-wins, no version token

**Decision**: no optimistic-concurrency token. Two reorders race in the ordinary way; each writes
atomically and the later commit defines the stored order.

**Rationale**: accepted in the spec Clarifications; YAGNI (Constitution VII). Ordering is an admin
convenience, not money or stock, so the rare race does not justify a version column, comparison
logic and extra tests.

**Alternatives rejected**: a per-list version/`updatedAt` token with stale-save rejection (new
field and logic for a rare admin race).

## D12 — No migration; the true storefront effect of variant order

**Decision**: reuse the existing `position` columns and ordering indexes; add no index and no
column. Reordering variants changes the order customers see on the product detail and changes
which variant is the "default" (the smallest position) used as the fallback image when the
product has no primary gallery picture. It does **not** change the catalogue card price, which is
the lowest on-sale variant price (`MIN(COALESCE(v.price_amount, p.price_amount))`), independent
of order.

**Rationale**: the columns and indexes already support every read and write; the spec's storefront
claim is corrected to the true behaviour so tests assert the right outcome.

**Alternatives rejected**: adding an explicit `default_variant_id` column (a second source of
truth for a fact "smallest position is default" already expresses).
