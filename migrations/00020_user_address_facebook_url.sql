-- +goose Up
-- Feature 021 (address facebook URL): every delivery address carries the contact
-- Facebook link the shop uses to confirm the order. The column is added nullable
-- first so existing rows survive, then legacy rows are backfilled with the
-- placeholder, and only then is the column made NOT NULL with the non-empty
-- length check -- so a database holding pre-021 addresses migrates cleanly.
--
-- data-migration: the only data change is the placeholder backfill below. The
-- Down section drops the column, which is a destructive rollback; it is
-- acceptable because the column is new to this feature and holds no rescued
-- data (no pre-existing values are lost by dropping it).

-- FR-008: the column starts nullable so the backfill can run before the
-- NOT NULL guarantee is installed.
ALTER TABLE addresses ADD COLUMN facebook_url text;

-- Clarify 2026-10-19: legacy rows (created before the rule) carry the
-- placeholder once, displayed verbatim on old data.
UPDATE addresses SET facebook_url = 'https://facebook.com/unknown' WHERE facebook_url IS NULL;

ALTER TABLE addresses ALTER COLUMN facebook_url SET NOT NULL;

-- FR-008: storage-level counterpart of the application rule (1-500 characters
-- after trimming); an empty or overlong link is refused where it is stored.
ALTER TABLE addresses
    ADD CONSTRAINT addresses_facebook_url_nonempty_ck CHECK (
        char_length(facebook_url) > 0 AND char_length(facebook_url) <= 500
    );

COMMENT ON COLUMN addresses.facebook_url IS 'Contact Facebook link of the address, required (FR-001). Legacy rows backfilled once with the placeholder https://facebook.com/unknown; new writes must carry a real https facebook.com/fb.com link (FR-002).';
COMMENT ON CONSTRAINT addresses_facebook_url_nonempty_ck ON addresses IS 'facebook_url holds 1 to 500 characters (the length rule of FR-003, enforced at write time).';

-- +goose Down
-- Reverse the feature: drop the check first, then the column. Destructive by
-- design (see the data-migration note above); re-running Up backfills the
-- placeholder again on rows that predate the rule.
ALTER TABLE addresses DROP CONSTRAINT addresses_facebook_url_nonempty_ck;
ALTER TABLE addresses DROP COLUMN facebook_url;
