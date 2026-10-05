# Contracts: User Authentication & Session Management

Auth endpoints under `/api/v1/auth` follow the foundation HTTP conventions
(`specs/002-cross-cutting-foundation/contracts/http-conventions.md`) and reuse the
foundation response envelope and error format.

| File | Purpose |
|------|---------|
| [openapi.yaml](./openapi.yaml) | Endpoint definitions, request/response schemas |
| [auth-error-codes.md](./auth-error-codes.md) | Module-specific error codes added to the catalogue |

## Endpoint summary

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| POST | `/api/v1/auth/register` | none | Start registration; sends email OTP |
| POST | `/api/v1/auth/verify-email` | none | Confirm OTP; activates account |
| POST | `/api/v1/auth/resend-verification` | none | Resend OTP (rate-limited) |
| POST | `/api/v1/auth/login` | none | Authenticate; issue tokens |
| POST | `/api/v1/auth/refresh` | refresh token | Rotate session; issue new tokens |
| POST | `/api/v1/auth/logout` | refresh token | Revoke the current session |
| POST | `/api/v1/auth/password/forgot` | none | Request password reset link |
| POST | `/api/v1/auth/password/reset` | none | Complete reset with token |
| POST | `/api/v1/auth/password/change` | access token | Change password while signed in; revokes every session |
| GET | `/api/v1/auth/me` | access token | Return the current identity |
| GET | `/api/v1/auth/admin/probe` | ADMIN access token | RBAC probe; denies and audits non-admin |

Anti-enumeration: `register`, `resend-verification`, and `password/forgot` return
the same generic response whether or not the email exists.
