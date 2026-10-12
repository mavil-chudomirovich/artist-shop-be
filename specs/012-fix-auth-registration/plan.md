# Implementation Plan: OTP-Gated Registration (No Account Before Verification)

**Branch**: `012-fix-auth-registration` | **Date**: 2026-10-12 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/012-fix-auth-registration/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command; its definition describes the execution workflow.

## Summary

Rework the auth module's registration so a sign-up creates **no durable account**
until its emailed one-time code is confirmed. Starting a registration validates the
input, rejects an email or username already taken, and writes a **temporary
registration** to Redis under an unpredictable `registrationId`, then emails a code.
Confirming the code loads the temporary registration, verifies the code (reusing the
existing hashed-OTP / attempt / cooldown machinery, re-keyed by reference), creates
the account in PostgreSQL as an active customer, and **only then** deletes the
temporary registration. New registrations for the same email replace the previous
temporary one; unknown/expired/cancelled references stay generic; resend replaces the
code; cancel removes it. A failure creating the account keeps the temporary
registration so confirmation can be retried idempotently, and the database's unique
constraints guarantee a single account under concurrency.

This is a contract-changing rework of the existing `/auth/verify-email` and
`/auth/resend-verification` endpoints and of the `/auth/register` payload. It **changes**
`POST /auth/register` to return a `registrationId` and create no account, **removes**
`/auth/verify-email` and `/auth/resend-verification`, and **adds**
`/auth/register/verify-otp`, `/auth/register/resend-otp`, and `/auth/register/cancel`.
It therefore also ships a `frontend-guide.md`, updated Swagger annotations, and an updated
`docs/api-reference.md` (Constitution VIII and the frontend-guide rule).

## Technical Context

**Language/Version**: Go 1.26 (module `github.com/mavil-chudomirovich/artist-shop-be`)

**Primary Dependencies**: existing platform foundation (`config`, `database`,
`httpx`, `middleware`, `audit`, `reqctx`, `logging`); `internal/share/cache`
(`github.com/redis/go-redis/v9`) with `Cache.Client()` for hash/Lua operations;
`golang.org/x/crypto/argon2` (password + OTP hashing); `github.com/google/uuid`;
hand-written SQL over `pgx`. No new third-party dependency.

**Storage**: PostgreSQL — the existing `users` table only; accounts are now inserted
**directly as `active`** (email already verified), so **no migration is required**.
Redis (shared cache) — the new temporary registration record and its pointer key, the
hashed OTP, the attempt counter, the block marker and the resend cooldown, all keyed
by `registrationId` and always carrying a TTL.

**Testing**: `go test`; unit tests for the use cases (start/verify/resend/cancel,
idempotent retry, attempt limits) and the Redis adapter; integration tests
(build tag `integration`, testcontainers) for the end-to-end flow against real
PostgreSQL + Redis; the existing fake email sender for delivery.

**Target Platform**: Linux server via Docker (same single binary as the foundation).

**Project Type**: web-service — modular monolith; this feature is one module.

**Performance Goals**: start/verify/resend/cancel p95 < 300 ms locally, excluding mail
delivery (which keeps the existing ~2 s response target and retry budget).

**Constraints**: single deployable; Redis is ephemeral (losing it at worst forces a
visitor to restart registration; it can never create a partial account); durable
uniqueness of email and username is enforced by the existing PostgreSQL constraints;
the temporary registration can never hold plaintext passwords or plaintext codes;
every temporary key must carry a TTL.

**Scale/Scope**: single-artist shop; small customer count; one module's registration
and email-confirmation path. Sign-in, token lifecycle, password reset/change, roles,
and admin provisioning are unchanged.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- [x] **Modular Monolith & Clean Architecture**: changes stay inside
      `internal/modules/auth`; the temporary-registration port is declared in
      `application/interface`, its Redis adapter in `infrastructure/implement/redis`;
      `domain` gains only a model and sentinel errors (stdlib + `share/access` +
      `uuid`). PASS.
- [x] **Transactional Integrity**: no money/inventory/payment logic here. Account
      creation is a single `INSERT`; durability of the account is the commit point and
      the Redis delete happens only after it succeeds. PASS.
- [x] **State Machines & Invariants**: the temporary-registration lifecycle
      (issued → replaced / verified / cancelled / expired) and the code lifecycle
      (issued → consumed / expired / blocked) are explicit with positive and negative
      tests. PASS.
- [x] **Test-First Critical Logic**: the OTP validation, replacement, idempotent
      account creation and attempt-limit logic are covered by tests first. PASS.
- [x] **Security & Least Privilege**: passwords and codes stored only as Argon2id
      hashes; references are unpredictable; rate limits retained on the flow
      endpoints; no secrets in logs; account ownership gates unchanged. PASS.
- [x] **Observability**: registration started / code verified / code failed / code
      resent / registration cancelled / delivery failed are emitted as audit events
      and structured logs without secrets. PASS.
- [x] **Simplicity (YAGNI)**: reuse the existing Redis cache, hashing, audit and
      email-retry machinery; no new infra, no new table, no new dependency. PASS.
- [x] **Frontend Integration Guide**: this feature changes client-facing endpoints, so
      `specs/012-fix-auth-registration/frontend-guide.md` is a required deliverable
      with before → after for each changed contract. PASS (planned).
- [x] **Swagger annotations**: all four endpoints carry annotations and
      `docs/swagger/` is regenerated with `make swagger`; `make swagger-check` passes.
      PASS (planned).

**Post-Phase 1 re-check**: the design adds no new layer, dependency, table, or
interface outside the auth module's existing structure; all gates still pass, so no
Complexity Tracking justification is required beyond the UUID allowance inherited from
feature 001.

## Project Structure

### Documentation (this feature)

```text
specs/012-fix-auth-registration/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── frontend-guide.md    # Phase 1/close-out deliverable (Constitution frontend-guide rule)
├── contracts/           # Phase 1 output (/speckit.plan command)
│   ├── README.md
│   ├── openapi.yaml
│   └── registration-error-codes.md
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
internal/modules/auth/
├── domain/
│   ├── model/
│   │   └── registration.go          # NEW: PendingRegistration + reference generator
│   ├── constant/
│   │   ├── codes.go                 # + AUTH_REGISTRATION_NOT_FOUND
│   │   └── audit.go                 # + AUTH_REGISTRATION_STARTED / _CANCELLED
│   ├── error/
│   │   └── errors.go                # + ErrRegistrationNotFound
│   └── repository/                  # unchanged
├── application/
│   ├── interface/
│   │   └── ports.go                 # RegistrationStore port; AuthService methods reworked
│   ├── implement/
│   │   ├── registration.go          # NEW: Start / VerifyOTP / ResendOTP / Cancel
│   │   ├── service.go               # Service gains Registration port + TTL config
│   │   ├── register.go              # REMOVED (superseded by registration.go)
│   │   └── verify_email.go          # REMOVED (superseded by registration.go)
│   ├── dto/dto.go                   # registration inputs/outputs
│   └── mapper/                      # unchanged
├── infrastructure/implement/redis/
│   └── registration.go              # NEW: Redis RegistrationStore (hash + pointer + Lua replace)
├── infrastructure/implement/email/  # unchanged
└── presentation/
    ├── dto/dto.go                   # registration HTTP request/response DTOs
    └── http/
        ├── handler.go               # Start / VerifyRegistrationOTP / Resend / Cancel handlers
        ├── router.go                # new routes replace verify-email / resend-verification
        └── errors.go                # + AUTH_REGISTRATION_NOT_FOUND mapping

internal/share/config/config.go     # + PENDING_REGISTRATION_TTL; OTP_TTL/OTP_MAX_ATTEMPTS defaults
cmd/api/main.go                     # wire RegistrationStore + new TTL

docs/api-reference.md               # auth section rewritten (Constitution VIII)
docs/swagger/                       # regenerated (make swagger)

# No migrations/ change (existing users constraints suffice; account inserted active).
```

**Structure Decision**: The rework stays within the auth module's finalized Clean
Architecture. The new persisted-in-cache concept is a port
(`RegistrationStore`) in `application/interface`, implemented in
`infrastructure/implement/redis`, keeping `application` free of Redis and matching how
the OTP store is structured today. `domain` gains a plain value model and sentinel
errors only. Reusing the existing OTP hashing/attempt/cooldown and email-retry
machinery (re-keyed by reference) avoids a parallel implementation.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| `domain` imports `github.com/google/uuid` | Pre-existing in the auth `domain` (primary keys are UUIDs; `Account.ID`); this feature adds no new import — `PendingRegistration.RegistrationID` is a plain `string` and the reference generator uses `crypto/rand` (stdlib) | Not introduced by this feature; the allowance is already recorded in Constitution I and feature 001. |

## Approach & Key Decisions

- **Temporary registration lives in Redis, not a staging table.** The account store is
  the source of truth; a half-finished registration is short-lived, never queried by
  other modules, and belongs with the OTP state already in Redis (see
  [research.md](./research.md) D1).
- **Reference, not email, keys the follow-up calls.** An unpredictable
  `registrationId` (32 random bytes, base64url) prevents a client from driving another
  address's registration and replaces the client-supplied email (D2).
- **One live registration per email.** A pointer key `auth:reg:email:{email} → id` plus
  an atomic Lua replace invalidates the previous registration's keys when a new one
  starts (D3, FR-026).
- **Verify creates then deletes.** The account `INSERT` (email/username unique) is the
  commit point; Redis is deleted only after success. An `ErrEmailTaken` on a retry is
  treated as success — the account already exists — giving idempotency and a single
  account under concurrency (D5).
- **Accounts are created `active`.** Verification is what creates them, so the
  `pending` status is no longer reached by self-registration (D6).
- **Anti-enumeration posture.** Starting a registration returns `409 AUTH_EMAIL_TAKEN`
  / `AUTH_USERNAME_TAKEN` for a taken address or handle (matching current behavior and
  spec scenario 3), while unknown/expired/cancelled references on verify, resend and
  cancel stay generic (FR-016). The change to the documented anti-enumeration claim is
  called out for `docs/api-reference.md` (D7).
- **Tunable defaults.** `PENDING_REGISTRATION_TTL=30m`, `OTP_TTL=5m`,
  `OTP_MAX_ATTEMPTS=5`, `OTP_RESEND_COOLDOWN=60s`, block 60s (D8).
