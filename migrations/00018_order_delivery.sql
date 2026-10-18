-- +goose Up
-- Module 07 (order) delivery & completion (feature 019). It widens the stored
-- state set to the seven states (renaming SHIPPED to SHIPPING and adding
-- DELIVERED), adds the delivery and completion columns, the completion-sweep
-- index, and the two evidence tables order_delivery_photos and order_feedback.
--
-- data-migration: the only data change is the status backfill below. The old
-- status check is dropped, the stored SHIPPED value is renamed SHIPPING, and only
-- then is the widened seven-value check installed — so a database holding 010
-- orders migrates cleanly instead of failing a check on the old value. The three
-- new orders columns are nullable and stay NULL on existing rows, so no existing
-- order is touched (research D1, D2).

-- FR-001, FR-002: widen the stored state set to the seven states. The old
-- constraint is dropped FIRST, the SHIPPED -> SHIPPING backfill runs, and the
-- seven-value check is added LAST, so the rename is never refused by a check.
ALTER TABLE orders DROP CONSTRAINT orders_status_ck;
UPDATE orders SET status = 'SHIPPING' WHERE status = 'SHIPPED';
ALTER TABLE orders ADD CONSTRAINT orders_status_ck CHECK (
    status IN ('PENDING', 'PAYMENT_PENDING', 'PAID', 'SHIPPING', 'DELIVERED', 'COMPLETED', 'CANCELLED')
);

-- FR-008, FR-012: when the operator marked the order delivered; NULL before
-- delivery. It anchors the five-day automatic completion window.
ALTER TABLE orders ADD COLUMN delivered_at timestamptz;
-- FR-014: when the order reached COMPLETED, by the customer or the sweep; NULL
-- before then.
ALTER TABLE orders ADD COLUMN completed_at timestamptz;
-- FR-014: the customer who confirmed receipt; NULL when the sweep completed it.
-- A loose reference with NO foreign key, as orders.user_id (research D10).
ALTER TABLE orders ADD COLUMN completed_by uuid;

COMMENT ON COLUMN orders.status IS 'One of PENDING, PAYMENT_PENDING, PAID, SHIPPING, DELIVERED, COMPLETED or CANCELLED. Changed only through the domain transitions (FR-001, FR-002).';
COMMENT ON COLUMN orders.delivered_at IS 'When the operator marked the order delivered; anchors the five-day auto-completion window; NULL before delivery (FR-008, FR-012).';
COMMENT ON COLUMN orders.completed_at IS 'When the order reached COMPLETED, by the customer or the sweep; NULL before then (FR-014).';
COMMENT ON COLUMN orders.completed_by IS 'The customer who confirmed receipt; NULL when the sweep completed it. A loose reference with NO foreign key (FR-014).';

-- FR-012, research D6: the completion sweep finds delivered orders past their
-- window without scanning every order.
CREATE INDEX orders_completion_idx ON orders (status, delivered_at);

-- FR-006, FR-008, FR-009, FR-010, research D3: dispatch and delivery evidence,
-- one table keyed by the stage it belongs to. Image bytes are held by the media
-- provider; only this reference is stored.
CREATE TABLE order_delivery_photos (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id   uuid NOT NULL,
    stage      text NOT NULL,
    public_id  text NOT NULL,
    url        text NOT NULL,
    width      integer NOT NULL,
    height     integer NOT NULL,
    position   integer NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE order_delivery_photos IS 'Dispatch and delivery evidence photos of an order, keyed by stage (FR-006, FR-008, FR-010).';
COMMENT ON COLUMN order_delivery_photos.order_id IS 'The order the photo belongs to. References orders(id) ON DELETE CASCADE: a removed order takes its evidence with it (FR-010).';
COMMENT ON COLUMN order_delivery_photos.stage IS 'The delivery step the photo is evidence for: SHIPPING (dispatch) or DELIVERED (delivery) (FR-006, FR-008).';
COMMENT ON COLUMN order_delivery_photos.public_id IS 'The media provider''s opaque identifier, used to release the asset (FR-010).';
COMMENT ON COLUMN order_delivery_photos.url IS 'The displayable link returned to clients (FR-010).';
COMMENT ON COLUMN order_delivery_photos.position IS 'Order within its stage; insertion order (FR-010).';
COMMENT ON COLUMN order_delivery_photos.created_at IS 'Set once.';

ALTER TABLE order_delivery_photos
    ADD CONSTRAINT order_delivery_photos_order_fk FOREIGN KEY (order_id)
    REFERENCES orders(id) ON DELETE CASCADE;
ALTER TABLE order_delivery_photos
    ADD CONSTRAINT order_delivery_photos_stage_ck CHECK (stage IN ('SHIPPING', 'DELIVERED'));
CREATE INDEX order_delivery_photos_ordering_idx ON order_delivery_photos (order_id, stage, position, id);

-- FR-015, FR-016, research D8: order-level feedback, at most one per order. The
-- content is text only; a rating and moderation belong to module 13.
CREATE TABLE order_feedback (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id   uuid NOT NULL,
    author_id  uuid NOT NULL,
    content    text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE order_feedback IS 'The customer''s one order-level feedback to the artist (FR-015, FR-016).';
COMMENT ON COLUMN order_feedback.order_id IS 'The order the feedback is about. References orders(id) ON DELETE CASCADE: a removed order takes its feedback with it (FR-015).';
COMMENT ON COLUMN order_feedback.author_id IS 'The customer who wrote it. A loose reference with NO foreign key, as orders.user_id (FR-016).';
COMMENT ON COLUMN order_feedback.content IS 'The feedback text, 1 to 2000 characters after trimming (FR-015).';
COMMENT ON COLUMN order_feedback.created_at IS 'Set once.';

ALTER TABLE order_feedback
    ADD CONSTRAINT order_feedback_order_fk FOREIGN KEY (order_id)
    REFERENCES orders(id) ON DELETE CASCADE;
CREATE UNIQUE INDEX order_feedback_order_key ON order_feedback (order_id);
ALTER TABLE order_feedback
    ADD CONSTRAINT order_feedback_content_ck CHECK (length(btrim(content)) BETWEEN 1 AND 2000);

-- +goose Down
-- Reverse the feature. The seven-value check is dropped FIRST, the stored
-- SHIPPING value is renamed back, and the six-value check is installed LAST, so
-- the reverse backfill is never refused by a check (the mirror of the Up order).
-- The two evidence tables and the sweep index are then removed, and the three new
-- orders columns dropped.
ALTER TABLE orders DROP CONSTRAINT orders_status_ck;
UPDATE orders SET status = 'SHIPPED' WHERE status = 'SHIPPING';
ALTER TABLE orders ADD CONSTRAINT orders_status_ck CHECK (
    status IN ('PENDING', 'PAYMENT_PENDING', 'PAID', 'SHIPPED', 'COMPLETED', 'CANCELLED')
);

DROP INDEX order_feedback_order_key;
ALTER TABLE order_feedback DROP CONSTRAINT order_feedback_content_ck;
ALTER TABLE order_feedback DROP CONSTRAINT order_feedback_order_fk;
DROP TABLE order_feedback;

DROP INDEX order_delivery_photos_ordering_idx;
ALTER TABLE order_delivery_photos DROP CONSTRAINT order_delivery_photos_stage_ck;
ALTER TABLE order_delivery_photos DROP CONSTRAINT order_delivery_photos_order_fk;
DROP TABLE order_delivery_photos;

DROP INDEX orders_completion_idx;
ALTER TABLE orders DROP COLUMN completed_by;
ALTER TABLE orders DROP COLUMN completed_at;
ALTER TABLE orders DROP COLUMN delivered_at;

COMMENT ON COLUMN orders.status IS 'One of PENDING, PAYMENT_PENDING, PAID, SHIPPED, COMPLETED or CANCELLED. Changed only through the domain transitions (FR-001, FR-010).';
