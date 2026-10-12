# Phase 1 Data Model: Password Reset via Emailed Code

This feature adds **no durable schema change**. The reset token uses the existing
`password_reset_requests` table; the emailed code and its brute-force state live in Redis,
each key with a TTL.

## PostgreSQL (existing — no migration)

### Entity: Account (`users`)

Read-only in this feature: the reset flow loads an account by normalized email to decide
whether to send a code and to attach the reset token to it. Unchanged: `email` (unique,
normalized), `password_hash`, `role`, `status`. A successful reset updates `password_hash`.

### Entity: Reset Token (`password_reset_requests`)

| Field | Type | Constraints | Notes |
|-------|------|-------------|-------|
| `id` | uuid | PK | |
| `user_id` | uuid | NOT NULL, FK `users(id)` ON DELETE CASCADE | |
| `token_hash` | text | NOT NULL, UNIQUE | SHA-256 (hex) of the opaque token; created by `verify-otp`. |
| `expires_at` | timestamptz | NOT NULL | `PASSWORD_RESET_TTL` from issuance. |
| `used_at` | timestamptz | NULL | Single-use; set on reset. |
| `created_at` | timestamptz | NOT NULL, default `now()` | |

**Issuance**: created only after a code is verified. `Create` invalidates the account's
other open requests first, so a newer reset supersedes an older token (D11).

**Lifecycle**
- `open → used` — the reset completes.
- `open → expired` — the TTL passes.
- `open → superseded` — a newer reset for the same account is issued (`used_at` set).

## Redis (ephemeral — new keys)

All keys use the `auth:reset:otp:` prefix and always carry a TTL. Keyed by the normalized
email.

| Key | Value | TTL | Purpose |
|-----|-------|-----|---------|
| `auth:reset:otp:{email}` | Argon2id hash of the 6-digit code | `OTP_TTL` | Active reset code. |
| `auth:reset:otp:attempts:{email}` | integer | `OTP_TTL` | Failed verification count for the current code. |
| `auth:reset:otp:blocked:{email}` | `1` | `OTP_BLOCK_TTL` | Set when attempts ≥ `OTP_MAX_ATTEMPTS`. |
| `auth:reset:otp:sent:{email}` | `1` | `OTP_RESEND_COOLDOWN` | Send-cooldown marker (also the resend gate). |

**Code lifecycle**
- issued — `Issue` sets the hash, resets the attempt counter, clears any block, arms the cooldown.
- wrong code — attempt counter incremented; at `OTP_MAX_ATTEMPTS` the code is removed and the block marker set.
- correct code — `Verify` clears the attempt counter but keeps the code; the use case then
  issues the reset token and calls `Invalidate` to remove the code, so a failed token write
  can be retried with the same code (research D3).
- replaced — a new `Issue` overwrites the hash, so only the newest code works.
- expired — keys vanish by TTL; the user requests a new code.

## Entity: Reset Code (challenge, not durable)

| Field | Type | Notes |
|-------|------|-------|
| `Email` | string | Normalized; the store key. |
| `CodeHash` | string | Argon2id hash of the 6-digit code; never stored plaintext. |
| `Attempts` | int | Failed verifications for the current code. |
| `Blocked` | bool | True after too many attempts, until the block TTL. |
| `SentAt` | (marker TTL) | Presence is the cooldown gate. |

## Relationships

```text
users 1──* password_reset_requests   (durable reset tokens; single-use)
users 1──1 auth:reset:otp:{email}     (Redis; active reset code)
password_reset_requests.token_hash    (looked up at reset; SHA-256 of the emitted token)
```

## Validation rules

- Email: normalized (trim + lowercase) before the account lookup and before keying the code
  store; the request never reveals whether it is registered (FR-002/FR-009).
- Password: the existing policy (≥ 8 chars, letter + digit + special) is enforced at reset
  and at change; the reset token stays usable if the policy fails (FR-011).
- Code: exactly 6 digits; stored only as an Argon2id hash; ≤ `OTP_MAX_ATTEMPTS` failures;
  blocked afterwards; `OTP_RESEND_COOLDOWN` between sends; single active code.
- Reset token: 32 random bytes; stored only as a hash; single-use; refused if used, expired
  or superseded.
- No plaintext password, code or reset token is persisted or logged (FR-017).

## Cross-module note

Other modules reference `users.id` as an opaque identifier and are unaffected. This feature
does not change the auth module's published surface beyond the two reset use cases.
