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

## Where the behavior is documented

Do not duplicate it here — these files are authoritative:

- Endpoints, error codes, rate limits, token lifecycle:
  [`docs/api-reference.md`](../../../docs/api-reference.md) (§1 conventions, §3 auth)
- Layer responsibilities and dependency rules: [`docs/architecture.md`](../../../docs/architecture.md)
- Module scope and completion criteria: [`docs/modules/01-auth.md`](../../../docs/modules/01-auth.md)
- Requirements and clarifications: [`specs/001-user-auth/spec.md`](../../../specs/001-user-auth/spec.md)

## Commands

Use the Makefile targets rather than raw `go test`
(see [`docs/makefile.md`](../../../docs/makefile.md)):

```sh
make seed        # provision the admin account (ADMIN_EMAIL/ADMIN_PASSWORD)
make test        # unit + HTTP tests
make test-integration  # repository/HTTP tests against Docker
```
