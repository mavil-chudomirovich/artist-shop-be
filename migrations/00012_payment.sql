-- +goose Up
-- Module 08 (payment) introduces the two tables the spec needs (data-model.md):
-- payments is the module's own record of what the customer agreed to pay, what the
-- provider said, and where that leaves both; payment_events is one row per
-- decision the module made about a payment, so "apply once" is provable after the
-- fact. The change is additive: no existing table is altered and no data is
-- backfilled.

-- One payment for one order, with its own four-state life (data-model.md).
CREATE TABLE payments (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id           uuid NOT NULL,
    user_id            uuid NOT NULL,
    order_code         bigint NOT NULL,
    amount             bigint NOT NULL,
    currency           char(3) NOT NULL,
    description        text NOT NULL,
    order_version      bigint NOT NULL,
    status             text NOT NULL,
    provider           text NOT NULL,
    provider_reference text,
    payment_link_id    text,
    checkout_url       text,
    qr_code            text,
    unmatched_reason   text,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);

COMMENT ON COLUMN payments.order_id IS 'The order this payment belongs to. A loose reference with NO foreign key, exactly as orders.user_id: the payment and the order live in different modules (research D5).';
COMMENT ON COLUMN payments.user_id IS 'The account that started the payment, copied at creation so the operator can see who paid without a join. A loose reference with NO foreign key.';
COMMENT ON COLUMN payments.order_code IS 'PayOS''s orderCode: a random positive 53-bit integer, unique by an index (FR-022, research D4).';
COMMENT ON COLUMN payments.amount IS 'The amount requested, in the currency''s minor unit. It equals the order total with no shipping fee (FR-004, ADR 015 §5). Never a float.';
COMMENT ON COLUMN payments.currency IS 'The currency the amount is in. Three uppercase letters; VND in MVP (FR-004).';
COMMENT ON COLUMN payments.description IS 'The short description sent to PayOS (the payer sees it).';
COMMENT ON COLUMN payments.order_version IS 'The order''s content version at the time the payment was created. A confirmation is applied only when this still matches (FR-012, research D5, D7).';
COMMENT ON COLUMN payments.status IS 'One of PENDING, PAID, CANCELLED or UNMATCHED. Changed only through the domain transitions (FR-011, research D6).';
COMMENT ON COLUMN payments.provider IS 'The provider name; payos.';
COMMENT ON COLUMN payments.provider_reference IS 'PayOS''s own transaction reference once it confirms. NULL until confirmed; the partial unique index makes it the idempotency key (FR-009, research D7).';
COMMENT ON COLUMN payments.payment_link_id IS 'PayOS''s paymentLinkId, the handle used to read and cancel the link.';
COMMENT ON COLUMN payments.checkout_url IS 'The PayOS checkout URL the customer opens.';
COMMENT ON COLUMN payments.qr_code IS 'The VietQR payload PayOS returns, for QR rendering.';
COMMENT ON COLUMN payments.unmatched_reason IS 'Why a verified confirmation could not be applied. NULL unless the status is UNMATCHED (FR-012, FR-013, research D8).';
COMMENT ON COLUMN payments.created_at IS 'Set once.';
COMMENT ON COLUMN payments.updated_at IS 'Touched on every write.';

-- FR-011, research D6: an unlisted state cannot be stored, so the transition table
-- cannot be bypassed by writing a value.
ALTER TABLE payments
    ADD CONSTRAINT payments_status_ck CHECK (
        status IN ('PENDING', 'PAID', 'CANCELLED', 'UNMATCHED')
    );
-- FR-004: the currency is an unambiguous three-letter code.
ALTER TABLE payments
    ADD CONSTRAINT payments_currency_ck CHECK (currency ~ '^[A-Z]{3}$');
-- FR-004, research D13: a payment is for a positive amount.
ALTER TABLE payments
    ADD CONSTRAINT payments_amount_ck CHECK (amount > 0);
-- FR-012: the version is the order's positive counter.
ALTER TABLE payments
    ADD CONSTRAINT payments_order_version_ck CHECK (order_version >= 1);
-- FR-012, FR-013: a payment is UNMATCHED iff it carries a reason — the operator's
-- queue needs the reason, and no other state may carry one.
ALTER TABLE payments
    ADD CONSTRAINT payments_unmatched_ck CHECK ((status = 'UNMATCHED') = (unmatched_reason IS NOT NULL));

-- FR-022, research D4: a collision must be retried by the module, not silently
-- stored twice; PayOS identifies by this code.
CREATE UNIQUE INDEX payments_order_code_key ON payments (order_code);
-- FR-009, research D7: the provider reference is the idempotency key. The index is
-- partial so several rows may hold no reference until one confirms.
CREATE UNIQUE INDEX payments_provider_reference_key
    ON payments (provider_reference) WHERE provider_reference IS NOT NULL;
-- FR-017: the customer reads their order's payments.
CREATE INDEX payments_order_idx ON payments (order_id, created_at, id);
-- FR-018: the operator's reconciliation queue filters by status.
CREATE INDEX payments_status_idx ON payments (status, created_at, id);

-- One row per decision the module made about a payment (data-model.md).
CREATE TABLE payment_events (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_id uuid NOT NULL,
    action     text NOT NULL,
    actor_id   uuid,
    detail     text,
    created_at timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE payment_events IS 'One row per decision the module made about a payment: created, applied, cancelled or recorded as unmatched (FR-022, research D8).';
COMMENT ON COLUMN payment_events.payment_id IS 'The payment this event belongs to. References payments(id) with ON DELETE CASCADE: a removed payment takes its history with it (FR-022).';
COMMENT ON COLUMN payment_events.action IS 'The module''s own audit action: PAYMENT_CREATED, PAYMENT_APPLIED, PAYMENT_CANCELLED or PAYMENT_UNMATCHED.';
COMMENT ON COLUMN payment_events.actor_id IS 'The acting account, when one acted. NULL for the provider''s webhook, which has no acting account (FR-019, research D8).';
COMMENT ON COLUMN payment_events.detail IS 'A short reason or the provider reference, for the audit trail.';
COMMENT ON COLUMN payment_events.created_at IS 'Set once. The history is read in created_at then id order.';

ALTER TABLE payment_events
    ADD CONSTRAINT payment_events_payment_fk FOREIGN KEY (payment_id)
    REFERENCES payments(id) ON DELETE CASCADE;
ALTER TABLE payment_events
    ADD CONSTRAINT payment_events_action_ck CHECK (
        action IN ('PAYMENT_CREATED', 'PAYMENT_APPLIED', 'PAYMENT_CANCELLED', 'PAYMENT_UNMATCHED')
    );
CREATE INDEX payment_events_payment_idx ON payment_events (payment_id, created_at, id);

-- +goose Down
DROP INDEX payment_events_payment_idx;
ALTER TABLE payment_events DROP CONSTRAINT payment_events_action_ck;
ALTER TABLE payment_events DROP CONSTRAINT payment_events_payment_fk;
DROP TABLE payment_events;

DROP INDEX payments_status_idx;
DROP INDEX payments_order_idx;
DROP INDEX payments_provider_reference_key;
DROP INDEX payments_order_code_key;
ALTER TABLE payments DROP CONSTRAINT payments_unmatched_ck;
ALTER TABLE payments DROP CONSTRAINT payments_order_version_ck;
ALTER TABLE payments DROP CONSTRAINT payments_amount_ck;
ALTER TABLE payments DROP CONSTRAINT payments_currency_ck;
ALTER TABLE payments DROP CONSTRAINT payments_status_ck;
DROP TABLE payments;
