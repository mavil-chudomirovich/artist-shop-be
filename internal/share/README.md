# Platform Foundation

Shared, cross-cutting building blocks for the modular-monolith backend. Business
modules under `internal/modules/<module>` depend on these packages; the platform
never depends on business modules.

## Packages

| Package | Responsibility |
|---------|----------------|
| `config` | Typed, validated configuration from environment variables; fail-fast startup |
| `logging` | Structured JSON logging via `log/slog` with secret redaction and correlation injection |
| `reqctx` | Request-scoped context values (correlation ID) |
| `database` | `pgxpool` connection and `WithTx` transaction manager |
| `database/migrate` | Versioned goose migrations embedded in the binary, applied under a PostgreSQL advisory lock |
| `httpserver` | Router assembly and HTTP server lifecycle |
| `httpx` | Response envelope, error catalogue, and error-to-response mapping |
| `middleware` | Correlation, recovery, body limit, CORS, rate limiting, and authn/authz hooks |
| `audit` | Append-only asynchronous audit writer with idempotent retry |
| `health` | Liveness (`/healthz`) and readiness (`/readyz`) signals |

## HTTP contract

All business endpoints live under `/api/v1` and follow
`specs/002-cross-cutting-foundation/contracts/http-conventions.md`. Errors use the
stable codes in `contracts/error-codes.md`.

## Authentication boundary

The foundation defines the `AuthHooks` interface and the `RequireAuthentication`
/ `RequireAdmin` enforcement middleware, but does not implement credential or
session logic. The auth module (module 01) supplies `AuthHooks` at the
composition root.

## Running

```sh
make run            # start the API (applies migrations automatically)
make test           # unit + API tests
make migrate-status # inspect schema version
```

## Integration tests

Tests requiring Docker and an isolated PostgreSQL are behind the `integration`
build tag and are skipped by default:

```sh
go test -tags integration ./...
```
