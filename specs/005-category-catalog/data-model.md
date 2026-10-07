# Phase 1 Data Model: Category Catalogue

One new table. The decisions behind its shape are in `research.md`; this file records what is
stored, what is constrained, and what the read paths project.

## Entity: Category

What a category is, in the operator's terms: a way of grouping what the shop sells, with a name
they wrote, a description customers read, a public link segment, a place in the order, and a
choice of whether customers can see it.

### Stored columns

| Column | Type | Null | Meaning |
|---|---|---|---|
| `id` | uuid | no | Primary key. Generated once and never changed — it is the reference a product will use, and the administrator surface addresses a category by it |
| `name` | text | no | The name the operator typed, trimmed. This is what a customer reads |
| `normalized_name` | text | no | The folding key for uniqueness (D2, D3). Never returned to anyone |
| `slug` | text | no | The link segment the operator wrote, lowercase and URL-safe by validation (FR-018). This is what a public link is built from |
| `normalized_slug` | text | no | The folding key for uniqueness. Never returned to anyone |
| `description` | text | no | What the operator wrote; an empty string is a valid description (the spec's edge case) |
| `position` | integer | no | The operator's place in the order. Any whole number, including zero and negative; it is a preference, not a sequence with holes to fill |
| `is_visible` | boolean | no | Whether a customer can see it. The only state a category has beyond existing (D9) |
| `created_at` | timestamptz | no | Set once. The first tie-break key (D6) |
| `updated_at` | timestamptz | no | Touched on every write |

### Constraints, and why each is at the storage layer

| Constraint | Kind | Requirement | Why not only in the application |
|---|---|---|---|
| `id` primary key | primary key | FR-015 | Nothing else can guarantee an identifier is unique and stable across writers |
| `categories_normalized_name_key` | unique index on `normalized_name` | FR-016, FR-021 | Two concurrent creates both pass an application check and both insert. Only the database can reject the second |
| `categories_normalized_slug_key` | unique index on `normalized_slug` | FR-017, FR-021 | Same race, same reason |
| `categories_normalized_name_shape_ck` | check: `normalized_name = btrim(normalized_name) AND normalized_name = lower(normalized_name)` | D2, D3 | Catches a future writer that forgot to normalise. Deliberately **shape only** — the database does not fold, because folding belongs to the Unicode tables, not to the database's collation (D3) |
| `categories_normalized_slug_shape_ck` | check: same shape rule on `normalized_slug` | D2, D3 | Same |
| `categories_slug_format_ck` | check: the slug matches the URL-safe pattern | FR-018 | An operator-supplied value that ends up in a link should be constrained where it is stored, not only where it arrives |
| `categories_name_length_ck` | check: `length(name) <= 120` | FR-019 | Bounds the stored value, not just the request |
| `categories_slug_length_ck` | check: `length(slug) <= 140` | FR-019 | Same |
| `categories_description_length_ck` | check: `length(description) <= 2000` | FR-019 | Same |
| `position` index | btree on `(position, created_at, id)` | FR-003 | The order customers see is this index's order. Leaving it unindexed would make the catalogue's one read path scan and sort |

**On the length checks and characters**: `length()` in PostgreSQL counts characters, not bytes,
which is exactly what FR-019 requires and what a Vietnamese name needs — 120 characters of
Vietnamese is up to 360 bytes, and a byte-counted bound would refuse it at a third of its
allowance.

**On the two normalised columns**: they exist only to be indexed and checked. They are never in
a response, never in an audit entry, and never named in the API contract. This is the one
deliberate denormalisation, recorded in the plan's Complexity Tracking.

### The forward obligation this table does not carry

The module document asks for a rule: a category with products cannot be removed. The reference
that makes that rule enforceable lives on the **product**, not here, so it cannot be created in
this migration.

| What | Where it belongs | Recorded as |
|---|---|---|
| `products.category_id` referencing `categories.id`, with a **restricting** delete | Module 04 Product's migration | `deferred.md` |
| The half that is verifiable now: the identifier a product will reference is immutable and never reused | This table, via the primary key and the absence of any update path to `id` | FR-015, tested |

## Transitions

A category has one lifecycle. Every transition is a method on the entity, not a settable field,
so a caller cannot reach an inconsistent state by assigning.

```
                    create
                      │
                      ▼
              ┌───────────────┐   hide   ┌──────────────┐
              │   on display  │ ───────► │  not on      │
              │               │ ◄─────── │  display     │
              └───────────────┘   show   └──────────────┘
                      │                        │
                      └────────── remove ──────┘
                                   │
                                   ▼
                              (row removed)
```

| Transition | Writes | Refuses when | Audit action |
|---|---|---|---|
| create | a new row, on display | the name or the slug already exists, or either value fails its shape or length rule | `CATEGORY_CREATED` |
| edit | name, slug, description, position | the new name or slug collides with a *different* category, or a value fails its rule | `CATEGORY_UPDATED` |
| hide | `is_visible = false` | it is already off display — reported as a no-op, not an error | `CATEGORY_HIDDEN` |
| show | `is_visible = true` | it is already on display — a no-op, not an error | `CATEGORY_SHOWN` |
| remove | the row is deleted | it does not exist | `CATEGORY_DELETED` |

**Editing a category without changing its name or slug succeeds.** The uniqueness check excludes
the row being edited, so a category is never a duplicate of itself (FR-019). This is the one
place the storage-layer unique index is not sufficient on its own — an `UPDATE` that rewrites
the same value would trip it — so the adapter updates the normalised columns to the values they
already hold rather than to a recomputed value that could differ.

## What each read path projects

| Path | Reader | Columns | Requirement |
|---|---|---|---|
| public list | anyone | `id`, `name`, `slug`, `description` | FR-004 — exactly these four and nothing else |
| public detail | anyone | `id`, `name`, `slug`, `description` | FR-004, FR-005 |
| administrator list | administrator | every column including `is_visible`, `position`, `created_at`, `updated_at` | FR-009 |
| administrator detail | administrator | every column | FR-009 |

`normalized_name` and `normalized_slug` appear in **no** projection. The adapter lists its
columns explicitly, so a column added to the table later cannot leak into a response by
accident — the trap that a `SELECT *` repository created in module 02.

## What this feature does not change

- No existing table is altered. The migration is purely additive and reversible.
- No existing column, index or constraint is touched.
- No data is backfilled: the catalogue starts empty, which the spec's edge cases cover. An empty
  catalogue answers an empty list rather than an error.
