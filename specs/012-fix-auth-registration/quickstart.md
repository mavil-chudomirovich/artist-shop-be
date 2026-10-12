# Quickstart & Validation: OTP-Gated Registration

This guide proves the feature end-to-end. It references [data-model.md](./data-model.md)
for key/TTL details and [contracts/openapi.yaml](./contracts/openapi.yaml) for payload
shapes; it does not restate them.

## Prerequisites

- Docker stack up: `make up` (PostgreSQL + Redis + API). Copy env first if needed:
  `make env`.
- Mail: run with the dev override `docker compose -f docker-compose.yml -f
  docker-compose.dev.yml up` so Mailpit captures the OTP, or read the OTP from the
  log-only sender in local runs. Never expect the OTP in the API response.
- Migrations applied (no new migration for this feature): `make migrate-up`.

## Automated checks

```bash
make lint                     # format + tidy + vet + golangci-lint
make test                     # unit tests (use cases, Redis adapter)
make test-integration         # end-to-end flow against PostgreSQL + Redis (build tag integration)
make swagger                  # regenerate docs/swagger/ after annotation changes
make swagger-check            # must pass (part of make check)
```

## Manual scenarios (curl)

Base URL `http://localhost:8080`. All responses use the `data` + `meta` envelope.

### 1. Start a registration (no account created)

```bash
curl -s -X POST localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"newuser@example.com","password":"Str0ng!pass"}'
```

Expected: `202` with `data.registrationId` (opaque), `status=PendingVerification`,
`otpExpiresIn`, `resendAvailableIn`. Then confirm no durable account exists yet:

```bash
# via psql
SELECT count(*) FROM users WHERE email = 'newuser@example.com';  -- 0
```

### 2. Verify with a wrong code

```bash
curl -s -X POST localhost:8080/api/v1/auth/register/verify-otp \
  -H 'Content-Type: application/json' \
  -d '{"registrationId":"<ID>","otp":"000000"}'
```

Expected: `400 AUTH_OTP_INVALID`; no account created; the registration still verifies
with the correct code afterwards.

### 3. Verify with the correct code

Read the code from Mailpit / the local log, then:

```bash
curl -s -X POST localhost:8080/api/v1/auth/register/verify-otp \
  -H 'Content-Type: application/json' \
  -d '{"registrationId":"<ID>","otp":"<CODE>"}'
```

Expected: `200` success (no tokens). A `users` row now exists with `status='active'`.
Repeating the same call returns success and creates **no** second account.

### 4. Sign in with the new account

```bash
curl -s -X POST localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"newuser@example.com","password":"Str0ng!pass"}'
```

Expected: `200` with an access/refresh token pair (unchanged flow).

### 5. Resend, cooldown, and cancel

```bash
# immediately after start/verify: cooldown still active
curl -s -X POST localhost:8080/api/v1/auth/register/resend-otp \
  -H 'Content-Type: application/json' -d '{"registrationId":"<ID>"}'
# expected: 429 AUTH_RESEND_COOLDOWN

# after the cooldown: old code is invalidated, a new one is sent
curl -s -X POST localhost:8080/api/v1/auth/register/resend-otp \
  -H 'Content-Type: application/json' -d '{"registrationId":"<ID>"}'
# expected: 202

# cancel with a fresh registration
curl -s -X POST localhost:8080/api/v1/auth/register/cancel \
  -H 'Content-Type: application/json' -d '{"registrationId":"<OTHER_ID>"}'
# expected: 200; the reference and code are now unusable
```

### 6. Replacement and generic references

- Start two registrations for the **same** email; only the newest reference and code
  work — verifying with the older reference returns `400 AUTH_REGISTRATION_NOT_FOUND`.
- Verify/resend/cancel with a random reference returns `400
  AUTH_REGISTRATION_NOT_FOUND` (resend/cancel keep the same generic answer).

### 7. Attempt limit

Submit a wrong code `OTP_MAX_ATTEMPTS` times; the next attempt returns `429
AUTH_OTP_TOO_MANY_ATTEMPTS` until a resend issues a new code.

## Expected outcomes (mapped to success criteria)

| Check | Expectation |
|-------|-------------|
| Start a registration, never verify | After `PENDING_REGISTRATION_TTL`, the reference is gone and no `users` row exists (SC-001, SC-008). |
| Correct code | Exactly one active account; replaying is a no-op success (SC-002). |
| Wrong / expired / blocked code | No account; correct error code (SC-003). |
| Resend timing | `429` before the cooldown, `202` after (SC-005). |
| Concurrent verify of one reference | At most one `users` row (SC-007). |
| Logs / Redis dump | No plaintext password or code (SC-006). |
| Existing auth tests | Sign-in, refresh, logout, reset, change all still pass (SC-009). |
