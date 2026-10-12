# Quickstart & Validation: Password Reset via Emailed Code

This guide proves the feature end-to-end. It references
[data-model.md](./data-model.md) for key/TTL details and
[contracts/openapi.yaml](./contracts/openapi.yaml) for payload shapes.

## Prerequisites

- Docker stack up with Mailpit: `make up-tools` (this points the API at Mailpit for local
  email). Apply migrations: `make migrate-up-docker`.
- Read the code from Mailpit (`http://localhost:8025`) or the API log.
- Use an account whose email you can read. Seed one via registration if needed.

## Automated checks

```bash
make lint
make test
make test-integration      # end-to-end flow against PostgreSQL + Redis
make swagger               # regenerate docs/swagger after annotation changes
make swagger-check         # must pass (part of make check)
```

## Manual scenarios (curl)

Base URL `http://localhost:8080`; responses use the `data` + `meta` envelope.

### 1. Request a reset code (generic)

```bash
curl -s -X POST localhost:8080/api/v1/auth/password/forgot \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@example.com"}'
```

Expected: `202` with a generic message. The email (Mailpit) contains a 6-digit code, **not**
a token. An unregistered address returns the same `202` and sends nothing.

### 2. Verify the code and get a reset token

```bash
curl -s -X POST localhost:8080/api/v1/auth/password/forgot/verify-otp \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@example.com","otp":"<CODE>"}'
```

Expected: `200` with `data.resetToken` and `data.expiresIn`. The code is now unusable.
A wrong code returns `400 AUTH_OTP_INVALID`; the 5th wrong attempt returns
`429 AUTH_OTP_TOO_MANY_ATTEMPTS`.

### 3. Set the new password

```bash
curl -s -X POST localhost:8080/api/v1/auth/password/reset \
  -H 'Content-Type: application/json' \
  -d '{"token":"<resetToken>","newPassword":"NewStr0ng!Pass"}'
```

Expected: `200` success with **no** token pair. Reusing the same token returns
`400 AUTH_RESET_INVALID`; a weak password returns `400 AUTH_WEAK_PASSWORD` and the token
stays usable.

### 4. Sign in with the new password

```bash
curl -s -X POST localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@example.com","password":"NewStr0ng!Pass"}'
```

Expected: `200` with a token pair. Signing in with the **old** password fails
(`401 AUTH_INVALID_CREDENTIALS`). Any session issued before the reset is now rejected.

### 5. Cooldown and superseding

- Request a code, then request again immediately → `429 AUTH_RESEND_COOLDOWN`; after
  `OTP_RESEND_COOLDOWN` it succeeds.
- After issuing one reset token, request a new code and verify it → the earlier token is
  refused at reset (`400 AUTH_RESET_INVALID`).

## Expected outcomes (mapped to success criteria)

| Check | Expectation |
|-------|-------------|
| Request for unregistered address | Same `202` as a registered one; no email sent (SC-002). |
| Correct code | Exactly one reset token; wrong/expired/limit-exceeded codes yield none (SC-003). |
| Invalid/used/expired token | Password unchanged, `400 AUTH_RESET_INVALID` (SC-004). |
| After reset | All prior sessions rejected; new session only by signing in with the new password (SC-005). |
| Logs / Redis dump | No plaintext password or code/token (SC-006). |
| Repeat requests from one source | Throttled (SC-007). |
| Existing auth tests | Sign-in, registration, password change still pass (SC-008). |
