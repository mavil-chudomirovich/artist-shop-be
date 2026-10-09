-- +goose Up
-- Module 07 (order) introduces the two tables the spec needs (data-model.md):
-- orders is the purchase a customer committed to, and order_items is one item of
-- it, holding a snapshot of the product as it was at checkout. The change is
-- additive: no existing table is altered and no data is backfilled.

-- The committed purchase. It carries who owns it, where it goes, what it costs and
-- where it is in its life. It exists so a customer's cart can become a record of
-- the sale that survives the cart and the product going away.
CREATE TABLE orders (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid NOT NULL,
    status          text NOT NULL,
    total_amount    bigint NOT NULL,
    currency        char(3) NOT NULL,
    recipient_name  text NOT NULL,
    recipient_phone text NOT NULL,
    province_code   text NOT NULL,
    province_name   text NOT NULL,
    ward_code       text NOT NULL,
    ward_name       text NOT NULL,
    street_address  text NOT NULL,
    expires_at      timestamptz NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

COMMENT ON COLUMN orders.user_id IS 'The account that owns the order. A loose reference with NO foreign key (research D12): an order is a record of a sale and must outlive the account it names, and a transfer changes it.';
COMMENT ON COLUMN orders.status IS 'One of PENDING_PAYMENT, PAID, SHIPPED, COMPLETED or CANCELLED. Changed only through the domain transitions (FR-009, research D9).';
COMMENT ON COLUMN orders.total_amount IS 'The committed total in the currency''s minor unit: the sum of quantity times unit_price_amount over the lines. Never a float and never rounded (FR-008, research D11).';
COMMENT ON COLUMN orders.currency IS 'The currency the total is in. Three uppercase letters (FR-008).';
COMMENT ON COLUMN orders.recipient_name IS 'The delivery name, captured from the chosen address at checkout so a later edit does not change a placed order (FR-003).';
COMMENT ON COLUMN orders.recipient_phone IS 'The delivery phone, captured at checkout (FR-003).';
COMMENT ON COLUMN orders.province_code IS 'The first-level unit code, captured at checkout (FR-003).';
COMMENT ON COLUMN orders.province_name IS 'The first-level unit name, captured at checkout (FR-003).';
COMMENT ON COLUMN orders.ward_code IS 'The second-level unit code, captured at checkout (FR-003).';
COMMENT ON COLUMN orders.ward_name IS 'The second-level unit name, captured at checkout (FR-003).';
COMMENT ON COLUMN orders.street_address IS 'The free-text street, captured at checkout (FR-003).';
COMMENT ON COLUMN orders.expires_at IS 'When an unpaid order cancels itself: its creation plus module 05''s hold window. The order owns its own deadline so it can leave "awaiting payment" by itself (FR-012, research D6).';
COMMENT ON COLUMN orders.created_at IS 'Set once; the order was placed now.';
COMMENT ON COLUMN orders.updated_at IS 'Touched on every write.';

-- FR-009, research D9: an unlisted state cannot be stored, so the domain's
-- transition table cannot be bypassed by writing a value.
ALTER TABLE orders
    ADD CONSTRAINT orders_status_ck CHECK (
        status IN ('PENDING_PAYMENT', 'PAID', 'SHIPPED', 'COMPLETED', 'CANCELLED')
    );
-- FR-008: the currency is an unambiguous three-letter code.
ALTER TABLE orders
    ADD CONSTRAINT orders_currency_ck CHECK (currency ~ '^[A-Z]{3}$');
-- FR-008: an order of at least one positive-price line has a positive total; a
-- non-positive total is refused where it is stored.
ALTER TABLE orders
    ADD CONSTRAINT orders_total_ck CHECK (total_amount > 0);

-- FR-018: the customer's list is one owner's orders, newest first.
CREATE INDEX orders_owner_idx ON orders (user_id, created_at, id);
-- FR-021: the operator's list is every order, newest first.
CREATE INDEX orders_admin_idx ON orders (created_at, id);
-- FR-012, research D6: the sweeper finds unpaid orders past their deadline
-- without scanning every order.
CREATE INDEX orders_expiry_idx ON orders (status, expires_at);

-- One item of an order: a snapshot of the product as it was at checkout, so an
-- order never depends on a product that may be removed (FR-002, research D3).
CREATE TABLE order_items (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id          uuid NOT NULL,
    product_id        uuid NOT NULL,
    name              text NOT NULL,
    slug              text NOT NULL,
    unit_price_amount bigint NOT NULL,
    currency          char(3) NOT NULL,
    quantity          bigint NOT NULL,
    position          integer NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now()
);

COMMENT ON COLUMN order_items.order_id IS 'The order the line belongs to. References orders(id) with ON DELETE CASCADE: a removed order takes its lines with it (FR-001).';
COMMENT ON COLUMN order_items.product_id IS 'The product bought, an informational reference with NO foreign key (research D3): the line is a snapshot and is never joined back to the product, so a removed product cannot cascade into or block the order.';
COMMENT ON COLUMN order_items.name IS 'The product''s name at checkout; a snapshot, never re-read (FR-002).';
COMMENT ON COLUMN order_items.slug IS 'The product''s link segment at checkout; a snapshot, never re-read (FR-002).';
COMMENT ON COLUMN order_items.unit_price_amount IS 'The unit price at checkout, in the currency''s minor unit. Never a float (FR-002, FR-008).';
COMMENT ON COLUMN order_items.currency IS 'The currency the unit price is in. Three uppercase letters (FR-008).';
COMMENT ON COLUMN order_items.quantity IS 'How many, at least 1 (FR-002).';
COMMENT ON COLUMN order_items.position IS 'The order the lines are listed in (FR-018, FR-021).';
COMMENT ON COLUMN order_items.created_at IS 'Set once.';

-- FR-001: a removed order takes its lines with it.
ALTER TABLE order_items
    ADD CONSTRAINT order_items_order_fk FOREIGN KEY (order_id)
    REFERENCES orders(id) ON DELETE CASCADE;
-- FR-002: a cart holds one line per product, so an order does too; the pair is
-- the natural key.
CREATE UNIQUE INDEX order_items_order_product_key ON order_items (order_id, product_id);
-- FR-002: a line of zero or fewer is not a line.
ALTER TABLE order_items
    ADD CONSTRAINT order_items_quantity_ck CHECK (quantity >= 1);
-- FR-008: a snapshot price is positive.
ALTER TABLE order_items
    ADD CONSTRAINT order_items_unit_price_ck CHECK (unit_price_amount > 0);
-- FR-008: the currency is unambiguous.
ALTER TABLE order_items
    ADD CONSTRAINT order_items_currency_ck CHECK (currency ~ '^[A-Z]{3}$');
-- FR-018, FR-021: the lines are read in order for every order read.
CREATE INDEX order_items_ordering_idx ON order_items (order_id, position, id);

-- +goose Down
DROP INDEX order_items_ordering_idx;
ALTER TABLE order_items DROP CONSTRAINT order_items_currency_ck;
ALTER TABLE order_items DROP CONSTRAINT order_items_unit_price_ck;
ALTER TABLE order_items DROP CONSTRAINT order_items_quantity_ck;
DROP INDEX order_items_order_product_key;
ALTER TABLE order_items DROP CONSTRAINT order_items_order_fk;
DROP TABLE order_items;

DROP INDEX orders_expiry_idx;
DROP INDEX orders_admin_idx;
DROP INDEX orders_owner_idx;
ALTER TABLE orders DROP CONSTRAINT orders_total_ck;
ALTER TABLE orders DROP CONSTRAINT orders_currency_ck;
ALTER TABLE orders DROP CONSTRAINT orders_status_ck;
DROP TABLE orders;
