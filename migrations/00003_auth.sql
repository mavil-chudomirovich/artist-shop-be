-- +goose Up
CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email         text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    role          text NOT NULL DEFAULT 'CUSTOMER' CHECK (role IN ('CUSTOMER', 'ADMIN')),
    status        text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'active', 'disabled')),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX users_role_idx ON users (role);
CREATE INDEX users_status_idx ON users (status);

CREATE TABLE sessions (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    refresh_token_hash text NOT NULL UNIQUE,
    expires_at         timestamptz NOT NULL,
    revoked_at         timestamptz,
    rotated_from       uuid REFERENCES sessions (id),
    user_agent         text,
    ip                 text,
    created_at         timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX sessions_user_idx ON sessions (user_id, expires_at);
CREATE INDEX sessions_rotated_from_idx ON sessions (rotated_from);

CREATE TABLE password_reset_requests (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash text NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX password_reset_user_idx ON password_reset_requests (user_id, created_at DESC);

-- +goose Down
DROP TABLE password_reset_requests;
DROP TABLE sessions;
DROP TABLE users;
