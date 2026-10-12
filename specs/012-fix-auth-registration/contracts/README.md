# Contracts: OTP-Gated Registration

These contracts cover only the registration/verification endpoints changed by this
feature. They follow the foundation HTTP conventions and the auth module's existing
envelope and error format. `docs/api-reference.md` is the authoritative cross-module
reference and wins if this file disagrees; `specs/001-user-auth/contracts/openapi.yaml`
remains the reference for the unchanged auth endpoints (login, refresh, logout,
password flows, `/me`, admin probe).

| File | Purpose |
|------|---------|
| [openapi.yaml](./openapi.yaml) | The four registration endpoints and their schemas |
| [registration-error-codes.md](./registration-error-codes.md) | Error codes used by the flow |

## Endpoint summary

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| POST | `/api/v1/auth/register` | none | Validate, store a temporary registration, email a code; returns the reference + timings |
| POST | `/api/v1/auth/register/verify-otp` | none | Confirm the code and create the account |
| POST | `/api/v1/auth/register/resend-otp` | none | Replace the code after the cooldown |
| POST | `/api/v1/auth/register/cancel` | none | Remove a temporary registration |

## Removed endpoints (breaking)

| Method | Path | Replacement |
|--------|------|-------------|
| POST | `/api/v1/auth/verify-email` | `/api/v1/auth/register/verify-otp` (now keyed by `registrationId`) |
| POST | `/api/v1/auth/resend-verification` | `/api/v1/auth/register/resend-otp` (now keyed by `registrationId`) |

## Flow notes

- The client keeps the `registrationId` returned by `register` and sends it — not an
  email — on verify, resend and cancel.
- Verify returns only a success result; the client then calls `/auth/login` (no
  auto-login).
- Unknown, expired or cancelled references return one generic `AUTH_REGISTRATION_NOT_FOUND`
  for verify, resend and cancel; only `register` distinguishes a taken email/username
  (`409`).
- Resend and cancel share the flow rate limit (`AUTH_FLOW_RATE_PER_MINUTE`).
