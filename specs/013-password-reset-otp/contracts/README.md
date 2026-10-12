# Contracts: Password Reset via Emailed Code

These contracts cover only the password-reset endpoints changed by this feature. They follow
the foundation HTTP conventions and the auth module's existing envelope and error format.
`docs/api-reference.md` is the authoritative cross-module reference and wins if this file
disagrees.

| File | Purpose |
|------|---------|
| [openapi.yaml](./openapi.yaml) | The three password-reset endpoints and their schemas |
| [reset-error-codes.md](./reset-error-codes.md) | Error codes used by the flow |

## Endpoint summary

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| POST | `/api/v1/auth/password/forgot` | none | Email a one-time reset code (generic response) |
| POST | `/api/v1/auth/password/forgot/verify-otp` | none | Verify the code and receive a single-use reset token |
| POST | `/api/v1/auth/password/reset` | none | Set the new password with the reset token |

## Flow notes

- The request step is also the resend: repeating it before `OTP_RESEND_COOLDOWN` is refused.
- The reset token is issued **only** by `verify-otp` and is never emailed; the email carries
  only the code.
- `reset` returns success only and does **not** issue a session; the client returns to
  sign-in. A successful reset ends every existing session of the account.
- The token is single-use and superseded when a newer reset is issued for the same account.
- The request and verification steps share the flow rate limit (`AUTH_FLOW_RATE_PER_MINUTE`).
