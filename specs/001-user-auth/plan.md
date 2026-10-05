# Implementation Plan: User Authentication & Session Management

**Branch**: `001-user-auth` | **Date**: 2026-09-18 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/001-user-auth/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command; its definition describes the execution workflow.

## Summary

Implement the `auth` business module on top of the platform foundation:
email/password registration gated by an emailed OTP stored and verified in Redis,
sign-in issuing a 15-minute JWT access token and a 45-day rotating opaque refresh
token, refresh/logout with rotation and reuse rejection, password reset and
password change (new refresh token keeps the replaced expiry, prior access tokens
are blacklisted in Redis), role-based access control (customer/admin), a CLI seed
command for the single admin account, security audit events, and rate limiting on
authentication routes.

The module lives under `internal/modules/auth`, depends only on `internal/share`
and its own domain, and implements the foundation's `middleware.AuthHooks` so the
router can authenticate and authorize requests.

## Technical Context

**Language/Version**: Go 1.26 (module `github.com/mavil-chudomirovich/artist-shop-be`)

**Primary Dependencies**: platform foundation (`config`, `database` + `WithTx`,
`httpx`, `middleware`, `audit`, `reqctx`); a new platform `cache` package backed by
`github.com/redis/go-redis/v9`; `golang.org/x/crypto/argon2` (password/OTP
hashing), `github.com/golang-jwt/jwt/v5` (HS256 access tokens),
`github.com/google/uuid`; hand-written SQL over `pgx` (see research: sqlc deferred).

**Storage**: PostgreSQL — new tables `users` (account), `sessions`,
`password_reset_requests`. Redis (shared cache) — email-confirmation OTPs, OTP
attempt/blacklist counters, OTP resend cooldown, sign-in failure/lockout counters
(SC-006), and the access-token blacklist.

**Testing**: `go test`; unit tests for password/OTP/JWT logic and state
transitions; integration tests (build tag `integration`, testcontainers) for the
repository and end-to-end auth flows; a fake email sender for OTP/reset.

**Target Platform**: Linux server via Docker (same single binary as the foundation).

**Project Type**: web-service — modular monolith; this feature is one module.

**Performance Goals**: sign-in and refresh p95 < 300 ms locally; password hashing
cost tuned so a single hash stays well under ~250 ms.

**Constraints**: single deployable; durable session/refresh state in PostgreSQL;
OTP storage, the sign-in failure/lockout counters, and the access-token blacklist
MUST use Redis (required); in-process rate limiting inherited from the foundation
with auth-specific thresholds; email delivered through an abstraction with an SMTP
implementation and a log-only default for local/dev. Redis is ephemeral: losing it
at most forces OTP re-issuance, resets sign-in failure counters, and re-validates at
most short-lived access tokens.

**Scale/Scope**: single-artist shop; exactly one admin account; customer count
small; auth is the only module scope here.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- [x] **Modular Monolith Boundaries**: module under `internal/modules/auth`; depends
      on `internal/share`; other modules use exported auth interfaces only. PASS.
- [x] **Transactional Integrity**: registration, password change, and reset run in a
      single transaction via `database.WithTx`; no money/inventory logic here. PASS.
- [x] **State Machines & Invariants**: account status (`pending → active →
      disabled`), email-verification (open → consumed/expired), and session
      (active → rotated/revoked/expired) transitions are explicit with positive
      and negative tests. PASS.
- [x] **Test-First Critical Logic**: password verification, token issuance/rotation,
      OTP validation, and auth state transitions are built test-first. PASS.
- [x] **Security & Least Privilege**: Argon2id, HS256 JWT with a strong secret,
      opaque rotating refresh tokens, generic anti-enumeration responses,
      ownership/role checks, rate limiting, no secrets in logs. PASS.
- [x] **Observability**: security events (sign-in success/failure, sign-out,
      password change, privilege denial) emitted through `audit.Emitter`; structured
      logs via the foundation. PASS.
- [x] **Simplicity (YAGNI)**: opaque refresh tokens instead of JWT refresh, in-process
      rate limiting, SMTP interface instead of a vendor SDK, hand-written SQL. PASS.

**Post-Phase 1 re-check**: design keeps all gates passing; no Complexity Tracking
entries required.

## Project Structure

### Documentation (this feature)

```text
specs/001-user-auth/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
cmd/
├── api/                 # existing composition root (mounts the auth router, wires AuthHooks)
├── migrate/             # existing schema migration CLI
└── seed/                # NEW: seed the single admin account from environment credentials

internal/
├── contracts/           # interfaces published between modules
├── share/               # cross-cutting code shared by modules (constitution I)
│   ├── access/          # shared roles/permissions
│   ├── repository/      # generic Repository[T, ID] + Base (CRUD + list + pagination)
│   ├── cache/           # Redis client
│   ├── config/ database/ httpx/ middleware/ httpserver/ health/ audit/ logging/ reqctx/
└── modules/
    └── auth/            # module — Clean Architecture
        ├── domain/
        │   ├── model/        # Account, Session, ResetRequest + password policy
        │   ├── constant/     # status, error codes, audit actions
        │   ├── error/        # business sentinel errors
        │   └── repository/   # repository interfaces
        ├── application/
        │   ├── interface/    # AuthService + external-service ports + UnitOfWork
        │   ├── implement/    # use cases
        │   ├── dto/          # input/output DTOs
        │   └── mapper/       # model ↔ dto
        ├── infrastructure/
        │   └── implement/    # postgres, redis, token, email, auditor
        └── presentation/
            ├── http/         # handlers, router, error mapping, AuthHooks
            ├── cli/          # admin seed command
            ├── worker/       # placeholder
            └── dto/          # HTTP request/response DTOs

migrations/              # versioned migrations (NNNNN_<module>_<description>.sql)
```

**Structure Decision**: The auth module follows the finalized Clean Architecture
(`docs/architecture.md`, constitution v1.3.0): `domain` holds model/constant/error
and repository interfaces; `application` holds use-case interfaces, external
service ports and UnitOfWork in `interface`, use cases in `implement`, plus `dto`
and a single `mapper`; `infrastructure/implement` provides the PostgreSQL
(embedding `share/repository.Base`), Redis, token, email, and audit adapters;
`presentation/{http,cli,worker,dto}` exposes HTTP, the seed command, a worker
placeholder, and HTTP DTOs. Cross-module communication goes through
`internal/contracts`. Cross-cutting code lives in `internal/share/`, which modules
depend on, never the reverse.

## Complexity Tracking

> No constitution violations. Table intentionally empty.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| (none) | — | — |
