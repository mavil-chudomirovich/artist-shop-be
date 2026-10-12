# Frontend Integration Guide: Password Reset via Emailed Code

**Feature**: `013-password-reset-otp` | **Base path**: `/api/v1`

This is the hand-off for the password-reset contract change. It enumerates every affected
endpoint with its method and path, marks it new/changed, and gives the behaviour
**before → after**. It agrees with
[`docs/api-reference.md`](../../docs/api-reference.md) (authoritative) and
[`contracts/openapi.yaml`](./contracts/openapi.yaml).

## Summary

| Method | Path | Status | Note |
|--------|------|--------|------|
| POST | `/api/v1/auth/password/forgot` | **Changed** | Now emails a one-time **code**, not a reset token. Response stays generic `202`. |
| POST | `/api/v1/auth/password/forgot/verify-otp` | **New** | Verify the code; returns a single-use **reset token**. |
| POST | `/api/v1/auth/password/reset` | **Changed** | Sets the new password; returns **success only (no session)** — the user returns to sign-in. |

Sign-in, refresh, logout, password change and all registration endpoints are **unchanged**.

## `POST /api/v1/auth/password/forgot`

**Before** → body `{ email }`; the email contained a reset token ("Use this token to reset
your password: …"); response `202` generic.

**After** → body `{ email }` (unchanged); the email now contains a **6-digit code**; response
`202` generic (unchanged):

```json
{ "data": { "message": "If the email is registered, a reset code has been sent." }, "meta": { "...": "..." } }
```

- New screen flow: after this call the client shows the **code entry** screen, not the
  password screen.
- The request is also the **resend**: repeat the same call after the cooldown.
- Same failure codes as before: `429 AUTH_RESEND_COOLDOWN`, `400 VALIDATION_ERROR`,
  `429 RATE_LIMITED`, `503 SERVICE_UNAVAILABLE` (nothing delivered; retry immediately).

## `POST /api/v1/auth/password/forgot/verify-otp` (new)

Body `{ email, otp }`; on success `200`:

```json
{ "data": { "resetToken": "opaque-single-use-token", "expiresIn": 1800 }, "meta": { "...": "..." } }
```

- Store `resetToken` client-side and switch to the **new-password** screen.
- The code is single-use; after a success it cannot be used again.
- Failures: `400 AUTH_OTP_INVALID` (wrong), `400 AUTH_OTP_EXPIRED` (expired / none active),
  `429 AUTH_OTP_TOO_MANY_ATTEMPTS` (attempt limit; request a new code), `400 VALIDATION_ERROR`.
- These responses do not reveal whether the email is registered.

## `POST /api/v1/auth/password/reset`

**Before** → body `{ token, newPassword }`; on success `200` returned a **token pair**
(`accessToken`, `refreshToken`, `expiresIn`) — the user was signed in automatically.

**After** → body `{ token, newPassword }` (unchanged); on success `200` returns **success
only**:

```json
{ "data": { "message": "Password reset." }, "meta": { "...": "..." } }
```

- **No session is returned.** On success, navigate the user to the **sign-in** screen; they
  log in with the new password.
- A successful reset ends every existing session of the account, so any tokens the client
  still holds are invalid — clear stored tokens.
- Failures: `400 AUTH_RESET_INVALID` (token invalid/used/expired/superseded), `400
  AUTH_WEAK_PASSWORD` (token stays usable, let the user retry), `429 RATE_LIMITED`.

## Error codes touched

| Code | HTTP | When |
|------|------|------|
| `AUTH_OTP_INVALID` | 400 | Wrong reset code on verify-otp. |
| `AUTH_OTP_EXPIRED` | 400 | No active / expired code on verify-otp (also returned when the address has no account, so existence is not revealed). |
| `AUTH_OTP_TOO_MANY_ATTEMPTS` | 429 | Code attempt limit reached. |
| `AUTH_RESEND_COOLDOWN` | 429 | New code requested too early. |
| `AUTH_RESET_INVALID` | 400 | Reset token invalid, used, expired, or superseded. |
| `AUTH_WEAK_PASSWORD` | 400 | New password fails the policy. |
| `SERVICE_UNAVAILABLE` | 503 | Reset code could not be delivered; a new request is allowed immediately. |

## Screen flow (target)

1. **Enter email** → `POST /password/forgot`.
2. **Enter code** → `POST /password/forgot/verify-otp` → keep `resetToken`.
3. **Enter new password + confirm** → `POST /password/reset`.
4. **Done** → navigate to **sign-in**; user signs in with the new password.
