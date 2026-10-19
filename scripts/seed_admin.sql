-- Seed the single admin account (manual, idempotent).
--
-- This file is a TEMPLATE: it holds no credentials. The two psql variables
-- below come from a git-ignored sibling file that you create once per
-- environment and never commit:
--
--   scripts/seed.vars.local.psql
--     \set admin_email 'admin@artist-shop.local'
--     \set admin_hash '$argon2id$v=19$m=65536,t=1,p=4$...$...'
--
-- Workflow:
--   1. Pick a password satisfying the policy (8+ chars, a letter, a digit,
--      a special character).
--   2. Hash it:  ADMIN_PASSWORD='<password>' make seed-hash
--      (reads ADMIN_PASSWORD from the environment, prints only the hash).
--   3. Fill scripts/seed.vars.local.psql with the email and the hash.
--   4. Run:  make seed-sql
--
-- Rotating the password = repeat steps 1-4; the upsert below updates the
-- stored hash. Never commit a real email/hash: scripts/*.local.psql is
-- git-ignored and excluded from the Docker build context.
--
-- What this does (mirrors the former cmd/seed use case ProvisionAdmin):
--   1. Upserts users(email) to role ADMIN / status active.
--   2. Appends one AUTH_ADMIN_PROVISIONED row to audit_logs (the table is
--      append-only: UPDATE/DELETE are blocked, INSERT stays allowed).
--
-- Implementation note: the work lives in a throwaway pg_temp function whose
-- arguments are filled by psql substitution at the top-level SELECT call.
-- psql never interpolates :'vars' inside a DO $$ body (it scans the block as
-- one string literal), so inlining the variables there would reach the server
-- unsubstituted and fail with a syntax error near ":".

CREATE OR REPLACE FUNCTION pg_temp.seed_admin(p_email text, p_hash text)
RETURNS void LANGUAGE plpgsql AS $seed_admin_body$
BEGIN
    IF trim(p_email) = '' THEN
        RAISE EXCEPTION 'admin_email is empty - fill scripts/seed.vars.local.psql (see header)';
    END IF;
    IF p_hash NOT LIKE '$argon2id$%' THEN
        RAISE EXCEPTION 'admin_hash must be an $argon2id$ hash from `make seed-hash` (see header)';
    END IF;

    -- Upsert the admin account (mirrors UserRepository.UpsertAdmin).
    INSERT INTO users (id, email, password_hash, role, status)
    VALUES (
        gen_random_uuid(),
        lower(trim(p_email)),
        p_hash,
        'ADMIN',
        'active'
    )
    ON CONFLICT (email) DO UPDATE
    SET password_hash = EXCLUDED.password_hash,
        role = 'ADMIN',
        status = 'active',
        updated_at = now();

    -- Record the provisioning (mirrors the audit writer row shape).
    INSERT INTO audit_logs (
        event_id, actor_id, actor_role, action, target_type, target_id,
        outcome, metadata, correlation_id, occurred_at
    ) VALUES (
        gen_random_uuid(),
        NULL,
        'ADMIN',
        'AUTH_ADMIN_PROVISIONED',
        'user',
        lower(trim(p_email)),
        'SUCCESS',
        '{}'::jsonb,
        NULL,
        now()
    )
    ON CONFLICT (event_id) DO NOTHING;
END
$seed_admin_body$;

SELECT pg_temp.seed_admin(:'admin_email', :'admin_hash');
