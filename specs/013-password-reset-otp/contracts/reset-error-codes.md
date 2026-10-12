# Password Reset Error Codes

Codes used by the password-reset flow; all already exist in the auth catalogue. No new code
is introduced. `docs/api-reference.md` §1.4 is authoritative and wins over this file.

| Code | HTTP | When |
|------|------|------|
| `VALIDATION_ERROR` | 400 | The email field is malformed on `forgot`/`verify-otp` (foundation code, with `details`). |
| `RATE_LIMITED` | 429 | The flow rate limit was exceeded (foundation code). |
| `AUTH_RESEND_COOLDOWN` | 429 | A new code was requested before the cooldown elapsed. |
| `SERVICE_UNAVAILABLE` | 503 | The reset code could not be delivered; nothing was sent and a new code can be requested immediately. |
| `AUTH_OTP_INVALID` | 400 | The reset code is wrong. |
| `AUTH_OTP_EXPIRED` | 400 | The reset code has expired, none is active, or the address has no account (a request for an unregistered address issues no code, so verification finds none); request a new code. |
| `AUTH_OTP_TOO_MANY_ATTEMPTS` | 429 | The code attempt limit was reached; request a new code. |
| `AUTH_RESET_INVALID` | 400 | The reset token is invalid, used, expired, or superseded. |
| `AUTH_WEAK_PASSWORD` | 400 | The new password fails the policy. |

## Notes

- `forgot` and `verify-otp` never reveal whether an address is registered: `forgot` always
  answers generically, and `verify-otp` failures name the code, not the address.
- A reset token that is valid but carries a password that fails the policy returns
  `AUTH_WEAK_PASSWORD` and the token stays usable (the user may retry).
- `verify-otp` on success returns the reset token; it is not a session and cannot authenticate
  any other request.
