-- +goose Up
-- Module 05 (inventory) introduces the three facts the spec needs about one
-- product's stock (data-model.md, research D1): stock_levels is what is on the
-- shelf and the row the no-negative rule is enforced on, inventory_transactions is
-- the append-only ledger of every physical change, and stock_holds is what a
-- payment attempt has set aside. Availability is derived from the first and the
-- third and is never stored. The change is additive: no existing table is altered.

-- The physical count, one row per product. It exists as its own row so a decrease
-- can be a conditional UPDATE and no read-then-write race can drive it negative
-- (research D2).
CREATE TABLE stock_levels (
    product_id uuid PRIMARY KEY,
    quantity   bigint NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

COMMENT ON COLUMN stock_levels.product_id IS 'The one product this level belongs to. References products(id) with ON DELETE CASCADE: a removed product takes its stock with it (research D11).';
COMMENT ON COLUMN stock_levels.quantity IS 'Physical units on the shelf. A whole number, never negative (FR-009). A product with no row is understood as zero (research D12).';
COMMENT ON COLUMN stock_levels.updated_at IS 'Touched on every write, so the level is attributable to a moment.';

ALTER TABLE stock_levels
    ADD CONSTRAINT stock_levels_quantity_ck CHECK (quantity >= 0);

-- The storage guard for the one path that writes a row: a level cannot be created
-- for a product that does not exist. Existence on the read and decrease paths is
-- answered by the ProductLookup contract, because this foreign key only fires on
-- an insert (research D4, D12).
ALTER TABLE stock_levels
    ADD CONSTRAINT stock_levels_product_fk FOREIGN KEY (product_id)
    REFERENCES products(id) ON DELETE CASCADE;

-- The append-only ledger: one immutable row per physical change. A hold is not a
-- physical change and never appears here (FR-004, research D1, D9).
CREATE TABLE inventory_transactions (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id         uuid NOT NULL,
    kind               text NOT NULL,
    delta              bigint NOT NULL,
    resulting_quantity bigint NOT NULL,
    source_reference   text,
    actor_id           uuid,
    note               text,
    created_at         timestamptz NOT NULL DEFAULT now()
);

COMMENT ON COLUMN inventory_transactions.kind IS 'One of RESTOCK, DAMAGE, ADJUSTMENT or SALE (FR-004, research D9). SALE is a paid hold becoming a decrease and is distinct from DAMAGE.';
COMMENT ON COLUMN inventory_transactions.delta IS 'The signed change: positive for a restock and a positive adjustment, negative for damage, a sale and a negative adjustment. Never zero, because a change that changes nothing is not a change (research D9).';
COMMENT ON COLUMN inventory_transactions.resulting_quantity IS 'The physical quantity after this change, so the history is self-contained and never describes a negative shelf (FR-009).';
COMMENT ON COLUMN inventory_transactions.source_reference IS 'The identity of an outside event that caused the change (a payment), so the event applies at most once (FR-020, FR-022). Null for a manual change.';
COMMENT ON COLUMN inventory_transactions.actor_id IS 'The administrator who made a manual change; null for a system-caused sale, because no human caused it (research D13).';
COMMENT ON COLUMN inventory_transactions.note IS 'An optional free-text note a manual change carries, so a damage or a correction can say why. Null for a system-caused change.';
COMMENT ON COLUMN inventory_transactions.created_at IS 'Set once. The history order is created_at then id (FR-008, research D14).';

-- FR-004, research D9: an unlisted kind cannot be stored.
ALTER TABLE inventory_transactions
    ADD CONSTRAINT inventory_transactions_kind_ck CHECK (
        kind IN ('RESTOCK', 'DAMAGE', 'ADJUSTMENT', 'SALE')
    );
-- research D9: the ledger holds no empty rows.
ALTER TABLE inventory_transactions
    ADD CONSTRAINT inventory_transactions_delta_ck CHECK (delta <> 0);
-- FR-009: the recorded result can never describe a negative shelf.
ALTER TABLE inventory_transactions
    ADD CONSTRAINT inventory_transactions_resulting_ck CHECK (resulting_quantity >= 0);
-- research D11: a removed product takes its history with it.
ALTER TABLE inventory_transactions
    ADD CONSTRAINT inventory_transactions_product_fk FOREIGN KEY (product_id)
    REFERENCES products(id) ON DELETE CASCADE;

-- FR-020, FR-022: the mechanism that makes an outside event apply at most once,
-- even when two identical events arrive together. A manual movement carries no
-- reference and is therefore unconstrained.
CREATE UNIQUE INDEX inventory_transactions_source_key
    ON inventory_transactions (source_reference) WHERE source_reference IS NOT NULL;

-- FR-008, research D14: the history read, in the order the changes happened.
CREATE INDEX inventory_transactions_history_idx
    ON inventory_transactions (product_id, created_at, id);

-- A quantity set aside for one order while it is being paid (the clarification).
-- It is the difference between what is on the shelf and what is available.
CREATE TABLE stock_holds (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id  uuid NOT NULL,
    order_id    uuid NOT NULL,
    quantity    bigint NOT NULL,
    status      text NOT NULL,
    expires_at  timestamptz NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz
);

COMMENT ON COLUMN stock_holds.product_id IS 'The product set aside. References products(id) with ON DELETE CASCADE (research D11).';
COMMENT ON COLUMN stock_holds.order_id IS 'The order the hold belongs to. A loose reference, deliberately not a foreign key: module 07 owns orders and does not exist yet (research D5).';
COMMENT ON COLUMN stock_holds.quantity IS 'How many units are set aside. A positive whole number (FR-018).';
COMMENT ON COLUMN stock_holds.status IS 'ACTIVE, CONSUMED or RELEASED. A hold is active only while it is unresolved and its expiry is in the future (research D6).';
COMMENT ON COLUMN stock_holds.expires_at IS 'When an unpaid hold returns its quantity. Computed in the application from the injected clock plus the fifteen-minute window, so the same time source creates and judges the hold (research D15).';
COMMENT ON COLUMN stock_holds.resolved_at IS 'When the hold stopped being active — paid, cancelled or expired. Null exactly while the hold is ACTIVE.';

-- FR-018: setting aside nothing is not a hold.
ALTER TABLE stock_holds
    ADD CONSTRAINT stock_holds_quantity_ck CHECK (quantity > 0);
-- The clarification: the three ways a hold ends, and no others.
ALTER TABLE stock_holds
    ADD CONSTRAINT stock_holds_status_ck CHECK (
        status IN ('ACTIVE', 'CONSUMED', 'RELEASED')
    );
-- A resolved hold has a time and an active one does not; the two facts cannot
-- disagree.
ALTER TABLE stock_holds
    ADD CONSTRAINT stock_holds_resolved_ck CHECK (
        (status = 'ACTIVE') = (resolved_at IS NULL)
    );
-- research D11: a removed product's holds leave with it.
ALTER TABLE stock_holds
    ADD CONSTRAINT stock_holds_product_fk FOREIGN KEY (product_id)
    REFERENCES products(id) ON DELETE CASCADE;

-- FR-019: an order cannot set aside the same product twice, even from two
-- concurrent beginnings.
CREATE UNIQUE INDEX stock_holds_active_key
    ON stock_holds (order_id, product_id) WHERE status = 'ACTIVE';
-- FR-018: the availability sum reads the active holds of one product.
CREATE INDEX stock_holds_active_idx
    ON stock_holds (product_id) WHERE status = 'ACTIVE';
-- FR-015, research D6: the sweeper finds expired active holds without scanning
-- the resolved ones.
CREATE INDEX stock_holds_sweep_idx
    ON stock_holds (status, expires_at);

-- +goose Down
DROP INDEX stock_holds_sweep_idx;
DROP INDEX stock_holds_active_idx;
DROP INDEX stock_holds_active_key;
ALTER TABLE stock_holds DROP CONSTRAINT stock_holds_product_fk;
ALTER TABLE stock_holds DROP CONSTRAINT stock_holds_resolved_ck;
ALTER TABLE stock_holds DROP CONSTRAINT stock_holds_status_ck;
ALTER TABLE stock_holds DROP CONSTRAINT stock_holds_quantity_ck;
DROP TABLE stock_holds;

DROP INDEX inventory_transactions_history_idx;
DROP INDEX inventory_transactions_source_key;
ALTER TABLE inventory_transactions DROP CONSTRAINT inventory_transactions_product_fk;
ALTER TABLE inventory_transactions DROP CONSTRAINT inventory_transactions_resulting_ck;
ALTER TABLE inventory_transactions DROP CONSTRAINT inventory_transactions_delta_ck;
ALTER TABLE inventory_transactions DROP CONSTRAINT inventory_transactions_kind_ck;
DROP TABLE inventory_transactions;

ALTER TABLE stock_levels DROP CONSTRAINT stock_levels_product_fk;
ALTER TABLE stock_levels DROP CONSTRAINT stock_levels_quantity_ck;
DROP TABLE stock_levels;
