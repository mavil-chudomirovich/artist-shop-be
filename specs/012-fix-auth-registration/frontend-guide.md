# Frontend Integration Guide: OTP-Gated Registration

**Feature**: `012-fix-auth-registration` | **Base path**: `/api/v1`

This is the hand-off for the registration/verification contract change. It enumerates
every affected endpoint with its method and path, marks it new/changed/removed, and
gives the behaviour **before → after**. It agrees with
[`docs/api-reference.md`](../../docs/api-reference.md) (authoritative) and
[`contracts/openapi.yaml`](./contracts/openapi.yaml).

## Summary

| Method | Path | Status | Note |
|--------|------|--------|------|
| POST | `/api/v1/auth/register` | **Changed** | Response now carries a `registrationId`; no account is created yet; a taken email/username now yields `409`. |
| POST | `/api/v1/auth/verify-email` | **Removed** | Replaced by `register/verify-otp`. |
| POST | `/api/v1/auth/resend-verification` | **Removed** | Replaced by `register/resend-otp`. |
| POST | `/api/v1/auth/register/verify-otp` | **New** | Confirms the code and creates the account. |
| POST | `/api/v1/auth/register/resend-otp` | **New** | Replaces the code. |
| POST | `/api/v1/auth/register/cancel` | **New** | Discards a temporary registration. |

Login, refresh, logout, password reset/change, `/me` and the admin probe are
**unchanged**.

## `POST /api/v1/auth/register`

**Before** → a generic `202` message was returned for every request; a `pending`
account row was created immediately; the OTP was keyed by the submitted email.

**After** → `202` with the registration reference:

```json
{
  "data": {
    "registrationId": "opaque-random-string",
    "status": "PendingVerification",
    "otpExpiresIn": 300,
    "resendAvailableIn": 60
  },
  "meta": { "requestId": "...", "timestamp": "..." }
}
```

- **No account is created** at this step. Nothing to clean up if the user abandons.
- Request body is unchanged: `{ email, username?, displayName?, password }`.
- **New failure**: `409 AUTH_EMAIL_TAKEN` when the email already has an account, and
  `409 AUTH_USERNAME_TAKEN` when the username is taken. (Previously the claim was that
  `register` was always generic; it now intentionally reveals a duplicate so the user can
  act immediately.)
- **New failure**: `400 VALIDATION_ERROR` with `error.details[].field` = `"email"` when the
  email is malformed; validated before anything is stored, so no temporary registration
  is created and no code is sent.
- The client must store `registrationId` and send it — not the email — on the next calls.

## `POST /api/v1/auth/verify-email` → `POST /api/v1/auth/register/verify-otp`

**Before** → body `{ email, otp }`; on success the pending account became active.

**After** → body `{ registrationId, otp }`; on success the account is created and usable:

```json
{ "data": { "message": "Registration confirmed." }, "meta": { "...": "..." } }
```

- **No tokens are returned** (unchanged behaviour): the client then calls
  `/api/v1/auth/login`.
- Failure codes: `400 AUTH_OTP_INVALID`, `400 AUTH_OTP_EXPIRED`,
  `429 AUTH_OTP_TOO_MANY_ATTEMPTS`, `400 AUTH_REGISTRATION_NOT_FOUND` for an
  unknown/expired/cancelled reference, and `409 AUTH_USERNAME_TAKEN` if another
  registration claimed the username in the meantime (no account created).
- Repeating a successful confirmation is safe (idempotent) — no duplicate account.

## `POST /api/v1/auth/resend-verification` → `POST /api/v1/auth/register/resend-otp`

**Before** → body `{ email }`; generic response.

**After** → body `{ registrationId }`; `202` on success. The previous code is
invalidated and the attempt counter reset.

- `429 AUTH_RESEND_COOLDOWN` while the cooldown is active (see `resendAvailableIn`).
- Unknown/expired/cancelled reference: the same generic response (does not reveal
  whether the reference existed).

## `POST /api/v1/auth/register/cancel` (new)

Body `{ registrationId }`; `200` on success (idempotent — an unknown reference also
returns success). Removes the temporary registration and invalidates its code.

## Error codes touched

| Code | HTTP | When |
|------|------|------|
| `AUTH_REGISTRATION_NOT_FOUND` | 400 | Unknown/expired/cancelled reference on verify, resend or cancel. |
| `AUTH_EMAIL_TAKEN` | 409 | `register` with an email that already has an account. |
| `AUTH_USERNAME_TAKEN` | 409 | `register` with a taken username, or `register/verify-otp` if another registration claimed the handle meanwhile. |
| `AUTH_OTP_INVALID` / `AUTH_OTP_EXPIRED` | 400 | Wrong / expired code on verify. |
| `AUTH_OTP_TOO_MANY_ATTEMPTS` | 429 | Code attempt limit reached. |
| `AUTH_RESEND_COOLDOWN` | 429 | Resend requested too early. |
| `SERVICE_UNAVAILABLE` | 503 | Confirmation email could not be delivered; a resend is allowed immediately. |
