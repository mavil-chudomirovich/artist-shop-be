-- +goose Up
-- Feature order-line-image (2026-10-24): an order line snapshots the display
-- picture of the variant it sold, falling back to the product's primary gallery
-- picture. The line is a snapshot: nothing a past order shows may depend on the
-- product or variant still existing (FR-002, research D3).
--
-- data-migration: existing lines are backfilled once from the CURRENT variant or
-- product image, because no historical image was ever recorded; a line whose
-- variant and product were both removed keeps the empty fallback. The Down
-- section drops the column, which is a destructive rollback; it is acceptable
-- because the column is new to this feature and holds no rescued data.

ALTER TABLE order_items ADD COLUMN image_url text NOT NULL DEFAULT '';

UPDATE order_items oi
SET image_url = COALESCE(
    (SELECT NULLIF(v.image_secure_url, '')
     FROM product_variants v
     WHERE v.id = oi.variant_id),
    (SELECT i.secure_url
     FROM product_images i
     WHERE i.product_id = oi.product_id
     ORDER BY (CASE WHEN i.is_primary THEN 0 ELSE 1 END), i.position, i.id
     LIMIT 1),
    '')
WHERE oi.image_url = '';

-- FR-002, FR-016: storage-level counterpart of the snapshot rule. An empty value
-- means "no image"; anything else must be a displayable HTTPS link, matching the
-- product image constraints.
ALTER TABLE order_items
    ADD CONSTRAINT order_items_image_url_ck CHECK (image_url = '' OR image_url LIKE 'https://%');

COMMENT ON COLUMN order_items.image_url IS 'The variant display picture at checkout/edit, or the product primary gallery picture as fallback; an empty string means no image. Snapshotted, never re-read from the product (FR-002).';
COMMENT ON CONSTRAINT order_items_image_url_ck ON order_items IS 'image_url is empty or a displayable HTTPS link, matching the product image rule (FR-016).';

-- +goose Down
-- Drop the check first, then the column it references. No reverse backfill is
-- needed because the snapshot is derived data.
ALTER TABLE order_items DROP CONSTRAINT order_items_image_url_ck;
ALTER TABLE order_items DROP COLUMN image_url;
