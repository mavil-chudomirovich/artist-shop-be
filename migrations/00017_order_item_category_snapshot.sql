-- +goose Up
-- Module 16 (admin) reports historical revenue by category, so each order line
-- snapshots the category (identity + name) it was sold under. The change is
-- additive: two nullable columns on order_items with no foreign key, because a
-- line is a record of a sale and must outlive the category it names (FR-022,
-- data-model.md).

ALTER TABLE order_items
    ADD COLUMN category_id uuid,
    ADD COLUMN category_name text;

COMMENT ON COLUMN order_items.category_id IS 'The category the product belonged to when the line was bought, snapshotted at checkout/edit. A loose reference with NO foreign key: a category may be deleted after the sale (FR-022).';
COMMENT ON COLUMN order_items.category_name IS 'The category name at the same instant, so a later rename cannot change historical revenue labels (FR-022).';

-- FR-022: a half snapshot is not a state the dashboard can interpret, so the pair
-- is both-or-neither at the storage layer, not only in application code.
ALTER TABLE order_items
    ADD CONSTRAINT order_items_category_ck
    CHECK ((category_id IS NULL) = (category_name IS NULL));

-- data-migration: existing lines predate the snapshot, so they are backfilled
-- from the product's CURRENT category where the variant and product still
-- resolve. A line whose variant or product was deleted stays null and is reported
-- as the "Khác" bucket rather than dropped; the backfill can only use the current
-- category because no historical one exists for those lines (data-model.md). The
-- snapshot is derived data, so Down needs no reverse backfill.
UPDATE order_items oi
SET category_id = p.category_id,
    category_name = c.name
FROM product_variants v
JOIN products p ON p.id = v.product_id
JOIN categories c ON c.id = p.category_id
WHERE oi.variant_id = v.id
  AND oi.category_id IS NULL;

-- +goose Down
-- Drop the check first, then the columns it references. The snapshot is derived
-- data, so no reverse backfill is needed.
ALTER TABLE order_items DROP CONSTRAINT order_items_category_ck;
ALTER TABLE order_items DROP COLUMN category_name;
ALTER TABLE order_items DROP COLUMN category_id;
