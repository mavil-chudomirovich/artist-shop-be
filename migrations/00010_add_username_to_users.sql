-- +goose Up
-- Add optional username to users table
ALTER TABLE users ADD COLUMN username text;
CREATE UNIQUE INDEX users_username_lower_idx ON users (LOWER(username)) WHERE username IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS users_username_lower_idx;
ALTER TABLE users DROP COLUMN IF EXISTS username;
