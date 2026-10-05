-- +goose Up
CREATE TABLE audit_logs (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id       uuid NOT NULL UNIQUE,
    actor_id       uuid,
    actor_role     text,
    action         text NOT NULL,
    target_type    text,
    target_id      text,
    outcome        text NOT NULL,
    metadata       jsonb NOT NULL DEFAULT '{}'::jsonb,
    correlation_id text,
    occurred_at    timestamptz NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_logs_actor_idx ON audit_logs (actor_id, occurred_at DESC);
CREATE INDEX audit_logs_action_idx ON audit_logs (action, occurred_at DESC);
CREATE INDEX audit_logs_target_idx ON audit_logs (target_type, target_id, occurred_at DESC);

-- +goose Down
DROP TABLE audit_logs;
