# Phase 1 Data Model: OTP-Gated Registration

This feature adds **no durable schema change**. The account store is the existing
`users` table; a self-registered account is inserted directly as `active` once its code
is confirmed. All new state is short-lived and lives in Redis, always with a TTL.

## PostgreSQL (existing — no migration)

### Entity: Account (`users`)

| Field | Type | Constraints | Notes |
|-------|------|-------------|-------|
| `id` | uuid | PK, default `gen_random_uuid()` | Subject of access tokens. |
| `email` | text | NOT NULL, UNIQUE | Stored normalized (trimmed, lowercased). |
| `username` | text | NULL, UNIQUE on `LOWER(username) WHERE NOT NULL` | Optional; uniqueness enforced by the DB. |
| `display_name` | text | NULL | Optional. |
| `password_hash` | text | NOT NULL | Argon2id encoded string; never logged. |
| `role` | text | NOT NULL, CHECK in (`CUSTOMER`,`ADMIN`) | `CUSTOMER` for this flow. |
| `status` | text | NOT NULL, CHECK in (`pending`,`active`,`disabled`) | **Inserted as `active`** by this flow (D6). |
| `created_at` | timestamptz | NOT NULL, default `now()` | |
| `updated_at` | timestamptz | NOT NULL, default `now()` | |

**Creation rule for this feature**: created only on a correct, unexpired code. The
`UNIQUE(email)` (and the unique lower-case username index) is the serialization point
that guarantees exactly one account under retry/concurrency (D5). On a unique violation
the account is looked up by the registration's email: present → the retry is a success;
absent → the violation is the username (`ErrUsernameTaken`, conflict), and the
temporary registration is kept.

## Redis (ephemeral — new keys)

All keys use the `auth:reg:` prefix and always carry a TTL.

| Key | Value | TTL | Purpose |
|-----|-------|-----|---------|
| `auth:reg:{id}` | hash: `email`, `username`, `displayName`, `passwordHash`, `createdAt`, `status` | `PENDING_REGISTRATION_TTL` | The temporary registration (never a plaintext password); `status` is `pending` or `completed`. |
| `auth:reg:email:{email}` | `registrationId` | `PENDING_REGISTRATION_TTL` | Pointer enforcing one live registration per email (FR-026). |
| `auth:reg:otp:{id}` | Argon2id hash of the 6-digit code | `OTP_TTL` | Active code challenge. |
| `auth:reg:otp:attempts:{id}` | integer | `OTP_TTL` | Failed confirmation count for the current code. |
| `auth:reg:otp:blocked:{id}` | `1` | `OTP_BLOCK_TTL` | Set when attempts ≥ `OTP_MAX_ATTEMPTS`. |
| `auth:reg:otp:sent:{id}` | `1` | `OTP_RESEND_COOLDOWN` | Resend cooldown marker. |

**Atomic replace (FR-026)**: starting a registration runs a Lua script on
`auth:reg:email:{email}` that deletes the previous registration's keys (if the pointer
resolves) and writes the new record and pointer, so a double-submit cannot leave two
live registrations or orphaned keys.

## Entity: PendingRegistration (domain model, not persisted durably)

| Field | Type | Notes |
|-------|------|-------|
| `RegistrationID` | string | Opaque, 32 random bytes base64url; lookup key (FR-005). |
| `Email` | string | Normalized before storage (FR-002). |
| `Username` | *string | Optional; checked for duplicates at start and again at creation (FR-023). |
| `DisplayName` | *string | Optional. |
| `PasswordHash` | string | Argon2id encoded; never plaintext (FR-019). |
| `CreatedAt` | time.Time | UTC. |
| `ExpiresAt` | time.Time | Derived from `PENDING_REGISTRATION_TTL` for reporting. |
| `Status` | string | `pending` or `completed`; `completed` records are retained (code-less) until TTL for idempotent replay. |

## Lifecycles

**Temporary registration** (`issued` → replaced / verified / cancelled / expired):
- `issued` — `Save` writes the record + pointer + code, arms the cooldown.
- `issued → replaced` — a new `Save` for the same email deletes the old keys and code
  (FR-026); the old reference and code stop working.
- `issued → verified` — correct code creates the account (active), then the code and the
  email pointer are removed and the record is **marked `completed`** and retained until
  TTL; a repeated confirmation of the same reference returns success without creating a
  second account (FR-010, FR-011).
- `issued → cancelled` — `Delete` removes the record, code and pointer (FR-017).
- `issued → expired` — keys vanish by TTL; verification then reports the generic
  not-found result (FR-007, FR-016).

**Code** (`issued` → consumed / expired / blocked):
- `issued` — `IssueOTP` sets the hash, resets the attempt counter, arms the cooldown.
- wrong code — `attempts` incremented; at `OTP_MAX_ATTEMPTS` the code is deleted and the
  block marker is set (FR-012, FR-013).
- correct code — attempt counter cleared; the record survives until account creation
  succeeds (FR-010).
- resend — a new `IssueOTP` replaces the hash, resets attempts, re-arms the cooldown; the
  old code is invalid (FR-015).
- expired — keys vanish by `OTP_TTL`; a resend is required (FR-007).

## Validation rules

- Email: normalized (trim + lowercase) before the duplicate check and before storage;
  uniqueness is case-insensitive by construction (FR-002).
- Password: existing policy (≥ 8 chars, letter + digit + special) checked at start, then
  hashed (FR-001, FR-019).
- Username: normalized (trim); checked against `users` at start and enforced by the DB
  unique index at creation (FR-023).
- Code: exactly 6 digits; stored only as an Argon2id hash; ≤ `OTP_MAX_ATTEMPTS` failures;
  blocked afterwards; `OTP_RESEND_COOLDOWN` between sends; single active code (FR-006,
  FR-008, FR-012–FR-015).
- Reference: 32 random bytes; never derived from the email (FR-005).
- No plaintext password, code, or reference-derivable secret is ever persisted or logged
  (FR-019).
- Unknown / expired / cancelled references return one generic error across verify, resend
  and cancel (FR-016).

## Relationships

```text
auth:reg:email:{email} 1──1 auth:reg:{id}          (Redis pointer; one live per email)
auth:reg:{id}         1──1 auth:reg:otp:{id}        (Redis; code challenge)
auth:reg:{id}         1──* auth:reg:otp:*:{id}      (attempts / blocked / sent markers)
auth:reg:{id}         ──▶  users (one account created on verification)
```

## Cross-module note

Other modules continue to reference `users.id` as an opaque identifier. This feature
does not change the auth module's published surface (`AuthService`, `internal/contracts`)
other than the registration use cases; profile data remains the `user` module's concern.
