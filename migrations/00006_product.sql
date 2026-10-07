-- +goose Up
-- Module 04 (product) introduces the catalogue: the products themselves, their
-- pictures and the membership rows that record what a combo set contains. The
-- change is additive except for one thing: products_category_fk is added to the
-- categories table module 03 owns, and it is what makes "a category with products
-- cannot be removed" true at the storage layer (FR-034, FR-035, research D15).
--
-- No inventory count is stored anywhere here. The sell state is an operator's act
-- until module 05 exists, so a quantity column would be a second source of truth
-- with nothing to read it (FR-038, research D8).
CREATE TABLE products (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name                 text NOT NULL,
    slug                 text NOT NULL,
    normalized_slug      text NOT NULL,
    description          text NOT NULL,
    price_amount         bigint NOT NULL,
    currency             char(3) NOT NULL,
    category_id          uuid NOT NULL,
    position             integer NOT NULL,
    sell_state           text NOT NULL,
    is_set               boolean NOT NULL DEFAULT false,
    is_preorder          boolean NOT NULL DEFAULT false,
    preorder_expected_at date,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now()
);

COMMENT ON COLUMN products.name IS 'Product name as the operator wrote it, trimmed. Not unique (research D5); this is what a customer reads.';
COMMENT ON COLUMN products.slug IS 'Operator-written public link segment: lowercase unaccented letters, digits and single hyphens (FR-029).';
COMMENT ON COLUMN products.normalized_slug IS 'Folding key for the slug: trimmed and Unicode case-folded by the application (research D4). Indexed for catalogue-wide uniqueness, never returned to anyone.';
COMMENT ON COLUMN products.description IS 'Free text shown to customers; an empty string is a valid description.';
COMMENT ON COLUMN products.price_amount IS 'Price in the currency minor unit (for example the dong for VND). A positive integer, never a float (FR-031, research D3).';
COMMENT ON COLUMN products.currency IS 'Currency code: exactly three uppercase letters (FR-030).';
COMMENT ON COLUMN products.category_id IS 'The one category the product belongs to. References categories(id) with ON DELETE RESTRICT, so a category that still has products cannot be removed (FR-034, research D15).';
COMMENT ON COLUMN products.position IS 'Operator preference for the catalogue order. Any whole number, including zero and negative; ties are broken by created_at then id (FR-009).';
COMMENT ON COLUMN products.sell_state IS 'Selling-life state: COMING_SOON, ACTIVE, OUT_OF_STOCK or DISCONTINUED (FR-022). Changed only through the domain transitions, never written directly.';
COMMENT ON COLUMN products.is_set IS 'Whether this product is a combo set: a product in its own right with a price the operator sets (FR-039, research D10).';
COMMENT ON COLUMN products.is_preorder IS 'Whether the product is announced as a pre-order while it is not on sale. It is what makes an unlaunched product visible to customers (FR-002, FR-040, research D9).';
COMMENT ON COLUMN products.preorder_expected_at IS 'Optional expected availability date of a pre-order. Only meaningful while is_preorder is true (FR-040).';

-- The folding key is computed in the application with Go's Unicode-aware case
-- folding, not by the database: lower() follows the database collation, and under
-- the C collation it would not fold a Vietnamese uppercase letter (research D4).
-- These checks enforce only the *shape* of the stored value: it must already be
-- trimmed and lowercase.
ALTER TABLE products
    ADD CONSTRAINT products_normalized_slug_shape_ck CHECK (
        normalized_slug = btrim(normalized_slug)
        AND normalized_slug = lower(normalized_slug)
    );

-- FR-029: the segment a public link is built from must be URL-safe where it is
-- stored, not only where it arrives.
ALTER TABLE products
    ADD CONSTRAINT products_slug_format_ck CHECK (
        slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'
    );

-- FR-030: length() counts characters, not bytes, which is what a Vietnamese
-- description needs: 5000 characters of Vietnamese is far more than 5000 bytes.
ALTER TABLE products
    ADD CONSTRAINT products_name_length_ck CHECK (length(name) <= 120);
ALTER TABLE products
    ADD CONSTRAINT products_slug_length_ck CHECK (length(slug) <= 140);
ALTER TABLE products
    ADD CONSTRAINT products_description_length_ck CHECK (length(description) <= 5000);

-- FR-031, FR-032: a price is a positive integer in the currency minor unit and
-- the currency is exactly three uppercase letters; neither can be stored any other
-- way, so no code path can create a product that violates them.
ALTER TABLE products
    ADD CONSTRAINT products_price_amount_ck CHECK (price_amount > 0);
ALTER TABLE products
    ADD CONSTRAINT products_currency_ck CHECK (currency ~ '^[A-Z]{3}$');

-- FR-022: an unlisted state cannot be stored, so the domain transition table
-- cannot be bypassed by writing a value.
ALTER TABLE products
    ADD CONSTRAINT products_sell_state_ck CHECK (
        sell_state IN ('COMING_SOON', 'ACTIVE', 'OUT_OF_STOCK', 'DISCONTINUED')
    );

-- FR-040: a product that is on sale is not "coming soon", and an expected date
-- belongs to a pre-order and to nothing else.
ALTER TABLE products
    ADD CONSTRAINT products_preorder_ck CHECK (NOT is_preorder OR sell_state <> 'ACTIVE');
ALTER TABLE products
    ADD CONSTRAINT products_preorder_date_ck CHECK (preorder_expected_at IS NULL OR is_preorder);

-- FR-034, FR-035, research D15: the reference module 03 could not create. A
-- restricting delete is what makes module 03's "a category with products cannot be
-- removed" true at the storage layer, rather than in one code path.
ALTER TABLE products
    ADD CONSTRAINT products_category_fk FOREIGN KEY (category_id)
    REFERENCES categories(id) ON DELETE RESTRICT;

-- FR-033: catalogue-wide uniqueness of the folding key; only the database can
-- reject a second concurrent writer.
CREATE UNIQUE INDEX products_normalized_slug_key ON products (normalized_slug);

-- FR-009: the order customers see is this index's order.
CREATE INDEX products_ordering_idx ON products (position, created_at, id);

-- FR-006: the public category filter and the foreign key both need it.
CREATE INDEX products_category_idx ON products (category_id);

-- Pictures: only a reference to the media provider's asset is stored, never the
-- bytes (FR-018, Constitution, Media constraint).
CREATE TABLE product_images (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id uuid NOT NULL,
    public_id  text NOT NULL,
    secure_url text NOT NULL,
    width      integer NOT NULL,
    height     integer NOT NULL,
    position   integer NOT NULL,
    is_primary boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);

COMMENT ON COLUMN product_images.product_id IS 'The product this picture belongs to. ON DELETE CASCADE removes the picture rows with the product (research D13).';
COMMENT ON COLUMN product_images.public_id IS 'The media provider opaque identifier, used to release the stored asset (FR-018).';
COMMENT ON COLUMN product_images.secure_url IS 'The displayable link returned to clients; it must be HTTPS (FR-016).';
COMMENT ON COLUMN product_images.width IS 'Stored pixel width as the provider reported it after the resize (research D16).';
COMMENT ON COLUMN product_images.height IS 'Stored pixel height as the provider reported it.';
COMMENT ON COLUMN product_images.position IS 'Display order, insertion order; the operator does not edit it (research D11).';
COMMENT ON COLUMN product_images.is_primary IS 'Whether this is the product main picture. At most one per product, enforced by product_images_one_primary_key (FR-016, research D7).';

ALTER TABLE product_images
    ADD CONSTRAINT product_images_product_fk FOREIGN KEY (product_id)
    REFERENCES products(id) ON DELETE CASCADE;
ALTER TABLE product_images
    ADD CONSTRAINT product_images_width_ck CHECK (width > 0);
ALTER TABLE product_images
    ADD CONSTRAINT product_images_height_ck CHECK (height > 0);
ALTER TABLE product_images
    ADD CONSTRAINT product_images_secure_url_ck CHECK (secure_url LIKE 'https://%');

-- FR-016, research D7: "at most one main picture" is a uniqueness property, so it
-- is an index rather than an application check. "At least one while any picture
-- exists" is a transition and lives in the use case.
CREATE UNIQUE INDEX product_images_one_primary_key
    ON product_images (product_id) WHERE is_primary;
CREATE INDEX product_images_ordering_idx ON product_images (product_id, position, id);

-- Set membership: a set is an ordinary product with is_set true; these rows record
-- what is inside one and never decide its price (FR-039, research D10).
CREATE TABLE product_set_items (
    set_product_id    uuid NOT NULL,
    member_product_id uuid NOT NULL,
    position          integer NOT NULL
);

COMMENT ON COLUMN product_set_items.set_product_id IS 'The combo set. ON DELETE CASCADE removes the membership rows when the set is removed and touches nothing else (FR-039).';
COMMENT ON COLUMN product_set_items.member_product_id IS 'A product inside the set. ON DELETE CASCADE removes it from the set when the member itself is removed.';
COMMENT ON COLUMN product_set_items.position IS 'The order the members are listed in.';

ALTER TABLE product_set_items
    ADD CONSTRAINT product_set_items_pkey PRIMARY KEY (set_product_id, member_product_id);
ALTER TABLE product_set_items
    ADD CONSTRAINT product_set_items_not_self_ck CHECK (set_product_id <> member_product_id);
ALTER TABLE product_set_items
    ADD CONSTRAINT product_set_items_set_fk FOREIGN KEY (set_product_id)
    REFERENCES products(id) ON DELETE CASCADE;
ALTER TABLE product_set_items
    ADD CONSTRAINT product_set_items_member_fk FOREIGN KEY (member_product_id)
    REFERENCES products(id) ON DELETE CASCADE;

-- +goose Down
ALTER TABLE product_set_items DROP CONSTRAINT product_set_items_member_fk;
ALTER TABLE product_set_items DROP CONSTRAINT product_set_items_set_fk;
ALTER TABLE product_set_items DROP CONSTRAINT product_set_items_not_self_ck;
ALTER TABLE product_set_items DROP CONSTRAINT product_set_items_pkey;
DROP TABLE product_set_items;

DROP INDEX product_images_ordering_idx;
DROP INDEX product_images_one_primary_key;
ALTER TABLE product_images DROP CONSTRAINT product_images_secure_url_ck;
ALTER TABLE product_images DROP CONSTRAINT product_images_height_ck;
ALTER TABLE product_images DROP CONSTRAINT product_images_width_ck;
ALTER TABLE product_images DROP CONSTRAINT product_images_product_fk;
DROP TABLE product_images;

DROP INDEX products_category_idx;
DROP INDEX products_ordering_idx;
DROP INDEX products_normalized_slug_key;
ALTER TABLE products DROP CONSTRAINT products_category_fk;
ALTER TABLE products DROP CONSTRAINT products_preorder_date_ck;
ALTER TABLE products DROP CONSTRAINT products_preorder_ck;
ALTER TABLE products DROP CONSTRAINT products_sell_state_ck;
ALTER TABLE products DROP CONSTRAINT products_currency_ck;
ALTER TABLE products DROP CONSTRAINT products_price_amount_ck;
ALTER TABLE products DROP CONSTRAINT products_description_length_ck;
ALTER TABLE products DROP CONSTRAINT products_slug_length_ck;
ALTER TABLE products DROP CONSTRAINT products_name_length_ck;
ALTER TABLE products DROP CONSTRAINT products_slug_format_ck;
ALTER TABLE products DROP CONSTRAINT products_normalized_slug_shape_ck;
DROP TABLE products;
