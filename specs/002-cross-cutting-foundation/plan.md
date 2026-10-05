# Implementation Plan: Cross-Cutting Foundation

**Branch**: `002-cross-cutting-foundation` | **Date**: 2026-09-18 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/002-cross-cutting-foundation/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command; its definition describes the execution workflow.

## Summary

Build the shared foundation for the modular-monolith backend: environment-driven
configuration with fail-fast validation, a pooled PostgreSQL connection, versioned
embedded migrations applied automatically at startup behind an advisory lock,
a single HTTP response envelope and stable error catalogue with correlation IDs,
structured JSON logging with secret redaction, the middleware pipeline
(authentication-context and authorization enforcement hooks, in-process rate
limiting, panic recovery, CORS, request-size limits), append-only asynchronous
audit records with idempotent retry, liveness/readiness endpoints, and a
unit + API + integration test harness with an isolated database.

The foundation intentionally does NOT implement credential or session logic;
module 01 (auth) supplies the authentication/authorization implementation behind
the interfaces defined here.

## Technical Context

**Language/Version**: Go 1.26 (module `github.com/mavil-chudomirovich/artist-shop-be`)

**Primary Dependencies**: `go-chi/chi` v5 (router/middleware), `go-chi/cors`,
`jackc/pgx` v5 + `pgxpool` (driver/pool), `pressly/goose` v3 (embedded
migrations), `caarlos0/env` v11 + `joho/godotenv` (config), standard-library
`log/slog` (structured logging), `google/uuid`, `golang.org/x/time/rate`
(in-process rate limiting with burst support),
`testcontainers/testcontainers-go` (isolated integration-test database, build
tag `integration`).

Deferred to later modules (not yet used by the foundation): `sqlc` for type-safe
query codegen and `go-playground/validator` for request validation.

**Storage**: PostgreSQL 16+ (primary relational database; one instance, no
external shared store for rate limiting).

**Testing**: `go test` with the standard library; integration tests spin up an
ephemeral PostgreSQL via `testcontainers-go` (build tag `integration`);
migrations applied per test database.

**Target Platform**: Linux server via Docker (production on VPS); developed on
Windows (PowerShell).

**Project Type**: web-service — modular monolith, single deployable.

**Performance Goals**: foundation endpoints p95 < 300 ms; configuration
validation completes < 5 s on failure; readiness turns ready within 30 s of
dependencies becoming usable.

**Constraints**: single deployable service; no microservices/service mesh; no
external rate-limit store; no distributed tracing/APM; modular boundaries
enforced by package structure (`internal/<module>`).

**Scale/Scope**: single-artist shop, low traffic, one running instance; foundation
covers cross-cutting concerns only (no business domain).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- [x] **Modular Monolith Boundaries**: foundation lives in a single shared
      `internal/share/*` area; business modules depend on it, never the
      reverse; no cross-module data access introduced. PASS.
- [x] **Transactional Integrity**: foundation provides the transaction manager
      other modules use; audit writes do not participate in business
      transactions (best-effort async per clarification). No money/inventory
      logic here. PASS (scoped).
- [x] **State Machines & Invariants**: no lifecycle state machine in this
      feature. N/A — no violation.
- [x] **Test-First Critical Logic**: no pricing/inventory/state logic; however
      error mapping, config validation, migration ordering, and middleware are
      built test-first. PASS.
- [x] **Security & Least Privilege**: config/secrets from environment only,
      secret redaction in logs, CORS allowlist, body-size limits, rate limiting,
      auth enforcement hooks, no internals in responses. PASS.
- [x] **Observability**: structured logs + correlation ID, `audit_logs`
      append-only, liveness/readiness. PASS.
- [x] **Simplicity (YAGNI)**: in-process rate limiting, single service, no
      WebSocket/tracing; deviations none. PASS.

**Post-Phase 1 re-check**: design keeps all gates passing; no Complexity Tracking
entries required.

## Project Structure

### Documentation (this feature)

```text
specs/002-cross-cutting-foundation/
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
└── api/
    └── main.go                 # composition root: load config, init logging/db, start server

internal/
├── platform/                   # shared foundation (this feature)
│   ├── config/                 # env loading, validation, typed settings
│   ├── database/               # pgxpool setup, transaction manager
│   │   └── migrate/            # goose runner + embedded SQL migrations
│   ├── logging/                # slog setup, redaction, correlation helpers
│   ├── httpx/                  # response envelope, error catalogue, error mapping
│   ├── middleware/             # correlation, recovery, CORS, body limit, rate limit, authn/authz hooks
│   ├── audit/                  # async audit writer + idempotent retry
│   └── health/                 # liveness/readiness handlers
└── modules/                    # business modules (auth, user, ... added later)

migrations/                     # versioned SQL migrations (embedded)
configs/
└── .env.example                # documented environment variables (no secrets)
```

**Structure Decision**: Single Go module at the repository root. Cross-cutting
concerns are grouped under `internal/share/<area>` (a dedicated shared module
per the constitution). Future business capabilities are added under
`internal/modules/<module>` (and/or `internal/<module>` for top-level modules),
depending only on `internal/platform` and their own domain. `cmd/api` is the only
executable. This is a modular monolith — one binary, one database.

## Complexity Tracking

> No constitution violations. Table intentionally empty.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| (none) | — | — |
