---
description: "Task list for User Authentication & Session Management"
---

# Tasks: User Authentication & Session Management

**Input**: Design documents from `/specs/001-user-auth/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Included. The specification requires test coverage for security logic
and the constitution mandates test-first for password verification, token
issuance/rotation, OTP validation, login lockout, and state transitions.

**Organization**: Tasks are grouped by user story to enable independent
implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1–US4)
- Exact file paths are included in each task

## Path Conventions

- Single Go module; module code under `internal/modules/auth/`, platform additions
  under `internal/share/`, migrations under `migrations/`, executables under `cmd/`.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Dependencies and local infrastructure for auth + Redis

- [X] T001 Add dependencies to `go.mod` (`github.com/redis/go-redis/v9`, `github.com/golang-jwt/jwt/v5`, `golang.org/x/crypto`, and `github.com/alicebob/miniredis/v2` for tests)
- [X] T002 [P] Add a `redis:7` service to `docker-compose.yml`
- [X] T003 [P] Extend `configs/.env.example` with Redis, JWT, OTP, login-lockout, password-reset, SMTP, and admin-seed variables
- [X] T004 [P] Document the `db`+`redis` dev workflow in `internal/share/README.md`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Shared infrastructure and domain/ports required by every story

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [X] T005 Extend configuration with `AuthConfig` and `RedisConfig` (JWT TTLs, OTP policy, login/flow rate-limit thresholds, lockout thresholds, reset TTL, SMTP, admin seed, Redis address) in `internal/share/config/config.go` including startup validation
- [X] T006 [P] Write unit tests for the new auth/Redis configuration in `internal/share/config/config_test.go`
- [X] T007 Implement the Redis client package (connect, ping, close) in `internal/share/cache/redis.go`
- [X] T008 [P] Write a miniredis-backed unit test for the cache package in `internal/share/cache/redis_test.go`
- [X] T009 Create the auth migration `migrations/00003_auth.sql` (`users`, `sessions`, `password_reset_requests` per data-model.md)
- [X] T010 Implement the Account domain entity, role/status value objects, state transitions, and typed errors in `internal/modules/auth/domain/account.go`
- [X] T011 [P] Write unit tests for account state transitions in `internal/modules/auth/domain/account_test.go`
- [X] T012 Implement Argon2id password hashing and the password policy in `internal/modules/auth/infrastructure/token/password.go`
- [X] T013 [P] Write unit tests for password hashing and policy in `internal/modules/auth/infrastructure/token/password_test.go`
- [X] T014 Implement JWT access-token issuance and verification in `internal/modules/auth/infrastructure/token/access.go`
- [X] T015 [P] Write unit tests for access tokens (expiry, tampering, claims) in `internal/modules/auth/infrastructure/token/access_test.go`
- [X] T016 Implement opaque refresh-token generation and SHA-256 hashing in `internal/modules/auth/infrastructure/token/refresh.go`
- [X] T017 [P] Write unit tests for refresh tokens in `internal/modules/auth/infrastructure/token/refresh_test.go`
- [X] T018 Implement the `EmailSender` interface with a log-only sender and an SMTP sender in `internal/modules/auth/infrastructure/email/sender.go`
- [X] T019 [P] Implement an in-memory fake email sender for tests in `internal/modules/auth/infrastructure/email/fake.go`
- [X] T020 Implement the Redis OTP store (put/verify/attempts/blacklist/resend-cooldown) in `internal/modules/auth/infrastructure/redis/otp.go`
- [X] T021 [P] Write miniredis-backed unit tests for the OTP store (3-strike blacklist, 1-minute cooldown) in `internal/modules/auth/infrastructure/redis/otp_test.go`
- [X] T022 Implement the Redis access-token blacklist store (per-jti and per-user minimum-iat) in `internal/modules/auth/infrastructure/redis/blacklist.go`
- [X] T023 [P] Write miniredis-backed unit tests for the blacklist store in `internal/modules/auth/infrastructure/redis/blacklist_test.go`
- [X] T024 Implement the Redis login-failure/lockout store (counter + blocked flag, clear on success) in `internal/modules/auth/infrastructure/redis/login_guard.go`
- [X] T025 [P] Write miniredis-backed unit tests for the login guard (10-strike lockout, 15-minute expiry, reset on success) in `internal/modules/auth/infrastructure/redis/login_guard_test.go`
- [X] T026 Implement the user repository (create, by-email, activate, update password) in `internal/modules/auth/infrastructure/postgres/user.go`
- [X] T027 Implement the session repository (create, by-token-hash, rotate, revoke) in `internal/modules/auth/infrastructure/postgres/session.go`
- [X] T028 Implement the password-reset repository (create, consume, invalidate-open) in `internal/modules/auth/infrastructure/postgres/reset.go`
- [X] T029 [P] Write testcontainers integration tests for the auth repositories in `internal/modules/auth/infrastructure/postgres/repository_integration_test.go`
- [X] T030 Implement `middleware.AuthHooks` access-token verification (signature, expiry, Redis blacklist) in `internal/modules/auth/application/hooks.go`

**Checkpoint**: Foundation ready — user stories can begin

---

## Phase 3: User Story 1 - Account registration and sign-in (Priority: P1) 🎯 MVP

**Goal**: Visitors register with email/password, confirm an emailed OTP, then sign
in (with brute-force lockout) and reach an authenticated area.

**Independent Test**: Register → read OTP → verify → login → `GET /api/v1/auth/me`;
10 failed logins lock the source with `AUTH_LOGIN_LOCKED`.

- [X] T031 [P] [US1] Write failing service tests for register/verify-email/resend/login + lockout in `internal/modules/auth/application/auth_test.go`
- [X] T032 [US1] Implement the registration service (pending account, OTP to Redis + email, generic response) in `internal/modules/auth/application/register.go`
- [X] T033 [US1] Implement the email-verification service (attempt limit, blacklist, cooldown, activate account) in `internal/modules/auth/application/verify_email.go`
- [X] T034 [US1] Implement the resend-verification service (1-minute cooldown, generic response) in `internal/modules/auth/application/verify_email.go`
- [X] T035 [US1] Implement the login service (password verify, status checks, Redis lockout enforcement, issue token pair, audit events) in `internal/modules/auth/application/login.go`
- [X] T036 [US1] Implement HTTP handlers and DTOs for register/verify-email/resend-verification/login and `GET /me` (authenticated identity) in `internal/modules/auth/presentation/http/handlers.go`
- [X] T037 [US1] Implement the auth router (`/api/v1/auth`), apply auth-specific rate-limit thresholds (sign-in 10/min, flows 5/min per source), and mount it from `cmd/api/main.go`
- [X] T038 [US1] Add contract tests for the auth endpoints (envelope, error codes, correlation) in `internal/modules/auth/presentation/http/contract_test.go`
- [X] T039 [US1] Add an integration test for the register→OTP→verify→login→me flow in `internal/modules/auth/presentation/http/auth_integration_test.go`

**Checkpoint**: User Story 1 fully functional and independently testable

---

## Phase 4: User Story 2 - Role-based access control (Priority: P2)

**Goal**: An admin account reaches admin-only capabilities; customers are denied
and denials are recorded.

**Independent Test**: Seed admin, sign in, reach the admin probe; sign in as
customer and confirm `403` plus an audit record.

- [X] T040 [P] [US2] Write failing tests for RBAC enforcement and the seed command in `internal/modules/auth/presentation/http/rbac_test.go`
- [X] T041 [US2] Implement `cmd/seed/main.go` to create the single admin account from `ADMIN_EMAIL`/`ADMIN_PASSWORD`
- [X] T042 [US2] Apply `RequireAuthentication`/`RequireAdmin` to protected routes, add an admin-only probe route, and emit privilege-denial audit events in `internal/modules/auth/presentation/http/router.go`
- [X] T043 [US2] Add tests proving a customer is denied and the denial is audited, and the admin is allowed, in `internal/modules/auth/presentation/http/rbac_test.go`

**Checkpoint**: Role boundary enforced and observable

---

## Phase 5: User Story 3 - Persistent session and sign-out (Priority: P3)

**Goal**: Sessions survive access-token expiry via rotation, work across devices,
and can be revoked on sign-out.

**Independent Test**: Login, refresh, reuse the old refresh token (rejected),
logout one device, confirm the other still works.

- [X] T044 [P] [US3] Write failing service tests for refresh and logout in `internal/modules/auth/application/session_test.go`
- [X] T045 [US3] Implement the refresh service with rotation and reuse rejection (no family revocation) in `internal/modules/auth/application/refresh.go`
- [X] T046 [US3] Implement the logout service revoking the presented session in `internal/modules/auth/application/logout.go`
- [X] T047 [US3] Implement refresh/logout handlers and add contract tests in `internal/modules/auth/presentation/http/handlers.go`
- [X] T048 [US3] Add an integration test for multi-device sessions, rotation, reuse, and sign-out in `internal/modules/auth/presentation/http/session_integration_test.go`

**Checkpoint**: Session lifecycle complete

---

## Phase 6: User Story 4 - Password reset (Priority: P4)

**Goal**: Users reset a forgotten password via an emailed link, and signed-in
users can change their password; prior access tokens are blacklisted.

**Independent Test**: Forgot → consume reset token → login with new password;
change password → old access token rejected.

- [X] T049 [P] [US4] Write failing service tests for forgot/reset/change in `internal/modules/auth/application/password_test.go`
- [X] T050 [US4] Implement the forgot-password service (single-use token, email link, generic response) in `internal/modules/auth/application/forgot_password.go`
- [X] T051 [US4] Implement the reset-password service (validate token, update password, revoke sessions, blacklist access tokens in Redis, issue tokens) in `internal/modules/auth/application/reset_password.go`
- [X] T052 [US4] Implement the change-password service (verify current password, preserve session expiry, blacklist prior access tokens) in `internal/modules/auth/application/change_password.go`
- [X] T053 [US4] Implement forgot/reset/change handlers and add contract tests in `internal/modules/auth/presentation/http/password_handlers.go`
- [X] T054 [US4] Add an integration test for reset and access-token blacklisting in `internal/modules/auth/presentation/http/password_integration_test.go`

**Checkpoint**: All user stories independently functional

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Hardening and validation affecting all stories

- [X] T055 [P] Emit audit events for all security actions (sign-in success/failure, lockout, sign-out, email verification, password change/reset, privilege denial) and add tests in `internal/modules/auth/application/audit_test.go`
- [X] T056 [P] Include Redis connectivity in the readiness check (Redis is required, so readiness is `not_ready` when Redis is down) and add coverage in `internal/share/health/health.go`
- [X] T057 [P] Add auth module documentation in `internal/modules/auth/README.md` and update `docs/modules/01-auth.md`
- [X] T058 Run `gofmt`, `go vet`, linter, `go test ./...`, and `go test -tags integration ./...` and fix findings
- [X] T059 Validate `quickstart.md` end-to-end with Docker (`db` + `redis`) and confirm Redis keys behave (3-strike OTP blacklist, 1-minute cooldown, 10-strike login lockout)
- [X] T060 Review the Constitution Check and record any deviations in `plan.md` Complexity Tracking

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies
- **Foundational (Phase 2)**: Depends on Setup — BLOCKS all user stories
- **User Stories (Phase 3+)**: Depend on Foundational completion
  - US1 and US2 share `transport/handlers.go` and `router.go` → sequence edits; US2 after US1
  - US3 depends on the token/refresh and session pieces from Foundational
  - US4 depends on the blacklist store and email sender from Foundational; it does not require US3
- **Polish (Phase 7)**: Depends on all desired stories

### User Story Dependencies

- **US1 (P1)**: After Foundational — no story dependencies
- **US2 (P2)**: After Foundational and US1 (router/handlers exist)
- **US3 (P3)**: After Foundational
- **US4 (P4)**: After Foundational (not gated on US3)

### Within Each User Story

- Tests written and failing before implementation
- Domain/token/store primitives before services; services before HTTP wiring
- Core implementation before integration tests

### Parallel Opportunities

- Setup: T002, T003, T004
- Foundational tests: T006, T008, T011, T013, T015, T017, T019, T021, T023, T025, T029
- US1: T031 (failing tests) once Foundational is done
- Polish: T055, T056, T057

---

## Parallel Example: Foundational

```bash
Task: "Write unit tests for auth/Redis config in internal/share/config/config_test.go"
Task: "Write miniredis unit tests for the OTP store in internal/modules/auth/infrastructure/redis/otp_test.go"
Task: "Write miniredis unit tests for the login guard in internal/modules/auth/infrastructure/redis/login_guard_test.go"
```

---

## Implementation Strategy

### MVP First (User Story 1)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL)
3. Complete Phase 3: US1
4. STOP and VALIDATE: register → OTP → verify → login → me

### Incremental Delivery

1. Setup + Foundational → auth compiles with Redis/token/domain primitives
2. US1 → registration + sign-in + lockout
3. US2 → RBAC + admin seed
4. US3 → refresh/logout
5. US4 → password reset/change

---

## Notes

- [P] tasks touch different files with no incomplete dependencies
- [Story] labels map tasks to user stories for traceability
- Verify tests fail before implementing
- Integration tests require the `integration` build tag and Docker

---

## Phase 8: Convergence

- [X] T061 [P] [US3] Add an integration test proving sign-out on one device leaves another device's session usable in `internal/modules/auth/presentation/http/http_integration_test.go` per FR-009 (partial) [delivered by T066]
- [X] T062 Harden bulk access-token invalidation so a token issued in the same second as a password change is rejected (record the changing token's `jti`) in `internal/modules/auth/infrastructure/implement/redis/blacklist.go` per SC-009 (partial)
- [X] T063 Centralize the privileged role constant in the auth domain and have `internal/share/middleware` accept it instead of defining its own (remove the duplicate `RoleAdmin`) per Constitution I (contradicts)

---

## Phase 9: Convergence

- [X] T064 Update `plan.md` (Project Structure + Structure Decision) and stale file paths in `tasks.md` to the finalized Clean Architecture layout (`presentation/application/infrastructure/domain`, `internal/share`) per plan: structure / Constitution I (partial)
- [X] T065 Add an HTTP end-to-end integration test (register → email OTP → verify → login → `me` → refresh/reuse) in `internal/modules/auth/presentation/http/http_integration_test.go` per FR-018/SC-008 (missing)
- [X] T066 Add an integration test proving sign-out on one device leaves another device's session usable in `internal/modules/auth/presentation/http/http_integration_test.go` per FR-009 (partial)

---

## Phase 10: Convergence

- [X] T067 Replace the remaining `internal/platform` references in `plan.md` (lines 20 and 66) with `internal/share` per plan: structure / Constitution I (partial)
- [X] T068 Update the stale `internal/modules/auth/controller/...` paths in `tasks.md` (T036–T054) to `internal/modules/auth/presentation/http/...` per tasks: paths (partial)
- [X] T069 Add unit tests for the generic repository (`Repository[T, ID]` / `Base`) covering Create/FindByID/Exists/FindAll against a test database in `internal/share/repository/repository_integration_test.go` per plan: generic repository (partial)
- [X] T070 Add a unit test for the application mapper (model → dto) in `internal/modules/auth/application/mapper/mapper_test.go` per plan: mapper (partial)
