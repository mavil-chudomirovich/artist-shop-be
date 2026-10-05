# Phase 1 Data Model: User Authentication & Session Management

The auth module persists durable state in PostgreSQL and keeps short-lived state
(OTP challenges, access-token blacklist) in Redis. Conventions follow the
foundation (UUID keys, `timestamptz` UTC, snake_case SQL).

## PostgreSQL tables

### Entity: Account (`users`)

| Field | Type | Constraints | Notes |
|-------|------|-------------|-------|
| `id` | uuid | PK, default `gen_random_uuid()` | Subject of access tokens. |
| `email` | text | NOT NULL, UNIQUE | Stored normalized (trimmed, lowercased). |
| `password_hash` | text | NOT NULL | Argon2id encoded string; never logged. |
| `role` | text | NOT NULL, CHECK in (`CUSTOMER`,`ADMIN`) | Default `CUSTOMER`. |
| `status` | text | NOT NULL, CHECK in (`pending`,`active`,`disabled`) | Default `pending`. |
| `created_at` | timestamptz | NOT NULL, default `now()` | |
| `updated_at` | timestamptz | NOT NULL, default `now()` | |

Indexes: UNIQUE(`email`), (`role`), (`status`).

**State transitions**
- `pending → active` — email OTP verified.
- `active → disabled` — operational admin action.
- `disabled → active` — operational admin action.
- Any other transition is rejected (typed domain error + test).

### Entity: Session (`sessions`)

| Field | Type | Constraints | Notes |
|-------|------|-------------|-------|
| `id` | uuid | PK, default `gen_random_uuid()` | |
| `user_id` | uuid | NOT NULL, FK `users(id)` ON DELETE CASCADE | |
| `refresh_token_hash` | text | NOT NULL, UNIQUE | SHA-256 (hex) of the opaque token. |
| `expires_at` | timestamptz | NOT NULL | 45 days from issuance (or the replaced session's expiry on rotation). |
| `revoked_at` | timestamptz | NULL | Set when rotated out or signed out. |
| `rotated_from` | uuid | NULL, FK `sessions(id)` | Rotation chain. |
| `user_agent` | text | NULL | Device hint (best effort). |
| `ip` | text | NULL | Source IP hint (best effort). |
| `created_at` | timestamptz | NOT NULL, default `now()` | |

Indexes: UNIQUE(`refresh_token_hash`), (`user_id`, `expires_at`), (`rotated_from`).

**Lifecycle**
- Issued on sign-in, registration confirmation, or password reset.
- `active → rotated` on refresh (`revoked_at` set; a child session is created).
- `active → revoked` on sign-out.
- `active → expired` when `expires_at` passes.
- Presenting a `rotated`/`revoked`/`expired` token is rejected; only the current
  token is accepted. Reuse does not revoke the family.

### Entity: PasswordResetRequest (`password_reset_requests`)

| Field | Type | Constraints | Notes |
|-------|------|-------------|-------|
| `id` | uuid | PK | |
| `user_id` | uuid | NOT NULL, FK `users(id)` ON DELETE CASCADE | |
| `token_hash` | text | NOT NULL, UNIQUE | SHA-256 (hex) of the emailed token. |
| `expires_at` | timestamptz | NOT NULL | 30 minutes. |
| `used_at` | timestamptz | NULL | Single-use. |
| `created_at` | timestamptz | NOT NULL, default `now()` | |

Indexes: UNIQUE(`token_hash`), (`user_id`, `created_at DESC`).

**Lifecycle**
- `open → used` when the reset completes.
- `open → expired` when the TTL passes.
- A new request invalidates earlier open requests for the same account.

## Redis keys (ephemeral)

All keys use the `auth:` prefix and always carry a TTL.

| Key | Value | TTL | Purpose |
|-----|-------|-----|---------|
| `auth:otp:{email}` | Argon2id hash of the 6-digit OTP | `OTP_TTL` | Active email-confirmation challenge. |
| `auth:otp:attempts:{email}` | integer | `OTP_TTL` | Failed verification count. |
| `auth:otp:blocked:{email}` | `1` | 1 minute | Set when attempts ≥ 3; blocks verification and resend. |
| `auth:otp:sent:{email}` | `1` | 1 minute | Resend cooldown marker. |
| `auth:blacklist:jti:{jti}` | `1` | Remaining access-token life | Targeted access-token revocation. |
| `auth:blacklist:user:{userID}` | unix seconds ("minimum valid `iat`") | Access-token TTL | Bulk access-token revocation on password change/reset. |
| `auth:login:failures:{source}` | integer | 15 minutes | Failed sign-in count per source (IP + account). |
| `auth:login:blocked:{source}` | `1` | 15 minutes | Set at 10 failures; blocks sign-in until it expires. |

**Login lockout lifecycle**
- Wrong credential: `failures` incremented; at 10 the `blocked` key is set for
  15 minutes and sign-in is refused with `AUTH_LOGIN_LOCKED`.
- Successful sign-in: both `failures` and `blocked` are deleted.

**OTP lifecycle**
- Issued: `otp` set (hash), `attempts = 0`, `sent` set.
- Wrong OTP: `attempts` incremented; at 3 the `otp` key is deleted and `blocked`
  is set for 1 minute.
- Correct OTP: `otp` deleted, account `pending → active` in PostgreSQL.
- Expired: keys vanish via TTL.
- Resend while `blocked` or `sent` is present is rejected.

## Relationships

```text
users 1─* sessions             (durable login sessions)
users 1─* password_reset_requests
sessions.rotated_from ─ sessions.id   (rotation chain)
users 1─1 auth:otp:{email}     (Redis, ephemeral)
users 1─* auth:blacklist:user:{id}, auth:blacklist:jti:{jti}  (Redis, ephemeral)
```

## Validation rules

- Email: normalized before insert; uniqueness is case-insensitive by construction.
- Password: ≥ 8 chars with a letter, a digit, and a special character (FR-003).
- OTP: exactly 6 digits; ≤ 3 failed attempts; blacklisted after 3; 1-minute resend
  cooldown; TTL `OTP_TTL`; single use.
- Refresh token: 32 random bytes; stored only as a hash; rotated on every use.
- No plaintext password, OTP, or refresh/reset token is ever persisted or logged.

## Cross-module note

Other modules reference `users.id` as an opaque identifier. Profile data belongs to
the future `user` module; auth owns only authentication and session concerns.
`internal/share/cache` is a platform addition introduced by this feature and is
reusable by later modules.
