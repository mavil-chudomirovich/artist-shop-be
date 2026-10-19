-- +goose Up
-- Feature 021 (address facebook URL): an order snapshots the delivery Facebook
-- link from the chosen address at checkout, so history stays truthful when the
-- address is edited later. Same shape as 00020 on addresses: add nullable,
-- backfill legacy rows with the placeholder, then install NOT NULL plus the
-- non-empty length check.
--
-- data-migration: the only data change is the placeholder backfill below. The
-- Down section drops the column, which is a destructive rollback; it is
-- acceptable because the column is new to this feature and holds no rescued
-- data (no pre-existing values are lost by dropping it).

-- FR-006: the column starts nullable so the backfill can run before the
-- NOT NULL guarantee is installed.
ALTER TABLE orders ADD COLUMN facebook_url text;

-- Clarify 2026-10-19: legacy orders (placed before the rule) carry the
-- placeholder once, displayed verbatim on old orders.
UPDATE orders SET facebook_url = 'https://facebook.com/unknown' WHERE facebook_url IS NULL;

ALTER TABLE orders ALTER COLUMN facebook_url SET NOT NULL;

-- FR-006: storage-level counterpart of the snapshot rule (1-500 characters
-- after trimming); an empty or overlong link is refused where it is stored.
ALTER TABLE orders
    ADD CONSTRAINT orders_facebook_url_nonempty_ck CHECK (
        char_length(facebook_url) > 0 AND char_length(facebook_url) <= 500
    );

COMMENT ON COLUMN orders.facebook_url IS 'Delivery Facebook link snapshotted from the chosen address at checkout; immutable once the order is placed (FR-006). Legacy orders backfilled once with the placeholder https://facebook.com/unknown.';
COMMENT ON CONSTRAINT orders_facebook_url_nonempty_ck ON orders IS 'facebook_url holds 1 to 500 characters (the length rule of FR-003, enforced at write time).';

-- +goose Down
-- Reverse the feature: drop the check first, then the column. Destructive by
-- design (see the data-migration note above); re-running Up backfills the
-- placeholder again on rows that predate the rule.
ALTER TABLE orders DROP CONSTRAINT orders_facebook_url_nonempty_ck;
ALTER TABLE orders DROP COLUMN facebook_url;
