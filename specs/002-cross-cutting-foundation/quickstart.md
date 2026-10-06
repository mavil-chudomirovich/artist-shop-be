# Quickstart: Cross-Cutting Foundation

Runnable validation scenarios proving the foundation works end-to-end. This is a
validation/run guide, not an implementation specification.

## Prerequisites

- Go 1.26+
- Docker (for the local PostgreSQL and for integration tests via testcontainers)
- PowerShell 5.1+ (Windows) or a POSIX shell (Linux/macOS)

## Configuration

Copy `configs/.env.example` to `.env` and adjust values. All configuration comes
from the environment (FR-001); no environment-specific values are committed.

Documented variables (values are examples):

| Variable | Example | Purpose |
|----------|---------|---------|
| `APP_ENV` | `development` | Environment name (`development`/`staging`/`production`). |
| `HTTP_ADDR` | `:8080` | Listen address. |
| `DATABASE_URL` | `postgres://app:app@localhost:5432/artist_shop?sslmode=disable` | PostgreSQL DSN. |
| `DB_MAX_CONNS` | `10` | Pool size. |
| `LOG_LEVEL` | `info` | Log level. |
| `CORS_ALLOWED_ORIGINS` | `http://localhost:3000` | Comma-separated origin allowlist. |
| `RATE_LIMIT_RPS` | `20` | Default requests/second per client. |
| `RATE_LIMIT_BURST` | `40` | Burst allowance. |
| `MAX_BODY_BYTES` | `4194304` | Maximum request body size (coarse; routes apply their own ceiling). |

## 1. Start a local database

```powershell
docker run --name artist-shop-db -e POSTGRES_USER=app -e POSTGRES_PASSWORD=app `
  -e POSTGRES_DB=artist_shop -p 5432:5432 -d postgres:16
```

## 2. Run the service

```powershell
go run ./cmd/api
```

Expected: the service starts, applies migrations automatically (guarded by an
advisory lock), and begins listening. Startup fails fast with a clear,
non-secret message if a required variable is missing (SC-002).

## 3. Validate liveness and readiness

```powershell
Invoke-RestMethod http://localhost:8080/healthz
# => { "status": "alive" }

Invoke-RestMethod http://localhost:8080/readyz
# => { "status": "ready", "checks": { "database": "ok", "migrations": "ok" } }
```

Not-ready behavior: stop the database container and re-query `/readyz`; it MUST
return 503 with `status: not_ready` and a failed check (SC-005).

## 4. Validate the error contract

```powershell
# Unsupported method -> 405 with the standard error envelope
Invoke-WebRequest -Method Post http://localhost:8080/healthz -SkipHttpErrorCheck
# => status 405; body { "error": { "code": "METHOD_NOT_ALLOWED", ... } }
```

Every error body MUST include `error.code`, `error.message`, and
`error.requestId`, and the response MUST carry an `X-Request-Id` header (SC-003,
SC-004). No stack traces or internal details are returned.

## 5. Validate migrations

- Restart the service against an existing database: it must reach the expected
  schema version without error (FR-004).
- Run the explicit migration command (documented in the repository) to apply the
  same migrations without starting the server.
- Start two instances simultaneously against a fresh database: the advisory lock
  ensures migrations apply once.

## 6. Run the test suite

```powershell
go test ./...
```

- Unit tests cover config validation, error mapping, envelope encoding, and log
  redaction.
- Integration tests use an ephemeral PostgreSQL via testcontainers (Docker must
  be running); they are skipped if Docker is unavailable.
- Security check: with `go test ./...`, no secret value appears in captured logs
  (SC-007).

## 7. Clean-checkout validation

From a fresh clone, following the steps above (documented setup, `.env` from the
example, `go run ./cmd/api`, then `/healthz` and `/readyz`) MUST bring the service
up in under 10 minutes (SC-001).

## References

- Response/error contract: [contracts/http-conventions.md](./contracts/http-conventions.md)
- Error codes: [contracts/error-codes.md](./contracts/error-codes.md)
- Entities and conventions: [data-model.md](./data-model.md)
