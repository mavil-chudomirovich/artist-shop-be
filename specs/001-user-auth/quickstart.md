# Quickstart: User Authentication & Session Management

Runnable validation scenarios proving the auth module works end-to-end. This is a
validation/run guide, not an implementation specification.

## Prerequisites

- Module 00 (platform foundation) implemented.
- Go 1.26+ and Docker.
- A local PostgreSQL and Redis (e.g. `docker compose up -d db redis`). Redis is
  required for OTP storage and the access-token blacklist.

## Configuration (added by this module)

| Variable | Example | Purpose |
|----------|---------|---------|
| `JWT_SECRET` | `change-me-32-bytes-minimum-secret` | HS256 signing secret (≥ 32 bytes). |
| `ACCESS_TOKEN_TTL` | `15m` | Access token lifetime. |
| `REFRESH_TOKEN_TTL` | `1080h` | Refresh token lifetime (45 days). |
| `REDIS_ADDR` | `localhost:6379` | Redis address for OTP + blacklist. |
| `REDIS_PASSWORD` | _(empty)_ | Redis password, if any. |
| `REDIS_DB` | `0` | Redis database index. |
| `OTP_TTL` | `15m` | Email confirmation OTP lifetime. |
| `OTP_MAX_ATTEMPTS` | `3` | Failed OTP attempts before the OTP is blacklisted. |
| `OTP_BLOCK_TTL` | `60s` | Blacklist/cooldown after too many attempts. |
| `OTP_RESEND_COOLDOWN` | `60s` | Minimum interval between OTP resends. |
| `PASSWORD_RESET_TTL` | `30m` | Reset link lifetime. |
| `SMTP_HOST` / `SMTP_PORT` / `SMTP_USERNAME` / `SMTP_PASSWORD` / `SMTP_FROM` | — | SMTP delivery; when unset, emails are logged to stdout (dev). |
| `ADMIN_EMAIL` / `ADMIN_PASSWORD` | — | Used only by the `cmd/seed` command. |

## 1. Apply migrations and seed the admin

```powershell
go run ./cmd/migrate up

$env:ADMIN_EMAIL = "artist@example.com"
$env:ADMIN_PASSWORD = "Str0ng!Pass"
go run ./cmd/seed
```

## 2. Run the service

```powershell
$env:DATABASE_URL = "postgres://app:app@localhost:5432/artist_shop?sslmode=disable"
$env:JWT_SECRET = "dev-secret-at-least-32-bytes-long!!"
go run ./cmd/api
```

## 3. Registration + email confirmation

1. `POST /api/v1/auth/register` with `{ "email", "password" }` → `202` generic
   message. With the log sender, the OTP appears in the service output and in
   Redis under `auth:otp:{email}`.
2. Signing in before confirmation → `403 AUTH_ACCOUNT_PENDING`.
3. Entering a wrong OTP three times → `429 AUTH_OTP_TOO_MANY_ATTEMPTS`; the OTP is
   blacklisted and a resend is refused for 1 minute.
4. `POST /api/v1/auth/verify-email` with the correct `{ "email", "otp" }` → `200`.
5. `POST /api/v1/auth/login` → `200` with `accessToken` + `refreshToken`.
6. `GET /api/v1/auth/me` with `Authorization: Bearer <accessToken>` → the identity.

Expected: a new visitor can register and reach an authenticated area (SC-001,
SC-008).

## 4. Session rotation, reuse, and sign-out

1. `POST /api/v1/auth/refresh` with the current `refreshToken` → new token pair.
2. Reuse the **old** refresh token → `401 AUTH_REFRESH_REUSED`; the new one still
   works (no family revocation).
3. Sign in on a second device (second `login`) then `POST /api/v1/auth/logout`
   with one refresh token → `204`; the other device's session still works
   (FR-008, FR-009).
4. Reuse the signed-out refresh token → `401` (SC-005).

## 5. Password change and access-token blacklisting

1. While signed in, capture the current access token.
2. `POST /api/v1/auth/password/change` with `{ currentPassword, newPassword }`
   → new token pair; the replacement refresh token keeps the replaced session's
   expiry.
3. Reuse the **old** access token → `401` (blacklisted via
   `credentials_valid_after`).
4. The old refresh token no longer works; the new one does.

## 6. Password reset

1. `POST /api/v1/auth/password/forgot` with an email → `202` generic response
   (same for unknown emails).
2. Take the reset token from the service output (dev) and
   `POST /api/v1/auth/password/reset` with `{ token, newPassword }` → `200` with a
   new token pair; previously issued access tokens are rejected.
3. Reusing the reset token → `400 AUTH_RESET_INVALID`.

## 7. Authorization boundary

- An admin identity reaches admin-only capabilities (`middleware.RequireRole`),
  while a customer session is denied with `403`; denials are written to
  `audit_logs` (SC-007).

## 8. Tests

```powershell
go test ./...                        # unit + API tests
$env:INTEGRATION = "1"
go test -tags integration ./...      # repository + end-to-end auth flows
```

## References

- Endpoints: [contracts/openapi.yaml](./contracts/openapi.yaml)
- Error codes: [contracts/auth-error-codes.md](./contracts/auth-error-codes.md)
- Entities and state transitions: [data-model.md](./data-model.md)
