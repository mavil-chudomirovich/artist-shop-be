---
description: "Task list for Cross-Cutting Foundation"
---

# Tasks: Cross-Cutting Foundation

**Input**: Design documents from `/specs/002-cross-cutting-foundation/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Included. The specification requires an isolated test database and a
unit + API test harness (FR-015, SC-008), and the constitution requires test-first
for foundation logic (config validation, error mapping, migration ordering,
middleware, redaction).

**Organization**: Tasks are grouped by user story to enable independent
implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1–US5)
- Exact file paths are included in each task

## Path Conventions

- Single Go module at repository root; executable in `cmd/`, shared foundation in
  `internal/share/`, migrations in `migrations/`, config in `configs/`.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and basic structure

- [X] T001 Initialize Go module `github.com/mavil-chudomirovich/artist-shop-be` and create base directories (`cmd/api`, `internal/platform`, `internal/modules`, `migrations`, `configs`) per plan.md
- [X] T002 Add core dependencies to `go.mod` (go-chi/chi v5, jackc/pgx v5, pressly/goose v3, caarlos0/env v11, joho/godotenv, google/uuid, go-playground/validator v10, go-chi/httprate, go-chi/cors, stretchr/testify, testcontainers-go + postgres module)
- [X] T003 [P] Configure formatting and linting in `.golangci.yml` and document `gofmt`/`go vet` usage
- [X] T004 [P] Create `configs/.env.example` documenting every environment variable from quickstart.md
- [X] T005 [P] Add `Makefile` with `run`, `test`, `lint`, `migrate-up`, `migrate-down`, `migrate-status` targets
- [X] T006 [P] Add `docker-compose.yml` for local PostgreSQL 16

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST complete before ANY user story

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [X] T007 Implement typed configuration structs and environment loading in `internal/share/config/config.go`
- [X] T008 Implement startup validation with fail-fast, non-secret error messages in `internal/share/config/config.go`
- [X] T009 [P] Write unit tests for config loading and validation in `internal/share/config/config_test.go`
- [X] T010 Implement `log/slog` JSON logger setup in `internal/share/logging/logging.go`
- [X] T011 [P] Implement log redaction helper and correlation attribute injection in `internal/share/logging/redact.go`
- [X] T012 [P] Write unit tests for redaction in `internal/share/logging/redact_test.go`
- [X] T013 Implement pooled PostgreSQL connection (`pgxpool`) in `internal/share/database/db.go`
- [X] T014 Implement `WithTx` transaction manager and `Querier` context helper in `internal/share/database/tx.go`
- [X] T015 Implement goose embedded migration runner with PostgreSQL advisory lock in `internal/share/database/migrate/migrate.go`
- [X] T016 [P] Create initial migration `migrations/00001_create_audit_logs.sql` per data-model.md
- [X] T017 Implement HTTP server lifecycle (start, graceful shutdown) and base router in `internal/share/httpserver/server.go`
- [X] T018 Implement isolated test database harness using testcontainers in `internal/share/database/testdb_test.go`
- [X] T019 Wire the composition root (config → logging → database → migrations → server) in `cmd/api/main.go`

**Checkpoint**: Foundation ready — user story implementation can now begin

---

## Phase 3: User Story 1 - Bootstrap the service from a clean checkout (Priority: P1) 🎯 MVP

**Goal**: A fresh clone starts the service and answers requests under the
versioned base path in the standard format.

**Independent Test**: From a clean checkout with the documented setup, start the
service and confirm an unknown `/api/v1/...` path returns the standard error
envelope rather than a framework default.

- [X] T020 [P] [US1] Write failing tests for success and error envelope encoding in `internal/share/httpx/response_test.go`
- [X] T021 [US1] Implement response envelope encode/write helpers in `internal/share/httpx/response.go`
- [X] T022 [US1] Register the versioned `/api/v1` router in `internal/share/httpserver/routes.go`
- [X] T023 [US1] Implement standard not-found and method-not-allowed handlers in `internal/share/httpserver/routes.go`
- [X] T024 [US1] Load optional `.env` in development only in `cmd/api/main.go`
- [X] T025 [US1] Add startup integration test (clean config → response on `/api/v1`) in `internal/share/httpserver/server_test.go`

**Checkpoint**: User Story 1 fully functional and testable independently

---

## Phase 4: User Story 2 - Deploy and operate per environment (Priority: P1)

**Goal**: The same build runs in multiple environments by configuration only,
and liveness/readiness signals reflect real dependency state.

**Independent Test**: Start in two environments with different configuration;
confirm behavior differs only by config, and `/readyz` reports not-ready before
the database/schema is usable and ready after.

- [X] T026 [P] [US2] Write failing tests for liveness and readiness handlers in `internal/share/health/health_test.go`
- [X] T027 [US2] Implement the liveness handler in `internal/share/health/health.go`
- [X] T028 [US2] Implement the readiness handler (database ping + migration version) in `internal/share/health/health.go`
- [X] T029 [US2] Register `/healthz` and `/readyz` outside `/api/v1` in `internal/share/httpserver/routes.go`
- [X] T030 [US2] Track database/migration readiness state in `internal/share/database/db.go`
- [X] T031 [US2] Wire environment separation (development/staging/production) through config in `internal/share/config/config.go`
- [X] T032 [US2] Add integration test for readiness transitions and 503 when the database is down in `internal/share/health/health_test.go`

**Checkpoint**: User Stories 1 AND 2 both work independently

---

## Phase 5: User Story 3 - Consume a predictable API contract (Priority: P2)

**Goal**: Every response uses one envelope; every error uses one format with a
stable code and correlation ID; middleware enforces limits and recovery.

**Independent Test**: Trigger success, client error, and recovered internal
failure; confirm each uses the standard format and carries `X-Request-Id` present
in the logs.

- [X] T033 [P] [US3] Write failing tests for the error catalogue and HTTP status mapping in `internal/share/httpx/errors_test.go`
- [X] T034 [US3] Implement `AppError`, the error catalogue, and `Wrap`/`Is` helpers in `internal/share/httpx/errors.go`
- [X] T035 [US3] Implement panic recovery middleware mapping to `INTERNAL_ERROR` in `internal/share/middleware/recovery.go`
- [X] T036 [P] [US3] Write failing tests for correlation propagation in `internal/share/middleware/correlation_test.go`
- [X] T037 [US3] Implement correlation ID middleware (`X-Request-Id`) in `internal/share/middleware/correlation.go`
- [X] T038 [US3] Implement error-to-response mapping at the HTTP boundary in `internal/share/httpx/errors.go`
- [X] T039 [US3] Implement request body size limit middleware in `internal/share/middleware/bodylimit.go`
- [X] T040 [US3] Implement CORS middleware using the configured origin allowlist in `internal/share/middleware/cors.go`
- [X] T041 [US3] Implement in-process rate limiting middleware keyed by client and route class in `internal/share/middleware/ratelimit.go`
- [X] T042 [P] [US3] Write failing tests for rate limiting and body limits in `internal/share/middleware/middleware_test.go`
- [X] T043 [US3] Add contract tests for the envelope, error format, and `X-Request-Id` header in `internal/share/httpx/contract_test.go`

**Checkpoint**: API contract enforceable and verified

---

## Phase 6: User Story 4 - Trace privileged and payment activity (Priority: P2)

**Goal**: Privileged actions and payment events produce exactly one durable,
append-only audit record each, without blocking the business operation.

**Independent Test**: Emit a privileged action and a payment event; confirm
exactly one row per event with all required fields, and that retries do not
duplicate rows.

- [X] T044 [P] [US4] Write failing tests for the async audit writer and idempotent retry in `internal/share/audit/writer_test.go`
- [X] T045 [US4] Implement the audit record model in `internal/share/audit/model.go`
- [X] T046 [US4] Implement the audit repository (insert keyed by `event_id`) in `internal/share/audit/repository.go`
- [X] T047 [US4] Implement the buffered async writer with bounded workers, backoff retry, and observable failures in `internal/share/audit/writer.go`
- [X] T048 [US4] Provide the audit emitter interface and wire it into the platform in `internal/share/audit/audit.go`
- [X] T049 [US4] Add migration `migrations/00002_audit_append_only.sql` enforcing append-only via revoked DML and/or guard trigger
- [X] T050 [US4] Add integration test proving exactly one row per event under retries in `internal/share/audit/writer_integration_test.go`

**Checkpoint**: Audit trail reliable and idempotent

---

## Phase 7: User Story 5 - Evolve the schema safely (Priority: P3)

**Goal**: Schema changes apply consistently across environments, can be rolled
back, and never run concurrently.

**Independent Test**: Apply a migration to empty and existing databases, roll it
back, and start two instances against a fresh database to confirm it applies once.

- [X] T051 [P] [US5] Write failing tests for migration up/down and advisory lock in `internal/share/database/migrate/migrate_test.go`
- [X] T052 [US5] Add the migration CLI (`migrate up|down|status`) in `cmd/migrate/main.go`
- [X] T053 [US5] Implement the advisory-lock concurrency guard and version reporting in `internal/share/database/migrate/migrate.go`
- [X] T054 [US5] Document the destructive-migration guard and data-migration-note requirement in `migrations/README.md`
- [X] T055 [US5] Add integration test proving concurrent startup applies migrations once in `internal/share/database/migrate/migrate_integration_test.go`

**Checkpoint**: All user stories independently functional

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple stories

- [X] T056 [P] Add platform package documentation in `internal/share/README.md`
- [X] T057 [P] Add a CI workflow running `gofmt`, `go vet`, lint, and `go test ./...` in `.github/workflows/ci.yml`
- [X] T058 Run `gofmt`, `go vet`, and the linter across the repository and fix findings
- [X] T059 Run `go test ./...` and confirm SC-008 (under 10 minutes) and SC-007 (no secrets in logs)
- [X] T060 Validate quickstart.md end-to-end and correct any discrepancies
- [X] T061 Review the Constitution Check and record any deviations in plan.md Complexity Tracking

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — starts immediately
- **Foundational (Phase 2)**: Depends on Setup — BLOCKS all user stories
- **User Stories (Phase 3+)**: Depend on Foundational completion
  - US1 and US2 (both P1) can proceed in parallel, but US2 reuses the router from US1
  - US3 depends on US1 (envelope/response helpers)
  - US4 is independent of US3 but uses the database and migrations
  - US5 depends on Foundational migration runner
- **Polish (Phase 8)**: Depends on all desired stories

### User Story Dependencies

- **US1 (P1)**: After Foundational — no story dependencies
- **US2 (P1)**: After Foundational — reuses router from US1 for route registration
- **US3 (P2)**: After Foundational and US1 (response helpers)
- **US4 (P2)**: After Foundational (database + migrations)
- **US5 (P3)**: After Foundational (migration runner)

### Within Each User Story

- Tests MUST be written and FAIL before implementation
- Models before services; services before HTTP wiring
- Core implementation before integration tests

### Parallel Opportunities

- Setup: T003, T004, T005, T006 in parallel
- Foundational: T009, T011, T012, T016 in parallel (distinct files)
- US3: T033, T036, T042 test tasks in parallel
- US4: T044 in parallel with US3 work once Foundational completes
- Polish: T056 and T057 in parallel

---

## Parallel Example: User Story 3

```bash
# Launch independent failing-test tasks together:
Task: "Write failing tests for error catalogue in internal/share/httpx/errors_test.go"
Task: "Write failing tests for correlation in internal/share/middleware/correlation_test.go"
Task: "Write failing tests for rate limiting in internal/share/middleware/middleware_test.go"
```

---

## Implementation Strategy

### MVP First (User Story 1)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL — blocks all stories)
3. Complete Phase 3: User Story 1
4. STOP and VALIDATE: clean checkout starts and responds under `/api/v1`

### Incremental Delivery

1. Setup + Foundational → foundation compiles and starts
2. US1 → versioned response contract → validate
3. US2 → environment + health → validate
4. US3 → full error/correlation/limits contract → validate
5. US4 → audit trail → validate
6. US5 → safe schema evolution → validate

---

## Notes

- [P] tasks touch different files with no incomplete dependencies
- [Story] labels map tasks to user stories for traceability
- Verify tests fail before implementing
- Commit after each task or logical group
- Stop at any checkpoint to validate a story independently

---

## Phase 9: Convergence

- [X] T062 Add schema-version awareness to the readiness check and consume the `Dependencies.Version` hook in `internal/share/health/health.go` and `internal/share/httpserver/routes.go` per FR-013/SC-005 (partial)
- [X] T063 Add an automated destructive-migration guard (require a `-- +goose Down` section and an explicit data-migration note/flag) to CI per FR-005 (partial)
- [X] T064 Wire the `Authentication(deps.Auth)` middleware into the router pipeline in `internal/share/httpserver/routes.go` and add coverage per FR-012 (partial)
- [X] T065 Implement route-class keying for the in-process rate limiter in `internal/share/middleware/ratelimit.go` per FR-021 (partial)
- [X] T066 Validate `quickstart.md` end-to-end against a running PostgreSQL (Docker) and record the clean-checkout result per SC-001 (partial)
- [X] T067 Add the `internal/modules` placeholder (e.g. `doc.go`) per plan: source structure (missing)
- [X] T068 Reconcile plan/research library decisions (`httprate`, `go-playground/validator`) with the implemented choices (`golang.org/x/time/rate`, no validator) per plan: Technical Context (partial)

---

## Phase 10: Convergence

- [X] T069 Attach the correlation ID to application logs (use `logging.WithCorrelation` in `internal/share/httpx/response.go` error logging and add a request-logging middleware) so every handled request emits a log entry carrying the correlation ID per FR-009/SC-004 (partial)
- [X] T070 Add middleware that rejects non-JSON request bodies with `UNSUPPORTED_MEDIA_TYPE` (415) when a body is present, and wire it into `internal/share/httpserver/routes.go` per contracts/http-conventions.md (missing)
- [X] T071 Make the `Makefile` `lint` target fail on formatting/lint findings and run `golangci-lint` (config `.golangci.yml` already present) per plan: quality gates (partial)

---

## Phase 11: Convergence

- [X] T072 Add a test asserting every error-code constant is registered in the `catalogue` (and that `New` never falls back for a known code) in `internal/share/httpx/errors_test.go` per FR-007/contracts/error-codes.md (partial)
- [X] T073 Add a concurrency test proving correlation IDs do not collide or leak across simultaneous requests in `internal/share/middleware/correlation_test.go` per spec Edge Cases (partial)
