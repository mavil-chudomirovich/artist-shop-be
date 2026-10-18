# Data Model — Catalog Reorder (Drag & Drop) (018)

No new table and no schema change. The feature reuses the existing `position` columns and their
ordering indexes; the only new storage access is a set of bounded reads and bulk writes, each one
statement.

## Reused columns and indexes (no migration)

| Table | Column | Index reused | Meaning |
|---|---|---|---|
| `categories` | `position integer NOT NULL` | `categories_ordering_idx (position, created_at, id)` | one catalogue-wide category order |
| `products` | `position integer NOT NULL` | `products_ordering_idx (position, created_at, id)` | one catalogue-wide product order |
| `product_variants` | `position integer NOT NULL` | `product_variants_product_idx (product_id, position, created_at, id)` | order within the parent product |

Ordering everywhere is `position ASC, created_at ASC, id ASC`; the id breaks the remaining tie and
is immutable, so identical requests return the same order. "Smallest position" is the first
record, and for variants the **default variant** (used as the fallback image when the product has
no primary gallery picture). `position` accepts any whole number, including zero and negative.

The `MIN(position)` read used by auto-front placement is served by the ordering indexes (their
leading column is `position` / `product_id, position`), so no new index is needed.

## Reads and writes (all new, all bounded)

### Reorder a list (category / product)

```
UPDATE categories AS c
SET position = pos.new_pos, updated_at = now()
FROM UNNEST($1::uuid[], $2::int[]) AS pos(id, new_pos)
WHERE c.id = pos.id
```

The products variant is the same statement against `products`. `$2` is `0..n-1` in submitted
order. Returns the affected-row count. New repository method on
`domain/repository/category.go` / `product.go`, implemented in the postgres adapter.

### Reorder a product's variants

```
UPDATE product_variants AS v
SET position = pos.new_pos, updated_at = now()
FROM UNNEST($1::uuid[], $2::int[]) AS pos(id, new_pos)
WHERE v.id = pos.id AND v.product_id = $3
```

The `v.product_id = $3` predicate is what makes a foreign variant id a no-op rather than a
cross-product write. New repository method on `domain/repository/variant.go`.

### Sibling id read (completeness / existence check)

- Categories: `SELECT id FROM categories` → set of all category ids.
- Products: `SELECT id FROM products` → set of all product ids.
- Variants: `SELECT id FROM product_variants WHERE product_id = $1` → set of the product's variant ids.

Used to require the request set to equal the sibling set (D2). One statement each.

### Auto-front placement read

- `SELECT COALESCE(MIN(position) - 1, 0) FROM categories` (and `products`).
- `SELECT COALESCE(MIN(position) - 1, 0) FROM product_variants WHERE product_id = $1`.

An empty sibling set yields `0`; otherwise one less than the current smallest.

### Product existence read (variant reorder only)

`ProductRepository.Exists(id)` (already exists) answers whether the parent product is present, so
a variant reorder for an unknown product answers `PRODUCT_NOT_FOUND` before any variant check.

## Entity changes

None to the persisted entities. `Position` on `model.Category`, `model.Product` and
`model.Variant` is unchanged (`int`); only the request/input DTOs change `position` to an optional
`*int`, and the use case resolves nil to the auto-front value before the entity is built.

## Validation rules (from the spec)

- A reorder array MUST NOT be empty (FR; else `VALIDATION_ERROR`).
- A reorder array MUST NOT repeat an identifier (dedicated duplicate code, D8).
- A reorder array MUST contain every sibling: a missing sibling is `VALIDATION_ERROR`; an unknown
  id is the resource not-found (404); a variant of another product is
  `VARIANT_BELONGS_TO_OTHER_PRODUCT` (400) (FR-005–FR-008).
- A reorder is atomic: on any refusal nothing is written; on success the stored order equals the
  submitted order (FR-004).
- On create with an omitted `position`, the record is placed at the front of its list
  (`MIN(position) - 1`); a supplied `position` is honoured, and an edit that omits it keeps the
  current order (FR-011, FR-012).
- Product and category ordering is catalogue-wide; variant ordering is within the product
  (FR-016).
- The complete-list fetch (`all=true`) is bounded by `maxAdminListAll = 1000`; beyond it the
  request is refused with a validation error (FR-015).

## Storefront effect (for acceptance tests)

- Category/product order on the public lists equals the saved order (FR-013).
- Variant order on the public product detail equals the saved order, and the smallest-position
  variant is the default used as the fallback image (FR-014).
- The catalogue card price is the lowest on-sale variant price and does **not** change with
  variant order (correcting the source plan's claim; D12).
