# Auth Module

Authentication and session management, organized with the project's Clean
Architecture (see `docs/architecture.md`).

## Structure

```
domain/
├── model/        # Account, Session, ResetRequest + password policy
├── constant/     # Status, error codes, audit actions
├── error/        # business sentinel errors
└── repository/   # UserRepository, SessionRepository, ResetRepository
application/
├── interface/    # AuthService + external-service ports + UnitOfWork
├── implement/    # use cases
├── dto/          # input/output DTOs
└── mapper/       # model ↔ dto
infrastructure/
└── implement/    # postgres, redis, token, email, auditor
presentation/
├── http/         # handlers, router, error mapping, AuthHooks
├── cli/          # seed command
├── worker/       # placeholder
└── dto/          # HTTP request/response DTOs
```

## Endpoints (`/api/v1/auth`)

`register`, `verify-email`, `resend-verification`, `login`, `refresh`, `logout`,
`password/forgot`, `password/reset`, `password/change`, `me`, `admin/probe`.

## Security

- Argon2id password hashing; 6-digit email OTP stored hashed in Redis with a
  3-strike blacklist and 1-minute resend cooldown.
- 15-minute HS256 JWT access tokens; 45-day opaque rotating refresh tokens.
- Access-token blacklist (per-`jti` and per-user minimum-`iat`) in Redis.
- Failed sign-in lockout (10 attempts / 15 minutes) and per-route rate limits.

## Commands

```sh
go run ./cmd/seed                  # provision admin (ADMIN_EMAIL/ADMIN_PASSWORD)
go test ./...                      # unit + HTTP tests
go test -tags integration ./...    # repository tests (Docker)
```
