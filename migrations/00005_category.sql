-- +goose Up
-- Module 03 (category) introduces the flat product catalogue. The change is
-- purely additive: it creates one table and touches nothing that already exists.
CREATE TABLE categories (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name            text NOT NULL,
    normalized_name text NOT NULL,
    slug            text NOT NULL,
    normalized_slug text NOT NULL,
    description     text NOT NULL,
    position        integer NOT NULL,
    is_visible      boolean NOT NULL DEFAULT true,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

COMMENT ON COLUMN categories.name IS 'Category name as the operator wrote it, trimmed. This is what a customer reads.';
COMMENT ON COLUMN categories.normalized_name IS 'Folding key for the name: trimmed and Unicode case-folded by the application (research D3). Indexed for catalogue-wide uniqueness, never returned to anyone.';
COMMENT ON COLUMN categories.slug IS 'Operator-written public link segment: lowercase unaccented letters, digits and single hyphens (FR-018).';
COMMENT ON COLUMN categories.normalized_slug IS 'Folding key for the slug: trimmed and case-folded by the application. Indexed for catalogue-wide uniqueness, never returned to anyone.';
COMMENT ON COLUMN categories.description IS 'Free text shown to customers; an empty string is a valid description.';
COMMENT ON COLUMN categories.position IS 'Operator preference for the catalogue order. Any whole number, including zero and negative; ties are broken by created_at then id (FR-003).';
COMMENT ON COLUMN categories.is_visible IS 'Whether a customer can see the category; the only state a category has beyond existing (research D9).';

-- The folding key is computed in the application with Go's Unicode-aware case
-- folding, not by the database: lower() follows the database collation, and under
-- the C collation it would not fold a Vietnamese uppercase letter, so the
-- catalogue would silently accept two names differing only by the case of one
-- Vietnamese letter (research D3).
--
-- These checks enforce only the *shape* of the stored value: it must already be
-- trimmed and lowercase. The database does not recompute the fold, because folding
-- belongs to the Unicode tables, not to the database's collation.
ALTER TABLE categories
    ADD CONSTRAINT categories_normalized_name_shape_ck CHECK (
        normalized_name = btrim(normalized_name)
        AND normalized_name = lower(normalized_name)
    );

ALTER TABLE categories
    ADD CONSTRAINT categories_normalized_slug_shape_ck CHECK (
        normalized_slug = btrim(normalized_slug)
        AND normalized_slug = lower(normalized_slug)
    );

-- FR-018: the segment a public link is built from must be URL-safe where it is
-- stored, not only where it arrives.
ALTER TABLE categories
    ADD CONSTRAINT categories_slug_format_ck CHECK (
        slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'
    );

-- FR-019: length() counts characters, not bytes, which is what a Vietnamese name
-- needs: 120 characters of Vietnamese is up to about 360 bytes.
ALTER TABLE categories
    ADD CONSTRAINT categories_name_length_ck CHECK (length(name) <= 120);

ALTER TABLE categories
    ADD CONSTRAINT categories_slug_length_ck CHECK (length(slug) <= 140);

ALTER TABLE categories
    ADD CONSTRAINT categories_description_length_ck CHECK (length(description) <= 2000);

-- FR-016, FR-017 and FR-021: catalogue-wide uniqueness that ignores letter case
-- and surrounding whitespace. An application-level check cannot survive two
-- concurrent writers, so the unique indexes are the guarantee and the domain
-- rule is the message a human reads.
CREATE UNIQUE INDEX categories_normalized_name_key ON categories (normalized_name);
CREATE UNIQUE INDEX categories_normalized_slug_key ON categories (normalized_slug);

-- FR-003: the order customers see is this index's order.
CREATE INDEX categories_ordering_idx ON categories (position, created_at, id);

-- +goose Down
DROP INDEX categories_ordering_idx;
DROP INDEX categories_normalized_slug_key;
DROP INDEX categories_normalized_name_key;

ALTER TABLE categories DROP CONSTRAINT categories_description_length_ck;
ALTER TABLE categories DROP CONSTRAINT categories_slug_length_ck;
ALTER TABLE categories DROP CONSTRAINT categories_name_length_ck;
ALTER TABLE categories DROP CONSTRAINT categories_slug_format_ck;
ALTER TABLE categories DROP CONSTRAINT categories_normalized_slug_shape_ck;
ALTER TABLE categories DROP CONSTRAINT categories_normalized_name_shape_ck;

DROP TABLE categories;
