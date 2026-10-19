# Auth Module

Authentication and session management, organized with the project's Clean
Architecture (see `docs/architecture.md`).

## Structure

```
domain/
├── model/        # Account, Session, ResetRequest + password policy
├── constant/     # Status, error codes, audit actions
├── error/        # business sentinel errors
└── repository/   # UserRepository, SessionRepository, ResetRepository
application/
├── interface/    # AuthService + external-service ports + UnitOfWork
├── implement/    # use cases
├── dto/          # input/output DTOs
└── mapper/       # model ↔ dto
infrastructure/
└── implement/    # postgres, redis, token, email, auditor
presentation/
├── http/         # handlers, router, error mapping, AuthHooks
├── cli/          # seed command
├── worker/       # placeholder
└── dto/          # HTTP request/response DTOs
```

## Registration flow (deferred account)

Registration is **OTP-gated**: starting a sign-up creates **no durable account**.
The submitted details are held in the shared cache as a temporary registration
keyed by an unpredictable `registrationId`, and a one-time code is emailed. Only
when the code is confirmed is an active CUSTOMER account created. An abandoned or
failed sign-up therefore leaves nothing durable behind.

The flow is four endpoints under `/api/v1/auth`:

| Method | Path | Result |
|--------|------|--------|
| POST | `/auth/register` | Validates, rejects a taken email/username (`409`), stores a temporary registration and emails a code; returns `202` with `registrationId`, `otpExpiresIn` and `resendAvailableIn`. |
| POST | `/auth/register/verify-otp` | Confirms `{ registrationId, otp }`; creates exactly one active account (idempotent on replay) and returns `200`. No session is issued — the client then signs in. |
| POST | `/auth/register/resend-otp` | Replaces the code after `OTP_RESEND_COOLDOWN` (`429` while it is active); returns `202`. |
| POST | `/auth/register/cancel` | Discards the temporary registration (idempotent); returns `200`. |

The temporary registration and its code expire on their own lifetimes
(`PENDING_REGISTRATION_TTL`, `OTP_TTL`). Unknown, expired or cancelled references
stay generic (`AUTH_REGISTRATION_NOT_FOUND`). The former email-keyed
`POST /auth/verify-email` and `POST /auth/resend-verification` were removed.

## Password-reset flow (code-verified)

Password reset is **code-gated**: requesting a reset emails a short one-time
**code**, and the reset token is issued only after that code is verified. The
email never carries the reset token. Completing the reset reports success **only**
— it does **not** sign the user in; the client returns to the sign-in screen.

The flow is three endpoints under `/api/v1/auth`:

| Method | Path | Result |
|--------|------|--------|
| POST | `/auth/password/forgot` | If the address is registered, emails a one-time code and returns a generic `202`. Refused with `429 AUTH_RESEND_COOLDOWN` during the resend waiting period; `503` when the mail could not be delivered (the cooldown is freed so the customer can retry immediately). |
| POST | `/auth/password/forgot/verify-otp` | Verifies `{ email, otp }` and, on success, returns a **single-use reset token** with its lifetime (`200`). The code is then dead. |
| POST | `/auth/password/reset` | Sets the new password from `{ token, newPassword }`, ends every session of the account, rejects prior access tokens and returns `200` with a success message. **No** token pair. |

The code and its attempt/block/cooldown state live in the shared cache keyed by
the normalized address (keys `auth:reset:otp:{email}`, `:attempts:`, `:blocked:`,
`:sent:`); the reset token keeps the durable `password_reset_requests` record
(`PASSWORD_RESET_TTL`). The request step is also the resend — there is no separate
resend endpoint. A newer reset supersedes an earlier open one. Relevant success and
failure events are audited (`AUTH_PASSWORD_RESET_REQUESTED`, `AUTH_PASSWORD_RESET_CODE_VERIFIED`,
`AUTH_PASSWORD_RESET_CODE_FAILED`, `AUTH_PASSWORD_RESET_DELIVERY_FAILED`,
`AUTH_PASSWORD_RESET`).

## Where the behavior is documented

Do not duplicate it here — these files are authoritative:

- Endpoints, error codes, rate limits, token lifecycle:
  [`docs/api-reference.md`](../../../docs/api-reference.md) (§1 conventions, §3 auth)
- Layer responsibilities and dependency rules: [`docs/architecture.md`](../../../docs/architecture.md)
- Module scope and completion criteria: [`docs/modules/01-auth.md`](../../../docs/modules/01-auth.md)
- Requirements and clarifications: [`specs/001-user-auth/spec.md`](../../../specs/001-user-auth/spec.md),
  the registration rework in [`specs/012-fix-auth-registration/spec.md`](../../../specs/012-fix-auth-registration/spec.md)
  and the code-verified reset in [`specs/013-password-reset-otp/spec.md`](../../../specs/013-password-reset-otp/spec.md)

## Commands

Use the Makefile targets rather than raw `go test`
(see [`docs/makefile.md`](../../../docs/makefile.md)):

```sh
ADMIN_PASSWORD='<mật-khẩu-mạnh>' make seed-hash  # in hash Argon2id (dev-only)
make seed-sql    # provision the admin account (scripts/seed_admin.sql)
make test        # unit + HTTP tests
make test-integration  # repository/HTTP tests against Docker
```
