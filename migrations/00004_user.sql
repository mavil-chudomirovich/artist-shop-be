-- +goose Up
-- Module 02 (user) extends the users table created by module 01 (auth) with the
-- profile columns. Additive and nullable only: existing accounts keep working and
-- a profile is optional by design, so no backfill is needed.
ALTER TABLE users
    ADD COLUMN display_name      text,
    ADD COLUMN phone             varchar(20),
    ADD COLUMN avatar_public_id  text,
    ADD COLUMN avatar_secure_url text,
    ADD COLUMN avatar_width      integer,
    ADD COLUMN avatar_height     integer;

COMMENT ON COLUMN users.display_name IS 'Customer-facing name, trimmed; NULL or empty when the customer never set one. Owned by module 02 (user).';
COMMENT ON COLUMN users.phone IS 'Customer phone, normalised to ten digits starting with 0; NULL when unset. Owned by module 02 (user).';
COMMENT ON COLUMN users.avatar_public_id IS 'Opaque media identifier of the stored avatar; NULL when the customer has no photo. Owned by module 02 (user).';
COMMENT ON COLUMN users.avatar_secure_url IS 'Displayable link of the stored avatar; NULL together with the other avatar columns. Owned by module 02 (user).';
COMMENT ON COLUMN users.avatar_width IS 'Pixel width of the stored avatar, at most 512 after the provider resize. Owned by module 02 (user).';
COMMENT ON COLUMN users.avatar_height IS 'Pixel height of the stored avatar, as reported by the media provider. Owned by module 02 (user).';

-- Storage-level guarantee of the "avatar columns are either all null or all
-- populated" invariant (FR-016). The application writes the four columns in one
-- statement, but a bug or a manual psql session could otherwise persist half an
-- avatar, leaving a reference the client is unable to render.
ALTER TABLE users
    ADD CONSTRAINT users_avatar_columns_all_or_none_ck CHECK (
        (avatar_public_id   IS NULL
            AND avatar_secure_url IS NULL
            AND avatar_width     IS NULL
            AND avatar_height    IS NULL)
        OR
        (avatar_public_id   IS NOT NULL
            AND avatar_secure_url IS NOT NULL
            AND avatar_width     IS NOT NULL
            AND avatar_height    IS NOT NULL)
    );

COMMENT ON CONSTRAINT users_avatar_columns_all_or_none_ck ON users IS 'The four avatar columns are either all null (no avatar) or all populated (complete avatar reference); a partial avatar is rejected at write time.';

CREATE TABLE addresses (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    recipient_name  text NOT NULL,
    recipient_phone varchar(20) NOT NULL,
    province_code   varchar(10) NOT NULL,
    province_name   text NOT NULL,
    ward_code       varchar(15) NOT NULL,
    ward_name       text NOT NULL,
    street_address  text NOT NULL,
    is_default      boolean NOT NULL DEFAULT false,
    deleted_at      timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- Province and ward names are captured when the address is saved so a later
-- rename of an administrative unit cannot rewrite what the customer entered.
COMMENT ON COLUMN addresses.province_name IS 'Province name captured at save time, so history stays truthful after a dataset rename.';
COMMENT ON COLUMN addresses.ward_name IS 'Ward name captured at save time, so history stays truthful after a dataset rename.';
COMMENT ON COLUMN addresses.is_default IS 'At most one visible address per account is the default; the partial unique index enforces it.';
COMMENT ON COLUMN addresses.deleted_at IS 'Non-null means the address is hidden: excluded from lists and never the default.';

-- Every read of an account's addresses filters on this predicate, so the index
-- carries the same predicate and stays small as hidden rows accumulate.
CREATE INDEX addresses_user_active_idx
    ON addresses (user_id) WHERE deleted_at IS NULL;

-- Storage-level guarantee of the "at most one default address" invariant
-- (ADR-003). Atomic, so neither two concurrent requests nor a manual psql
-- session can create two defaults.
CREATE UNIQUE INDEX addresses_one_default_per_user
    ON addresses (user_id) WHERE is_default AND deleted_at IS NULL;

-- +goose Down
DROP INDEX addresses_one_default_per_user;
DROP INDEX addresses_user_active_idx;
DROP TABLE addresses;

ALTER TABLE users
    DROP CONSTRAINT users_avatar_columns_all_or_none_ck;

ALTER TABLE users
    DROP COLUMN avatar_height,
    DROP COLUMN avatar_width,
    DROP COLUMN avatar_secure_url,
    DROP COLUMN avatar_public_id,
    DROP COLUMN phone,
    DROP COLUMN display_name;
