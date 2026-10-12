# Implementation Plan: Password Reset via Emailed Code

**Branch**: `013-password-reset-otp` | **Date**: 2026-10-12 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/013-password-reset-otp/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command; its definition describes the execution workflow.

## Summary

Insert a code-verification step into the password-reset flow and stop auto-signing the
user in. Today `POST /auth/password/forgot` emails the reset token directly and
`POST /auth/password/reset` returns a signed-in session. This feature changes the request
step to email a short **one-time code**, adds
`POST /auth/password/forgot/verify-otp` which exchanges a correct code for a separate,
single-use **reset token**, and changes the reset step to report success **without** issuing
a session (the user returns to sign-in). The reset token mechanism (durable
`password_reset_requests`, single-use, TTL) is reused unchanged; only its issuance moves
behind code verification. The codes and their attempt/cooldown state live in Redis in a
focused store, mirroring the registration flow's code handling.

This is a contract-changing rework of `/auth/password/forgot` and `/auth/password/reset`
and adds one endpoint, so it also ships `frontend-guide.md`, updated Swagger annotations,
and an updated `docs/api-reference.md` (Constitution VIII and the frontend-guide rule).

## Technical Context

**Language/Version**: Go 1.26 (module `github.com/mavil-chudomirovich/artist-shop-be`)

**Primary Dependencies**: existing auth module and platform (`config`, `database`,
`httpx`, `middleware`, `audit`, `logging`); `internal/share/cache`
(`github.com/redis/go-redis/v9`); `golang.org/x/crypto/argon2` (code hashing — via the
existing `PasswordHasher`); `github.com/google/uuid`. No new third-party dependency.

**Storage**: PostgreSQL — the existing `password_reset_requests` table (reset token),
unchanged; **no migration**. Redis (shared cache) — a new per-email reset-code challenge:
hashed code, attempt counter, block marker, send-cooldown marker, each with its own TTL.

**Testing**: `go test`; unit tests for the request/verify/reset use cases and the Redis
code store; integration tests (build tag `integration`, testcontainers) for the end-to-end
flow; the existing fake email sender.

**Target Platform**: Linux server via Docker (same single binary as the foundation).

**Project Type**: web-service — modular monolith; this feature is one module.

**Performance Goals**: request/verify/reset should stay well under ~300 ms under normal local
load — a design target, not an automated CI assertion — excluding mail delivery (which uses
the same retry budget as the registration flow).

**Constraints**: single deployable; Redis is ephemeral (losing it at worst forces a new
code; it never creates a partial reset); the reset token is the only durable reset state;
the code, its attempts and the plaintext password/token are never logged or stored in
readable form; every Redis key carries a TTL.

**Scale/Scope**: single-artist shop; small customer count; the password-reset path only.
Sign-in, registration, token lifecycle, password change and admin provisioning are unchanged.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- [x] **Modular Monolith & Clean Architecture**: changes stay inside
      `internal/modules/auth`; the reset-code port is declared in `application/interface`,
      its Redis adapter in `infrastructure/implement/redis`; `domain` gains at most a model
      and sentinel errors (stdlib + `share/access` + `uuid`); `application` depends only on
      `application/interface` and `domain`. PASS.
- [x] **Transactional Integrity**: no money/inventory/payment logic. The password update,
      token consume, session revocation and access-token blacklist run in one `UnitOfWork`
      transaction; creating the reset token runs in its own transaction. PASS.
- [x] **State Machines & Invariants**: the reset-code lifecycle (issued → verified /
      replaced / expired / blocked) and the reset-token lifecycle (issued → used / expired /
      superseded) are explicit with positive and negative tests. PASS.
- [x] **Test-First Critical Logic**: code issue/verify/attempts, token issuance and the
      password change are covered by tests written first. PASS.
- [x] **Security & Least Privilege**: codes and passwords stored only as Argon2id hashes;
      generic anti-enumeration responses; rate limits on the flow; a successful reset ends
      every session and blacklists prior access tokens; no secrets in logs. PASS.
- [x] **Observability**: reset requested / code verified / code failed / reset completed /
      delivery failed are emitted as audit events and structured logs without secrets. PASS.
- [x] **Simplicity (YAGNI)**: reuse the existing durable reset-token table, the existing
      Argon2id hasher, the delivery-retry helpers and the Redis cache; no new table, no new
      dependency, no separate resend endpoint. PASS.
- [x] **Frontend Integration Guide**: the feature changes two client-facing endpoints and
      adds one, so `specs/013-password-reset-otp/frontend-guide.md` is a required
      deliverable with before → after for each changed contract. PASS (planned).
- [x] **Swagger annotations**: all three endpoints carry annotations and `docs/swagger/` is
      regenerated with `make swagger`; `make swagger-check` passes. PASS (planned).

**Post-Phase 1 re-check**: the design adds no new layer, dependency or table; all gates
still pass. The `domain`-imports-`uuid` allowance is inherited from feature 001.

## Project Structure

### Documentation (this feature)

```text
specs/013-password-reset-otp/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── frontend-guide.md    # Phase 1/close-out deliverable (Constitution frontend-guide rule)
├── contracts/           # Phase 1 output (/speckit.plan command)
│   ├── README.md
│   ├── openapi.yaml
│   └── reset-error-codes.md
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
internal/modules/auth/
├── application/
│   ├── interface/ports.go        # + PasswordResetOTPStore; AuthService gains VerifyResetOTP
│   │                             #   and ResetPassword drops its SessionOutput return
│   ├── implement/
│   │   ├── password.go           # ForgotPassword rewritten (emails a code),
│   │   │                         #   + VerifyResetOTP (code → reset token),
│   │   │                         #   ResetPassword now returns error (no session)
│   │   └── service.go            # Service gains ResetOTP port (uses existing generateOTP)
│   └── dto/dto.go                # + VerifyResetOTPInput, ResetTokenOutput
├── domain/
│   ├── error/errors.go           # reuse existing OTP/reset sentinels (add only if needed)
│   └── constant/audit.go         # + code-verified / code-failed / delivery-failed events
│                                 #   (reuse reset-requested and reset-completed)
├── infrastructure/implement/redis/
│   └── reset_otp.go              # NEW: Redis PasswordResetOTPStore (per-email keys)
└── presentation/
    ├── dto/dto.go                # + VerifyResetOTPRequest, ResetTokenResponse
    └── http/
        ├── handler.go            # ForgotPassword (code), VerifyResetOTP (new), ResetPassword (changed)
        ├── router.go             # + POST /password/forgot/verify-otp
        └── errors.go             # reuse OTP/reset code mappings

cmd/api/main.go                   # wire the reset-code store (reuse OTP_* config)

docs/api-reference.md             # §3.8/§3.9 rewritten + new endpoint (Constitution VIII)
docs/swagger/                     # regenerated (make swagger)
internal/modules/auth/README.md   # reset flow description

# No migrations/ change (password_reset_requests already exists).
```

**Structure Decision**: The reset-code challenge is a port
(`PasswordResetOTPStore`) in `application/interface`, implemented in
`infrastructure/implement/redis`, so `application` stays free of Redis and mirrors the
registration store's placement. The reset token keeps its existing durable repository and
table; only its issuance moves behind code verification. Reusing the Argon2id hasher,
`generateOTP`, and the delivery-retry helpers avoids a parallel implementation.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| `domain` imports `github.com/google/uuid` | Pre-existing in the auth `domain` (primary keys are UUIDs); this feature adds no new import | Not introduced by this feature; the allowance is recorded in Constitution I and feature 001. |

## Approach & Key Decisions

- **Code lives in Redis, keyed by email; the reset token stays in PostgreSQL.** The code
  challenge is short-lived and never queried by other modules; the reset token is the
  durable, single-use authority to change a password (see [research.md](./research.md) D1/D2).
- **Verification exchanges the code for the reset token.** `verify-otp` verifies the code,
  creates a `password_reset_requests` row, invalidates the code and returns the token
  (D3). The token is not emailed (FR-003).
- **Reset reports success without a session.** The password change still ends every session
  and blacklists prior access tokens; the user signs in again (D4, FR-012/FR-013).
- **The request step is also the resend.** No separate resend endpoint; the cooldown marker
  gates repeats (D5).
- **Code policy is reused from registration.** `OTP_TTL`, `OTP_MAX_ATTEMPTS`,
  `OTP_BLOCK_TTL`, `OTP_RESEND_COOLDOWN`; reset-token lifetime uses `PASSWORD_RESET_TTL`
  (D6).
- **Anti-enumeration.** The request always answers generically; verify failures do not
  reveal whether an address is registered (D7).
- **Interface change.** `AuthService.ResetPassword` returns `error`; `VerifyResetOTP` is
  added. This is contained in the auth module (D9).
