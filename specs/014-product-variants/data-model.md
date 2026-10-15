# Data Model — Product Variants (014)

Two new tables and two new columns on `products`. Every change is additive: existing rows and
existing behaviour are untouched (a product with `variant_group_id` null is exactly today's product).
All timestamps are `timestamptz` (UTC); all ids are UUID (`gen_random_uuid()`).

## Entity: Variant group

A catalogue entry that offers one product in several interchangeable choices. It is a display
container — **no price, no stock, no sell state** — and it is never bought.

### Table `product_variant_groups`

| Column | Type | Null | Notes |
|---|---|---|---|
| `id` | uuid | no | PK, `DEFAULT gen_random_uuid()` |
| `name` | text | no | operator-written, trimmed (FR-009) |
| `slug` | text | no | public link segment; unique among groups (FR-019) |
| `description` | text | no | may be empty |
| `category_id` | uuid | no | the one category (FR-023) |
| `position` | integer | no | ordering preference in the catalogue (FR-009) |
| `option_name` | text | no | the single choice-dimension name, e.g. "Card" (FR-009) |
| `created_at` | timestamptz | no | `DEFAULT now()` |
| `updated_at` | timestamptz | no | `DEFAULT now()` |

No `normalized_slug` column. The catalogue's slug-format check already forces the stored value to be
lowercase and trimmed, so a folding key would always equal the slug — this deliberately does **not**
copy the redundant `normalized_slug` pattern the spec's `deferred.md` D10 already flags for products
and categories.

**Constraints**
- `PRIMARY KEY (id)`.
- `CREATE UNIQUE INDEX product_variant_groups_slug_key ON product_variant_groups (slug)` (FR-019).
- `product_variant_groups_slug_format_ck CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$')` — the same
  pattern the product slug uses, so the two agree (FR-019, reusing feature 006's FR-029 rule).
- `product_variant_groups_name_length_ck CHECK (length(name) <= 120)`,
  `..._slug_length_ck CHECK (length(slug) <= 140)`,
  `..._description_length_ck CHECK (length(description) <= 5000)` — character counts, the catalogue's
  existing bounds (FR-020).
- `product_variant_groups_category_fk FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE RESTRICT`
  — a category with groups cannot be removed, mirroring `products_category_fk` (FR-023).
- `CREATE INDEX product_variant_groups_ordering_idx ON product_variant_groups (position, created_at, id)`
  — the FR-009 order (FR-001).
- `CREATE INDEX product_variant_groups_category_idx ON product_variant_groups (category_id)`.

### Table `product_variant_group_images`

Mirrors `product_images`; only a media-provider reference is stored, never bytes (Constitution,
Media).

| Column | Type | Null | Notes |
|---|---|---|---|
| `id` | uuid | no | PK |
| `group_id` | uuid | no | FK → `product_variant_groups(id)` ON DELETE CASCADE |
| `public_id` | text | no | provider opaque id (to release the asset) |
| `secure_url` | text | no | displayable HTTPS link |
| `width` | integer | no | > 0 |
| `height` | integer | no | > 0 |
| `position` | integer | no | display order |
| `is_primary` | boolean | no | `DEFAULT false` |
| `created_at` | timestamptz | no | `DEFAULT now()` |

**Constraints**: `width > 0`, `height > 0`, `secure_url LIKE 'https://%'`, FK cascade on group delete,
`CREATE UNIQUE INDEX product_variant_group_images_one_primary_key ON product_variant_group_images (group_id) WHERE is_primary`,
`CREATE INDEX product_variant_group_images_ordering_idx ON product_variant_group_images (group_id, position, id)`.

## Entity: Option (a product attached to a group)

An option is **not** a new entity type: it is the existing `products` row with two columns set. All of
a product's existing columns keep their meaning (price, currency, sell state, pictures, slug,
category, `is_set`, preorder). The spec's Q2 decided a group has its own gallery **in addition to**
each option's own pictures, so `product_images` is unchanged.

### Columns added to `products`

| Column | Type | Null | Notes |
|---|---|---|---|
| `variant_group_id` | uuid | yes | FK → `product_variant_groups(id)`; null = standalone product |
| `variant_label` | text | yes | the short choice text, e.g. "Flins"; set iff grouped |

**Constraints added to `products`** (FR-021, FR-022, FR-024)
- `products_variant_group_fk FOREIGN KEY (variant_group_id) REFERENCES product_variant_groups(id) ON DELETE RESTRICT`
  — a group with options cannot be removed (FR-025, D10).
- `products_variant_label_ck CHECK ((variant_group_id IS NULL) = (variant_label IS NULL))` — a label
  belongs to a grouped product and to nothing else.
- `products_set_not_variant_ck CHECK (NOT (is_set AND variant_group_id IS NOT NULL))` — a combo set
  is never an option (FR-024, D9).
- `CREATE UNIQUE INDEX products_variant_label_key ON products (variant_group_id, variant_label) WHERE variant_group_id IS NOT NULL`
  — an option label is unique within its group (FR-021).
- `CREATE INDEX products_variant_group_idx ON products (variant_group_id)` — the option read and the
  FK both need it.

**One-group-per-product** needs no check: the product row carries a single `variant_group_id`, so it
structurally belongs to at most one group (FR-021).

## Reads

### Public list (merged) — `ListCatalogue`

One statement (CTE, D4). The **entry response** carries `type` (`"product"` | `"variant-group"`),
`id`, `name`, `slug`, `price`, `imageUrl`, `isPreorder`. The **internal projection** additionally
selects `description`, `category_id`, `position`, `created_at` — used for the ordering, the
pagination window and the total, not all of which are exposed in the response.

- **Group rows**: `product_variant_groups g` where the group's category is visible **and**
  `EXISTS(visible option of g)`. `image_url` = `g`'s primary image; `price_amount`/`currency` =
  `MIN(option price)` over visible options (Q3, FR-005).
- **Standalone rows**: `products p WHERE p.variant_group_id IS NULL` under the existing visibility
  predicate; `image_url` = `p`'s primary image; price = `p`'s own.
- `ORDER BY position, created_at, id`; `LIMIT/OFFSET`; a `COUNT(*)` over the same CTE for the total.

"Visible option" = the existing product predicate: its own category visible and
(`sell_state = 'ACTIVE'` OR (`is_preorder` AND `sell_state <> 'DISCONTINUED'`)).

### Group detail (public) — `FindVisibleGroupBySlug`

The group's row, its images (ordered), and its visible options (ordered by `position, created_at,
id`), each option projected as `id, slug, variant_label, price_amount, currency, is_preorder, image_url`.
Not found when the group's category is hidden, no option is visible, the slug is unknown, or the
group was removed — one identical answer (FR-008).

### Product detail (public) — `FindVisibleBySlug` (extended)

Unchanged, plus: when the found product has `variant_group_id`, read its group's shared content
(`id, slug, name, description, option_name`, primary image) and its sibling options, so the response
carries the selector (FR-007).

### Option read by group (admin/public) — `ListGroupOptions`

Options of a group ordered by `position, created_at, id`; an admin variant includes hidden options,
the public variant only visible ones. An option projects `id, slug, variant_label, price_amount,
currency, is_preorder, image_url`; the admin variant also projects the option's `sell_state`, its
`name` and its `position`, so the operator sees price, state and picture per option.

### Admin group reads

`FindByID` (row + images + options, including hidden), `ListAll` (paged, ordered `position,
created_at, id`).

## Lifecycle & cleanup

- **Create group** → status-free row + optional images. **Edit group** → partial edit (name, slug,
  description, category, position, option_name) with the catalogue's validation and slug-collision
  handling.
- **Attach** = product update setting `variant_group_id` + `variant_label` (one statement); **detach**
  = clearing both. No product price/picture/state/link is touched (FR-010).
- **Remove group**: refused while options remain (FK RESTRICT, FR-025). On success, its image rows
  cascade away; no product is touched (FR-012).
- **Remove product**: an option removed like any product; it simply leaves the group. Unchanged
  cascade to its own pictures (FR-026).
- **Category removal**: refused while a product (FR-023's existing rule) or now a group still
  references it — the group FK is RESTRICT like the product's.

## Validation rules (from the spec)

- Group name ≤ 120 chars, slug ≤ 140, description ≤ 5000 (character count on trimmed value);
  slug matches `^[a-z0-9]+(-[a-z0-9]+)*$` (FR-019, FR-020).
- Option label required iff `variant_group_id` set; non-empty and trimmed; unique within the group
  (FR-021).
- A referenced category must exist (group and product) (FR-023).
- A product that is a set cannot be attached; an option cannot be made a set (FR-024).
- Group link unique among groups, product link unique among products — separate namespaces (FR-019).
