-- +goose Up
-- Module 04 (product) gains its own visibility flag (specs/016-hide-guard,
-- research D6). A product is shown to customers only while its flag is on, beside
-- the existing category and on-sale-variant terms of the catalogue predicate. The
-- change is additive: no existing column, constraint or index is altered.
--
-- Every product ends the migration visible: the column default covers existing
-- rows, so no backfill is needed and no product disappears from the catalogue.
--
-- All timestamps are timestamptz (UTC).
ALTER TABLE products ADD COLUMN is_visible boolean NOT NULL DEFAULT true;

COMMENT ON COLUMN products.is_visible IS 'Whether the product itself is on display. Turned off only through the guarded flag edit, which refuses while a PENDING or PAYMENT_PENDING order holds one of its variants (specs/016-hide-guard).';

-- +goose Down
-- Reverse of the Up section: the flag column is dropped. The catalogue predicate
-- falls back to the category and on-sale-variant terms it already had.
ALTER TABLE products DROP COLUMN is_visible;
