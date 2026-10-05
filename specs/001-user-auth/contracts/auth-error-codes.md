# Auth Error Codes

Module-specific additions to the foundation catalogue documented in
[`docs/api-reference.md`](../../../docs/api-reference.md) §1.4 (which wins over this
file). Codes are stable and each maps to exactly one HTTP status.

| Code | HTTP | Meaning |
|------|------|---------|
| `AUTH_WEAK_PASSWORD` | 400 | Password fails the policy (min 8, letter, digit, special). |
| `AUTH_INVALID_CREDENTIALS` | 401 | Email or password is incorrect (generic, anti-enumeration). |
| `AUTH_ACCOUNT_PENDING` | 403 | Email not confirmed yet. |
| `AUTH_ACCOUNT_DISABLED` | 403 | Account is disabled. |
| `AUTH_OTP_INVALID` | 400 | OTP is wrong or already consumed. |
| `AUTH_OTP_EXPIRED` | 400 | OTP has expired. |
| `AUTH_OTP_TOO_MANY_ATTEMPTS` | 429 | OTP attempt limit exceeded. |
| `AUTH_RESEND_COOLDOWN` | 429 | Resend requested before the cooldown elapsed. |
| `AUTH_LOGIN_LOCKED` | 429 | Too many failed sign-in attempts from this source; try again later. |
| `AUTH_TOKEN_INVALID` | 401 | Access or refresh token is malformed, unknown, or revoked. |
| `AUTH_TOKEN_EXPIRED` | 401 | Access or refresh token has expired. |
| `AUTH_REFRESH_REUSED` | 401 | A rotated-out or revoked refresh token was presented; every session of the account is revoked. |
| `AUTH_RESET_INVALID` | 400 | Password reset token is invalid, used, or expired. |

## Notes

- `AUTH_INVALID_CREDENTIALS` MUST be returned for both unknown email and wrong
  password so responses do not reveal account existence.
- `AUTH_ACCOUNT_PENDING` is only returned after credentials are verified, so it
  does not leak whether an email exists.
- Registration never returns an "email already taken" code: `POST /register` and
  `POST /resend-verification` always answer with the same generic `202`, so the
  existence of an email cannot be probed.
- Privilege denials use the foundation `FORBIDDEN` 403 (not an auth-specific code)
  and MUST also be written to the audit log as `AUTH_PRIVILEGE_DENIED`.
