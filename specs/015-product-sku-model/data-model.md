# Data Model — Product SKU Model (015)

A new `product_variants` table, changes to `products`, changes to `inventory`/`cart`/`order` tables,
and one migration with backfill that also removes feature 014's structures. All ids are UUID.

## Table `products` (changed)

| Column | Change | Notes |
|---|---|---|
| `id, name, description, category_id, position, price_amount, currency, created_at, updated_at` | kept | `price_amount`/`currency` is now the **default price** a variant falls back to |
| `code` | **added** | `text NOT NULL` — the operator-facing product code **and the public link segment**; URL-safe format `^[a-z0-9]+(-[a-z0-9]+)*$`, ≤140 characters, unique on `lower(btrim(code))` (FR-011) |
| `sell_state` | **dropped** | moved to the variant (FR-015) |
| `is_preorder`, `preorder_expected_at` | **dropped** | pre-order is out of scope (spec) |
| `is_set` | **dropped** | combo-set is out of scope (spec); `product_set_items` is dropped too |
| `variant_group_id`, `variant_label` | **dropped** | feature 014's grouping removed |

New constraints: `products_code_key` unique on `lower(btrim(code))`; `products_code_format_ck`
(`code ~ '^[a-z0-9]+(-[a-z0-9]+)*$'`) and `products_code_length_ck` (`length(code) <= 140`). The
price and category constraints stay; the product's `slug`/`normalized_slug` constraints and unique
index are **dropped** by migration `00015`. `product_images` (gallery) is unchanged.

## Table `product_variants` (new)

| Column | Type | Null | Notes |
|---|---|---|---|
| `id` | uuid | no | PK, `gen_random_uuid()` |
| `product_id` | uuid | no | FK → `products(id)` `ON DELETE CASCADE` |
| `name` | text | no | display name (FR-003) |
| `sku` | text | no | globally unique (FR-011) |
| `price_amount` | bigint | yes | null = use the product's default price (FR-017); `> 0` when present |
| `image_public_id` | text | yes | media reference, or null |
| `image_secure_url` | text | yes | `https://%` when present |
| `image_width` | integer | yes | `> 0` when present |
| `image_height` | integer | yes | `> 0` when present |
| `position` | integer | no | display order; the smallest is the default variant |
| `sell_state` | text | no | `COMING_SOON`/`ACTIVE`/`OUT_OF_STOCK`/`DISCONTINUED` |
| `created_at` | timestamptz | no | `now()` |
| `updated_at` | timestamptz | no | `now()` |

**Constraints / indexes**
- `CREATE UNIQUE INDEX product_variants_sku_key ON product_variants (lower(btrim(sku)))` (FR-011;
  compare ignoring case and surrounding whitespace, like the slug rule).
- `product_variants_price_ck CHECK (price_amount IS NULL OR price_amount > 0)`.
- `product_variants_sell_state_ck CHECK (sell_state IN ('COMING_SOON','ACTIVE','OUT_OF_STOCK','DISCONTINUED'))`.
- image checks: `secure_url` https when present; width/height `> 0` when present; all four null or all
  four present.
- `product_variants_name_length_ck CHECK (length(name) <= 120)`.
- `CREATE INDEX product_variants_product_idx ON product_variants (product_id, position, created_at, id)`
  — the ordered option list and the "default = first" rule (FR-002, FR-003).
- FK cascade: a removed product takes its variants.

**Variant image write path**: a variant's image is **uploaded through the media capability** (the same
flow a product picture uses), so the server stores the resulting `image_public_id` / `image_secure_url`
/ `image_width` / `image_height`; the client sends bytes, not a URL, and at most one image is kept
(uploading again replaces it, deleting clears it). This mirrors 006's product-picture upload and keeps
the "only a reference is stored" rule.

The "at least one variant per product" rule is a cross-row invariant; the **application** enforces it
(create requires ≥1 variant, remove of the last variant refused). It is not expressible as a single
row constraint — recorded so a reader does not expect a check (FR-023 "where enforceable").

## Inventory tables (repointed)

| Table | Change |
|---|---|
| `stock_levels` | key becomes `variant_id` (PK); FK → `product_variants(id)` `ON DELETE CASCADE`; `quantity >= 0` |
| `inventory_transactions` | `variant_id` replaces `product_id`; FK cascade; history index `(variant_id, created_at, id)`; kinds/delta/resulting checks unchanged |
| `stock_holds` | `variant_id` replaces `product_id`; active-hold unique `(order_id, variant_id) WHERE status='ACTIVE'`; FK cascade |

Every rule the module already enforces (no negative shelf, append-only ledger, one active hold per
order+unit, status/resolved invariant) is preserved, only re-keyed.

The **automatic** `ACTIVE ↔ OUT_OF_STOCK` edge (FR-015) is a write from inventory to the product
module: when a variant's availability crosses zero (or returns), the inventory use case calls the
product module's variant-availability contract (`MarkVariantOutOfStock` / `MarkVariantOnSale`, keyed by
**variant id**) **inside the same transaction** as the stock change, exactly as feature 007 signalled a
product's state before this restructure. The signals are **tolerant no-ops** for states they do not
apply to (`MarkVariantOutOfStock` acts only on `ACTIVE`; `MarkVariantOnSale` acts only on
`OUT_OF_STOCK`), so a stock change never forces an illegal transition or fails the transaction.

## Cart table (repointed)

| Column | Change |
|---|---|
| `cart_items.variant_id` | **added**, `NOT NULL`; the line identity |
| `cart_items.product_id` | **dropped** (the variant implies its product) |
| `unit_price_amount`, `currency`, `quantity` | unchanged |

Line uniqueness becomes `(cart_id, variant_id)`. The captured price stays a snapshot taken at add time
(FR-018).

## Order table (extended)

`order_items` gains snapshot columns and a new natural key:

| Column | Change | Notes |
|---|---|---|
| `variant_id` | **added**, nullable | informational, no FK (snapshot) |
| `product_code` | **added** | product code at purchase (FR-019) |
| `variant_name` | **added** | variant name at purchase (FR-019) |
| `sku` | **added** | SKU at purchase (FR-019) |
| `product_id` | **kept** | informational, no FK (unchanged role) |
| `name`, `unit_price_amount`, `currency`, `quantity`, `position` | kept | `name` is **reused as the product-name snapshot** (no second name column). The line's `slug` snapshot was **dropped** by migration `00015`; the snapshot now keeps `product_code` + `sku` (`order_items.slug` no longer exists) |

The natural key becomes `(order_id, product_code, sku)` (FR-020). The `orders` table is unchanged
(total/currency/address snapshot).

## Migration `00014_product_sku_model.sql` (sequence)

1. **Create** `product_variants` with every constraint/index above.
2. **Add** `products.code` (nullable), backfill `code = slug`; then `SET NOT NULL` and create the
   unique index on `lower(btrim(code))`.
3. **Backfill default variant**: one row per product — `name = products.name`, `sku = products.code`,
   `price_amount = products.price_amount`, `sell_state = products.sell_state`,
   `image_* = that product's primary picture (or null)`, `position = 0`.
4. **Inventory repoint**: add `variant_id` to `stock_levels`, `inventory_transactions`, `stock_holds`;
   backfill from each product's default variant; then move PK/FK/indexes to `variant_id` and drop the
   old `product_id` columns.
5. **Cart repoint**: add `cart_items.variant_id` (backfill from the default variant), make it the line
   key, drop `product_id`.
6. **Order extension**: add the columns of the "Order" section; backfill `variant_id`, `product_code`,
   `variant_name`, `sku` from each line's product's default variant (the product name is already in the
   existing `name` column); move the unique key to `(order_id, product_code, sku)`.
7. **Drop** 014 and the product flags: `product_set_items`, `product_variant_groups`,
   `product_variant_group_images`, `products.variant_group_id`, `products.variant_label`,
   `products.sell_state`, `products.is_preorder`, `products.preorder_expected_at`, `products.is_set`
   (all `IF EXISTS`).
8. **Down**: reverse every step (recreate 014's objects as empty, re-add `product_id` columns backfilled
   from the variant's product, drop `product_variants` and `products.code`).

**Backfill assumption**: 014 was never applied to a live database and every product maps to exactly one
default variant, so the repoint is one-to-one.

**`00013` fate**: `00013_product_variants.sql` (feature 014) is a committed historical migration and is
**kept**; `00014` drops its objects with `DROP ... IF EXISTS`, so a fresh database that ran `00013` and
then `00014` ends in the new model and an existing one that never ran `00013` also reaches it. `00013`'s
guard test (`migrations/product_variants_guard_test.go`) stays as the historical record of that
migration; `00014` gets its own guard test.

**`00015` collapsed the link segment**: `00015_product_code_as_link_segment.sql` drops
`products.slug` / `normalized_slug` (and their constraints and unique index) and `order_items.slug`,
and replaces the code shape check with the URL-safe format above — a product's link segment is now its
`code`, and a category keeps its own slug.

## Reads (projections)

- **Catalogue entry** (`ListCatalogue`, one entry per product): product `id, name, code`, `position`,
  `created_at`, the **lowest effective variant price** (`MIN(COALESCE(v.price_amount, p.price_amount))`
  over on-sale variants), the product's primary gallery picture else the default variant's image.
  Visible only when the category is visible **and** an on-sale variant exists.
- **Product detail**: the product's content + its gallery **plus a summary (the lowest on-sale variant
  price and the product's primary gallery picture)**; when the product has no gallery picture the
  summary falls back to the default variant's image. Then its `ACTIVE`/`OUT_OF_STOCK` variants
  (`id, name, effective price, sell_state`, ordered by `position, created_at, id`), each joined with
  its availability from the inventory read (D10).
- **Variant facts** (contract, for cart/order): `id, product_id, product_name, product_code,
  variant_name, sku, on_sale, effective price`.
- **Variant existence** (contract, for inventory writes): does a variant id exist.

## Validation rules (from the spec)

- Product code and SKU required, unique, trimmed, case-insensitive comparison (FR-011).
- A variant's price optional; a null falls back to the product's default (FR-017).
- Sell states and the transition set per FR-015 (D4).
- A product must keep ≥1 variant; a removed product cascades to its variants (FR-012).
- Money stays integer minor units + the product's currency (FR-024).
