# Registration Error Codes

Additions/changes to the auth module catalogue for the OTP-gated registration flow.
`docs/api-reference.md` §1.4 is authoritative and wins over this file; codes are stable
and each maps to exactly one HTTP status.

| Code | HTTP | Meaning |
|------|------|---------|
| `AUTH_REGISTRATION_NOT_FOUND` | 400 | The registration reference is unknown, expired, or cancelled. Returned generically by verify, resend and cancel so it does not reveal whether a reference existed. |
| `AUTH_EMAIL_TAKEN` | 409 | The email already belongs to an account (returned by `register` only). |
| `AUTH_USERNAME_TAKEN` | 409 | The username is already taken (returned by `register`, and by `register/verify-otp` when another registration claimed the handle between start and confirmation). |
| `AUTH_WEAK_PASSWORD` | 400 | Password fails the policy. |
| `AUTH_OTP_INVALID` | 400 | The code is wrong. |
| `AUTH_OTP_EXPIRED` | 400 | The code has expired; a resend is required. |
| `AUTH_OTP_TOO_MANY_ATTEMPTS` | 429 | The code attempt limit was reached; a resend is required. |
| `AUTH_RESEND_COOLDOWN` | 429 | A resend was requested before the cooldown elapsed. |
| `VALIDATION_ERROR` | 400 | A field failed validation (foundation code, with `details`). |
| `MALFORMED_REQUEST` | 400 | The JSON body could not be parsed (foundation code). |
| `RATE_LIMITED` | 429 | The flow rate limit was exceeded (foundation code). |
| `SERVICE_UNAVAILABLE` | 503 | The confirmation code could not be delivered; nothing was sent and a new code can be requested. |

## Notes

- `register` validates the **email format** before any temporary or durable write: a
  malformed address returns the shared `400 VALIDATION_ERROR` with `details[].field` =
  `"email"`, and leaves no temporary registration and sends no code (FR-001). No
  auth-specific code is added for this case.
- The code, its lifetime, the attempt counter, the block marker and the cooldown are all
  scoped to the `registrationId`, so the existing `AUTH_OTP_*` and `AUTH_RESEND_COOLDOWN`
  semantics are unchanged; only the key changes.
- `AUTH_ACCOUNT_PENDING` remains in the catalogue for other flows but is no longer
  produced by self-registration (accounts are created `active`).
- `register` returning `409 AUTH_EMAIL_TAKEN` / `AUTH_USERNAME_TAKEN` supersedes the
  earlier "always generic `202`" claim in `specs/001-user-auth/contracts/README.md`; the
  authoritative `docs/api-reference.md` is updated in this change.
