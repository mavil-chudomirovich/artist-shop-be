-- +goose Up
-- FR-025 (feature 019, extension): the optional photos attached to a customer's
-- order-level feedback, stored exactly like delivery evidence. Image bytes live
-- outside the service, held by the media provider; only this reference is stored.
-- The five-photo ceiling is a count, which no unique index can express, so it is
-- enforced by the use case (research D16, data-model.md).

CREATE TABLE order_feedback_photos (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    feedback_id uuid NOT NULL,
    public_id   text NOT NULL,
    url         text NOT NULL,
    width       integer NOT NULL,
    height      integer NOT NULL,
    position    integer NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE order_feedback_photos IS 'The optional photos attached to a customer''s order-level feedback, up to five, ordered by position (FR-025).';
COMMENT ON COLUMN order_feedback_photos.feedback_id IS 'The feedback the photo belongs to. References order_feedback(id) ON DELETE CASCADE: a removed feedback takes its photos with it (FR-025).';
COMMENT ON COLUMN order_feedback_photos.public_id IS 'The media provider''s opaque identifier, used to release the asset (FR-025).';
COMMENT ON COLUMN order_feedback_photos.url IS 'The displayable link returned to clients (FR-025).';
COMMENT ON COLUMN order_feedback_photos.position IS 'Order within the feedback; insertion order (FR-025).';
COMMENT ON COLUMN order_feedback_photos.created_at IS 'Set once.';

ALTER TABLE order_feedback_photos
    ADD CONSTRAINT order_feedback_photos_feedback_fk FOREIGN KEY (feedback_id)
    REFERENCES order_feedback(id) ON DELETE CASCADE;
CREATE INDEX order_feedback_photos_ordering_idx ON order_feedback_photos (feedback_id, position, id);

-- +goose Down
-- Reverse the extension: the ordering index, the foreign key and the table.
DROP INDEX order_feedback_photos_ordering_idx;
ALTER TABLE order_feedback_photos DROP CONSTRAINT order_feedback_photos_feedback_fk;
DROP TABLE order_feedback_photos;
