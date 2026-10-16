# Research — Product SKU Model (015)

Phase 0 decisions. The spec's `Clarifications` (stock per variant; cart/order per variant with product
code + SKU snapshots; this feature replaces 014; sell state per variant; product gallery plus one image
per variant; at least one variant with backfill; pre-order and combo-set dropped; product hidden when
no variant is on sale; catalogue card shows the lowest variant price; per-variant stock status shown;
product code and SKU required and unique; add-to-cart requires a variant) are inputs, not open
questions.

## D1 — Product parent, variant child, replace 014

**Decision**: `products` becomes the parent (shared content) and a new **`product_variants`** table
holds the children. Feature 014's `product_variant_groups`, `product_variant_group_images`,
`products.variant_group_id` and `products.variant_label` are **removed**.

**Rationale**: the spec (Clarifications) rejects "each choice is a product" in favour of one parent with
a variants array; keeping 014's table would be a second grouping mechanism with no owner.

**Alternatives rejected**: keep 014 and add variants (two overlapping mechanisms); a self-referencing
parent product (a parent row would need a placeholder price/state and could be sold — 014's D1 already
rejected this).

## D2 — What `products` keeps, gains and loses

**Decision**:
- **Keeps**: `id`, `name`, `slug`, `normalized_slug`, `description`, `category_id`, `position`,
  `price_amount`/`currency` (its **default price**), `created_at`, `updated_at`; `product_images`
  (the gallery).
- **Gains**: `code text NOT NULL` with a unique index — the operator-facing product code (FR-011).
- **Loses**: `sell_state`, `is_preorder`, `preorder_expected_at`, `is_set` (state moves to the
  variant; pre-order and combo-set are dropped, spec Clarifications). `product_set_items` is dropped
  with `is_set`.

**Rationale**: a product is no longer sold on its own, so a product-level sell state and the two flags
do not belong; the default price stays as the fallback a variant may override (FR-017).

**Alternatives rejected**: renaming `price_amount` to `default_price_amount` (churn across product
reads for no behavioural gain — the column is documented as the default price); keeping `sell_state` on
the product as a rollup (a second source of truth against the per-variant state).

## D3 — The variant row

**Decision**: `product_variants` columns: `id`, `product_id` (FK → products, ON DELETE CASCADE),
`name` (display), `sku` (globally unique), `price_amount bigint NULL` (a null means "use the product's
default price"; the currency is always the product's), `image_public_id` / `image_secure_url` /
`image_width` / `image_height` (all nullable — one image, or none), `position`, `sell_state`,
`created_at`, `updated_at`.

**Rationale**: a single image per variant (spec: "một ảnh riêng") is uploaded through the existing
media capability (only a reference is stored, exactly as `product_images` does) with a variant image
endpoint; the price is optional because it falls back to the product's default (FR-017).

**Alternatives rejected**: a `product_variant_images` table (a table for one image each is heavier than
four nullable columns); storing the currency on the variant (one currency per product, spec
Assumptions); a `variant_slug` (a variant is addressed inside its product, not by its own link).

## D4 — Per-variant sell state and the stock-driven transition

**Decision**: the variant carries the sell state (`COMING_SOON`, `ACTIVE`, `OUT_OF_STOCK`,
`DISCONTINUED`) with the same transition set feature 006/014 used: `COMING_SOON → ACTIVE`;
`ACTIVE ↔ OUT_OF_STOCK` (operator, or **automatically** as availability crosses zero and returns);
any non-`DISCONTINUED` → `DISCONTINUED`; `DISCONTINUED` terminal. The inventory module signals the two
automatic edges through the product contract, now addressed by **variant id** (D7), in the same
transaction as the stock change.

**Rationale**: spec FR-015 asks for an explicit table, and the existing edges already model the shop
(the inventory signal that moved a product's state now moves a variant's). The two automatic signals are
**tolerant no-ops**: `MarkVariantOutOfStock` acts only when the variant is `ACTIVE` (otherwise it leaves
`COMING_SOON`/`DISCONTINUED` untouched), and `MarkVariantOnSale` acts only when the variant is
`OUT_OF_STOCK`, so a restock of a not-yet-launched or retired variant cannot force an illegal transition
or fail the stock transaction.

**Alternatives rejected**: a product-level state (contradicts the spec — one variant may be out of
stock while a sibling sells); fully manual state (the spec's edge case expects stock reaching zero to
mark a variant out of stock).

## D5 — Inventory is repointed to the variant

**Decision**: `stock_levels` is keyed by `variant_id` (primary key); `inventory_transactions` and
`stock_holds` carry `variant_id`; the active-hold uniqueness becomes `(order_id, variant_id)`. The
`inventory_transactions` FK and the history index move to `variant_id`.

**Rationale**: spec FR-014/FR-022 make the variant the stock unit; the module's whole model
(one row per unit, conditional updates, one transaction per change) is otherwise unchanged.

**Alternatives rejected**: keeping stock keyed by product and a separate per-variant allowance (two
stock models); a `(product_id, variant_id)` pair (variant id already implies the product).

## D6 — Cross-module contracts are re-keyed to the variant

**Decision**:
- `internal/contracts/product.go`: `ProductCatalog` is **replaced** by a variant-facts read the cart
  and order consume — `Variants(variantIDs) []VariantSummary` with
  `{VariantID, ProductID, ProductName, ProductCode, VariantName, SKU, OnSale, Price}`. `ProductLookup`
  becomes a variant-existence lookup for inventory. Feature 007's availability-**signal** contract
  becomes variant-keyed — `MarkVariantOutOfStock` / `MarkVariantOnSale(variantID)` — which inventory
  calls (in the stock transaction) when a variant's availability crosses zero or returns (FR-015).
- `internal/contracts/inventory.go`: `AvailableQuantity(variantIDs)` and the reservation methods
  (`Reserve`/`Release`/`ApplySale`) take a **variant id** and report it back.
- `internal/contracts/cart.go`: `CartLine` carries the chosen `VariantID` plus the snapshots the order
  needs (`ProductID`, `ProductName`, `ProductCode`, `VariantName`, `SKU`, `Quantity`, `UnitPrice`).

**Rationale**: the consumer's unit of sale is the variant, so the DTO that travels across the boundary
must name the variant and the facts about it; passing a product id would force the consumer to guess a
variant and lose the SKU (Constitution I, `docs/system-design/contract-purity.md`).

**Alternatives rejected**: widening the existing product DTOs with an optional variant (a product read
that no longer matches the model); a per-line query per variant (breaks the module's bulk-read rule).

## D7 — Cart holds a variant

**Decision**: `cart_items` stores `variant_id` (unique per cart) plus the captured `unit_price_amount`
and `currency` and `quantity`; adding to the cart requires a `variantId`. The cart read shows the
product name and the variant name (and slug for a link), the captured price and the availability of
the variant, calling the variant-facts contract and the availability contract in bulk as it does today.

**Rationale**: spec FR-006/FR-018; a cart can hold two variants of one product as two lines.

**Alternatives rejected**: keeping `cart_items.product_id` and adding a nullable `variant_id` (a line
without a variant would be un-sellable under the new model); a composite key `(product_id, variant_id)`
(needless — variant id already identifies the product).

## D8 — Order line snapshots the product code and SKU

**Decision**: `order_items` carries `variant_id` (a loose, informational reference, as `product_id` is
today) and snapshots the product name (reusing the existing `name` column), `product_code`,
`variant_name`, `sku`, `unit_price_amount`, `currency`, `quantity`, `position`. The natural key is
`(order_id, product_code, sku)` — each (product code, SKU) pair is one order line (spec FR-020).

**Rationale**: an order is a record of a sale and must survive the product and its variants (the
existing decision for `order_items`); the spec wants the code and SKU kept.

**Alternatives rejected**: keeping `product_id` as the line key (cannot distinguish two variants of one
product); snapshotting only the product id and reading the variant later (breaks the snapshot rule).

## D9 — Public catalogue and product detail

**Decision**:
- The catalogue (`GET /products`) returns **one entry per product**: name, slug, the **lowest effective
  price** among the product's **on-sale** variants, and a card image (the default variant's image, else the
  product's primary gallery picture). A product appears only when its category is visible and at least
  one variant is **on sale** (spec Clarifications).
- The product detail returns the product's shared content (name, description, gallery) plus its
  variants that are on sale or out of stock, each with name, effective price and stock status; a
  variant that is not yet on sale or retired is not returned. The default (first) variant is the lowest
  `position`, ties by `created_at` then `id`.
- The entry shape no longer carries feature 014's `type` discriminator (there is one kind again).

**Rationale**: spec FR-001–FR-005; the card price and image follow the spec's choices.

**Alternatives rejected**: one entry per variant (spec FR-001 forbids it); showing a fully sold-out
product (spec Clarifications say the product is hidden when no variant is on sale).

## D10 — The per-variant availability read on the product page

**Decision**: the product module consumes the inventory module's availability contract (re-keyed to
variant ids in D6) to show each variant's stock status on the product detail. The bulk read returns one
entry per requested variant, so the page costs one inventory call however many variants it lists.

**Rationale**: spec FR-003/FR-005 require the status; inventory owns stock, so the product surface must
ask across the boundary rather than read the table (Constitution I).

**Alternatives rejected**: reading `stock_levels` from the product module (forbidden); recomputing
availability from a product-side copy (a second source of truth).

**Note**: this adds a product → inventory read edge (the reverse direction of the existing inventory →
product signals). Both directions are already contract-mediated; the composition root supplies both
adapters, so no module imports another. Recorded in the plan's Complexity Tracking.

## D11 — Migration and backfill

**Decision**: one migration `00014_product_sku_model.sql`, in order:
1. `CREATE TABLE product_variants` (D3) with its constraints/indexes.
2. `ALTER TABLE products ADD COLUMN code text` (nullable), backfill `code = slug`, then set `NOT NULL`
   and add the unique index (slug is already unique, so the codes are unique).
3. Insert one **default variant** per existing product: `product_id`, `name = product.name`,
   `sku = product.code`, `price_amount = product.price_amount`, `sell_state = product.sell_state`,
   `image_* = the product's primary picture`, `position = 0`.
4. Repoint inventory: add `variant_id` to `stock_levels`/`inventory_transactions`/`stock_holds`,
   backfill it from the product's default variant, move the keys/FKs/indexes to `variant_id`, and drop
   the old `product_id` columns.
5. Repoint `cart_items`: add `variant_id` (backfilled from the default variant) and make it the line
   key.
6. Extend `order_items`: add `variant_id` (nullable), `product_code`, `variant_name`, `sku` (the product
   name reuses the existing `name`); backfill from the default variant; move the natural key to
   `(order_id, product_code, sku)`.
7. **Drop** feature 014's objects (`product_set_items`, `product_variant_groups`,
   `product_variant_group_images`, `products.variant_group_id`/`variant_label`) and the product flags
   (`sell_state`, `is_preorder`, `preorder_expected_at`, `is_set`).
8. Full `-- +goose Down` reversing the above.

**Rationale**: the spec requires no product be left without a variant and the old model be removed; the
default-variant backfill makes the "≥1 variant" rule true for existing data and keeps a one-to-one
mapping for the inventory/cart/order repoint.

**Alternatives rejected**: a nullable variant on cart/order (leaves legacy rows un-sellable); deleting
inventory/cart/order rows to avoid the repoint (destroys data); two migrations (the repoint is not
independently useful).

**Assumption**: 014 was never applied to a live database (it is committed but not pushed), so 00014
uses `DROP ... IF EXISTS` for 014's objects and the default-variant backfill assumes every existing
product maps cleanly to one variant.

## D12 — Removing feature 014

**Decision**: delete 014's code (`VariantGroup` model/port/adapter/DTOs/handlers), its routes
(`/product-groups/{slug}`, `/admin/product-groups*`), its tests, and its Swagger paths; update
`docs/api-reference.md`, `docs/modules/04-product.md` and add ADR `021-product-sku-model` that
supersedes `020-product-variants`; update `specs/014-product-variants/` with a superseding note. Any
014 deferred item that still applies (multi-dimension options) carries forward; per-variant
availability, which 014 deferred, is now delivered here.

**Rationale**: the spec replaces 014; two grouping models must not coexist (test and client confusion).
The **product-level sell-state endpoint** (`POST /admin/products/{id}/state`) is removed too, because the
product has no sell state any more; a variant's state is changed through a dedicated
`POST /admin/products/{id}/variants/{variantId}/state` (transitions keep their own endpoint, as the
module's D11 rule holds), and a new variant starts `COMING_SOON`.

**Migration note**: `00013_product_variants.sql` (feature 014) is **kept** as a committed historical
migration; `00014` drops its objects with `DROP ... IF EXISTS`, so a database that ran `00013` and one
that did not both reach the new model. `00013`'s guard test stays as the record of that migration.

**Alternatives rejected**: leaving 014 in place (contradicts the spec and the model).

## D13 — What stays unchanged

**Decision**: modules 01 (auth), 02 (user), 03 (category) and 08 (payment) are unchanged — payment keys
on the order, never the product. The shared foundation (access roles, audit writer, media capability,
envelope, pagination, error catalogue) is reused as-is. `product_images` (the gallery) is retained.

**Rationale**: the restructure's blast radius is the sellable-unit identity, which stops at the order
line; payment and identity modules never carried a product id.

**Alternatives rejected**: none material.

## D14 — Creating a product requires a variant

**Decision**: the product create operation accepts the product fields **and at least one variant** in
one request (no request may create a product with zero variants), and removing a product's last variant
is refused.

**Rationale**: spec FR-012 requires ≥1 variant; allowing zero at any point would break the
"default = first variant" and "lowest price" rules.

**Alternatives rejected**: create a product first and add variants later (a window where the catalogue
rules have no meaning); auto-create a placeholder variant (silent data).
