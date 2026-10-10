-- +goose Up
-- Module 07 (order) confirmation & editing (feature 010). It reshapes orders for
-- the six-state machine, renames the payment deadline, adds the content version
-- and the confirmation instant, and creates order_edit_history.
--
-- data-migration: the only data change is the status backfill below. The old
-- status check is dropped, the stored PENDING_PAYMENT value is renamed
-- PAYMENT_PENDING, and only then is the widened six-state check installed — so a
-- database holding 009 orders migrates cleanly instead of failing a check on the
-- old value. Renaming expires_at to payment_expires_at preserves every existing
-- deadline; a pre-existing row keeps order_version = 1 and its new columns are
-- backfilled to their defaults/empties (research D1, D4, D10).

-- FR-001, FR-006, FR-018: widen the stored state set to the six states. The old
-- constraint is dropped FIRST and the six-state check added LAST, with the
-- backfill in between, so the rename is never refused by a check.
ALTER TABLE orders DROP CONSTRAINT orders_status_ck;
UPDATE orders SET status = 'PAYMENT_PENDING' WHERE status = 'PENDING_PAYMENT';
ALTER TABLE orders ADD CONSTRAINT orders_status_ck CHECK (
    status IN ('PENDING', 'PAYMENT_PENDING', 'PAID', 'SHIPPED', 'COMPLETED', 'CANCELLED')
);

-- FR-006, FR-009, research D4: the deadline is the PAYMENT deadline, and an order
-- awaiting the artist has none, so the column is renamed and becomes nullable.
ALTER TABLE orders RENAME COLUMN expires_at TO payment_expires_at;
ALTER TABLE orders ALTER COLUMN payment_expires_at DROP NOT NULL;

-- FR-017, research D10: the content version; a positive counter that starts at 1.
ALTER TABLE orders ADD COLUMN order_version bigint NOT NULL DEFAULT 1;
ALTER TABLE orders ADD CONSTRAINT orders_version_ck CHECK (order_version >= 1);

-- FR-006, research D4: when the artist confirmed; NULL while awaiting confirmation.
ALTER TABLE orders ADD COLUMN confirmed_at timestamptz;

COMMENT ON COLUMN orders.status IS 'One of PENDING, PAYMENT_PENDING, PAID, SHIPPED, COMPLETED or CANCELLED. Changed only through the domain transitions (FR-001, FR-010).';
COMMENT ON COLUMN orders.payment_expires_at IS 'When an awaiting-payment order cancels itself; NULL while the order awaits the artist (FR-006, FR-008, research D4).';
COMMENT ON COLUMN orders.order_version IS 'The content version: starts at 1, bumped on every accepted edit and on confirmation. An internal seam used by module 08 to reject a payment callback for older content; never exposed to clients (FR-017, research D10).';
COMMENT ON COLUMN orders.confirmed_at IS 'When the artist confirmed the order; NULL while it awaits confirmation (FR-006, research D4).';

-- FR-008, research D5: the sweeper finds awaiting-payment orders past their
-- deadline without scanning every order. Rebuilt on the renamed column.
DROP INDEX orders_expiry_idx;
CREATE INDEX orders_expiry_idx ON orders (status, payment_expires_at);

-- FR-017, research D14: one row per accepted edit, so the shop can see what
-- changed, by whom and when. Read whole, never queried by field, so the
-- before/after content is stored as JSONB rather than normalized rows.
CREATE TABLE order_edit_history (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id   uuid NOT NULL,
    version    bigint NOT NULL,
    actor_id   uuid NOT NULL,
    "before"   jsonb NOT NULL,
    "after"    jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE order_edit_history IS 'One row per accepted order edit (FR-017, research D14).';
COMMENT ON COLUMN order_edit_history.order_id IS 'The order that was edited. References orders(id) ON DELETE CASCADE: a removed order takes its edit history with it (FR-017).';
COMMENT ON COLUMN order_edit_history.version IS 'The order_version the edit produced (FR-017).';
COMMENT ON COLUMN order_edit_history.actor_id IS 'The account that edited. A loose reference with NO foreign key, as orders.user_id (research D14).';
COMMENT ON COLUMN order_edit_history."before" IS 'The order lines and address before the edit, as a JSON document (FR-017).';
COMMENT ON COLUMN order_edit_history."after" IS 'The order lines and address after the edit, as a JSON document (FR-017).';
COMMENT ON COLUMN order_edit_history.created_at IS 'Set once.';

ALTER TABLE order_edit_history
    ADD CONSTRAINT order_edit_history_order_fk FOREIGN KEY (order_id)
    REFERENCES orders(id) ON DELETE CASCADE;
ALTER TABLE order_edit_history
    ADD CONSTRAINT order_edit_history_version_ck CHECK (version >= 1);
CREATE INDEX order_edit_history_order_idx ON order_edit_history (order_id, version);

-- +goose Down
-- Reverse the feature. The six-state check is replaced by the old five-state one
-- BEFORE the stored values are collapsed back, so the reverse backfill is never
-- refused by a check (the mirror of the Up backfill).
ALTER TABLE orders DROP CONSTRAINT orders_status_ck;
ALTER TABLE orders ADD CONSTRAINT orders_status_ck CHECK (
    status IN ('PENDING_PAYMENT', 'PAID', 'SHIPPED', 'COMPLETED', 'CANCELLED')
);
UPDATE orders SET status = 'PENDING_PAYMENT' WHERE status IN ('PENDING', 'PAYMENT_PENDING');

DROP INDEX order_edit_history_order_idx;
ALTER TABLE order_edit_history DROP CONSTRAINT order_edit_history_version_ck;
ALTER TABLE order_edit_history DROP CONSTRAINT order_edit_history_order_fk;
DROP TABLE order_edit_history;

DROP INDEX orders_expiry_idx;
ALTER TABLE orders RENAME COLUMN payment_expires_at TO expires_at;
UPDATE orders SET expires_at = now() WHERE expires_at IS NULL;
ALTER TABLE orders ALTER COLUMN expires_at SET NOT NULL;
CREATE INDEX orders_expiry_idx ON orders (status, expires_at);

ALTER TABLE orders DROP COLUMN confirmed_at;
ALTER TABLE orders DROP CONSTRAINT orders_version_ck;
ALTER TABLE orders DROP COLUMN order_version;
