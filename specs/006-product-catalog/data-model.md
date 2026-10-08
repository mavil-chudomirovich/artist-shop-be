# Phase 1 Data Model: Product Catalogue

Three new tables, and one foreign key added to a table module 03 owns. The decisions behind their
shape are in `research.md`; this file records what is stored, what is constrained, and what the
read paths project.

## Entity: Product

What a product is, in the operator's terms: one thing the shop sells, with the name and
description they wrote, a price and its currency, the category it belongs to, a place in the
order, and where it is in its selling life. A product may also be a combo set, or be announced as
a pre-order, or both.

### Stored columns

| Column | Type | Null | Meaning |
|---|---|---|---|
| `id` | uuid | no | Primary key. Generated once and never changed; the administrator surface addresses a product by it |
| `name` | text | no | The name the operator typed, trimmed. This is what a customer reads. **Not unique** (D5) |
| `slug` | text | no | The link segment the operator wrote, lowercase and URL-safe by validation. This is what a public link is built from |
| `normalized_slug` | text | no | The folding key for uniqueness (D4). Never returned to anyone |
| `description` | text | no | What the operator wrote; an empty string is a valid description |
| `price_amount` | bigint | no | The price in the currency's **minor unit**. An integer, never a float (D3, FR-030) |
| `currency` | char(3) | no | The currency the amount is denominated in, three uppercase letters (D3) |
| `category_id` | uuid | no | The one category the product belongs to. References `categories(id)` with a **restricting** delete (D15, FR-034) |
| `position` | integer | no | The operator's place in the order. Any whole number, including zero and negative |
| `sell_state` | text | no | One of four states (D8). Never written directly — only through a transition |
| `is_set` | boolean | no | Whether this product is a combo set (D10) |
| `is_preorder` | boolean | no | Whether the product is announced as a pre-order (D9) |
| `preorder_expected_at` | date | yes | The expected availability date of a pre-order. Only meaningful when `is_preorder` is true |
| `created_at` | timestamptz | no | Set once. The first tie-break key (D17) |
| `updated_at` | timestamptz | no | Touched on every write |

### Constraints, and why each is at the storage layer

| Constraint | Kind | Requirement | Why not only in the application |
|---|---|---|---|
| `products_pkey` | primary key on `id` | FR-015 | Nothing else can guarantee an identifier is unique and stable across writers |
| `products_normalized_slug_key` | unique index on `normalized_slug` | FR-027, FR-033 | Two concurrent creates both pass an application check and both insert. Only the database can reject the second |
| `products_normalized_slug_shape_ck` | check: `normalized_slug = btrim(normalized_slug) AND normalized_slug = lower(normalized_slug)` | D4 | Catches a future writer that forgot to normalise. **Shape only** — the database does not fold, because folding belongs to the Unicode tables, not to the database's collation |
| `products_slug_format_ck` | check: `slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'` | FR-028 | A value that ends up in a link is constrained where it is stored, not only where it arrives |
| `products_slug_length_ck` | check: `length(slug) <= 140` | FR-029 | Bounds the stored value, not just the request |
| `products_name_length_ck` | check: `length(name) <= 120` | FR-029 | Same |
| `products_description_length_ck` | check: `length(description) <= 5000` | FR-029 | Same |
| `products_price_amount_ck` | check: `price_amount > 0` | FR-031, FR-033 | Zero and negative prices are refused where they are stored, so no code path can create one |
| `products_currency_ck` | check: `currency ~ '^[A-Z]{3}$'` | FR-030 | Keeps the currency an unambiguous three-letter code |
| `products_sell_state_ck` | check: `sell_state IN ('COMING_SOON','ACTIVE','OUT_OF_STOCK','DISCONTINUED')` | FR-021 | An unlisted state cannot be stored, so the domain's transition table cannot be bypassed by writing a value |
| `products_preorder_ck` | check: `NOT is_preorder OR sell_state <> 'ACTIVE'` | FR-039 | A product that is on sale is not "coming soon"; the two facts cannot disagree in storage |
| `products_preorder_date_ck` | check: `preorder_expected_at IS NULL OR is_preorder` | FR-039 | An expected date belongs to a pre-order and to nothing else |
| `products_category_fk` | foreign key → `categories(id)` **ON DELETE RESTRICT** | FR-034, FR-035 | This is the constraint that makes module 03's completion criterion true. It refuses the delete at the storage layer, so no code path can leave a product pointing at a category that is gone |
| `products_category_idx` | btree on `(category_id)` | FR-006 | The public filter and the foreign key both need it |
| `products_ordering_idx` | btree on `(position, created_at, id)` | FR-009 | Gives the catalogue its order without a sort. Like module 03's, it does not cover the public read's visibility filter — at the catalogue's size the planner scans regardless, and a covering index is a scale decision, not a guess |

**On the length checks and characters**: `length()` in PostgreSQL counts characters, not bytes,
which is what FR-029 requires and what Vietnamese text needs — 120 characters of Vietnamese is up
to 360 bytes, and a byte-counted bound would refuse it at a third of its allowance.

**On the two facts that are *not* storage constraints**: the ten-picture ceiling and "only a
product with `is_set` true may be a set" are both enforced in the use cases. Neither can be
expressed as a constraint on these tables — a count is not a row property, and the second is a
property of the *referencing* row in a different table — and FR-033 lists the three rules that must
reach the storage layer, neither of which is among them. The picture ceiling is additionally made
race-free by the transaction in D6 rather than by the schema.

## Entity: Picture

An image attached to a product, held outside the service by the media provider. The service keeps
the reference, never the bytes.

### Stored columns

| Column | Type | Null | Meaning |
|---|---|---|---|
| `id` | uuid | no | Primary key; the administrator surface addresses a picture by it |
| `product_id` | uuid | no | The product it belongs to. References `products(id)` with a **cascading** delete |
| `public_id` | text | no | The provider's opaque identifier, used to release the asset |
| `secure_url` | text | no | The displayable link returned to clients |
| `width` | integer | no | The stored pixel width, as the provider reported it after the resize (D16) |
| `height` | integer | no | The stored pixel height |
| `position` | integer | no | The order the pictures are shown in. Insertion order; the operator does not edit it (D11) |
| `is_primary` | boolean | no | Whether this is the product's main picture |
| `created_at` | timestamptz | no | Set once |

### Constraints

| Constraint | Kind | Requirement | Why not only in the application |
|---|---|---|---|
| `product_images_pkey` | primary key on `id` | FR-016 | A picture is addressable for removal and promotion |
| `product_images_product_fk` | foreign key → `products(id)` **ON DELETE CASCADE** | D13 | A removed product's pictures are removed with it, in the same statement |
| `product_images_one_primary_key` | **partial** unique index on `(product_id)` where `is_primary` | FR-016 | "At most one main picture" is a uniqueness property. Without the index two concurrent promotions leave a product whose main picture is whichever row the client reads first |
| `product_images_width_ck` | check: `width > 0` | FR-016 | A reference with no dimensions is one no client could lay out |
| `product_images_height_ck` | check: `height > 0` | FR-016 | Same |
| `product_images_secure_url_ck` | check: `secure_url LIKE 'https://%'` | FR-016 | A cleartext link handed to every browsing client is a tracking beacon and an in-flight tampering opportunity; the value is refused rather than stored |
| `product_images_ordering_idx` | btree on `(product_id, position, id)` | FR-016 | The pictures are read in order for every product read |

**On "at least one main picture"**: not a constraint, because a product with no pictures has no
main picture and is a valid product. It is a transition — the first picture added becomes the main
one, and removing the main one promotes the next by position (D7).

## Entity: Set membership

Which products make up a combo set. A set is a product in its own right with a price the operator
sets, so this records what is inside one and does not decide that price (D10).

| Column | Type | Null | Meaning |
|---|---|---|---|
| `set_product_id` | uuid | no | The set. References `products(id)` with a **cascading** delete |
| `member_product_id` | uuid | no | A product inside it. References `products(id)` with a **cascading** delete |
| `position` | integer | no | The order the members are listed in |

| Constraint | Kind | Requirement | Why |
|---|---|---|---|
| `product_set_items_pkey` | primary key on `(set_product_id, member_product_id)` | FR-038 | A product appears in a set once. The pair is the natural key, so a duplicate is refused rather than counted |
| `product_set_items_not_self_ck` | check: `set_product_id <> member_product_id` | FR-038 | A set cannot contain itself, which would be a cycle with no meaning |
| `product_set_items_set_fk` | foreign key → `products(id)` **ON DELETE CASCADE** | D13 | Removing a set removes its membership rows, and removes nothing else — the members survive, which is US5 scenario 3 |
| `product_set_items_member_fk` | foreign key → `products(id)` **ON DELETE CASCADE** | D13 | Removing a member removes it from any set it was in, rather than leaving a dangling reference |

## Transitions

The sell state has four values and five edges. Every transition is a method on the entity, never a
settable field, so a caller cannot reach a state by assigning one (D8).

```
        COMING_SOON ──launch──► ACTIVE ◄──restock── OUT_OF_STOCK
             │                    │         ──sell out──┘
             │                    │              │
             └──────────retire────┴──────────────┘
                                  │
                                  ▼
                            DISCONTINUED
                            (no way out)
```

| Transition | From | To | Refuses when | Clears the pre-order label | Audit action |
|---|---|---|---|---|---|
| launch | `COMING_SOON` | `ACTIVE` | the product is not `COMING_SOON` | yes (FR-039) | `PRODUCT_STATE_CHANGED` |
| sell out | `ACTIVE` | `OUT_OF_STOCK` | the product is not `ACTIVE` | — | `PRODUCT_STATE_CHANGED` |
| restock | `OUT_OF_STOCK` | `ACTIVE` | the product is not `OUT_OF_STOCK` | — | `PRODUCT_STATE_CHANGED` |
| retire | any of the three | `DISCONTINUED` | the product is already `DISCONTINUED` | — | `PRODUCT_STATE_CHANGED` |
| *(none)* | `DISCONTINUED` | any | always | — | — |

**A refused transition names the current state** (FR-023) and leaves the product exactly as it was.
The transition table is the single source of truth: the presentation layer does not decide whether
a move is legal, it reports the domain's refusal.

**The other lifecycle events**, which are not sell-state transitions:

| Event | Writes | Refuses when | Audit action |
|---|---|---|---|
| create | a new row, in `COMING_SOON` | the slug already exists, or a value fails its shape or length rule, or the category does not exist, or the price is not positive | `PRODUCT_CREATED` |
| edit | name, slug, description, price, currency, category, position, set membership, pre-order label | the new slug collides with a *different* product, or a value fails its rule, or the category does not exist | `PRODUCT_UPDATED` |
| add picture | a picture row, and possibly the primary flag | the product already has ten pictures, or the upload is not a supported image, or it is too large | `PRODUCT_IMAGE_ADDED` |
| remove picture | the picture row is deleted, and possibly a promotion | the picture does not belong to the product | `PRODUCT_IMAGE_REMOVED` |
| promote picture | the primary flag moves | the picture does not belong to the product, or is already primary (a no-op, not an error) | `PRODUCT_IMAGE_PRIMARY_SET` |
| remove product | the row is deleted, cascading to its pictures and its membership rows | it does not exist | `PRODUCT_DELETED` |

**Editing a product without changing its slug succeeds.** The uniqueness check excludes the row
being edited, so a product is never a duplicate of itself. As in module 03, the adapter
**recomputes** `normalized_slug` from the slug on every write using the domain's pure folding
function — the same slug always produces the same key, so a same-slug edit writes back the value
that is already there and cannot trip the unique index.

## What each read path projects

| Path | Reader | Columns | Requirement |
|---|---|---|---|
| public list | anyone | `id`, `name`, `slug`, `price_amount`, `currency`, `is_preorder`, the primary picture's `secure_url` | FR-004 |
| public detail | anyone | the same, plus `description` and every picture in order | FR-005 |
| administrator list | administrator | every product column, plus the picture count and the primary picture's `secure_url` | FR-010 |
| administrator detail | administrator | every column, every picture, and for a set the member products' `id`, `name`, `slug` | FR-010, D18 |

Both public reads carry the **whole visibility predicate** in their own query, so nothing is
filtered in Go and pagination stays correct (research D1):

```sql
(sell_state = 'ACTIVE' OR (is_preorder AND sell_state <> 'DISCONTINUED'))
  AND category_id = ANY($visible)
```

The first half is FR-002's own condition — on sale, or announced as a pre-order. The
`sell_state <> 'DISCONTINUED'` guard is **not** redundant: retiring is reachable from every state
and does **not** clear the pre-order label (only launching does, FR-040), so a retired product can
still carry it, and `is_preorder` alone would then serve a product FR-002 says must never appear.
This was found while implementing US3 and corrected here and in the adapter.

The predicate lives in the query rather than in Go for the same reason the category half does:
dropping rows after the page is fetched returns a short page and a `total` that counts rows the
caller never received (SC-001, `quickstart.md` 16e). The second half is the category's display
state, reached through the contract because module 04 may not read module 03's table.

**On the rule having two expressions**: the state half is stated once as
`model.Product.VisibleToCustomers` (and `Buyable`) and once as the SQL string above, because a
query cannot call Go and the filter must run in SQL. They are pinned together by an integration
test rather than by convention: a change to one that the other does not follow fails that test.

`normalized_slug` appears in **no client-facing projection** — no response, no audit entry, no log
line. The adapter recomputes it on write, so no read path carries it.

## What this feature changes outside its own tables

| Change | Table | Why |
|---|---|---|
| A new foreign key `products_category_fk` | `categories` (module 03) | The constraint is module 04's to create because the reference lives on the product; it is created by this feature's migration and constrains module 03's deletes |
| Module 03's delete path translates the refusal into `CATEGORY_IN_USE` | module 03's code, not its schema | FR-036 requires the operator to be told what happened rather than shown a server failure |

No column, index or constraint of an existing table is otherwise touched, and no data is
backfilled: the catalogue starts empty, which the spec's edge cases cover.
