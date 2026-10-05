# Phase 0 Research: Cross-Cutting Foundation

All technical unknowns from the plan's Technical Context are resolved here.
Each decision records the choice, rationale, and alternatives considered.

## 1. HTTP router and middleware composition

- **Decision**: `github.com/go-chi/chi/v5`.
- **Rationale**: Thin wrapper over `net/http`, idiomatic `http.Handler`
  middleware chaining, no framework lock-in, small dependency surface, strong fit
  for a modular monolith where each module mounts its own sub-router. Matches the
  constitution's simplicity principle.
- **Alternatives considered**: Gin/Echo (larger API surface, custom context,
  more lock-in); standard library only (more boilerplate for routing and
  middleware, no route groups); Fiber (non-`net/http`, unnecessary divergence).

## 2. Database driver and connection pooling

- **Decision**: `github.com/jackc/pgx/v5` with `pgxpool`.
- **Rationale**: First-class PostgreSQL support (types, `COPY`, `LISTEN/NOTIFY`,
  native protocol), superior performance, and a real connection pool. Required
  by the constitution's PostgreSQL constraint and by transactional integrity.
- **Alternatives considered**: `database/sql` + `lib/pq` (lib/pq is in
  maintenance mode; weaker type support); GORM connection layer (unnecessary
  abstraction).

## 3. Query layer / code generation

- **Decision**: `sqlc` generating type-safe Go from hand-written SQL.
- **Rationale**: Compile-time checked SQL, no runtime reflection, keeps SQL
  explicit and reviewable, produces testable repository methods. Aligns with
  module-owned repositories and transactional use.
- **Alternatives considered**: GORM (implicit behavior, harder to reason about
  performance and transactions); hand-written pgx scanning (more boilerplate and
  error-prone).

## 4. Schema migrations

- **Decision**: `github.com/pressly/goose/v3` with SQL migrations embedded via
  `embed.FS`, applied automatically at startup under a PostgreSQL advisory lock;
  also runnable via an explicit command.
- **Rationale**: Simple, supports up/down (rollback path per FR-004), embeddable
  so migrations ship with the binary, advisory lock prevents concurrent
  application when more than one instance starts (FR-004). Matches the
  clarified decision (auto on startup, lock, manual command available).
- **Alternatives considered**: `golang-migrate` (viable; goose chosen for simpler
  embedding and advisory-lock handling); Atlas (powerful but heavier and
  declarative-first, more than needed now); no library/raw SQL (no versioning or
  rollback guarantees).

## 5. Configuration loading

- **Decision**: `github.com/caarlos0/env/v11` for typed struct tags plus
  `github.com/joho/godotenv` for local development only; explicit startup
  validation with fail-fast.
- **Rationale**: Environment is the single source of truth (FR-001), typed
  parsing reduces stringly-typed bugs, `.env` convenience for local dev without
  changing production behavior. Missing/invalid required values fail fast
  (FR-002) with non-secret messages.
- **Alternatives considered**: Viper/Koanf (heavy, multi-source precedence not
  needed); raw `os.Getenv` (no parsing/validation, more boilerplate).

## 6. Structured logging

- **Decision**: standard library `log/slog` with the JSON handler; a redaction
  layer that never logs known sensitive keys; correlation ID attached as a
  logger attribute.
- **Rationale**: Zero third-party dependency, stable API, machine-parseable JSON
  (FR-010), straightforward redaction (FR-011) and correlation integration
  (FR-009). Matches simplicity principle.
- **Alternatives considered**: zerolog (fast but extra dependency); zap
  (powerful but heavier ergonomics); unstructured `log` (fails FR-010).

## 7. Error model and HTTP mapping

- **Decision**: A typed application error (`AppError`) carrying a stable code,
  HTTP status, safe message, and optional details; sentinel codes in an error
  catalogue; mapping performed in one place at the HTTP boundary. Internal
  errors are wrapped with context and logged, never exposed.
- **Rationale**: Single stable error format (FR-007), no internals leaked
  (FR-008), testable mapping, reusable across all modules.
- **Alternatives considered**: Ad-hoc `http.Error` (no consistency); returning
  raw `error` and mapping per handler (duplication, drift); third-party problem
  library (RFC 7807 — possible but adds a dependency and format constraints not
  yet required).

## 8. Response envelope

- **Decision**: Success `{ "data": <payload|null>, "meta": { ... } }`; error
  `{ "error": { "code", "message", "details?", "requestId" } }`. HTTP status
  carries the coarse outcome. Correlation ID also returned in `X-Request-Id`.
- **Rationale**: One envelope for all responses (FR-006), machine-readable code
  plus human message (FR-007), request correlation visible to clients (FR-009).
- **Alternatives considered**: Bare payloads for success (inconsistent handling
  on clients, no place for meta); `{success, data, error}` single shape (verbose
  and redundant with HTTP status).

## 9. Correlation ID

- **Decision**: Generate a UUID v4 when `X-Request-Id` is absent or malformed;
  accept and propagate a well-formed incoming value; store in request context,
  echo in the response header and error body, and attach to every log record.
- **Rationale**: Satisfies FR-009 and SC-004 with no external dependency.
- **Alternatives considered**: UUID v7 / ULID (time-sortable, but correlation IDs
  are not used as keys or ordered, so v4 suffices); server-generated only
  (breaks distributed tracing across the frontend and future services).

## 10. Rate limiting

- **Decision**: a small in-process token-bucket limiter built on
  `golang.org/x/time/rate`, keyed by client IP and route class (read/write/auth),
  with configurable requests-per-second and burst; no external store.
- **Rationale**: `httprate` (the initial candidate) uses a fixed window and does
  not support a burst parameter, but the configuration exposes `RATE_LIMIT_BURST`.
  `x/time/rate` expresses both rate and burst directly, is a first-party
  dependency, and remains sufficient for a single-instance modular monolith.
- **Alternatives considered**: `go-chi/httprate` (simpler but no burst support);
  Redis-backed limiter (rejected — no external store in scope); gateway/CDN
  limiting (no gateway in this deployment).

## 11. CORS policy

- **Decision**: `github.com/go-chi/cors` with an explicit allowlist of origins
  per environment (configured), restricted methods/headers, credentials only for
  allowlisted origins.
- **Rationale**: Deny-by-default is safer; frontend origins vary by environment.
- **Alternatives considered**: wildcard `*` (unsafe with credentials);
  no CORS handling (breaks browser clients).

## 12. Audit writer

- **Decision**: Buffered in-process queue consumed by a bounded worker pool;
  each audit event carries a deterministic idempotency key persisted under a
  unique constraint; failed writes retried with exponential backoff and surfaced
  via error logs/metrics; business operations are never blocked or failed.
- **Rationale**: Implements the clarified best-effort async model (FR-014) while
  guaranteeing exactly-once eventual persistence (SC-006) through the unique
  idempotency key.
- **Alternatives considered**: Synchronous in-transaction audit (rejected by
  clarification); unbounded goroutine per event (unbounded memory); external
  queue (out of scope).

## 13. Testing strategy and isolated database

- **Decision**: `go test` with the standard library assertions (no `testify`); unit
  tests for pure logic (config, error mapping, envelope encoding, redaction);
  integration tests via `testcontainers-go` PostgreSQL with migrations applied,
  behind the `integration` build tag; HTTP-level tests through `httptest` using
  the real router and middleware.
- **Rationale**: Satisfies FR-015 (isolated test DB, unit + API tests) and
  SC-008 (fast, clean-checkout run). Docker is already available in the
  environment.
- **Alternatives considered**: Shared long-lived test database (flaky,
  non-isolated); SQLite substitute (diverges from PostgreSQL behavior — rejected);
  pure mocks (insufficient for pool/transaction/migration behavior).

## 14. Conventions for keys and time

- **Decision**: UUID primary keys (default `gen_random_uuid()`); all timestamps
  `timestamptz` stored in UTC; snake_case identifiers in SQL; JSON keys
  lowerCamelCase. Monetary values (future modules) are integer minor units +
  currency.
- **Rationale**: Per constitution (Data constraints) and consistent client
  contracts.
- **Alternatives considered**: Auto-increment integers (guessable, weaker for
  public IDs); naive timestamps (timezone bugs).

## 15. Transaction management primitive

- **Decision**: A `platform/database` transaction manager exposing
  `WithTx(ctx, fn)` that commits on success and rolls back on error/panic; all
  module repository calls accept a `Querier` (pool or tx) drawn from context.
- **Rationale**: Centralizes the transactional integrity the constitution
  requires and lets future modules compose multi-step writes atomically. No
  business logic is added here.
- **Alternatives considered**: Per-handler `Begin/Commit` (error-prone,
  duplicated); ambient global tx (hidden, untestable).
