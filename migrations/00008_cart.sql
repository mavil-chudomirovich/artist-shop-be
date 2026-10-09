-- +goose Up
-- Module 06 (cart) introduces the two tables the spec needs (data-model.md): carts
-- holds the one working basket of a signed-in customer, and cart_items holds one
-- line per product the customer intends to buy, with the quantity and the price
-- captured when it was added. The change is additive: no existing table is
-- altered.

-- The one cart of one account. It exists so a customer's basket survives across
-- sessions and so a line has a cart to belong to.
CREATE TABLE carts (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

COMMENT ON COLUMN carts.user_id IS 'The one account the cart belongs to. References users(id) with ON DELETE CASCADE: an account takes its cart with it (FR-001, research D3).';
COMMENT ON COLUMN carts.created_at IS 'Set once.';
COMMENT ON COLUMN carts.updated_at IS 'Touched on every write, so the cart is attributable to a moment.';

-- FR-001: "one cart per account" is a uniqueness property. Two concurrent first
-- adds both pass an application check and both insert; only the database can
-- refuse the second (research D3, D8).
CREATE UNIQUE INDEX carts_user_key ON carts (user_id);

-- FR-001: an account's cart belongs to the account, so removing the account
-- removes its cart (research D3).
ALTER TABLE carts
    ADD CONSTRAINT carts_user_fk FOREIGN KEY (user_id)
    REFERENCES users(id) ON DELETE CASCADE;

-- One product the customer intends to buy.
CREATE TABLE cart_items (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    cart_id           uuid NOT NULL,
    product_id        uuid NOT NULL,
    quantity          bigint NOT NULL,
    unit_price_amount bigint NOT NULL,
    currency          char(3) NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

COMMENT ON COLUMN cart_items.cart_id IS 'The cart the line belongs to. References carts(id) with ON DELETE CASCADE (FR-001, research D3).';
COMMENT ON COLUMN cart_items.product_id IS 'The product. A loose reference with NO foreign key (research D3): removing a product must leave the customer''s line in place so the cart can report it as no longer available (FR-012). A cascading key would silently delete the line and a restricting key would block removing a product, which module 04 deliberately allows.';
COMMENT ON COLUMN cart_items.quantity IS 'How many the customer chose. A whole number, at least 1 (FR-006).';
COMMENT ON COLUMN cart_items.unit_price_amount IS 'The price in the currency''s minor unit, captured when the product was added and never re-read (FR-008, research D4).';
COMMENT ON COLUMN cart_items.currency IS 'The currency the captured amount is denominated in. Three uppercase letters (FR-009).';
COMMENT ON COLUMN cart_items.created_at IS 'Set once.';
COMMENT ON COLUMN cart_items.updated_at IS 'Touched on every write.';

-- FR-001: a removed cart takes its lines with it.
ALTER TABLE cart_items
    ADD CONSTRAINT cart_items_cart_fk FOREIGN KEY (cart_id)
    REFERENCES carts(id) ON DELETE CASCADE;

-- FR-006: a line holding zero or fewer is not a line; a customer empties a line by
-- removing it, and the database refuses a stored zero.
ALTER TABLE cart_items
    ADD CONSTRAINT cart_items_quantity_ck CHECK (quantity >= 1);
-- FR-009: a captured price is positive, as module 04's prices are.
ALTER TABLE cart_items
    ADD CONSTRAINT cart_items_unit_price_ck CHECK (unit_price_amount > 0);
-- FR-009: the captured currency is an unambiguous three-letter code.
ALTER TABLE cart_items
    ADD CONSTRAINT cart_items_currency_ck CHECK (currency ~ '^[A-Z]{3}$');

-- FR-002: "one line per product" is a uniqueness property. Two concurrent adds
-- both pass an application check and both insert, and the database is what
-- refuses the duplicate; the add is an upsert on this key, so the second sums the
-- quantity instead of failing (research D3, D9).
CREATE UNIQUE INDEX cart_items_cart_product_key ON cart_items (cart_id, product_id);

-- +goose Down
DROP INDEX cart_items_cart_product_key;
ALTER TABLE cart_items DROP CONSTRAINT cart_items_currency_ck;
ALTER TABLE cart_items DROP CONSTRAINT cart_items_unit_price_ck;
ALTER TABLE cart_items DROP CONSTRAINT cart_items_quantity_ck;
ALTER TABLE cart_items DROP CONSTRAINT cart_items_cart_fk;
DROP TABLE cart_items;

DROP INDEX carts_user_key;
ALTER TABLE carts DROP CONSTRAINT carts_user_fk;
DROP TABLE carts;
